// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package switchserver is the Matter Switch cluster server (0x003B): a
// semantic port of matter.js SwitchBaseServer
// (packages/node/src/behaviors/switch/SwitchServer.ts at 862ad75f), the
// server that derives every Switch event from one stream of switch
// positions. The directory is cluster/switch; `switch` is a Go keyword, so
// the package is named switchserver.
//
// The cluster's identity is its generated definition
// (cluster/spec/switchcluster, ADR 0013): ids, revision, the feature bits
// LS, MS, MSR, MSL, MSM and AS ([Config].Features, matter.js
// SwitchServer.with(...)), the attribute and event lists the selection
// yields, the write statuses and the event payloads. The behaviour is
// matter.js's:
//
//   - [Server.SetRawPosition] is matter.js `rawPosition`: debounced by the
//     debounce delay when one is set, decided at once otherwise
//     (#handleRawPosition :128-151, #handleDebouncedPosition :153-157).
//   - [Server.SetCurrentPosition] is a position the host debounced itself,
//     matter.js's write of `currentPosition` (#handleWrittenPosition
//     :177-190).
//   - Both end in the same decision (#decidePosition :230-326): the events
//     of a latching switch (SwitchLatched), of a momentary one
//     (InitialPress, ShortRelease, LongPress, LongRelease) and of its
//     multi-press sequences (MultiPressOngoing, MultiPressComplete), with
//     the long-press and multi-press timers on [clock.Clock].
//   - The decided events and CurrentPosition writes apply in the order the
//     inputs occurred (the result queue, #queueEvent :219-227 and
//     #applyResults :204-217).
//
// A source that knows only gestures keeps its path: the server implements
// [contract.SwitchEventEmitter], so a host that narrates InitialPress,
// ShortRelease, LongPress and LongRelease reaches the same events and
// moves CurrentPosition with them. Report a switch through positions or
// through gestures, not both — matter.js says the same of rawPosition and
// currentPosition (the class doc, :63-64).
package switchserver

import (
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/SukramJ/go-fabric/clock"
	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/cluster/spec"
	swdef "github.com/SukramJ/go-fabric/cluster/spec/switchcluster"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
)

// ClusterID is the Switch cluster id (0x003B).
const ClusterID = swdef.ClusterID

// Feature is a Switch FeatureMap bit, the generated type.
type Feature = swdef.Feature

// FeatureMap bits as [Feature] values for [Config].Features. The
// definition's conformance holds: exactly one of LS and MS; MSR only
// without AS; MSL with MSR or AS; MSM with MSR, and mandatory with AS.
const (
	FeatureLS  = swdef.FeatureLatchingSwitch
	FeatureMS  = swdef.FeatureMomentarySwitch
	FeatureMSR = swdef.FeatureMomentarySwitchRelease
	FeatureMSL = swdef.FeatureMomentarySwitchLongPress
	FeatureMSM = swdef.FeatureMomentarySwitchMultiPress
	FeatureAS  = swdef.FeatureActionSwitch
)

// The defaults of matter.js's added state (SwitchServer.ts:14-15,
// :465-475).
const (
	// DefaultLongPressDelay is DEFAULT_LONG_PRESS_DELAY, Seconds(2) (:15).
	DefaultLongPressDelay = 2 * time.Second
	// DefaultMultiPressDelay is DEFAULT_MULTIPRESS_DELAY, Millis(300) (:14).
	DefaultMultiPressDelay = 300 * time.Millisecond
	// DefaultNumberOfPositions is the State default (:474).
	DefaultNumberOfPositions uint8 = 2
	// DefaultMultiPressMax is the State default (:475).
	DefaultMultiPressMax uint8 = 2
)

// ErrConfig reports a configuration the cluster's model refuses.
var ErrConfig = errors.New("switch: configuration outside the cluster's model")

// ErrPositionOutOfRange reports a position outside 0..NumberOfPositions-1.
// The error [Server.SetRawPosition] and [Server.SetCurrentPosition] return
// wraps it and answers CONSTRAINT_ERROR (matter.js #assertPositionInRange
// :122-126).
var ErrPositionOutOfRange = errors.New("switch: position outside 0..NumberOfPositions-1")

// Config carries the construction parameters. The zero value of a delay
// or a count is matter.js's default.
type Config struct {
	// Features is the FeatureMap selection; the definition's conformance
	// is checked.
	Features Feature
	// Endpoint is the endpoint the events are addressed to. The bridge
	// also sets it at assembly ([Server.SetEndpoint]).
	Endpoint uint16
	// NumberOfPositions is the attribute; 0 is 2 (:474).
	NumberOfPositions uint8
	// MultiPressMax is the attribute (MSM only); 0 is 2 (:475).
	MultiPressMax uint8
	// CurrentPosition is the position the switch starts in; rawPosition
	// starts there too (initialize :81-82).
	CurrentPosition uint8
	// MomentaryNeutralPosition is the position a momentary switch rests
	// in (:471, default 0).
	MomentaryNeutralPosition uint8
	// DebounceDelay debounces [Server.SetRawPosition]; 0 decides a raw
	// position at once (debounceDelay, optional, :462).
	DebounceDelay time.Duration
	// LongPressDelay is the hold that makes a press long (MSL); 0 is
	// [DefaultLongPressDelay] (:465).
	LongPressDelay time.Duration
	// MultiPressDelay is the release that ends a multi-press sequence
	// (MSM); 0 is [DefaultMultiPressDelay] (:468).
	MultiPressDelay time.Duration
	// Clock runs the debounce, long-press and multi-press timers; nil is
	// [clock.System].
	Clock clock.Clock
	// DataVersion is an optional host-owned tracker.
	DataVersion *cluster.DataVersionTracker
}

// Server is the Switch server. It implements [contract.ClusterServer]
// through the generated [spec.Server] it embeds (reads, the read-only
// write answers, the lists, the data version and the change
// notification), [contract.EventReceiver], [contract.SwitchEventEmitter]
// and [contract.SwitchPositionSink].
//
// Locking. Every input — a position, a gesture, a timer — is decided
// under one mutex at the moment it occurs, as matter.js decides its input
// reactors synchronously and unlocked (initialize :88-94). The decided
// events and CurrentPosition writes leave the server in that order
// through one outbox, delivered with no lock held, so an event emitter or
// a change listener may read the server. matter.js's switch state lock —
// the transaction lock its #applyQueuedResults reactor and resetState wait
// for (:95, :104-105) — is the [Server.hold] seam: while it is held,
// queued results wait and [Server.ResetState] waits.
type Server struct {
	*spec.Server

	clock clock.Clock
	ls    bool
	msr   bool
	msl   bool
	msm   bool
	as    bool

	mu sync.Mutex
	// held is matter.js's switch state lock taken by another transaction;
	// lockFree is signalled when it is released.
	held     bool
	lockFree *sync.Cond
	// applyPending is a resultsQueued reaction that waits for the lock.
	applyPending bool

	endpoint uint16

	// State (SwitchServer.ts:454-476).
	numberOfPositions uint8
	multiPressMax     uint8
	currentPosition   uint8
	rawPosition       uint8
	debounceDelay     time.Duration
	longPressDelay    time.Duration
	multiPressDelay   time.Duration
	neutralPosition   uint8

	// Internal (SwitchServer.ts:401-452).
	debounceTimer   timerSlot
	longPressTimer  timerSlot
	multiPressTimer timerSlot
	results         []result
	// positionBeingWritten is the position this server writes to
	// currentPosition (-1: none).
	positionBeingWritten       int
	multiPressSequenceActive   bool
	currentNumberOfPresses     int
	multiPressReportingAborted bool
	currentPressReported       bool
	previouslyReportedPosition uint8
	previousMultiPressPosition int // -1: null
	currentLongPressPosition   int // -1: null
	currentIsLongPress         bool
	outbox                     []delivery
	draining                   bool
	// observe is the parity tests' seam: told about every change of
	// currentPosition and rawPosition, in delivery order.
	observe func(kind changeKind, oldValue, newValue uint8)
}

// result is one decided consequence of an input (SwitchBaseServer.Result,
// :389-399): an event to emit, or a write of currentPosition.
type result struct {
	event    uint32
	payload  any
	position uint8
	isWrite  bool
}

// changeKind names an attribute change in the outbox.
type changeKind uint8

const (
	changeEvent changeKind = iota
	changeCurrentPosition
	changeRawPosition
)

// delivery is one entry of the outbox: an event, or a change of
// currentPosition or rawPosition.
type delivery struct {
	kind     changeKind
	event    uint32
	payload  any
	oldValue uint8
	newValue uint8
}

// timerSlot is one of the three timers; gen tells a stale run (one that
// began before it was stopped) to do nothing, as matter.js's Timer.stop
// prevents the callback.
type timerSlot struct {
	t   clock.Timer
	gen uint64
}

func (t *timerSlot) stop() {
	if t.t != nil {
		t.t.Stop()
		t.t = nil
	}
	t.gen++
}

// statusError answers a Matter status.
type statusError struct {
	code im.StatusCode
	err  error
}

func (e statusError) Error() string                   { return e.err.Error() }
func (e statusError) Unwrap() error                   { return e.err }
func (e statusError) MatterStatusCode() im.StatusCode { return e.code }

// Compile-time assertions.
var (
	_ contract.ClusterServer          = (*Server)(nil)
	_ contract.EventReceiver          = (*Server)(nil)
	_ contract.ClusterAttributeLister = (*Server)(nil)
	_ contract.ClusterEventLister     = (*Server)(nil)
	_ contract.SwitchEventEmitter     = (*Server)(nil)
	_ contract.SwitchPositionSink     = (*Server)(nil)
)

// New builds a Switch server. The feature selection is held to the
// definition's conformance, NumberOfPositions and MultiPressMax to their
// "min 2", CurrentPosition and MomentaryNeutralPosition to the positions.
// Mirrors matter.js SwitchServer.ts:80-96 (initialize): rawPosition and
// the decided position start at CurrentPosition.
func New(cfg Config) (*Server, error) {
	n := cfg.NumberOfPositions
	if n == 0 {
		n = DefaultNumberOfPositions
	}
	initial := map[uint32]any{
		swdef.AttrNumberOfPositions: n,
		swdef.AttrCurrentPosition:   cfg.CurrentPosition,
	}
	msm := cfg.Features&FeatureMSM != 0
	mpm := cfg.MultiPressMax
	if mpm == 0 {
		mpm = DefaultMultiPressMax
	}
	if msm {
		initial[swdef.AttrMultiPressMax] = mpm
	}
	srv, err := spec.NewServer(swdef.Definition, spec.Options{Features: uint32(cfg.Features)},
		spec.ServerConfig{DataVersion: cfg.DataVersion, Initial: initial})
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrConfig, err)
	}
	if cfg.CurrentPosition >= n || cfg.MomentaryNeutralPosition >= n {
		return nil, fmt.Errorf("%w: CurrentPosition %d, MomentaryNeutralPosition %d with %d positions",
			ErrConfig, cfg.CurrentPosition, cfg.MomentaryNeutralPosition, n)
	}
	s := &Server{
		Server:                     srv,
		clock:                      clock.Or(cfg.Clock),
		ls:                         cfg.Features&FeatureLS != 0,
		msr:                        cfg.Features&FeatureMSR != 0,
		msl:                        cfg.Features&FeatureMSL != 0,
		msm:                        msm,
		as:                         cfg.Features&FeatureAS != 0,
		endpoint:                   cfg.Endpoint,
		numberOfPositions:          n,
		multiPressMax:              mpm,
		currentPosition:            cfg.CurrentPosition,
		rawPosition:                cfg.CurrentPosition,
		debounceDelay:              cfg.DebounceDelay,
		longPressDelay:             cmpOr(cfg.LongPressDelay, DefaultLongPressDelay),
		multiPressDelay:            cmpOr(cfg.MultiPressDelay, DefaultMultiPressDelay),
		neutralPosition:            cfg.MomentaryNeutralPosition,
		positionBeingWritten:       -1,
		currentNumberOfPresses:     1,
		previouslyReportedPosition: cfg.CurrentPosition,
		previousMultiPressPosition: -1,
		currentLongPressPosition:   -1,
	}
	s.lockFree = sync.NewCond(&s.mu)
	return s, nil
}

func cmpOr(d, def time.Duration) time.Duration {
	if d == 0 {
		return def
	}
	return d
}

// SetEndpoint sets the endpoint the events are addressed to; the bridge
// calls it at assembly.
func (s *Server) SetEndpoint(ep uint16) {
	s.mu.Lock()
	s.endpoint = ep
	s.mu.Unlock()
}

// Endpoint returns the endpoint the events are addressed to.
func (s *Server) Endpoint() uint16 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.endpoint
}

// CurrentPosition returns the CurrentPosition attribute as last written.
func (s *Server) CurrentPosition() uint8 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.currentPosition
}

// RawPosition returns matter.js's rawPosition: the last raw position
// reported, debounced or not.
func (s *Server) RawPosition() uint8 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.rawPosition
}

// SetDebounceDelay sets the debounce delay of [Server.SetRawPosition]; 0
// decides a raw position at once (matter.js state.debounceDelay, :462).
func (s *Server) SetDebounceDelay(d time.Duration) {
	s.mu.Lock()
	s.debounceDelay = d
	s.mu.Unlock()
}

// SetLongPressDelay sets the hold that makes a press long; 0 is
// [DefaultLongPressDelay] (state.longPressDelay, :465). A timer already
// running keeps its delay.
func (s *Server) SetLongPressDelay(d time.Duration) {
	s.mu.Lock()
	s.longPressDelay = cmpOr(d, DefaultLongPressDelay)
	s.mu.Unlock()
}

// SetMultiPressDelay sets the release that ends a multi-press sequence; 0
// is [DefaultMultiPressDelay] (state.multiPressDelay, :468).
func (s *Server) SetMultiPressDelay(d time.Duration) {
	s.mu.Lock()
	s.multiPressDelay = cmpOr(d, DefaultMultiPressDelay)
	s.mu.Unlock()
}

// SetMatterEventEmitter implements [contract.EventReceiver].
func (s *Server) SetMatterEventEmitter(emitter contract.EventEmitter) {
	s.Server.SetMatterEventEmitter(emitter)
}

// assertPositionInRange mirrors matter.js SwitchServer.ts:122-126; the
// caller holds s.mu.
func (s *Server) assertPositionInRange(position uint8) error {
	if position >= s.numberOfPositions {
		return statusError{im.StatusConstraintError, fmt.Errorf("%w: position %d invalid", ErrPositionOutOfRange, position)}
	}
	return nil
}

// SetRawPosition reports the switch's raw position, matter.js
// `state.rawPosition`: debounced by the debounce delay when one is set
// (SwitchServer.ts:128-151, the rawPosition$Changed reactor :90). A
// position outside the range is refused with CONSTRAINT_ERROR
// (#assertPositionInRange, the rawPosition$Changing reactor :85); an
// unchanged one changes nothing, as an unchanged state value fires no
// $Changed event in matter.js.
func (s *Server) SetRawPosition(position uint8) error {
	s.mu.Lock()
	if err := s.assertPositionInRange(position); err != nil {
		s.mu.Unlock()
		return err
	}
	s.setRawPositionLocked(position)
	s.mu.Unlock()
	s.drain()
	return nil
}

// setRawPositionLocked stores a raw position and reacts to its change;
// the caller holds s.mu.
func (s *Server) setRawPositionLocked(position uint8) {
	if position == s.rawPosition {
		return
	}
	old := s.rawPosition
	s.rawPosition = position
	s.push(delivery{kind: changeRawPosition, oldValue: old, newValue: position})
	s.handleRawPosition(position)
}

// SetCurrentPosition reports a position the host debounced itself,
// matter.js's write of `state.currentPosition` (#handleWrittenPosition
// SwitchServer.ts:177-190, the currentPosition$Changed reactor :91). The
// written position supersedes position writes still queued, not the
// events queued before it. A position outside the range is refused with
// CONSTRAINT_ERROR (:86); an unchanged one changes nothing.
func (s *Server) SetCurrentPosition(position uint8) error {
	s.mu.Lock()
	if err := s.assertPositionInRange(position); err != nil {
		s.mu.Unlock()
		return err
	}
	s.writeCurrentPosition(position)
	s.mu.Unlock()
	s.drain()
	return nil
}

// writeCurrentPosition is a write of currentPosition: matter.js runs the
// currentPosition$Changed reactor (#handleWrittenPosition) before any
// later observer, so its events leave before the change. The caller holds
// s.mu.
func (s *Server) writeCurrentPosition(position uint8) {
	if position == s.currentPosition {
		return
	}
	old := s.currentPosition
	s.currentPosition = position
	s.handleWrittenPosition(position)
	s.push(delivery{kind: changeCurrentPosition, oldValue: old, newValue: position})
}

// handleRawPosition mirrors matter.js SwitchServer.ts:128-151.
func (s *Server) handleRawPosition(position uint8) {
	s.debounceTimer.stop()

	// Nothing is left to debounce for the decided position, such as after
	// ResetState resynchronized rawPosition (:131-134).
	if position == s.previouslyReportedPosition {
		return
	}

	// With a debounce delay the raw position is debounced, else it is used
	// at once (:136-143).
	if s.debounceDelay > 0 {
		s.startTimer(&s.debounceTimer, s.debounceDelay, func() { s.handleDebouncedPosition(position) })
		return
	}

	// With nothing queued currentPosition equals the decided position, so
	// a direct write is decided correctly (:145-150).
	if len(s.results) > 0 {
		s.handleDebouncedPosition(position)
	} else {
		s.writeCurrentPosition(position)
	}
}

// handleDebouncedPosition mirrors matter.js SwitchServer.ts:153-157.
func (s *Server) handleDebouncedPosition(position uint8) {
	if s.queuePosition(position) {
		s.resultsQueued()
	}
}

// queuePosition decides a change to position and queues its events and
// its write of currentPosition; false if the position is unchanged.
// Mirrors matter.js SwitchServer.ts:164-171.
func (s *Server) queuePosition(position uint8) bool {
	if position == s.previouslyReportedPosition {
		return false
	}
	s.decidePosition(position)
	s.results = append(s.results, result{isWrite: true, position: position})
	return true
}

// handleWrittenPosition mirrors matter.js SwitchServer.ts:177-190: a write
// by the application is an input of its own; the server's own writes were
// decided when they were queued.
func (s *Server) handleWrittenPosition(position uint8) {
	if int(position) == s.positionBeingWritten {
		s.positionBeingWritten = -1
		return
	}

	// The written position supersedes queued writes, which would revert
	// it, but not the events queued before it (:183-184).
	s.results = slices.DeleteFunc(s.results, func(r result) bool { return r.isWrite })

	if position != s.previouslyReportedPosition {
		s.decidePosition(position)
	}
	s.applyResults()
}

// resultsQueued is matter.js's resultsQueued observable, whose reactor
// #applyQueuedResults runs with the switch state lock (:95, :192-196):
// while the lock is held the application waits for its release.
func (s *Server) resultsQueued() {
	if s.held {
		s.applyPending = true
		return
	}
	for s.applyResults() {
	}
}

// applyResults applies queued results in order, up to and including the
// next position write, so each write commits on its own and no change is
// coalesced away; it reports whether results remain. Mirrors matter.js
// SwitchServer.ts:204-217.
func (s *Server) applyResults() bool {
	for len(s.results) > 0 {
		r := s.results[0]
		s.results = s.results[1:]
		if !r.isWrite {
			s.push(delivery{kind: changeEvent, event: r.event, payload: r.payload})
			continue
		}
		s.positionBeingWritten = int(r.position)
		s.writeCurrentPosition(r.position)
		break
	}
	return len(s.results) > 0
}

// queueEvent mirrors matter.js SwitchServer.ts:219-227. An event the
// feature selection does not emit is dropped when it is applied, as
// matter.js's `event(server.events)?.emit` finds no such event.
func (s *Server) queueEvent(event uint32, payload any) {
	s.results = append(s.results, result{event: event, payload: payload})
}

// decidePosition decides the events of a change to position and starts or
// stops the timers it affects. Mirrors matter.js SwitchServer.ts:230-326.
func (s *Server) decidePosition(position uint8) {
	previousPosition := s.previouslyReportedPosition
	s.previouslyReportedPosition = position

	if s.ls {
		// Generated when the latching switch is moved to a new position
		// (:234-238).
		s.queueEvent(swdef.EventSwitchLatched, swdef.SwitchLatchedEvent{NewPosition: position})
		return
	}

	neutral := s.neutralPosition
	isPressed := position != neutral

	// A move between two pressed positions, as with a joystick, continues
	// the press instead of starting a new one (:243-244).
	isMove := isPressed && previousPosition != neutral

	// A press reported as long stays one press until its release; an
	// action switch reports one press per cycle (:248-251).
	var isNewPress bool
	if s.as {
		isNewPress = !isMove && !s.multiPressSequenceActive
	} else {
		isNewPress = !isMove || !s.currentIsLongPress
	}

	// Momentary switch (:253-262).
	if isPressed && isNewPress {
		if !s.multiPressReportingAborted {
			s.queueEvent(swdef.EventInitialPress, swdef.InitialPressEvent{NewPosition: position})
			s.currentPressReported = true
		} else if !isMove {
			s.currentPressReported = false
		}
	}

	switch {
	case s.msl:
		switch {
		case !isPressed:
			if s.currentIsLongPress {
				// Released after a LongPress since the previous
				// InitialPress (:266-270).
				s.queueEvent(swdef.EventLongRelease, swdef.LongReleaseEvent{PreviousPosition: previousPosition})
			} else if s.currentLongPressPosition >= 0 && !s.as && s.currentPressReported {
				// Released with no LongPress since the previous
				// InitialPress (:271-280).
				s.queueEvent(swdef.EventShortRelease, swdef.ShortReleaseEvent{PreviousPosition: previousPosition})
			}
			s.longPressTimer.stop()
			s.currentIsLongPress = false
			s.currentLongPressPosition = -1
		case isMove && (s.as || s.currentIsLongPress):
			// Long press detection restarts with each InitialPress, which
			// this move does not report (:285-287).
			s.currentLongPressPosition = int(position)
		default:
			// :288-296.
			s.longPressTimer.stop()
			s.currentIsLongPress = false
			s.currentLongPressPosition = int(position)
			s.startTimer(&s.longPressTimer, s.longPressDelay, s.handleLongPress)
		}
	case s.msr && !isPressed && s.currentPressReported:
		// Without MSL a release is short, even after a long hold
		// (:297-301).
		s.queueEvent(swdef.EventShortRelease, swdef.ShortReleaseEvent{PreviousPosition: previousPosition})
	}

	if !isPressed {
		s.currentPressReported = false
	}

	if s.msm {
		// :307-325.
		if !isPressed {
			if s.multiPressSequenceActive {
				s.previousMultiPressPosition = int(previousPosition)
				s.startTimer(&s.multiPressTimer, s.multiPressDelay, s.handleMultiPressComplete)
			}
		} else if !isMove {
			s.multiPressTimer.stop()
			if !s.multiPressSequenceActive {
				s.multiPressSequenceActive = true
			} else if !s.multiPressReportingAborted {
				s.countFurtherPress(position)
			}
		}
	}
}

// countFurtherPress mirrors matter.js SwitchServer.ts:328-340.
func (s *Server) countFurtherPress(position uint8) {
	s.currentNumberOfPresses++
	counted := s.currentNumberOfPresses

	if counted > int(s.multiPressMax) {
		s.multiPressReportingAborted = true
		return
	}

	if !s.as {
		s.queueEvent(swdef.EventMultiPressOngoing, swdef.MultiPressOngoingEvent{
			NewPosition: position, CurrentNumberOfPressesCounted: uint8(counted), //nolint:gosec // counted ≤ multiPressMax, a uint8
		})
	}
}

// handleLongPress runs when the switch was held in one press for the
// long-press delay. Mirrors matter.js SwitchServer.ts:343-354: a long press
// only starts a cycle of its own; inside a multi-press sequence it counts
// as a press.
func (s *Server) handleLongPress() {
	position := s.currentLongPressPosition
	if position < 0 || s.currentNumberOfPresses > 1 {
		return
	}
	s.queueEvent(swdef.EventLongPress, swdef.LongPressEvent{NewPosition: uint8(position)}) //nolint:gosec // a position, a uint8
	s.currentIsLongPress = true
	s.endMultiPressSequence()
	s.resultsQueued()
}

// handleMultiPressComplete runs when the switch stayed released for the
// multi-press delay. Mirrors matter.js SwitchServer.ts:357-370.
func (s *Server) handleMultiPressComplete() {
	if prev := s.previousMultiPressPosition; prev >= 0 {
		total := s.currentNumberOfPresses
		if s.multiPressReportingAborted {
			total = 0
		}
		s.queueEvent(swdef.EventMultiPressComplete, swdef.MultiPressCompleteEvent{
			PreviousPosition:            uint8(prev),  //nolint:gosec // a position, a uint8
			TotalNumberOfPressesCounted: uint8(total), //nolint:gosec // ≤ multiPressMax + 1, a uint8 count
		})
		s.resultsQueued()
	}
	s.endMultiPressSequence()
}

// endMultiPressSequence mirrors matter.js SwitchServer.ts:372-377.
func (s *Server) endMultiPressSequence() {
	s.multiPressSequenceActive = false
	s.currentNumberOfPresses = 1
	s.multiPressReportingAborted = false
	s.previousMultiPressPosition = -1
}

// ResetState starts a clean cycle, mainly for automated tests: it waits
// for the switch state lock, then drops pending debounced positions,
// timers and results not applied yet, so nothing from before the reset is
// reported or written after it, and continues from the current position.
// Mirrors matter.js SwitchServer.ts:98-119.
func (s *Server) ResetState() {
	s.mu.Lock()
	for s.held {
		s.lockFree.Wait()
	}
	s.resetLocked()
	s.mu.Unlock()
	s.drain()
}

// resetLocked is resetState's body (:106-116) under the lock.
func (s *Server) resetLocked() {
	s.debounceTimer.stop()
	s.multiPressTimer.stop()
	s.longPressTimer.stop()
	s.results = nil
	s.positionBeingWritten = -1
	s.previouslyReportedPosition = s.currentPosition
	s.currentLongPressPosition = -1
	s.currentIsLongPress = false
	s.currentPressReported = false
	s.endMultiPressSequence()
	s.setRawPositionLocked(s.currentPosition)
}

// hold takes matter.js's switch state lock for another transaction: until
// [Server.release], queued results wait and ResetState waits. It is the
// seam of the parity tests' holdingSwitchLock (SwitchServerTest.ts:98-104).
func (s *Server) hold() {
	s.mu.Lock()
	s.held = true
	s.mu.Unlock()
}

// release releases the lock taken by hold: the results queued meanwhile
// apply, then a waiting ResetState runs.
func (s *Server) release() {
	s.mu.Lock()
	s.held = false
	if s.applyPending {
		s.applyPending = false
		for s.applyResults() {
		}
	}
	s.lockFree.Broadcast()
	s.mu.Unlock()
	s.drain()
}

// startTimer arms slot to run fn under s.mu after d, unless it is stopped
// or re-armed first; the caller holds s.mu.
func (s *Server) startTimer(slot *timerSlot, d time.Duration, fn func()) {
	slot.stop()
	gen := slot.gen
	slot.t = s.clock.AfterFunc(d, func() {
		s.mu.Lock()
		if slot.gen != gen {
			s.mu.Unlock()
			return
		}
		slot.t = nil
		fn()
		s.mu.Unlock()
		s.drain()
	})
}

// push appends one delivery to the outbox; the caller holds s.mu.
func (s *Server) push(d delivery) { s.outbox = append(s.outbox, d) }

// drain delivers the outbox in order with no lock held. One goroutine
// drains at a time; an input on another goroutine meanwhile appends and
// leaves its deliveries to the running drain, so the order holds.
func (s *Server) drain() {
	s.mu.Lock()
	if s.draining {
		s.mu.Unlock()
		return
	}
	s.draining = true
	for len(s.outbox) > 0 {
		d := s.outbox[0]
		s.outbox = s.outbox[1:]
		endpoint, observe := s.endpoint, s.observe
		s.mu.Unlock()
		s.deliver(endpoint, observe, d)
		s.mu.Lock()
	}
	s.draining = false
	s.mu.Unlock()
}

func (s *Server) deliver(endpoint uint16, observe func(changeKind, uint8, uint8), d delivery) {
	switch d.kind {
	case changeEvent:
		// An event outside EventList is dropped (#queueEvent :225).
		_ = s.Emit(endpoint, d.event, d.payload)
		return
	case changeCurrentPosition:
		// Validated on input: the store's own check cannot fail.
		_ = s.Set(swdef.AttrCurrentPosition, d.newValue)
	case changeRawPosition:
	}
	if observe != nil {
		observe(d.kind, d.oldValue, d.newValue)
	}
}

// The gesture input: a source that narrates the Matter gestures itself
// reaches the same events, and the gestures move CurrentPosition — to the
// pressed position on InitialPress, back to the neutral position on a
// release — as matter.js's currentPosition moves during a press. The
// decision machine is not involved: the host decided the gesture.

// FireInitialPress implements [contract.SwitchEventEmitter]: InitialPress,
// then CurrentPosition moves to newPosition (when it is a position).
func (s *Server) FireInitialPress(newPosition uint8) {
	s.gesture(swdef.EventInitialPress, swdef.InitialPressEvent{NewPosition: newPosition}, newPosition, true)
}

// FireShortRelease implements [contract.SwitchEventEmitter]: ShortRelease
// (MSR), then CurrentPosition returns to the neutral position.
func (s *Server) FireShortRelease(previousPosition uint8) {
	s.gesture(swdef.EventShortRelease, swdef.ShortReleaseEvent{PreviousPosition: previousPosition}, s.neutral(), true)
}

// FireLongPress implements [contract.SwitchEventEmitter]: LongPress (MSL);
// CurrentPosition stays.
func (s *Server) FireLongPress(newPosition uint8) {
	s.gesture(swdef.EventLongPress, swdef.LongPressEvent{NewPosition: newPosition}, 0, false)
}

// FireLongRelease implements [contract.SwitchEventEmitter]: LongRelease
// (MSL), then CurrentPosition returns to the neutral position.
func (s *Server) FireLongRelease(previousPosition uint8) {
	s.gesture(swdef.EventLongRelease, swdef.LongReleaseEvent{PreviousPosition: previousPosition}, s.neutral(), true)
}

func (s *Server) neutral() uint8 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.neutralPosition
}

// gesture emits one narrated event and, with move, writes position to
// CurrentPosition; the decided position and rawPosition follow it, so a
// later position input is decided from there.
func (s *Server) gesture(event uint32, payload any, position uint8, move bool) {
	s.mu.Lock()
	s.push(delivery{kind: changeEvent, event: event, payload: payload})
	if move && position < s.numberOfPositions && position != s.currentPosition {
		old := s.currentPosition
		s.currentPosition, s.rawPosition, s.previouslyReportedPosition = position, position, position
		s.push(delivery{kind: changeCurrentPosition, oldValue: old, newValue: position})
	}
	s.mu.Unlock()
	s.drain()
}

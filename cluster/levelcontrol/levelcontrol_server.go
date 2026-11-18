// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package levelcontrol contains the Matter LevelControl cluster server
// (0x0008) — the cluster Matter offers for a characteristic that moves
// along a continuous scale, as against the labelled choices of
// ModeSelect.
//
// The server owns no level. Every attribute it answers is read from a
// [LevelSource] the host implements; a server that moved an internal
// field and reported Success would tell a controller the device changed
// without anything having reached it.
//
// # Two ways to move the level
//
// A device that ramps by itself takes the eight conformance-M commands as
// they arrive: by default the server forwards each to the port method of
// the same name, and the host applies TransitionTime and Rate natively
// (notes/parity/by_design.md BD-Matter-LevelControl-NativeRamp).
//
// A device that cannot ramp turns on the module's transition engine with
// [Config.Transitions]. The server then runs matter.js LevelControlServer
// itself — MoveToLevel, Move, Step and Stop, their On/Off coupling, the
// ExecuteIfOff gate, CoupleColorTempToLevel — on the transition engine of
// [transition] (matter.js behavior/Transitions.ts), and the host receives
// the levels it steps through as [LevelSource.MoveToLevel] requests with
// TransitionTime 0. CurrentLevel and RemainingTime are then reported by
// the server's own rules (see [Server.OnAttributeChange]).
//
// # What is served, and what is deliberately not
//
// Three attributes carry conformance "M": CurrentLevel (0x0), Options
// (0xf) and OnLevel (0x11)
// (matter.js packages/model/src/standard/elements/level-control.element.ts:29-30,
// :65, :49-50), and MinLevel (0x2) / MaxLevel (0x3), whose "Rev >= v7, O"
// makes them mandatory at the revision served (:34-41; matter.js sets both
// on every LevelControl, LevelControlServer.ts:114-120). With
// [Config.Lighting] the server advertises the LT feature and adds the two
// attributes LT makes mandatory, RemainingTime (0x1) and
// StartUpCurrentLevel (0x4000) (element :33, :68-71); MinLevel is then 1.
// Every other attribute is feature-gated or optional and is absent here,
// because advertising an attribute whose feature bit is clear is the shape
// a controller can end a commissioning over:
//
//   - CurrentFrequency (0x4), MinFrequency (0x5) and MaxFrequency (0x6)
//     carry "FQ" (:42-47);
//   - OnOffTransitionTime (0x10), OnTransitionTime (0x12),
//     OffTransitionTime (0x13) and DefaultMoveRate (0x14) carry "O"
//     (:48, :53-64). The engine reads them as matter.js does when a state
//     leaves them unset: a MoveToLevel without TransitionTime and a Move
//     without Rate apply at once.
//
// MoveToClosestFrequency (0x8) is likewise absent: conformance "FQ"
// (element :118-124). The eight commands this server does handle are all
// conformance "M" (element :72, :80, :88, :97, :101, :105, :109, :113),
// so they are accepted whatever the FeatureMap says.
//
// # The On/Off coupling
//
// Four of the eight commands are the "with On/Off" variants, which differ
// from their plain forms only in that they also drive the OnOff cluster
// on the same endpoint. Forwarded, they reach the host through their own
// port methods rather than through a flag on a shared call: a flag inside
// a request struct is a field a host implementation can leave unread and
// still compile, which turns "turn the speaker on and set it to 40" into
// a silent "set it to 40 while it stays muted". A separate method cannot
// be left unimplemented — the interface does not accept the host until
// each of the eight exists.
//
// Forwarded, the server does not decide whether a plain (non-On/Off)
// command executes while the device is off. That gate reads the OnOff
// attribute of a different cluster on the same endpoint
// (matter.js LevelControlServer.ts:729-736 #optionsAllowExecution), which
// the host owns. The arithmetic that *is* this cluster's — folding
// OptionsMask and OptionsOverride onto the Options attribute — is
// [EffectiveOptions], so a host applies the gate without re-deriving the
// bitmap rule. With the engine the host names its On/Off state in
// [Transitions.OnOff] and the server applies the gate and the coupling
// itself.
//
// # The generated definition
//
// The cluster's identity is the generated definition (cluster/spec/
// levelcontrol, ADR 0013): ids, revision, the attribute and command lists
// of the selection, the Options bits the selection makes conformant, the
// write checks (OnLevel's "minLevel to maxLevel" through the definition's
// peer bound) and the write privileges. The requests keep the bridge's
// lenient decoding — MoveToLevel and MoveToLevelWithOnOff its hand-written
// decoder, the other six the tag map — because Google Home omits the
// mandatory-but-nullable TransitionTime, which the generated decoder
// refuses (notes/parity/by_design.md
// BD-Matter-LevelControl-LenientDecoders); the generated request structs
// are accepted from an in-process caller and carried over to the
// cluster/wire structs the server's engine drives; a host's [LevelSource]
// receives the generated requests again (port.go).
package levelcontrol

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"sync/atomic"

	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/cluster/spec"
	lvldef "github.com/SukramJ/go-fabric/cluster/spec/levelcontrol"
	"github.com/SukramJ/go-fabric/cluster/transition"
	"github.com/SukramJ/go-fabric/cluster/wire"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
)

// ClusterID is the LevelControl cluster id.
const ClusterID = lvldef.ClusterID

// Attribute ids of the served attributes; the package doc names the gated
// and optional ones this server leaves out.
const (
	// AttrCurrentLevel is the level the device is at, access "R V",
	// quality "X N S Q" — the X is why an unobserved level reads as TLV
	// null rather than as 0, which is a real level
	// (element :29-30).
	AttrCurrentLevel = lvldef.AttrCurrentLevel
	// AttrOptions is the OptionsBitmap that sets the default behaviour of
	// the commands that consult it, access "RW VO" (element :65).
	AttrOptions = lvldef.AttrOptions
	// AttrMinLevel and AttrMaxLevel carry conformance "Rev >= v7, O"
	// (element :34-41): mandatory at the revision this server advertises.
	// matter.js sets both on every LevelControl (LevelControlServer.ts:
	// 114-120 initialize: "Spec 1.5.1 made minLevel/maxLevel mandatory at
	// rev 7"), to 0 (1 with LT) and 254. Found missing by the CHIP
	// conformance checker (TC-IDM-10.2).
	AttrMinLevel = lvldef.AttrMinLevel
	AttrMaxLevel = lvldef.AttrMaxLevel
	// AttrOnLevel is the level CurrentLevel is set to when an On/Off
	// cluster on the same endpoint turns on, access "RW VO", quality "X"
	// — null means "no effect" (element :49-50, resource :144-153).
	AttrOnLevel = lvldef.AttrOnLevel
	// AttrRemainingTime is the time left in the current transition, in
	// tenths of a second, access "R V", quality "Q", conformance "LT"
	// (element :33). Served with [Config.Lighting].
	AttrRemainingTime = lvldef.AttrRemainingTime
	// AttrStartUpCurrentLevel is the level the device takes at power-up,
	// access "RW VM", quality "X N", conformance "LT" (element :68-71).
	// Served with [Config.Lighting].
	AttrStartUpCurrentLevel = lvldef.AttrStartUpCurrentLevel
)

// Command ids, from the generated definition. All eight are conformance "M" with response "status", so none of
// them produces a generated command
// (element :72, :80, :88, :97, :101, :105, :109, :113).
const (
	CmdMoveToLevel          = lvldef.CmdMoveToLevel
	CmdMove                 = lvldef.CmdMove
	CmdStep                 = lvldef.CmdStep
	CmdStop                 = lvldef.CmdStop
	CmdMoveToLevelWithOnOff = lvldef.CmdMoveToLevelWithOnOff
	CmdMoveWithOnOff        = lvldef.CmdMoveWithOnOff
	CmdStepWithOnOff        = lvldef.CmdStepWithOnOff
	CmdStopWithOnOff        = lvldef.CmdStopWithOnOff
)

// FeatureMap bits, named so a later projection has the verified bit
// positions rather than a guess. The element gives each one as a
// constraint, which is the bit index (element :24-26).
const (
	// FeatureOnOff is the OO bit — this cluster is coupled to an OnOff
	// cluster on the same endpoint. Conformance "O", default 1 (:24).
	FeatureOnOff = uint32(lvldef.FeatureOnOff)
	// FeatureLighting is the LT bit — the lighting profile, which adds
	// RemainingTime and StartUpCurrentLevel. Conformance "O" (:25).
	FeatureLighting = uint32(lvldef.FeatureLighting)
	// FeatureFrequency is the FQ bit — a frequency axis alongside the
	// level one. Conformance "P" (:26).
	FeatureFrequency = uint32(lvldef.FeatureFrequency)
)

// featureMap is what this server advertises by default: OO alone.
//
// OO is the element's own default (default 1, :24) and it gates no
// attribute, so advertising it promises nothing this server does not
// serve — it says the four "with On/Off" commands act on a real OnOff
// cluster, which is exactly what the port's four WithOnOff methods do.
// LT is added with [Config.Lighting], together with the attributes it
// gates; FQ stays clear because this server serves none of its
// attributes.
const featureMap uint32 = FeatureOnOff

// OptionsBitmap bits (element :127-130). ExecuteIfOff carries
// conformance "LT | OO" and CoupleColorTempToLevel carries "LT".
const (
	// OptionExecuteIfOff, when set, lets a plain (non-On/Off) command run
	// even while the device is off (resource :190-199).
	OptionExecuteIfOff = uint8(lvldef.OptionsExecuteIfOff)
	// OptionCoupleColorTempToLevel, when set, moves the ColorControl
	// colour temperature along with the level. LT-gated: accepted with
	// [Config.Lighting] only.
	OptionCoupleColorTempToLevel = uint8(lvldef.OptionsCoupleColorTempToLevel)
)

// optionsSupportedMask is the set of Options bits that are conformant
// under [featureMap]: ExecuteIfOff exists under "LT | OO" and OO is
// advertised; CoupleColorTempToLevel needs LT ([Server.optionsMask]). A
// write carrying any other bit is refused rather than stored, because a
// stored bit would be read back as a capability the cluster does not
// have.
const optionsSupportedMask = OptionExecuteIfOff

// MoveMode values, taken from the wire package (MoveModeEnum, element
// :131-135).
const (
	MoveModeUp   = wire.LevelMoveModeUp
	MoveModeDown = wire.LevelMoveModeDown
)

// StepMode values, taken from the wire package (StepModeEnum, element
// :136-140).
const (
	StepModeUp   = wire.LevelStepModeUp
	StepModeDown = wire.LevelStepModeDown
)

// LevelSource is the narrow host port this server reads and drives, in the
// generated requests of cluster/spec/levelcontrol — what
// levelcontroldef.Declare carries. The documentation of its methods
// follows.
//
// A controller's Options or OnLevel write reaches SetOptions or
// SetOnLevel only when the written value differs from the one Options or
// OnLevel reads at that moment; a write of the held value reaches no
// setter. A write that reaches a setter counts as a change — its
// DataVersion advances and it is reported — whether or not the getter
// shows it when the setter returns, so a host may apply it later.
type LevelSource = lvldef.LevelSource

// source is the LevelSource the server drives, in cluster/wire's requests. The
// host implements it over whatever actually owns the level; nothing in
// this package caches or invents a value.
//
// CurrentLevel and OnLevel report (value, known). known=false becomes a
// TLV null, which is what quality X asks for on both (element :29-30,
// :49-50) — the spec's own way of saying the value is not known, as
// opposed to reporting level 0 for a device that has not answered yet.
// Options carries no X quality (element :65), so it has no unknown
// reading: the host names the bitmap a controller should see.
//
// The eight command methods own the southbound urgency of the writes
// they perform: the cluster contract carries no priority, so a host whose
// command queue ranks by urgency names the value it wants inside its own
// implementation, where the device vocabulary is in scope.
//
// The four WithOnOff methods exist separately from their plain forms on
// purpose — see the package doc. Implementing one by delegating to the
// other is a decision the host writes down; forgetting the coupling is
// not something it can do by accident.
//
// Under [Config.Transitions] the server calls MoveToLevel alone, with
// TransitionTime 0 and ExecuteIfOff forced, once per level it applies;
// the other seven command methods are not called.
//
// The server drives the cluster/wire form internally (source); a host's
// [LevelSource] — the same methods in the generated requests — reaches it
// through portSource, the module's [NativeSource] directly.
type source interface {
	// CurrentLevel reports the level the device is at.
	CurrentLevel() (level uint8, known bool)
	// Options reports the stored OptionsBitmap.
	Options() (options uint8)
	// OnLevel reports the configured on-level.
	OnLevel() (level uint8, known bool)

	// SetOptions persists a bitmap the server has already checked
	// against the advertised FeatureMap; it is called only for a bitmap
	// that differs from what Options read.
	SetOptions(ctx context.Context, options uint8) error
	// SetOnLevel persists a new on-level; level is nil for a null write,
	// which is the spec's "OnLevel has no effect". It is called only for
	// an on-level that differs from what OnLevel read.
	SetOnLevel(ctx context.Context, level *uint8) error

	// MoveToLevel drives the device to req.Level without touching OnOff.
	MoveToLevel(ctx context.Context, req wire.MoveToLevelRequest) error
	// MoveToLevelWithOnOff drives the device to req.Level and drives the
	// endpoint's OnOff state with it.
	MoveToLevelWithOnOff(ctx context.Context, req wire.MoveToLevelRequest) error
	// Move starts a continuous move without touching OnOff.
	Move(ctx context.Context, req wire.MoveRequest) error
	// MoveWithOnOff starts a continuous move and drives the endpoint's
	// OnOff state with it.
	MoveWithOnOff(ctx context.Context, req wire.MoveRequest) error
	// Step applies one step without touching OnOff.
	Step(ctx context.Context, req wire.StepRequest) error
	// StepWithOnOff applies one step and drives the endpoint's OnOff
	// state with it.
	StepWithOnOff(ctx context.Context, req wire.StepRequest) error
	// Stop halts a running move or step without touching OnOff.
	Stop(ctx context.Context, req wire.StopRequest) error
	// StopWithOnOff halts a running move or step and drives the
	// endpoint's OnOff state with it.
	StopWithOnOff(ctx context.Context, req wire.StopRequest) error
}

// EffectiveOptions folds a command's OptionsMask and OptionsOverride
// onto the stored Options attribute and returns the temporary bitmap
// that is in effect for that command (resource :182-186).
//
// Per bit: a set mask bit takes the bit from override, a clear mask bit
// keeps the attribute's bit. Mirrors matter.js
// LevelControlServer.ts:714-727 #calculateEffectiveOptions
//
//	executeIfOff: optionsMask.executeIfOff ? optionsOverride.executeIfOff : options.executeIfOff
//
// The result is masked to [optionsSupportedMask] for the same reason a
// write is: a bit whose feature is not advertised has no meaning here.
//
// It is exported because the ExecuteIfOff gate itself belongs to the
// host — it reads the OnOff cluster's state, which this server cannot
// see — and the host should not have to restate the bitmap rule to
// apply it.
func EffectiveOptions(attribute, mask, override uint8) uint8 {
	return ((attribute &^ mask) | (override & mask)) & optionsSupportedMask
}

// Config carries the construction parameters for [Server].
type Config struct {
	// Source is the level the server drives: a host's [LevelSource] or the
	// module's own ([NativeSource]). A nil Source makes every attribute
	// read null and every command fail — the server never pretends to have
	// moved a device it cannot reach.
	Source LevelSource
	// DataVersion is an optional tracker owned by the host. When non-nil
	// the server bumps and reports the caller's counter, so a level the
	// device changed on its own reaches a subscriber the moment the host
	// bumps it, and the version survives server reconstruction; when nil
	// an embedded tracker is used.
	//
	// Deprecated: no controller reads this tracker — the wire carries the
	// endpoint's DataVersion for the cluster, which the bridge keeps
	// (decision 165, F-db021da0-46); leave it nil. Removable in
	// v0.25.0.
	DataVersion *cluster.DataVersionTracker
	// Lighting advertises the LT feature: MinLevel 1, RemainingTime,
	// StartUpCurrentLevel and the CoupleColorTempToLevel option
	// (level-control.element.ts:25, :33, :68-71, :127-130). A light needs
	// it — every lighting device type marks LT mandatory. This server
	// keeps StartUpCurrentLevel itself and reports it, but does not apply
	// it: the level is the host's port's, and a host that powers up its
	// device applies the value itself. The server that holds its own
	// level ([New]) applies it at start-up unless it is bridged, as
	// matter.js skips start-up behaviour only on an endpoint an Aggregator
	// owns (LevelControlServer.ts:222-245, [HeldConfig.StartUpCurrentLevel]).
	Lighting bool
	// Features, with HasFeatures, is the FeatureMap the server advertises
	// — OO and LT, each optional, so every subset conforms — in place of
	// OO with Lighting above: a device kit's selection, which matter.js's
	// LevelControlServer starts empty: it is LevelControlBaseServer.with()
	// (LevelControlServer.ts:865), and a defined feature list, the empty
	// one included, is applied as the selection (packages/node/src/
	// behavior/cluster/ClusterBehaviorType.ts:75-76, `features !==
	// undefined` → applyFeatureSelection). LT then decides the Lighting
	// surface.
	Features    uint32
	HasFeatures bool
	// Transitions, when non-nil, runs the module's transition engine
	// instead of forwarding the commands (see the package doc). Nil keeps
	// the hand-off path, where the host ramps natively and RemainingTime
	// reads 0.
	Transitions *Transitions
}

// Server implements [contract.ClusterServer] for LevelControl (0x0008).
type Server struct {
	src      source
	lighting bool
	// featureBits is the advertised FeatureMap: OO, and LT with lighting.
	featureBits uint32
	inst        *spec.Instance

	// held is the server's own state when [New] built it; nil over a
	// host's [LevelSource].
	held *heldLevel
	// host, events and veto are the typed server's command handlers,
	// event path and write veto (typed.go).
	host   spec.HostHandlers
	events spec.Events
	veto   spec.WriteVeto

	embedded cluster.DataVersionTracker // used when Config.DataVersion is nil
	ext      *cluster.DataVersionTracker

	// The transition engine and what it couples to; engine is nil on the
	// hand-off path.
	engine *transition.Engine
	onOff  OnOff
	ct     ColorTemperatureCoupling
	// cmd serialises the commands the engine runs.
	cmd sync.Mutex
	// changes and quiet report CurrentLevel and RemainingTime under the
	// engine.
	changes cluster.AttributeChanges
	quiet   *cluster.Quieter
	// quiescing suppresses the quieter's report while MatterQuiesce drops
	// the one it holds back.
	quiescing atomic.Bool
	// levelOrigin is the contract.Origin of the last level change, for the
	// quieter's report of it.
	levelOrigin atomic.Uint32
	// forwarding counts the commands being forwarded to the port on the
	// hand-off path and the levels the engine is handing it, so a change
	// the host's notifier reports meanwhile is the command's.
	forwarding atomic.Int32
	// noteMu guards noted and makes a notification's count, the level it
	// read and its advance of the DataVersion one step.
	noteMu sync.Mutex
	// noted is the last host notification [Server.OnMatterValueChanged]
	// advanced the DataVersion for — their count and the level it read —,
	// against which a hand-off command's move is compared
	// ([Server.bumpIfMoved]).
	noted levelReading

	// mu guards startUp.
	mu sync.Mutex
	// startUp is StartUpCurrentLevel; nil is null.
	startUp *uint8
	// level / levelKnown is the CurrentLevel last seen, so a change is
	// reported once, by the quieter rules.
	level      uint8
	levelKnown bool
	// coupling is what the running transition couples to, recorded under
	// couplingGen, the generation of the command that started it
	// ([Server.nextCoupling]).
	coupling    coupling
	couplingGen uint64
	// onSeenGen is the generation of the with-On/Off transition a step
	// has read the device on during; offEndedGen the one a step then read
	// it off during, which the next step's apply ends
	// ([Server.stepCouple], R2-P6-fsm-1401).
	onSeenGen, offEndedGen uint64
	// observe serialises an observation of CurrentLevel: the read from
	// the source and the compare-and-store ([Server.noteLevel]).
	observe sync.Mutex
	// listeners counts the change listeners and forwards holds the
	// callbacks [Server.OnMatterValueChanged] forwards the host's
	// notifications to, in registration order; the host's notifier is
	// subscribed once while either has one ([Server.reconcileHost]).
	listeners  int
	forwards   []forward
	forwardSeq uint64
	unsubSrc   func()
	// subMu serialises subscribing to and leaving the host's notifier.
	subMu sync.Mutex
	// wasOn is the OnOff a held server last saw (followOnOff).
	wasOn bool

	// scenes is the endpoint's ScenesManagement ([Server.SetScenes]); nil
	// without one.
	scenes atomic.Pointer[scenesRef]
}

// Compile-time assertions.
var (
	_ contract.ClusterServer                  = (*Server)(nil)
	_ contract.ClusterDataVersion             = (*Server)(nil)
	_ contract.ClusterAttributeLister         = (*Server)(nil)
	_ contract.ClusterCommandLister           = (*Server)(nil)
	_ contract.ClusterAttributeWritePrivilege = (*Server)(nil)
	_ contract.ChangeNotifier                 = (*Server)(nil)
	_ contract.AttributeChangeNotifier        = (*Server)(nil)
	_ contract.AttributeReportNotifier        = (*Server)(nil)
	_ contract.SelfReportedAttributeLister    = (*Server)(nil)
	_ contract.ClusterQuiescer                = (*Server)(nil)
)

// NewServer constructs a LevelControl server over the host port in cfg.
func NewServer(cfg Config) *Server {
	features := featureMap
	if cfg.Lighting {
		features |= FeatureLighting
	}
	if cfg.HasFeatures {
		features = cfg.Features & (featureMap | FeatureLighting)
	}
	s := newServer(sourceOf(cfg.Source), cfg.DataVersion, features)
	if cfg.Transitions != nil {
		t := *cfg.Transitions
		// Coupled to OnOff under OO only, as [New] is: without the
		// feature a "with On/Off" command switches nothing and no command
		// is gated on the On/Off state. Mirrors matter.js
		// packages/node/src/behaviors/level-control/LevelControlServer.ts:558
		// at db021da0 (`const coupleOnOff = this.features.onOff && withOnOff && …`)
		// and :729-736 (`#optionsAllowExecution`).
		if features&FeatureOnOff == 0 {
			t.OnOff = nil
		}
		s.initEngine(t, true)
	} else if s.src != nil {
		s.level, s.levelKnown = s.src.CurrentLevel()
	}
	return s
}

// newServer is a server over src advertising features — OO and LT, each
// optional: every subset is a conformant selection, so the instance has
// nothing to refuse. MinLevel and MaxLevel are "Rev >= v7, O": served at
// the revision advertised.
func newServer(src source, dv *cluster.DataVersionTracker, features uint32) *Server {
	s := &Server{src: src, ext: dv, lighting: features&FeatureLighting != 0, featureBits: features}
	s.inst, _ = spec.New(lvldef.Definition, spec.Options{
		Features:   features,
		Attributes: []uint32{AttrMinLevel, AttrMaxLevel},
	})
	return s
}

// minLevel is MinLevel: 1 under the Lighting feature, 0 otherwise
// (matter.js LevelControlServer.ts:89-91).
func (s *Server) minLevel() uint8 {
	if s.lighting {
		return lvldef.LightingLevelMin
	}
	return lvldef.LevelMin
}

// optionsMask is the set of Options bits the advertised FeatureMap makes
// conformant: the OptionsBitmap members whose conformance ("LT | OO",
// "LT") the selection does not disallow.
func (s *Server) optionsMask() uint8 {
	var mask uint64
	ctx := s.inst.Context()
	for i := range lvldef.OptionsBitmapDef.Members {
		if m := &lvldef.OptionsBitmapDef.Members[i]; m.Conformance.Applicability(ctx) != spec.ApplicabilityNone {
			mask |= m.Mask()
		}
	}
	return uint8(mask) //nolint:gosec // a map8's members
}

// features is the advertised FeatureMap.
func (s *Server) features() uint32 { return s.featureBits }

// tracker returns the active DataVersion counter — the host's when it
// supplied one, the embedded one otherwise.
func (s *Server) tracker() *cluster.DataVersionTracker {
	if s.ext != nil {
		return s.ext
	}
	return &s.embedded
}

// OnMatterValueChanged implements [contract.ChangeNotifier] by
// forwarding the host port's own notifier, bumping the DataVersion
// before each fire so the report the bridge ships carries the
// post-change version. CurrentLevel carries quality "N ... Q"
// (element :29-30) and is the attribute this server reports; without
// this hop a level the device changed by itself — a speaker turned up at
// the device — would reach a controller only on its next read. Mirrors
// the forwarding in cluster/modeselect/modeselect_server.go. Returns a
// no-op unsubscribe when the port cannot notify.
//
// Under the engine the server reports CurrentLevel itself (see
// [Server.OnAttributeChange]) and this hop stays silent.
//
// A host notification reaches every forwarded callback before the server
// observes CurrentLevel for its change listeners ([Server.observeLevel]):
// a notifier wiring that compares the values it reads sees the change
// first, and the change the listeners are told of after is one it saw —
// the bridge advances the cluster's DataVersion once for it, not again
// for the listeners' change (bridge quieterChanged).
func (s *Server) OnMatterValueChanged(cb func()) (unsubscribe func()) {
	n, ok := s.src.(contract.ChangeNotifier)
	if !ok || n == nil || s.engine != nil {
		return func() {}
	}
	s.mu.Lock()
	id := s.forwardSeq
	s.forwardSeq++
	s.forwards = append(s.forwards, forward{id: id, cb: cb})
	s.mu.Unlock()
	s.reconcileHost()
	var once sync.Once
	return func() {
		once.Do(func() {
			s.mu.Lock()
			s.forwards = slices.DeleteFunc(s.forwards, func(f forward) bool { return f.id == id })
			s.mu.Unlock()
			s.reconcileHost()
		})
	}
}

// forward is one callback [Server.OnMatterValueChanged] forwards to.
type forward struct {
	id uint64
	cb func()
}

// onHost is the server's one subscription to the host's notifier: the
// forwarded callbacks first, each after the version moved, then, while a
// change listener is registered, the server's own observation of
// CurrentLevel.
func (s *Server) onHost() {
	s.mu.Lock()
	cbs := make([]func(), len(s.forwards))
	for i, f := range s.forwards {
		cbs[i] = f.cb
	}
	follow := s.listeners > 0
	s.mu.Unlock()
	for _, cb := range cbs {
		level, known := s.sourceLevel()
		s.noteMu.Lock()
		s.noted = levelReading{level, known, s.noted.notified + 1}
		s.tracker().Bump()
		s.noteMu.Unlock()
		if cb != nil {
			cb()
		}
	}
	if follow {
		s.observeLevel(s.sourceOrigin())
	}
}

// reconcileHost subscribes to the host's notifier while a change listener
// or a forwarded callback is registered and leaves it when neither is.
func (s *Server) reconcileHost() {
	n, ok := s.src.(contract.ChangeNotifier)
	if !ok || n == nil {
		return
	}
	s.subMu.Lock()
	defer s.subMu.Unlock()
	s.mu.Lock()
	need := s.listeners > 0 || len(s.forwards) > 0
	have := s.unsubSrc != nil
	s.mu.Unlock()
	switch {
	case need && !have:
		stop := n.OnMatterValueChanged(s.onHost)
		s.mu.Lock()
		s.unsubSrc = stop
		s.mu.Unlock()
	case !need && have:
		s.mu.Lock()
		stop := s.unsubSrc
		s.unsubSrc = nil
		s.mu.Unlock()
		stop()
	}
}

// Revision returns the cluster revision of the generated definition.
// Reading it rather than restating it is the point: a regeneration moves
// this value, and a hand-written copy would not follow.
func Revision() uint16 { return lvldef.Revision }

// MatterClusterID returns 0x0008.
func (*Server) MatterClusterID() uint32 { return ClusterID }

// MatterDataVersion implements [contract.ClusterDataVersion]: the
// server's own tracker, the host's when it supplied one.
//
// Deprecated: no controller reads this version — the wire carries the
// endpoint's DataVersion for the cluster, which the bridge keeps
// (decision 165, F-db021da0-46). Removable in v0.25.0.
func (s *Server) MatterDataVersion() uint32 { return s.tracker().Current() }

// MatterRead resolves the three conformance-M attributes plus the two
// universal globals every cluster server answers itself.
//
// With no host port all three read as TLV null. Only CurrentLevel and
// OnLevel carry quality X, so null is not the reading the spec asks for
// on Options — but the alternative is a bitmap the host never stated,
// and a value invented here is indistinguishable on the wire from one
// the host confirmed.
func (s *Server) MatterRead(attrID uint32) (value any, ok bool) {
	switch attrID {
	case AttrCurrentLevel, AttrOptions, AttrOnLevel:
		if s.src == nil {
			return nil, true
		}
		return s.readFromSource(attrID)
	case AttrMinLevel:
		return s.minLevel(), true
	case AttrMaxLevel:
		return lvldef.LevelMax, true
	case AttrRemainingTime:
		if !s.lighting {
			return nil, false
		}
		return s.remainingTime(), true
	case AttrStartUpCurrentLevel:
		if !s.lighting {
			return nil, false
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.startUp == nil {
			return nil, true
		}
		return *s.startUp, true
	default:
		return s.inst.ReadGlobal(attrID)
	}
}

// readFromSource projects one host-reported value onto the wire types
// the encoder expects: uint8 for the two level attributes and for the
// map8 bitmap. Only reached with a non-nil port.
func (s *Server) readFromSource(attrID uint32) (any, bool) {
	switch attrID {
	case AttrCurrentLevel:
		return nullableLevel(s.src.CurrentLevel())
	case AttrOnLevel:
		return nullableLevel(s.src.OnLevel())
	case AttrOptions:
		// Masked on the way out for the same reason a write is checked:
		// a bit whose feature is not advertised must not be readable as
		// though the cluster honoured it.
		return s.src.Options() & s.optionsMask(), true
	default:
		return nil, false
	}
}

// nullableLevel maps a host reading onto (value, true) or the TLV null
// the quality-X attributes carry when nothing has been observed.
func nullableLevel(level uint8, known bool) (any, bool) {
	if !known {
		return nil, true
	}
	return level, true
}

// MatterWrite applies the writable attributes: Options and OnLevel
// ("RW VO"), and StartUpCurrentLevel ("RW VM") with [Config.Lighting].
// The definition checks the write — served (UNSUPPORTED_ATTRIBUTE
// otherwise), writable (UNSUPPORTED_WRITE), the type and null, the
// OptionsBitmap's defined bits, OnLevel's "minLevel to maxLevel", the
// nullable uint8 range of StartUpCurrentLevel; the Options bits the
// FeatureMap does not make conformant are the server's to refuse.
func (s *Server) MatterWrite(ctx context.Context, attrID uint32, value any) error {
	_, err := s.MatterWriteChanged(ctx, attrID, value)
	return err
}

// MatterWriteChanged implements [contract.ChangeReportingWriter]: the
// write of [Server.MatterWrite], changed when it moved the attribute — the
// comparison that advances the server's own version and notifies.
func (s *Server) MatterWriteChanged(ctx context.Context, attrID uint32, value any) (bool, error) {
	v, err := s.inst.ValidateWrite(attrID, value, s.peer)
	if err != nil {
		return false, err
	}
	// The host's veto ([Server.SetBeforeWrite]) is consulted behind the
	// server's own rules and its comparison with the held value, for a
	// write that changes it only ([spec.WriteVeto.Check]).
	switch attrID {
	case AttrOptions:
		n, _ := v.(uint64)                   // a map8 per ValidateWrite
		return s.writeOptions(ctx, uint8(n)) //nolint:gosec // a map8 per ValidateWrite
	case AttrOnLevel:
		return s.writeOnLevel(ctx, nullableUint8Of(v))
	default: // AttrStartUpCurrentLevel, the last writable attribute
		next := nullableUint8Of(v)
		s.mu.Lock()
		held := equalLevel(s.startUp, next)
		s.mu.Unlock()
		// A write of the value in effect changes nothing and leaves the
		// DataVersion (Datasource.ts:1456-1457, :1476-1480 at db021da0
		// #noteIfChanged).
		if held {
			return false, nil
		}
		if err := s.veto.Check(ctx, attrID, storedUint8(v)); err != nil {
			return false, err
		}
		s.mu.Lock()
		changed := !equalLevel(s.startUp, next)
		s.startUp = next
		s.mu.Unlock()
		if changed {
			s.tracker().Bump()
			s.changes.NotifyFrom(contract.OriginControllerWrite, AttrStartUpCurrentLevel)
		}
		return changed, nil
	}
}

// equalLevel reports whether two nullable levels are the same value.
func equalLevel(a, b *uint8) bool {
	return (a == nil) == (b == nil) && (a == nil || *a == *b)
}

// peer resolves the MinLevel and MaxLevel bounds of OnLevel's
// "minLevel to maxLevel".
func (s *Server) peer(attrID uint32) (any, bool) {
	if attrID == AttrMinLevel {
		return s.minLevel(), true
	}
	return lvldef.LevelMax, true
}

// storedUint8 is a validated value of the three writable attributes —
// each a (nullable) uint8 or map8 — in its stored form, as the write veto
// receives it: a uint8, nil for null.
func storedUint8(v any) any {
	if p := nullableUint8Of(v); p != nil {
		return *p
	}
	return nil
}

// nullableUint8Of is a validated nullable uint8: nil for null.
func nullableUint8Of(v any) *uint8 {
	n, ok := v.(uint64)
	if !ok {
		return nil
	}
	b := uint8(n) //nolint:gosec // a nullable uint8 per ValidateWrite
	return &b
}

// writeOptions forwards an Options write. The bitmap carries constraint
// "desc"; what "desc" resolves to here is [Server.optionsMask] — the bits
// whose conformance the advertised FeatureMap satisfies.
func (s *Server) writeOptions(ctx context.Context, options uint8) (changed bool, err error) {
	if options&^s.optionsMask() != 0 {
		return false, constraintErr{fmt.Sprintf(
			"levelcontrol: Options 0x%02X sets a bit that is not conformant under FeatureMap 0x%02X", options, s.features(),
		)}
	}
	if s.src == nil {
		return false, errNoSource("Options write")
	}
	// The value in effect, written again, reaches no setter and changes
	// nothing; another reaches the setter, advances the DataVersion and is
	// reported to the server's listeners, whatever the host's state reads
	// when the setter returns (Datasource.ts:1456-1457, :1476-1492 at
	// db021da0 #noteIfChanged; LevelControlServer.ts:253-255 reacts to no
	// Options write).
	if s.src.Options() == options {
		return false, nil
	}
	if err := s.veto.Check(ctx, AttrOptions, options); err != nil {
		return false, err
	}
	if err := s.src.SetOptions(ctx, options); err != nil {
		return false, fmt.Errorf("levelcontrol: Options write: %w", err)
	}
	s.tracker().Bump()
	s.changes.NotifyFrom(contract.OriginControllerWrite, AttrOptions)
	return true, nil
}

// writeOnLevel forwards an OnLevel write. A null clears the on-level,
// which the spec reads as "it has no effect" (resource :146-149).
func (s *Server) writeOnLevel(ctx context.Context, level *uint8) (changed bool, err error) {
	if s.src == nil {
		return false, errNoSource("OnLevel write")
	}
	// The on-level in effect, written again, reaches no setter and changes
	// nothing, as for Options above.
	if held, known := s.src.OnLevel(); known == (level != nil) && (!known || held == *level) {
		return false, nil
	}
	var vetoed any // the stored form: a uint8, nil for null
	if level != nil {
		vetoed = *level
	}
	if err := s.veto.Check(ctx, AttrOnLevel, vetoed); err != nil {
		return false, err
	}
	if err := s.src.SetOnLevel(ctx, level); err != nil {
		return false, fmt.Errorf("levelcontrol: OnLevel write: %w", err)
	}
	s.tracker().Bump()
	s.changes.NotifyFrom(contract.OriginControllerWrite, AttrOnLevel)
	return true, nil
}

// MinWritePrivilege implements [contract.ClusterAttributeWritePrivilege]:
// StartUpCurrentLevel is "RW VM", Options and OnLevel "RW VO".
func (s *Server) MinWritePrivilege(attrID uint32) uint8 { return s.inst.MinWritePrivilege(attrID) }

// MatterInvoke dispatches the eight conformance-M commands. Forwarded,
// each one reaches the host port; none reports Success on its own, and a
// host refusal is returned as an error so the dispatcher maps it to a
// failure status rather than to Success. Under the engine the server runs
// the command (transitions.go) and the host receives the levels it steps
// through.
//
// The DataVersion advances only after a command that succeeded and moved
// CurrentLevel ([Server.finish]).
//
// A command the host replaced through [Server.Handle] is the host's
// handler alone ([spec.CommandRegistrar]): the server runs no gate, no
// transition and no scene invalidation for it.
func (s *Server) MatterInvoke(ctx context.Context, cmdID uint32, fields any) (any, error) {
	if resp, handled, err := s.host.Invoke(ctx, cmdID, fields); handled {
		return resp, err
	}
	switch cmdID {
	case CmdMoveToLevel, CmdMoveToLevelWithOnOff:
		return s.invokeMoveToLevel(ctx, cmdID, fields)
	case CmdMove, CmdMoveWithOnOff:
		return s.invokeMove(ctx, cmdID, fields)
	case CmdStep, CmdStepWithOnOff:
		return s.invokeStep(ctx, cmdID, fields)
	case CmdStop, CmdStopWithOnOff:
		return s.invokeStop(ctx, cmdID, fields)
	default:
		return nil, im.UnsupportedCommandf("levelcontrol: unknown command 0x%02X", cmdID)
	}
}

// invokeMoveToLevel handles MoveToLevel (0x0) and MoveToLevelWithOnOff
// (0x4), which share a payload (element :101, type "MoveToLevel").
func (s *Server) invokeMoveToLevel(ctx context.Context, cmdID uint32, fields any) (any, error) {
	name := commandName(cmdID)
	req, err := moveToLevelRequestFrom(name, fields)
	if err != nil {
		return nil, err
	}
	if req.Level > lvldef.LevelMax {
		// constraint "max 254" (element :74).
		return nil, constraintErr{fmt.Sprintf("levelcontrol: %s Level %d violates constraint max %d",
			name, req.Level, lvldef.LevelMax)}
	}
	if s.src == nil {
		return nil, errNoSource(name)
	}
	before := s.levelNow()
	if s.engine != nil {
		return s.finish(name, s.runMoveToLevel(ctx, req, cmdID == CmdMoveToLevelWithOnOff), before)
	}
	call := s.src.MoveToLevel
	if cmdID == CmdMoveToLevelWithOnOff {
		call = s.src.MoveToLevelWithOnOff
	}
	executes := s.handOffExecutes(cmdID == CmdMoveToLevelWithOnOff, req.OptionsMask, req.OptionsOverride)
	return s.forward(name, executes, before, func() error { return call(ctx, req) })
}

// invokeMove handles Move (0x1) and MoveWithOnOff (0x5), which share a
// payload (element :105, type "Move").
func (s *Server) invokeMove(ctx context.Context, cmdID uint32, fields any) (any, error) {
	name := commandName(cmdID)
	req, err := moveRequestFrom(name, fields)
	if err != nil {
		return nil, err
	}
	if err := checkMoveMode(name, req.MoveMode); err != nil {
		return nil, err
	}
	if req.Rate != nil && *req.Rate == 0 {
		// matter.js LevelControlServer.ts:305-308 #assertRateValue:
		// `Illegal move rate of 0`, Status.InvalidCommand.
		return nil, invalidCommandErr{fmt.Sprintf("levelcontrol: %s carries an illegal move rate of 0", name)}
	}
	if s.src == nil {
		return nil, errNoSource(name)
	}
	before := s.levelNow()
	if s.engine != nil {
		return s.finish(name, s.runMove(ctx, req, cmdID == CmdMoveWithOnOff), before)
	}
	call := s.src.Move
	if cmdID == CmdMoveWithOnOff {
		call = s.src.MoveWithOnOff
	}
	executes := s.handOffExecutes(cmdID == CmdMoveWithOnOff, req.OptionsMask, req.OptionsOverride)
	return s.forward(name, executes, before, func() error { return call(ctx, req) })
}

// invokeStep handles Step (0x2) and StepWithOnOff (0x6), which share a
// payload (element :109, type "Step").
func (s *Server) invokeStep(ctx context.Context, cmdID uint32, fields any) (any, error) {
	name := commandName(cmdID)
	req, err := stepRequestFrom(name, fields)
	if err != nil {
		return nil, err
	}
	if err := checkStepMode(name, req.StepMode); err != nil {
		return nil, err
	}
	if s.src == nil {
		return nil, errNoSource(name)
	}
	before := s.levelNow()
	if s.engine != nil {
		return s.finish(name, s.runStep(ctx, req, cmdID == CmdStepWithOnOff), before)
	}
	call := s.src.Step
	if cmdID == CmdStepWithOnOff {
		call = s.src.StepWithOnOff
	}
	executes := s.handOffExecutes(cmdID == CmdStepWithOnOff, req.OptionsMask, req.OptionsOverride)
	return s.forward(name, executes, before, func() error { return call(ctx, req) })
}

// invokeStop handles Stop (0x3) and StopWithOnOff (0x7), which share a
// payload (element :113, type "Stop").
func (s *Server) invokeStop(ctx context.Context, cmdID uint32, fields any) (any, error) {
	name := commandName(cmdID)
	req, err := stopRequestFrom(name, fields)
	if err != nil {
		return nil, err
	}
	if s.src == nil {
		return nil, errNoSource(name)
	}
	before := s.levelNow()
	if s.engine != nil {
		return s.finish(name, s.runStop(req, cmdID == CmdStopWithOnOff), before)
	}
	call := s.src.Stop
	if cmdID == CmdStopWithOnOff {
		call = s.src.StopWithOnOff
	}
	executes := s.handOffExecutes(cmdID == CmdStopWithOnOff, req.OptionsMask, req.OptionsOverride)
	return s.forward(name, executes, before, func() error { return call(ctx, req) })
}

// forward runs a command on the hand-off path — call hands it to the
// port — and reports the level the port moved, from the command
// ([Server.observeLevel]); a change the host's notifier reports while
// call runs is the command's too. A command that executes
// ([Server.handOffExecutes]) and that the port accepted ends the current
// scene ([Server.MatterInvalidatesScenes]).
func (s *Server) forward(name string, executes bool, before levelReading, call func() error) (any, error) {
	s.forwarding.Add(1)
	err := call()
	s.forwarding.Add(-1)
	if err == nil {
		s.observeLevel(contract.OriginCommand)
		if executes {
			s.invalidateScenes()
		}
	}
	return s.finish(name, err, before)
}

// finish turns a port result into the (response, error) pair the
// dispatcher expects. On the hand-off path the DataVersion advances when
// the command changed CurrentLevel from before, once
// ([Server.bumpIfMoved]) — the one attribute of this cluster's state a
// command changes (RemainingTime is computed, as matter.js's virtual
// remainingTime is, LevelControlServer.ts:806-823 at db021da0) — and a
// refusal keeps it where it was. Under the engine every
// change of CurrentLevel advanced it already ([Server.observeLevel]), and
// every report of RemainingTime ([Server.initEngine]).
// Mirrors matter.js
// packages/node/src/behavior/state/managed/Datasource.ts:1456-1457 at
// db021da0 (a commit advances the version only `if (changes)`).
func (s *Server) finish(name string, err error, before levelReading) (any, error) {
	if err != nil {
		return nil, fmt.Errorf("levelcontrol: %s: %w", name, err)
	}
	if s.engine == nil {
		s.bumpIfMoved(before)
	}
	return nil, nil
}

// levelReading is CurrentLevel as the source reports it — the level of
// an unknown one is 0 — and the count of host notifications that had
// advanced the DataVersion before it was read ([Server.noted]).
type levelReading struct {
	level    uint8
	known    bool
	notified uint64
}

// sourceLevel reads CurrentLevel from the source, which is not nil; the
// level of an unknown one is 0.
func (s *Server) sourceLevel() (uint8, bool) {
	level, known := s.src.CurrentLevel()
	if !known {
		level = 0
	}
	return level, known
}

// levelNow reads CurrentLevel from the source after the count of host
// notifications, so a notification between the two is one the reading
// may already show and [Server.bumpIfMoved] compares with its value.
func (s *Server) levelNow() levelReading {
	s.noteMu.Lock()
	notified := s.noted.notified
	s.noteMu.Unlock()
	level, known := s.sourceLevel()
	return levelReading{level, known, notified}
}

// bumpIfMoved advances the DataVersion once for the command when
// CurrentLevel now differs from the level last served under a version of
// its own: the level the last host notification since before read — that
// notification advanced the version for it —, or before's when none
// fired. A notification that fired before the port moved the level, for
// a change of something else, leaves the move to this advance. Mirrors
// matter.js packages/node/src/behavior/state/managed/Datasource.ts:1456-1457
// at db021da0 (a commit advances the version once `if (changes)`).
func (s *Server) bumpIfMoved(before levelReading) {
	level, known := s.sourceLevel()
	s.noteMu.Lock()
	defer s.noteMu.Unlock()
	last := before
	if s.noted.notified != before.notified {
		last = s.noted
	}
	if level != last.level || known != last.known {
		s.tracker().Bump()
	}
}

// commandName renders a command id for error messages. Only the eight
// ids this server dispatches reach it.
func commandName(cmdID uint32) string {
	switch cmdID {
	case CmdMoveToLevel:
		return "MoveToLevel"
	case CmdMove:
		return "Move"
	case CmdStep:
		return "Step"
	case CmdStop:
		return "Stop"
	case CmdMoveToLevelWithOnOff:
		return "MoveToLevelWithOnOff"
	case CmdMoveWithOnOff:
		return "MoveWithOnOff"
	case CmdStepWithOnOff:
		return "StepWithOnOff"
	case CmdStopWithOnOff:
		return "StopWithOnOff"
	default:
		return fmt.Sprintf("command 0x%02X", cmdID)
	}
}

// checkMoveMode refuses a MoveMode outside MoveModeEnum. The field
// carries constraint "desc" (element :82) and the enum defines exactly
// Up (0x0) and Down (0x1) (element :131-135), so a third value names no
// direction — forwarding it would let a host pick one.
func checkMoveMode(name string, mode uint8) error {
	if mode != MoveModeUp && mode != MoveModeDown {
		return constraintErr{fmt.Sprintf("levelcontrol: %s MoveMode %d is not in MoveModeEnum", name, mode)}
	}
	return nil
}

// checkStepMode mirrors [checkMoveMode] for StepModeEnum
// (element :90, :136-140).
func checkStepMode(name string, mode uint8) error {
	if mode != StepModeUp && mode != StepModeDown {
		return constraintErr{fmt.Sprintf("levelcontrol: %s StepMode %d is not in StepModeEnum", name, mode)}
	}
	return nil
}

// moveToLevelRequestFrom normalises the payload the bridge hands over.
//
// The bridge decodes MoveToLevel and MoveToLevelWithOnOff into the
// cluster/wire request (bridge/fields_reader.go commandFieldsReader, case
// 0x0008) and hands every other LevelControl command over as the lenient
// tag map; the generated request structs (cluster/spec/levelcontrol) an
// in-process caller builds are carried over to the cluster/wire struct
// the engine drives, and the raw payload goes through
// [wire.DecodeMoveToLevel].
func moveToLevelRequestFrom(name string, fields any) (wire.MoveToLevelRequest, error) {
	switch v := generatedValue(fields).(type) {
	case nil:
		return wire.MoveToLevelRequest{}, missingFields(name)
	case wire.MoveToLevelRequest:
		return v, nil
	case lvldef.MoveToLevelRequest:
		return wire.MoveToLevelRequest{
			Level: v.Level, TransitionTime: nullablePtr(v.TransitionTime),
			OptionsMask: uint8(v.OptionsMask), OptionsOverride: uint8(v.OptionsOverride),
		}, nil
	case lvldef.MoveToLevelWithOnOffRequest:
		return wire.MoveToLevelRequest{
			Level: v.Level, TransitionTime: nullablePtr(v.TransitionTime),
			OptionsMask: uint8(v.OptionsMask), OptionsOverride: uint8(v.OptionsOverride),
		}, nil
	case *wire.MoveToLevelRequest:
		if v == nil {
			return wire.MoveToLevelRequest{}, missingFields(name)
		}
		return *v, nil
	case []byte:
		req, err := wire.DecodeMoveToLevel(v)
		if err != nil {
			// A field of the wrong type or out of range carries its
			// status (INVALID_ACTION / CONSTRAINT_ERROR, W3-clucore-08).
			if spec.CarriesStatus(err) {
				return req, err
			}
			return req, invalidCommandErr{fmt.Sprintf("levelcontrol: %s: %v", name, err)}
		}
		return req, nil
	case map[uint8]any:
		return moveToLevelFromTagMap(name, v)
	default:
		return wire.MoveToLevelRequest{}, unexpectedFields(name, "MoveToLevelRequest", fields)
	}
}

// generatedValue is a generated request given by pointer as its value,
// so the converters take both forms; a nil pointer is no payload. Any
// other payload is returned as it is.
func generatedValue(fields any) any {
	switch v := fields.(type) {
	case *lvldef.MoveToLevelRequest:
		return derefOrNil(v)
	case *lvldef.MoveToLevelWithOnOffRequest:
		return derefOrNil(v)
	case *lvldef.MoveRequest:
		return derefOrNil(v)
	case *lvldef.MoveWithOnOffRequest:
		return derefOrNil(v)
	case *lvldef.StepRequest:
		return derefOrNil(v)
	case *lvldef.StepWithOnOffRequest:
		return derefOrNil(v)
	case *lvldef.StopRequest:
		return derefOrNil(v)
	case *lvldef.StopWithOnOffRequest:
		return derefOrNil(v)
	default:
		return fields
	}
}

// derefOrNil is *p, or nil for a nil p.
func derefOrNil[T any](p *T) any {
	if p == nil {
		return nil
	}
	return *p
}

// moveToLevelFromTagMap reads the four MoveToLevel fields out of the
// generic tag map (element :73-77).
func moveToLevelFromTagMap(name string, m map[uint8]any) (wire.MoveToLevelRequest, error) {
	var req wire.MoveToLevelRequest
	level, err := requiredUint8(name, m, 0, "Level")
	if err != nil {
		return req, err
	}
	req.Level = level
	if req.TransitionTime, err = nullableUint16(name, m, 1, "TransitionTime"); err != nil {
		return req, err
	}
	if req.OptionsMask, err = optionalBitmap(name, m, 2, "OptionsMask"); err != nil {
		return req, err
	}
	req.OptionsOverride, err = optionalBitmap(name, m, 3, "OptionsOverride")
	return req, err
}

// moveRequestFrom mirrors [moveToLevelRequestFrom] for Move /
// MoveWithOnOff, reusing [wire.DecodeMove] for the raw-payload shape.
func moveRequestFrom(name string, fields any) (wire.MoveRequest, error) {
	switch v := generatedValue(fields).(type) {
	case nil:
		return wire.MoveRequest{}, missingFields(name)
	case wire.MoveRequest:
		return v, nil
	case lvldef.MoveRequest:
		return wire.MoveRequest{MoveMode: uint8(v.MoveMode), Rate: nullablePtr(v.Rate), OptionsMask: uint8(v.OptionsMask), OptionsOverride: uint8(v.OptionsOverride)}, nil
	case lvldef.MoveWithOnOffRequest:
		return wire.MoveRequest{MoveMode: uint8(v.MoveMode), Rate: nullablePtr(v.Rate), OptionsMask: uint8(v.OptionsMask), OptionsOverride: uint8(v.OptionsOverride)}, nil
	case *wire.MoveRequest:
		if v == nil {
			return wire.MoveRequest{}, missingFields(name)
		}
		return *v, nil
	case []byte:
		req, err := wire.DecodeMove(v)
		if err != nil {
			// A field of the wrong type or out of range carries its
			// status (INVALID_ACTION / CONSTRAINT_ERROR, W3-clucore-08).
			if spec.CarriesStatus(err) {
				return req, err
			}
			return req, invalidCommandErr{fmt.Sprintf("levelcontrol: %s: %v", name, err)}
		}
		return req, nil
	case map[uint8]any:
		return moveFromTagMap(name, v)
	default:
		return wire.MoveRequest{}, unexpectedFields(name, "MoveRequest", fields)
	}
}

// moveFromTagMap reads the four Move fields out of the generic tag map
// (element :81-85).
func moveFromTagMap(name string, m map[uint8]any) (wire.MoveRequest, error) {
	var req wire.MoveRequest
	mode, err := requiredUint8(name, m, 0, "MoveMode")
	if err != nil {
		return req, err
	}
	req.MoveMode = mode
	if req.Rate, err = nullableUint8(name, m, 1, "Rate"); err != nil {
		return req, err
	}
	if req.OptionsMask, err = optionalBitmap(name, m, 2, "OptionsMask"); err != nil {
		return req, err
	}
	req.OptionsOverride, err = optionalBitmap(name, m, 3, "OptionsOverride")
	return req, err
}

// stepRequestFrom mirrors [moveToLevelRequestFrom] for Step /
// StepWithOnOff, reusing [wire.DecodeStep] for the raw-payload shape.
func stepRequestFrom(name string, fields any) (wire.StepRequest, error) {
	switch v := generatedValue(fields).(type) {
	case nil:
		return wire.StepRequest{}, missingFields(name)
	case wire.StepRequest:
		return v, nil
	case lvldef.StepRequest:
		return wire.StepRequest{
			StepMode: uint8(v.StepMode), StepSize: v.StepSize, TransitionTime: nullablePtr(v.TransitionTime),
			OptionsMask: uint8(v.OptionsMask), OptionsOverride: uint8(v.OptionsOverride),
		}, nil
	case lvldef.StepWithOnOffRequest:
		return wire.StepRequest{
			StepMode: uint8(v.StepMode), StepSize: v.StepSize, TransitionTime: nullablePtr(v.TransitionTime),
			OptionsMask: uint8(v.OptionsMask), OptionsOverride: uint8(v.OptionsOverride),
		}, nil
	case *wire.StepRequest:
		if v == nil {
			return wire.StepRequest{}, missingFields(name)
		}
		return *v, nil
	case []byte:
		req, err := wire.DecodeStep(v)
		if err != nil {
			// A field of the wrong type or out of range carries its
			// status, as MoveToLevel's and Move's (R2-P6-wire-1416).
			if spec.CarriesStatus(err) {
				return req, err
			}
			return req, invalidCommandErr{fmt.Sprintf("levelcontrol: %s: %v", name, err)}
		}
		return req, nil
	case map[uint8]any:
		return stepFromTagMap(name, v)
	default:
		return wire.StepRequest{}, unexpectedFields(name, "StepRequest", fields)
	}
}

// stepFromTagMap reads the five Step fields out of the generic tag map
// (element :89-94).
func stepFromTagMap(name string, m map[uint8]any) (wire.StepRequest, error) {
	var req wire.StepRequest
	mode, err := requiredUint8(name, m, 0, "StepMode")
	if err != nil {
		return req, err
	}
	req.StepMode = mode
	if req.StepSize, err = requiredUint8(name, m, 1, "StepSize"); err != nil {
		return req, err
	}
	if req.TransitionTime, err = nullableUint16(name, m, 2, "TransitionTime"); err != nil {
		return req, err
	}
	if req.OptionsMask, err = optionalBitmap(name, m, 3, "OptionsMask"); err != nil {
		return req, err
	}
	req.OptionsOverride, err = optionalBitmap(name, m, 4, "OptionsOverride")
	return req, err
}

// stopRequestFrom mirrors [moveToLevelRequestFrom] for Stop /
// StopWithOnOff, reusing [wire.DecodeStop] for the raw-payload shape.
func stopRequestFrom(name string, fields any) (wire.StopRequest, error) {
	switch v := generatedValue(fields).(type) {
	case nil:
		return wire.StopRequest{}, missingFields(name)
	case wire.StopRequest:
		return v, nil
	case lvldef.StopRequest:
		return wire.StopRequest{OptionsMask: uint8(v.OptionsMask), OptionsOverride: uint8(v.OptionsOverride)}, nil
	case lvldef.StopWithOnOffRequest:
		return wire.StopRequest{OptionsMask: uint8(v.OptionsMask), OptionsOverride: uint8(v.OptionsOverride)}, nil
	case *wire.StopRequest:
		if v == nil {
			return wire.StopRequest{}, missingFields(name)
		}
		return *v, nil
	case []byte:
		req, err := wire.DecodeStop(v)
		if err != nil {
			// A field of the wrong type or out of range carries its
			// status, as MoveToLevel's and Move's (R2-P6-wire-1416).
			if spec.CarriesStatus(err) {
				return req, err
			}
			return req, invalidCommandErr{fmt.Sprintf("levelcontrol: %s: %v", name, err)}
		}
		return req, nil
	case map[uint8]any:
		return stopFromTagMap(name, v)
	default:
		return wire.StopRequest{}, unexpectedFields(name, "StopRequest", fields)
	}
}

// stopFromTagMap reads the two Stop fields out of the generic tag map
// (element :98-99).
func stopFromTagMap(name string, m map[uint8]any) (wire.StopRequest, error) {
	var req wire.StopRequest
	var err error
	if req.OptionsMask, err = optionalBitmap(name, m, 0, "OptionsMask"); err != nil {
		return req, err
	}
	req.OptionsOverride, err = optionalBitmap(name, m, 1, "OptionsOverride")
	return req, err
}

// requiredUint8 reads a mandatory, non-nullable uint8 field. An absent
// or null tag is a malformed command rather than a zero value, because
// zero is a legal Level, MoveMode and StepMode.
func requiredUint8(name string, m map[uint8]any, tag uint8, field string) (uint8, error) {
	raw, present := m[tag]
	if !present || raw == nil {
		return 0, invalidCommandErr{fmt.Sprintf("levelcontrol: %s is missing the mandatory %s field", name, field)}
	}
	v, ok := asUint8(raw)
	if !ok {
		return 0, constraintErr{fmt.Sprintf("levelcontrol: %s %s is %T, want a uint8", name, field, raw)}
	}
	return v, nil
}

// nullableUint8 reads a mandatory but quality-X uint8 field. An absent
// tag is read as null, mirroring bridge/fields_reader.go
// decodeMoveToLevelRequest, which leaves an absent nullable field nil.
func nullableUint8(name string, m map[uint8]any, tag uint8, field string) (*uint8, error) {
	raw, present := m[tag]
	if !present || raw == nil {
		return nil, nil
	}
	v, ok := asUint8(raw)
	if !ok {
		return nil, constraintErr{fmt.Sprintf("levelcontrol: %s %s is %T, want a uint8", name, field, raw)}
	}
	return &v, nil
}

// nullableUint16 mirrors [nullableUint8] for the uint16 TransitionTime
// fields.
func nullableUint16(name string, m map[uint8]any, tag uint8, field string) (*uint16, error) {
	raw, present := m[tag]
	if !present || raw == nil {
		return nil, nil
	}
	v, ok := asUint16(raw)
	if !ok {
		return nil, constraintErr{fmt.Sprintf("levelcontrol: %s %s is %T, want a uint16", name, field, raw)}
	}
	return &v, nil
}

// optionalBitmap reads an OptionsMask / OptionsOverride field. Both are
// conformance M, but an absent bitmap means "override nothing", which is
// the same as the zero value — so absence is accepted rather than
// refused.
func optionalBitmap(name string, m map[uint8]any, tag uint8, field string) (uint8, error) {
	raw, present := m[tag]
	if !present || raw == nil {
		return 0, nil
	}
	v, ok := asUint8(raw)
	if !ok {
		return 0, constraintErr{fmt.Sprintf("levelcontrol: %s %s is %T, want a map8", name, field, raw)}
	}
	return v, nil
}

// asUint8 narrows a decoded wire value to the uint8 a level, enum or
// map8 field carries. The bridge surfaces unsigned TLV integers as
// uint64 and signed ones as int64 (bridge/attribute_value_reader.go
// primitiveAttributeValue), so a strict assertion on uint8 would reject
// every value that actually arrives. Out-of-range and negative values
// are rejected rather than wrapped, which would turn level 300 into
// level 44.
func asUint8(v any) (uint8, bool) {
	n, ok := asInt64(v)
	if !ok || n < 0 || n > 0xFF {
		return 0, false
	}
	return uint8(n), true //nolint:gosec // range-checked against 0 and uint8 max above
}

// asUint16 mirrors [asUint8] for the uint16 TransitionTime fields.
func asUint16(v any) (uint16, bool) {
	n, ok := asInt64(v)
	if !ok || n < 0 || n > 0xFFFF {
		return 0, false
	}
	return uint16(n), true //nolint:gosec // range-checked against 0 and uint16 max above
}

// asInt64 funnels every numeric shape the decoders produce to int64 so
// the two narrowing helpers share one range check.
func asInt64(v any) (int64, bool) {
	const maxInt64 = uint64(1)<<63 - 1
	switch x := v.(type) {
	case uint8:
		return int64(x), true
	case uint16:
		return int64(x), true
	case uint32:
		return int64(x), true
	case uint64:
		if x > maxInt64 {
			return 0, false
		}
		return int64(x), true //nolint:gosec // range-checked against int64 max on the line above
	case int:
		return int64(x), true
	case int8:
		return int64(x), true
	case int16:
		return int64(x), true
	case int32:
		return int64(x), true
	case int64:
		return x, true
	default:
		return 0, false
	}
}

// nullablePtr carries a decoded nullable field over to the pointer form
// the cluster/wire structs use: nil for null.
func nullablePtr[T any](n spec.Nullable[T]) *T {
	if n.Null {
		return nil
	}
	v := n.Value
	return &v
}

// missingFields reports an invocation that carried no payload at all.
// Every one of the eight commands has at least one conformance-M field,
// so nil is malformed rather than "take the defaults".
func missingFields(name string) error {
	return invalidCommandErr{fmt.Sprintf("levelcontrol: %s carried no fields", name)}
}

// unexpectedFields reports a payload shape this server cannot read.
func unexpectedFields(name, want string, got any) error {
	return invalidCommandErr{fmt.Sprintf(
		"levelcontrol: %s expected %s, []byte or map[uint8]any, got %T", name, want, got,
	)}
}

// errNoSource reports that the server has no host port to reach.
func errNoSource(what string) error {
	return fmt.Errorf("levelcontrol: %s has no host port", what)
}

// MatterReportable lists the attributes that move while the device runs
// and that the host's change notification reports. Options and OnLevel
// are absent: both change only through a write, which already bumps the
// DataVersion the dispatcher reports. Under the engine the list is
// empty: the server reports CurrentLevel and RemainingTime itself
// ([Server.MatterSelfReportedAttributes]).
func (s *Server) MatterReportable() []uint32 {
	if s.engine != nil {
		return []uint32{}
	}
	return []uint32{AttrCurrentLevel}
}

// MatterAttributes implements [contract.ClusterAttributeLister], in id
// order and without the universal globals — the dispatcher merges those.
func (s *Server) MatterAttributes() []uint32 { return s.inst.MatterAttributes() }

// MatterAcceptedCommands implements [contract.ClusterCommandLister], in
// command-id order.
func (s *Server) MatterAcceptedCommands() []uint32 { return s.inst.MatterAcceptedCommands() }

// MatterGeneratedCommands implements [contract.ClusterCommandLister].
// All eight commands declare response "status", so the server emits no
// command payloads.
func (*Server) MatterGeneratedCommands() []uint32 { return nil }

// constraintErr maps onto the Matter ConstraintError status.
type constraintErr struct{ msg string }

func (e constraintErr) Error() string                 { return e.msg }
func (constraintErr) MatterStatusCode() im.StatusCode { return im.StatusConstraintError }

// invalidCommandErr maps onto InvalidCommand for a malformed or
// unacceptable command payload.
type invalidCommandErr struct{ msg string }

func (e invalidCommandErr) Error() string                 { return e.msg }
func (invalidCommandErr) MatterStatusCode() im.StatusCode { return im.StatusInvalidCommand }

// Compile-time assertions for the typed status carriers.
var (
	_ im.StatusCodeError = constraintErr{}
	_ im.StatusCodeError = invalidCommandErr{}
)

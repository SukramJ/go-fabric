// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package discovery

// The controller's discovery of commissionable devices as matter.js shapes
// it: a cancelable search over every transport a request allows, in the
// kinds of packages/node/src/behavior/system/controller/discovery/ at
// 862ad75f — Discovery.ts (the engine, :37-483), InstanceDiscovery.ts
// (:18-46), ContinuousDiscovery.ts (:18-44), ParallelPaseDiscovery.ts
// (:45-216, the race) with its two subclasses PaseDiscovery.ts (:28-71)
// and CommissioningDiscovery.ts (:23-91), and DiscoveryError.ts (:12-31).
// matter.js's ActiveDiscoveries.ts (the node's registry of running
// discoveries, closed with the node) and index.ts have no counterpart: a
// Session ends with the context it was started under.

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	fabricclock "github.com/SukramJ/go-fabric/clock"
)

// TransportKind names a discovery transport — matter.js ChannelType of a
// Scanner (UDP for DNS-SD, BLE).
type TransportKind string

// The transports.
const (
	TransportIP  TransportKind = "ip"
	TransportBLE TransportKind = "ble"
)

// Transport is one way a discovery searches — a matter.js Scanner of the
// node's ScannerSet, its findCommissionableDevicesContinuously
// (Discovery.ts:276-286).
type Transport interface {
	// Kind is the transport's kind.
	Kind() TransportKind
	// FindContinuously reports every commissionable device id selects to
	// found until ctx ends, and then returns nil; an error is the
	// transport's failure. found may run on any goroutine.
	FindContinuously(ctx context.Context, id Identifier, found func(Device)) error
}

// IPTransport is the DNS-SD scanner s as a [Transport].
func IPTransport(s *Scanner) Transport { return ipTransport{s} }

type ipTransport struct{ s *Scanner }

func (ipTransport) Kind() TransportKind { return TransportIP }

func (t ipTransport) FindContinuously(ctx context.Context, id Identifier, found func(Device)) error {
	_, err := t.s.FindCommissionable(ctx, id, func(d Device) {
		if len(d.Addresses) > 0 {
			found(d)
		}
	})
	return err
}

// Capabilities are the transports an onboarding payload names — its
// discovery capabilities bitmap (Matter 1.6.1 Core §5.1.3.1 Table 60).
type Capabilities struct {
	SoftAP, BLE, OnIPNetwork bool
}

// CapabilitiesOf decodes a QR payload's discovery capabilities bitmap: bit
// 0 SoftAP, bit 1 BLE, bit 2 on the IP network.
func CapabilitiesOf(bitmap uint8) Capabilities {
	return Capabilities{SoftAP: bitmap&1 != 0, BLE: bitmap&2 != 0, OnIPNetwork: bitmap&4 != 0}
}

// SelectTransports is the transports of available a discovery with caps
// runs on — ScannerSet.select (Discovery.ts:272-273): every one for nil
// caps, else the IP network regardless of its bit and BLE only where caps
// name it (Discovery.Options.discoveryCapabilities, :496-505).
func SelectTransports(available []Transport, caps *Capabilities) []Transport {
	if caps == nil {
		return available
	}
	var out []Transport
	for _, t := range available {
		if t.Kind() != TransportBLE || caps.BLE {
			out = append(out, t)
		}
	}
	return out
}

// Candidate is one device a transport found.
type Candidate struct {
	// Transport is the kind of the transport that found the device.
	Transport TransportKind
	// Device is the device as the transport reports it; over BLE its
	// InstanceID is the peripheral's address.
	Device Device
}

// SessionOptions configure a discovery — matter.js Discovery.Options and
// InstanceOptions (Discovery.ts:485-513).
type SessionOptions struct {
	// Identifier selects the devices; the zero value is any commissionable
	// device (Discovery.#identifier, :160-195).
	Identifier Identifier
	// Timeout ends the search after it as Stop does (:290-292); 0 searches
	// until stopped, cancelled or the context ends.
	Timeout time.Duration
	// Capabilities are the transports the device is expected on; nil is
	// every transport given.
	Capabilities *Capabilities
	// Prepare, when set, readies a found device before the discovery takes
	// it — matter.js's ClientNodeFactory step (#found, :333-357), here the
	// host's (a registry node, say). Its error loses this device only: it
	// is logged once per device and, as the first such error, replaces a
	// "not discovered" outcome (:404-430). The discovery ends only after
	// every preparation finished (:303-331).
	Prepare func(ctx context.Context, c Candidate) error
	// Clock times Timeout and the race's stagger; nil is the system clock.
	Clock fabricclock.Clock
	// Logger receives the diagnostics; nil discards.
	Logger *slog.Logger
}

// ErrDiscovery is the base of the errors that report how a discovery ended
// without a result — matter.js DiscoveryError (DiscoveryError.ts:12).
// [*AggregateError] is not of it, as matter.js's DiscoveryAggregateError
// is no subtype (:31).
var ErrDiscovery = errors.New("discovery: no result")

// The outcomes of a discovery without a result (DiscoveryError.ts:14-29).
var (
	// ErrDeviceNotDiscovered: no device matched the request.
	ErrDeviceNotDiscovered = fmt.Errorf("%w: device not discovered", ErrDiscovery)
	// ErrDiscoveryIncomplete: a device was found but no attempt won and
	// none failed with an error of its own (the timeout cancelled them).
	ErrDiscoveryIncomplete = fmt.Errorf("%w: incomplete", ErrDiscovery)
	// ErrNoDiscoveryTransport: no transport the request allows is there.
	ErrNoDiscoveryTransport = fmt.Errorf("%w: no transport", ErrDiscovery)
)

// AggregateError is the failure of several transports or attempts, each
// with an error of its own — matter.js DiscoveryAggregateError
// (DiscoveryError.ts:31).
type AggregateError struct {
	Message string
	Errs    []error
}

func (e *AggregateError) Error() string {
	msgs := make([]string, len(e.Errs))
	for i, err := range e.Errs {
		msgs[i] = err.Error()
	}
	return e.Message + ": " + strings.Join(msgs, "; ")
}

// Unwrap returns the errors.
func (e *AggregateError) Unwrap() []error { return e.Errs }

// kind is what a discovery does with what it finds — Discovery's abstract
// onDiscovered and onComplete and its onInterrupt hook (:94-98, :201-203).
type kind[T any] interface {
	onDiscovered(s *Session[T], c Candidate)
	onComplete(s *Session[T]) (T, error)
	onInterrupt(s *Session[T], reason error) bool
	onStop(s *Session[T])
}

// Session is one running discovery — matter.js Discovery, a cancelable
// promise (Discovery.ts:37-483). [Session.Stop] ends the search with the
// result it has, [Session.Cancel] or the end of the context it was started
// under interrupt it with a reason, as does the failure of every
// transport; the failure of only some is logged. An interruption after the
// discovery has its result (a race's winner, an instance's node) is
// ignored. [Session.Wait] returns the outcome once the discovery wound
// down — after every preparation and attempt finished.
type Session[T any] struct {
	kind   kind[T]
	opts   SessionOptions
	log    *slog.Logger
	clock  fabricclock.Clock
	ctx    context.Context // values of the caller's context, never its end
	caller context.Context
	scan   context.Context
	stopFn context.CancelFunc

	mu             sync.Mutex
	stopped        bool
	abortReason    error
	localError     error
	unusable       map[string]bool
	scannerCount   int
	scannerErrors  []error
	preparing      sync.WaitGroup
	done           chan struct{}
	result         T
	err            error
	description    string
	callerStopHook func() bool
}

func start[T any](ctx context.Context, k kind[T], transports []Transport, opts SessionOptions, name string) *Session[T] {
	s := &Session[T]{kind: k, opts: opts, log: opts.Logger, clock: opts.Clock, done: make(chan struct{}), unusable: map[string]bool{}, description: name}
	if s.log == nil {
		s.log = slog.New(slog.DiscardHandler)
	}
	if s.clock == nil {
		s.clock = fabricclock.System{}
	}
	s.ctx = context.WithoutCancel(ctx)
	s.scan, s.stopFn = context.WithCancel(s.ctx)
	// The caller's context ends the discovery as a cancel with its cause
	// does (the abort option, :202-205, :496-499), at once when it already
	// ended.
	s.caller = ctx
	s.callerStopHook = context.AfterFunc(ctx, func() { s.Cancel(context.Cause(ctx)) })
	s.checkCaller()
	go s.run(transports)
	return s
}

// checkCaller interrupts the discovery at once when the caller's context
// has ended — the race's decisions read it so an abort is never missed for
// the delivery of the context's own notification.
func (s *Session[T]) checkCaller() {
	if s.caller.Err() != nil {
		s.Cancel(context.Cause(s.caller))
	}
}

// String names the discovery as matter.js's toString does (:117-158).
func (s *Session[T]) String() string { return s.description }

// Done is closed once the discovery has its outcome.
func (s *Session[T]) Done() <-chan struct{} { return s.done }

// Wait waits for the outcome, or for ctx to end (ctx's error then; the
// discovery runs on).
func (s *Session[T]) Wait(ctx context.Context) (T, error) {
	select {
	case <-s.done:
		return s.result, s.err
	case <-ctx.Done():
		var zero T
		return zero, ctx.Err()
	}
}

// Stop ends the search: the outcome is what it would be at the timeout
// (Discovery.stop, :111-115).
func (s *Session[T]) Stop() {
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return
	}
	s.stopped = true
	s.mu.Unlock()
	s.kind.onStop(s)
	s.stopFn()
}

// Cancel interrupts the discovery with reason (nil is context.Canceled):
// the search stops and the outcome is reason, unless the discovery
// already has its result. The first reason stays (:205-217).
func (s *Session[T]) Cancel(reason error) {
	if reason == nil {
		reason = context.Canceled
	}
	s.interrupt(reason)
}

func (s *Session[T]) interrupt(reason error) {
	if !s.kind.onInterrupt(s, reason) {
		s.log.Info("matter.controller.discovery.interruption_ignored", slog.String("discovery", s.description), slog.Any("reason", reason))
		return
	}
	s.mu.Lock()
	if s.abortReason == nil {
		s.abortReason = reason
	}
	s.mu.Unlock()
	s.Stop()
}

func (s *Session[T]) isStopped() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stopped
}

// run is #performDiscovery and #afterDiscovery (:263-302, :455-482).
func (s *Session[T]) run(transports []Transport) {
	defer s.callerStopHook()
	selected := SelectTransports(transports, s.opts.Capabilities)
	s.reportTransports(transports, selected)
	s.mu.Lock()
	s.scannerCount = len(selected)
	s.mu.Unlock()
	if s.opts.Timeout > 0 {
		t := s.clock.AfterFunc(s.opts.Timeout, s.Stop)
		defer t.Stop()
	}
	var scanners sync.WaitGroup
	if !s.isStopped() {
		for _, t := range selected {
			scanners.Go(func() {
				err := t.FindContinuously(s.scan, s.opts.Identifier, func(d Device) { s.prepare(Candidate{Transport: t.Kind(), Device: d}) })
				if err != nil && (s.scan.Err() == nil || !errors.Is(err, context.Canceled)) {
					s.scannerFailed(t, err)
				}
			})
		}
	}
	scanners.Wait()
	s.preparing.Wait()
	result, err := s.kind.onComplete(s)
	if err != nil {
		err = s.failure(err)
	}
	s.mu.Lock()
	if s.abortReason != nil {
		if err == nil || !errors.Is(err, s.abortReason) {
			s.log.Debug("matter.controller.discovery.interrupted", slog.String("discovery", s.description), slog.Any("instead_of", err))
		}
		var zero T
		result, err = zero, s.abortReason
	}
	s.stopped = true
	s.result, s.err = result, err
	s.mu.Unlock()
	s.stopFn()
	close(s.done)
}

// prepare readies a found device, then hands it to the kind (#prepare,
// #tryFound, #found, :303-357).
func (s *Session[T]) prepare(c Candidate) {
	s.preparing.Go(func() {
		if s.opts.Prepare != nil {
			if err := s.opts.Prepare(s.ctx, c); err != nil {
				s.localFailure(c, err)
				return
			}
		}
		s.kind.onDiscovered(s, c)
	})
}

// scannerFailed is #scannerFailed (:364-375): one failed transport is a
// warning; every one failed interrupts the discovery.
func (s *Session[T]) scannerFailed(t Transport, err error) {
	s.log.Warn("matter.controller.discovery.scanner_failed", slog.String("discovery", s.description), slog.String("transport", string(t.Kind())), slog.Any("err", err))
	s.mu.Lock()
	s.scannerErrors = append(s.scannerErrors, err)
	all := len(s.scannerErrors) == s.scannerCount
	s.mu.Unlock()
	if all {
		s.interrupt(s.scannerFailure())
	}
}

func (s *Session[T]) scannerFailure() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return &AggregateError{
		Message: fmt.Sprintf("%s failed: %d of %d scanner(s) failed", s.description, len(s.scannerErrors), s.scannerCount),
		Errs:    append([]error(nil), s.scannerErrors...),
	}
}

// reportTransports is #reportTransports (:378-402).
func (s *Session[T]) reportTransports(available, selected []Transport) {
	bleInstalled, bleUsed := false, false
	for _, t := range available {
		bleInstalled = bleInstalled || t.Kind() == TransportBLE
	}
	for _, t := range selected {
		bleUsed = bleUsed || t.Kind() == TransportBLE
	}
	attrs := []any{slog.String("discovery", s.description)}
	if bleInstalled && !bleUsed {
		attrs = append(attrs, slog.String("note", "BLE not requested"))
	}
	s.log.Info("matter.controller.discovery.initiating", attrs...)
	if len(selected) == 0 {
		s.log.Warn("matter.controller.discovery.no_scanner", slog.String("discovery", s.description))
		return
	}
	if c := s.opts.Capabilities; c != nil && c.BLE && !bleInstalled {
		s.log.Info("matter.controller.discovery.ble_not_enabled", slog.String("discovery", s.description))
	}
}

// localFailure is #localFailure (:404-418): a found device that cannot be
// used, a warning once per device.
func (s *Session[T]) localFailure(c Candidate, err error) {
	s.mu.Lock()
	first := !s.unusable[c.Device.InstanceID]
	s.unusable[c.Device.InstanceID] = true
	if s.localError == nil {
		s.localError = err
	}
	s.mu.Unlock()
	if first {
		s.log.Warn("matter.controller.discovery.device_unusable", slog.String("discovery", s.description), slog.String("device", c.Device.InstanceID), slog.Any("err", err))
	} else {
		s.log.Debug("matter.controller.discovery.device_still_unusable", slog.String("discovery", s.description), slog.String("device", c.Device.InstanceID), slog.Any("err", err))
	}
}

// failure prefers the real cause over "not discovered" (#failure,
// :420-453): an unusable device, a failed transport, no transport at all.
func (s *Session[T]) failure(err error) error {
	if !errors.Is(err, ErrDeviceNotDiscovered) {
		return err
	}
	s.mu.Lock()
	local, failed, count := s.localError, len(s.scannerErrors), s.scannerCount
	s.mu.Unlock()
	switch {
	case local != nil:
		return local
	case failed > 0:
		return s.scannerFailure()
	case count == 0:
		return fmt.Errorf("%w: %s cannot find anything: no scanner is installed for the requested transports", ErrNoDiscoveryTransport, s.description)
	}
	return err
}

// describe names a discovery of id (Discovery.#description, :125-158).
func describe(id Identifier) string {
	switch id.kind {
	case identifyInstance:
		return "discovery of node instance " + id.instanceID
	case identifyLongDiscriminator:
		return fmt.Sprintf("discovery of node with discriminator %d", id.long)
	case identifyShortDiscriminator:
		return fmt.Sprintf("discovery of node with discriminator %d", id.short)
	case identifyVendor:
		if id.hasProduct {
			return fmt.Sprintf("discovery of product %d from vendor %d", id.product, id.vendor)
		}
		return fmt.Sprintf("discovery of node from vendor %d", id.vendor)
	case identifyProduct:
		return fmt.Sprintf("discovery of product %d", id.product)
	case identifyDeviceType:
		return fmt.Sprintf("discovery of node with device type %d", id.deviceType)
	}
	return "node discovery"
}

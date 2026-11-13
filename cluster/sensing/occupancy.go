// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package sensing

import (
	"context"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"sync"
	"time"

	"github.com/SukramJ/go-fabric/clock"
	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/cluster/spec"
	occdef "github.com/SukramJ/go-fabric/cluster/spec/occupancysensing"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
)

// OccupancyClusterID is the OccupancySensing cluster id.
const OccupancyClusterID = occdef.ClusterID

// Generated types the OccupancySensing server speaks.
type (
	// OccupancyFeature is a FeatureMap bit.
	OccupancyFeature = occdef.Feature
	// OccupancyHoldTimeLimits bounds HoldTime.
	OccupancyHoldTimeLimits = occdef.HoldTimeLimitsStruct
)

// OccupancyConfig carries the construction parameters of an [Occupancy].
// HoldTime and HoldTimeLimits are the values the host persisted or fixed
// (the declaration's Initial).
type OccupancyConfig struct {
	// Features is the FeatureMap: at least one detector type (OTHER, PIR,
	// US, PHY, AIR, RAD, RFS, VIS: "O.a+"), and OccupancyEvent (OCCEVENT)
	// for the OccupancyChanged event.
	Features OccupancyFeature
	// Attributes are the optional attributes served: HoldTime — which
	// brings HoldTimeLimits and the hold timer — and the deprecated delay
	// attributes the definition gates on it.
	Attributes []uint32
	// Events are the optional events served beyond the selection's.
	Events []uint32
	// HoldTimeLimits is fixed; nil is chip's empty limits, sanitised as
	// chip's SetHoldTimeLimits does to {1, 10, 1}.
	HoldTimeLimits *OccupancyHoldTimeLimits
	// HoldTime is the value the host persisted; nil starts at
	// HoldTimeLimits.HoldTimeDefault. A value outside the limits refuses
	// construction with CONSTRAINT_ERROR, as matter.js validates the
	// configured state at initialization
	// (packages/node/test/node/DotQualifiedConstraintTest.ts:189 at
	// 862ad75f); chip clamps it (OccupancySensingCluster.cpp:100).
	HoldTime *uint16
	// Clock is the device's clock: the hold timer; nil is the system
	// clock.
	Clock clock.Clock
	// DataVersion is an optional host-owned tracker.
	DataVersion *cluster.DataVersionTracker
}

// Occupancy implements [contract.ClusterServer] for OccupancySensing: the
// generated server ([spec.Server]) with matter.js's OccupancySensingServer
// behaviour and, ahead of matter.js, chip's hold timer.
//
//   - The sensor type and its bitmap follow the detector features when
//     the host leaves them unset (matter.js
//     packages/node/src/behaviors/occupancy-sensing/OccupancySensingServer.ts:27-65
//     at 862ad75f; [OccupancySensorTypeFor]).
//   - With OccupancyEvent (OCCEVENT) every change of Occupancy emits
//     OccupancyChanged with the new bitmap, whoever made it — the host's
//     Set or the hold timer (OccupancySensingServer.ts:67-74, :88-90:
//     reactTo occupancy$Changed). Without the feature no event is
//     emitted: chip forces OCCEVENT on and always emits
//     (connectedhomeip src/app/clusters/occupancy-sensor-server/OccupancySensingCluster.h:51-56,
//     OccupancySensingCluster.cpp:315-320 at 6170af84); matter.js decides.
//   - With HoldTime served, the hold timer is chip's
//     (OccupancySensingCluster.cpp:206-302 at 6170af84). matter.js has no
//     timer: it only clears the HoldTime dependents when HoldTime is unset
//     (OccupancySensingServer.ts:76-85, :93-104) — the Ahead-of-matter.js
//     register in notes/parity/by_design.md lists it. A host's occupied
//     sets Occupancy and cancels the timer; its unoccupied starts the timer
//     on the clock's monotonic reading, and Occupancy clears when HoldTime
//     elapses (SetOccupancy :277-297, TimerFired :299-302). A HoldTime
//     outside HoldTimeLimits is refused with CONSTRAINT_ERROR, and a new
//     HoldTime re-arms a running timer for the time that remains, or
//     clears Occupancy at once when that has already elapsed (SetHoldTime
//     :206-275). The deprecated *OccupiedToUnoccupiedDelay attributes,
//     when served, read and write HoldTime (:151-155, :170-181).
//
// Persistence: chip persists HoldTime (.cpp:111-123, :265-272). As for
// [AmbientContext], the host persists it here: a change is reported
// through the server's change notification (the typed OnChanged), and
// [OccupancyConfig].HoldTime restores it.
type Occupancy struct {
	*spec.Instance
	spec.SelfHeld

	srv   *spec.Server
	clock clock.Clock
	event bool // OCCEVENT

	// occMu makes a decision about Occupancy and its store one step: the
	// hold timer's check that it is still current and its store of
	// unoccupied, a host report's stop of the timer and its store, and
	// setHoldTime's clear of an elapsed hold. Without it a host's
	// occupied report could land between the timer's check and its store
	// and be overwritten (review II, R2-P1-clu-03). Taken before mu.
	occMu sync.Mutex

	// holdMu makes a change of the hold time and its store one step, for
	// a controller's write (the Sink changes holdTime, spec.Server stores
	// after it) as for the host's Set: two at once otherwise leave the
	// HoldTime attribute and holdTime, which the timer and the deprecated
	// delay attributes use, at different values (review II,
	// R2-P1-clu-04). Taken before occMu.
	holdMu sync.Mutex

	// mu guards endpoint, hold, limits, holdTime, timer, started and gen.
	mu       sync.Mutex
	endpoint uint16
	hold     bool      // HoldTime served: chip's IsHoldTimeEnabled
	limits   [3]uint16 // min, max, default
	holdTime uint16
	timer    clock.Timer
	started  time.Duration // the monotonic reading the timer started at
	gen      uint64
}

// Compile-time assertions.
var (
	_ contract.ClusterServer                  = (*Occupancy)(nil)
	_ contract.ClusterQuiescer                = (*Occupancy)(nil)
	_ contract.ClusterDataVersion             = (*Occupancy)(nil)
	_ contract.ClusterAttributeLister         = (*Occupancy)(nil)
	_ contract.ClusterEventLister             = (*Occupancy)(nil)
	_ contract.ClusterAttributeWritePrivilege = (*Occupancy)(nil)
	_ contract.AttributeChangeNotifier        = (*Occupancy)(nil)
	_ contract.EventReceiver                  = (*Occupancy)(nil)
	_ spec.AttributeSetter                    = (*Occupancy)(nil)
	_ spec.EventEmitting                      = (*Occupancy)(nil)
)

// holdTimeAliases are the deprecated attributes chip reads and writes as
// HoldTime (OccupancySensingCluster.cpp:151-155, :170-173).
var holdTimeAliases = []uint32{
	occdef.AttrPirOccupiedToUnoccupiedDelay,
	occdef.AttrUltrasonicOccupiedToUnoccupiedDelay,
	occdef.AttrPhysicalContactOccupiedToUnoccupiedDelay,
}

// OccupancySensorTypeFor is matter.js's sync of OccupancySensorTypeBitmap
// and OccupancySensorType from the detector features
// (packages/node/src/behaviors/occupancy-sensing/OccupancySensingServer.ts:34-55
// at 862ad75f): the bitmap from PIR, Ultrasonic and PhysicalContact, the
// type PirAndUltrasonic, Pir, else PhysicalContact. ok is false when none
// of the three is selected; typed is false when the bitmap leaves the type
// unset (Ultrasonic alone), as there.
func OccupancySensorTypeFor(features OccupancyFeature) (kind occdef.OccupancySensorTypeEnum, bitmap occdef.OccupancySensorTypeBitmap, typed, ok bool) {
	if features&occdef.FeaturePassiveInfrared != 0 {
		bitmap |= occdef.OccupancySensorTypeBitmapPir
	}
	if features&occdef.FeatureUltrasonic != 0 {
		bitmap |= occdef.OccupancySensorTypeBitmapUltrasonic
	}
	if features&occdef.FeaturePhysicalContact != 0 {
		bitmap |= occdef.OccupancySensorTypeBitmapPhysicalContact
	}
	if bitmap == 0 {
		return 0, 0, false, false
	}
	switch {
	case bitmap&occdef.OccupancySensorTypeBitmapPir != 0 && bitmap&occdef.OccupancySensorTypeBitmapUltrasonic != 0:
		return occdef.OccupancySensorTypeEnumPirAndUltrasonic, bitmap, true, true
	case bitmap&occdef.OccupancySensorTypeBitmapPir != 0:
		return occdef.OccupancySensorTypeEnumPir, bitmap, true, true
	case bitmap&occdef.OccupancySensorTypeBitmapPhysicalContact != 0:
		return occdef.OccupancySensorTypeEnumPhysicalContact, bitmap, true, true
	}
	return 0, bitmap, false, true
}

// NewOccupancy builds an OccupancySensing server, unoccupied. Mirrors
// chip OccupancySensingCluster.cpp:95-101 (the constructor: the limits
// sanitised; a configured HoldTime is held to them, not clamped, as
// matter.js validates it) and matter.js
// OccupancySensingServer.ts:22-86 (initialize).
func NewOccupancy(cfg OccupancyConfig) (*Occupancy, error) {
	opts := spec.Options{Features: uint32(cfg.Features), Attributes: slices.Clone(cfg.Attributes), Events: slices.Clone(cfg.Events)}
	if slices.Contains(opts.Attributes, occdef.AttrHoldTime) && !slices.Contains(opts.Attributes, occdef.AttrHoldTimeLimits) {
		// HoldTimeLimits' conformance is "HoldTime".
		opts.Attributes = append(opts.Attributes, occdef.AttrHoldTimeLimits)
	}
	o := &Occupancy{clock: clock.Or(cfg.Clock), event: cfg.Features&occdef.FeatureOccupancyEvent != 0}
	srv, err := spec.NewServer(occdef.Definition, opts, spec.ServerConfig{
		DataVersion: cfg.DataVersion, Source: occSource{o}, Sink: occSink{o},
	})
	if err != nil {
		return nil, fmt.Errorf("sensing: OccupancySensing: %w", err)
	}
	o.srv, o.Instance, o.SelfHeld = srv, srv.Instance, spec.NewSelfHeld(srv)
	o.hold = srv.Serves(occdef.AttrHoldTime)

	// A type the features leave unset starts at the enum's
	// SelectDefaultValue, Pir (0), as the kit's generated default does
	// (device fillSelectDefaults).
	kind, bitmap, _, _ := OccupancySensorTypeFor(cfg.Features)
	initial := map[uint32]any{
		occdef.AttrOccupancy:                 occdef.OccupancyBitmap(0),
		occdef.AttrOccupancySensorType:       kind,
		occdef.AttrOccupancySensorTypeBitmap: bitmap,
	}
	if o.hold {
		var limits OccupancyHoldTimeLimits
		if cfg.HoldTimeLimits != nil {
			limits = *cfg.HoldTimeLimits
		}
		o.limits = sanitizeOccupancyLimits(limits) // :99
		o.holdTime = o.limits[2]
		if cfg.HoldTime != nil {
			if h := *cfg.HoldTime; h < o.limits[0] || h > o.limits[1] {
				return nil, spec.Errorf(im.StatusConstraintError, "sensing: OccupancySensing: HoldTime %d outside %s (%d to %d)",
					h, occdef.Definition.Attribute(occdef.AttrHoldTime).Constraint.Text, o.limits[0], o.limits[1])
			}
			o.holdTime = *cfg.HoldTime
		}
		initial[occdef.AttrHoldTimeLimits] = o.limitsValue()
		initial[occdef.AttrHoldTime] = o.holdTime
	}
	if err := srv.SetAttributes(initial); err != nil {
		return nil, fmt.Errorf("sensing: OccupancySensing: %w", err)
	}
	if o.event {
		// OccupancySensingServer.ts:68-69: reactTo(occupancy$Changed).
		srv.OnAttributeChange(func(c contract.Change) {
			if slices.Contains(c.Attributes, occdef.AttrOccupancy) {
				o.emitOccupancyChanged()
			}
		})
	}
	return o, nil
}

// sanitizeOccupancyLimits is chip's SetHoldTimeLimits sanitising
// (OccupancySensingCluster.cpp:333-351): min at least 1, max at least
// max(10, min), default clamped into them.
func sanitizeOccupancyLimits(l OccupancyHoldTimeLimits) [3]uint16 {
	lo := max(1, l.HoldTimeMin)
	hi := max(10, lo, l.HoldTimeMax)
	return [3]uint16{lo, hi, min(max(l.HoldTimeDefault, lo), hi)}
}

func (o *Occupancy) limitsValue() OccupancyHoldTimeLimits {
	return OccupancyHoldTimeLimits{HoldTimeMin: o.limits[0], HoldTimeMax: o.limits[1], HoldTimeDefault: o.limits[2]}
}

// emitOccupancyChanged is #emitOccupancyChanged
// (OccupancySensingServer.ts:88-90): the event carries the new bitmap.
func (o *Occupancy) emitOccupancyChanged() {
	v, _ := o.srv.Value(occdef.AttrOccupancy)
	bitmap, _ := convertTo[occdef.OccupancyBitmap](v)
	o.mu.Lock()
	endpoint := o.endpoint
	o.mu.Unlock()
	_ = o.srv.Emit(endpoint, occdef.EventOccupancyChanged, occdef.OccupancyChangedEvent{Occupancy: bitmap})
}

// occSource answers the deprecated delay attributes with HoldTime
// (OccupancySensingCluster.cpp:151-155).
type occSource struct{ o *Occupancy }

// MatterAttribute implements [spec.Source].
func (s occSource) MatterAttribute(attrID uint32) (any, bool) {
	if !slices.Contains(holdTimeAliases, attrID) {
		return nil, false
	}
	s.o.mu.Lock()
	defer s.o.mu.Unlock()
	return s.o.holdTime, true
}

// occSink carries out a controller's write of HoldTime after the
// definition's checks; the generated server stores it and reports it.
type occSink struct{ o *Occupancy }

// MatterWriteAttribute implements [spec.Sink].
func (k occSink) MatterWriteAttribute(_ context.Context, attrID uint32, value any) error {
	if attrID != occdef.AttrHoldTime {
		return nil
	}
	h, _ := value.(uint16)
	return k.o.setHoldTime(h)
}

// setHoldTime is chip's SetHoldTime (OccupancySensingCluster.cpp:206-275)
// without the store, which its caller does: the limits, the aliases'
// change report, and a running timer re-armed for what remains of the new
// hold time — or Occupancy cleared at once when that has elapsed.
func (o *Occupancy) setHoldTime(h uint16) error {
	o.occMu.Lock()
	defer o.occMu.Unlock()
	o.mu.Lock()
	if !o.hold { // :208
		o.mu.Unlock()
		return spec.Errorf(im.StatusUnsupportedAttribute, "OccupancySensing: HoldTime is not served")
	}
	if h < o.limits[0] || h > o.limits[1] { // :209-210
		o.mu.Unlock()
		return spec.Errorf(im.StatusConstraintError, "OccupancySensing: HoldTime %d outside %d..%d", h, o.limits[0], o.limits[1])
	}
	if h == o.holdTime { // :212-215: the same value is a successful no-op
		o.mu.Unlock()
		return nil
	}
	o.holdTime = h
	elapsedAll := false
	if o.timer != nil { // :234-258
		elapsed := clock.MonotonicOf(o.clock) - o.started
		hold := time.Duration(h) * time.Second
		if elapsed >= hold {
			o.stopLocked()
			elapsedAll = true
		} else {
			o.armLocked(hold - elapsed)
		}
	}
	var aliases []uint32
	for _, id := range holdTimeAliases {
		if o.Serves(id) {
			aliases = append(aliases, id)
		}
	}
	o.mu.Unlock()
	if len(aliases) > 0 { // :217-232
		_ = o.srv.MarkChanged(aliases...)
	}
	if elapsedAll {
		o.doSetOccupancy(false)
	}
	return nil
}

// armLocked (re)starts the hold timer to fire after d; the timer started
// reading stays as it is (chip keeps mTimerStartedTimestamp on a re-arm,
// OccupancySensingCluster.cpp:253-256).
func (o *Occupancy) armLocked(d time.Duration) {
	o.gen++
	if o.timer != nil {
		o.timer.Stop()
	}
	gen := o.gen
	o.timer = o.clock.AfterFunc(d, func() { o.timerFired(gen) })
}

func (o *Occupancy) stopLocked() {
	o.gen++
	if o.timer != nil {
		o.timer.Stop()
		o.timer = nil
	}
}

// timerFired is chip's TimerFired (OccupancySensingCluster.cpp:299-302).
func (o *Occupancy) timerFired(gen uint64) {
	o.occMu.Lock()
	defer o.occMu.Unlock()
	o.mu.Lock()
	if gen != o.gen {
		o.mu.Unlock()
		return
	}
	o.timer = nil
	o.mu.Unlock()
	o.doSetOccupancy(false)
}

// setOccupancy is chip's SetOccupancy (OccupancySensingCluster.cpp:277-297):
// with the hold timer, occupied cancels it and sets Occupancy, unoccupied
// starts it from now and leaves Occupancy as it is until it fires.
func (o *Occupancy) setOccupancy(occupied bool) {
	o.occMu.Lock()
	defer o.occMu.Unlock()
	o.mu.Lock()
	if o.hold {
		if occupied {
			o.stopLocked()
		} else {
			o.started = clock.MonotonicOf(o.clock)
			o.armLocked(time.Duration(o.holdTime) * time.Second)
			o.mu.Unlock()
			return
		}
	}
	o.mu.Unlock()
	o.doSetOccupancy(occupied)
}

// doSetOccupancy is chip's DoSetOccupancy (OccupancySensingCluster.cpp:304-321):
// the store reports a change and the OCCEVENT reaction emits the event;
// an equal value changes nothing.
func (o *Occupancy) doSetOccupancy(occupied bool) {
	var bitmap occdef.OccupancyBitmap
	if occupied {
		bitmap = occdef.OccupancyOccupied
	}
	_ = o.srv.SetAttributes(map[uint32]any{occdef.AttrOccupancy: bitmap})
}

// SetAttributes implements [spec.AttributeSetter], the typed Set: the
// values are checked and stored as the generated server stores them, all
// or nothing, except that Occupancy goes through the hold timer when
// HoldTime is served (chip SetOccupancy) and HoldTime through its limits
// and the running timer (chip SetHoldTime); a deprecated delay attribute
// sets HoldTime.
func (o *Occupancy) SetAttributes(values map[uint32]any) error {
	rest := maps.Clone(values)
	var occ *bool
	if v, ok := rest[occdef.AttrOccupancy]; ok {
		delete(rest, occdef.AttrOccupancy)
		if _, err := o.srv.CheckSet(occdef.AttrOccupancy, v, nil); err != nil {
			return err
		}
		bitmap, _ := convertTo[occdef.OccupancyBitmap](v)
		occupied := bitmap&occdef.OccupancyOccupied != 0
		occ = &occupied
	}
	var hold *uint16
	for _, id := range append([]uint32{occdef.AttrHoldTime}, holdTimeAliases...) {
		v, ok := rest[id]
		if !ok {
			continue
		}
		delete(rest, id)
		if !o.Serves(id) {
			return fmt.Errorf("%w: OccupancySensing 0x%04X", spec.ErrNotServed, id)
		}
		h, ok := convertTo[uint16](v)
		if !ok {
			return fmt.Errorf("%w: OccupancySensing 0x%04X: %T", spec.ErrInvalidValue, id, v)
		}
		if err := o.checkHoldTime(h); err != nil {
			return fmt.Errorf("%w: %w", spec.ErrInvalidValue, err)
		}
		hold = &h
	}
	if len(rest) > 0 {
		if err := o.srv.SetAttributes(rest); err != nil {
			return err
		}
	}
	if hold != nil {
		o.holdMu.Lock()
		err := o.setHoldTime(*hold)
		if err == nil {
			_ = o.srv.SetAttributes(map[uint32]any{occdef.AttrHoldTime: *hold})
		}
		o.holdMu.Unlock()
		if err != nil {
			return fmt.Errorf("%w: %w", spec.ErrInvalidValue, err)
		}
	}
	if occ != nil {
		o.setOccupancy(*occ)
	}
	return nil
}

// convertTo is v in T's form (spec.Convert); false when T cannot hold it.
func convertTo[T any](v any) (T, bool) {
	var zero T
	c, ok := spec.Convert(v, reflect.TypeFor[T]())
	if !ok || !c.IsValid() {
		return zero, false
	}
	t, ok := c.Interface().(T)
	return t, ok
}

// checkHoldTime is SetHoldTime's limit check (OccupancySensingCluster.cpp:209-210).
func (o *Occupancy) checkHoldTime(h uint16) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if h < o.limits[0] || h > o.limits[1] {
		return spec.Errorf(im.StatusConstraintError, "OccupancySensing: HoldTime %d outside %d..%d", h, o.limits[0], o.limits[1])
	}
	return nil
}

// HoldTime returns the hold time in seconds; zero when HoldTime is not
// served.
func (o *Occupancy) HoldTime() uint16 {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.holdTime
}

// MatterDataVersion implements [contract.ClusterDataVersion].
func (o *Occupancy) MatterDataVersion() uint32 { return o.srv.MatterDataVersion() }

// OnAttributeChange implements [contract.AttributeChangeNotifier].
func (o *Occupancy) OnAttributeChange(cb func(change contract.Change)) (unsubscribe func()) {
	return o.srv.OnAttributeChange(cb)
}

// MatterRead resolves an attribute.
func (o *Occupancy) MatterRead(attrID uint32) (any, bool) { return o.srv.MatterRead(attrID) }

// MatterWrite applies a controller's write: HoldTime, or a deprecated
// delay attribute as HoldTime (OccupancySensingCluster.cpp:170-181).
func (o *Occupancy) MatterWrite(ctx context.Context, attrID uint32, value any) error {
	if slices.Contains(holdTimeAliases, attrID) && o.Serves(attrID) {
		attrID = occdef.AttrHoldTime
	}
	if attrID == occdef.AttrHoldTime {
		// The Sink's setHoldTime and the server's store, one step (holdMu).
		o.holdMu.Lock()
		defer o.holdMu.Unlock()
	}
	return o.srv.MatterWrite(ctx, attrID, value)
}

// MatterInvoke answers every command: the cluster has none.
func (o *Occupancy) MatterInvoke(ctx context.Context, cmdID uint32, fields any) (any, error) {
	return spec.InvokeHeld(ctx, &o.SelfHeld, cmdID, fields)
}

// SetMatterEventEmitter implements [contract.EventReceiver].
func (o *Occupancy) SetMatterEventEmitter(emitter contract.EventEmitter) {
	o.srv.SetMatterEventEmitter(emitter)
}

// SetEndpoint stamps the endpoint the events are addressed to; the bridge
// calls it at reassembly.
func (o *Occupancy) SetEndpoint(endpoint uint16) {
	o.mu.Lock()
	o.endpoint = endpoint
	o.mu.Unlock()
}

// Close stops the hold timer; it is [Occupancy.MatterQuiesce].
func (o *Occupancy) Close() { o.MatterQuiesce() }

// MatterQuiesce implements [contract.ClusterQuiescer]: the bridge calls it
// when the server leaves the topology or stops, and it cancels the hold
// timer, so Occupancy does not change after the bridge let the server go.
// Mirrors connectedhomeip
// src/app/clusters/occupancy-sensor-server/OccupancySensingCluster.cpp:127-134
// at 6170af84 (Shutdown: CancelTimer).
func (o *Occupancy) MatterQuiesce() {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.stopLocked()
}

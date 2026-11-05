// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// OccupancySensing: a port of connectedhomeip
// src/app/clusters/occupancy-sensor-server/tests/TestOccupancySensingCluster.cpp
// at 6170af84, by meaning — the hold timer, which matter.js does not have
// (the Ahead-of-matter.js register in notes/parity/by_design.md). chip's
// ClusterTester reads and writes the cluster directly; here the server's
// MatterRead and MatterWrite do. chip's SetOccupancy and SetHoldTime are
// the server's SetAttributes (the typed Set), its TimerDelegateMock the
// fake clock. Two chip rules give way to matter.js: chip forces OCCEVENT
// on and always emits OccupancyChanged (TestOccupancyChangedEventAlwaysGenerated,
// :716-746), matter.js emits it with the feature only; and chip shows
// the *OccupiedToUnoccupiedDelay aliases (conformance D) by default
// (mShowDeprecatedAttributes = true, OccupancySensingCluster.h:81), here a
// host serves them when it opts them in.
package sensing_test

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/cluster/sensing"
	occdef "github.com/SukramJ/go-fabric/cluster/spec/occupancysensing"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
)

// occLimits is a HoldTimeLimits value.
func occLimits(lo, hi, dflt uint16) *sensing.OccupancyHoldTimeLimits {
	return &sensing.OccupancyHoldTimeLimits{HoldTimeMin: lo, HoldTimeMax: hi, HoldTimeDefault: dflt}
}

// holdTimeAttrs serves HoldTime (and so HoldTimeLimits): chip's
// WithHoldTime.
var holdTimeAttrs = []uint32{occdef.AttrHoldTime}

func newOcc(t *testing.T, cfg sensing.OccupancyConfig) (*sensing.Occupancy, *events) {
	t.Helper()
	if cfg.Features == 0 {
		cfg.Features = occdef.FeaturePassiveInfrared
	}
	o, err := sensing.NewOccupancy(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(o.Close)
	em := &events{}
	o.SetMatterEventEmitter(em)
	o.SetEndpoint(1)
	return o, em
}

func setOccupied(t *testing.T, o *sensing.Occupancy, occupied bool) {
	t.Helper()
	bitmap := occdef.OccupancyBitmap(0)
	if occupied {
		bitmap = occdef.OccupancyOccupied
	}
	if err := occdef.Wrap(o).Set(occdef.Patch{Occupancy: &bitmap}); err != nil {
		t.Fatal(err)
	}
}

func occupied(t *testing.T, o *sensing.Occupancy) bool {
	t.Helper()
	return occdef.Wrap(o).Get().Occupancy&occdef.OccupancyOccupied != 0
}

func readU16(t *testing.T, o *sensing.Occupancy, id uint32) uint16 {
	t.Helper()
	v, ok := o.MatterRead(id)
	u, isU16 := v.(uint16)
	if !ok || !isU16 {
		t.Fatalf("attribute 0x%04X: %T %v", id, v, ok)
	}
	return u
}

func setHoldTime(o *sensing.Occupancy, h uint16) error {
	return occdef.Wrap(o).Set(occdef.Patch{HoldTime: &h})
}

func writeU16(o *sensing.Occupancy, id uint32, v uint16) error {
	return o.MatterWrite(context.Background(), id, v)
}

// occupancyEvents are the OccupancyChanged payloads emitted.
func occupancyEvents(e *events) []occdef.OccupancyBitmap {
	var out []occdef.OccupancyBitmap
	for _, r := range e.all() {
		if ev, ok := r.data.(occdef.OccupancyChangedEvent); ok && r.id == occdef.EventOccupancyChanged {
			out = append(out, ev.Occupancy)
		}
	}
	return out
}

// TestOccupancyHoldTimeAttribute ports TestHoldTimeAttribute (:139-210).
func TestOccupancyHoldTimeAttribute(t *testing.T) {
	o, _ := newOcc(t, sensing.OccupancyConfig{
		Features: occdef.FeaturePassiveInfrared | occdef.FeatureUltrasonic | occdef.FeaturePhysicalContact,
		Attributes: []uint32{
			occdef.AttrHoldTime, occdef.AttrPirOccupiedToUnoccupiedDelay,
			occdef.AttrUltrasonicOccupiedToUnoccupiedDelay, occdef.AttrPhysicalContactOccupiedToUnoccupiedDelay,
		},
		HoldTimeLimits: occLimits(10, 200, 100), HoldTime: ptr[uint16](100), Clock: newFake(),
	})
	if err := setHoldTime(o, 150); err != nil || readU16(t, o, occdef.AttrHoldTime) != 150 || o.HoldTime() != 150 { // 1
		t.Fatalf("SetHoldTime 150: %v, %d", err, o.HoldTime())
	}
	if err := setHoldTime(o, 150); err != nil { // 2: the same value is a no-op success
		t.Errorf("SetHoldTime 150 again: %v", err)
	}
	for _, h := range []uint16{5, 250} { // 3, 4
		if err := setHoldTime(o, h); statusOf(err) != im.StatusConstraintError && err == nil {
			t.Errorf("SetHoldTime %d: %v, want refused", h, err)
		}
	}
	if err := writeU16(o, occdef.AttrHoldTime, 180); err != nil || readU16(t, o, occdef.AttrHoldTime) != 180 { // 5
		t.Fatalf("write 180: %v", err)
	}
	for _, alias := range []uint32{ // 6-8
		occdef.AttrPirOccupiedToUnoccupiedDelay, occdef.AttrUltrasonicOccupiedToUnoccupiedDelay,
		occdef.AttrPhysicalContactOccupiedToUnoccupiedDelay,
	} {
		if got := readU16(t, o, alias); got != 180 {
			t.Errorf("alias 0x%04X reads %d, want HoldTime 180", alias, got)
		}
	}
	if err := writeU16(o, occdef.AttrPirOccupiedToUnoccupiedDelay, 120); err != nil || readU16(t, o, occdef.AttrHoldTime) != 120 { // 9
		t.Errorf("write the PIR alias 120: %v, HoldTime %d", err, readU16(t, o, occdef.AttrHoldTime))
	}
	for _, h := range []uint16{5, 250} { // 10, 11
		if err := writeU16(o, occdef.AttrHoldTime, h); statusOf(err) != im.StatusConstraintError {
			t.Errorf("write %d: %v, want CONSTRAINT_ERROR", h, err)
		}
	}
	// 12: without HoldTime there is no hold time to set.
	plain, _ := newOcc(t, sensing.OccupancyConfig{Clock: newFake()})
	if err := setHoldTime(plain, 10); err == nil {
		t.Error("SetHoldTime on a server without HoldTime succeeded")
	}
}

// TestOccupancyAliasNotifications ports TestAliasAttributeNotifications
// (:212-239): a write of the alias reports the alias and HoldTime.
func TestOccupancyAliasNotifications(t *testing.T) {
	o, _ := newOcc(t, sensing.OccupancyConfig{
		Attributes:     []uint32{occdef.AttrHoldTime, occdef.AttrPirOccupiedToUnoccupiedDelay},
		HoldTimeLimits: occLimits(10, 200, 100), HoldTime: ptr[uint16](100), Clock: newFake(),
	})
	var changed []uint32
	o.OnAttributeChange(func(c contract.Change) { changed = append(changed, c.Attributes...) })
	if err := writeU16(o, occdef.AttrPirOccupiedToUnoccupiedDelay, 150); err != nil {
		t.Fatal(err)
	}
	for _, id := range []uint32{occdef.AttrPirOccupiedToUnoccupiedDelay, occdef.AttrHoldTime} {
		if !slices.Contains(changed, id) {
			t.Errorf("changes %v lack 0x%04X", changed, id)
		}
	}
}

// TestOccupancyHoldTimeLimitsConstraints ports TestHoldTimeLimitsConstraints
// (:469-540): the limits are sanitised as chip's SetHoldTimeLimits does.
func TestOccupancyHoldTimeLimitsConstraints(t *testing.T) {
	for _, c := range []struct {
		in, want *sensing.OccupancyHoldTimeLimits
	}{
		{occLimits(0, 20, 10), occLimits(1, 20, 10)},
		{occLimits(15, 10, 15), occLimits(15, 15, 15)},
		{occLimits(10, 20, 5), occLimits(10, 20, 10)},
		{occLimits(10, 20, 25), occLimits(10, 20, 20)},
	} {
		o, _ := newOcc(t, sensing.OccupancyConfig{Attributes: holdTimeAttrs, HoldTimeLimits: c.in, Clock: newFake()})
		v, _ := o.MatterRead(occdef.AttrHoldTimeLimits)
		if got, ok := v.(sensing.OccupancyHoldTimeLimits); !ok || got != *c.want {
			t.Errorf("limits %+v read %+v, want %+v", *c.in, v, *c.want)
		}
	}
}

// TestOccupancyAttributeList ports TestAttributeListMandatoryOnly,
// TestAttributeListWithOptionalAttributes and the PIR case of
// TestAttributeListWithFeatureSpecificOptionalAttributes (:542-600).
func TestOccupancyAttributeList(t *testing.T) {
	ids := func(o *sensing.Occupancy) []uint32 {
		var out []uint32
		for _, id := range o.MatterAttributes() {
			if id < 0xF000 {
				out = append(out, id)
			}
		}
		return out
	}
	mandatory := []uint32{occdef.AttrOccupancy, occdef.AttrOccupancySensorType, occdef.AttrOccupancySensorTypeBitmap}
	o, _ := newOcc(t, sensing.OccupancyConfig{Clock: newFake()})
	if got := ids(o); !slices.Equal(got, mandatory) {
		t.Errorf("mandatory only: %v, want %v", got, mandatory)
	}
	o, _ = newOcc(t, sensing.OccupancyConfig{Attributes: holdTimeAttrs, HoldTimeLimits: occLimits(10, 200, 100), Clock: newFake()})
	if got, want := ids(o), append(slices.Clone(mandatory), occdef.AttrHoldTime, occdef.AttrHoldTimeLimits); !slices.Equal(got, want) {
		t.Errorf("with HoldTime: %v, want %v", got, want)
	}
	o, _ = newOcc(t, sensing.OccupancyConfig{
		Attributes:     []uint32{occdef.AttrHoldTime, occdef.AttrPirOccupiedToUnoccupiedDelay},
		HoldTimeLimits: occLimits(10, 200, 100), Clock: newFake(),
	})
	if got, want := ids(o), append(slices.Clone(mandatory), occdef.AttrHoldTime, occdef.AttrHoldTimeLimits, occdef.AttrPirOccupiedToUnoccupiedDelay); !slices.Equal(got, want) {
		t.Errorf("with the PIR alias: %v, want %v", got, want)
	}
}

// TestOccupancySetOccupancy ports TestSetOccupancy (:659-684).
func TestOccupancySetOccupancy(t *testing.T) {
	o, _ := newOcc(t, sensing.OccupancyConfig{Clock: newFake()})
	if occupied(t, o) {
		t.Fatal("occupied at start")
	}
	setOccupied(t, o, true)
	if !occupied(t, o) {
		t.Fatal("not occupied after the set")
	}
	setOccupied(t, o, false)
	if occupied(t, o) {
		t.Fatal("occupied after the clear")
	}
}

// TestOccupancyChangedEvent ports TestOccupancyChangedEvent (:686-714) on
// the OccupancyEvent feature, and TestOccupancyChangedEventAlwaysGenerated
// (:716-746) as matter.js decides it: without the feature no event.
func TestOccupancyChangedEvent(t *testing.T) {
	o, em := newOcc(t, sensing.OccupancyConfig{Features: occdef.FeaturePassiveInfrared | occdef.FeatureOccupancyEvent, Clock: newFake()})
	setOccupied(t, o, true)
	setOccupied(t, o, false)
	if got := occupancyEvents(em); !slices.Equal(got, []occdef.OccupancyBitmap{occdef.OccupancyOccupied, 0}) {
		t.Errorf("events %v, want [occupied, unoccupied]", got)
	}

	plain, em := newOcc(t, sensing.OccupancyConfig{Clock: newFake()})
	setOccupied(t, plain, true)
	setOccupied(t, plain, false)
	if got := occupancyEvents(em); len(got) != 0 {
		t.Errorf("without OccupancyEvent: events %v, want none (matter.js)", got)
	}
}

// TestOccupancyHoldTime ports TestOccupancyHoldTime (:813-841) and
// TestOccupancyHoldTimeRetrigger (:843-881).
func TestOccupancyHoldTime(t *testing.T) {
	clk := newFake()
	o, em := newOcc(t, sensing.OccupancyConfig{
		Features:   occdef.FeaturePassiveInfrared | occdef.FeatureOccupancyEvent,
		Attributes: holdTimeAttrs, HoldTimeLimits: occLimits(1, 10, 1), HoldTime: ptr[uint16](5), Clock: clk,
	})
	setOccupied(t, o, true)
	setOccupied(t, o, false)
	if !occupied(t, o) || clk.Pending() != 1 {
		t.Fatalf("after the clear: occupied %v, timers %d, want still occupied and one timer", occupied(t, o), clk.Pending())
	}
	clk.Advance(4 * time.Second)
	if !occupied(t, o) {
		t.Fatal("unoccupied before HoldTime elapsed")
	}
	clk.Advance(time.Second)
	if occupied(t, o) || clk.Pending() != 0 {
		t.Fatalf("after HoldTime: occupied %v, timers %d", occupied(t, o), clk.Pending())
	}
	if got := occupancyEvents(em); !slices.Equal(got, []occdef.OccupancyBitmap{occdef.OccupancyOccupied, 0}) {
		t.Errorf("events %v, want [occupied, unoccupied] — the timer's change emits too", got)
	}

	// Retrigger: occupied again cancels the timer, a new clear starts it
	// from the beginning.
	setOccupied(t, o, true)
	setOccupied(t, o, false)
	clk.Advance(3 * time.Second)
	setOccupied(t, o, true)
	if !occupied(t, o) || clk.Pending() != 0 {
		t.Fatalf("retrigger: occupied %v, timers %d, want occupied and none", occupied(t, o), clk.Pending())
	}
	setOccupied(t, o, false)
	clk.Advance(4 * time.Second)
	if !occupied(t, o) {
		t.Fatal("retrigger: unoccupied before the full HoldTime")
	}
	clk.Advance(time.Second)
	if occupied(t, o) {
		t.Fatal("retrigger: occupied after the full HoldTime")
	}
}

// TestOccupancyHoldTimeChangeWithActiveTimer ports
// TestHoldTimeDecreaseWithActiveTimer (:911-939) and
// TestHoldTimeIncreaseWithActiveTimer (:941-978).
func TestOccupancyHoldTimeChangeWithActiveTimer(t *testing.T) {
	clk := newFake()
	o, _ := newOcc(t, sensing.OccupancyConfig{Attributes: holdTimeAttrs, HoldTimeLimits: occLimits(1, 100, 1), HoldTime: ptr[uint16](20), Clock: clk})
	setOccupied(t, o, true)
	setOccupied(t, o, false)
	clk.Advance(10 * time.Second)
	if err := setHoldTime(o, 5); err != nil {
		t.Fatal(err)
	}
	if occupied(t, o) || clk.Pending() != 0 {
		t.Fatalf("a HoldTime below the elapsed time: occupied %v, timers %d, want cleared at once", occupied(t, o), clk.Pending())
	}

	clk = newFake()
	o, _ = newOcc(t, sensing.OccupancyConfig{Attributes: holdTimeAttrs, HoldTimeLimits: occLimits(1, 100, 20), HoldTime: ptr[uint16](20), Clock: clk})
	setOccupied(t, o, true)
	setOccupied(t, o, false)
	clk.Advance(10 * time.Second)
	if err := writeU16(o, occdef.AttrHoldTime, 30); err != nil {
		t.Fatal(err)
	}
	if !occupied(t, o) || clk.Pending() != 1 {
		t.Fatalf("a longer HoldTime: occupied %v, timers %d", occupied(t, o), clk.Pending())
	}
	clk.Advance(19 * time.Second)
	if !occupied(t, o) {
		t.Fatal("unoccupied 29 s into a 30 s hold")
	}
	clk.Advance(time.Second)
	if occupied(t, o) {
		t.Fatal("occupied 30 s into a 30 s hold")
	}
}

// TestOccupancyConstructorClampsHoldTime ports
// TestConstructorClampsInitialHoldTime (:980-1005).
func TestOccupancyConstructorClampsHoldTime(t *testing.T) {
	for _, c := range []struct{ in, want uint16 }{{5, 10}, {25, 20}} {
		o, _ := newOcc(t, sensing.OccupancyConfig{Attributes: holdTimeAttrs, HoldTimeLimits: occLimits(10, 20, 15), HoldTime: ptr(c.in), Clock: newFake()})
		if o.HoldTime() != c.want {
			t.Errorf("HoldTime %d starts at %d, want %d", c.in, o.HoldTime(), c.want)
		}
	}
	o, _ := newOcc(t, sensing.OccupancyConfig{Attributes: holdTimeAttrs, HoldTimeLimits: occLimits(10, 20, 15), Clock: newFake()})
	if o.HoldTime() != 15 {
		t.Errorf("no HoldTime starts at %d, want HoldTimeDefault 15", o.HoldTime())
	}
}

// TestOccupancyShutdownCancelsTimer ports TestShutdownCancelsTimer
// (:1082-1103).
func TestOccupancyShutdownCancelsTimer(t *testing.T) {
	clk := newFake()
	o, _ := newOcc(t, sensing.OccupancyConfig{Attributes: holdTimeAttrs, HoldTimeLimits: occLimits(10, 200, 100), Clock: clk})
	setOccupied(t, o, true)
	setOccupied(t, o, false)
	if clk.Pending() != 1 {
		t.Fatalf("timers %d, want 1", clk.Pending())
	}
	o.MatterQuiesce()
	if clk.Pending() != 0 {
		t.Fatalf("timers %d after quiesce, want 0", clk.Pending())
	}
	clk.Advance(200 * time.Second)
	if !occupied(t, o) {
		t.Fatal("a quiesced server cleared Occupancy")
	}
}

// TestOccupancySensorTypeFromFeatures: matter.js's initialize sync of the
// sensor type and its bitmap (OccupancySensingServer.ts:34-55 at
// 862ad75f); Ultrasonic alone leaves the type at its SelectDefaultValue.
func TestOccupancySensorTypeFromFeatures(t *testing.T) {
	for _, c := range []struct {
		f      sensing.OccupancyFeature
		kind   occdef.OccupancySensorTypeEnum
		bitmap occdef.OccupancySensorTypeBitmap
	}{
		{occdef.FeaturePassiveInfrared, occdef.OccupancySensorTypeEnumPir, occdef.OccupancySensorTypeBitmapPir},
		{occdef.FeaturePassiveInfrared | occdef.FeatureUltrasonic, occdef.OccupancySensorTypeEnumPirAndUltrasonic, occdef.OccupancySensorTypeBitmapPir | occdef.OccupancySensorTypeBitmapUltrasonic},
		{occdef.FeaturePhysicalContact, occdef.OccupancySensorTypeEnumPhysicalContact, occdef.OccupancySensorTypeBitmapPhysicalContact},
		{occdef.FeatureUltrasonic, occdef.OccupancySensorTypeEnumPir, occdef.OccupancySensorTypeBitmapUltrasonic},
		{occdef.FeatureRadar, occdef.OccupancySensorTypeEnumPir, 0},
	} {
		o, _ := newOcc(t, sensing.OccupancyConfig{Features: c.f, Clock: newFake()})
		st := occdef.Wrap(o).Get()
		if kind, bitmap := st.OccupancySensorType, st.OccupancySensorTypeBitmap; kind != c.kind || bitmap != c.bitmap {
			t.Errorf("features 0x%X: type %v bitmap %v, want %v %v", uint32(c.f), kind, bitmap, c.kind, c.bitmap)
		}
	}
}

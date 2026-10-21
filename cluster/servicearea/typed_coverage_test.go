// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package servicearea_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/servicearea"
	"github.com/SukramJ/go-fabric/cluster/spec"
	sa "github.com/SukramJ/go-fabric/cluster/spec/servicearea"
	"github.com/SukramJ/go-fabric/im"
)

// statusCodeOf is the IM status an error carries.
func statusCodeOf(tb testing.TB, err error) im.StatusCode {
	tb.Helper()
	var sce im.StatusCodeError
	if !errors.As(err, &sce) {
		tb.Fatalf("error %v carries no IM status", err)
	}
	return sce.MatterStatusCode()
}

// TestSetAttributesAppliesEveryHeldAttribute: the device's own change of
// the lists, CurrentArea and EstimatedEndTime lands as one, in either
// accepted list form.
func TestSetAttributesAppliesEveryHeldAttribute(t *testing.T) {
	t.Parallel()

	srv := newServer(t, vacuum(&device{}))
	seen := notifications(srv)
	err := srv.SetAttributes(map[uint32]any{
		sa.AttrSupportedMaps:    spec.List[servicearea.Map]{{MapId: 0, Name: "Ground"}, {MapId: 1, Name: "Upstairs"}},
		sa.AttrSupportedAreas:   []servicearea.Area{area(7, ptr[uint32](0), "Kitchen"), area(10, ptr[uint32](1), "Bedroom")},
		sa.AttrSelectedAreas:    []uint32{7, 10},
		sa.AttrCurrentArea:      uint32(10),
		sa.AttrEstimatedEndTime: uint32(900),
		sa.AttrProgress:         []servicearea.Progress{{AreaId: 7}, {AreaId: 10}},
	})
	if err != nil {
		t.Fatalf("SetAttributes: %v", err)
	}
	if got := read[spec.List[servicearea.Area]](t, srv, sa.AttrSupportedAreas); len(got) != 2 || got[1].AreaId != 10 {
		t.Errorf("SupportedAreas %+v", got)
	}
	if got := read[spec.List[servicearea.Map]](t, srv, sa.AttrSupportedMaps); len(got) != 2 {
		t.Errorf("SupportedMaps %+v", got)
	}
	if got := read[[]uint32](t, srv, sa.AttrSelectedAreas); !slices.Equal(got, []uint32{7, 10}) {
		t.Errorf("SelectedAreas %v", got)
	}
	if got := read[uint32](t, srv, sa.AttrCurrentArea); got != 10 {
		t.Errorf("CurrentArea %v", got)
	}
	if got := read[uint32](t, srv, sa.AttrEstimatedEndTime); got != 900 {
		t.Errorf("EstimatedEndTime %v", got)
	}
	if got := read[spec.List[servicearea.Progress]](t, srv, sa.AttrProgress); len(got) != 2 {
		t.Errorf("Progress %+v", got)
	}
	for _, id := range []uint32{sa.AttrSupportedMaps, sa.AttrSupportedAreas, sa.AttrSelectedAreas, sa.AttrCurrentArea, sa.AttrProgress} {
		if !slices.Contains(*seen, id) {
			t.Errorf("attribute 0x%04X was not reported", id)
		}
	}

	// An empty list is accepted, and an empty map changes nothing.
	if err := srv.SetAttributes(map[uint32]any{sa.AttrSelectedAreas: []uint32{}, sa.AttrProgress: []servicearea.Progress{}}); err != nil {
		t.Fatalf("SetAttributes(empty lists): %v", err)
	}
	if got := read[[]uint32](t, srv, sa.AttrSelectedAreas); len(got) != 0 {
		t.Errorf("SelectedAreas %v, want empty", got)
	}
	if err := srv.SetAttributes(nil); err != nil {
		t.Errorf("SetAttributes(nil) = %v", err)
	}
}

// TestSetAttributesNullCurrentAreaResetsTheEstimate: a null CurrentArea
// makes EstimatedEndTime null too, and a null EstimatedEndTime is applied.
func TestSetAttributesNullCurrentAreaResetsTheEstimate(t *testing.T) {
	t.Parallel()

	cfg := vacuum(&device{})
	cfg.InitialCurrentArea = ptr[uint32](7)
	srv := newServer(t, cfg)
	if err := srv.SetEstimatedEndTime(ptr[uint32](500)); err != nil {
		t.Fatal(err)
	}
	if err := srv.SetAttributes(map[uint32]any{sa.AttrCurrentArea: nil}); err != nil {
		t.Fatalf("SetAttributes: %v", err)
	}
	if v, _ := srv.MatterRead(sa.AttrCurrentArea); v != nil {
		t.Errorf("CurrentArea %v, want null", v)
	}
	if v, _ := srv.MatterRead(sa.AttrEstimatedEndTime); v != nil {
		t.Errorf("EstimatedEndTime %v, want null", v)
	}

	if err := srv.SetAttributes(map[uint32]any{sa.AttrEstimatedEndTime: uint32(40)}); err != nil {
		t.Fatalf("SetAttributes(EstimatedEndTime): %v", err)
	}
	if err := srv.SetAttributes(map[uint32]any{sa.AttrEstimatedEndTime: nil}); err != nil {
		t.Fatalf("SetAttributes(EstimatedEndTime null): %v", err)
	}
	if v, _ := srv.MatterRead(sa.AttrEstimatedEndTime); v != nil {
		t.Errorf("EstimatedEndTime %v, want null", v)
	}

	// Without EstimatedEndTime a null CurrentArea has nothing to reset.
	plain := vacuum(&device{})
	plain.EstimatedEndTime = false
	plain.InitialCurrentArea = ptr[uint32](7)
	psrv := newServer(t, plain)
	if err := psrv.SetAttributes(map[uint32]any{sa.AttrCurrentArea: nil}); err != nil {
		t.Errorf("SetAttributes without EstimatedEndTime: %v", err)
	}
}

// TestSetAttributesRefusalsChangeNothing: an unserved attribute, a value
// of the wrong type and a value that breaks matter.js's assertions are
// refused, and a refusal in the middle restores what was stored before it.
func TestSetAttributesRefusalsChangeNothing(t *testing.T) {
	t.Parallel()

	srv := newServer(t, vacuum(&device{}))
	before := srv.MatterDataVersion()
	areasBefore := read[spec.List[servicearea.Area]](t, srv, sa.AttrSupportedAreas)

	if err := srv.SetAttributes(map[uint32]any{0xFFF0: uint32(1)}); !errors.Is(err, spec.ErrNotServed) {
		t.Errorf("unknown attribute: %v, want ErrNotServed", err)
	}
	if err := srv.SetAttributes(map[uint32]any{sa.AttrCurrentArea: "kitchen"}); !errors.Is(err, spec.ErrInvalidValue) {
		t.Errorf("CurrentArea of a string: %v, want ErrInvalidValue", err)
	}
	// The maps and areas are stored before the selection is refused.
	err := srv.SetAttributes(map[uint32]any{
		sa.AttrSupportedMaps:  []servicearea.Map{{MapId: 0, Name: "Ground"}, {MapId: 1, Name: "Upstairs"}},
		sa.AttrSupportedAreas: []servicearea.Area{area(7, ptr[uint32](0), "Kitchen"), area(10, ptr[uint32](1), "Bedroom")},
		sa.AttrSelectedAreas:  []uint32{99},
	})
	if !errors.Is(err, servicearea.ErrInvalidState) {
		t.Errorf("an unsupported selection: %v, want ErrInvalidState", err)
	}
	if got := read[spec.List[servicearea.Area]](t, srv, sa.AttrSupportedAreas); len(got) != len(areasBefore) || got[0].AreaId != areasBefore[0].AreaId {
		t.Errorf("SupportedAreas %+v, want the state before the refusal", got)
	}
	if got := read[spec.List[servicearea.Map]](t, srv, sa.AttrSupportedMaps); len(got) != 1 {
		t.Errorf("SupportedMaps %+v, want the state before the refusal", got)
	}
	if srv.MatterDataVersion() != before {
		t.Error("a refused change moved the data version")
	}

	// Each attribute's own rule.
	for name, values := range map[string]map[uint32]any{
		"duplicate map":     {sa.AttrSupportedMaps: []servicearea.Map{{MapId: 0, Name: "A"}, {MapId: 0, Name: "B"}}},
		"unsupported area":  {sa.AttrCurrentArea: uint32(99)},
		"progress dup":      {sa.AttrProgress: []servicearea.Progress{{AreaId: 7}, {AreaId: 7}}},
		"areas without map": {sa.AttrSupportedAreas: []servicearea.Area{area(7, nil, "Kitchen")}},
	} {
		if err := srv.SetAttributes(values); !errors.Is(err, servicearea.ErrInvalidState) {
			t.Errorf("%s: %v, want ErrInvalidState", name, err)
		}
	}
}

// TestSetAttributesNeedsTheOptionalAttribute: an attribute the server does
// not serve is refused even when the value fits.
func TestSetAttributesNeedsTheOptionalAttribute(t *testing.T) {
	t.Parallel()

	cfg := vacuum(&device{})
	cfg.Features = 0
	cfg.SupportedMaps = nil
	cfg.SupportedAreas = []servicearea.Area{area(7, nil, "Kitchen")}
	srv := newServer(t, cfg)
	if err := srv.SetAttributes(map[uint32]any{sa.AttrProgress: []servicearea.Progress{{AreaId: 7}}}); !errors.Is(err, spec.ErrNotServed) {
		t.Errorf("Progress without PROG: %v, want ErrNotServed", err)
	}
}

// TestHandleReplacesSelectAreas: a host handler takes the command, nil
// hands it back and a command the cluster does not accept is refused.
func TestHandleReplacesSelectAreas(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	d := &device{}
	srv := newServer(t, vacuum(d))
	calls := 0
	if err := srv.Handle(sa.CmdSelectAreas, func(context.Context, any) (any, error) {
		calls++
		return nil, nil
	}); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if _, err := srv.MatterInvoke(ctx, sa.CmdSelectAreas, sa.SelectAreasRequest{NewAreas: []uint32{7}}); err != nil || calls != 1 || len(d.selections) != 0 {
		t.Errorf("host handler: err %v, calls %d, selections %v; want the handler only", err, calls, d.selections)
	}
	if err := srv.Handle(sa.CmdSelectAreas, nil); err != nil {
		t.Fatalf("Handle(nil): %v", err)
	}
	if _, err := srv.MatterInvoke(ctx, sa.CmdSelectAreas, sa.SelectAreasRequest{NewAreas: []uint32{7}}); err != nil || len(d.selections) != 1 {
		t.Errorf("after nil: err %v, selections %v; want the server's own handling", err, d.selections)
	}
	if err := srv.Handle(0x42, func(context.Context, any) (any, error) { return nil, nil }); !errors.Is(err, spec.ErrNotAccepted) {
		t.Errorf("Handle(0x42) = %v, want ErrNotAccepted", err)
	}
	_, err := srv.MatterInvoke(ctx, 0x42, nil)
	if statusCodeOf(t, err) != im.StatusUnsupportedCommand {
		t.Errorf("MatterInvoke(0x42) = %v, want UnsupportedCommand", err)
	}
}

// TestEmitRefusesEveryEvent: ServiceArea defines no event.
func TestEmitRefusesEveryEvent(t *testing.T) {
	t.Parallel()

	srv := newServer(t, vacuum(&device{}))
	if err := srv.Emit(1, 0, nil); !errors.Is(err, spec.ErrNotEmitted) {
		t.Errorf("Emit = %v, want ErrNotEmitted", err)
	}
}

// TestSetBeforeWriteAcceptsAVeto: the veto can be set and removed; every
// write is refused by the definition before it would run (no attribute is
// writable), so the veto never sees one.
func TestSetBeforeWriteAcceptsAVeto(t *testing.T) {
	t.Parallel()

	srv := newServer(t, vacuum(&device{}))
	ran := false
	srv.SetBeforeWrite(func(context.Context, uint32, any) error {
		ran = true
		return errors.New("vetoed")
	})
	err := srv.MatterWrite(context.Background(), sa.AttrSelectedAreas, []uint32{7})
	if statusCodeOf(t, err) != im.StatusUnsupportedWrite {
		t.Errorf("MatterWrite = %v, want UnsupportedWrite", err)
	}
	if ran {
		t.Error("the veto ran for a write the definition refuses")
	}
	srv.SetBeforeWrite(nil)
}

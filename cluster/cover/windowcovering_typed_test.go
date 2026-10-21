// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package cover_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/cover"
	"github.com/SukramJ/go-fabric/cluster/spec"
	wcdef "github.com/SukramJ/go-fabric/cluster/spec/windowcovering"
)

func ptrOf[T any](v T) *T { return &v }

// typedCovering is the self-held WindowCovering server (a position-aware
// lift at 50 %, no Mover) under its typed server.
func typedCovering(t *testing.T) *wcdef.Server {
	t.Helper()
	srv, err := cover.New(cover.Config{FeatureMap: cover.ServedFeatureMap, InitialPositionPercent100ths: 5000})
	if err != nil {
		t.Fatal(err)
	}
	return wcdef.Wrap(srv)
}

func TestWindowCoveringTypedSetGetOnChanged(t *testing.T) {
	t.Parallel()
	s := typedCovering(t)
	var changes [][]uint32
	unsubscribe := s.OnChanged(func(_ wcdef.State, changed []uint32) { changes = append(changes, changed) })
	defer unsubscribe()

	if err := s.Set(wcdef.Patch{
		CurrentPositionLiftPercent100ths: ptrOf(uint16(2500)),
		TargetPositionLiftPercent100ths:  ptrOf(uint16(2500)),
		OperationalStatus:                ptrOf(wcdef.OperationalStatusBitmap(0)),
	}); err != nil {
		t.Fatal(err)
	}
	st := s.Get()
	if st.CurrentPositionLiftPercent100ths == nil || *st.CurrentPositionLiftPercent100ths != 2500 ||
		st.TargetPositionLiftPercent100ths == nil || *st.TargetPositionLiftPercent100ths != 2500 ||
		st.CurrentPositionLiftPercentage == nil || *st.CurrentPositionLiftPercentage != 25 {
		t.Errorf("state %+v", st)
	}
	want := []uint32{wcdef.AttrCurrentPositionLiftPercentage, wcdef.AttrTargetPositionLiftPercent100ths, wcdef.AttrCurrentPositionLiftPercent100ths}
	if len(changes) != 1 || !slices.Equal(changes[0], want) {
		t.Errorf("changes %v, want [%v]", changes, want)
	}

	// Mode is stored as given and ConfigStatus follows it.
	if err := s.Set(wcdef.Patch{Mode: ptrOf(wcdef.ModeMaintenanceMode)}); err != nil {
		t.Fatal(err)
	}
	if got := s.Get(); got.Mode != wcdef.ModeMaintenanceMode || got.ConfigStatus&wcdef.ConfigStatusOperational != 0 {
		t.Errorf("after MaintenanceMode: %+v", got)
	}
	if last := changes[len(changes)-1]; !slices.Equal(last, []uint32{wcdef.AttrConfigStatus, wcdef.AttrMode}) {
		t.Errorf("the Mode change reported %v", last)
	}

	for name, p := range map[string]wcdef.Patch{
		"ConfigStatus (derived)":  {ConfigStatus: ptrOf(wcdef.ConfigStatusBitmap(0))},
		"Percentage (derived)":    {CurrentPositionLiftPercentage: ptrOf(uint8(10))},
		"another Type":            {Type: ptrOf(wcdef.TypeShutter)},
		"a null current position": {Null: []uint32{wcdef.AttrCurrentPositionLiftPercent100ths}},
	} {
		if err := s.Set(p); !errors.Is(err, spec.ErrNotHeld) && !errors.Is(err, spec.ErrInvalidValue) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if got := s.Get(); *got.CurrentPositionLiftPercent100ths != 2500 {
		t.Errorf("a refused patch moved the lift to %d", *got.CurrentPositionLiftPercent100ths)
	}
}

func TestWindowCoveringTypedHandleReplacesTheCommand(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := typedCovering(t)
	var got []wcdef.GoToLiftPercentageRequest
	if err := s.HandleGoToLiftPercentage(func(_ context.Context, req wcdef.GoToLiftPercentageRequest) (struct{}, error) {
		got = append(got, req)
		return struct{}{}, nil
	}); err != nil {
		t.Fatal(err)
	}
	req := wcdef.GoToLiftPercentageRequest{LiftPercent100thsValue: 1000}
	if _, err := s.Underlying().MatterInvoke(ctx, wcdef.CmdGoToLiftPercentage, req); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != req || *s.Get().CurrentPositionLiftPercent100ths != 5000 {
		t.Errorf("the host's handler saw %v; lift %d", got, *s.Get().CurrentPositionLiftPercent100ths)
	}
	if err := s.HandleGoToLiftPercentage(nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Underlying().MatterInvoke(ctx, wcdef.CmdGoToLiftPercentage, &req); err != nil {
		t.Fatal(err)
	}
	if lift := *s.Get().CurrentPositionLiftPercent100ths; lift != 1000 || len(got) != 1 {
		t.Errorf("after nil: lift %d, host calls %d", lift, len(got))
	}
	if err := s.HandleGoToTiltPercentage(func(context.Context, wcdef.GoToTiltPercentageRequest) (struct{}, error) {
		return struct{}{}, nil
	}); !errors.Is(err, spec.ErrNotAccepted) {
		t.Errorf("a tilt command on a lift: %v", err)
	}
}

func TestWindowCoveringTypedEmitAndOnWrite(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := typedCovering(t)
	if err := spec.EmitOn(s.Underlying(), 1, 0, nil); !errors.Is(err, spec.ErrNotEmitted) {
		t.Errorf("an event of a cluster without events: %v", err)
	}
	refusal := errors.New("the installer locked the mode")
	var seen []wcdef.ModeBitmap
	if err := s.OnWrite(func(_ context.Context, next wcdef.State, attrID uint32) error {
		seen = append(seen, next.Mode)
		return refusal
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.Underlying().MatterWrite(ctx, wcdef.AttrMode, uint64(wcdef.ModeMaintenanceMode)); !errors.Is(err, refusal) {
		t.Fatalf("a vetoed write: %v", err)
	}
	if len(seen) != 1 || seen[0] != wcdef.ModeMaintenanceMode || s.Get().Mode != 0 {
		t.Errorf("the veto saw %v; Mode %v", seen, s.Get().Mode)
	}
}

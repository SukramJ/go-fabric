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
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
)

// recordingEmitter records every event the server emits.
type recordingEmitter struct{ events []uint32 }

func (e *recordingEmitter) MatterEmitEvent(_ uint16, _, eventID uint32, _ any, _ contract.EventPriority) {
	e.events = append(e.events, eventID)
}

// TestWindowCoveringTypedSetRules: OperationalStatus is the host's to set
// and reported; an unserved attribute is refused by the definition; a
// configuration attribute set to the value it reads changes nothing, to
// another one is not held.
func TestWindowCoveringTypedSetRules(t *testing.T) {
	t.Parallel()
	s := typedCovering(t)
	var changes [][]uint32
	defer s.OnChanged(func(_ wcdef.State, changed []uint32) { changes = append(changes, changed) })()

	moving := wcdef.OperationalStatusBitmap(0x05) // Global and Lift opening
	if err := s.Set(wcdef.Patch{OperationalStatus: &moving}); err != nil {
		t.Fatal(err)
	}
	if got := s.Get().OperationalStatus; got != moving {
		t.Errorf("OperationalStatus %v, want %v", got, moving)
	}
	if len(changes) != 1 || !slices.Equal(changes[0], []uint32{wcdef.AttrOperationalStatus}) {
		t.Errorf("changes %v, want [[OperationalStatus]]", changes)
	}

	// Tilt is not served by a lift-only covering.
	if err := s.Set(wcdef.Patch{CurrentPositionTiltPercent100ths: ptrOf(uint16(10))}); !errors.Is(err, spec.ErrNotServed) {
		t.Errorf("an unserved tilt position: %v, want ErrNotServed", err)
	}

	// The Type it reads is accepted and reports nothing; another is not held.
	typ := s.Get().Type
	changes = nil
	if err := s.Set(wcdef.Patch{Type: &typ}); err != nil || len(changes) != 0 {
		t.Errorf("the configured Type: %v, changes %v", err, changes)
	}
	other := typ + 1
	if err := s.Set(wcdef.Patch{Type: &other}); !errors.Is(err, spec.ErrNotHeld) {
		t.Errorf("another Type: %v, want ErrNotHeld", err)
	}
}

// TestWindowCoveringTypedEmitThroughTheEmitter: with the endpoint's
// emitter installed, an event WindowCovering does not define is refused
// and nothing reaches the emitter.
func TestWindowCoveringTypedEmitThroughTheEmitter(t *testing.T) {
	t.Parallel()
	srv, err := cover.New(cover.Config{FeatureMap: cover.ServedFeatureMap})
	if err != nil {
		t.Fatal(err)
	}
	em := &recordingEmitter{}
	srv.SetMatterEventEmitter(em)
	if err := srv.Emit(1, 0x00, nil); !errors.Is(err, spec.ErrNotEmitted) {
		t.Errorf("Emit: %v, want ErrNotEmitted", err)
	}
	if len(em.events) != 0 {
		t.Errorf("events %v reached the emitter", em.events)
	}
}

// TestWindowCoveringModeWriteRules: a controller's Mode write with
// CalibrationMode alone is CONSTRAINT_ERROR (no calibration procedure);
// with MaintenanceMode beside it the calibration bit is cleared, the write
// is vetted by OnWrite and ConfigStatus follows and is reported.
func TestWindowCoveringModeWriteRules(t *testing.T) {
	t.Parallel()
	s := typedCovering(t)
	srv := s.Underlying()
	ctx := context.Background()

	err := srv.MatterWrite(ctx, wcdef.AttrMode, uint64(wcdef.ModeCalibrationMode))
	var sce im.StatusCodeError
	if !errors.As(err, &sce) || sce.MatterStatusCode() != im.StatusConstraintError {
		t.Errorf("CalibrationMode write: %v, want CONSTRAINT_ERROR", err)
	}

	var vetted []any
	if err := s.OnWrite(func(_ context.Context, next wcdef.State, attrID uint32) error {
		vetted = append(vetted, next.Mode)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	var changes [][]uint32
	defer s.OnChanged(func(_ wcdef.State, changed []uint32) { changes = append(changes, changed) })()
	if err := srv.MatterWrite(ctx, wcdef.AttrMode, uint64(wcdef.ModeMaintenanceMode|wcdef.ModeCalibrationMode)); err != nil {
		t.Fatalf("Maintenance+Calibration write: %v", err)
	}
	if got := s.Get(); got.Mode != wcdef.ModeMaintenanceMode || got.ConfigStatus&wcdef.ConfigStatusOperational != 0 {
		t.Errorf("after the write: Mode %v, ConfigStatus %v", got.Mode, got.ConfigStatus)
	}
	if len(vetted) != 1 {
		t.Errorf("OnWrite saw %v, want one write", vetted)
	}
	found := false
	for _, c := range changes {
		found = found || slices.Contains(c, wcdef.AttrConfigStatus)
	}
	if !found {
		t.Errorf("changes %v, want ConfigStatus reported", changes)
	}
}

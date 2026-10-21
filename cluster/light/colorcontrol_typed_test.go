// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package light_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/light"
	"github.com/SukramJ/go-fabric/cluster/spec"
	ccdef "github.com/SukramJ/go-fabric/cluster/spec/colorcontrol"
	"github.com/SukramJ/go-fabric/contract"
)

func ptrOf[T any](v T) *T { return &v }

// typedColorControl is the self-held ColorControl server (hue and
// saturation, colour temperature 153–500 mireds) under its typed server.
func typedColorControl(t *testing.T) *ccdef.Server {
	t.Helper()
	srv, err := light.NewColorControl(light.ColorControlServerConfig{
		Features:  light.ColorFeatureHueSaturation | light.ColorFeatureColorTemperature,
		MinMireds: 153, MaxMireds: 500, InitialMireds: 250,
	})
	if err != nil {
		t.Fatal(err)
	}
	return ccdef.Wrap(srv)
}

func TestColorControlTypedSetGetOnChanged(t *testing.T) {
	t.Parallel()
	s := typedColorControl(t)
	var changes [][]uint32
	unsubscribe := s.OnChanged(func(_ ccdef.State, change contract.Change) {
		changed := change.Attributes
		changes = append(changes, changed)
	})
	defer unsubscribe()

	if err := s.Set(ccdef.Patch{
		CurrentHue: ptrOf(uint8(100)), ColorMode: ptrOf(ccdef.ColorModeCurrentHueAndCurrentSaturation),
		Options: ptrOf(ccdef.OptionsExecuteIfOff),
	}); err != nil {
		t.Fatal(err)
	}
	st := s.Get()
	if st.CurrentHue != 100 || st.ColorMode != ccdef.ColorModeCurrentHueAndCurrentSaturation || st.Options != ccdef.OptionsExecuteIfOff {
		t.Errorf("state %+v", st)
	}
	if st.EnhancedColorMode != ccdef.EnhancedColorModeEnum(st.ColorMode) {
		t.Errorf("EnhancedColorMode %v did not follow ColorMode %v", st.EnhancedColorMode, st.ColorMode)
	}
	var reported []uint32
	for _, c := range changes {
		reported = append(reported, c...)
	}
	for _, id := range []uint32{ccdef.AttrColorMode, ccdef.AttrOptions} {
		if !slices.Contains(reported, id) {
			t.Errorf("0x%04X not reported: %v", id, changes)
		}
	}

	// Outside the physical range: nothing of the patch is stored.
	if err := s.Set(ccdef.Patch{CurrentHue: ptrOf(uint8(7)), ColorTemperatureMireds: ptrOf(uint16(600))}); !errors.Is(err, spec.ErrInvalidValue) {
		t.Errorf("a mired value outside the range: %v", err)
	}
	if got := s.Get(); got.CurrentHue != 100 || got.ColorTemperatureMireds != 250 {
		t.Errorf("a refused patch stored %+v", got)
	}
	// Derived and configured attributes are not the server's to set.
	if err := s.Set(ccdef.Patch{RemainingTime: ptrOf(uint16(5))}); !errors.Is(err, spec.ErrNotHeld) {
		t.Errorf("RemainingTime: %v", err)
	}
	if err := s.Set(ccdef.Patch{ColorTempPhysicalMinMireds: ptrOf(uint16(200))}); !errors.Is(err, spec.ErrNotHeld) {
		t.Errorf("another physical minimum: %v", err)
	}
	if err := s.Set(ccdef.Patch{ColorTempPhysicalMinMireds: ptrOf(uint16(153))}); err != nil {
		t.Errorf("the physical minimum it reads: %v", err)
	}
}

func TestColorControlTypedHandleReplacesTheCommand(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := typedColorControl(t)
	var got []ccdef.MoveToColorTemperatureRequest
	if err := s.HandleMoveToColorTemperature(func(_ context.Context, req ccdef.MoveToColorTemperatureRequest) (struct{}, error) {
		got = append(got, req)
		return struct{}{}, nil
	}); err != nil {
		t.Fatal(err)
	}
	req := ccdef.MoveToColorTemperatureRequest{ColorTemperatureMireds: 400}
	if _, err := s.Underlying().MatterInvoke(ctx, ccdef.CmdMoveToColorTemperature, req); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != req || s.Get().ColorTemperatureMireds != 250 {
		t.Errorf("the host's handler saw %v; mireds %d", got, s.Get().ColorTemperatureMireds)
	}
	// nil hands the command back: the server's own logic runs, on the
	// pointer form too.
	if err := s.HandleMoveToColorTemperature(nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Underlying().MatterInvoke(ctx, ccdef.CmdMoveToColorTemperature, &req); err != nil {
		t.Fatal(err)
	}
	if m := s.Get().ColorTemperatureMireds; m != 400 || len(got) != 1 {
		t.Errorf("after nil: mireds %d, host calls %d", m, len(got))
	}
	// MoveToColor needs XY, which this server lacks.
	if err := s.HandleMoveToColor(func(context.Context, ccdef.MoveToColorRequest) (struct{}, error) {
		return struct{}{}, nil
	}); !errors.Is(err, spec.ErrNotAccepted) {
		t.Errorf("a command the selection does not accept: %v", err)
	}
}

func TestColorControlTypedEmitAndOnWrite(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := typedColorControl(t)
	// ColorControl defines no event.
	if err := spec.EmitOn(s.Underlying(), 1, 0, nil); !errors.Is(err, spec.ErrNotEmitted) {
		t.Errorf("an event of a cluster without events: %v", err)
	}
	refusal := errors.New("the lamp stays as it is")
	var seen []ccdef.OptionsBitmap
	if err := s.OnWrite(func(_ context.Context, next ccdef.State, attrID uint32) error {
		if attrID == ccdef.AttrOptions {
			seen = append(seen, next.Options)
			return refusal
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.Underlying().MatterWrite(ctx, ccdef.AttrOptions, uint64(1)); !errors.Is(err, refusal) {
		t.Fatalf("a vetoed write: %v", err)
	}
	if len(seen) != 1 || seen[0] != ccdef.OptionsExecuteIfOff || s.Get().Options != 0 {
		t.Errorf("the veto saw %v; Options %v", seen, s.Get().Options)
	}
	if err := s.OnWrite(nil); err != nil {
		t.Fatal(err)
	}
	if err := s.Underlying().MatterWrite(ctx, ccdef.AttrOptions, uint64(1)); err != nil || s.Get().Options != ccdef.OptionsExecuteIfOff {
		t.Errorf("without the veto: %v, Options %v", err, s.Get().Options)
	}
}

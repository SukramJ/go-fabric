// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package cover_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/cover"
	"github.com/SukramJ/go-fabric/cluster/wire"
	"github.com/SukramJ/go-fabric/im"
)

func newLiftServer() *cover.WindowCoveringServer {
	return cover.NewWindowCoveringServer(cover.Config{
		Type:                         0,
		EndProductType:               0,
		FeatureMap:                   0b101, // Lift + PositionAwareLift
		InitialPositionPercent100ths: 0,
	})
}

// readPercent100ths reads CurrentPositionLiftPercent100ths (id 0xe,
// matter.js packages/model/src/standard/elements/window-covering-cluster.element.ts:71).
func readPercent100ths(t *testing.T, srv *cover.WindowCoveringServer) uint16 {
	t.Helper()
	raw, ok := srv.MatterRead(wire.WindowCoveringAttrCurrentPositionLiftPercent100ths)
	if !ok {
		t.Fatal("CurrentPositionLiftPercent100ths: ok = false")
	}
	v, isU16 := raw.(uint16)
	if !isU16 {
		t.Fatalf("CurrentPositionLiftPercent100ths is %T, want uint16 (percent100ths)", raw)
	}
	return v
}

// TestWindowCoveringGoToLiftPercentageAcceptsEveryPayloadShape drives the
// command through each shape extractPercent100ths normalises. The
// LiftPercent100thsValue is field 0 of GoToLiftPercentage
// (window-covering-cluster.element.ts:92-95), and the position it names
// must arrive unchanged whichever shape carried it — the encoding a
// caller happened to use is not allowed to move the blind somewhere else.
func TestWindowCoveringGoToLiftPercentageAcceptsEveryPayloadShape(t *testing.T) {
	t.Parallel()
	const want uint16 = 4275

	shapes := map[string]any{
		"bare uint16":    want,
		"typed request":  wire.GoToLiftPercentageRequest{LiftPercent100thsValue: want},
		"typed pointer":  &wire.GoToLiftPercentageRequest{LiftPercent100thsValue: want},
		"bridge tag map": map[uint8]any{0: uint64(want)},
		"string map":     map[string]any{"percent": want},
	}
	for name, fields := range shapes {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			srv := newLiftServer()
			if _, err := srv.MatterInvoke(
				context.Background(), wire.WindowCoveringCmdGoToLiftPercentage, fields,
			); err != nil {
				t.Fatalf("GoToLiftPercentage: %v", err)
			}
			if got := readPercent100ths(t, srv); got != want {
				t.Fatalf("CurrentPositionLiftPercent100ths = %d, want %d", got, want)
			}
		})
	}
}

// TestWindowCoveringGoToLiftPercentageRejectsAPayloadItCannotRead covers
// the refusal paths of the payload normaliser. Each must leave the
// position untouched: a command the server cannot read must not move the
// covering to a guessed position.
func TestWindowCoveringGoToLiftPercentageRejectsAPayloadItCannotRead(t *testing.T) {
	t.Parallel()
	cases := map[string]any{
		"nil typed pointer":  (*wire.GoToLiftPercentageRequest)(nil),
		"tag map missing 0":  map[uint8]any{1: uint64(0)},
		"tag map wrong type": map[uint8]any{0: "half"},
		// percent100ths is uint16 on the wire; a wider integer is a
		// malformed field (window-covering-cluster.element.ts:95).
		"tag map wider than uint16": map[uint8]any{0: uint64(70000)},
		"string map missing key":    map[string]any{"lift": uint16(0)},
		"string map wrong type":     map[string]any{"percent": 42},
		"unknown Go type":           []string{"5000"},
	}
	for name, fields := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			srv := cover.NewWindowCoveringServer(cover.Config{
				FeatureMap:                   0b101,
				InitialPositionPercent100ths: 2500,
			})
			if _, err := srv.MatterInvoke(
				context.Background(), wire.WindowCoveringCmdGoToLiftPercentage, fields,
			); err == nil {
				t.Fatal("GoToLiftPercentage accepted a payload it cannot read")
			}
			if got := readPercent100ths(t, srv); got != 2500 {
				t.Fatalf("position moved to %d on a rejected payload, want the initial 2500", got)
			}
		})
	}
}

// TestWindowCoveringConstraintErrorNamesTheLimit pins the two
// constraint-violating writes and the message each carries. The limits
// are read from matter.js, not chosen here: LiftPercent100thsValue is a
// percent100ths with constraint "max 10000"
// (window-covering-cluster.element.ts:71,95) and Mode carries constraint
// "max 15" (window-covering-cluster.element.ts:79).
//
// The message is asserted because a bare ConstraintError tells an
// installer nothing about which limit they hit.
func TestWindowCoveringConstraintErrorNamesTheLimit(t *testing.T) {
	t.Parallel()

	t.Run("lift percent above 10000", func(t *testing.T) {
		t.Parallel()
		srv := newLiftServer()
		_, err := srv.MatterInvoke(
			context.Background(), wire.WindowCoveringCmdGoToLiftPercentage, uint16(10001),
		)
		var status im.StatusCodeError
		if !errors.As(err, &status) {
			t.Fatalf("err = %v (%T), want an im.StatusCodeError", err, err)
		}
		if got := status.MatterStatusCode(); got != im.StatusConstraintError {
			t.Errorf("status = %v, want ConstraintError", got)
		}
		if msg := err.Error(); !strings.Contains(msg, "10000") || !strings.Contains(msg, "10001") {
			t.Errorf("message = %q, want it to name both the limit 10000 and the value 10001", msg)
		}
	})

	t.Run("mode above 15", func(t *testing.T) {
		t.Parallel()
		srv := newLiftServer()
		err := srv.MatterWrite(context.Background(), wire.WindowCoveringAttrMode, uint8(16))
		var status im.StatusCodeError
		if !errors.As(err, &status) {
			t.Fatalf("err = %v (%T), want an im.StatusCodeError", err, err)
		}
		if got := status.MatterStatusCode(); got != im.StatusConstraintError {
			t.Errorf("status = %v, want ConstraintError", got)
		}
		if msg := err.Error(); !strings.Contains(msg, "15") {
			t.Errorf("message = %q, want it to name the limit 15", msg)
		}
		// A refused write must not land.
		raw, ok := srv.MatterRead(wire.WindowCoveringAttrMode)
		if !ok || raw.(uint8) != 0 {
			t.Errorf("Mode = %v after a refused write, want the default 0", raw)
		}
	})
}

// TestWindowCoveringModeWriteRejectsANonNumericValue covers the type
// guard on the one writable attribute. Mode is a ModeBitmap
// (window-covering-cluster.element.ts:79); a string is not a bitmap and
// must be refused rather than coerced to zero, which would silently clear
// every mode bit the installer had set.
func TestWindowCoveringModeWriteRejectsANonNumericValue(t *testing.T) {
	t.Parallel()
	srv := newLiftServer()
	if err := srv.MatterWrite(context.Background(), wire.WindowCoveringAttrMode, "reversed"); err == nil {
		t.Fatal("Mode accepted a string")
	}
}

// TestWindowCoveringReadsEveryAdvertisedAttribute walks MatterAttributes
// and requires MatterRead to answer each one. A server that lists an
// attribute it cannot read answers UnsupportedAttribute on a wildcard
// read, which Apple Home and chip-tool both treat as a conformance
// failure.
func TestWindowCoveringReadsEveryAdvertisedAttribute(t *testing.T) {
	t.Parallel()
	srv := newLiftServer()
	for _, attrID := range srv.MatterAttributes() {
		if _, ok := srv.MatterRead(attrID); !ok {
			t.Errorf("MatterRead(0x%04X): ok = false, but the attribute is advertised", attrID)
		}
	}
	// An attribute this profile does not carry stays unsupported: tilt
	// needs the TL feature (window-covering-cluster.element.ts:96-99).
	// CurrentPositionTiltPercent100ths, id 0xf.
	if _, ok := srv.MatterRead(0x000F); ok {
		t.Error("CurrentPositionTiltPercent100ths answered on a lift-only server")
	}
}

// TestWindowCoveringDeprecatedLiftPercentageTracksPercent100ths pins the
// scaling of the deprecated uint8 CurrentPositionLiftPercentage (id 0x8)
// against the percent100ths attribute (id 0xe,
// window-covering-cluster.element.ts:71). A Matter 1.0 controller reads
// only the former, so a wrong divisor shows it a blind at the wrong
// height with nothing else disagreeing.
func TestWindowCoveringDeprecatedLiftPercentageTracksPercent100ths(t *testing.T) {
	t.Parallel()
	srv := newLiftServer()
	if _, err := srv.MatterInvoke(
		context.Background(), wire.WindowCoveringCmdGoToLiftPercentage, uint16(7350),
	); err != nil {
		t.Fatalf("GoToLiftPercentage: %v", err)
	}
	raw, ok := srv.MatterRead(wire.WindowCoveringAttrCurrentPositionLiftPercentage)
	if !ok {
		t.Fatal("CurrentPositionLiftPercentage: ok = false")
	}
	// 7350 hundredths of a percent is 73 whole percent, truncated.
	if got := raw.(uint8); got != 73 {
		t.Errorf("CurrentPositionLiftPercentage = %d, want 73 for 7350 percent100ths", got)
	}
}

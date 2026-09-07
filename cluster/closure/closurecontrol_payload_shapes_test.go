// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package closure_test

import (
	"context"
	"errors"
	"testing"

	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/cluster/closure"
	clusterwire "github.com/SukramJ/go-fabric/cluster/wire"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
)

// closureMoveToPayload encodes a MoveTo command payload the way the
// firmware sends it: an anonymous struct carrying context-tagged fields,
// Position on tag 0 (matter.js
// packages/model/src/standard/elements/closure-control.element.ts:79).
func closureMoveToPayload(t *testing.T, fields func(e *tlv.Encoder)) []byte {
	t.Helper()
	e := tlv.NewEncoder()
	e.StartStruct(tlv.AnonymousTag())
	fields(e)
	if err := e.EndContainer(); err != nil {
		t.Fatalf("EndContainer: %v", err)
	}
	b, err := e.Bytes()
	if err != nil {
		t.Fatalf("Bytes: %v", err)
	}
	return b
}

// TestClosureControlAdvertisesItsClusterIDAndFeatureMap pins the two
// values a controller uses to find the cluster at all: the ID 0x0104 and
// the advertised feature set. Positioning (bit 0) and Ventilation (bit 1)
// are the profile this server exists for — Ventilation carries
// conformance "[PS]" (closure-control.element.ts:31), so it is never
// advertised without Positioning.
func TestClosureControlAdvertisesItsClusterIDAndFeatureMap(t *testing.T) {
	t.Parallel()
	srv := closure.NewControlServer((&recordingHandlers{}).config())

	if got := srv.MatterClusterID(); got != clusterwire.ClosureControlClusterID {
		t.Errorf("MatterClusterID = 0x%04X, want 0x%04X", got, clusterwire.ClosureControlClusterID)
	}
	if got := srv.FeatureMap(); got != closure.PositioningVentilationFeatureMap {
		t.Errorf("FeatureMap = 0x%X, want 0x%X", got, closure.PositioningVentilationFeatureMap)
	}
	// The same value must reach a controller through the global
	// attribute, not only through the Go accessor.
	raw, ok := srv.MatterRead(cluster.AttrGlobalFeatureMap)
	if !ok {
		t.Fatal("MatterRead(FeatureMap): ok = false")
	}
	if raw != closure.PositioningVentilationFeatureMap {
		t.Errorf("MatterRead(FeatureMap) = %v, want 0x%X", raw, closure.PositioningVentilationFeatureMap)
	}
}

// TestClosureControlMoveToAcceptsEveryPayloadShape drives MoveTo through
// each shape moveToRequest normalises: the typed request a host may
// decode itself, a pointer to it, and the raw TLV the firmware sends.
// Each must reach the device handler with the same target, because the
// shape a payload arrives in is a transport detail and must not change
// what the drive does.
func TestClosureControlMoveToAcceptsEveryPayloadShape(t *testing.T) {
	t.Parallel()
	pos := clusterwire.ClosureTargetPositionMoveToVentilationPosition

	shapes := map[string]any{
		"typed value":   clusterwire.MoveToRequest{Position: &pos},
		"typed pointer": &clusterwire.MoveToRequest{Position: &pos},
		"raw TLV": closureMoveToPayload(t, func(e *tlv.Encoder) {
			e.PutUint(tlv.ContextTag(0), uint64(pos))
		}),
		"bridge tag map": map[uint8]any{0: uint64(pos)},
	}
	for name, fields := range shapes {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			h := &recordingHandlers{}
			srv := closure.NewControlServer(h.config())
			if _, err := srv.MatterInvoke(
				context.Background(), clusterwire.ClosureControlCmdMoveTo, fields,
			); err != nil {
				t.Fatalf("MoveTo: %v", err)
			}
			if len(h.moved) != 1 || h.moved[0] != pos {
				t.Fatalf("device saw %v, want one move to %d", h.moved, pos)
			}
		})
	}
}

// TestClosureControlMoveToRejectsAPayloadItCannotRead covers the refusal
// paths of the payload normaliser. Every one of them must fail the
// command: a MoveTo the server cannot read is not a MoveTo it may guess
// at, because the guess moves a real drive.
func TestClosureControlMoveToRejectsAPayloadItCannotRead(t *testing.T) {
	t.Parallel()
	cases := map[string]any{
		// A nil typed pointer carries no fields at all.
		"nil typed pointer": (*clusterwire.MoveToRequest)(nil),
		// Position is TargetPositionEnum, an enum8
		// (closure-control.element.ts:79,97): a wider integer is a
		// malformed field, not a large position.
		"position wider than enum8": map[uint8]any{0: uint64(256)},
		"position not an integer":   map[uint8]any{0: "open"},
		// Latch is type bool (closure-control.element.ts:80).
		"latch not a bool": map[uint8]any{1: uint64(1)},
		// Speed is ThreeLevelAutoEnum, also enum8
		// (closure-control.element.ts:81).
		"speed wider than enum8": map[uint8]any{2: uint64(300)},
		"speed not an integer":   map[uint8]any{2: true},
		// Every field explicitly null reads as absent, and all three
		// fields are "O.a+" — at least one is mandatory.
		"all fields null": map[uint8]any{0: nil, 1: nil, 2: nil},
		// A shape the normaliser has no case for.
		"unknown Go type": "MoveToFullyOpen",
	}
	for name, fields := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			h := &recordingHandlers{}
			srv := closure.NewControlServer(h.config())
			_, err := srv.MatterInvoke(context.Background(), clusterwire.ClosureControlCmdMoveTo, fields)
			if err == nil {
				t.Fatal("MoveTo accepted a payload it cannot read")
			}
			if len(h.moved) != 0 {
				t.Fatalf("device was moved to %v on a rejected payload", h.moved)
			}
		})
	}
}

// TestClosureControlMoveToTagMapCarriesLatchAndSpeed verifies the two
// fields that are read but cannot act on their own. Latch (tag 1) and
// Speed (tag 2) belong to MotionLatching and Speed
// (closure-control.element.ts:80-81); this server advertises neither, so
// a request carrying only them is a ConstraintError rather than a silent
// success. The tag map must still decode them — a decoder that dropped
// Latch would make this request look like an empty one, which is a
// different error.
func TestClosureControlMoveToTagMapCarriesLatchAndSpeed(t *testing.T) {
	t.Parallel()
	h := &recordingHandlers{}
	srv := closure.NewControlServer(h.config())

	_, err := srv.MatterInvoke(context.Background(), clusterwire.ClosureControlCmdMoveTo, map[uint8]any{
		1: true,
		2: uint64(2),
	})
	if err == nil {
		t.Fatal("MoveTo with only Latch and Speed was accepted")
	}
	var status im.StatusCodeError
	if !errors.As(err, &status) {
		t.Fatalf("err = %v (%T), want an im.StatusCodeError", err, err)
	}
	if got := status.MatterStatusCode(); got != im.StatusConstraintError {
		t.Errorf("status = %v, want ConstraintError", got)
	}
	// Not the malformed-payload error: the fields decoded fine, the
	// feature set is what cannot serve them.
	if errors.Is(err, clusterwire.ErrClosureControlMalformed) {
		t.Error("a decodable Latch+Speed request must not report as malformed")
	}
}

// TestClosureControlMoveToAndStopFailWithoutAHandler covers the two
// unhandled-command paths. A server mounted without handlers must report
// Failure: answering Success would tell the controller a drive moved
// when nothing was wired to move it.
func TestClosureControlMoveToAndStopFailWithoutAHandler(t *testing.T) {
	t.Parallel()
	srv := closure.NewControlServer(closure.Config{})
	pos := clusterwire.ClosureTargetPositionMoveToFullyOpen

	if _, err := srv.MatterInvoke(
		context.Background(), clusterwire.ClosureControlCmdMoveTo,
		clusterwire.MoveToRequest{Position: &pos},
	); err == nil {
		t.Error("MoveTo without a handler reported success")
	}
	if _, err := srv.MatterInvoke(
		context.Background(), clusterwire.ClosureControlCmdStop, nil,
	); err == nil {
		t.Error("Stop without a handler reported success")
	}
	// The state must not claim a move that never left the process.
	if raw, ok := srv.MatterRead(clusterwire.ClosureControlAttrMainState); !ok ||
		raw.(uint8) != uint8(clusterwire.ClosureMainStateSetupRequired) {
		t.Errorf("MainState = %v, want SetupRequired after two failed commands", raw)
	}
}

// TestClosureControlStopReportsADeviceRefusal pins the failing-Stop path:
// the device error is wrapped and reaches the caller, and the cluster
// keeps the state it had rather than recording a stop that did not
// happen.
func TestClosureControlStopReportsADeviceRefusal(t *testing.T) {
	t.Parallel()
	sentinel := errors.New("drive is jammed")
	h := &recordingHandlers{stopErr: sentinel}
	srv := closure.NewControlServer(h.config())

	_, err := srv.MatterInvoke(context.Background(), clusterwire.ClosureControlCmdStop, nil)
	if !errors.Is(err, sentinel) {
		t.Fatalf("err = %v, want it to wrap %v", err, sentinel)
	}
	if raw, ok := srv.MatterRead(clusterwire.ClosureControlAttrMainState); !ok ||
		raw.(uint8) != uint8(clusterwire.ClosureMainStateSetupRequired) {
		t.Errorf("MainState = %v, want SetupRequired — a refused Stop is not a stop", raw)
	}
}

// TestClosureControlUnknownCommandIsRefused covers the default arm of the
// command dispatch. The cluster carries exactly three commands — Stop
// 0x0, MoveTo 0x1, Calibrate 0x2 (closure-control.element.ts:75-84) — so
// anything else is an error, not a no-op.
func TestClosureControlUnknownCommandIsRefused(t *testing.T) {
	t.Parallel()
	h := &recordingHandlers{}
	srv := closure.NewControlServer(h.config())

	if _, err := srv.MatterInvoke(context.Background(), 0x7F, nil); err == nil {
		t.Fatal("an unknown command reported success")
	}
	if h.stopped != 0 || len(h.moved) != 0 {
		t.Fatal("an unknown command reached a device handler")
	}
}

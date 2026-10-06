// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package closure_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/closure"
	clusterwire "github.com/SukramJ/go-fabric/cluster/wire"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
)

type emitted struct {
	endpoint uint16
	event    uint32
	data     any
	priority contract.EventPriority
}

type recordingEmitter struct{ events []emitted }

func (r *recordingEmitter) MatterEmitEvent(endpoint uint16, _, event uint32, data any, priority contract.EventPriority) {
	r.events = append(r.events, emitted{endpoint, event, data, priority})
}

func (r *recordingEmitter) ids() []uint32 {
	var out []uint32
	for _, e := range r.events {
		out = append(out, e.event)
	}
	return out
}

func newEmittingServer(t *testing.T, featureMap uint32) (*closure.ControlServer, *recordingEmitter, *recordingHandlers) {
	t.Helper()
	h := &recordingHandlers{}
	cfg := h.config()
	cfg.FeatureMap = featureMap
	srv := closure.NewControlServer(cfg)
	rec := &recordingEmitter{}
	srv.SetMatterEventEmitter(rec)
	srv.SetEndpoint(7)
	return srv, rec, h
}

// The event, command and attribute surface follows the advertised features
// (closure-control.element.ts): OperationalError and SecureStateChanged are
// M, MovementCompleted is !IS, EngageStateChanged MO; Stop is !IS, MoveTo
// M, Calibrate CL.
func TestClosureControlListsFollowTheFeatures(t *testing.T) {
	t.Parallel()
	pv := closure.NewControlServer(closure.Config{})
	if got := pv.MatterEvents(); !slices.Equal(got, []uint32{0x00, 0x01, 0x03}) {
		t.Errorf("PS+VT events = %v", got)
	}
	if got := pv.MatterAcceptedCommands(); !slices.Equal(got, []uint32{0x00, 0x01}) {
		t.Errorf("PS+VT accepted commands = %v", got)
	}
	if got := pv.MatterGeneratedCommands(); len(got) != 0 {
		t.Errorf("generated commands = %v, want none", got)
	}
	full := closure.NewControlServer(closure.Config{FeatureMap: clusterwire.ClosureControlFeaturePositioning |
		clusterwire.ClosureControlFeatureCalibration | clusterwire.ClosureControlFeatureManuallyOperable})
	if got := full.MatterEvents(); !slices.Equal(got, []uint32{0x00, 0x01, 0x02, 0x03}) {
		t.Errorf("PS+CL+MO events = %v", got)
	}
	if got := full.MatterAcceptedCommands(); !slices.Equal(got, []uint32{0x00, 0x01, 0x02}) {
		t.Errorf("PS+CL+MO accepted commands = %v", got)
	}
	instant := closure.NewControlServer(closure.Config{FeatureMap: clusterwire.ClosureControlFeatureMotionLatching | clusterwire.ClosureControlFeatureInstantaneous})
	if got := instant.MatterEvents(); !slices.Equal(got, []uint32{0x00, 0x03}) {
		t.Errorf("LT+IS events = %v", got)
	}
	if got := instant.MatterAcceptedCommands(); !slices.Equal(got, []uint32{0x01}) {
		t.Errorf("LT+IS accepted commands = %v", got)
	}
}

// A motion that ends at its target emits MovementCompleted; the closure
// reaching FullyClosed — SecureState true — and leaving it emits
// SecureStateChanged each time; an error emits OperationalError (critical)
// with the errors.
func TestClosureControlEvents(t *testing.T) {
	t.Parallel()
	srv, rec, _ := newEmittingServer(t, 0)
	closed := clusterwire.ClosureCurrentPositionFullyClosed
	opened := clusterwire.ClosureCurrentPositionFullyOpened
	srv.SetCurrentPosition(&closed)
	srv.SetMainState(clusterwire.ClosureMainStateStopped)
	open := clusterwire.ClosureTargetPositionMoveToFullyOpen
	if _, err := srv.MatterInvoke(context.Background(), clusterwire.ClosureControlCmdMoveTo, clusterwire.MoveToRequest{Position: &open}); err != nil {
		t.Fatal(err)
	}
	srv.SetCurrentPosition(&opened)
	srv.SetMainState(clusterwire.ClosureMainStateStopped)
	srv.SetCurrentPosition(&opened) // no change, no event
	srv.ReportError(clusterwire.ClosureErrorList{clusterwire.ClosureError(0)})
	srv.ReportError(nil) // an error event carries at least one error
	want := []uint32{0x03, 0x03, 0x01, 0x00}
	if got := rec.ids(); !slices.Equal(got, want) {
		t.Fatalf("events %v, want SecureStateChanged(true), SecureStateChanged(false), MovementCompleted, OperationalError", got)
	}
	if v := rec.events[0].data.(closure.SecureStateChangedEvent); !v.SecureValue {
		t.Errorf("first SecureStateChanged %+v, want true", v)
	}
	if v := rec.events[1].data.(closure.SecureStateChangedEvent); v.SecureValue {
		t.Errorf("second SecureStateChanged %+v, want false", v)
	}
	if e := rec.events[3]; e.priority != contract.EventPriorityCritical || e.endpoint != 7 {
		t.Errorf("OperationalError %+v, want critical on endpoint 7", e)
	}
	if raw, _ := srv.MatterRead(clusterwire.ClosureControlAttrMainState); raw.(uint8) != uint8(clusterwire.ClosureMainStateError) {
		t.Errorf("MainState after an error = %v, want Error", raw)
	}
	if raw, _ := srv.MatterRead(clusterwire.ClosureControlAttrCurrentErrorList); len(raw.(clusterwire.ClosureErrorList)) != 1 {
		t.Errorf("CurrentErrorList = %v, want the reported error", raw)
	}

	enc := tlv.NewEncoder()
	closure.OperationalErrorEvent{ErrorState: clusterwire.ClosureErrorList{1, 3}}.EncodeTLV(enc, tlv.AnonymousTag())
	if b, _ := enc.Bytes(); len(b) == 0 {
		t.Error("OperationalError encodes to nothing")
	}
}

// EngageStateChanged needs ManuallyOperable; a change of the engaged state
// emits it and moves MainState in and out of Disengaged.
func TestClosureControlEngageState(t *testing.T) {
	t.Parallel()
	srv, rec, _ := newEmittingServer(t, clusterwire.ClosureControlFeaturePositioning|clusterwire.ClosureControlFeatureManuallyOperable)
	srv.SetEngaged(false)
	srv.SetEngaged(false)
	srv.SetEngaged(true)
	if got := rec.ids(); !slices.Equal(got, []uint32{0x02, 0x02}) {
		t.Fatalf("events %v, want two EngageStateChanged", got)
	}
	if raw, _ := srv.MatterRead(clusterwire.ClosureControlAttrMainState); raw.(uint8) != uint8(clusterwire.ClosureMainStateStopped) {
		t.Errorf("MainState after re-engaging = %v, want Stopped", raw)
	}
	plain, prec, _ := newEmittingServer(t, 0)
	plain.SetEngaged(false)
	if len(prec.events) != 0 {
		t.Error("EngageStateChanged without the ManuallyOperable feature")
	}
}

// MoveTo and Stop as the specification text matter.js carries describes
// them (closure-control.resource.ts): no field at all is INVALID_COMMAND;
// an absent Position falls back to OverallTargetState.Position; Stop
// changes only a closure in motion and always answers SUCCESS.
func TestClosureControlMoveToAndStopRules(t *testing.T) {
	t.Parallel()
	srv, _, h := newEmittingServer(t, 0)
	_, err := srv.MatterInvoke(context.Background(), clusterwire.ClosureControlCmdMoveTo, map[uint8]any{})
	if sce, ok := errors.AsType[im.StatusCodeError](err); !ok || sce.MatterStatusCode() != im.StatusInvalidCommand {
		t.Errorf("MoveTo with no field: %v, want INVALID_COMMAND", err)
	}
	vent := clusterwire.ClosureTargetPositionMoveToVentilationPosition
	if _, err := srv.MatterInvoke(context.Background(), clusterwire.ClosureControlCmdMoveTo, clusterwire.MoveToRequest{Position: &vent}); err != nil {
		t.Fatal(err)
	}
	latch := true
	if _, err := srv.MatterInvoke(context.Background(), clusterwire.ClosureControlCmdMoveTo, clusterwire.MoveToRequest{Latch: &latch}); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(h.moved, []clusterwire.ClosureTargetPosition{vent, vent}) {
		t.Errorf("moves %v, want the ventilation target twice (the second from the OverallTargetState fallback)", h.moved)
	}
	srv.SetMainState(clusterwire.ClosureMainStateSetupRequired)
	if _, err := srv.MatterInvoke(context.Background(), clusterwire.ClosureControlCmdStop, nil); err != nil || h.stopped != 0 {
		t.Errorf("Stop in SetupRequired: err %v, stopped %d — want SUCCESS and no action", err, h.stopped)
	}
	if raw, _ := srv.MatterRead(clusterwire.ClosureControlAttrMainState); raw.(uint8) != uint8(clusterwire.ClosureMainStateSetupRequired) {
		t.Errorf("MainState after Stop in SetupRequired = %v", raw)
	}
}

// The server tells the bridge which attributes a state change moved, so an
// arrival or an error the drive reports between commands reaches
// subscribers (TC-CLCTRL-4.1 waits for MainState Stopped after a MoveTo).
func TestClosureControlNotifiesChangedAttributes(t *testing.T) {
	t.Parallel()
	srv, _, _ := newEmittingServer(t, 0)
	var got [][]uint32
	unsub := srv.OnMatterAttributesChanged(func(ids []uint32) { got = append(got, slices.Clone(ids)) })
	defer unsub()
	open := clusterwire.ClosureTargetPositionMoveToFullyOpen
	if _, err := srv.MatterInvoke(context.Background(), clusterwire.ClosureControlCmdMoveTo, clusterwire.MoveToRequest{Position: &open}); err != nil {
		t.Fatal(err)
	}
	opened := clusterwire.ClosureCurrentPositionFullyOpened
	srv.SetCurrentPosition(&opened)
	srv.SetMainState(clusterwire.ClosureMainStateStopped)
	srv.SetMainState(clusterwire.ClosureMainStateStopped) // unchanged: no notification
	srv.ReportError(clusterwire.ClosureErrorList{clusterwire.ClosureError(1)})
	want := [][]uint32{
		{clusterwire.ClosureControlAttrMainState, clusterwire.ClosureControlAttrOverallTargetState},
		{clusterwire.ClosureControlAttrOverallCurrentState, clusterwire.ClosureControlAttrOverallTargetState},
		{clusterwire.ClosureControlAttrMainState},
		{clusterwire.ClosureControlAttrMainState, clusterwire.ClosureControlAttrCurrentErrorList},
	}
	if len(got) != len(want) {
		t.Fatalf("notifications %v, want %v", got, want)
	}
	for i := range want {
		if !slices.Equal(got[i], want[i]) {
			t.Errorf("notification %d = %v, want %v", i, got[i], want[i])
		}
	}
}

// The event payloads encode as closure-control.element.ts defines them:
// OperationalError {0: list<enum8>}, MovementCompleted {}, EngageStateChanged
// {0: bool}, SecureStateChanged {0: bool}.
func TestClosureControlEventPayloadBytes(t *testing.T) {
	t.Parallel()
	enc := func(e interface {
		EncodeTLV(*tlv.Encoder, tlv.Tag)
	},
	) []byte {
		x := tlv.NewEncoder()
		e.EncodeTLV(x, tlv.AnonymousTag())
		b, err := x.Bytes()
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	for name, tc := range map[string]struct {
		got, want []byte
	}{
		"OperationalError":   {enc(closure.OperationalErrorEvent{ErrorState: clusterwire.ClosureErrorList{1, 3}}), []byte{0x15, 0x36, 0x00, 0x04, 0x01, 0x04, 0x03, 0x18, 0x18}},
		"MovementCompleted":  {enc(closure.MovementCompletedEvent{}), []byte{0x15, 0x18}},
		"EngageStateChanged": {enc(closure.EngageStateChangedEvent{EngageValue: true}), []byte{0x15, 0x29, 0x00, 0x18}},
		"SecureStateChanged": {enc(closure.SecureStateChangedEvent{SecureValue: false}), []byte{0x15, 0x28, 0x00, 0x18}},
	} {
		if !slices.Equal(tc.got, tc.want) {
			t.Errorf("%s = % X, want % X", name, tc.got, tc.want)
		}
	}
}

// ReportError keeps at most ten errors (CurrentErrorList "max 10[all]").
func TestClosureControlReportErrorTruncates(t *testing.T) {
	t.Parallel()
	srv, rec, _ := newEmittingServer(t, 0)
	srv.ReportError(make(clusterwire.ClosureErrorList, 12))
	if v := rec.events[0].data.(closure.OperationalErrorEvent); len(v.ErrorState) != 10 {
		t.Errorf("event carries %d errors, want 10", len(v.ErrorState))
	}
	pd := clusterwire.ClosureTargetPositionMoveToPedestrianPosition
	_, err := srv.MatterInvoke(context.Background(), clusterwire.ClosureControlCmdMoveTo, clusterwire.MoveToRequest{Position: &pd})
	if sce, ok := errors.AsType[im.StatusCodeError](err); !ok || sce.MatterStatusCode() != im.StatusConstraintError || err.Error() == "" {
		t.Errorf("MoveTo Pedestrian without PD: %v, want CONSTRAINT_ERROR", err)
	}
	_, err = srv.MatterInvoke(context.Background(), clusterwire.ClosureControlCmdMoveTo, map[uint8]any{})
	if err == nil || err.Error() == "" {
		t.Error("an empty MoveTo carries no message")
	}
	_, err = srv.MatterInvoke(context.Background(), clusterwire.ClosureControlCmdCalibrate, nil)
	if err == nil || err.Error() == "" {
		t.Error("Calibrate without CL carries no message")
	}
}

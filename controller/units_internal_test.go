// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controller

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/cluster/camera"
	wrdef "github.com/SukramJ/go-fabric/cluster/spec/webrtctransportrequestor"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/secure/sessionparams"
)

// TestSessionIntervalsValidate accepts whole milliseconds inside the
// bounds and nil, and refuses a negative, a fractional and an
// out-of-bounds value.
func TestSessionIntervalsValidate(t *testing.T) {
	t.Parallel()
	var none *SessionIntervals
	if err := none.validate(); err != nil {
		t.Fatalf("nil: %v", err)
	}
	ok := &SessionIntervals{Idle: 500 * time.Millisecond, Active: 300 * time.Millisecond, ActiveThreshold: 4 * time.Second}
	if err := ok.validate(); err != nil {
		t.Fatalf("valid: %v", err)
	}
	for name, s := range map[string]*SessionIntervals{
		"negative":   {Idle: -time.Millisecond},
		"fractional": {Active: 1500 * time.Microsecond},
		"too long":   {ActiveThreshold: 70 * time.Second},
	} {
		if err := s.validate(); !errors.Is(err, ErrConfiguration) {
			t.Errorf("%s: %v, want ErrConfiguration", name, err)
		}
	}
}

// TestSessionIntervalsApply writes the set fields and keeps the rest.
func TestSessionIntervalsApply(t *testing.T) {
	t.Parallel()
	p := sessionparams.Parameters{SessionIdleInterval: 1, SessionActiveInterval: 2, SessionActiveThreshold: 3}
	var none *SessionIntervals
	none.apply(&p)
	(&SessionIntervals{Active: 400 * time.Millisecond}).apply(&p)
	if p.SessionIdleInterval != 1 || p.SessionActiveInterval != 400 || p.SessionActiveThreshold != 3 {
		t.Fatalf("after Active only: %+v", p)
	}
	(&SessionIntervals{Idle: time.Second, ActiveThreshold: 5 * time.Second}).apply(&p)
	if p.SessionIdleInterval != 1000 || p.SessionActiveInterval != 400 || p.SessionActiveThreshold != 5000 {
		t.Fatalf("after Idle and ActiveThreshold: %+v", p)
	}
}

// TestFieldsAs takes the fields by value or by pointer and refuses a nil
// pointer and another type as InvalidCommand.
func TestFieldsAs(t *testing.T) {
	t.Parallel()
	type req struct{ N int }
	if got, err := fieldsAs[req](req{N: 1}); err != nil || got.N != 1 {
		t.Fatalf("value: %+v, %v", got, err)
	}
	if got, err := fieldsAs[req](&req{N: 2}); err != nil || got.N != 2 {
		t.Fatalf("pointer: %+v, %v", got, err)
	}
	for _, fields := range []any{(*req)(nil), "other", nil} {
		_, err := fieldsAs[req](fields)
		var sc im.StatusCodeError
		if !errors.As(err, &sc) || sc.MatterStatusCode() != im.StatusInvalidCommand || err.Error() == "" {
			t.Errorf("fields %T: %v, want InvalidCommand", fields, err)
		}
	}
}

// TestServedQualities marks no Diagnostics cluster and no Fixed
// attribute of a cluster or attribute without a definition.
func TestServedQualities(t *testing.T) {
	t.Parallel()
	q := servedQualities{}
	if q.DiagnosticsCluster(wrdef.ClusterID) || q.AttributeFixed(0xFFF1FC00, 0) || q.AttributeFixed(wrdef.ClusterID, 0xFFF0) {
		t.Fatal("served qualities")
	}
}

// TestCameraPortFansOut: every requestor signal reaches each listener
// with its kind and payload.
func TestCameraPortFansOut(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	got := make(chan CameraSignal, 8)
	s := &served{listeners: map[uint64]func(CameraSignal){1: func(sig CameraSignal) { got <- sig }}}
	p := cameraPort{s: s}
	session := camera.WebRTCSession{Id: 7}
	p.Offer(ctx, session, wrdef.OfferRequest{Sdp: "offer"})
	p.Answer(ctx, session, "answer")
	p.IceCandidates(ctx, session, []camera.ICECandidate{{Candidate: "c"}})
	p.End(ctx, session, 3)
	want := []CameraSignalKind{CameraSignalOffer, CameraSignalAnswer, CameraSignalICECandidates, CameraSignalEnd}
	for _, kind := range want {
		sig := <-got
		if sig.Kind != kind || sig.Session.Id != 7 {
			t.Fatalf("signal %+v, want kind %v", sig, kind)
		}
		switch kind {
		case CameraSignalOffer:
			if sig.SDP != "offer" {
				t.Fatalf("offer SDP %q", sig.SDP)
			}
		case CameraSignalAnswer:
			if sig.SDP != "answer" {
				t.Fatalf("answer SDP %q", sig.SDP)
			}
		case CameraSignalICECandidates:
			if len(sig.Candidates) != 1 || sig.Candidates[0].Candidate != "c" {
				t.Fatalf("candidates %+v", sig.Candidates)
			}
		case CameraSignalEnd:
			if sig.Reason != 3 {
				t.Fatalf("reason %d", sig.Reason)
			}
		}
	}
}

// TestControllerAccessorsWithoutServices: without an OTA provider there
// is no BDX responder.
func TestControllerAccessorsWithoutServices(t *testing.T) {
	t.Parallel()
	c := &Controller{tcp: TCPConfig{}}
	if c.BDX() != nil || c.Groups() != nil || c.Clock() != nil || c.TCP() != (TCPConfig{}) {
		t.Fatal("accessors of a bare controller")
	}
}

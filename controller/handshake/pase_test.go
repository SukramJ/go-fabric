// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package handshake_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/controller/handshake"
	"github.com/SukramJ/go-fabric/transport/mrp"
)

// TestPASEEstablishesSession: PASE against a node in its commissioning
// window yields a secure session whose keys work both ways.
func TestPASEEstablishesSession(t *testing.T) {
	t.Parallel()
	_, addr := startNode(t)
	m, rec := newManager(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	called := false
	s, err := handshake.PASE(ctx, m, addr, testPasscode, handshake.PASEOptions{SuppressPeerLoss: true, OnPBKDFParamResponse: func() { called = true }})
	if err != nil {
		t.Fatalf("PASE: %v", err)
	}
	if !called {
		t.Error("OnPBKDFParamResponse not called")
	}
	if !s.IsSecure() || s.ID() == 0 || s.PeerSessionID() == 0 || s.PeerNodeID() != 0 || s.NodeID() != 0 || len(s.AttestationChallenge()) != 16 {
		t.Fatalf("PASE session: id %d peer %d nodes %d/%d challenge %d", s.ID(), s.PeerSessionID(), s.NodeID(), s.PeerNodeID(), len(s.AttestationChallenge()))
	}
	// The node's PBKDFParamResponse carries its session parameters
	// (bridge DefaultSessionParameters): they are the reported ones.
	if r := s.ReportedParameters(); r == nil || r.MaxPathsPerInvoke == 0 || s.Parameters().SessionIdleInterval == 0 {
		t.Fatalf("reported parameters %+v, parameters %+v", r, s.Parameters())
	}
	want := []uint8{mrp.SCOpcodePBKDFParamRequest, mrp.SCOpcodePake1, mrp.SCOpcodePake3}
	var got []uint8
	for _, op := range rec.opcodesSent() {
		if op != mrp.StandaloneAckOpcode {
			got = append(got, op)
		}
	}
	if !slices.Equal(got, want) {
		t.Fatalf("unsecured messages sent %x, want %x", got, want)
	}
	readNodeLabel(t, m, s)
	m.CloseSession(ctx, s)
}

// TestPASEWrongPasscode: a wrong passcode is ErrPasscodeMismatch, and the
// device is told with a StatusReport InvalidParam (PaseClient.ts:94-115);
// a PASE with the right passcode then succeeds.
func TestPASEWrongPasscode(t *testing.T) {
	t.Parallel()
	_, addr := startNode(t)
	m, rec := newManager(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := handshake.PASE(ctx, m, addr, testPasscode+1, handshake.PASEOptions{}); !errors.Is(err, handshake.ErrPasscodeMismatch) {
		t.Fatalf("PASE with a wrong passcode = %v, want ErrPasscodeMismatch", err)
	}
	if n := invalidParams(rec.unsecuredStatusReports()); n != 1 {
		t.Fatalf("%d InvalidParam reports sent, want 1", n)
	}
	s, err := handshake.PASE(ctx, m, addr, testPasscode, handshake.PASEOptions{})
	if err != nil {
		t.Fatalf("PASE after the mismatch: %v", err)
	}
	readNodeLabel(t, m, s)
}

// TestPASECancelledBeforePake3: a ctx cancelled once the PBKDF exchange
// is done ends the handshake before Pake1 is sent, and the device is
// still told with InvalidParam (PaseClient.ts:95-96: "for PASE we send
// InvalidParam even on abort").
func TestPASECancelledBeforePake3(t *testing.T) {
	t.Parallel()
	_, addr := startNode(t)
	m, rec := newManager(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, err := handshake.PASE(ctx, m, addr, testPasscode, handshake.PASEOptions{OnPBKDFParamResponse: cancel})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("PASE = %v, want context.Canceled", err)
	}
	if slices.Contains(rec.opcodesSent(), mrp.SCOpcodePake1) {
		t.Fatal("Pake1 sent after the cancellation")
	}
	if n := invalidParams(rec.unsecuredStatusReports()); n != 1 {
		t.Fatalf("%d InvalidParam reports sent, want 1", n)
	}
}

// TestPASECancelledAfterPake3: a ctx cancelled once Pake3 is on the wire
// does not end the handshake — the device has committed its state
// (PaseClient.ts:192-197) — and no InvalidParam is sent.
func TestPASECancelledAfterPake3(t *testing.T) {
	t.Parallel()
	_, addr := startNode(t)
	m, rec := newManager(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	rec.setHook(func(d []byte) {
		if op, _, ok := unsecuredMessage(d); ok && op == mrp.SCOpcodePake3 {
			cancel()
		}
	})
	s, err := handshake.PASE(ctx, m, addr, testPasscode, handshake.PASEOptions{})
	if err != nil {
		t.Fatalf("PASE = %v, want the session despite the cancellation", err)
	}
	if ctx.Err() == nil {
		t.Fatal("the hook did not cancel")
	}
	if n := invalidParams(rec.unsecuredStatusReports()); n != 0 {
		t.Fatalf("%d InvalidParam reports sent, want none", n)
	}
	readNodeLabel(t, m, s)
}

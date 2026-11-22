// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

import (
	"context"
	"log/slog"
	"sync"
	"testing"

	"thingfabric.dev/go-fabric/secure/operational"
	"thingfabric.dev/go-fabric/secure/sigma"
)

// establishingRig is a CASE adapter of the node's responder whose Sigma3
// was processed: its session is installed, its Success not yet sent.
type establishingRig struct {
	sessions *operational.Manager
	est      *caseEstablishment
	peer     uint64
	id       uint16

	mu     sync.Mutex
	opened []uint16
}

func newEstablishingRig(t *testing.T) *establishingRig {
	t.Helper()
	return newEstablishingRigLogged(t, slog.New(slog.DiscardHandler))
}

// newEstablishingRigLogged is newEstablishingRig with the responder logging
// to logger.
func newEstablishingRigLogged(t *testing.T, logger *slog.Logger) *establishingRig {
	t.Helper()
	fx := newCaseFabricFixture(t)
	r := &establishingRig{sessions: operational.NewManager(testStore(t)), peer: fx.peer.NodeID}
	fx.ids.testEstablishment = func(e *caseEstablishment) { r.est = e }
	adapter, err := buildCaseAdapter(r.sessions, fx.ids, nil, logger, func(id uint16) {
		r.mu.Lock()
		r.opened = append(r.opened, id)
		r.mu.Unlock()
	})
	if err != nil {
		t.Fatal(err)
	}
	initiator, err := sigma.NewPeerInitiator(sigma.InitiatorConfig{
		Identity: fx.peer, Verifier: fx.verifier, SessionID: 0x1001,
		PeerNodeID: fx.local.NodeID, RootPublicKey: fx.root,
	})
	if err != nil {
		t.Fatal(err)
	}
	sigma1, err := initiator.GenerateSigma1()
	if err != nil {
		t.Fatal(err)
	}
	_, sigma2, err := adapter.ProcessSigma1(sigma1)
	if err != nil {
		t.Fatalf("ProcessSigma1: %v", err)
	}
	sigma3, err := initiator.ProcessSigma2Bytes(sigma2)
	if err != nil {
		t.Fatalf("ProcessSigma2: %v", err)
	}
	if _, _, err := adapter.ProcessSigma3(sigma3); err != nil {
		t.Fatalf("ProcessSigma3: %v", err)
	}
	snap := adapter.SnapshotResponder()
	if snap == nil {
		t.Fatal("no responder")
	}
	r.id = snap.SessionID()
	return r
}

func (r *establishingRig) openedIDs() []uint16 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]uint16(nil), r.opened...)
}

// TestCASEResponderSessionEstablishingOnTheDevice: the node's CASE responder
// installs its session establishing (findings F-b25ed960-23): before the
// Success is sent it routes (Get) but no picker returns it, it is not
// listed and the host has not been told; the bridge's completion
// establishes it, its failure closes it. matter.js CaseServer.ts:314-344,
// SessionManager.ts:683-705, :1145-1149, :1266-1284 at b25ed960.
func TestCASEResponderSessionEstablishingOnTheDevice(t *testing.T) {
	t.Parallel()
	t.Run("no picker returns the session before its Success is sent", func(t *testing.T) {
		t.Parallel()
		r := newEstablishingRig(t)
		if _, err := r.sessions.Get(r.id); err != nil {
			t.Fatalf("Get(%#x): %v; the establishing session must route", r.id, err)
		}
		if id, ok := r.sessions.NewestSessionForPeer(1, r.peer, nil); ok {
			t.Fatalf("NewestSessionForPeer returned %#x before the Success was sent", id)
		}
		if ids := r.sessions.SessionIDsForPeer(1, r.peer); len(ids) != 0 {
			t.Fatalf("SessionIDsForPeer = %v before the Success was sent", ids)
		}
		if got := r.sessions.Sessions(); len(got) != 0 {
			t.Fatalf("Sessions() lists %+v before the Success was sent", got)
		}
		if got := r.openedIDs(); len(got) != 0 {
			t.Fatalf("the host was told of %v before the Success was sent", got)
		}
	})
	t.Run("the Success's first transmission establishes the session", func(t *testing.T) {
		t.Parallel()
		r := newEstablishingRig(t)
		if !r.est.complete(r.id, true) {
			t.Fatal("complete = false")
		}
		if id, ok := r.sessions.NewestSessionForPeer(1, r.peer, nil); !ok || id != r.id {
			t.Fatalf("NewestSessionForPeer = %#x, %v; want %#x", id, ok, r.id)
		}
		if got := r.openedIDs(); len(got) != 1 || got[0] != r.id {
			t.Fatalf("the host was told of %v, want [%#x]", got, r.id)
		}
	})
	t.Run("a Success that never went out closes the session", func(t *testing.T) {
		t.Parallel()
		r := newEstablishingRig(t)
		if r.est.complete(r.id, false) {
			t.Fatal("complete = true for a failed handshake")
		}
		if _, err := r.sessions.Get(r.id); err == nil {
			t.Fatal("the session whose Success never went out is still in the table")
		}
		if got := r.openedIDs(); len(got) != 0 {
			t.Fatalf("the host was told of %v", got)
		}
	})
	t.Run("a session closed while establishing is not established", func(t *testing.T) {
		t.Parallel()
		r := newEstablishingRig(t)
		r.sessions.CloseFabric(1)
		if r.est.complete(r.id, true) {
			t.Fatal("complete = true for a session its fabric's teardown closed")
		}
		if got := r.openedIDs(); len(got) != 0 {
			t.Fatalf("the host was told of %v", got)
		}
	})
}

// recordedLogs keeps the message and the level of every record logged.
type recordedLogs struct {
	mu      sync.Mutex
	records []slog.Record
}

func (l *recordedLogs) Enabled(context.Context, slog.Level) bool { return true }

func (l *recordedLogs) Handle(_ context.Context, r slog.Record) error {
	l.mu.Lock()
	l.records = append(l.records, r.Clone())
	l.mu.Unlock()
	return nil
}

func (l *recordedLogs) WithAttrs([]slog.Attr) slog.Handler { return l }

func (l *recordedLogs) WithGroup(string) slog.Handler { return l }

// at returns the levels of the records logged as msg, with the session id
// each names.
func (l *recordedLogs) at(msg string) (levels []slog.Level, ids []int64) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for i := range l.records {
		r := &l.records[i]
		if r.Message != msg {
			continue
		}
		levels = append(levels, r.Level)
		r.Attrs(func(a slog.Attr) bool {
			if a.Key == "session_id" {
				ids = append(ids, a.Value.Int64())
			}
			return true
		})
	}
	return levels, ids
}

// TestCASESessionEstablishedIsLoggedAtCompletion (owner decision 353):
// the responder's install of a session logs case.session_establishing at
// Debug, and case.session_established is logged once, at Info, when the
// bridge's completion establishes the session — not before; a failed
// handshake logs no establishment.
func TestCASESessionEstablishedIsLoggedAtCompletion(t *testing.T) {
	t.Parallel()
	t.Run("established at completion", func(t *testing.T) {
		t.Parallel()
		logs := &recordedLogs{}
		r := newEstablishingRigLogged(t, slog.New(logs))
		if levels, _ := logs.at("case.session_establishing"); len(levels) != 1 || levels[0] != slog.LevelDebug {
			t.Fatalf("install: case.session_establishing at %v, want once at DEBUG", levels)
		}
		if levels, _ := logs.at("case.session_established"); len(levels) != 0 {
			t.Fatalf("case.session_established logged %d times before the completion", len(levels))
		}
		if !r.est.complete(r.id, true) {
			t.Fatal("complete = false")
		}
		levels, ids := logs.at("case.session_established")
		if len(levels) != 1 || levels[0] != slog.LevelInfo || len(ids) != 1 || ids[0] != int64(r.id) {
			t.Fatalf("after the completion: case.session_established at %v for %v, want once at INFO for %#x", levels, ids, r.id)
		}
	})
	t.Run("a failed handshake is not established", func(t *testing.T) {
		t.Parallel()
		logs := &recordedLogs{}
		r := newEstablishingRigLogged(t, slog.New(logs))
		r.est.complete(r.id, false)
		if levels, _ := logs.at("case.session_established"); len(levels) != 0 {
			t.Fatalf("case.session_established logged for a failed handshake (%v)", levels)
		}
	})
}

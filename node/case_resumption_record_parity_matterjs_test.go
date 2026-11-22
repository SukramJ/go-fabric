// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

// The CASE responder's resumption record is saved for a session that
// became established, never for one that did not (F-b25ed960-37). Ports by
// meaning — the record half — of matter.js
// packages/node/test/node/ServerSubscriptionSessionTest.ts at b25ed960,
// :340-383, :423-464, :466-495 and :497-532; the bridge's report of the
// handshake's end is the caseEstablishment completion here. matter.js
// saves after the initiator's Success on a resume and after the Success's
// send on a full handshake, only for an established session
// (packages/protocol/src/session/case/CaseServer.ts:204-217, :329-357 at
// b25ed960).

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/SukramJ/go-fabric/bridge"
	"github.com/SukramJ/go-fabric/secure/operational"
	"github.com/SukramJ/go-fabric/secure/sigma"
	"github.com/SukramJ/go-fabric/store"
	"github.com/SukramJ/go-fabric/transport/mrp"
)

// failingUpserts is a store whose resumption upserts fail while fail is
// set.
type failingUpserts struct {
	*store.Store
	fail atomic.Bool
}

func (f *failingUpserts) UpsertResumption(ctx context.Context, rec store.ResumptionRecord) error {
	if f.fail.Load() {
		return errors.New("test: upsert refused")
	}
	return f.Store.UpsertResumption(ctx, rec)
}

// recordRig is the node's CASE responder over a store that holds the
// fixture's fabric; each handshake runs on its own adapter, as the
// per-exchange provider builds one.
type recordRig struct {
	fx       caseFabricFixture
	st       *failingUpserts
	sessions *operational.Manager

	mu  sync.Mutex
	est *caseEstablishment
}

func newRecordRig(t *testing.T) *recordRig {
	t.Helper()
	fx := newCaseFabricFixture(t)
	st := &failingUpserts{Store: fabricStore(t, fx)}
	r := &recordRig{fx: fx, st: st, sessions: operational.NewManager(st)}
	fx.ids.resumptions = st
	fx.ids.testEstablishment = func(e *caseEstablishment) {
		r.mu.Lock()
		r.est = e
		r.mu.Unlock()
	}
	return r
}

func (r *recordRig) adapter(t *testing.T) (*bridge.CaseAdapter, *caseEstablishment) {
	t.Helper()
	a, err := buildCaseAdapter(r.sessions, r.fx.ids, nil, slog.New(slog.DiscardHandler), nil)
	if err != nil {
		t.Fatal(err)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return a, r.est
}

// full runs a full handshake up to its Sigma3 and returns the installed
// session's id and the adapter's completion bookkeeping.
func (r *recordRig) full(t *testing.T) (uint16, *caseEstablishment) {
	t.Helper()
	a, est := r.adapter(t)
	initiator, err := sigma.NewPeerInitiator(sigma.InitiatorConfig{
		Identity: r.fx.peer, Verifier: r.fx.verifier, SessionID: 0x1001,
		PeerNodeID: r.fx.local.NodeID, RootPublicKey: r.fx.root,
	})
	if err != nil {
		t.Fatal(err)
	}
	sigma1, err := initiator.GenerateSigma1()
	if err != nil {
		t.Fatal(err)
	}
	_, sigma2, err := a.ProcessSigma1(sigma1)
	if err != nil {
		t.Fatalf("ProcessSigma1: %v", err)
	}
	sigma3, err := initiator.ProcessSigma2Bytes(sigma2)
	if err != nil {
		t.Fatalf("ProcessSigma2: %v", err)
	}
	if _, _, err := a.ProcessSigma3(sigma3); err != nil {
		t.Fatalf("ProcessSigma3: %v", err)
	}
	return a.SnapshotResponder().SessionID(), est
}

// resume presents rec in a Sigma1 on a new adapter. It returns the reply's
// opcode and, for a Sigma2_Resume, the installed session's id.
func (r *recordRig) resume(t *testing.T, rec store.ResumptionRecord) (uint8, uint16, *caseEstablishment) {
	t.Helper()
	a, est := r.adapter(t)
	initiator, err := sigma.NewPeerInitiator(sigma.InitiatorConfig{
		Identity: r.fx.peer, Verifier: r.fx.verifier, SessionID: 0x1002,
		PeerNodeID: r.fx.local.NodeID, RootPublicKey: r.fx.root,
		Resumption: &sigma.ResumptionRecord{
			SharedSecret: rec.SharedSecret, ResumptionID: rec.ResumptionID,
			FabricIndex: rec.FabricIndex, PeerNodeID: r.fx.local.NodeID, PeerCATs: rec.CASEAuthTags,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	sigma1, err := initiator.GenerateSigma1()
	if err != nil {
		t.Fatal(err)
	}
	op, _, err := a.ProcessSigma1(sigma1)
	if err != nil {
		t.Fatalf("ProcessSigma1 (resume): %v", err)
	}
	if op != mrp.SCOpcodeSigma2Resume {
		return op, 0, est
	}
	return op, a.SnapshotResponder().SessionID(), est
}

// record is the peer's stored resumption record; ok false without one.
func (r *recordRig) record(t *testing.T) (store.ResumptionRecord, bool) {
	t.Helper()
	rec, err := r.st.GetResumptionByPeer(context.Background(), 1, r.fx.peer.NodeID)
	if errors.Is(err, store.ErrResumptionNotFound) {
		return store.ResumptionRecord{}, false
	}
	if err != nil {
		t.Fatal(err)
	}
	return rec, true
}

// established runs a full handshake to its completion and returns the
// record it saved.
func (r *recordRig) established(t *testing.T) store.ResumptionRecord {
	t.Helper()
	id, est := r.full(t)
	if !est.complete(id, true) {
		t.Fatal("the full handshake's session is not established")
	}
	rec, ok := r.record(t)
	if !ok {
		t.Fatal("no record after an established full handshake")
	}
	return rec
}

func TestCASEResponderSavesTheRecordOfAnEstablishedSessionOnly(t *testing.T) {
	t.Parallel()
	t.Run("does not complete a resumption whose session closed during the handshake", func(t *testing.T) {
		t.Parallel()
		r := newRecordRig(t)
		r1 := r.established(t)
		op, id, est := r.resume(t, r1)
		if op != mrp.SCOpcodeSigma2Resume {
			t.Fatalf("resume answered with opcode %#x", op)
		}
		if err := r.sessions.Close(id); err != nil {
			t.Fatal(err)
		}
		if est.complete(id, true) {
			t.Fatal("a resumed session closed during its handshake was established")
		}
		if got, ok := r.record(t); !ok || !bytes.Equal(got.ResumptionID, r1.ResumptionID) {
			t.Fatalf("record %x, %v; want the former record %x kept", got.ResumptionID, ok, r1.ResumptionID)
		}
	})
	t.Run("keeps a new full CASE session whose SigmaFinished went out but was not acknowledged", func(t *testing.T) {
		t.Parallel()
		// The Success's first transmission establishes the session; the
		// record is saved whatever its acknowledgement does later.
		r := newRecordRig(t)
		id, est := r.full(t)
		if !est.complete(id, true) {
			t.Fatal("not established")
		}
		if _, ok := r.record(t); !ok {
			t.Fatal("no record for the established session")
		}
	})
	t.Run("keeps a new full CASE session and saves its record when SigmaFinished fails after it went out for another reason", func(t *testing.T) {
		t.Parallel()
		r := newRecordRig(t)
		id, est := r.full(t)
		if !est.complete(id, true) {
			t.Fatal("not established")
		}
		if _, err := r.sessions.Get(id); err != nil {
			t.Fatalf("the established session is gone: %v", err)
		}
		if _, ok := r.record(t); !ok {
			t.Fatal("no record for the established session")
		}
	})
	t.Run("does not complete a new full CASE session that closed before SigmaFinished went out", func(t *testing.T) {
		t.Parallel()
		r := newRecordRig(t)
		id, est := r.full(t)
		if err := r.sessions.Close(id); err != nil {
			t.Fatal(err)
		}
		if est.complete(id, true) {
			t.Fatal("a session closed before its Success went out was established")
		}
		if got, ok := r.record(t); ok {
			t.Fatalf("a record %x was saved for a session never established", got.ResumptionID)
		}
	})
}

// TestCASEResumeRecordReplacedOnlyOnceConfirmed: a resume replaces the
// presented record only once the initiator's Success established the
// session; one it does not confirm leaves the record — and its id free for
// the next resume. A record whose store fails keeps its id claimed.
func TestCASEResumeRecordReplacedOnlyOnceConfirmed(t *testing.T) {
	t.Parallel()
	t.Run("a resume the initiator does not confirm keeps the record", func(t *testing.T) {
		t.Parallel()
		r := newRecordRig(t)
		r1 := r.established(t)
		op, id, est := r.resume(t, r1)
		if op != mrp.SCOpcodeSigma2Resume {
			t.Fatalf("resume answered with opcode %#x", op)
		}
		if got, ok := r.record(t); !ok || !bytes.Equal(got.ResumptionID, r1.ResumptionID) {
			t.Fatalf("record %x, %v before the Success; want the former %x", got.ResumptionID, ok, r1.ResumptionID)
		}
		if est.complete(id, false) {
			t.Fatal("a failed resume was established")
		}
		if got, ok := r.record(t); !ok || !bytes.Equal(got.ResumptionID, r1.ResumptionID) {
			t.Fatalf("record %x, %v after the failure; want the former %x", got.ResumptionID, ok, r1.ResumptionID)
		}
		if op, _, _ := r.resume(t, r1); op != mrp.SCOpcodeSigma2Resume {
			t.Fatalf("the next resume with the kept record answered %#x; its id is still claimed", op)
		}
	})
	t.Run("a confirmed resume renews the id and keeps the parameters", func(t *testing.T) {
		t.Parallel()
		r := newRecordRig(t)
		r1 := r.established(t)
		_, id, est := r.resume(t, r1)
		if !est.complete(id, true) {
			t.Fatal("the confirmed resume was not established")
		}
		got, ok := r.record(t)
		if !ok || bytes.Equal(got.ResumptionID, r1.ResumptionID) {
			t.Fatalf("record %x, %v; want a renewed id", got.ResumptionID, ok)
		}
		if got.SessionParameters != r1.SessionParameters {
			t.Fatalf("parameters %+v; want the former record's %+v", got.SessionParameters, r1.SessionParameters)
		}
		if _, err := r.st.GetResumptionByID(context.Background(), r1.ResumptionID); err == nil {
			t.Fatal("the presented id still resolves")
		}
	})
	t.Run("a record that cannot be stored keeps its id claimed", func(t *testing.T) {
		t.Parallel()
		r := newRecordRig(t)
		r1 := r.established(t)
		_, id, est := r.resume(t, r1)
		r.st.fail.Store(true)
		if !est.complete(id, true) {
			t.Fatal("the confirmed resume was not established")
		}
		r.st.fail.Store(false)
		if op, _, _ := r.resume(t, r1); op == mrp.SCOpcodeSigma2Resume {
			t.Fatal("the presented id resumed again after its replacement failed to store")
		}
	})
}

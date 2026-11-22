// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

import (
	"context"
	"crypto/rand"
	"log/slog"
	"testing"

	"thingfabric.dev/go-fabric/secure/operational"
	"thingfabric.dev/go-fabric/secure/sigma"
	"thingfabric.dev/go-fabric/store"
)

// fabricStore is a store that holds the fixture's fabric 1, so a
// resumption record of it can be stored at all.
func fabricStore(t *testing.T, fx caseFabricFixture) *store.Store {
	t.Helper()
	st := testStore(t)
	idx, err := st.AddFabric(context.Background(), store.FabricRecord{FabricID: fx.local.FabricID, NodeID: fx.local.NodeID, RootPublicKey: fx.root})
	if err != nil || idx != 1 {
		t.Fatalf("AddFabric = %d, %v; want fabric 1", idx, err)
	}
	return st
}

// updateWithRecords is fx.updateIdentity with the update's removal of the
// fabric's resumption records (node.fabricUpdated: load, then
// RemoveResumptionsByFabric, then the sweep).
func (fx caseFabricFixture) updateWithRecords(t *testing.T, st store.Resumption, sessions *operational.Manager) {
	t.Helper()
	fx.updateIdentity(sessions)
	if err := st.RemoveResumptionsByFabric(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
}

// TestNoResumptionRecordOutlivesAnUpdateAfterTheAdmit (facade residual 3b,
// R2-P6-sec-2412): an UpdateNOC whose identity load and record removal both
// land after a handshake admitted its session and before the handshake
// stored its resumption record leaves no record made under the replaced
// identity — in either role, and for the responder whether the update
// lands before the session's establishment or after it, before the record
// (F-b25ed960-37). The session itself is the update's sweep's.
func TestNoResumptionRecordOutlivesAnUpdateAfterTheAdmit(t *testing.T) {
	t.Parallel()
	// responder runs a full handshake with update landing at the seam
	// install sets, then reports the Success sent.
	responder := func(t *testing.T, install func(fx caseFabricFixture, update func())) {
		t.Helper()
		fx := newCaseFabricFixture(t)
		st := fabricStore(t, fx)
		sessions := operational.NewManager(st)
		fx.ids.resumptions = st
		install(fx, func() { fx.updateWithRecords(t, st, sessions) })
		var est *caseEstablishment
		fx.ids.testEstablishment = func(e *caseEstablishment) { est = e }
		adapter, err := buildCaseAdapter(sessions, fx.ids, nil, slog.New(slog.DiscardHandler), nil)
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
		_, _, _ = adapter.ProcessSigma3(sigma3)
		if snap := adapter.SnapshotResponder(); snap != nil {
			est.complete(snap.SessionID(), true)
		}
		if rec, err := st.GetResumptionByPeer(context.Background(), 1, fx.peer.NodeID); err == nil {
			t.Fatalf("a resumption record made under the replaced identity outlived the update: %x", rec.ResumptionID)
		}
	}
	t.Run("responder, update before the establishment", func(t *testing.T) {
		t.Parallel()
		// After the admit, before the completion: the update's sweep
		// closes the establishing session, which saves nothing.
		responder(t, func(fx caseFabricFixture, update func()) { fx.ids.testAfterAdmit = update })
	})
	t.Run("responder, update between the establishment and the record", func(t *testing.T) {
		t.Parallel()
		// Established, the record not yet stored: the record's own check
		// removes it again ([caseIdentities.dropIfReplaced]).
		responder(t, func(fx caseFabricFixture, update func()) { fx.ids.testBeforeRecord = update })
	})
	t.Run("initiator", func(t *testing.T) {
		t.Parallel()
		fx := newCaseFabricFixture(t)
		st := fabricStore(t, fx)
		sessions := operational.NewManager(st)
		fx.ids.resumptions = st
		provider := caseInitiatorProvider(st, sessions, fx.ids, slog.New(slog.DiscardHandler), func(uint16) { fx.updateWithRecords(t, st, sessions) })
		init, err := provider(context.Background(), 1, fx.peer.NodeID)
		if err != nil {
			t.Fatal(err)
		}
		var keys sigma.SessionKeys
		_, _ = rand.Read(keys.I2RKey[:])
		_, _ = rand.Read(keys.R2IKey[:])
		id := make([]byte, 16)
		_, _ = rand.Read(id)
		secret := make([]byte, 32)
		_ = init.OnEstablished(context.Background(), sigma.InitiatorResult{PeerNodeID: fx.peer.NodeID, PeerSessionID: 0x2002, Keys: keys, ResumptionID: id, SharedSecret: secret})
		if rec, err := st.GetResumptionByPeer(context.Background(), 1, fx.peer.NodeID); err == nil {
			t.Fatalf("a resumption record made under the replaced identity outlived the update: %x", rec.ResumptionID)
		}
	})
}

// TestAResumptionRecordWithinOneIdentityStays: without an update the
// record is kept, and a record of a later handshake is never removed by an
// earlier one's check.
func TestAResumptionRecordWithinOneIdentityStays(t *testing.T) {
	t.Parallel()
	fx := newCaseFabricFixture(t)
	st := fabricStore(t, fx)
	sessions := operational.NewManager(st)
	fx.ids.resumptions = st
	ctx := context.Background()
	gen := fx.ids.byIdx[1].gen
	rec := func(b byte) store.ResumptionRecord {
		id := make([]byte, 16)
		id[0] = b
		return store.ResumptionRecord{FabricIndex: 1, PeerNodeID: fx.peer.NodeID, ResumptionID: id, SharedSecret: make([]byte, 32)}
	}
	if err := fx.ids.persistAdmitted(ctx, sessions, gen, rec(1)); err != nil {
		t.Fatal(err)
	}
	if got, err := st.GetResumptionByPeer(ctx, 1, fx.peer.NodeID); err != nil || got.ResumptionID[0] != 1 {
		t.Fatalf("the admitted record: %+v, %v", got, err)
	}
	// An update, then a later handshake's record under the new identity;
	// the earlier handshake's check finds a record that is not its own.
	fx.updateIdentity(sessions)
	if err := sessions.PersistResumptionRecord(ctx, rec(2)); err != nil {
		t.Fatal(err)
	}
	fx.ids.dropIfReplaced(ctx, gen, rec(1))
	if got, err := st.GetResumptionByPeer(ctx, 1, fx.peer.NodeID); err != nil || got.ResumptionID[0] != 2 {
		t.Fatalf("a later handshake's record was removed: %+v, %v", got, err)
	}
}

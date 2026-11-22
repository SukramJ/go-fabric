// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"thingfabric.dev/go-fabric/cluster/core"
	"thingfabric.dev/go-fabric/contract"
	"thingfabric.dev/go-fabric/endpoint"
	"thingfabric.dev/go-fabric/im"
	"thingfabric.dev/go-fabric/mdns"
	"thingfabric.dev/go-fabric/secure/operational"
	"thingfabric.dev/go-fabric/transport/message"
)

// sessionListingRegistry is a session registry that lists its sessions, as
// operational.Manager does, records what is closed and says when the
// first close came.
type sessionListingRegistry struct {
	fakeSessionRegistry
	listMu   sync.Mutex
	sessions []operational.SessionInfo
	// closedOne is closed by the first Close.
	closedOne  chan struct{}
	closedOnce sync.Once
}

func newSessionListingRegistry(sessions ...operational.SessionInfo) *sessionListingRegistry {
	return &sessionListingRegistry{sessions: sessions, closedOne: make(chan struct{})}
}

func (r *sessionListingRegistry) Close(sessionID uint16) error {
	err := r.fakeSessionRegistry.Close(sessionID)
	r.closedOnce.Do(func() { close(r.closedOne) })
	return err
}

// awaitClose returns once the registry closed a session; a registry that
// closes none holds it for five seconds of the test's context, and the
// caller's own comparison of what was closed then fails.
func (r *sessionListingRegistry) awaitClose(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	select {
	case <-r.closedOne:
	case <-ctx.Done():
	}
}

func (r *sessionListingRegistry) Sessions() []operational.SessionInfo {
	r.listMu.Lock()
	defer r.listMu.Unlock()
	return slices.Clone(r.sessions)
}

func (r *sessionListingRegistry) closedIDs() []uint16 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.closed)
}

// caseOnFabricLookup resolves one session as a CASE session of a fabric.
type caseOnFabricLookup struct {
	SessionLookup
	id     uint16
	fabric uint8
}

func (l caseOnFabricLookup) FabricFor(id uint16) (uint8, bool) {
	if id != l.id {
		return 0, false
	}
	return l.fabric, true
}

func (l caseOnFabricLookup) SubjectFor(id uint16) (nodeID uint64, cats []uint32, ok bool) {
	return 0x1234, nil, id == l.id
}

func (l caseOnFabricLookup) IsPASE(id uint16) (pase, ok bool) { return false, id == l.id }

// TestCommissioningCompleteThroughTheBridgeClosesThePASESessions: a host
// that wires the bridge and the root clusters itself — no node facade —
// has the PASE sessions cleared by a successful CommissioningComplete,
// before its response goes out, Matter §11.10.6.6 step 4. Mirrors matter.js
// packages/protocol/src/common/FailsafeContext.ts:138-160 at b25ed960
// (completeCommission; step 4 at :156-157: "The Secure Session Context of any PASE session
// still established at the Server SHALL be cleared" → closePaseSession).
// The CASE session the command arrived on stays, and the close is logged
// with the PASE session's id (matter.session.pase_close, the line the
// Go-TH leg's TC-DD-3.5 step 15 reads).
func TestCommissioningCompleteThroughTheBridgeClosesThePASESessions(t *testing.T) {
	t.Parallel()
	const (
		caseSession uint16 = 91
		paseSession uint16 = 77
		exchangeID  uint16 = 0x3091
	)
	var logs syncBuffer
	b, err := New(wbEmptySnapshotter, mdns.NewNoop(), Config{Listen: "127.0.0.1:0"},
		slog.New(slog.NewTextHandler(&logs, nil)))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	startCtx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if err := b.Start(startCtx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer stopCancel()
		_ = b.Stop(stopCtx)
	})
	b.AttachACLLister(endpoint.UnenforcedACL{})
	gc, err := core.NewGeneralCommissioning(core.GeneralCommissioningConfig{
		LocationCapability: core.RegulatoryIndoor, FailSafeMaxSeconds: 600,
	})
	if err != nil {
		t.Fatal(err)
	}
	b.AttachRootClusters([]contract.ClusterServer{gc})
	reg := newSessionListingRegistry(
		operational.SessionInfo{SessionID: paseSession, FabricIndex: 1, IsPASE: true},
		operational.SessionInfo{SessionID: caseSession, FabricIndex: 1},
	)
	b.AttachSessionRegistry(reg)
	peerConn, peerSess, peerAddr := chunkTestPeer(t, b, caseSession)
	b.mu.RLock()
	inner := b.sessions
	b.mu.RUnlock()
	b.AttachSessionLookup(caseOnFabricLookup{SessionLookup: inner, id: caseSession, fabric: 1})

	// The fail-safe of the commissioning AddNOC moved onto fabric 1.
	if err := gc.ArmFailSafeFor(context.Background(), 60, 1); err != nil {
		t.Fatalf("ArmFailSafeFor: %v", err)
	}
	request := encryptIMFromPeer(t, peerSess, caseSession, message.ProtocolHeader{
		Opcode: im.OpcodeInvokeRequest, ExchangeID: exchangeID, Initiator: true, NeedsAck: true,
	}, encodeInvokeRequestSuppress(t, false, im.ConcreteCommandPath{
		Endpoint: 0, Cluster: 0x0030, Command: 0x04, HasEndpoint: true,
	}))
	if err := b.dispatch(context.Background(), request, peerAddr); err != nil {
		t.Fatalf("dispatch of CommissioningComplete: %v", err)
	}
	reply, _, ok := readOneIMDatagram(t, peerConn, peerSess, 5*time.Second)
	if !ok || reply.Opcode != im.OpcodeInvokeResponse {
		t.Fatalf("reply = (%v, opcode %#x), want the InvokeResponse", ok, reply.Opcode)
	}
	if gc.FailSafeArmed() {
		t.Fatal("the fail-safe is still armed: CommissioningComplete did not succeed, the test checks nothing")
	}
	// The close precedes the response: it is done once the reply is here.
	if got := reg.closedIDs(); !slices.Equal(got, []uint16{paseSession}) {
		t.Fatalf("sessions closed after CommissioningComplete = %v, want the PASE session [%d] alone", got, paseSession)
	}
	// The line is written before the first close, so it is there now.
	found := false
	for line := range strings.Lines(logs.String()) {
		if strings.Contains(line, "msg=matter.session.pase_close ") &&
			strings.Contains(line, " reason=commissioning_complete ") && strings.HasSuffix(line, " ids=77\n") {
			found = true
		}
	}
	if !found {
		t.Fatalf("the bridge's log holds no matter.session.pase_close line naming session 77:\n%s", logs.String())
	}
}

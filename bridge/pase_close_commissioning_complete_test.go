// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/secure/operational"
	"github.com/SukramJ/go-fabric/transport/message"
)

// sessionListingRegistry is a session registry that lists its sessions, as
// operational.Manager does, and records what is closed.
type sessionListingRegistry struct {
	fakeSessionRegistry
	listMu   sync.Mutex
	sessions []operational.SessionInfo
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
// has the PASE sessions cleared after a successful CommissioningComplete,
// Matter §11.10.6.6 step 4. Mirrors matter.js
// packages/protocol/src/common/FailsafeContext.ts:152-153 at db021da0
// (completeCommission: "The Secure Session Context of any PASE session
// still established at the Server SHALL be cleared" → closePaseSession).
// The CASE session the command arrived on stays.
func TestCommissioningCompleteThroughTheBridgeClosesThePASESessions(t *testing.T) {
	t.Parallel()
	const (
		caseSession uint16 = 91
		paseSession uint16 = 77
		exchangeID  uint16 = 0x3091
	)
	b := newStartedBridge(t)
	b.AttachACLLister(endpoint.UnenforcedACL{})
	gc, err := core.NewGeneralCommissioning(core.GeneralCommissioningConfig{
		LocationCapability: core.RegulatoryIndoor, FailSafeMaxSeconds: 600,
	})
	if err != nil {
		t.Fatal(err)
	}
	b.AttachRootClusters([]contract.ClusterServer{gc})
	reg := &sessionListingRegistry{sessions: []operational.SessionInfo{
		{SessionID: paseSession, FabricIndex: 1, IsPASE: true},
		{SessionID: caseSession, FabricIndex: 1},
	}}
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
	deadline := time.Now().Add(5 * time.Second)
	for len(reg.closedIDs()) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := reg.closedIDs(); !slices.Equal(got, []uint16{paseSession}) {
		t.Fatalf("sessions closed after CommissioningComplete = %v, want the PASE session [%d] alone", got, paseSession)
	}
}

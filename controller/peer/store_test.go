// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package peer

import (
	"net"
	"testing"

	"github.com/SukramJ/go-fabric/controller/discovery"
	"github.com/SukramJ/go-fabric/controller/handshake"
	"github.com/SukramJ/go-fabric/secure/sessionparams"
)

func TestMemoryStore(t *testing.T) {
	t.Parallel()
	var s MemoryStore
	sii := discovery.TXTData{}
	d := Descriptor{
		Address:                   handshake.PeerAddress{FabricIndex: 1, NodeID: 9},
		DataModelRevision:         17,
		OperationalAddress:        &net.UDPAddr{IP: net.ParseIP("fd00::1"), Port: 5540},
		DiscoveryData:             &sii,
		ReportedSessionParameters: &sessionparams.Parameters{SessionIdleInterval: 800},
		CaseAuthenticatedTags:     []uint32{0x0001_0001},
	}
	for _, e := range []Descriptor{d, {Address: handshake.PeerAddress{FabricIndex: 1, NodeID: 3}}, {Address: handshake.PeerAddress{FabricIndex: 2, NodeID: 1}}} {
		if err := s.SavePeer(e); err != nil {
			t.Fatal(err)
		}
	}
	d.OperationalAddress.Port = 1 // the store kept a copy
	got, err := s.LoadPeers(1)
	if err != nil || len(got) != 2 || got[0].Address.NodeID != 3 || got[1].OperationalAddress.Port != 5540 || got[1].CaseAuthenticatedTags[0] != 0x0001_0001 {
		t.Fatalf("LoadPeers = %+v, %v", got, err)
	}
	if err := s.DeletePeer(d.Address); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.LoadPeers(1); len(got) != 1 || s.Saves() != 3 {
		t.Fatalf("after delete %d peers, %d saves", len(got), s.Saves())
	}
}

func TestMemoryResumptionStore(t *testing.T) {
	t.Parallel()
	var s MemoryResumptionStore
	addr := handshake.PeerAddress{FabricIndex: 1, NodeID: 9}
	if ok, _ := s.DeleteResumptionRecord(addr); ok {
		t.Fatal("deleted a record that does not exist")
	}
	if err := s.SaveResumptionRecord(handshake.ResumptionRecord{Peer: addr}); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.FindResumptionRecord(addr); !ok {
		t.Fatal("saved record not found")
	}
	if ok, _ := s.DeleteResumptionRecord(addr); !ok {
		t.Fatal("record not deleted")
	}
	if _, ok := s.FindResumptionRecord(addr); ok {
		t.Fatal("deleted record found")
	}
}

// The in-memory stores implement the interfaces a Set takes.
var (
	_ Store           = (*MemoryStore)(nil)
	_ ResumptionStore = (*MemoryResumptionStore)(nil)
)

func TestStoresImplementTheInterfaces(t *testing.T) {
	t.Parallel()
	var s Store = &MemoryStore{}
	var r ResumptionStore = &MemoryResumptionStore{}
	if _, err := s.LoadPeers(1); err != nil {
		t.Fatal(err)
	}
	if ok, err := r.DeleteResumptionRecord(handshake.PeerAddress{}); ok || err != nil {
		t.Fatal("empty store deleted a record")
	}
}

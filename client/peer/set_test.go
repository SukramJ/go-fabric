// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package peer

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/client/discovery"
	"github.com/SukramJ/go-fabric/client/exchange"
	"github.com/SukramJ/go-fabric/client/handshake"
	"github.com/SukramJ/go-fabric/secure/sessionparams"
)

func dur(d time.Duration) *time.Duration { return &d }
func str(s string) *string               { return &s }

func TestNewSetNeedsItsCollaborators(t *testing.T) {
	t.Parallel()
	if _, err := NewSet(Context{}); err == nil {
		t.Fatal("NewSet accepted an empty context")
	}
	h := newHarness(t, func(c *Context) { c.Timing = &Timing{DelayBeforeNextAddress: time.Second} })
	if got := h.set.Timing(); got.DelayBeforeNextAddress != time.Second || got.DelayAfterPeerError != 5*time.Minute {
		t.Fatalf("Timing = %+v", got)
	}
	h.set.SetTiming(Timing{DelayAfterPeerError: time.Minute})
	if got := h.set.Timing(); got.DelayBeforeNextAddress != 45*time.Second || got.DelayAfterPeerError != time.Minute {
		t.Fatalf("SetTiming over the defaults = %+v", got)
	}
	h.set.SetHandleError(func(error) (time.Duration, bool, error) { return 0, false, nil })
	if h.set.errorHandler() == nil {
		t.Fatal("SetHandleError lost the handler")
	}
}

// TestSetForGetAndUnknownFabric: For creates once and returns the same
// peer after; an unknown fabric index is refused (PeerSet.ts:159-175).
func TestSetForGetAndUnknownFabric(t *testing.T) {
	t.Parallel()
	h := newHarness(t, nil)
	p, err := h.set.For(testAddr)
	if err != nil {
		t.Fatal(err)
	}
	if again, _ := h.set.For(testAddr); again != p || h.set.Get(testAddr) != p || !h.set.Has(testAddr) || h.set.Size() != 1 || h.set.Peers()[0] != p {
		t.Fatal("For did not return the known peer")
	}
	if p.Address() != testAddr || p.Service() == nil {
		t.Fatal("accessors")
	}
	if _, err := h.set.For(handshake.PeerAddress{FabricIndex: 9, NodeID: 1}); err == nil {
		t.Fatal("For accepted an unknown fabric")
	}
	if h.set.Get(handshake.PeerAddress{FabricIndex: 1, NodeID: 2}) != nil {
		t.Fatal("Get found an unknown peer")
	}
}

// TestAddKnownPeerMergeAndCoalescedSave: the merge table of
// PeerSet.ts:315-337 — operational address and CATs replace, discovery
// data fills what the peer lacks, reported parameters only when unknown
// — and one save per burst of changes (Peer.ts:99-106).
func TestAddKnownPeerMergeAndCoalescedSave(t *testing.T) {
	t.Parallel()
	var updates atomic.Int32
	h := newHarness(t, func(c *Context) { c.Updated = func(*Peer) { updates.Add(1) } })
	first := Descriptor{
		Address:                   testAddr,
		OperationalAddress:        udpAddr("[fd00::1]:5540"),
		DiscoveryData:             &discovery.TXTData{SII: dur(800 * time.Millisecond), DN: str("old")},
		ReportedSessionParameters: &sessionparams.Parameters{SessionIdleInterval: 900},
		CaseAuthenticatedTags:     []uint32{1},
	}
	p, err := h.set.AddKnownPeer(first)
	if err != nil {
		t.Fatal(err)
	}
	if !p.Service().Reachable() {
		t.Fatal("a known operational address does not make the service reachable (Peer.ts:119-122)")
	}
	before := h.store.Saves()
	if _, err := h.set.AddKnownPeer(Descriptor{
		Address:                   testAddr,
		OperationalAddress:        udpAddr("[fd00::2]:5540"),
		DiscoveryData:             &discovery.TXTData{SII: dur(time.Second), SAI: dur(400 * time.Millisecond)},
		ReportedSessionParameters: &sessionparams.Parameters{SessionIdleInterval: 100},
		CaseAuthenticatedTags:     []uint32{2},
	}); err != nil {
		t.Fatal(err)
	}
	d := p.Descriptor()
	if d.OperationalAddress.String() != "[fd00::2]:5540" || d.CaseAuthenticatedTags[0] != 2 ||
		*d.DiscoveryData.SII != 800*time.Millisecond || *d.DiscoveryData.SAI != 400*time.Millisecond || *d.DiscoveryData.DN != "old" ||
		d.ReportedSessionParameters.SessionIdleInterval != 900 {
		t.Fatalf("merged descriptor %+v / %+v", d, *d.DiscoveryData)
	}
	eventually(t, "the coalesced save", func() bool { return h.store.Saves() == before+1 && updates.Load() >= 1 })
	time.Sleep(10 * time.Millisecond)
	if n := h.store.Saves(); n != before+1 {
		t.Fatalf("%d saves for one burst, want 1", n-before)
	}
	// A descriptor that changes nothing saves nothing.
	if _, err := h.set.AddKnownPeer(Descriptor{Address: testAddr, OperationalAddress: udpAddr("[fd00::2]:5540"), CaseAuthenticatedTags: []uint32{2}}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(10 * time.Millisecond)
	if n := h.store.Saves(); n != before+1 {
		t.Fatal("an unchanged descriptor was saved")
	}
	stored, _ := h.store.LoadPeers(1)
	if len(stored) != 1 || stored[0].OperationalAddress.String() != "[fd00::2]:5540" {
		t.Fatalf("stored %+v", stored)
	}
}

// TestServiceChangeUpdatesDiscoveryData: the TXT record's keys overlay the
// discovery data, and a record without SII clears the one an earlier
// record stated (Peer.ts:126-135).
func TestServiceChangeUpdatesDiscoveryData(t *testing.T) {
	t.Parallel()
	h := newHarness(t, nil)
	p, _ := h.set.AddKnownPeer(Descriptor{Address: testAddr, DiscoveryData: &discovery.TXTData{DN: str("kept"), SAT: dur(time.Second)}})
	h.mdns.announce(t, srvRR(h.qname, h.hostQ, 5540, 120), txtRR(h.qname, 120, "SII=800", "SAI=300"), aaaaRR(h.hostQ, "fd00::7", 120))
	eventually(t, "the TXT record", func() bool {
		dd := p.Descriptor().DiscoveryData
		return dd != nil && dd.SII != nil && *dd.SII == 800*time.Millisecond
	})
	dd := p.Descriptor().DiscoveryData
	if dd.SAT != nil || *dd.DN != "kept" || *dd.SAI != 300*time.Millisecond {
		t.Fatalf("discovery data %+v", dd)
	}
	sp := p.SessionParameters()
	if sp.SessionIdleInterval != 800 || sp.SessionActiveInterval != 300 || sp.SessionActiveThreshold != exchange.FallbackActiveThreshold {
		t.Fatalf("SessionParameters from TXT = %+v", sp)
	}
	h.mdns.announce(t, txtRR(h.qname, 0, "SII=800", "SAI=300"), txtRR(h.qname, 120, "SAI=500"))
	eventually(t, "the second TXT record", func() bool {
		dd := p.Descriptor().DiscoveryData
		return dd.SII == nil && dd.SAI != nil && *dd.SAI == 500*time.Millisecond
	})
}

// TestAdoptSessions: a CASE session of our fabric becomes its peer's —
// operational address and reported parameters follow it, NewestSession is
// the one last heard from, and a closing session leaves the peer, the last
// firing Disconnected (PeerSet.ts:95-125, Peer.ts:141-187, :571-601).
func TestAdoptSessions(t *testing.T) {
	t.Parallel()
	var disconnected atomic.Int32
	h := newHarness(t, func(c *Context) { c.Disconnected = func(*Peer) { disconnected.Add(1) } })
	if h.set.Adopt(nil) != nil {
		t.Fatal("adopted nil")
	}
	unsec, _ := h.mgr.NewUnsecuredSession(udpAddr("[fd00::9]:5540"), nil)
	if h.set.Adopt(unsec) != nil {
		t.Fatal("adopted an unsecured session")
	}
	s1 := h.secureSession(t, 0x10, udpAddr("[fd00::1]:5540"))
	p := h.set.Adopt(s1)
	if p == nil || h.set.Get(testAddr) != p || p.Descriptor().OperationalAddress.String() != "[fd00::1]:5540" {
		t.Fatal("Adopt did not create the peer at the session's address")
	}
	h.clock.Advance(time.Second)
	s2 := h.secureSession(t, 0x11, udpAddr("[fd00::2]:5540"))
	h.set.Adopt(s2)
	if h.set.Adopt(s2) != p || len(p.Sessions()) != 2 {
		t.Fatalf("second session: %d sessions", len(p.Sessions()))
	}
	if p.NewestSession() != s2 || !p.HasSession() || p.Descriptor().OperationalAddress.String() != "[fd00::2]:5540" {
		t.Fatal("NewestSession is not the session last heard from")
	}
	s2.HandlePeerLoss(errors.New("gone"))
	if p.NewestSession() != s1 {
		t.Fatal("a lost session is still the newest")
	}
	eventually(t, "the lost session to leave", func() bool { return len(p.Sessions()) == 1 })
	if disconnected.Load() != 0 {
		t.Fatal("Disconnected with a session left")
	}
	s1.Close()
	eventually(t, "Disconnected", func() bool { return disconnected.Load() == 1 && !p.HasSession() })
}

// TestSessionParametersPreferTheReport: reported values win over the TXT
// record, which fills only what the report leaves out (Peer.ts:290-303).
func TestSessionParametersPreferTheReport(t *testing.T) {
	t.Parallel()
	h := newHarness(t, nil)
	p, _ := h.set.AddKnownPeer(Descriptor{
		Address:                   testAddr,
		DiscoveryData:             &discovery.TXTData{SII: dur(800 * time.Millisecond), SAI: dur(400 * time.Millisecond), SAT: dur(2 * time.Second)},
		ReportedSessionParameters: &sessionparams.Parameters{SessionIdleInterval: 1000, DataModelRevision: 19},
	})
	got := p.SessionParameters()
	if got.SessionIdleInterval != 1000 || got.SessionActiveInterval != 400 || got.SessionActiveThreshold != 2000 || got.DataModelRevision != 19 {
		t.Fatalf("SessionParameters = %+v", got)
	}
}

// TestCloseDeleteAndDisconnect: Close sends CloseSession for each session
// and leaves the set (Peer.ts:543-565); Delete forgets the resumption
// record and the stored descriptor (:529-541); Disconnect with a cause
// gives the sessions up without a word (:504-527).
func TestCloseDeleteAndDisconnect(t *testing.T) {
	t.Parallel()
	h := newHarness(t, nil)
	s := h.secureSession(t, 0x20, udpAddr("[fd00::1]:5540"))
	p := h.set.Adopt(s)
	_ = h.resume.SaveResumptionRecord(handshake.ResumptionRecord{Peer: testAddr})
	_ = h.store.SavePeer(p.Descriptor())
	h.udp.drain()
	p.Delete(context.Background())
	if d := h.udp.next(t); d.to.String() != "[fd00::1]:5540" {
		t.Fatalf("CloseSession went to %v", d.to)
	}
	if !s.IsClosed() || h.set.Has(testAddr) {
		t.Fatal("Close left the session or the peer")
	}
	if _, ok := h.resume.FindResumptionRecord(testAddr); ok {
		t.Fatal("Delete kept the resumption record")
	}
	if got, _ := h.store.LoadPeers(1); len(got) != 0 {
		t.Fatal("Delete kept the stored descriptor")
	}
	p.Close(context.Background()) // idempotent

	q, _ := h.set.For(testAddr)
	s2 := h.secureSession(t, 0x21, udpAddr("[fd00::1]:5540"))
	h.set.Adopt(s2)
	q.Disconnect(context.Background(), nil)
	if s2.IsPeerLost() {
		t.Fatal("Disconnect without a cause touched the session")
	}
	h.udp.drain()
	h.set.Disconnect(context.Background())
	q.Disconnect(context.Background(), errors.New("removed"))
	if !s2.IsPeerLost() || !s2.IsClosed() {
		t.Fatal("Disconnect with a cause kept the session")
	}
	h.udp.none(t)
	if !h.set.isDisconnected() {
		t.Fatal("Set.Disconnect")
	}
}

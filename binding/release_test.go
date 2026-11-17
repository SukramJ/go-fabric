// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package binding

import (
	"context"
	"log/slog"
	"net"
	"runtime"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/client/discovery"
	"github.com/SukramJ/go-fabric/client/exchange"
	"github.com/SukramJ/go-fabric/client/handshake"
	"github.com/SukramJ/go-fabric/client/peer"
	"github.com/SukramJ/go-fabric/mdns"
	"github.com/SukramJ/go-fabric/secure/sigma"
)

// TestPeerCreatedForAnEntryIsReleasedWithItsLastEntry: a peer the manager
// created for a client entry is released — its connection process
// stopped, the peer left in the set (owner decision 221) — once the last
// entry naming that node goes; a peer the set already held is left alone.
// matter.js keeps the peer it created (BindingManager.ts:584-585 with
// :613-627 at db021da0); the rule is this module's (review II,
// R2-P6-bound-2104, TM R3.4).
func TestPeerCreatedForAnEntryIsReleasedWithItsLastEntry(t *testing.T) {
	t.Parallel()
	h := newHarness(t, nil)
	h.listen()
	e1 := Entry{FabricIndex: 1, Node: 2, Endpoint: 1, Cluster: 0x0006}
	e2 := Entry{FabricIndex: 1, Node: 2, Endpoint: 3, Cluster: 0x0006}
	h.m.Register(1, e1)
	h.m.Register(1, e2)
	h.flush()
	p, _ := h.peers.For(peerAddr(1, 2))
	waitFor(t, func() bool { return h.link.connected(p) == 2 })
	h.m.Unregister(1, e1)
	h.flush()
	if n := h.link.released(p); n != 0 {
		t.Fatalf("released %d times while an entry still names the node", n)
	}
	if h.link.connectsEnded(p) {
		t.Fatal("the Connect calls ended while an entry still names the node")
	}
	h.m.Unregister(1, e2)
	h.flush()
	waitFor(t, func() bool { return h.link.released(p) == 1 })
	// The entries' Connect calls end with the release: one still running
	// would start the stopped process again.
	if !h.link.connectsEnded(p) {
		t.Fatal("an entry's Connect call outlived the release")
	}

	// A peer the set held before the entry is not the manager's to release.
	known, _ := h.peers.For(peerAddr(1, 3))
	e3 := Entry{FabricIndex: 1, Node: 3, Endpoint: 1, Cluster: 0x0006}
	h.m.Register(1, e3)
	h.flush()
	h.m.Unregister(1, e3)
	h.flush()
	if n := h.link.released(known); n != 0 {
		t.Fatalf("a peer the set already held was released %d times", n)
	}
}

// dropConn is an exchange socket that sends nowhere.
type dropConn struct{}

func (dropConn) Send(*net.UDPAddr, []byte) error { return nil }

// quietMDNS is an mDNS socket that sends nowhere and receives nothing;
// sent receives once a query went out.
type quietMDNS struct {
	out  chan mdns.QueryResponse
	sent chan struct{}
}

func (q quietMDNS) Send([]byte) error {
	select {
	case q.sent <- struct{}{}:
	default:
	}
	return nil
}
func (q quietMDNS) Responses() <-chan mdns.QueryResponse { return q.out }
func (quietMDNS) Close() error                           { return nil }

// TestReleaseStopsConnectingOnly: releasing a peer over client/peer — the
// entry's Connect call ended, then the peer's Disconnect — stops its
// connection process and nothing else: the peer stays in the set, for
// another holder may use it (owner decision 221, R2-P6-bound-2104).
func TestReleaseStopsConnectingOnly(t *testing.T) {
	t.Parallel()
	mgr, err := exchange.NewManager(dropConn{}, exchange.Config{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(mgr.Close)
	quiet := quietMDNS{out: make(chan mdns.QueryResponse), sent: make(chan struct{}, 1)}
	names := discovery.NewNames(quiet, slog.New(slog.DiscardHandler))
	t.Cleanup(func() { _ = names.Close() })
	set, err := peer.NewSet(peer.Context{
		Exchanges: mgr, Names: names,
		Fabrics: func(handshake.PeerAddress) (handshake.Fabric, bool) {
			return handshake.Fabric{Identity: &sigma.Identity{}}, true
		},
		Logger: slog.New(slog.DiscardHandler),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { set.Close(context.Background()) })
	addr := peerAddr(1, 2)
	p, err := set.For(addr)
	if err != nil {
		t.Fatal(err)
	}
	// The manager's Connect call, on the context it ends at the release.
	ctx, cancel := context.WithCancel(t.Context())
	connected := make(chan error, 1)
	go func() {
		_, err := p.Connect(ctx, peer.ConnectOptions{})
		connected <- err
	}()
	// The connection process runs once it asks DNS-SD for the node.
	select {
	case <-quiet.sent:
	case <-time.After(10 * time.Second):
		t.Fatal("the connection process never queried DNS-SD")
	}
	cancel()
	realLink{}.release(context.Background(), p)
	select {
	case err := <-connected:
		if err == nil {
			t.Fatal("Connect succeeded against a peer that answers nothing")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the release did not stop the connection process")
	}
	if got := set.Get(addr); got != p {
		t.Fatalf("the set holds %p for the node after the release, want the same peer %p", got, p)
	}
}

// TestPeerCreatedIsReleasedEveryBindingPeriod: the manager's record of a
// peer it created outlives the release — the peer stays in the set — so a
// later entry for the node opens a new binding period, released again
// with its last entry (review II, R2-P6-bound-2104, Fable F1).
func TestPeerCreatedIsReleasedEveryBindingPeriod(t *testing.T) {
	t.Parallel()
	h := newHarness(t, nil)
	h.listen()
	e := Entry{FabricIndex: 1, Node: 2, Endpoint: 1, Cluster: 0x0006}
	h.m.Register(1, e)
	h.flush()
	p, _ := h.peers.For(peerAddr(1, 2))
	waitFor(t, func() bool { return h.link.connected(p) == 1 })
	h.m.Unregister(1, e)
	h.flush()
	waitFor(t, func() bool { return h.link.released(p) == 1 })
	again := Entry{FabricIndex: 1, Node: 2, Endpoint: 4, Cluster: 0x0006}
	h.m.Register(1, again)
	h.flush()
	waitFor(t, func() bool { return h.link.connected(p) == 2 })
	h.m.Unregister(1, again)
	h.flush()
	waitFor(t, func() bool { return h.link.released(p) == 2 })
	if !h.link.connectsEnded(p) {
		t.Fatal("the second period's Connect call outlived its release")
	}
}

// TestReleaseWaitsForTheEntriesConnectCalls: the release stops the
// connection process only once the entries' Connect calls have returned,
// so none of them starts it again after the stop (review II,
// R2-P6-bound-2104, Fable F2).
func TestReleaseWaitsForTheEntriesConnectCalls(t *testing.T) {
	t.Parallel()
	h := newHarness(t, nil)
	held := make(chan struct{})
	release := sync.OnceFunc(func() { close(held) })
	t.Cleanup(release) // before the manager's Close, which waits for the call
	h.link.block, h.link.held = true, held
	h.listen()
	e := Entry{FabricIndex: 1, Node: 2, Endpoint: 1, Cluster: 0x0006}
	h.m.Register(1, e)
	h.flush()
	p, _ := h.peers.For(peerAddr(1, 2))
	waitFor(t, func() bool { return h.link.connected(p) == 1 })
	h.m.Unregister(1, e)
	h.flush()
	// The Connect call has seen the cancel but not returned yet: no release.
	for range 100 {
		runtime.Gosched()
	}
	if n := h.link.released(p); n != 0 {
		t.Fatalf("released %d times before the entry's Connect call returned", n)
	}
	release()
	waitFor(t, func() bool { return h.link.released(p) == 1 })
	h.link.mu.Lock()
	defer h.link.mu.Unlock()
	if want := []string{"connect returned", "release"}; !slices.Equal(h.link.events, want) {
		t.Fatalf("events %v, want %v", h.link.events, want)
	}
}

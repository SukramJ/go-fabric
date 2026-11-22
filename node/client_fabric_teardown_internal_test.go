// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

import (
	"context"
	"log/slog"
	"net"
	"net/netip"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"thingfabric.dev/go-fabric/client/discovery"
	"thingfabric.dev/go-fabric/client/exchange"
	"thingfabric.dev/go-fabric/client/handshake"
	"thingfabric.dev/go-fabric/client/peer"
	"thingfabric.dev/go-fabric/mdns"
	"thingfabric.dev/go-fabric/secure/sigma"
)

// discardConn is an exchange.Conn that sends nowhere.
type discardConn struct{}

func (discardConn) Send(*net.UDPAddr, []byte) error { return nil }

// TestFabricUpdateWaitsForTheHandshakeWhateverItsContext (dev6a R8): the
// UpdateNOC's client-stack teardown waits for a connection process to end
// before it closes the fabric's sessions, also when the command's context
// has ended — a handshake finishing in that instant would otherwise adopt
// a session under the old NOC after the walk. The process is held in its
// attempt (the peer set's Fabrics lookup) while the teardown runs with a
// cancelled context; the teardown must not return before it is released.
func TestFabricUpdateWaitsForTheHandshakeWhateverItsContext(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		logger := slog.New(slog.DiscardHandler)
		names := discovery.NewNames(&quietMDNS{out: make(chan mdns.QueryResponse)}, logger)
		mgr, err := exchange.NewManager(discardConn{}, exchange.Config{Logger: logger})
		if err != nil {
			t.Fatal(err)
		}
		// The set reads Fabrics when it takes the peer too; the hold is
		// armed for the connection process's attempt only.
		var armed atomic.Bool
		held, release := make(chan struct{}), make(chan struct{})
		fabrics := func(handshake.PeerAddress) (handshake.Fabric, bool) {
			if armed.CompareAndSwap(true, false) {
				close(held)
				<-release
			}
			return handshake.Fabric{Identity: &sigma.Identity{}}, true
		}
		mem := &handshake.MemoryResumptionStore{}
		peers, err := peer.NewSet(peer.Context{Exchanges: mgr, Names: names, Fabrics: fabrics, Resumption: mem, Logger: logger})
		if err != nil {
			t.Fatal(err)
		}
		n := &Node{logger: logger, client: &clientStack{exchanges: mgr, names: names, peers: peers, resumption: mem}}

		addr := handshake.PeerAddress{FabricIndex: 3, NodeID: 0x77}
		p, err := peers.For(addr)
		if err != nil {
			t.Fatal(err)
		}
		armed.Store(true)
		connectCtx, stopConnect := context.WithCancel(context.Background())
		connected := make(chan struct{})
		go func() {
			defer close(connected)
			_, _ = p.Connect(connectCtx, peer.ConnectOptions{ConnectionTimeout: time.Hour})
		}()
		hint := netip.MustParseAddrPort("[::1]:9")
		for p.HintAddresses(hint) == 0 {
			synctest.Wait()
		}
		<-held

		ended, end := context.WithCancel(context.Background())
		end()
		swept := make(chan struct{})
		go func() {
			defer close(swept)
			n.closeClientFabricSessions(ended, addr.FabricIndex)
		}()
		synctest.Wait()
		select {
		case <-swept:
			t.Error("the teardown returned while the connection process under the old NOC was still running")
		default:
		}
		close(release)
		<-swept

		stopConnect()
		<-connected
		peers.Close(context.Background())
		_ = names.Close()
	})
}

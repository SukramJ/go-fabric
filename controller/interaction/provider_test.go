// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package interaction_test

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/bridge/bridgetest"
	"github.com/SukramJ/go-fabric/clock"
	"github.com/SukramJ/go-fabric/controller/discovery"
	"github.com/SukramJ/go-fabric/controller/exchange"
	"github.com/SukramJ/go-fabric/controller/handshake"
	"github.com/SukramJ/go-fabric/controller/interaction"
	"github.com/SukramJ/go-fabric/controller/peer"
	"github.com/SukramJ/go-fabric/mdns"
	"github.com/SukramJ/go-fabric/secure/channel"
	"github.com/SukramJ/go-fabric/secure/mattercert"
)

// TestSessionProvider: the session-bound provider (DedicatedChannelExchangeProvider,
// ExchangeProvider.ts:122-164) names no peer address for a session without
// a fabric, reports reachable, the base path floors, and the maximum
// response time of the session; an exchange on a closed session fails.
func TestSessionProvider(t *testing.T) {
	t.Parallel()
	s := newScripted(t)
	p := interaction.SessionProvider(s.mgr, s.session)
	if addr, ok := p.PeerAddress(); !ok || addr != (exchange.PeerAddress{FabricIndex: 1, NodeID: 0x2222}) {
		t.Fatalf("peer address %+v, %v", addr, ok)
	}
	if _, ok := interaction.SessionProvider(s.mgr, paseLike(t, s)).PeerAddress(); ok {
		t.Fatal("a session without a fabric index has a peer address")
	}
	if !p.VerifyReachability(context.Background()) || p.ReadPathsSupported() != 9 || p.SubscribePathsSupported() != 3 {
		t.Fatal("base provider answers")
	}
	if p.Session() != s.session || p.Manager() != s.mgr {
		t.Fatal("session or manager")
	}
	c := interaction.Bind(p, interaction.Config{})
	if c.Provider() != p || c.Session() != s.session {
		t.Fatal("Bind")
	}
	local := exchange.IntervalsOf(s.mgr.LocalParameters())
	if got, want := c.MaximumPeerResponseTime(0, false), exchange.MaxPeerResponseTime(nil, false, local, exchange.DefaultExpectedProcessingTime); got != want {
		t.Fatalf("MaximumPeerResponseTime = %v, want %v", got, want)
	}
	if c.MaximumPeerResponseTime(time.Second, true) <= c.MaximumPeerResponseTime(time.Second, false) {
		t.Fatal("including the sending time does not lengthen the wait")
	}
	s.session.Close()
	if _, err := p.InitiateExchange(context.Background(), interaction.ExchangeOptions{}); !errors.Is(err, exchange.ErrSessionClosed) {
		t.Fatalf("exchange on a closed session: %v", err)
	}
}

// paseLike is a secure session of s's manager without a fabric index.
func paseLike(t *testing.T, s *scripted) *exchange.Session {
	t.Helper()
	ps, err := s.mgr.NewSecureSession(exchange.SecureSessionConfig{
		ID: 0x0109, Addr: peerAddr, PeerParameters: exchange.PeerParameters(nil),
		Channel: channel.Config{EncryptKey: bytes.Repeat([]byte{3}, 16), DecryptKey: bytes.Repeat([]byte{4}, 16), PeerSessionID: 9},
	})
	if err != nil {
		t.Fatal(err)
	}
	return ps
}

// fakeMDNS is an mDNS socket that sends nowhere and receives nothing.
type fakeMDNS struct{ out chan mdns.QueryResponse }

func (f *fakeMDNS) Send([]byte) error                    { return nil }
func (f *fakeMDNS) Responses() <-chan mdns.QueryResponse { return f.out }
func (f *fakeMDNS) Close() error                         { return nil }

// peerSet is a peer set over s's manager whose fabric 1 is a bridgetest
// fabric.
func peerSet(t *testing.T, s *scripted) *peer.Set {
	t.Helper()
	c := bridgetest.Dial(t, "127.0.0.1:9", bridgetest.Options{Passcode: 20202021, FabricID: 0x0F0B, NodeID: 0x2222, Seed: 3})
	id := c.ControllerIdentity(0x1111)
	id.FabricIndex = 1
	v, err := mattercert.NewVerifier(c.RootPublicKey(), clock.System{})
	if err != nil {
		t.Fatal(err)
	}
	fabric := handshake.Fabric{Identity: &id, RootPublicKey: c.RootPublicKey(), Verifier: v}
	names := discovery.NewNames(&fakeMDNS{out: make(chan mdns.QueryResponse)}, nil)
	set, err := peer.NewSet(peer.Context{
		Exchanges: s.mgr, Names: names, Clock: s.clock,
		Fabrics: func(a handshake.PeerAddress) (handshake.Fabric, bool) { return fabric, a.FabricIndex == 1 },
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		set.Close(context.Background())
		_ = names.Close()
	})
	return set
}

// TestPeerProvider: the peer provider (PeerExchangeProvider.ts:19-163)
// names the peer's address, runs an exchange on its newest session, and
// with RequireExistingSession fails without one instead of connecting.
func TestPeerProvider(t *testing.T) {
	t.Parallel()
	s := newScripted(t)
	set := peerSet(t, s)
	session, err := s.mgr.NewSecureSession(exchange.SecureSessionConfig{
		ID: 0x0102, Addr: peerAddr, FabricIndex: 1,
		Channel:        channel.Config{EncryptKey: bytes.Repeat([]byte{1}, 16), DecryptKey: bytes.Repeat([]byte{2}, 16), LocalNodeID: 0x1111, PeerNodeID: 0x2222, PeerSessionID: 0x7102},
		PeerParameters: exchange.PeerParameters(nil),
	})
	if err != nil {
		t.Fatal(err)
	}
	p := set.Adopt(session)
	if p == nil {
		t.Fatal("session not adopted")
	}
	c := interaction.ForPeer(p, interaction.Config{})
	prov := c.Provider()
	if addr, ok := prov.PeerAddress(); !ok || addr != p.Address() || prov.Manager() != s.mgr || c.Session() != session {
		t.Fatalf("peer address %+v, session %v", addr, c.Session())
	}
	if prov.ReadPathsSupported() != 9 || prov.SubscribePathsSupported() != 3 || prov.MaximumPeerResponseTime(0, true) <= prov.MaximumPeerResponseTime(0, false) {
		t.Fatal("provider answers")
	}
	x, err := prov.InitiateExchange(context.Background(), interaction.ExchangeOptions{RequireExistingSession: true})
	if err != nil || x.Session() != session {
		t.Fatalf("exchange %v, %v", x, err)
	}
	x.Close()
	session.Close()
	if _, err := prov.InitiateExchange(context.Background(), interaction.ExchangeOptions{RequireExistingSession: true}); !errors.Is(err, interaction.ErrNoSession) {
		t.Fatalf("without a session: %v", err)
	}
}

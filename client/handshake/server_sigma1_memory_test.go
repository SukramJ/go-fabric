// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package handshake

import (
	"net"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/client/exchange"
	"github.com/SukramJ/go-fabric/clock/clocktest"
	"github.com/SukramJ/go-fabric/secure/sigma"
)

// silentConn is a socket that sends nothing anywhere.
type silentConn struct{}

func (silentConn) Send(*net.UDPAddr, []byte) error { return nil }

// newSigma1Memory is a CASE server whose remembered Sigma1 randoms run on
// clk; nothing in these tests reaches its manager.
func newSigma1Memory(t *testing.T, clk *clocktest.Fake) *CASEServer {
	t.Helper()
	mgr, err := exchange.NewManager(silentConn{}, exchange.Config{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(mgr.Close)
	c, err := NewCASEServer(mgr, CASEServerConfig{Fabrics: FabricList{}})
	if err != nil {
		t.Fatal(err)
	}
	c.clock = clk
	return c
}

// randomOf is a distinct initiator random per i.
func randomOf(i int) [sigma.RandomSize]byte {
	var r [sigma.RandomSize]byte
	r[0], r[1], r[2] = byte(i), byte(i>>8), 0xa5
	return r
}

// started claims and remembers random, as a Sigma1 that started a
// handshake does.
func started(t *testing.T, c *CASEServer, random [sigma.RandomSize]byte) {
	t.Helper()
	if !c.claim(random) {
		t.Fatalf("random %x refused before it started a handshake", random[:3])
	}
	c.remember(random)
}

func (c *CASEServer) sigma1Remembered() (seen, order int) {
	c.seenMu.Lock()
	defer c.seenMu.Unlock()
	return len(c.seen), len(c.seenOrder)
}

// TestCASEServerForgetsASigma1AfterItsLifetime: a Sigma1 random that
// started a handshake is refused for seenSigma1Lifetime and accepted from
// then on, and a remember past the lifetime drops the expired ones however
// few the memory holds.
func TestCASEServerForgetsASigma1AfterItsLifetime(t *testing.T) {
	t.Parallel()
	clk := clocktest.New(time.Unix(1_000_000, 0))
	c := newSigma1Memory(t, clk)
	first := randomOf(0)
	started(t, c, first)
	clk.Advance(seenSigma1Lifetime - time.Millisecond)
	if c.claim(first) {
		t.Fatalf("a random remembered %v ago was accepted; the lifetime is %v", seenSigma1Lifetime-time.Millisecond, seenSigma1Lifetime)
	}
	clk.Advance(time.Millisecond)
	if !c.claim(first) {
		t.Fatalf("a random remembered %v ago was refused", seenSigma1Lifetime)
	}
	c.release(first)
	started(t, c, randomOf(1))
	if seen, order := c.sigma1Remembered(); seen != 1 || order != 1 {
		t.Fatalf("after a remember past the lifetime the memory holds %d randoms (%d in order), want only the new one", seen, order)
	}
}

// TestCASEServerRemembersAtMostTheLimit: beyond seenSigma1Limit
// remembered randoms the oldest is forgotten — accepted again inside its
// lifetime — while the latest stays refused, and the map and its order
// never hold more than the limit.
func TestCASEServerRemembersAtMostTheLimit(t *testing.T) {
	t.Parallel()
	clk := clocktest.New(time.Unix(1_000_000, 0))
	c := newSigma1Memory(t, clk)
	for i := range seenSigma1Limit + 1 {
		started(t, c, randomOf(i))
		if seen, order := c.sigma1Remembered(); seen != order || seen > seenSigma1Limit {
			t.Fatalf("after %d randoms the memory holds %d (%d in order), want equal and at most %d", i+1, seen, order, seenSigma1Limit)
		}
	}
	if !c.claim(randomOf(0)) {
		t.Fatalf("the oldest of %d randoms was refused; the memory holds %d", seenSigma1Limit+1, seenSigma1Limit)
	}
	c.release(randomOf(0))
	if c.claim(randomOf(seenSigma1Limit)) {
		t.Fatal("the latest random was accepted")
	}
}

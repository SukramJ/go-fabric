// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package peer

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/client/exchange"
	"github.com/SukramJ/go-fabric/transport/mrp"
)

type connectResult struct {
	s   *exchange.Session
	err error
}

// connectAsync runs Connect in the background.
func connectAsync(p *Peer, opts ConnectOptions) <-chan connectResult {
	out := make(chan connectResult, 1)
	go func() {
		s, err := p.Connect(context.Background(), opts)
		out <- connectResult{s, err}
	}()
	return out
}

func waitResult(t *testing.T, ch <-chan connectResult) connectResult {
	t.Helper()
	select {
	case r := <-ch:
		return r
	case <-time.After(10 * time.Second):
		t.Fatal("Connect did not return")
		return connectResult{}
	}
}

// process is the peer's running connection process.
func process(p *Peer) *connection {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.connecting
}

const (
	addr1 = "[fd00::1]:5540"
	addr2 = "[fd00::2]:5540"
	addr9 = "[fd00::9]:5540"
)

// TestConnectDiscoveredAddress: with no operational address the process
// waits for discovery (resolving actively), then attempts the discovered
// address (PeerConnection.ts:186-200, :341-358).
func TestConnectDiscoveredAddress(t *testing.T) {
	t.Parallel()
	h := newHarness(t, nil)
	p, _ := h.set.For(testAddr)
	res := connectAsync(p, ConnectOptions{ConnectionTimeout: -1})
	eventually(t, "the process", func() bool { return process(p) != nil && p.status.IsResolving() })
	if p.TimeOffline() != 0 {
		t.Fatal("time offline on a fresh process")
	}
	h.clock.Advance(time.Second)
	if p.TimeOffline() != time.Second {
		t.Fatalf("TimeOffline = %v", p.TimeOffline())
	}
	h.announceAddrs(t, "fd00::1")
	s1 := h.sigma1(t, addr1)
	if s1.proto.ExchangeID == 0 && s1.hdr.SourceNodeID == 0 {
		t.Fatal("Sigma1 without an initiator")
	}
	p.Close(context.Background())
	if r := waitResult(t, res); !errors.Is(r.err, ErrClosed) {
		t.Fatalf("Connect after Close = %v", r.err)
	}
	if _, err := p.Connect(context.Background(), ConnectOptions{}); !errors.Is(err, ErrClosed) {
		t.Fatalf("Connect on a closed peer = %v", err)
	}
}

// TestFallbackOnlyAndNotFirst: the operational address is the fallback
// when nothing is discovered; a fallback attempted as the first address
// uses the normal backoff, one attempted after another address adds
// MaxDelayBetweenInitialContactRetries to each backoff (PeerConnection.ts:360-379,
// :519-520, :597-603).
func TestFallbackOnlyAndNotFirst(t *testing.T) {
	t.Parallel()
	h := newHarness(t, nil)
	p, _ := h.set.AddKnownPeer(Descriptor{Address: testAddr, OperationalAddress: udpAddr(addr9)})
	connectAsync(p, ConnectOptions{ConnectionTimeout: -1})
	h.sigma1(t, addr9)
	if h.clock.hasTimer(2 * time.Minute) {
		t.Fatal("the first fallback attempt waits the initial-contact maximum")
	}
	p.Close(context.Background())

	h2 := newHarness(t, nil)
	q, _ := h2.set.AddKnownPeer(Descriptor{Address: testAddr, OperationalAddress: udpAddr(addr9)})
	h2.announceAddrs(t, "fd00::1")
	eventually(t, "the address", func() bool { return len(q.Service().Addresses()) == 1 })
	connectAsync(q, ConnectOptions{ConnectionTimeout: -1})
	h2.sigma1(t, addr1)
	h2.expire(t, "fd00::1")
	// The discovered address left: its attempt ends, the fallback waits for
	// the stagger, then goes with the long backoff.
	h2.clock.waitTimer(t, 45*time.Second)
	h2.clock.Advance(45 * time.Second)
	h2.sigma1(t, addr9)
	h2.clock.waitTimer(t, 2*time.Minute)
}

// TestFallbackRetiredAfterStabilization: discovery that names other
// addresses retires the fallback after AddressChangeStabilizationDelay and
// the discovered address starts at once, without the stagger
// (PeerConnection.ts:264-303).
func TestFallbackRetiredAfterStabilization(t *testing.T) {
	t.Parallel()
	h := newHarness(t, nil)
	p, _ := h.set.AddKnownPeer(Descriptor{Address: testAddr, OperationalAddress: udpAddr(addr9)})
	connectAsync(p, ConnectOptions{ConnectionTimeout: -1})
	h.sigma1(t, addr9)
	h.announceAddrs(t, "fd00::1")
	h.clock.waitTimer(t, 10*time.Second)
	h.clock.waitTimer(t, 45*time.Second)
	h.clock.Advance(10*time.Second - time.Millisecond)
	h.noSigma1(t, addr1)
	h.clock.Advance(time.Millisecond)
	h.sigma1(t, addr1)
	eventually(t, "the fallback to go", func() bool {
		c := process(p)
		c.mu.Lock()
		defer c.mu.Unlock()
		_, ok := c.attempts[attemptKey{ap: addressOf(udpAddr(addr9)).AddrPort()}]
		return !ok && c.fallback == nil
	})
}

// TestTwoAddressesStaggered: two discovered addresses start
// DelayBeforeNextAddress apart (PeerConnection.ts:233-260).
func TestTwoAddressesStaggered(t *testing.T) {
	t.Parallel()
	h := newHarness(t, nil)
	p, _ := h.set.For(testAddr)
	h.announceAddrs(t, "fd00::1", "fd00::2")
	eventually(t, "the addresses", func() bool { return len(p.Service().Addresses()) == 2 })
	connectAsync(p, ConnectOptions{ConnectionTimeout: -1})
	first := h.sigma1(t, addr1)
	_ = first
	h.clock.waitTimer(t, 45*time.Second)
	h.clock.Advance(45*time.Second - time.Millisecond)
	h.noSigma1(t, addr2)
	h.clock.Advance(time.Millisecond)
	h.sigma1(t, addr2)
}

// TestOperationalAddressStaysAsFallback: when every discovered address
// expires, the operational address's attempt goes on as the fallback
// (PeerConnection.ts:422-429).
func TestOperationalAddressStaysAsFallback(t *testing.T) {
	t.Parallel()
	h := newHarness(t, nil)
	p, _ := h.set.AddKnownPeer(Descriptor{Address: testAddr, OperationalAddress: udpAddr(addr1)})
	h.announceAddrs(t, "fd00::1")
	eventually(t, "the address", func() bool { return len(p.Service().Addresses()) == 1 })
	connectAsync(p, ConnectOptions{ConnectionTimeout: -1})
	s1 := h.sigma1(t, addr1)
	h.expire(t, "fd00::1")
	eventually(t, "the fallback mark", func() bool {
		c := process(p)
		c.mu.Lock()
		defer c.mu.Unlock()
		return c.fallback != nil && len(c.attempts) == 1
	})
	// The attempt is the same exchange: it retransmits on.
	h.clock.Advance(5 * time.Second)
	d := decodeUnsecured(t, h.udp.nextTo(t, addr1).bytes)
	if d.proto.ExchangeID != s1.proto.ExchangeID {
		t.Fatal("the attempt restarted")
	}
}

// TestConnectionErrorDelays: the pause after each kind of failure
// (PeerConnection.ts:674-696, :745-751).
func TestConnectionErrorDelays(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		reply func(t *testing.T, h *harness, s unsecured)
		delay time.Duration
	}{
		{"busy", func(t *testing.T, h *harness, s unsecured) {
			t.Helper()
			h.status(s, addr1, generalStatusBusy, secureChannelStatusBusy, busyData(500))
		}, 500*time.Millisecond + 7500*time.Millisecond},
		{"busy without delay", func(t *testing.T, h *harness, s unsecured) {
			t.Helper()
			h.status(s, addr1, generalStatusBusy, secureChannelStatusBusy, nil)
		}, 5 * time.Minute},
		{"invalid param", func(t *testing.T, h *harness, s unsecured) {
			t.Helper()
			h.status(s, addr1, mrp.SCStatusGeneralFailure, mrp.SCStatusProtocolInvalidParameter, nil)
		}, 5 * time.Minute},
		{"no shared trust roots without a record", func(t *testing.T, h *harness, s unsecured) {
			t.Helper()
			h.status(s, addr1, mrp.SCStatusGeneralFailure, mrp.SCStatusProtocolNoSharedTrustRoots, nil)
		}, 5 * time.Minute},
		{"message missing", func(t *testing.T, h *harness, s unsecured) {
			t.Helper()
			h.reply(s, addr1, mrp.StandaloneAckOpcode, nil)
			h.clock.waitTimer(t, caseResponseTimeout())
			h.clock.Advance(caseResponseTimeout())
		}, 15 * time.Second},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			h := newHarness(t, nil)
			p, _ := h.set.AddKnownPeer(Descriptor{Address: testAddr, OperationalAddress: udpAddr(addr1)})
			connectAsync(p, ConnectOptions{ConnectionTimeout: -1})
			c.reply(t, h, h.sigma1(t, addr1))
			h.clock.waitTimer(t, c.delay)
			h.clock.Advance(c.delay - time.Millisecond)
			h.noSigma1(t, addr1)
			h.clock.Advance(time.Millisecond)
			h.sigma1(t, addr1)
		})
	}
}

// caseResponseTimeout is the wait for Sigma2 after Sigma1 was acked: our
// worst-case retransmission time plus CASE's 35 s and the 5 s buffer.
func caseResponseTimeout() time.Duration {
	return exchange.MaxPeerResponseTime(nil, false, exchange.IntervalsOf(exchange.DefaultLocalParameters()), 35*time.Second)
}

// TestNoSharedTrustRootsDeletesTheRecordAndRetries: the record goes and
// the next attempt starts at once (PeerConnection.ts:681-685).
func TestNoSharedTrustRootsDeletesTheRecordAndRetries(t *testing.T) {
	t.Parallel()
	h := newHarness(t, nil)
	_ = h.resume.SaveResumptionRecord(resumptionRecordFor(t, h))
	p, _ := h.set.AddKnownPeer(Descriptor{Address: testAddr, OperationalAddress: udpAddr(addr1)})
	connectAsync(p, ConnectOptions{ConnectionTimeout: -1})
	h.status(h.sigma1(t, addr1), addr1, mrp.SCStatusGeneralFailure, mrp.SCStatusProtocolNoSharedTrustRoots, nil)
	h.sigma1(t, addr1)
	if _, ok := h.resume.FindResumptionRecord(testAddr); ok {
		t.Fatal("the resumption record survived")
	}
}

// TestHandleErrorOverrideAndFatal: the handler replaces the delay; an
// error it returns ends the process and Connect with it
// (PeerConnection.ts:704-717).
func TestHandleErrorOverrideAndFatal(t *testing.T) {
	t.Parallel()
	fatal := errors.New("give up")
	calls := 0
	h := newHarness(t, func(c *Context) {
		c.HandleError = func(err error) (time.Duration, bool, error) {
			calls++
			if calls == 1 {
				return time.Second, true, nil
			}
			if calls == 2 {
				return 0, false, nil
			}
			return 0, false, fatal
		}
	})
	p, _ := h.set.AddKnownPeer(Descriptor{Address: testAddr, OperationalAddress: udpAddr(addr1)})
	res := connectAsync(p, ConnectOptions{ConnectionTimeout: -1})
	h.status(h.sigma1(t, addr1), addr1, mrp.SCStatusGeneralFailure, mrp.SCStatusProtocolInvalidParameter, nil)
	h.clock.waitTimer(t, time.Second)
	h.clock.Advance(time.Second)
	h.status(h.sigma1(t, addr1), addr1, mrp.SCStatusGeneralFailure, mrp.SCStatusProtocolInvalidParameter, nil)
	h.clock.waitTimer(t, 5*time.Minute)
	h.clock.Advance(5 * time.Minute)
	h.status(h.sigma1(t, addr1), addr1, mrp.SCStatusGeneralFailure, mrp.SCStatusProtocolInvalidParameter, nil)
	if r := waitResult(t, res); !errors.Is(r.err, fatal) {
		t.Fatalf("Connect = %v, want the fatal error", r.err)
	}
}

// TestUnhandledErrorAndDisconnectedSet: an attempt that cannot start waits
// DelayAfterUnhandledError; after Set.Disconnect every attempt fails
// (PeerSet.ts:248-262).
func TestUnhandledErrorAndDisconnectedSet(t *testing.T) {
	t.Parallel()
	h := newHarness(t, nil)
	p, _ := h.set.AddKnownPeer(Descriptor{Address: testAddr, OperationalAddress: udpAddr(addr1)})
	h.set.Disconnect(context.Background())
	res := connectAsync(p, ConnectOptions{ConnectionTimeout: 3 * time.Minute})
	h.clock.waitTimer(t, 2*time.Minute)
	h.udp.none(t)
	h.clock.Advance(3 * time.Minute)
	var unreachable *UnreachableError
	if r := waitResult(t, res); !errors.As(r.err, &unreachable) || !errors.Is(r.err, exchange.ErrTransient) {
		t.Fatalf("Connect = %v", r.err)
	}
}

// TestConnectReuseSharedProcessAndTimeout: a live session is returned at
// once; concurrent Connects share one process; the wait is the timeout
// less the time the peer has been offline, and ends in
// UnreachableError; a session added from elsewhere ends the process
// and Connect returns it (Peer.ts:414-491, :628-673).
func TestConnectReuseSharedProcessAndTimeout(t *testing.T) {
	t.Parallel()
	h := newHarness(t, nil)
	p, _ := h.set.AddKnownPeer(Descriptor{Address: testAddr, OperationalAddress: udpAddr(addr1)})
	long := connectAsync(p, ConnectOptions{})
	h.sigma1(t, addr1)
	c := process(p)
	h.clock.Advance(30 * time.Second)
	short := connectAsync(p, ConnectOptions{ConnectionTimeout: time.Minute})
	h.clock.waitTimer(t, 30*time.Second) // a minute less the 30 s offline
	if process(p) != c {
		t.Fatal("the second Connect started a second process")
	}
	h.noSigma1(t, addr1)
	h.clock.Advance(30 * time.Second)
	var unreachable *UnreachableError
	if r := waitResult(t, short); !errors.As(r.err, &unreachable) || unreachable.TimeOffline != time.Minute {
		t.Fatalf("short Connect = %v", r.err)
	}
	if _, err := p.Connect(context.Background(), ConnectOptions{ConnectionTimeout: time.Minute}); !errors.As(err, &unreachable) {
		t.Fatalf("a Connect whose timeout is already spent = %v", err)
	}
	// A session from elsewhere ends the process; the waiting Connect takes it.
	s := h.secureSession(t, 0x30, udpAddr(addr1))
	h.set.Adopt(s)
	if r := waitResult(t, long); r.err != nil || r.s != s {
		t.Fatalf("long Connect = %v, %v", r.s, r.err)
	}
	eventually(t, "the process to end", func() bool { return process(p) == nil && p.TimeOffline() == 0 })
	if got, err := p.Connect(context.Background(), ConnectOptions{}); err != nil || got != s {
		t.Fatal("Connect did not reuse the live session")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s.Close()
	if _, err := p.Connect(ctx, ConnectOptions{ConnectionTimeout: -1}); !errors.Is(err, context.Canceled) {
		t.Fatalf("Connect with a cancelled context = %v", err)
	}
}

// TestKickRestartsTheExchange: a kick is ignored below
// KickMinRetransmissions or KickMinRestartSaving and within the cooldown;
// otherwise the attempt opens a fresh exchange (PeerConnection.ts:568-594).
func TestKickRestartsTheExchange(t *testing.T) {
	t.Parallel()
	h := newHarness(t, nil)
	p, _ := h.set.AddKnownPeer(Descriptor{Address: testAddr, OperationalAddress: udpAddr(addr1)})
	connectAsync(p, ConnectOptions{ConnectionTimeout: -1})
	first := h.sigma1(t, addr1)
	c := process(p)
	exchangeOf := func() *exchange.Exchange {
		var x *exchange.Exchange
		eventually(t, "the attempt's exchange", func() bool {
			c.mu.Lock()
			defer c.mu.Unlock()
			x = currentExchange(c)
			return x != nil && x.RetransmissionCount() == 0
		})
		return x
	}
	// Below the retransmission minimum: nothing.
	p.Kick()
	h.noSigma1(t, addr1)
	// Retransmit until a restart saves a minute.
	x := exchangeOf()
	for x.RetransmissionRestartSaving() < time.Minute {
		h.clock.Advance(5 * time.Second)
		eventually(t, "the retransmission", func() bool { return x.RetransmissionCount() > 0 })
	}
	h.clock.Advance(3 * time.Second) // past the kick throttle
	p.Kick()
	restarted := h.sigma1(t, addr1)
	if restarted.proto.ExchangeID == first.proto.ExchangeID {
		t.Fatal("the kick did not open a fresh exchange")
	}
	// Again until the restart would pay, but within the 10-minute cooldown.
	x = exchangeOf()
	for x.RetransmissionRestartSaving() < time.Minute {
		h.clock.Advance(5 * time.Second)
	}
	h.clock.Advance(3 * time.Second)
	p.Kick()
	h.noSigma1(t, addr1)
	h.clock.Advance(10 * time.Minute)
	p.Kick()
	h.sigma1(t, addr1)
}

// TestDiscoverKick: a fallback that discovery names becomes a real
// address and kicks the process with origin discover, which restarts an
// exchange that has retransmitted enough (PeerConnection.ts:346-352).
func TestDiscoverKick(t *testing.T) {
	t.Parallel()
	h := newHarness(t, nil)
	p, _ := h.set.AddKnownPeer(Descriptor{Address: testAddr, OperationalAddress: udpAddr(addr1)})
	connectAsync(p, ConnectOptions{ConnectionTimeout: -1})
	first := h.sigma1(t, addr1)
	c := process(p)
	var x *exchange.Exchange
	eventually(t, "the exchange", func() bool {
		c.mu.Lock()
		defer c.mu.Unlock()
		x = currentExchange(c)
		return x != nil && c.fallback != nil
	})
	for x.RetransmissionRestartSaving() < time.Minute {
		h.clock.Advance(5 * time.Second)
	}
	h.announceAddrs(t, "fd00::1")
	restarted := h.sigma1(t, addr1)
	if restarted.proto.ExchangeID == first.proto.ExchangeID {
		t.Fatal("discovery of the fallback did not restart the exchange")
	}
	c.mu.Lock()
	fb := c.fallback
	c.mu.Unlock()
	if fb != nil {
		t.Fatal("the discovered fallback is still marked as fallback")
	}
}

// TestEstablishmentUnresponsive: retransmitting Sigma1 past the MRP
// budget fires the hook once (PeerConnection.ts:536, :815-826) and marks
// the service unreachable on the first retransmission (:847-855).
func TestEstablishmentUnresponsive(t *testing.T) {
	t.Parallel()
	var fired atomic.Int32
	h := newHarness(t, func(c *Context) { c.EstablishmentUnresponsive = func(*Peer) { fired.Add(1) } })
	p, _ := h.set.AddKnownPeer(Descriptor{Address: testAddr, OperationalAddress: udpAddr(addr1)})
	connectAsync(p, ConnectOptions{ConnectionTimeout: -1})
	h.sigma1(t, addr1)
	if !p.Service().Reachable() {
		t.Fatal("unreachable before any retransmission")
	}
	for range 40 {
		h.clock.Advance(10 * time.Second)
		if fired.Load() > 0 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	eventually(t, "the hook", func() bool { return fired.Load() == 1 })
	if p.Service().Reachable() {
		t.Fatal("retransmitting left the service reachable")
	}
}

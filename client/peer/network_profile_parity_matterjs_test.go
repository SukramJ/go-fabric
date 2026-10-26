// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package peer

// A port of matter.js packages/protocol/test/peer/NetworkProfileTest.ts at
// 862ad75f (12 cases), plus the medium selection of NetworkProfiles.forPeer
// (packages/protocol/src/peer/NetworkProfile.ts:184-221) and the
// Semaphore's cooldown and timeout (packages/general/src/util/Semaphore.ts)
// the profiles throttle with.

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/client/exchange"
	"github.com/SukramJ/go-fabric/clock/clocktest"
)

func TestNetworkProfilesParity(t *testing.T) {
	t.Parallel()
	ms, s := time.Millisecond, time.Second
	t.Run("additionalMrpDelay", func(t *testing.T) {
		t.Parallel()
		get := func(t *testing.T, n *NetworkProfiles, id string) *NetworkProfile {
			t.Helper()
			p, err := n.Get(id)
			if err != nil {
				t.Fatal(err)
			}
			return p
		}
		t.Run("fast and unlimited carry no additive delay", func(t *testing.T) {
			n := NewNetworkProfiles(nil, nil, nil)
			if get(t, n, "fast").AdditionalMrpDelay != 0 || get(t, n, "unlimited").AdditionalMrpDelay != 0 {
				t.Error("fast or unlimited carries a delay")
			}
		})
		t.Run("wifi carries 1s", func(t *testing.T) {
			if d := get(t, NewNetworkProfiles(nil, nil, nil), "wifi").AdditionalMrpDelay; d != s {
				t.Errorf("wifi %v", d)
			}
		})
		t.Run("icdLit is unthrottled and carries no additive delay", func(t *testing.T) {
			p := get(t, NewNetworkProfiles(nil, nil, nil), "icdLit")
			if p.AdditionalMrpDelay != 0 || p.Connect != nil {
				t.Fatalf("icdLit delay %v connect %v", p.AdditionalMrpDelay, p.Connect)
			}
			ctx := context.Background()
			s1, err1 := p.Semaphore.Obtain(ctx)
			s2, err2 := p.Semaphore.Obtain(ctx)
			if err1 != nil || err2 != nil {
				t.Fatal(err1, err2)
			}
			defer s1.Close()
			defer s2.Close()
			if p.Semaphore.Running() != 2 || p.Semaphore.Count() != 0 {
				t.Errorf("running %d queued %d", p.Semaphore.Running(), p.Semaphore.Count())
			}
		})
		t.Run("conservative, thread and unknown carry 1.5s", func(t *testing.T) {
			n := NewNetworkProfiles(nil, nil, nil)
			for _, id := range []string{"conservative", "thread", "unknown"} {
				if d := get(t, n, id).AdditionalMrpDelay; d != 1500*ms {
					t.Errorf("%s %v", id, d)
				}
			}
		})
		t.Run("bdx defaults to the messaging margin", func(t *testing.T) {
			n := NewNetworkProfiles(nil, nil, nil)
			if get(t, n, "wifi").Margins.BDX != s || get(t, n, "fast").Margins.BDX != 0 || get(t, n, "conservative").Margins.BDX != 1500*ms {
				t.Error("bdx margin is not the messaging margin")
			}
		})
		t.Run("a configured bdx margin overrides the default and reaches the sub-profiles", func(t *testing.T) {
			n := NewNetworkProfiles(nil, nil, nil)
			three := 3 * s
			n.SetDefaults(ProfilesConfig{Conservative: &PartialLimits{BDXAdditionalMrpDelay: &three}})
			c := get(t, n, "conservative")
			if c.AdditionalMrpDelay != 1500*ms || c.Margins.BDX != 3*s {
				t.Errorf("delay %v bdx %v", c.AdditionalMrpDelay, c.Margins.BDX)
			}
		})
		t.Run("connect and probe sub-profiles inherit the parent additive delay", func(t *testing.T) {
			c := get(t, NewNetworkProfiles(nil, nil, nil), "conservative")
			if c.Connect.AdditionalMrpDelay != 1500*ms || c.ProbeAddress.AdditionalMrpDelay != 1500*ms {
				t.Errorf("connect %v probe %v", c.Connect.AdditionalMrpDelay, c.ProbeAddress.AdditionalMrpDelay)
			}
		})
		t.Run("a configured value overrides the default and propagates to sub-profiles", func(t *testing.T) {
			p := NewNetworkProfiles(nil, nil, nil).Configure("custom", Limits{
				ConcreteLimits: ConcreteLimits{Exchanges: 4, AdditionalMrpDelay: durationPtr(3 * s)},
				Connect:        &ConcreteLimits{Exchanges: 1},
			}, nil)
			if p.AdditionalMrpDelay != 3*s || p.Connect.AdditionalMrpDelay != 3*s {
				t.Errorf("delay %v connect %v", p.AdditionalMrpDelay, p.Connect.AdditionalMrpDelay)
			}
		})
		t.Run("a sub-profile may override the inherited value", func(t *testing.T) {
			p := NewNetworkProfiles(nil, nil, nil).Configure("custom2", Limits{
				ConcreteLimits: ConcreteLimits{Exchanges: 4, AdditionalMrpDelay: durationPtr(3 * s)},
				Connect:        &ConcreteLimits{Exchanges: 1, AdditionalMrpDelay: durationPtr(0)},
			}, nil)
			if p.AdditionalMrpDelay != 3*s || p.Connect.AdditionalMrpDelay != 0 {
				t.Errorf("delay %v connect %v", p.AdditionalMrpDelay, p.Connect.AdditionalMrpDelay)
			}
		})
		t.Run("defaults to zero when neither limits nor parent specify a value", func(t *testing.T) {
			p := NewNetworkProfiles(nil, nil, nil).Configure("custom3", Limits{ConcreteLimits: ConcreteLimits{Exchanges: 4}}, nil)
			if p.AdditionalMrpDelay != 0 {
				t.Errorf("delay %v", p.AdditionalMrpDelay)
			}
		})
	})
	t.Run("defaults", func(t *testing.T) {
		t.Parallel()
		t.Run("layers successive assignments instead of resetting to static templates", func(t *testing.T) {
			n := NewNetworkProfiles(nil, nil, nil)
			two, three := 2*s, 3*s
			n.SetDefaults(ProfilesConfig{Fast: &PartialLimits{PartialConcreteLimits: PartialConcreteLimits{AdditionalMrpDelay: &two}}})
			n.SetDefaults(ProfilesConfig{Thread: &PartialLimits{PartialConcreteLimits: PartialConcreteLimits{AdditionalMrpDelay: &three}}})
			fast, _ := n.Get("fast")
			thread, _ := n.Get("thread")
			if fast.AdditionalMrpDelay != 2*s || thread.AdditionalMrpDelay != 3*s {
				t.Errorf("fast %v thread %v", fast.AdditionalMrpDelay, thread.AdditionalMrpDelay)
			}
		})
	})
	t.Run("get", func(t *testing.T) {
		t.Parallel()
		t.Run("rejects unknown ids, including Object prototype keys", func(t *testing.T) {
			n := NewNetworkProfiles(nil, nil, nil)
			for _, id := range []string{"nope", "toString", "constructor"} {
				if _, err := n.Get(id); !errors.Is(err, ErrUnknownNetworkProfile) {
					t.Errorf("%s: %v", id, err)
				}
			}
		})
	})
}

// TestNetworkProfilesForMedium is NetworkProfiles.forPeer's selection
// (NetworkProfile.ts:184-221).
func TestNetworkProfilesForMedium(t *testing.T) {
	t.Parallel()
	yes, no := true, false
	n := NewNetworkProfiles(nil, nil, nil)
	for _, c := range []struct {
		name string
		m    *Medium
		want string
	}{
		{"no properties", nil, "unknown"},
		{"thread on channel 15", &Medium{SupportsThread: true, ThreadChannel: 15}, "thread:15"},
		{"thread active without channel", &Medium{ThreadActive: &yes}, "thread"},
		{"thread supported but inactive, wifi active", &Medium{SupportsThread: true, ThreadActive: &no, SupportsWifi: true, WifiActive: &yes}, "wifi"},
		{"wifi capable without medium data", &Medium{SupportsWifi: true}, "wifi"},
		{"wifi capable, ethernet active", &Medium{SupportsWifi: true, SupportsEthernet: true, EthernetActive: &yes}, "fast"},
		{"ethernet", &Medium{SupportsEthernet: true}, "fast"},
		{"other means", &Medium{RootEndpointServers: 3}, "fast"},
		{"nothing known", &Medium{}, "unknown"},
	} {
		if got := n.ForMedium(c.m).ID; got != c.want {
			t.Errorf("%s: %s, want %s", c.name, got, c.want)
		}
	}
	if a, b := n.ForMedium(&Medium{SupportsEthernet: true}), n.ForMedium(&Medium{RootEndpointServers: 1}); a != b {
		t.Error("two fast peers got two profiles; a profile is shared")
	}
}

// TestSemaphoreParity: the cooldown between grants and the slot timeout
// of matter.js's Semaphore (Semaphore.ts:85-95, :149-188), and Clear and
// Close failing the waiters (:224-255).
func TestSemaphoreParity(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	t.Run("cooldown", func(t *testing.T) {
		t.Parallel()
		fake := clocktest.New(time.Unix(1, 0))
		s := NewSemaphore(fake, "t", 4, 100*time.Millisecond, 0)
		first, err := s.Obtain(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer first.Close()
		got := make(chan *Slot, 1)
		go func() {
			sl, _ := s.Obtain(ctx)
			got <- sl
		}()
		eventually(t, "a queued request", func() bool { return s.Count() == 1 })
		select {
		case <-got:
			t.Fatal("granted inside the cooldown")
		default:
		}
		fake.Advance(100 * time.Millisecond)
		sl := <-got
		defer sl.Close()
		if s.Running() != 2 {
			t.Errorf("running %d", s.Running())
		}
	})
	t.Run("timeout releases a slot", func(t *testing.T) {
		t.Parallel()
		fake := clocktest.New(time.Unix(1, 0))
		s := NewSemaphore(fake, "t", 1, 0, 10*time.Second)
		if _, err := s.Obtain(ctx); err != nil {
			t.Fatal(err)
		}
		got := make(chan *Slot, 1)
		go func() {
			sl, _ := s.Obtain(ctx)
			got <- sl
		}()
		eventually(t, "a queued request", func() bool { return s.Count() == 1 })
		fake.Advance(10 * time.Second)
		(<-got).Close()
	})
	t.Run("close fails the waiters and later requests", func(t *testing.T) {
		t.Parallel()
		s := NewSemaphore(nil, "t", 1, 0, 0)
		held, _ := s.Obtain(ctx)
		defer held.Close()
		errc := make(chan error, 1)
		go func() {
			_, err := s.Obtain(ctx)
			errc <- err
		}()
		eventually(t, "a queued request", func() bool { return s.Count() == 1 })
		s.Close()
		if err := <-errc; !errors.Is(err, ErrSemaphoreClosed) {
			t.Errorf("waiter: %v", err)
		}
		if _, err := s.Obtain(ctx); !errors.Is(err, ErrSemaphoreClosed) {
			t.Errorf("later: %v", err)
		}
	})
	t.Run("an abandoned wait leaves the queue", func(t *testing.T) {
		t.Parallel()
		s := NewSemaphore(nil, "t", 1, 0, 0)
		held, _ := s.Obtain(ctx)
		cctx, cancel := context.WithCancel(ctx)
		errc := make(chan error, 1)
		go func() {
			_, err := s.Obtain(cctx)
			errc <- err
		}()
		eventually(t, "a queued request", func() bool { return s.Count() == 1 })
		cancel()
		if err := <-errc; !errors.Is(err, context.Canceled) || s.Count() != 0 {
			t.Errorf("err %v queued %d", err, s.Count())
		}
		held.Close()
		if s.Running() != 0 {
			t.Errorf("running %d", s.Running())
		}
	})
}

// TestConnectionThrottledByTheConnectProfile: a connection process holds a
// slot of its profile's connect sub-profile for its whole run, and its CASE
// exchange is padded with the medium's connect margin — matter.js
// PeerConnection.ts:110-121 at 862ad75f. With the default profiles a
// peer of unknown medium connects on "unknown:connect" (4 slots, 1.5 s).
func TestConnectionThrottledByTheConnectProfile(t *testing.T) {
	t.Parallel()
	h := newHarness(t, func(c *Context) { c.Networks = NewNetworkProfiles(c.Clock, nil, nil) })
	p, _ := h.set.AddKnownPeer(Descriptor{Address: testAddr, OperationalAddress: udpAddr(addr1)})
	connectAsync(p, ConnectOptions{ConnectionTimeout: -1})
	h.sigma1(t, addr1)
	connect, err := h.set.Networks().Get("unknown:connect")
	if err != nil {
		t.Fatal(err)
	}
	if n := connect.Semaphore.Running(); n != 1 {
		t.Errorf("unknown:connect runs %d slots, want 1", n)
	}
	// The first retransmission waits the padded backoff.
	iv := exchange.IntervalsOf(p.SessionParameters())
	h.clock.waitTimer(t, exchange.RetransmissionIntervalPadded(0, iv, false, 0.5, exchange.Pad{Additional: 1500 * time.Millisecond}))
	p.Close(closingCtx())
	eventually(t, "the slot released", func() bool { return connect.Semaphore.Running() == 0 })
}

// TestConnectionOnANamedNetwork: a connect's Network option names the profile
// the process throttles on — "icdLit" has no connect sub-profile, so its
// slot is the profile's own (PeerConnection.ts:110-118) — and a name no
// profile has fails the connect (NetworkProfile.ts:136-148).
func TestConnectionOnANamedNetwork(t *testing.T) {
	t.Parallel()
	h := newHarness(t, nil)
	p, _ := h.set.AddKnownPeer(Descriptor{Address: testAddr, OperationalAddress: udpAddr(addr1)})
	if _, err := p.Connect(context.Background(), ConnectOptions{Network: "nope"}); !errors.Is(err, ErrUnknownNetworkProfile) {
		t.Errorf("Connect on network nope: %v", err)
	}
	connectAsync(p, ConnectOptions{ConnectionTimeout: -1, Network: NetworkICDLit})
	h.sigma1(t, addr1)
	lit, _ := h.set.Networks().Get(NetworkICDLit)
	if lit.Semaphore.Running() != 1 {
		t.Errorf("icdLit runs %d slots, want 1", lit.Semaphore.Running())
	}
	p.Close(closingCtx())
}

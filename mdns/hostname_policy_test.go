// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package mdns

// The macOS host-name verdict (issue 465, notes/parity/by_design.md
// L7-D03): the OS host name is kept only when the system resolver answers
// it; otherwise the MAC-derived name of the chooser applies, as matter.js
// names its host on every platform (MdnsServer.ts:80, :608-610 at
// db021da0).

import (
	"context"
	"net"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock/clocktest"
)

// fakeLookup answers every name with one address when served, with an
// error otherwise, and counts its calls.
func fakeLookup(served bool, calls *atomic.Int32, asked *atomic.Value) lookupIPAddrFunc {
	return func(_ context.Context, host string) ([]net.IPAddr, error) {
		calls.Add(1)
		if asked != nil {
			asked.Store(host)
		}
		if !served {
			return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
		}
		return []net.IPAddr{{IP: net.ParseIP("fe80::1")}}, nil
	}
}

// policyWithEth0 is a policy for goos whose interfaces carry one usable
// MAC, aa:bb:cc:dd:ee:01.
func policyWithEth0(t *testing.T, goos string, lookup lookupIPAddrFunc) *hostNamePolicy {
	t.Helper()
	hw, err := net.ParseMAC("aa:bb:cc:dd:ee:01")
	if err != nil {
		t.Fatal(err)
	}
	eth0 := hostNameIface{flags: net.FlagUp | net.FlagMulticast, mac: hw, addrs: 1}
	return &hostNamePolicy{
		goos:    goos,
		lookup:  lookup,
		list:    func() ([]hostNameIface, error) { return []hostNameIface{eth0}, nil },
		chooser: &hostNameChooser{},
	}
}

const eth0HostName = "AABBCCDDEE010000"

func TestKeptOSHostName(t *testing.T) {
	t.Parallel()
	t.Run("darwin, served: the OS host name", func(t *testing.T) {
		t.Parallel()
		if got := keptOSHostName("darwin", "Lisa", func(string) bool { return true }); got != "Lisa" {
			t.Errorf("kept %q, want Lisa", got)
		}
	})
	t.Run("darwin, unanswered: none", func(t *testing.T) {
		t.Parallel()
		if got := keptOSHostName("darwin", "Lisa", func(string) bool { return false }); got != "" {
			t.Errorf("kept %q, want none", got)
		}
	})
	t.Run("not darwin: none, and the resolver is not asked", func(t *testing.T) {
		t.Parallel()
		var calls int
		for _, goos := range []string{"linux", "windows", "freebsd"} {
			if got := keptOSHostName(goos, "Lisa", func(string) bool { calls++; return true }); got != "" {
				t.Errorf("%s: kept %q, want none", goos, got)
			}
		}
		if calls != 0 {
			t.Errorf("served asked %d times off darwin, want 0", calls)
		}
	})
}

func TestHostNamePolicy(t *testing.T) {
	t.Parallel()
	osName := osHostName()
	t.Run("darwin, served: the OS host name, asked as <name>.local", func(t *testing.T) {
		t.Parallel()
		if osName == "" {
			t.Skip("no OS host name")
		}
		var calls atomic.Int32
		var asked atomic.Value
		p := policyWithEth0(t, "darwin", fakeLookup(true, &calls, &asked))
		if got := p.hostName(nil); got != osName {
			t.Errorf("host %q, want the OS host name %q", got, osName)
		}
		if got, _ := asked.Load().(string); got != osName+".local" {
			t.Errorf("resolver asked %q, want %q", got, osName+".local")
		}
	})
	t.Run("darwin, unanswered: the chooser's MAC-derived name", func(t *testing.T) {
		t.Parallel()
		if osName == "" {
			t.Skip("no OS host name")
		}
		var calls atomic.Int32
		p := policyWithEth0(t, "darwin", fakeLookup(false, &calls, nil))
		if got := p.hostName(nil); got != eth0HostName {
			t.Errorf("host %q, want %s", got, eth0HostName)
		}
	})
	t.Run("not darwin: the MAC-derived name, the resolver not asked", func(t *testing.T) {
		t.Parallel()
		var calls atomic.Int32
		p := policyWithEth0(t, "linux", fakeLookup(true, &calls, nil))
		if got := p.hostName(nil); got != eth0HostName {
			t.Errorf("host %q, want %s", got, eth0HostName)
		}
		if n := calls.Load(); n != 0 {
			t.Errorf("resolver asked %d times off darwin, want 0", n)
		}
	})
	t.Run("the verdict is taken once", func(t *testing.T) {
		t.Parallel()
		if osName == "" {
			t.Skip("no OS host name")
		}
		var calls atomic.Int32
		p := policyWithEth0(t, "darwin", fakeLookup(false, &calls, nil))
		for range 3 {
			if got := p.hostName(nil); got != eth0HostName {
				t.Errorf("host %q, want %s", got, eth0HostName)
			}
		}
		if n := calls.Load(); n != 1 {
			t.Errorf("resolver asked %d times, want once", n)
		}
	})
}

// TestOSHostNameServedDeadline: a resolver that never answers is
// "unanswered" once osHostNameLookupTimeout has passed on the clock.
func TestOSHostNameServedDeadline(t *testing.T) {
	t.Parallel()
	blocking := func(ctx context.Context, _ string) ([]net.IPAddr, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	clk := clocktest.New(testEpoch)
	done := make(chan bool, 1)
	go func() { done <- osHostNameServed(clk, blocking, "Lisa") }()
	for clk.Pending() == 0 {
		runtime.Gosched()
	}
	clk.Advance(osHostNameLookupTimeout - time.Millisecond)
	select {
	case <-done:
		t.Fatal("unanswered before the deadline")
	default:
	}
	clk.Advance(time.Millisecond)
	if <-done {
		t.Fatal("a resolver that never answers is served")
	}
}

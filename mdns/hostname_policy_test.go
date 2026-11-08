// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package mdns

// The macOS host-name verdict (issue 465, notes/parity/by_design.md
// L7-D03): the OS host name is kept only when the system resolver answers
// it with an address of this host; otherwise the MAC-derived name applies,
// as matter.js names its host on every platform (MdnsServer.ts:80,
// :608-610 at db021da0). The verdict is kept until the next retake, the
// Responder's on every interface change (MdnsServer.ts:40-43, :321-339).

import (
	"bytes"
	"context"
	"log/slog"
	"net"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock/clocktest"
)

// localTestAddr is the address the test host's interface carries;
// foreignTestAddr is another host's.
const (
	localTestAddr   = "192.0.2.10"
	foreignTestAddr = "192.0.2.99"
)

// fakeResolver answers every name with its addresses — none is an
// unanswered name — and counts its calls. It is safe for concurrent use.
type fakeResolver struct {
	mu    sync.Mutex
	addrs []string
	calls atomic.Int32
	asked atomic.Value
}

func newFakeResolver(addrs ...string) *fakeResolver { return &fakeResolver{addrs: addrs} }

// set replaces the answer.
func (f *fakeResolver) set(addrs ...string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.addrs = addrs
}

func (f *fakeResolver) lookup(_ context.Context, host string) ([]net.IPAddr, error) {
	f.calls.Add(1)
	f.asked.Store(host)
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.addrs) == 0 {
		return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
	}
	out := make([]net.IPAddr, 0, len(f.addrs))
	for _, a := range f.addrs {
		out = append(out, net.IPAddr{IP: net.ParseIP(a)})
	}
	return out, nil
}

// testPolicy is a policy for goos on the resolver res, whose host carries
// localTestAddr and, when addressed, one interface with a usable MAC,
// aa:bb:cc:dd:ee:01.
func testPolicy(t *testing.T, goos string, res *fakeResolver, addressed bool) *hostNamePolicy {
	t.Helper()
	hw, err := net.ParseMAC("aa:bb:cc:dd:ee:01")
	if err != nil {
		t.Fatal(err)
	}
	var ifaces []hostNameIface
	if addressed {
		ifaces = []hostNameIface{{flags: net.FlagUp | net.FlagMulticast, mac: hw, addrs: 1}}
	}
	return &hostNamePolicy{
		goos:   goos,
		lookup: res.lookup,
		localAddrs: func() ([]net.Addr, error) {
			return []net.Addr{&net.IPNet{IP: net.ParseIP(localTestAddr), Mask: net.CIDRMask(24, 32)}}, nil
		},
		list:    func() ([]hostNameIface, error) { return ifaces, nil },
		chooser: &hostNameChooser{},
	}
}

const eth0HostName = "AABBCCDDEE010000"

// testLog is a logger at debug level into the returned buffer.
func testLog() (*slog.Logger, *syncBuffer) {
	var b syncBuffer
	return slog.New(slog.NewTextHandler(&b, &slog.HandlerOptions{Level: slog.LevelDebug})), &b
}

// syncBuffer is a bytes.Buffer safe for concurrent use.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.String()
}

func TestHostNamePolicy(t *testing.T) {
	t.Parallel()
	osName := osHostName()
	if osName == "" {
		t.Skip("no OS host name")
	}
	t.Run("served by this host: the OS host name, asked as <name>.local", func(t *testing.T) {
		t.Parallel()
		res := newFakeResolver(localTestAddr)
		p := testPolicy(t, "darwin", res, true)
		if got := p.keptHostName(nil, eth0HostName); got != osName {
			t.Errorf("kept %q, want the OS host name %q", got, osName)
		}
		if got, _ := res.asked.Load().(string); got != osName+".local" {
			t.Errorf("resolver asked %q, want %q", got, osName+".local")
		}
	})
	t.Run("served with one local address among foreign ones: the OS host name", func(t *testing.T) {
		t.Parallel()
		p := testPolicy(t, "darwin", newFakeResolver(foreignTestAddr, localTestAddr), true)
		if got := p.keptHostName(nil, eth0HostName); got != osName {
			t.Errorf("kept %q, want the OS host name %q", got, osName)
		}
	})
	t.Run("served by another host: the MAC-derived name, one warning", func(t *testing.T) {
		t.Parallel()
		log, buf := testLog()
		p := testPolicy(t, "darwin", newFakeResolver(foreignTestAddr), true)
		for range 2 {
			if got := p.keptHostName(log, eth0HostName); got != "" {
				t.Errorf("kept %q, want none (a foreign answer)", got)
			}
		}
		out := buf.String()
		if strings.Count(out, "mdns.hostname.os_foreign") != 1 || !strings.Contains(out, "level=WARN") ||
			!strings.Contains(out, "addresses="+foreignTestAddr) || !strings.Contains(out, "fallback="+eth0HostName) {
			t.Errorf("want one os_foreign warning with addrs and fallback, got:\n%s", out)
		}
	})
	t.Run("unanswered: the MAC-derived name", func(t *testing.T) {
		t.Parallel()
		log, buf := testLog()
		p := testPolicy(t, "darwin", newFakeResolver(), true)
		if got := p.keptHostName(log, eth0HostName); got != "" {
			t.Errorf("kept %q, want none", got)
		}
		if out := buf.String(); !strings.Contains(out, "mdns.hostname.os_unanswered") {
			t.Errorf("want an os_unanswered line, got:\n%s", out)
		}
	})
	t.Run("not darwin: nothing kept, the resolver not asked", func(t *testing.T) {
		t.Parallel()
		for _, goos := range []string{"linux", "windows", "freebsd"} {
			res := newFakeResolver(localTestAddr)
			p := testPolicy(t, goos, res, true)
			if got := p.keptHostName(nil, eth0HostName); got != "" {
				t.Errorf("%s: kept %q, want none", goos, got)
			}
			if n := res.calls.Load(); n != 0 {
				t.Errorf("%s: resolver asked %d times, want 0", goos, n)
			}
		}
	})
	t.Run("the verdict is kept after an addressed interface", func(t *testing.T) {
		t.Parallel()
		res := newFakeResolver()
		p := testPolicy(t, "darwin", res, true)
		for range 3 {
			p.keptHostName(nil, eth0HostName)
		}
		if n := res.calls.Load(); n != 1 {
			t.Errorf("resolver asked %d times, want once", n)
		}
	})
	t.Run("without an addressed interface the verdict is not kept", func(t *testing.T) {
		t.Parallel()
		log, buf := testLog()
		res := newFakeResolver()
		p := testPolicy(t, "darwin", res, false)
		for range 2 {
			p.keptHostName(log, eth0HostName)
		}
		if n := res.calls.Load(); n != 2 {
			t.Errorf("resolver asked %d times, want 2", n)
		}
		out := buf.String()
		if !strings.Contains(out, "mdns.hostname.verdict_deferred") || strings.Contains(out, "level=WARN") {
			t.Errorf("want verdict_deferred lines and no warning, got:\n%s", out)
		}
	})
	t.Run("a served verdict is kept without an addressed interface", func(t *testing.T) {
		t.Parallel()
		res := newFakeResolver(localTestAddr)
		p := testPolicy(t, "darwin", res, false)
		for range 2 {
			if got := p.keptHostName(nil, eth0HostName); got != osName {
				t.Errorf("kept %q, want %q", got, osName)
			}
		}
		if n := res.calls.Load(); n != 1 {
			t.Errorf("resolver asked %d times, want once", n)
		}
	})
	t.Run("retake asks again", func(t *testing.T) {
		t.Parallel()
		res := newFakeResolver(localTestAddr)
		p := testPolicy(t, "darwin", res, true)
		if got := p.keptHostName(nil, eth0HostName); got != osName {
			t.Fatalf("kept %q, want %q", got, osName)
		}
		res.set()
		if got := p.keptHostName(nil, eth0HostName); got != osName {
			t.Errorf("kept %q before the retake, want the kept %q", got, osName)
		}
		p.retake()
		if got := p.keptHostName(nil, eth0HostName); got != "" {
			t.Errorf("kept %q after the retake, want none", got)
		}
		if n := res.calls.Load(); n != 2 {
			t.Errorf("resolver asked %d times, want 2", n)
		}
	})
}

// TestDefaultHostNameIsMACDerived: the builders' default asks no resolver
// and has the 16-hex shape on every platform.
func TestDefaultHostNameIsMACDerived(t *testing.T) {
	t.Parallel()
	h, addressed := macHostName(testPolicy(t, "darwin", newFakeResolver(), true).list, &hostNameChooser{})
	if h != eth0HostName || !addressed {
		t.Errorf("macHostName = %q, %v, want %s, true", h, addressed, eth0HostName)
	}
	h, addressed = macHostName(testPolicy(t, "darwin", newFakeResolver(), false).list, &hostNameChooser{})
	if len(h) != 16 || strings.ToUpper(h) != h || addressed {
		t.Errorf("macHostName without an interface = %q, %v, want 16 uppercase hex digits, false", h, addressed)
	}
}

// TestOSHostNameVerdictDeadline: a resolver that never answers is
// "unanswered" once osHostNameLookupTimeout has passed on the clock.
func TestOSHostNameVerdictDeadline(t *testing.T) {
	t.Parallel()
	blocking := func(ctx context.Context, _ string) ([]net.IPAddr, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	noLocal := func() ([]net.Addr, error) { return nil, nil }
	clk := clocktest.New(testEpoch)
	done := make(chan hostNameVerdict, 1)
	go func() { v, _ := osHostNameVerdict(clk, blocking, noLocal, "Lisa"); done <- v }()
	for clk.Pending() == 0 {
		runtime.Gosched()
	}
	clk.Advance(osHostNameLookupTimeout - time.Millisecond)
	select {
	case <-done:
		t.Fatal("a verdict before the deadline")
	default:
	}
	clk.Advance(time.Millisecond)
	if v := <-done; v != verdictUnanswered {
		t.Fatalf("a resolver that never answers gives verdict %d, want unanswered", v)
	}
}

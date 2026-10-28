// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package discovery

import (
	"context"
	"errors"
	"testing"

	"github.com/miekg/dns"
)

const (
	eType = "_esphomelib._tcp.local"
	eInst = "kitchen-proxy." + eType
	eHost = "kitchen-proxy.local"
)

type browseResult struct {
	instances []Instance
	err       error
}

func startBrowse(ctx context.Context, n *Names, st string) <-chan browseResult {
	done := make(chan browseResult, 1)
	go func() {
		in, err := n.Browse(ctx, st)
		done <- browseResult{in, err}
	}()
	return done
}

func TestBrowse_FindsAForeignServiceType(t *testing.T) {
	t.Parallel()
	n, conn, _ := testNames(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := startBrowse(ctx, n, eType+".")
	waitFor(t, func() bool { return asked(t, conn, eType, TypePTR) })
	inject(t, conn, 0,
		[]dns.RR{ptrRR(eType, eInst, 4500)},
		srvRR(eInst, eHost, 6053, 120, true),
		txtRR(eInst, 4500, true, "mac=aabbccddeeff", "version=2026.10.0", "api_encryption=Noise_NNpsk0_25519_ChaChaPoly_SHA256"),
		aRR(eHost, "192.0.2.20", 120, true),
		// An unrelated service in the same packet stays out of the cache.
		srvRR("printer._ipp._tcp.local", "printer.local", 631, 120, true),
	)
	waitFor(t, func() bool { return n.Has(eInst) })
	cancel()
	r := <-done
	if r.err != nil || len(r.instances) != 1 {
		t.Fatalf("browse = %+v", r)
	}
	in := r.instances[0]
	if in.Name != "kitchen-proxy" || in.QName != eInst {
		t.Fatalf("instance = %+v", in)
	}
	if got := addrStrings(in.Addresses); !equalStrings(got, []string{"192.0.2.20:6053"}) {
		t.Fatalf("addresses = %v", got)
	}
	if mac, _ := in.Parameters.Get("mac"); mac != "aabbccddeeff" {
		t.Fatalf("mac = %q", mac)
	}
	if n.Has("printer._ipp._tcp.local") {
		t.Fatal("a record of another service type must not enter the cache")
	}
}

func TestBrowse_AsksForWhatAnInstanceLacks(t *testing.T) {
	t.Parallel()
	n, conn, _ := testNames(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := startBrowse(ctx, n, eType)
	waitFor(t, func() bool { return asked(t, conn, eType, TypePTR) })
	inject(t, conn, 0, []dns.RR{ptrRR(eType, eInst, 4500)})
	waitFor(t, func() bool { return asked(t, conn, eInst, TypeSRV) && asked(t, conn, eInst, TypeTXT) })
	inject(t, conn, 0, []dns.RR{srvRR(eInst, eHost, 6053, 120, true), txtRR(eInst, 4500, true, "mac=aabbccddeeff")})
	waitFor(t, func() bool { return asked(t, conn, eHost, TypeA) && asked(t, conn, eHost, TypeAAAA) })
	inject(t, conn, 7, []dns.RR{aaaaRR(eHost, "fe80::20", 120, true)})
	waitFor(t, func() bool {
		var ok bool
		onLoop(t, n, func() { ok = n.has(eHost) && n.get(eHost).isDiscovered() })
		return ok
	})
	cancel()
	r := <-done
	if r.err != nil || len(r.instances) != 1 {
		t.Fatalf("browse = %+v", r)
	}
	if got := addrStrings(r.instances[0].Addresses); !equalStrings(got, []string{"[fe80::20%eth7]:6053"}) {
		t.Fatalf("addresses = %v", got)
	}
}

func TestBrowse_CachedInstancesAndWithdrawals(t *testing.T) {
	t.Parallel()
	n, conn, clk := testNames(t)
	other := "hall-proxy." + eType
	ctx, cancel := context.WithCancel(t.Context())
	done := startBrowse(ctx, n, eType)
	waitFor(t, func() bool { return asked(t, conn, eType, TypePTR) })
	inject(t, conn, 0,
		[]dns.RR{ptrRR(eType, eInst, 4500), ptrRR(eType, other, 4500)},
		srvRR(eInst, eHost, 6053, 120, true), aRR(eHost, "192.0.2.20", 120, true),
		// hall-proxy's SRV target never resolves: it is not returned.
		srvRR(other, "hall-proxy.local", 6053, 120, true),
	)
	waitFor(t, func() bool { return n.Has(eInst) && n.Has(other) })
	cancel()
	if r := <-done; r.err != nil || len(r.instances) != 1 || r.instances[0].Name != "kitchen-proxy" {
		t.Fatalf("first browse = %+v", r)
	}

	// A second browse counts the cached instance at once.
	conn.reset()
	ctx2, cancel2 := context.WithCancel(t.Context())
	done = startBrowse(ctx2, n, eType)
	waitFor(t, func() bool { return asked(t, conn, eType, TypePTR) })
	cancel2()
	if r := <-done; r.err != nil || len(r.instances) != 1 {
		t.Fatalf("second browse = %+v", r)
	}

	// A goodbye for the PTR withdraws the instance once it takes effect.
	ctx3, cancel3 := context.WithCancel(t.Context())
	defer cancel3()
	done = startBrowse(ctx3, n, eType)
	waitFor(t, func() bool { return asked(t, conn, eType, TypePTR) })
	inject(t, conn, 0, []dns.RR{ptrRR(eType, eInst, 0)})
	waitFor(t, func() bool {
		var pending bool
		onLoop(t, n, func() { pending = len(n.expiring) > 0 })
		return pending
	})
	clk.advance(n, DefaultEvictionDelay)
	cancel3()
	if r := <-done; r.err != nil || len(r.instances) != 0 {
		t.Fatalf("browse after the goodbye = %+v", r)
	}
}

func TestBrowse_Refusals(t *testing.T) {
	t.Parallel()
	n, _, _ := testNames(t)
	if _, err := n.Browse(t.Context(), " "); !errors.Is(err, errServiceType) {
		t.Fatalf("empty type = %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := startBrowse(ctx, n, eType)
	_ = n.Close()
	if r := <-done; !errors.Is(r.err, ErrClosed) {
		t.Fatalf("closed cache = %+v", r)
	}
}

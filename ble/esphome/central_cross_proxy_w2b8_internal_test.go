// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package esphome

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"thingfabric.dev/go-fabric/contract"
	"thingfabric.dev/go-fabric/controller/ble"
	"thingfabric.dev/go-fabric/controller/ble/bletest"

	"thingfabric.dev/go-fabric/ble/esphome/esphometest"
)

const crossAddrW2B8 = "C0:FF:EE:00:06:75"

// crossRigW2B8 is one peripheral seen by two proxies, "one" and "two". The
// peripheral is a node's radio whose GATT service is registered but not
// yet served, so a proxy's connect to it stays in progress until gate.
type crossRigW2B8 struct {
	t      *testing.T
	c      *Central
	f1, f2 *esphometest.Proxy
	p1, p2 *proxy
	addr   uint64
	svc    contract.GATTService
	// reports gets a token at every advertisement the central reports:
	// the tick the rig's waits re-check their condition on.
	reports    chan struct{}
	gateOnce   sync.Once
	serveStop  context.CancelFunc
	serveEnded chan struct{}
}

func newCrossRigW2B8(t *testing.T) *crossRigW2B8 {
	t.Helper()
	per := &bletest.Peripheral{Address: crossAddrW2B8, Discriminator: 3840, VendorID: 0xFFF1, ProductID: 0x8001}
	svc, err := per.Radio().RegisterService(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	newFake := func(rssi int32) *esphometest.Proxy {
		f, err := esphometest.New(esphometest.Options{RSSI: rssi}, per)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(f.Close)
		return f
	}
	r := &crossRigW2B8{t: t, f1: newFake(-40), f2: newFake(-80), svc: svc, serveEnded: make(chan struct{}), reports: make(chan struct{}, 1)}
	// Runs before the fakes close: a connect held at the radio returns.
	t.Cleanup(func() {
		r.gate()
		r.serveStop()
		<-r.serveEnded
	})
	c, err := NewCentral([]Proxy{{Addr: r.f1.Addr(), Name: "one"}, {Addr: r.f2.Addr(), Name: "two"}}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	r.c = c
	for _, p := range c.proxyList() {
		switch p.name {
		case "one":
			r.p1 = p
		case "two":
			r.p2 = p
		}
	}
	if r.addr, err = parseMAC(crossAddrW2B8); err != nil {
		t.Fatal(err)
	}
	c.SetDiscoveryCallback(func(ble.Peripheral, []byte) {
		select {
		case r.reports <- struct{}{}:
		default:
		}
	})
	if err := c.StartScanning(context.Background()); err != nil {
		t.Fatal(err)
	}
	r.waitRSSI(r.p1, -40)
	r.waitRSSI(r.p2, -80)
	return r
}

// gate serves the radio's GATT service: connects held there complete.
func (r *crossRigW2B8) gate() {
	r.gateOnce.Do(func() {
		ctx, cancel := context.WithCancel(context.Background())
		r.serveStop = cancel
		go func() {
			defer close(r.serveEnded)
			_ = r.svc.Serve(ctx, func(contract.BLEConnection) {})
		}()
	})
}

// waitRSSI waits until the central holds p's report of the peripheral at rssi.
func (r *crossRigW2B8) waitRSSI(p *proxy, rssi int32) {
	r.t.Helper()
	r.await("the report of "+p.name, func() bool {
		r.c.mu.Lock()
		defer r.c.mu.Unlock()
		s, ok := r.c.sightings[r.addr][p]
		return ok && s.rssi == rssi
	})
}

// preferTwo makes proxy "two" the one choose picks.
func (r *crossRigW2B8) preferTwo() {
	r.t.Helper()
	r.f2.SetRSSI(crossAddrW2B8, -20)
	r.waitRSSI(r.p2, -20)
	if px, _, err := r.c.choose(r.addr); err != nil || px != r.p2 {
		r.t.Fatalf("choose = %v, %v; want proxy two", px, err)
	}
}

func connectsW2B8(f *esphometest.Proxy) int {
	n := 0
	for _, e := range f.Events() {
		if strings.HasPrefix(e, "connect "+crossAddrW2B8) {
			n++
		}
	}
	return n
}

// await re-checks cond at every advertisement the central reports — the
// proxies forward a batch every 50 ms while the scan runs — and fails the
// test when cond still does not hold after 10 s.
func (r *crossRigW2B8) await(what string, cond func() bool) {
	r.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for !cond() {
		select {
		case <-r.reports:
		case <-ctx.Done():
			r.t.Fatalf("timed out waiting for %s", what)
		}
	}
}

// secondThroughTwo is a Connect for the address that choose sends through
// proxy "two"; it wants the refusal want and no CONNECT at proxy two.
func (r *crossRigW2B8) secondThroughTwo(want error) {
	r.t.Helper()
	r.preferTwo()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	k, err := r.c.Connect(ctx, ble.Peripheral{Address: crossAddrW2B8})
	if err == nil {
		_ = k.Disconnect()
		r.t.Fatal("a second Connect for the address through another proxy succeeded")
	}
	if !errors.Is(err, want) {
		r.t.Fatalf("second Connect through another proxy: %v, want %v", err, want)
	}
	if n := connectsW2B8(r.f2); n != 0 {
		r.t.Fatalf("%d CONNECTs reached proxy two, want 0: %q", n, r.f2.Events())
	}
}

// released waits until the central holds no reservation for the address.
func (r *crossRigW2B8) released() {
	r.t.Helper()
	r.await("the reservation's release", func() bool {
		r.c.mu.Lock()
		defer r.c.mu.Unlock()
		return r.c.holds[r.addr] == nil
	})
}

// TestConnectForOneAddressIsRefusedAcrossProxies: the central reserves the
// peripheral's address before it chooses the proxy, so a second Connect for
// it through another proxy is refused while the first is in progress or up
// (decision 675, P8-esphome-cross-proxy), and the address is free again
// once the first failed or closed.
func TestConnectForOneAddressIsRefusedAcrossProxies(t *testing.T) {
	t.Run("in progress, then up, then closed", func(t *testing.T) {
		r := newCrossRigW2B8(t)
		type result struct {
			k   ble.Connection
			err error
		}
		first := make(chan result, 1)
		go func() {
			k, err := r.c.Connect(context.Background(), ble.Peripheral{Address: crossAddrW2B8})
			first <- result{k, err}
		}()
		r.await("the first CONNECT at proxy one", func() bool { return connectsW2B8(r.f1) == 1 })
		r.secondThroughTwo(errConnectInProgress)

		r.gate()
		res := <-first
		if res.err != nil {
			t.Fatalf("first Connect: %v", res.err)
		}
		r.secondThroughTwo(errAlreadyConnected)

		if err := res.k.Disconnect(); err != nil {
			t.Fatal(err)
		}
		r.released()
		k, err := r.c.Connect(context.Background(), ble.Peripheral{Address: crossAddrW2B8})
		if err != nil {
			t.Fatalf("Connect after the first closed: %v", err)
		}
		_ = k.Disconnect()
		if n := connectsW2B8(r.f2); n != 1 {
			t.Fatalf("%d CONNECTs reached proxy two, want 1", n)
		}
	})

	t.Run("failed", func(t *testing.T) {
		r := newCrossRigW2B8(t)
		ctx, cancel := context.WithCancel(context.Background())
		first := make(chan error, 1)
		go func() {
			_, err := r.c.Connect(ctx, ble.Peripheral{Address: crossAddrW2B8})
			first <- err
		}()
		r.await("the first CONNECT at proxy one", func() bool { return connectsW2B8(r.f1) == 1 })
		cancel()
		if err := <-first; !errors.Is(err, context.Canceled) {
			t.Fatalf("first Connect: %v, want context.Canceled", err)
		}
		r.released()
		r.preferTwo()
		r.gate()
		k, err := r.c.Connect(context.Background(), ble.Peripheral{Address: crossAddrW2B8})
		if err != nil {
			t.Fatalf("Connect after the first failed: %v", err)
		}
		_ = k.Disconnect()
		if n := connectsW2B8(r.f2); n != 1 {
			t.Fatalf("%d CONNECTs reached proxy two, want 1", n)
		}
	})
}

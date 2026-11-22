// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package esphome

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"sync"
	"testing"
	"time"

	"thingfabric.dev/go-fabric/client/discovery"
	"thingfabric.dev/go-fabric/clock"
	"thingfabric.dev/go-fabric/clock/clocktest"

	"thingfabric.dev/go-fabric/ble/esphome/internal/wire"
)

// bareCentral is a central with no proxy and no run loop, for driving its
// handlers directly.
func bareCentral(t *testing.T, o Options) *Central {
	t.Helper()
	o.normalize()
	c := &Central{opts: o, log: o.Logger, clk: o.Clock, sightings: map[uint64]map[*proxy]sighting{}, skipped: map[string]string{}}
	c.ctx, c.cancel = context.WithCancel(t.Context())
	t.Cleanup(c.cancel)
	if o.Discover != nil {
		d, err := o.Discover.normalized()
		if err != nil {
			t.Fatal(err)
		}
		c.disc = &d
	}
	return c
}

// bareProxy is a proxy of c without an API connection: what it sends is
// dropped.
func bareProxy(t *testing.T, c *Central, name string) *proxy {
	t.Helper()
	p, err := newProxy(c, Proxy{Addr: "127.0.0.1:1", Name: name})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

var netipLoopback = netip.MustParseAddr("127.0.0.1")

// instances are n advertised devices, each at its own address.
func instances(n int) []discovery.Instance {
	out := make([]discovery.Instance, n)
	for i := range out {
		name := fmt.Sprintf("dev-%04d", i)
		out[i] = discovery.Instance{Name: name, QName: name + "._esphomelib._tcp.local", Addresses: []discovery.Address{{IP: netipLoopback, Port: uint16(10000 + i)}}} //nolint:gosec // i < n, a test's few hundred
	}
	return out
}

// TestPeerFedStateIsBounded is the guard of the class "state a proxy or a
// LAN host makes the central hold": every collection the central fills
// from what a peer sends or announces has a bound, and past it the central
// sheds the excess or ends the connection that sent it. One case per site;
// a new site gets a case.
func TestPeerFedStateIsBounded(t *testing.T) {
	// R2-P6-bound-2002: the services of a GATT database.
	t.Run("gatt database", func(t *testing.T) {
		c := bareCentral(t, Options{})
		p := bareProxy(t, c, "flood")
		k := newConnection(p, 0x0a0b0c0d0e0f)
		o := &op{name: "services", services: true, done: make(chan reply, 1)}
		k.pending = o
		svc := wire.GATTService{UUID: wire.UUID{Short: 0x1800}, Handle: 1, Characteristics: []wire.GATTCharacteristic{
			{UUID: wire.UUID{Short: 0x2a00}, Handle: 2, Descriptors: []wire.GATTDescriptor{{UUID: wire.UUID{Short: 0x2902}, Handle: 3}}},
		}}
		frame := &wire.BluetoothGATTGetServicesResponse{Services: make([]wire.GATTService, 100)}
		for i := range frame.Services {
			frame.Services[i] = svc
		}
		for range 1000 {
			k.onReply(wire.TypeBluetoothGATTGetServicesResponse, &wire.AddressHandle{}, frame)
		}
		if n := attributes(o.got); n > maxGATTAttributes {
			t.Fatalf("the database holds %d attributes, bound %d", n, maxGATTAttributes)
		}
		select {
		case r := <-o.done:
			if !errors.Is(r.err, errGATTDatabaseTooLarge) {
				t.Fatalf("services reply %v, want errGATTDatabaseTooLarge", r.err)
			}
		default:
			t.Fatal("the services operation did not fail past the bound")
		}
		if !k.isGone() {
			t.Fatal("the connection outlived a database past the bound")
		}
	})

	// R2-P6-bound-2003: the sightings of advertising addresses.
	t.Run("sightings", func(t *testing.T) {
		clk := clocktest.New(time.Unix(1_700_000_000, 0))
		c := bareCentral(t, Options{Clock: clk})
		p := bareProxy(t, c, "proxy")
		for i := range uint64(20_000) {
			c.discovered(p, 0x100000+i, 1, -60, nil)
			if i%1000 == 0 {
				clk.Advance(time.Second)
			}
		}
		c.mu.Lock()
		n := len(c.sightings)
		_, newest := c.sightings[0x100000+19_999]
		c.mu.Unlock()
		if n > maxSightings {
			t.Fatalf("%d sightings, bound %d", n, maxSightings)
		}
		if !newest {
			t.Fatal("the newest sighting was shed")
		}
	})

	// R2-P6-bound-2004: the indications queued for a slow subscriber.
	t.Run("indication queue", func(t *testing.T) {
		c := bareCentral(t, Options{})
		p := bareProxy(t, c, "proxy")
		k := newConnection(p, 0x0a0b0c0d0e0f)
		k.c2 = 0x10
		release := make(chan struct{})
		defer close(release)
		k.on = func([]byte) { <-release }
		for range 10_000 {
			k.onNotify(k.c2, []byte{0})
		}
		k.mu.Lock()
		n := len(k.queue)
		k.mu.Unlock()
		if n > maxQueuedIndications {
			t.Fatalf("%d indications queued, bound %d", n, maxQueuedIndications)
		}
		if !k.isGone() {
			t.Fatal("the connection outlived a queue past the bound")
		}
	})

	// R2-P6-bound-2009: the refused names, and the dials one browse starts.
	t.Run("refused names", func(t *testing.T) {
		b := &swapBrowser{out: instances(100)}
		c := bareCentral(t, Options{Discover: &DiscoverOptions{Browser: b}})
		c.rescan(t.Context())
		c.setMu.Lock()
		before := len(c.skipped)
		c.setMu.Unlock()
		if before != 100 {
			t.Fatalf("%d refused, want the 100 keyless devices", before)
		}
		b.set(nil)
		c.rescan(t.Context())
		c.setMu.Lock()
		after := len(c.skipped)
		c.setMu.Unlock()
		if after != 0 {
			t.Fatalf("%d refused names kept after they left the LAN", after)
		}
	})
	t.Run("concurrent dials", func(t *testing.T) {
		for _, via := range []string{"Discover", "Central"} {
			t.Run(via, func(t *testing.T) {
				var d dialGauge
				o := Options{Dial: d.dial, Discover: &DiscoverOptions{Browser: &swapBrowser{out: instances(64)}, AllowPlaintext: true}}
				if via == "Discover" {
					_, _ = Discover(t.Context(), o)
				} else {
					bareCentral(t, o).rescan(t.Context())
				}
				if m := d.peak(); m > maxConcurrentDials {
					t.Fatalf("%d dials in flight, bound %d", m, maxConcurrentDials)
				}
			})
		}
	})
}

// attributes counts the services, characteristics and descriptors of a
// database.
func attributes(ss []wire.GATTService) int {
	n := len(ss)
	for _, s := range ss {
		n += len(s.Characteristics)
		for _, ch := range s.Characteristics {
			n += len(ch.Descriptors)
		}
	}
	return n
}

// swapBrowser answers with instances the test replaces.
type swapBrowser struct {
	mu  sync.Mutex
	out []discovery.Instance
}

func (b *swapBrowser) Browse(context.Context, string) ([]discovery.Instance, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.out, nil
}

func (b *swapBrowser) set(out []discovery.Instance) {
	b.mu.Lock()
	b.out = out
	b.mu.Unlock()
}

// dialGauge is an Options.Dial that fails every dial after a moment and
// records how many were in flight at once.
type dialGauge struct {
	mu        sync.Mutex
	in, maxIn int
}

func (d *dialGauge) dial(ctx context.Context, _, _ string) (net.Conn, error) {
	d.mu.Lock()
	d.in++
	d.maxIn = max(d.maxIn, d.in)
	d.mu.Unlock()
	t := clock.System{}.NewTimer(5 * time.Millisecond)
	defer t.Stop()
	select {
	case <-t.C():
	case <-ctx.Done():
	}
	d.mu.Lock()
	d.in--
	d.mu.Unlock()
	return nil, errors.New("unreachable")
}

func (d *dialGauge) peak() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.maxIn
}

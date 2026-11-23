// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package esphome_test

import (
	"context"
	"fmt"
	"log/slog"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"

	"thingfabric.dev/go-fabric/client/discovery"

	"thingfabric.dev/go-fabric/ble/esphome"
	"thingfabric.dev/go-fabric/ble/esphome/esphometest"
)

// sameDeviceSkips counts the proxy_skipped lines of a same-device alias:
// each is a completed dial of the device the alias names.
func (c *capture) sameDeviceSkips() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for _, r := range c.recs {
		if r.msg == "ble.esphome.proxy_skipped" && strings.Contains(r.reason, "same MAC") {
			n++
		}
	}
	return n
}

// TestSameDeviceAliasesAreDialledOnce holds R3-bound-3302 (a regression of
// R2-C2-bound-2003's fix): an alias found to be a device of the set is
// remembered, so a rescan does not dial it again, and a remembered alias
// takes no room — a second device announced after more aliases than the
// room holds joins at a later browse.
func TestSameDeviceAliasesAreDialledOnce(t *testing.T) {
	const aliases = 40
	x := fake(t, esphometest.Options{Name: "x", MAC: "AABBCCDDEE31", Key: testKey})
	y := fake(t, esphometest.Options{Name: "y", MAC: "AABBCCDDEE32", Key: testKey})
	xa, ya := netip.MustParseAddrPort(x.Addr()), netip.MustParseAddrPort(y.Addr())
	b := make(listBrowser, 0, aliases+2)
	b = append(b, instanceAt("x-proxy", "aabbccddee31", xa))
	for i := range aliases {
		b = append(b, instanceAt(fmt.Sprintf("x-alias-%03d", i), "aabbccddee31", xa))
	}
	b = append(b, instanceAt("y-proxy", "aabbccddee32", ya))
	log := &capture{}
	central(t, esphome.Options{Logger: slog.New(log), Discover: &esphome.DiscoverOptions{
		Browser: b, Keys: esphome.Keys{"AA:BB:CC:DD:EE:31": testKey, "AA:BB:CC:DD:EE:32": testKey},
		Rescan: 40 * time.Millisecond, Timeout: 20 * time.Millisecond,
	}})
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, ok := log.find("ble.esphome.proxy_discovered", "y-proxy"); ok {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the second device never joined behind %d aliases (%d same-device dials)", aliases, log.sameDeviceSkips())
		}
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(300 * time.Millisecond) // several more browses
	if got := log.sameDeviceSkips(); got > aliases {
		t.Errorf("%d same-device dials for %d aliases, want each alias dialled at most once", got, aliases)
	}
	if got := log.count("ble.esphome.proxy_cap"); got > aliases+1 {
		t.Errorf("%d proxy_cap lines, want each name logged at most once", got)
	}
	if got := log.count("ble.esphome.proxy_discovered"); got != 2 {
		t.Errorf("%d proxies discovered, want 2", got)
	}
}

// stagedBrowser answers its first browse with first and every later one
// with first and then.
type stagedBrowser struct {
	mu          sync.Mutex
	n           int
	first, then []discovery.Instance
}

func (b *stagedBrowser) Browse(ctx context.Context, _ string) ([]discovery.Instance, error) {
	b.mu.Lock()
	b.n++
	n := b.n
	b.mu.Unlock()
	<-ctx.Done()
	if n == 1 {
		return b.first, nil
	}
	return append(append([]discovery.Instance{}, b.first...), b.then...), nil
}

// TestPlaintextMACDoesNotShadowAKeyedProxy holds R3-sec-3303 (a regression
// of R2-C2-bound-2003's fix): a MAC stated over the plaintext API
// identifies nothing, so a plaintext device in the set that states a
// keyed proxy's MAC does not keep the keyed proxy out.
func TestPlaintextMACDoesNotShadowAKeyedProxy(t *testing.T) {
	hostile := fake(t, esphometest.Options{Name: "hostile", MAC: "AABBCCDDEE41"})
	keyed := fake(t, esphometest.Options{Name: "keyed", MAC: "AABBCCDDEE41", Key: testKey})
	plain := discovery.Instance{
		Name: "hostile", QName: "hostile._esphomelib._tcp.local",
		Addresses:  []discovery.Address{{IP: netip.MustParseAddrPort(hostile.Addr()).Addr(), Port: netip.MustParseAddrPort(hostile.Addr()).Port()}},
		Parameters: discovery.Parameters{"mac": []byte("112233445566")},
	}
	b := &stagedBrowser{
		first: []discovery.Instance{plain},
		then:  []discovery.Instance{instanceAt("keyed-proxy", "aabbccddee41", netip.MustParseAddrPort(keyed.Addr()))},
	}
	log := &capture{}
	central(t, esphome.Options{Logger: slog.New(log), Discover: &esphome.DiscoverOptions{
		Browser: b, Keys: esphome.Keys{"keyed-proxy": testKey}, AllowPlaintext: true,
		Rescan: 40 * time.Millisecond, Timeout: 20 * time.Millisecond,
	}})
	if _, ok := log.find("ble.esphome.proxy_discovered", "hostile"); !ok {
		t.Fatal("the plaintext device did not join first")
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, ok := log.find("ble.esphome.proxy_discovered", "keyed-proxy"); ok {
			break
		}
		if time.Now().After(deadline) {
			r, _ := log.find("ble.esphome.proxy_skipped", "keyed-proxy")
			t.Fatalf("the keyed proxy was kept out by a plaintext device stating its MAC (%q)", r.reason)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestDiscoverPrefersTheKeyedEntry holds R3-sec-3303 for the one-shot
// Discover: a plaintext device stating a keyed device's MAC is the one
// left out, whatever the browse order.
func TestDiscoverPrefersTheKeyedEntry(t *testing.T) {
	hostile := fake(t, esphometest.Options{Name: "hostile", MAC: "AABBCCDDEE51"})
	keyed := fake(t, esphometest.Options{Name: "keyed", MAC: "AABBCCDDEE51", Key: testKey})
	ha := netip.MustParseAddrPort(hostile.Addr())
	b := listBrowser{
		{
			Name: "hostile", QName: "hostile._esphomelib._tcp.local",
			Addresses:  []discovery.Address{{IP: ha.Addr(), Port: ha.Port()}},
			Parameters: discovery.Parameters{"mac": []byte("112233445566")},
		},
		instanceAt("keyed-proxy", "aabbccddee51", netip.MustParseAddrPort(keyed.Addr())),
	}
	ps, err := esphome.Discover(context.Background(), esphome.Options{Logger: slog.New(&capture{}), Discover: &esphome.DiscoverOptions{
		Browser: b, Keys: esphome.Keys{"keyed-proxy": testKey}, AllowPlaintext: true, Timeout: 20 * time.Millisecond,
	}})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if got := proxyNames(ps); len(got) != 1 || got[0] != "keyed-proxy" {
		t.Errorf("Discover returned %v, want only the keyed proxy", got)
	}
}

// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package esphome_test

import (
	"context"
	"fmt"
	"log/slog"
	"net/netip"
	"testing"
	"time"

	"thingfabric.dev/go-fabric/client/discovery"

	"thingfabric.dev/go-fabric/ble/esphome"
	"thingfabric.dev/go-fabric/ble/esphome/esphometest"
)

// listBrowser answers every browse with its instances.
type listBrowser []discovery.Instance

func (b listBrowser) Browse(ctx context.Context, _ string) ([]discovery.Instance, error) {
	<-ctx.Done()
	return b, nil
}

func instanceAt(name, mac string, addr netip.AddrPort) discovery.Instance {
	return discovery.Instance{
		Name: name, QName: name + "._esphomelib._tcp.local",
		Addresses:  []discovery.Address{{IP: addr.Addr(), Port: addr.Port()}},
		Parameters: discovery.Parameters{"mac": []byte(mac), "api_encryption": []byte("Noise_NNpsk0_25519_ChaChaPoly_SHA256")},
	}
}

func (c *capture) count(msg string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for _, r := range c.recs {
		if r.msg == msg {
			n++
		}
	}
	return n
}

// TestDiscoveredAliasesOfOneDeviceAreOneProxy holds R2-C2-bound-2003: n
// instance names announced for one device (its TXT mac and address, the
// key held by MAC) become one proxy with one API connection — the device
// is identified by the MAC its DeviceInfoResponse states on the keyed
// connection, not by the instance name.
func TestDiscoveredAliasesOfOneDeviceAreOneProxy(t *testing.T) {
	const n = 8
	dev := fake(t, esphometest.Options{Name: "real-proxy", MAC: "AABBCCDDEE21", Key: testKey})
	ap := netip.MustParseAddrPort(dev.Addr())
	b := make(listBrowser, 0, n)
	for i := range n {
		b = append(b, instanceAt(fmt.Sprintf("alias-%03d", i), "aabbccddee21", ap))
	}
	log := &capture{}
	central(t, esphome.Options{Logger: slog.New(log), Discover: &esphome.DiscoverOptions{
		Browser: b, Keys: esphome.Keys{"AA:BB:CC:DD:EE:21": testKey}, Rescan: time.Hour, Timeout: 50 * time.Millisecond,
	}})
	if got := log.count("ble.esphome.proxy_discovered"); got != 1 {
		t.Fatalf("%d proxies discovered for one device under %d names, want 1", got, n)
	}
	eventually(t, "one API client on the device", func() bool { return dev.Clients() == 1 })

	ps, err := esphome.Discover(context.Background(), esphome.Options{Logger: slog.New(&capture{}), Discover: &esphome.DiscoverOptions{
		Browser: b, Keys: esphome.Keys{"AA:BB:CC:DD:EE:21": testKey}, Timeout: 50 * time.Millisecond,
	}})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(ps) != 1 {
		t.Fatalf("Discover returned %d proxies for one device, want 1", len(ps))
	}
}

// TestDiscoveredProxiesAreBounded: the discovered set holds at most
// MaxDiscoveredProxies devices; the rest are logged, not dialled.
// R2-C2-bound-2003.
func TestDiscoveredProxiesAreBounded(t *testing.T) {
	const n = esphome.MaxDiscoveredProxies + 2
	b := make(listBrowser, 0, n)
	for i := range n {
		mac := fmt.Sprintf("AABBCCDD%04X", i)
		f := fake(t, esphometest.Options{Name: fmt.Sprintf("proxy-%03d", i), MAC: mac})
		b = append(b, discovery.Instance{
			Name: fmt.Sprintf("proxy-%03d", i), QName: fmt.Sprintf("proxy-%03d._esphomelib._tcp.local", i),
			Addresses:  []discovery.Address{{IP: netip.MustParseAddrPort(f.Addr()).Addr(), Port: netip.MustParseAddrPort(f.Addr()).Port()}},
			Parameters: discovery.Parameters{"mac": []byte(mac)},
		})
	}
	log := &capture{}
	central(t, esphome.Options{Logger: slog.New(log), Discover: &esphome.DiscoverOptions{
		Browser: b, AllowPlaintext: true, Rescan: time.Hour, Timeout: 50 * time.Millisecond,
	}})
	if got := log.count("ble.esphome.proxy_discovered"); got != esphome.MaxDiscoveredProxies {
		t.Fatalf("%d proxies discovered of %d devices, want the bound %d", got, n, esphome.MaxDiscoveredProxies)
	}
}

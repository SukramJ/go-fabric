// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package esphome_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"log/slog"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/client/discovery"

	"github.com/SukramJ/go-fabric/ble/esphome"
	"github.com/SukramJ/go-fabric/ble/esphome/esphometest"
	"github.com/SukramJ/go-fabric/ble/esphome/internal/wire"
)

// browseWindow is the tests' browse time: the in-memory network answers
// at once.
const browseWindow = 300 * time.Millisecond

// names is a DNS-SD cache on the in-memory network.
func names(t *testing.T, n *esphometest.Network) *discovery.Names {
	t.Helper()
	d := discovery.NewNames(n.Conn(), nil)
	t.Cleanup(func() { _ = d.Close() })
	return d
}

// logRecord is one captured log line: its message and its proxy and
// reason attributes.
type logRecord struct{ msg, proxy, reason string }

// capture is a slog handler that keeps every record.
type capture struct {
	mu   sync.Mutex
	recs []logRecord
}

func (c *capture) Enabled(context.Context, slog.Level) bool { return true }
func (c *capture) WithAttrs([]slog.Attr) slog.Handler       { return c }
func (c *capture) WithGroup(string) slog.Handler            { return c }

func (c *capture) Handle(_ context.Context, r slog.Record) error {
	rec := logRecord{msg: r.Message}
	r.Attrs(func(a slog.Attr) bool {
		switch a.Key {
		case "proxy":
			rec.proxy = a.Value.String()
		case "reason":
			rec.reason = a.Value.String()
		default:
		}
		return true
	})
	c.mu.Lock()
	c.recs = append(c.recs, rec)
	c.mu.Unlock()
	return nil
}

func (c *capture) find(msg, proxy string) (logRecord, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, r := range c.recs {
		if r.msg == msg && r.proxy == proxy {
			return r, true
		}
	}
	return logRecord{}, false
}

func base64Key(b byte) string {
	return base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{b}, 32))
}

func proxyNames(ps []esphome.Proxy) []string {
	out := make([]string, 0, len(ps))
	for _, p := range ps {
		out = append(out, p.Name)
	}
	slices.Sort(out)
	return out
}

// TestDiscover: of the ESPHome devices advertised, the ones with
// ACTIVE_CONNECTIONS whose key is known (by name or by the TXT mac) or
// that answer plaintext are proxies; the rest are skipped with a reason.
func TestDiscover(t *testing.T) {
	n := esphometest.NewNetwork()
	plain := fake(t, esphometest.Options{Name: "plain-proxy", MAC: "AABBCCDDEE01"})
	byName := fake(t, esphometest.Options{Name: "named-proxy", MAC: "AABBCCDDEE02", Key: testKey})
	byMAC := fake(t, esphometest.Options{Name: "mac-proxy", MAC: "AABBCCDDEE03", Key: testKey})
	passive := fake(t, esphometest.Options{
		Name: "passive-proxy", MAC: "AABBCCDDEE04",
		FeatureFlags: esphometest.DefaultFeatureFlags &^ wire.FeatureActiveConnections,
	})
	noKey := fake(t, esphometest.Options{Name: "keyless-proxy", MAC: "AABBCCDDEE05", Key: testKey})
	for _, f := range []*esphometest.Proxy{plain, byName, byMAC, passive, noKey} {
		n.Advertise(f)
	}
	log := &capture{}
	prev := slog.Default()
	slog.SetDefault(slog.New(log))
	t.Cleanup(func() { slog.SetDefault(prev) })
	keys := esphome.Keys{"Named-Proxy": testKey, "aa-bb-cc-dd-ee-03": testKey}
	got, err := esphome.Discover(t.Context(), names(t, n), keys, browseWindow)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"mac-proxy", "named-proxy", "plain-proxy"}; !slices.Equal(proxyNames(got), want) {
		t.Fatalf("discovered %v, want %v", proxyNames(got), want)
	}
	for _, p := range got {
		if f := map[string]*esphometest.Proxy{"plain-proxy": plain, "named-proxy": byName, "mac-proxy": byMAC}[p.Name]; p.Addr != f.Addr() {
			t.Errorf("%s at %s, want %s", p.Name, p.Addr, f.Addr())
		}
		if (p.Key != "") != (p.Name != "plain-proxy") {
			t.Errorf("%s key %q", p.Name, p.Key)
		}
	}
	for name, reason := range map[string]string{
		"passive-proxy": esphome.ErrNoActiveConnections.Error(),
		"keyless-proxy": esphome.ErrRequiresEncryption.Error(),
	} {
		r, ok := log.find("ble.esphome.proxy_skipped", name)
		if !ok || r.reason != reason {
			t.Errorf("%s: skipped %v %q, want %q", name, ok, r.reason, reason)
		}
	}
	// Discover connects once and lets go.
	eventually(t, "the probes to end", func() bool { return plain.Clients() == 0 && byName.Clients() == 0 })

	// A wrong key is a refusal, not a proxy.
	got, err = esphome.Discover(t.Context(), names(t, n), esphome.Keys{"named-proxy": base64Key(9)}, browseWindow)
	if err != nil || slices.Contains(proxyNames(got), "named-proxy") {
		t.Fatalf("wrong key: %v, %v", proxyNames(got), err)
	}
	if r, ok := log.find("ble.esphome.proxy_skipped", "named-proxy"); !ok || r.reason != esphome.ErrInvalidKey.Error() {
		t.Fatalf("wrong key skipped %v %q", ok, r.reason)
	}
}

func TestDiscoverRefusals(t *testing.T) {
	if _, err := esphome.Discover(t.Context(), nil, nil, 0); !errors.Is(err, esphome.ErrNoBrowser) {
		t.Fatalf("no browser: %v", err)
	}
	if _, err := esphome.NewCentral(nil, esphome.Options{Discover: &esphome.DiscoverOptions{}}); !errors.Is(err, esphome.ErrNoBrowser) {
		t.Fatalf("NewCentral without a browser: %v", err)
	}
	// Nothing given and nothing advertised.
	n := esphometest.NewNetwork()
	_, err := esphome.NewCentral(nil, esphome.Options{Discover: &esphome.DiscoverOptions{Browser: names(t, n), Timeout: browseWindow}})
	if !errors.Is(err, esphome.ErrNoProxies) {
		t.Fatalf("nothing discovered: %v", err)
	}
	// Advertised, but no proxy for commissioning.
	n.Advertise(fake(t, esphometest.Options{Name: "passive", FeatureFlags: wire.FeaturePassiveScan}))
	_, err = esphome.NewCentral(nil, esphome.Options{Discover: &esphome.DiscoverOptions{Browser: names(t, n), Timeout: browseWindow}})
	if !errors.Is(err, esphome.ErrNoProxies) {
		t.Fatalf("only a passive proxy: %v", err)
	}
}

func TestParseKey(t *testing.T) {
	name, key, err := esphome.ParseKey(" kitchen-proxy = " + testKey + " ")
	if err != nil || name != "kitchen-proxy" || key != testKey {
		t.Fatalf("ParseKey = %q %q %v", name, key, err)
	}
	if _, _, err := esphome.ParseKey("AA:BB:CC:DD:EE:FF=" + testKey); err != nil {
		t.Fatalf("a MAC: %v", err)
	}
	for _, bad := range []string{"", "kitchen", "=" + testKey, "kitchen=", "kitchen=bm90LWEta2V5"} {
		if _, _, err := esphome.ParseKey(bad); err == nil {
			t.Errorf("ParseKey(%q) succeeded", bad)
		}
	}
}

// TestCentralDiscovery: a central with discovery merges the proxies it is
// given with the ones advertised — a device advertised at a given proxy's
// address is that proxy — follows a proxy that moves after the loss, and
// drops one that leaves while idle.
func TestCentralDiscovery(t *testing.T) {
	per := peripheral("C0:FF:EE:00:00:01")
	n := esphometest.NewNetwork()
	given := fake(t, esphometest.Options{Name: "given-proxy", MAC: "AABBCCDDEE11"}, per)
	found := fake(t, esphometest.Options{Name: "found-proxy", MAC: "AABBCCDDEE12", Key: testKey}, per)
	leaving := fake(t, esphometest.Options{Name: "leaving-proxy", MAC: "AABBCCDDEE13"})
	for _, f := range []*esphometest.Proxy{given, found, leaving} {
		n.Advertise(f)
	}
	log := &capture{}
	c := central(t, esphome.Options{Logger: slog.New(log), Discover: &esphome.DiscoverOptions{
		Browser: names(t, n), Keys: esphome.Keys{"found-proxy": testKey}, Rescan: time.Hour, Timeout: browseWindow,
	}}, esphome.Proxy{Addr: given.Addr(), Name: "given"})
	eventually(t, "one client per proxy", func() bool {
		return given.Clients() == 1 && found.Clients() == 1 && leaving.Clients() == 1
	})
	if _, ok := log.find("ble.esphome.proxy_discovered", "given-proxy"); ok {
		t.Fatal("the given proxy must not be added a second time")
	}

	// Scanning runs through the given and the discovered proxy.
	scanFor(t, c, per.Address)
	eventually(t, "both proxies subscribed", func() bool {
		return count(given.Events(), "subscribe") == 1 && count(found.Events(), "subscribe") == 1
	})

	// The discovered proxy takes a new address: after the loss the
	// central browses again and reconnects there.
	if err := found.Move(); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the moved proxy resubscribed", func() bool { return count(found.Events(), "subscribe") == 2 })
	if _, ok := log.find("ble.esphome.proxy_moved", "found-proxy"); !ok {
		t.Fatal("no proxy_moved line")
	}
	if !c.IsPeripheralReachable(per.Address) {
		eventually(t, "the peripheral reachable again", func() bool { return c.IsPeripheralReachable(per.Address) })
	}

	// A proxy that leaves the network is dropped once it is idle.
	n.Withdraw(leaving)
	// The goodbye takes effect after the cache's eviction delay.
	time.Sleep(discovery.DefaultEvictionDelay + 200*time.Millisecond)
	leaving.Close()
	eventually(t, "the leaving proxy dropped", func() bool {
		_, ok := log.find("ble.esphome.proxy_removed", "leaving-proxy")
		return ok
	})
	// The given proxy stays even when it is not advertised.
	n.Withdraw(given)
	given.DropClients()
	eventually(t, "the given proxy back", func() bool { return count(given.Events(), "subscribe") == 2 })
	if _, ok := log.find("ble.esphome.proxy_removed", "given"); ok {
		t.Fatal("a given proxy must stay")
	}
}

// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

import (
	"context"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/SukramJ/go-fabric/mdns"
)

// mdnsTestConfig is the smallest configuration New accepts, on its own
// SQLite file, never started.
func mdnsTestConfig(t *testing.T) Config {
	t.Helper()
	return Config{
		Network:            Network{Listen: "127.0.0.1:0", PreferIPv4: true},
		Commissioning:      Commissioning{Passcode: 20202021, Discriminator: 3840, PBKDFIterations: 1000, Salt: []byte("SPAKE2P Key Salt")},
		ProductDescription: ProductDescription{VendorID: 0xFFF1, ProductID: 0x8001},
		BasicInformation:   BasicInformation{NodeLabel: "mdns test", SerialNumber: "MDNS-1"},
		Store:              Store{Path: filepath.Join(t.TempDir(), "mdns.db")},
		Logger:             slog.New(slog.DiscardHandler),
	}
}

func newMDNSTestNode(t *testing.T, cfg Config) *Node {
	t.Helper()
	n, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = n.Stop(context.Background()) })
	return n
}

// A nil Advertiser is Zeroconf, Noop under Network.MDNS.Disabled; a host's
// advertiser stays the host's. The nodes are never started: nothing is
// published.
func TestDefaultAdvertiser(t *testing.T) {
	cfg := mdnsTestConfig(t)
	cfg.Network.MDNS.Disabled = true
	n := newMDNSTestNode(t, cfg)
	if _, ok := n.advertiser.(*mdns.Noop); !ok || !n.ownsAdvertiser {
		t.Fatalf("Disabled: advertiser %T, want the node's *mdns.Noop", n.advertiser)
	}
	if n.MDNSEnabled() || n.DiagnoseMDNS() != nil || len(n.MDNSServices()) != 0 {
		t.Fatal("a Noop advertiser reads as advertising")
	}

	cfg = mdnsTestConfig(t)
	cfg.Network.MDNS.HostName = "bs-host"
	n = newMDNSTestNode(t, cfg)
	zc, ok := n.advertiser.(*mdns.Zeroconf)
	if !ok || !n.ownsAdvertiser || zc.HostName != "bs-host" {
		t.Fatalf("nil Advertiser: %T, want the node's *mdns.Zeroconf with the host name", n.advertiser)
	}
	if !n.MDNSEnabled() {
		t.Fatal("Zeroconf reads as not advertising")
	}
	// Unstarted, it publishes nothing, which the diagnosis reports.
	if f := n.DiagnoseMDNS(); len(f) == 0 || f[0].Code != "no_services" {
		t.Fatalf("DiagnoseMDNS before Start = %+v, want no_services", f)
	}

	host := mdns.NewNoop()
	cfg = mdnsTestConfig(t)
	cfg.Advertiser = host
	n = newMDNSTestNode(t, cfg)
	if n.advertiser != mdns.Advertiser(host) || n.ownsAdvertiser {
		t.Fatal("the host's advertiser was replaced")
	}
}

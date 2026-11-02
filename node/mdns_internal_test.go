// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

import (
	"context"
	"log/slog"
	"path/filepath"
	"slices"
	"sync"
	"testing"

	"github.com/SukramJ/go-fabric/mdns"
	"github.com/SukramJ/go-fabric/secure/sigma"
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
	n, err := New(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = n.Stop(context.Background()) })
	return n
}

// A nil Advertiser is the mDNS responder, Noop under Network.MDNS.Off; a host's
// advertiser stays the host's. The nodes are never started: nothing is
// published.
func TestDefaultAdvertiser(t *testing.T) {
	cfg := mdnsTestConfig(t)
	cfg.Network.MDNS.Off = true
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
	r, ok := n.advertiser.(*mdns.Responder)
	if !ok || !n.ownsAdvertiser || r.HostName != "bs-host" {
		t.Fatalf("nil Advertiser: %T, want the node's *mdns.Responder with the host name", n.advertiser)
	}
	if !n.MDNSEnabled() {
		t.Fatal("the responder reads as not advertising")
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

// connectedAdvertiser records the ServiceConnected calls.
type connectedAdvertiser struct {
	*mdns.Noop
	mu        sync.Mutex
	connected []string
}

func (a *connectedAdvertiser) ServiceConnected(instance, serviceType string) {
	a.mu.Lock()
	a.connected = append(a.connected, instance+"|"+serviceType)
	a.mu.Unlock()
}

// TestOperationalConnectedEndsTheBroadcast: a CASE session on a fabric
// tells the advertiser that fabric's operational service is connected
// (matter.js DeviceAdvertiser.ts:114-129); a session without a loaded
// identity, or a PASE session's fabric index 0, tells it nothing.
func TestOperationalConnectedEndsTheBroadcast(t *testing.T) {
	adv := &connectedAdvertiser{Noop: mdns.NewNoop()}
	cfg := mdnsTestConfig(t)
	cfg.Advertiser = adv
	n := newMDNSTestNode(t, cfg)
	n.caseIDs.mu.Lock()
	n.caseIDs.byIdx[3] = &caseFabric{identity: &sigma.Identity{CompressedFabricID: [8]byte{0, 0, 0, 0, 0, 0, 0, 0x18}, NodeID: 1}}
	n.caseIDs.mu.Unlock()
	n.operationalConnected(0)
	n.operationalConnected(2)
	n.operationalConnected(3)
	adv.mu.Lock()
	defer adv.mu.Unlock()
	if want := []string{"0000000000000018-0000000000000001|_matter._tcp"}; !slices.Equal(adv.connected, want) {
		t.Errorf("ServiceConnected calls %v, want %v", adv.connected, want)
	}
}

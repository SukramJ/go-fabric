// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controller_test

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/client/discovery"
	"github.com/SukramJ/go-fabric/controller"
	"github.com/SukramJ/go-fabric/controller/attestation"
	"github.com/SukramJ/go-fabric/controller/ble/bletest"
	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/controller/commissioner"
	"github.com/SukramJ/go-fabric/diag"
	"github.com/SukramJ/go-fabric/diagevent"
	"github.com/SukramJ/go-fabric/fabrictest"
)

// discoveredSite is siteController with a trace and counting metrics,
// and the site's device as Controller.Discover finds it.
func discoveredSite(t *testing.T) (*fabrictest.Site, *fabrictest.Device, *controller.Controller, *countingMetrics, client.Device) {
	t.Helper()
	m := &countingMetrics{}
	site, dev, c := siteController(t, fabrictest.Topology{}, func(cfg *controller.Config) {
		cfg.Diagnostics = diag.Diagnostics{Capacity: 64}
		cfg.Metrics = m
	})
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	var found []client.Device
	if err := site.Run(ctx, func(ctx context.Context) (err error) {
		found, err = c.Discover(ctx, discovery.ByLongDiscriminator(dev.Discriminator()), 5*time.Second)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if len(found) != 1 {
		t.Fatalf("Discover found %d devices, want 1", len(found))
	}
	return site, dev, c, m, found[0]
}

// TestPairDiscoveredAttestationRejected: PairDiscovered with the zero
// attestation policy rejects the site device's test Certification
// Declaration finding, as Pair does (TestPairAttestationRejected); it
// reports "Discovery" 2.1 skipped first, records the start with the
// device's discriminator and the failed pairing at the attestation step
// in the trace, and counts one pair.failures.
func TestPairDiscoveredAttestationRejected(t *testing.T) {
	t.Parallel()
	site, dev, c, m, found := discoveredSite(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	l := &progressLog{}
	err := site.Run(ctx, func(ctx context.Context) error {
		_, err := c.PairDiscovered(ctx, found, dev.Passcode(), controller.PairOptions{Progress: l.add})
		return err
	})
	if !errors.Is(err, commissioner.ErrAttestationRejected) || !errors.Is(err, commissioner.ErrCommissioning) {
		t.Fatalf("PairDiscovered: %v, want ErrAttestationRejected and ErrCommissioning", err)
	}
	if len(c.Nodes()) != 0 {
		t.Fatalf("%d commissioned nodes after a rejection", len(c.Nodes()))
	}
	if got := l.lines(); len(got) < 2 || got[0] != "2.1 Discovery skipped" || got[1] != "2.2 PASE started" {
		t.Fatalf("progress %q, want the discovery skipped first", got)
	}
	events := c.DiagnosticEvents()
	if !hasEvent(events, diagevent.KindPairing, diagevent.SeverityError, "Pairing failed at step OperationalCredentials.DeviceAttestation: ") {
		t.Fatalf("no failed attestation step in %+v", events)
	}
	start := events[len(events)-1]
	if start.Message != "Pairing started." || start.Detail["discriminator"] != strconv.Itoa(int(dev.Discriminator())) || start.Detail["code_kind"] != "" {
		t.Fatalf("the oldest event %+v, want the start with the device's discriminator", start)
	}
	if got := m.counter(controller.MetricPairFailures); got != 1 {
		t.Fatalf("pair.failures %d, want 1", got)
	}
}

// TestPairDiscoveredPairs: with a policy that accepts the test
// attestation the discovered device is paired, the trace records it and
// pair.failures stays 0.
func TestPairDiscoveredPairs(t *testing.T) {
	t.Parallel()
	site, dev, c, m, found := discoveredSite(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	var n *client.Node
	if err := site.Run(ctx, func(ctx context.Context) (err error) {
		n, err = c.PairDiscovered(ctx, found, dev.Passcode(), controller.PairOptions{OnAttestationFailure: attestation.Accept()})
		return err
	}); err != nil {
		t.Fatalf("PairDiscovered: %v", err)
	}
	if n == nil || len(c.Nodes()) != 1 {
		t.Fatalf("node %v, %d nodes", n, len(c.Nodes()))
	}
	if !hasEvent(c.DiagnosticEvents(), diagevent.KindPairing, diagevent.SeverityInfo, " was paired.") {
		t.Fatalf("no paired node in %+v", c.DiagnosticEvents())
	}
	if got := m.counter(controller.MetricPairFailures); got != 0 {
		t.Fatalf("pair.failures %d for a pairing that succeeded", got)
	}
}

// TestPairDiscoveredConflictingOptions: the discovered device names how
// it is reached, so PairOptions.BLE and Address are ErrConfiguration, and
// so are a device without an address, an address without a port and a
// passcode the setup codes refuse; each is a failed pairing in the
// trace and in pair.failures, and nothing is commissioned.
func TestPairDiscoveredConflictingOptions(t *testing.T) {
	t.Parallel()
	site, dev, c, m, found := discoveredSite(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	cases := []struct {
		name     string
		dev      client.Device
		opts     controller.PairOptions
		passcode uint32
	}{
		{"BLE", found, controller.PairOptions{BLE: bletest.NewCentral()}, dev.Passcode()},
		{"Address", found, controller.PairOptions{Address: &net.UDPAddr{IP: net.IPv6loopback, Port: 5540}}, dev.Passcode()},
		{"no address", client.Device{D: found.D, InstanceID: found.InstanceID}, controller.PairOptions{}, dev.Passcode()},
		{"address without port", client.Device{D: found.D, InstanceID: found.InstanceID, Addresses: []discovery.Address{{IP: netip.IPv6Loopback()}}}, controller.PairOptions{}, dev.Passcode()},
		{"invalid passcode", found, controller.PairOptions{}, 12345678},
	}
	for _, tc := range cases {
		err := site.Run(ctx, func(ctx context.Context) error {
			_, err := c.PairDiscovered(ctx, tc.dev, tc.passcode, tc.opts)
			return err
		})
		if !errors.Is(err, controller.ErrConfiguration) {
			t.Errorf("%s: %v, want ErrConfiguration", tc.name, err)
		}
	}
	if got := m.counter(controller.MetricPairFailures); got != int64(len(cases)) {
		t.Fatalf("pair.failures %d, want %d", got, len(cases))
	}
	if len(c.Nodes()) != 0 {
		t.Fatalf("%d nodes after refused pairings", len(c.Nodes()))
	}
	failed := 0
	for _, e := range c.DiagnosticEvents() {
		if e.Kind == diagevent.KindPairing && e.Severity == diagevent.SeverityError && strings.HasPrefix(e.Message, "Pairing failed") {
			failed++
		}
	}
	if failed != len(cases) {
		t.Fatalf("%d failed pairings in the trace, want %d", failed, len(cases))
	}
}

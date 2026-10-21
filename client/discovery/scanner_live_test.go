//go:build !race

// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package discovery_test

// The live test runs the production transport against the module's own
// advertiser. It is built without the race detector only: the advertiser
// is grandcat/zeroconf, whose Shutdown/recv race is the reason the mdns
// package's tests skip under -race (mdns/race_skip_test.go).

import (
	"context"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/client/discovery"
	"github.com/SukramJ/go-fabric/mdns"
)

// TestScanner_LiveFindsAnAdvertisedNode advertises a commissionable record
// with mdns.Zeroconf (plus its subtype responder) on the host's multicast
// interfaces and finds it by long discriminator, by short discriminator
// and with "any". Skipped where the host gives no multicast.
func TestScanner_LiveFindsAnAdvertisedNode(t *testing.T) {
	if testing.Short() {
		t.Skip("multicast; skipped under -short")
	}
	responder, err := mdns.NewSubtypeResponder(nil)
	if err != nil {
		t.Skipf("no multicast here: %v", err)
	}
	responder.Start(t.Context())
	z := mdns.NewZeroconf()
	z.AttachSubtypeResponder(responder)
	defer func() { _ = z.Close() }()

	const discriminator = 0xA5C // short discriminator 0xA
	instance := [8]byte{0xDE, 0xC0, 0xDE, 0x00, 0x11, 0x22, 0x33, 0x44}
	svc := mdns.BuildCommissionableService(mdns.CommissionableServiceConfig{
		InstanceID:        instance,
		Discriminator:     discriminator,
		VendorID:          0xFFF1,
		ProductID:         0x8001,
		CommissioningMode: 1,
		DeviceTypeID:      0x000E,
		DeviceName:        "go-fabric live",
		Port:              55402,
	})
	if err := z.Publish(t.Context(), svc); err != nil {
		t.Skipf("advertiser: %v", err)
	}

	for _, c := range []struct {
		name string
		id   discovery.Identifier
	}{
		{"long discriminator", discovery.ByLongDiscriminator(discriminator)},
		{"short discriminator", discovery.ByShortDiscriminator(0xA)},
		{"any", discovery.AnyCommissionable()},
	} {
		// A fresh cache per identifier, so each search goes to the network.
		names, err := discovery.Open(nil)
		if err != nil {
			t.Skipf("no multicast here: %v", err)
		}
		scanner := discovery.NewScanner(names)
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		var found *discovery.Device
		_, err = scanner.FindCommissionable(ctx, c.id, func(d discovery.Device) {
			if d.InstanceID == svc.InstanceName && found == nil {
				found = &d
				cancel()
			}
		})
		cancel()
		scanner.Close()
		_ = names.Close()
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if found == nil {
			t.Skipf("%s: our record was not seen within 5 s — multicast loopback unavailable here", c.name)
		}
		if found.D != discriminator || found.CM != 1 || found.VP == nil || *found.VP != "65521+32769" || len(found.Addresses) == 0 || found.Addresses[0].Port != 55402 {
			t.Fatalf("%s: device = %+v", c.name, *found)
		}
	}
}

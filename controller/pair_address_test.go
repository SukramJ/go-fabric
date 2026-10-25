// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controller_test

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/client/discovery"
	"github.com/SukramJ/go-fabric/controller"
	"github.com/SukramJ/go-fabric/controller/attestation"
	"github.com/SukramJ/go-fabric/controller/ble/bletest"
	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/fabrictest"
)

// TestPairByAddress: a device whose DNS-SD records are withdrawn is not
// discovered, and PairOptions.Address pairs it all the same — a
// client.Device of its address alone (controller.DeviceAt).
func TestPairByAddress(t *testing.T) {
	t.Parallel()
	site, dev, c := siteController(t, fabrictest.Topology{}, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	adv := dev.Advertiser()
	for _, svc := range adv.Active() {
		if err := adv.Withdraw(ctx, svc.InstanceName, svc.ServiceType); err != nil {
			t.Fatal(err)
		}
	}
	var found []client.Device
	var n *client.Node
	if err := site.Run(ctx, func(ctx context.Context) (err error) {
		if found, err = c.DiscoverWith(ctx, discovery.ByLongDiscriminator(dev.Discriminator()), 2*time.Second, controller.DiscoverOptions{First: true}); err != nil {
			return err
		}
		n, err = c.Pair(ctx, manualCode(t, dev), controller.PairOptions{Address: dev.Addr(), OnAttestationFailure: attestation.Accept()})
		return err
	}); err != nil {
		t.Fatalf("Pair by address: %v", err)
	}
	if len(found) != 0 {
		t.Fatalf("a withdrawn device was discovered: %+v", found)
	}
	if !n.IsCommissioned() || len(c.Nodes()) != 1 {
		t.Fatalf("commissioned %v, nodes %d", n.IsCommissioned(), len(c.Nodes()))
	}
}

// TestDeviceAt: an address becomes a device of that address alone; one
// without an IP or a port, and BLE with an address, are ErrConfiguration.
func TestDeviceAt(t *testing.T) {
	t.Parallel()
	d, err := controller.DeviceAt(&net.UDPAddr{IP: net.ParseIP("192.0.2.7"), Port: 5540})
	if err != nil || d.InstanceID != "" || len(d.Addresses) != 1 || d.Addresses[0].IP.String() != "192.0.2.7" || d.Addresses[0].Port != 5540 {
		t.Fatalf("DeviceAt: %+v %v", d, err)
	}
	if d, err := controller.DeviceAt(&net.UDPAddr{IP: net.ParseIP("fe80::1"), Port: 5540, Zone: "eth0"}); err != nil || d.Addresses[0].IP.Zone() != "eth0" {
		t.Fatalf("DeviceAt with a zone: %+v %v", d, err)
	}
	for _, a := range []*net.UDPAddr{nil, {Port: 5540}, {IP: net.ParseIP("192.0.2.7")}} {
		if _, err := controller.DeviceAt(a); !errors.Is(err, controller.ErrConfiguration) {
			t.Errorf("DeviceAt(%v): %v", a, err)
		}
	}
	_, _, c := siteController(t, fabrictest.Topology{}, nil)
	_, err = c.Pair(context.Background(), "34970112332", controller.PairOptions{Address: &net.UDPAddr{IP: net.ParseIP("192.0.2.7"), Port: 5540}, BLE: bletest.NewCentral()})
	if !errors.Is(err, controller.ErrConfiguration) {
		t.Fatalf("Pair with BLE and Address: %v", err)
	}
	if _, err := c.Pair(context.Background(), "34970112332", controller.PairOptions{Address: &net.UDPAddr{Port: 5540}}); !errors.Is(err, controller.ErrConfiguration) {
		t.Fatalf("Pair with an address without an IP: %v", err)
	}
}

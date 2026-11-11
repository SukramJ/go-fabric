// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package fabrictest

import (
	"fmt"

	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/controller/ble/bletest"
	"github.com/SukramJ/go-fabric/internal/nodeseam"
	"github.com/SukramJ/go-fabric/node"
)

// BLE is the device's Bluetooth radio as a peripheral of the in-memory BLE
// model (controller/ble/bletest) — the radio its node is built with
// ([Topology].BLE, node.Network.BLE). The node runs it as it runs a
// platform's: while uncommissioned it advertises its commissionable
// payload — its discriminator, vendor id and product id — on the
// advertisement's schedule and serves each central's BTP session through
// its bridge (bridge.Config.BLE); once commissioned it is off the radio.
// A central built on it — bletest.NewCentral, or a platform's test double
// such as ble/esphome/esphometest — lets controller.PairOptions.BLE
// commission the device over BLE; the rest of the pairing runs on the
// site's network. Its address is C0:FF:EE:00:xx:yy, xx:yy the device's
// index; every call returns the same peripheral, across restarts. A device
// without Topology.BLE fails the test.
func (d *Device) BLE() *bletest.Peripheral {
	d.mu.Lock()
	p := d.ble
	d.mu.Unlock()
	if p == nil {
		d.site.tb.Helper()
		d.site.tb.Fatalf("fabrictest: device %d has no BLE radio (Topology.BLE)", d.index)
	}
	return p
}

// blePeripheral is the device's peripheral, built from cfg at the first
// start.
func (d *Device) blePeripheral(cfg node.Config) *bletest.Peripheral {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.ble == nil {
		d.ble = &bletest.Peripheral{
			Address:       fmt.Sprintf("C0:FF:EE:00:%02X:%02X", (d.index>>8)&0xFF, d.index&0xFF),
			Discriminator: cfg.Commissioning.Discriminator,
			VendorID:      cfg.ProductDescription.VendorID,
			ProductID:     cfg.ProductDescription.ProductID,
		}
	}
	return d.ble
}

// UseWiFiCommissioning makes the device built from cfg serve
// NetworkCommissioning with the Wi-Fi feature instead of Ethernet, its
// scan and connect done by driver — what a commissioner's network steps
// configure when it pairs the device over BLE. A test's Topology.Configure
// calls it; the knob is not part of node.Config, whose nodes are
// Ethernet-attached (review 2026-11 W5-api-11).
func UseWiFiCommissioning(cfg *node.Config, driver contract.WiFiDriver) {
	nodeseam.UseWiFiCommissioning(cfg, driver)
}

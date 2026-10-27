// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package fabrictest

import (
	"fmt"
	"log/slog"

	"github.com/SukramJ/go-fabric/bridge/bridgetest"
	"github.com/SukramJ/go-fabric/controller/ble/bletest"
	"github.com/SukramJ/go-fabric/transport/btp"
)

// BLE is the device as a peripheral of the in-memory BLE model
// (controller/ble/bletest): it advertises the device's commissionable
// payload — its discriminator, vendor id and product id — and serves every
// BTP session through the device's own bridge, as a device's BLE transport
// hands BTP to its exchange layer. A central built on it —
// bletest.NewCentral, or a platform's test double such as
// ble/esphome/esphometest — lets controller.PairOptions.BLE commission the
// device over BLE; the rest of the pairing runs on the site's network. Its
// address is C0:FF:EE:00:xx:yy, xx:yy the device's index. Each call returns
// a new peripheral of the same device.
func (d *Device) BLE() *bletest.Peripheral {
	d.mu.Lock()
	pd := d.cfg.ProductDescription
	disc := d.cfg.Commissioning.Discriminator
	d.mu.Unlock()
	return &bletest.Peripheral{
		Address:       fmt.Sprintf("C0:FF:EE:00:%02X:%02X", (d.index>>8)&0xFF, d.index&0xFF),
		Discriminator: disc,
		VendorID:      pd.VendorID,
		ProductID:     pd.ProductID,
		Serve: func(conn *btp.Conn) (func([]byte), func()) {
			srv, err := bridgetest.ServeConn(d.Bridge(), conn)
			if err != nil {
				d.site.log.Warn("fabrictest.ble_serve_failed", slog.Int("device", d.index), slog.Any("err", err))
				return nil, nil
			}
			return srv.Deliver, srv.Close
		},
	}
}

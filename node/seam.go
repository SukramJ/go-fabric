// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

import (
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/internal/nodeseam"
	"github.com/SukramJ/go-fabric/transport/udp"
)

func init() {
	nodeseam.UseClientListener = func(cfg any, open func() (*udp.Listener, error)) bool {
		c, ok := cfg.(*Config)
		if !ok || c == nil || open == nil {
			return false
		}
		if c.Client == nil {
			c.Client = &ClientConfig{}
		}
		c.Client.open = open
		return true
	}
	nodeseam.UseOTAHarness = func(cfg any) bool {
		c, ok := cfg.(*Config)
		if !ok || c == nil || c.OTA == nil {
			return false
		}
		ota := *c.OTA
		ota.harness = true
		c.OTA = &ota
		return true
	}
	nodeseam.UseWiFiCommissioning = func(cfg any, driver contract.WiFiDriver) bool {
		c, ok := cfg.(*Config)
		if !ok || c == nil || driver == nil {
			return false
		}
		c.Network.wifiCommissioning = wifiCommissioning{Driver: driver}
		return true
	}
}

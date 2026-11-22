// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

import (
	"thingfabric.dev/go-fabric/client/discovery"
	"thingfabric.dev/go-fabric/internal/nodeseam"
	"thingfabric.dev/go-fabric/transport/udp"
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
	nodeseam.UsePeerClient = func(n any, names *discovery.Names, listen string, open func() (*udp.Listener, error)) bool {
		node, ok := n.(*Node)
		if !ok || node == nil {
			return false
		}
		node.peerClient.mu.Lock()
		node.peerClient.template = ClientConfig{Names: names, Listen: listen, open: open}
		node.peerClient.mu.Unlock()
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
	nodeseam.UseSpecificationVersion = func(cfg any, version uint32) bool {
		c, ok := cfg.(*Config)
		if !ok || c == nil {
			return false
		}
		c.specificationVersion = version
		return true
	}
}

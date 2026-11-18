// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/SukramJ/go-fabric/mdns"
	"github.com/SukramJ/go-fabric/node"
)

// TestConfigRefusesPairsOneOfWhichWouldBeDropped (R2-P5-apinode-17,
// R2-P5-apispec-12): Validate refuses a host Advertiser beside a Network.MDNS
// that only the node's own advertiser reads, a deprecated Config limit that
// differs from its Limits field, and an advertised device name over 32
// bytes (decision 26).
func TestConfigRefusesPairsOneOfWhichWouldBeDropped(t *testing.T) {
	t.Parallel()
	base := func() node.Config {
		cfg := minimalConfig(nil)
		cfg.Store = node.Store{Path: "unused.db"}
		return cfg
	}
	for _, tc := range []struct {
		name   string
		edit   func(*node.Config)
		refuse bool
	}{
		{"Advertiser with MDNS.Off", func(c *node.Config) { c.Network.MDNS.Off = true }, true},
		{"Advertiser with MDNS.HostName", func(c *node.Config) { c.Network.MDNS.HostName = "host" }, true},
		{"Advertiser with MDNS.Interfaces", func(c *node.Config) { c.Network.MDNS.Interfaces = []string{"eth0"} }, true},
		{"the node's own advertiser with MDNS", func(c *node.Config) { c.Advertiser = nil; c.Network.MDNS.HostName = "host" }, false},
		{"SupportedFabrics in both places, different", func(c *node.Config) { c.SupportedFabrics, c.Limits.SupportedFabrics = 5, 6 }, true},
		{"SupportedFabrics in both places, equal", func(c *node.Config) { c.SupportedFabrics, c.Limits.SupportedFabrics = 6, 6 }, false},
		{"Limits.SupportedFabrics above the bound", func(c *node.Config) { c.Limits.SupportedFabrics = 255 }, true},
		{"MaxPathsPerInvoke in both places, different", func(c *node.Config) { c.MaxPathsPerInvoke, c.Limits.MaxPathsPerInvoke = 2, 3 }, true},
		{"Limits.MaxPathsPerInvoke alone", func(c *node.Config) { c.Limits.MaxPathsPerInvoke = 3 }, false},
		{"ProductDescription.Name of 32 bytes", func(c *node.Config) { c.ProductDescription.Name = strings.Repeat("n", 32) }, false},
		{"ProductDescription.Name of 33 bytes", func(c *node.Config) { c.ProductDescription.Name = strings.Repeat("n", 33) }, true},
		{"NodeLabel standing in, 33 bytes", func(c *node.Config) { c.BasicInformation.NodeLabel = strings.Repeat("l", 33) }, true},
		// The NodeLabel's own constraint refuses it behind a Name too
		// (review II, R2-P6-api-2407).
		{"a long NodeLabel behind a Name", func(c *node.Config) {
			c.ProductDescription.Name = "short"
			c.BasicInformation.NodeLabel = strings.Repeat("l", 33)
		}, true},
	} {
		cfg := base()
		tc.edit(&cfg)
		err := cfg.Validate()
		if refused := errors.Is(err, node.ErrInvalidConfig); refused != tc.refuse {
			t.Errorf("%s: Validate() = %v, want refused %v", tc.name, err, tc.refuse)
		}
	}
}

// TestLimitsFieldsReachTheNode: a limit set on Limits is the one the node
// serves, as the deprecated Config field was.
func TestLimitsFieldsReachTheNode(t *testing.T) {
	t.Parallel()
	cfg := minimalConfig(openDB(t, t.TempDir()+"/node.db"))
	cfg.Limits.SupportedFabrics = 7
	cfg.Limits.MaxPathsPerInvoke = 1
	cfg.Advertiser = mdns.NewNoop()
	n := newNode(t, cfg)
	if v, ok := n.Root().OperationalCredentials.MatterRead(0x0002); !ok || v != uint8(7) {
		t.Errorf("SupportedFabrics = %v (%v), want 7", v, ok)
	}
	if v, ok := n.Root().BasicInformation.MatterRead(0x0016); !ok || v != uint16(1) {
		t.Errorf("MaxPathsPerInvoke = %v (%v), want 1", v, ok)
	}
}

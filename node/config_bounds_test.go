// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/cluster/tls"
	epmem "github.com/SukramJ/go-fabric/endpoint/memstore"
	"github.com/SukramJ/go-fabric/node"
	"github.com/SukramJ/go-fabric/store/memstore"
)

// TestValidateBoundsTheUncheckedFields (review II, R2-P6-api-2409,
// R2-P6-api-2452): Config.Validate refuses, with ErrInvalidConfig, the
// values no other check bounded — and still takes the defaults and the
// documented sentinels beside them.
func TestValidateBoundsTheUncheckedFields(t *testing.T) {
	t.Parallel()
	neg := -time.Second
	zero := time.Duration(0)
	for _, tc := range []struct {
		name    string
		edit    func(*node.Config)
		refused bool
	}{
		{"Health.Interval 1ns", func(c *node.Config) { c.Health = &node.Health{Interval: time.Nanosecond} }, true},
		{"Health.Interval 1s", func(c *node.Config) { c.Health = &node.Health{Interval: time.Second} }, false},
		{"Health.Interval 0", func(c *node.Config) { c.Health = &node.Health{} }, false},
		{"Network.InterfaceCheck 1ms", func(c *node.Config) { c.Network.InterfaceCheck = time.Millisecond }, true},
		{"Network.InterfaceCheck off", func(c *node.Config) { c.Network.InterfaceCheck = -1 }, false},
		{"OTA.HTTPSMaxImageSize negative", func(c *node.Config) { c.OTA = &node.OTAConfig{Dir: "x", HTTPSMaxImageSize: -1} }, true},
		{"OTA.HTTPSIdleTimeout negative", func(c *node.Config) { c.OTA = &node.OTAConfig{Dir: "x", HTTPSIdleTimeout: -1} }, true},
		{"OTA.AnnouncedUpdateQueryDelay negative", func(c *node.Config) { c.OTA = &node.OTAConfig{Dir: "x", AnnouncedUpdateQueryDelay: &neg} }, true},
		{"OTA.AnnouncedUpdateQueryDelay 0", func(c *node.Config) { c.OTA = &node.OTAConfig{Dir: "x", AnnouncedUpdateQueryDelay: &zero} }, false},
		{"ProductDescription.DeviceType unknown", func(c *node.Config) { c.ProductDescription.DeviceType = 0xFFFE }, true},
		{"ProductDescription.DeviceType OnOffLight", func(c *node.Config) { c.ProductDescription.DeviceType = 0x0100 }, false},
		{"Network.MDNS.HostName with .local", func(c *node.Config) { c.Advertiser, c.Network.MDNS.HostName = nil, "bridge.local" }, true},
		{"Network.MDNS.HostName 64 bytes", func(c *node.Config) { c.Advertiser, c.Network.MDNS.HostName = nil, strings.Repeat("h", 64) }, true},
		{"Network.MDNS.HostName label", func(c *node.Config) { c.Advertiser, c.Network.MDNS.HostName = nil, "bridge-1" }, false},
		{"EndpointState and EndpointPersistence", func(c *node.Config) {
			c.EndpointState = &hostState{node.NewEndpointState(memstore.New(), nil)}
			c.EndpointPersistence = &fakePersistence{labels: map[string]string{}, scenes: map[string][]byte{}} //nolint:staticcheck // the pair is what is refused
		}, true},
		{"TLS.MaxRootCertificates 4", func(c *node.Config) { c.TLS = &node.TLSConfig{CertificatePort: tlsKeys{}, MaxRootCertificates: 4} }, true},
		{"TLS.MaxClientCertificates 4", func(c *node.Config) { c.TLS = &node.TLSConfig{CertificatePort: tlsKeys{}, MaxClientCertificates: 4} }, true},
		{"TLS.MaxProvisioned 255", func(c *node.Config) { c.TLS = &node.TLSConfig{CertificatePort: tlsKeys{}, MaxProvisioned: 255} }, true},
		{"TLS.MaxProvisioned 4", func(c *node.Config) { c.TLS = &node.TLSConfig{CertificatePort: tlsKeys{}, MaxProvisioned: 4} }, true},
		{"TLS defaults", func(c *node.Config) { c.TLS = &node.TLSConfig{CertificatePort: tlsKeys{}} }, false},
		{"TLS bounds", func(c *node.Config) {
			c.TLS = &node.TLSConfig{CertificatePort: tlsKeys{}, MaxRootCertificates: 5, MaxClientCertificates: 5, MaxProvisioned: 254}
		}, false},
	} {
		cfg := minimalConfig(nil)
		cfg.Store = node.Store{Credentials: memstore.New(), Endpoints: epmem.New()}
		tc.edit(&cfg)
		err := cfg.Validate()
		if refused := err != nil; refused != tc.refused || refused && !errors.Is(err, node.ErrInvalidConfig) {
			t.Errorf("%s: Validate() = %v, want refused %v (ErrInvalidConfig)", tc.name, err, tc.refused)
		}
	}
}

// TestTLSConfigBoundsAgreeWithTheCluster (review II, R2-P6-api-2452): the
// capacities Validate takes are the ones the cluster/tls constructors
// take, at each edge, so Validate and New never disagree.
func TestTLSConfigBoundsAgreeWithTheCluster(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	for _, v := range []uint8{1, 4, 5, 6, 253, 254, 255} {
		cfg := minimalConfig(nil)
		cfg.Store = node.Store{Credentials: memstore.New(), Endpoints: epmem.New()}
		cfg.TLS = &node.TLSConfig{CertificatePort: tlsKeys{}, MaxRootCertificates: v, MaxClientCertificates: v}
		_, certErr := tls.NewCertificateManagement(ctx, tls.CertificateManagementConfig{Port: tlsKeys{}, MaxRootCertificates: v, MaxClientCertificates: v})
		if (cfg.Validate() == nil) != (certErr == nil) {
			t.Errorf("certificate capacity %d: Validate %v, the cluster %v", v, cfg.Validate(), certErr)
		}
		cfg.TLS = &node.TLSConfig{CertificatePort: tlsKeys{}, MaxProvisioned: v}
		_, clientErr := tls.NewClientManagement(ctx, tls.ClientManagementConfig{MaxProvisioned: v})
		if (cfg.Validate() == nil) != (clientErr == nil) {
			t.Errorf("MaxProvisioned %d: Validate %v, the cluster %v", v, cfg.Validate(), clientErr)
		}
	}
}

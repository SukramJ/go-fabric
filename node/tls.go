// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

import (
	"context"
	"errors"
	"fmt"

	padef "thingfabric.dev/go-fabric/cluster/spec/pushavstreamtransport"
	"thingfabric.dev/go-fabric/cluster/tls"
	"thingfabric.dev/go-fabric/endpoint"
	"thingfabric.dev/go-fabric/store"
	"thingfabric.dev/go-fabric/surface"
)

// TLSConfig mounts TlsCertificateManagement and TlsClientManagement on
// endpoint 0 (brief 26-A3, owner decision 2026-10-28): connectedhomeip
// serves both on the root endpoint only (its example delegates refuse any
// other, examples/all-clusters-app/all-clusters-common/src/
// tls-certificate-management-instance.cpp:261 at 6170af84), and RootNode
// lists them as optional. The node hands their lookups to every
// PushAvStreamTransport server of its topology whose declaration names
// none (cluster/camera PushTransport.AttachTLS), as chip's codegen wires the two
// TLS delegates into the push transport (push-av-stream-transport-server/
// CodegenIntegration.cpp:124, :133).
type TLSConfig struct {
	// CertificatePort is the node's TLS key store; required.
	CertificatePort tls.CertificatePort
	// MaxRootCertificates and MaxClientCertificates are the per-fabric
	// capacities, 5 to 254; 0 is chip's 5
	// (CHIP_CONFIG_TLS_MAX_ROOT_PER_FABRIC_CERTS_TABLE_SIZE,
	// CHIP_CONFIG_TLS_MAX_CLIENT_CERTS_PER_FABRIC_TABLE_SIZE,
	// src/lib/core/CHIPConfig.h:2043, :2052).
	MaxRootCertificates   uint8
	MaxClientCertificates uint8
	// MaxProvisioned is the per-fabric endpoint capacity, 5 to 254; 0 is
	// chip's 5 (CHIP_CONFIG_TLS_MAX_PROVISIONED_ENDPOINTS, :2063).
	MaxProvisioned uint8
	// CertificateStore and EndpointStore persist the tables; nil is the
	// node's store.
	CertificateStore tls.Store
	EndpointStore    tls.Store
}

// The capacities the cluster/tls servers take, each tlsMinCapacity to
// tlsMaxCapacity (cluster/tls NewCertificateManagement and
// NewClientManagement refuse the rest).
// TestTLSConfigBoundsAgreeWithTheCluster holds these to the servers.
const (
	tlsMinCapacity = 5
	tlsMaxCapacity = 254
)

// capacityErrors refuses a capacity the cluster/tls constructors would
// refuse at New, so Validate reports it as [ErrInvalidConfig] (review II,
// R2-P6-api-2452); 0 is the default and valid.
func (t *TLSConfig) capacityErrors() []error {
	var errs []error
	for _, f := range []struct {
		name string
		v    uint8
	}{{"MaxRootCertificates", t.MaxRootCertificates}, {"MaxClientCertificates", t.MaxClientCertificates}, {"MaxProvisioned", t.MaxProvisioned}} {
		if f.v != 0 && (f.v < tlsMinCapacity || f.v > tlsMaxCapacity) {
			errs = append(errs, fmt.Errorf("node: TLS.%s %d is outside %d to %d", f.name, f.v, tlsMinCapacity, tlsMaxCapacity))
		}
	}
	return errs
}

// errNoCertificatePort reports a TLSConfig without its CertificatePort.
var errNoCertificatePort = errors.New("node: TLS needs a CertificatePort")

// tlsKey is the key the root's TLS tables persist under.
const tlsKey = "root"

// orFive is v, or chip's default 5 for 0.
func orFive(v uint8) uint8 {
	if v == 0 {
		return 5
	}
	return v
}

// buildTLS builds the root's two TLS servers of cfg.TLS.
func buildTLS(ctx context.Context, cfg *TLSConfig, st store.Port) (*tls.CertificateManagement, *tls.ClientManagement, error) {
	if cfg.CertificatePort == nil {
		return nil, nil, errNoCertificatePort
	}
	certStore, epStore := cfg.CertificateStore, cfg.EndpointStore
	if certStore == nil && st != nil {
		certStore = st
	}
	if epStore == nil && st != nil {
		epStore = st
	}
	certs, err := tls.NewCertificateManagement(ctx, tls.CertificateManagementConfig{
		MaxRootCertificates: orFive(cfg.MaxRootCertificates), MaxClientCertificates: orFive(cfg.MaxClientCertificates),
		Port: cfg.CertificatePort, Store: certStore, Key: tlsKey,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("tls certificate management: %w", err)
	}
	client, err := tls.NewClientManagement(ctx, tls.ClientManagementConfig{
		MaxProvisioned: orFive(cfg.MaxProvisioned), Certificates: certs, Store: epStore, Key: tlsKey,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("tls client management: %w", err)
	}
	return certs, client, nil
}

// tlsLevelErrors refuses [Config].TLS under surface policies whose level
// does not have both TLS clusters, or that refuse them (review II,
// R4-doc-4754, owner decision 656). Each cluster is judged as the root
// judges it once built ([rootClusterElement], host-declared). Below
// 1.5.1 the level policy does not remove them — the RootNode device type
// may mandate them at the pin, so the root keeps them "mandatory at pin
// (absent at level …)" — but a node that composes at such a level would
// serve two clusters its level does not know; the TLS configuration is
// refused instead, before anything is built. Policies whose level is
// invalid are left to their own check. The arm for policies that remove
// a cluster is a guard only: both are judged mandatory, so
// [surface.Evaluate] keeps them against a Remove and the arm does not
// fire at this pin.
func (c *Config) tlsLevelErrors() []error {
	if c.TLS == nil || c.Surface.Validate() != nil {
		return nil
	}
	level := c.Surface.EffectiveLevel()
	ids := []uint32{tls.CertificateManagementClusterID, tls.ClientManagementClusterID}
	cands := make([]surface.Element, 0, len(ids))
	for _, id := range ids {
		el := rootClusterElement(id, true)
		if el.Levels != 0 && !el.Levels.Has(level) {
			return []error{fmt.Errorf("node: Config.TLS needs TlsCertificateManagement and TlsClientManagement, which surface level %s does not have", level)}
		}
		cands = append(cands, el)
	}
	kept, _, err := surface.Evaluate(cands, c.Surface)
	if err != nil {
		return []error{fmt.Errorf("node: Config.TLS: %w", err)}
	}
	if len(kept) != len(ids) {
		return []error{fmt.Errorf("node: Config.TLS needs TlsCertificateManagement and TlsClientManagement on the root, which the surface policies remove (level %s)", level)}
	}
	return nil
}

// tlsAttacher is a cluster server that takes the node's TLS lookups: the
// PushAvStreamTransport server (cluster/camera PushTransport).
type tlsAttacher interface {
	AttachTLS(endpoints padef.TLSEndpoints, certificates padef.TLSCertificates)
}

// attachTLS hands the root's TLS servers to the topology's push
// transports as its endpoints join; without node TLS nothing changes.
func (n *Node) attachTLS(t *endpoint.Topology) {
	certs, client := n.root.TLSCertificateManagement, n.root.TLSClientManagement
	if certs == nil || client == nil {
		return
	}
	for _, ep := range t.Endpoints {
		if ep == nil {
			continue
		}
		for _, srv := range endpoint.ClusterServers(ep) {
			if a, ok := srv.(tlsAttacher); ok {
				a.AttachTLS(client, certs)
			}
		}
	}
}

// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

import (
	"context"
	"errors"
	"fmt"

	padef "github.com/SukramJ/go-fabric/cluster/spec/pushavstreamtransport"
	"github.com/SukramJ/go-fabric/cluster/tls"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/store"
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
	// capacities, at least 5; 0 is chip's 5
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

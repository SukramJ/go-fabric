// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"context"
	"errors"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/camera"
	"github.com/SukramJ/go-fabric/cluster/spec"
	padef "github.com/SukramJ/go-fabric/cluster/spec/pushavstreamtransport"
	tcmdef "github.com/SukramJ/go-fabric/cluster/spec/tlscertificatemanagement"
	tcldef "github.com/SukramJ/go-fabric/cluster/spec/tlsclientmanagement"
	"github.com/SukramJ/go-fabric/cluster/tls"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/device"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/node"
)

type tlsKeys struct{}

func (tlsKeys) GenerateCSR(context.Context, uint8, uint16, bool, []byte) (csr, sig []byte, err error) {
	return []byte{1}, []byte{2}, nil
}
func (tlsKeys) CertificateValid(context.Context, []byte) bool                { return true }
func (tlsKeys) ClientKeyMatches(context.Context, uint8, uint16, []byte) bool { return true }
func (tlsKeys) RemoveClientKey(context.Context, uint8, uint16)               {}

type pushPort struct{}

func (pushPort) AllocatePushTransport(context.Context, uint16, uint8, padef.TransportOptionsStruct, padef.TLSCredentials) error {
	return nil
}
func (pushPort) DeallocatePushTransport(context.Context, uint16) error { return nil }
func (pushPort) ModifyPushTransport(context.Context, uint16, padef.TransportOptionsStruct) error {
	return nil
}

func (pushPort) SetTransportStatus(context.Context, []uint16, padef.TransportStatusEnum) error {
	return nil
}

func (pushPort) ManuallyTriggerTransport(context.Context, uint16, padef.TriggerActivationReasonEnum, *padef.TransportMotionTriggerTimeControlStruct) error {
	return nil
}
func (pushPort) TransportBusy(uint16) bool { return false }
func (pushPort) ValidateBandwidthLimit(context.Context, padef.StreamUsageEnum, *uint16, *uint16) error {
	return nil
}
func (pushPort) CMAFSessionNumber(uint16) (uint64, bool) { return 0, false }

// serverOn returns the cluster's server on ep.
func serverOn(ep *endpoint.Endpoint, clusterID uint32) contract.ClusterServer {
	for _, srv := range endpoint.ClusterServers(ep) {
		if srv.MatterClusterID() == clusterID {
			return srv
		}
	}
	return nil
}

func invokeOn(t *testing.T, srv contract.ClusterServer, cmd uint32, req any) (any, error) {
	t.Helper()
	ctx := im.WithSubject(im.WithFabricFilter(context.Background(), false, 1), 0x77, nil)
	return srv.MatterInvoke(ctx, cmd, req)
}

// TestNodeServesTheTLSServers: node.Config.TLS mounts TlsCertificateManagement
// and TlsClientManagement on endpoint 0, and a camera's
// PushAvStreamTransport, declared without TLS lookups, validates its
// TLSEndpointID against them (brief 26-A3, owner decision 2026-10-28).
func TestNodeServesTheTLSServers(t *testing.T) {
	cam, err := device.NewCamera(device.Options{StableKey: endpoint.StringKey("cam"), Name: "cam"},
		padef.Declare{PushTransportPort: pushPort{}})
	if err != nil {
		t.Fatal(err)
	}
	d := quietSite(t).AddDevice(fabrictest.Topology{
		Bridged:   []node.Endpoint{cam},
		Configure: func(cfg *node.Config) { cfg.TLS = &node.TLSConfig{CertificatePort: tlsKeys{}} },
	})
	var root *endpoint.Endpoint
	for _, ep := range d.Endpoints() {
		if ep.IsRoot() {
			root = ep
		}
	}
	if root == nil || root.ID != 0 {
		t.Fatal("no root endpoint")
	}
	certs, ok := serverOn(root, tcmdef.ClusterID).(*tls.CertificateManagement)
	if !ok {
		t.Fatal("TlsCertificateManagement is not on endpoint 0")
	}
	client, ok := serverOn(root, tcldef.ClusterID).(*tls.ClientManagement)
	if !ok {
		t.Fatal("TlsClientManagement is not on endpoint 0")
	}
	if d.Node().Root().TLSCertificateManagement != certs || d.Node().Root().TLSClientManagement != client {
		t.Fatal("RootClusters does not name the mounted servers")
	}

	resp, err := invokeOn(t, certs, tcmdef.CmdProvisionRootCertificate, tcmdef.ProvisionRootCertificateRequest{Certificate: []byte("root"), Caid: spec.NullOf[uint16]()})
	if err != nil {
		t.Fatal(err)
	}
	caid := resp.(tcmdef.ProvisionRootCertificateResponse).Caid
	resp, err = invokeOn(t, certs, tcmdef.CmdClientCsr, tcmdef.ClientCsrRequest{Nonce: make([]byte, 32), Ccdid: spec.NullOf[uint16]()})
	if err != nil {
		t.Fatal(err)
	}
	ccdid := resp.(tcmdef.ClientCsrResponse).Ccdid
	if _, err = invokeOn(t, certs, tcmdef.CmdProvisionClientCertificate, tcmdef.ProvisionClientCertificateRequest{Ccdid: ccdid, ClientCertificate: []byte("client")}); err != nil {
		t.Fatal(err)
	}
	resp, err = invokeOn(t, client, tcldef.CmdProvisionEndpoint, tcldef.ProvisionEndpointRequest{
		Hostname: []byte("upload.example"), Port: 443, Caid: caid, Ccdid: spec.ValueOf(ccdid), EndpointId: spec.NullOf[uint16](),
	})
	if err != nil {
		t.Fatal(err)
	}
	tlsEndpoint := resp.(tcldef.ProvisionEndpointResponse).EndpointId

	num, ok := d.EndpointOf("cam")
	if !ok {
		t.Fatal("the camera is not served")
	}
	var push *camera.PushTransport
	for _, ep := range d.Endpoints() {
		if ep.ID == num {
			push, _ = serverOn(ep, padef.ClusterID).(*camera.PushTransport)
		}
	}
	if push == nil {
		t.Fatal("the camera serves no kit PushAvStreamTransport")
	}
	opts := padef.TransportOptionsStruct{
		StreamUsage: padef.StreamUsageLiveView, TlsEndpointId: tlsEndpoint + 1, Url: "https://upload.example/cam/",
		TriggerOptions:   padef.TransportTriggerOptionsStruct{TriggerType: padef.TransportTriggerTypeContinuous},
		ContainerOptions: padef.ContainerOptionsStruct{CmafContainerOptions: &padef.CMAFContainerOptionsStruct{SegmentDuration: 1000}},
	}
	_, err = invokeOn(t, push, padef.CmdAllocatePushTransport, padef.AllocatePushTransportRequest{TransportOptions: opts})
	var cs im.MatterClusterStatusError
	if !errors.As(err, &cs) || cs.MatterClusterStatus() != uint8(padef.StatusCodeInvalidTlsEndpoint) {
		t.Fatalf("an unprovisioned TLS endpoint: %v", err)
	}
	// The node's endpoint is valid: the check moves on to the stream usage,
	// which the camera without streams does not support.
	opts.TlsEndpointId = tlsEndpoint
	_, err = invokeOn(t, push, padef.CmdAllocatePushTransport, padef.AllocatePushTransportRequest{TransportOptions: opts})
	if !errors.As(err, &cs) || cs.MatterClusterStatus() != uint8(padef.StatusCodeInvalidStreamUsage) {
		t.Fatalf("the node's TLS endpoint: %v", err)
	}
}

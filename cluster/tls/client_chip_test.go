// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// TlsClientManagement: a port of connectedhomeip
// src/app/clusters/tls-client-management-server/tests/
// TestTLSClientManagementCluster.cpp at 6170af84, by meaning. chip's
// MockCertificateTable holds two root certificates of the test fabric
// (:292-295); here a CertificateManagement holds two the test provisions
// first. chip's MockTLSClientManagementDelegate (:150-282) keeps the
// endpoints in the test from id 1; here the server keeps them, with
// chip's IncrementingIdHelper ids from 0.
package tls_test

import (
	"bytes"
	"context"
	"slices"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	tcldef "github.com/SukramJ/go-fabric/cluster/spec/tlsclientmanagement"
	"github.com/SukramJ/go-fabric/cluster/tls"
	"github.com/SukramJ/go-fabric/im"
)

const testMaxProvisioned = 10 // :42

// newClient builds a ClientManagement over a CertificateManagement that
// holds two root certificates of the test fabric (chip's SetUp, :292-295).
func newClient(t *testing.T) (*tls.ClientManagement, *tls.CertificateManagement, []uint16) {
	t.Helper()
	certs, _ := newCerts(t)
	roots := []uint16{provisionRoot(t, certs, testCertificate(t, 1)), provisionRoot(t, certs, testCertificate(t, 2))}
	m, err := tls.NewClientManagement(context.Background(), tls.ClientManagementConfig{MaxProvisioned: testMaxProvisioned, Certificates: certs})
	if err != nil {
		t.Fatal(err)
	}
	return m, certs, roots
}

func provisionRequest(caid uint16) tcldef.ProvisionEndpointRequest {
	return tcldef.ProvisionEndpointRequest{
		Hostname: []byte("example.com"), Port: 443, Caid: caid,
		Ccdid: spec.NullOf[uint16](), EndpointId: spec.NullOf[uint16](),
	}
}

func provisionEndpoint(t *testing.T, m *tls.ClientManagement, req tcldef.ProvisionEndpointRequest) uint16 {
	t.Helper()
	resp, err := invoke[tcldef.ProvisionEndpointResponse](t, m, peer(testFabric), tcldef.CmdProvisionEndpoint, req)
	wantStatus(t, "ProvisionEndpoint", err, im.StatusSuccess)
	return resp.EndpointId
}

func clusterCode(err error) (uint8, bool) {
	cs, ok := err.(im.MatterClusterStatusError)
	if !ok {
		return 0, false
	}
	return cs.MatterClusterStatus(), true
}

// TestClientAttributesList (:317-327): the mandatory attributes only.
func TestClientAttributesList(t *testing.T) {
	m, _, _ := newClient(t)
	got := m.MatterAttributes()
	for _, id := range []uint32{tcldef.AttrMaxProvisioned, tcldef.AttrProvisionedEndpoints} {
		if !slices.Contains(got, id) {
			t.Fatalf("AttributeList %v lacks 0x%04X", got, id)
		}
	}
}

// TestClientAcceptedCommands (:329-344).
func TestClientAcceptedCommands(t *testing.T) {
	m, _, _ := newClient(t)
	got := slices.Sorted(slices.Values(m.MatterAcceptedCommands()))
	want := []uint32{tcldef.CmdProvisionEndpoint, tcldef.CmdFindEndpoint, tcldef.CmdRemoveEndpoint}
	if !slices.Equal(got, want) {
		t.Fatalf("AcceptedCommandList %v, want %v", got, want)
	}
}

// TestClientReadMaxProvisioned (:346-351).
func TestClientReadMaxProvisioned(t *testing.T) {
	m, _, _ := newClient(t)
	if got := readAs[uint8](t, m.MatterRead, tcldef.AttrMaxProvisioned); got != testMaxProvisioned {
		t.Fatalf("MaxProvisioned %d", got)
	}
}

// TestClientReadClusterRevision (:353-358).
func TestClientReadClusterRevision(t *testing.T) {
	m, _, _ := newClient(t)
	if got := readAs[uint16](t, m.MatterRead, 0xFFFD); got != tcldef.Revision {
		t.Fatalf("ClusterRevision %d", got)
	}
}

// TestClientReadFeatureMap (:360-365).
func TestClientReadFeatureMap(t *testing.T) {
	m, _, _ := newClient(t)
	if got := readAs[uint32](t, m.MatterRead, 0xFFFC); got != 0 {
		t.Fatalf("FeatureMap %d", got)
	}
}

// TestClientReadProvisionedEndpointsEmpty (:367-374).
func TestClientReadProvisionedEndpointsEmpty(t *testing.T) {
	m, _, _ := newClient(t)
	if got := readAs[[]tcldef.TLSEndpointStruct](t, m.MatterRead, tcldef.AttrProvisionedEndpoints); len(got) != 0 {
		t.Fatalf("ProvisionedEndpoints %v", got)
	}
}

// TestProvisionEndpointSuccess (:376-394): the endpoint is stored under
// the first id.
func TestProvisionEndpointSuccess(t *testing.T) {
	m, _, roots := newClient(t)
	if id := provisionEndpoint(t, m, provisionRequest(roots[0])); id != 0 {
		t.Fatalf("EndpointID %d, want the first id 0", id)
	}
	got := readAs[[]tcldef.TLSEndpointStruct](t, m.MatterRead, tcldef.AttrProvisionedEndpoints)
	if len(got) != 1 || got[0].EndpointId != 0 || got[0].FabricIndex != testFabric {
		t.Fatalf("ProvisionedEndpoints %+v", got)
	}
}

// TestProvisionEndpointConstraintErrors (:396-416): a hostname of 3
// bytes is CONSTRAINT_ERROR; a CAID the fabric does not hold fails.
func TestProvisionEndpointConstraintErrors(t *testing.T) {
	m, _, roots := newClient(t)
	req := provisionRequest(roots[0])
	req.Hostname = []byte("abc")
	_, err := m.MatterInvoke(peer(testFabric), tcldef.CmdProvisionEndpoint, req)
	wantStatus(t, "short hostname", err, im.StatusConstraintError)
	req = provisionRequest(256)
	if _, err = m.MatterInvoke(peer(testFabric), tcldef.CmdProvisionEndpoint, req); err == nil {
		t.Fatal("CAID 256 provisioned")
	}
}

// TestProvisionEndpointRootCertNotFound (:418-432).
func TestProvisionEndpointRootCertNotFound(t *testing.T) {
	m, _, _ := newClient(t)
	_, err := m.MatterInvoke(peer(testFabric), tcldef.CmdProvisionEndpoint, provisionRequest(99))
	wantStatus(t, "unknown root", err, im.StatusFailure)
	if code, ok := clusterCode(err); !ok || code != uint8(tcldef.StatusCodeRootCertificateNotFound) {
		t.Fatalf("cluster status %d %v", code, ok)
	}
}

// TestProvisionEndpointClientCertNotFound (:434-443).
func TestProvisionEndpointClientCertNotFound(t *testing.T) {
	m, _, roots := newClient(t)
	req := provisionRequest(roots[0])
	req.Ccdid = spec.ValueOf[uint16](99)
	_, err := m.MatterInvoke(peer(testFabric), tcldef.CmdProvisionEndpoint, req)
	if code, ok := clusterCode(err); !ok || code != uint8(tcldef.StatusCodeClientCertificateNotFound) {
		t.Fatalf("cluster status %d %v (%v)", code, ok, err)
	}
}

// TestFindEndpointSuccess (:445-475).
func TestFindEndpointSuccess(t *testing.T) {
	m, _, roots := newClient(t)
	req := provisionRequest(roots[0])
	id := provisionEndpoint(t, m, req)
	resp, err := invoke[tcldef.FindEndpointResponse](t, m, peer(testFabric), tcldef.CmdFindEndpoint, tcldef.FindEndpointRequest{EndpointId: id})
	wantStatus(t, "FindEndpoint", err, im.StatusSuccess)
	if resp.Endpoint.EndpointId != id || !bytes.Equal(resp.Endpoint.Hostname, req.Hostname) {
		t.Fatalf("FindEndpointResponse %+v", resp)
	}
}

// TestFindEndpointNotFound (:477-488).
func TestFindEndpointNotFound(t *testing.T) {
	m, _, _ := newClient(t)
	_, err := m.MatterInvoke(peer(testFabric), tcldef.CmdFindEndpoint, tcldef.FindEndpointRequest{EndpointId: 999})
	wantStatus(t, "FindEndpoint", err, im.StatusNotFound)
}

// TestFindEndpointConstraintError (:490-501).
func TestFindEndpointConstraintError(t *testing.T) {
	m, _, _ := newClient(t)
	_, err := m.MatterInvoke(peer(testFabric), tcldef.CmdFindEndpoint, tcldef.FindEndpointRequest{EndpointId: 65535})
	wantStatus(t, "FindEndpoint", err, im.StatusConstraintError)
}

// TestRemoveEndpointSuccess (:503-532).
func TestRemoveEndpointSuccess(t *testing.T) {
	m, _, roots := newClient(t)
	id := provisionEndpoint(t, m, provisionRequest(roots[0]))
	_, err := m.MatterInvoke(peer(testFabric), tcldef.CmdRemoveEndpoint, tcldef.RemoveEndpointRequest{EndpointId: id})
	wantStatus(t, "RemoveEndpoint", err, im.StatusSuccess)
	if got := readAs[[]tcldef.TLSEndpointStruct](t, m.MatterRead, tcldef.AttrProvisionedEndpoints); len(got) != 0 {
		t.Fatalf("ProvisionedEndpoints %+v", got)
	}
}

// TestRemoveEndpointNotFound (:534-545).
func TestRemoveEndpointNotFound(t *testing.T) {
	m, _, _ := newClient(t)
	_, err := m.MatterInvoke(peer(testFabric), tcldef.CmdRemoveEndpoint, tcldef.RemoveEndpointRequest{EndpointId: 999})
	wantStatus(t, "RemoveEndpoint", err, im.StatusNotFound)
}

// TestRemoveEndpointConstraintError (:547-558).
func TestRemoveEndpointConstraintError(t *testing.T) {
	m, _, _ := newClient(t)
	_, err := m.MatterInvoke(peer(testFabric), tcldef.CmdRemoveEndpoint, tcldef.RemoveEndpointRequest{EndpointId: 65535})
	wantStatus(t, "RemoveEndpoint", err, im.StatusConstraintError)
}

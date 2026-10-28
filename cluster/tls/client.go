// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package tls

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"

	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/cluster/spec"
	tcmdef "github.com/SukramJ/go-fabric/cluster/spec/tlscertificatemanagement"
	tcldef "github.com/SukramJ/go-fabric/cluster/spec/tlsclientmanagement"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
)

// ClientManagementClusterID is the TlsClientManagement cluster id.
const ClientManagementClusterID = tcldef.ClusterID

// Endpoint is an entry of ProvisionedEndpoints.
type Endpoint = tcldef.TLSEndpointStruct

// The spec's bounds (connectedhomeip TLSClientManagementCluster.cpp:43-45,
// TLSClientManagementCluster.h:35).
const (
	MaxEndpointID     uint16 = 65534
	MinHostnameLength int    = 4
	MaxHostnameLength int    = 253
)

// Configuration errors.
var (
	// ErrClientConfig reports a configuration chip's constructor refuses
	// (TLSClientManagementCluster.cpp:59-60).
	ErrClientConfig = errors.New("tls: TlsClientManagement configuration outside the cluster's model")
	// ErrNoEndpoint reports a reference count change of an endpoint the
	// fabric did not provision.
	ErrNoEndpoint = errors.New("tls: no provisioned TLS endpoint of the id")
	// ErrReferenceCount reports a reference count that would pass 255
	// (chip's CHIP_ERROR_INVALID_ARGUMENT,
	// tls-client-management-instance.cpp:422).
	ErrReferenceCount = errors.New("tls: the endpoint's reference count would pass 255")
)

// CertificateLookup is the node's certificates an endpoint names: the
// TlsCertificateManagement server ([CertificateManagement], its typed
// server's Host) is one.
type CertificateLookup interface {
	RootCertificate(fabricIndex uint8, caid uint16) (tcmdef.TLSCertStruct, bool)
	ClientCertificate(fabricIndex uint8, ccdid uint16) (tcmdef.TLSClientCertificateDetailStruct, bool)
}

// ClientManagementConfig carries the construction parameters of a
// [ClientManagement].
type ClientManagementConfig struct {
	// MaxProvisioned is the per-fabric capacity, 5 to 254 (chip :59-60).
	MaxProvisioned uint8
	// Certificates are the certificates an endpoint may name; nil names
	// none, so every ProvisionEndpoint is RootCertificateNotFound (chip
	// holds the CertificateTable, :53-58).
	Certificates CertificateLookup
	// Store persists the endpoints under Key (the device's StableKey); nil
	// keeps them in memory.
	Store Store
	Key   string
	// DataVersion is an optional host-owned tracker.
	DataVersion *cluster.DataVersionTracker
}

// ClientManagement implements [contract.ClusterServer] for
// TlsClientManagement: connectedhomeip's server logic over the generated
// server — ProvisionEndpoint, FindEndpoint and RemoveEndpoint with chip's
// checks and statuses, the per-fabric endpoint table with node-wide ids
// and reference counts, the removal of a fabric's endpoints — and the
// TlsCertificateManagement server's [DependencyChecker]. Mirrors
// TLSClientManagementCluster.cpp and the all-clusters example delegate
// (tls-client-management-instance.cpp) at 6170af84 (matter.js's server is
// the generated behaviour).
type ClientManagement struct {
	*spec.Instance
	spec.SelfHeld

	srv   *spec.Server
	certs CertificateLookup
	store Store
	key   string
	cfg   ClientManagementConfig

	// mu guards the state below.
	mu  sync.Mutex
	rec endpointRecord
	ids idAllocator
}

// Compile-time assertions.
var (
	_ contract.ClusterServer                  = (*ClientManagement)(nil)
	_ contract.ClusterDataVersion             = (*ClientManagement)(nil)
	_ contract.ClusterAttributeLister         = (*ClientManagement)(nil)
	_ contract.ClusterCommandLister           = (*ClientManagement)(nil)
	_ contract.ClusterAttributeWritePrivilege = (*ClientManagement)(nil)
	_ contract.ClusterCommandInvokePrivilege  = (*ClientManagement)(nil)
	_ contract.AttributeChangeNotifier        = (*ClientManagement)(nil)
	_ contract.FabricScopedReader             = (*ClientManagement)(nil)
	_ spec.CommandRegistrar                   = (*ClientManagement)(nil)
	_ tcldef.Host                             = (*ClientManagement)(nil)
	_ DependencyChecker                       = (*ClientManagement)(nil)
	_ CertificateLookup                       = (*CertificateManagement)(nil)
)

// NewClientManagement builds a TlsClientManagement server with the
// endpoints the store saved for cfg.Key. Mirrors chip's constructor
// (TLSClientManagementCluster.cpp:53-63). When cfg.Certificates is a
// [CertificateManagement] without a dependency checker, the server
// installs itself as its checker.
func NewClientManagement(ctx context.Context, cfg ClientManagementConfig) (*ClientManagement, error) {
	if cfg.MaxProvisioned < 5 || cfg.MaxProvisioned > 254 { // :59-60
		return nil, fmt.Errorf("%w: MaxProvisioned %d", ErrClientConfig, cfg.MaxProvisioned)
	}
	m := &ClientManagement{certs: cfg.Certificates, store: cfg.Store, key: settingEndpoints + cfg.Key, cfg: cfg, ids: idAllocator{maxID: 0xFFFF}}
	if err := load(ctx, cfg.Store, m.key, &m.rec); err != nil {
		return nil, err
	}
	m.ids.next = m.rec.Next
	handlers := map[uint32]spec.CommandHandler{
		tcldef.CmdProvisionEndpoint: m.provision,
		tcldef.CmdFindEndpoint:      m.find,
		tcldef.CmdRemoveEndpoint:    m.remove,
	}
	srv, err := spec.NewServer(tcldef.Definition, spec.Options{}, spec.ServerConfig{Handlers: handlers, DataVersion: cfg.DataVersion})
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrClientConfig, err)
	}
	m.srv, m.Instance, m.SelfHeld = srv, srv.Instance, spec.NewSelfHeld(srv)
	if err := srv.SetAttributes(map[uint32]any{
		tcldef.AttrMaxProvisioned:       cfg.MaxProvisioned,
		tcldef.AttrProvisionedEndpoints: m.listValue(),
	}); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrClientConfig, err)
	}
	if cm, ok := cfg.Certificates.(*CertificateManagement); ok {
		cm.mu.Lock()
		if cm.checker == nil {
			cm.checker = m
		}
		cm.mu.Unlock()
	}
	return m, nil
}

func endpointStruct(e endpointEntry) Endpoint {
	ccdid := spec.NullOf[uint16]()
	if e.CCDID != nil {
		ccdid = spec.ValueOf(*e.CCDID)
	}
	return Endpoint{
		EndpointId: e.EndpointID, Hostname: bytes.Clone(e.Hostname), Port: e.Port, Caid: e.CAID,
		Ccdid: ccdid, ReferenceCount: e.ReferenceCount, FabricIndex: e.Fabric,
	}
}

// listValue is ProvisionedEndpoints at the current table; the caller holds
// mu or owns m alone.
func (m *ClientManagement) listValue() spec.List[Endpoint] {
	out := make(spec.List[Endpoint], 0, len(m.rec.Endpoints))
	for _, e := range m.rec.Endpoints {
		out = append(out, endpointStruct(e))
	}
	return out
}

func (m *ClientManagement) index(fabric uint8, endpointID uint16) int {
	return slices.IndexFunc(m.rec.Endpoints, func(e endpointEntry) bool { return e.Fabric == fabric && e.EndpointID == endpointID })
}

// commitLocked saves the table; a failed save restores prev. The caller
// holds mu.
func (m *ClientManagement) commitLocked(ctx context.Context, prev endpointRecord) error {
	m.rec.Next = m.ids.next
	if err := save(ctx, m.store, m.key, m.rec); err != nil {
		m.rec = prev
		m.ids.next = prev.Next
		return spec.Errorf(im.StatusFailure, "TlsClientManagement: %v", err)
	}
	return nil
}

func (m *ClientManagement) publish() {
	m.mu.Lock()
	v := m.listValue()
	m.mu.Unlock()
	_ = m.srv.SetAttributesFrom(contract.OriginCommand, map[uint32]any{tcldef.AttrProvisionedEndpoints: v})
}

func cloneEndpoints(r endpointRecord) endpointRecord {
	out := r
	out.Endpoints = slices.Clone(r.Endpoints)
	return out
}

// ProvisionedEndpoint implements [tcldef.Host].
func (m *ClientManagement) ProvisionedEndpoint(fabricIndex uint8, endpointID uint16) (Endpoint, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if i := m.index(fabricIndex, endpointID); i >= 0 {
		return endpointStruct(m.rec.Endpoints[i]), true
	}
	return Endpoint{}, false
}

// MutateEndpointReferenceCount implements [tcldef.Host]: delta moves the
// count, never below 0; past 255 is [ErrReferenceCount]. Mirrors chip's
// example delegate (tls-client-management-instance.cpp:410-432).
func (m *ClientManagement) MutateEndpointReferenceCount(fabricIndex uint8, endpointID uint16, delta int8) error {
	m.mu.Lock()
	i := m.index(fabricIndex, endpointID)
	if i < 0 {
		m.mu.Unlock()
		return ErrNoEndpoint
	}
	prev := cloneEndpoints(m.rec)
	e := &m.rec.Endpoints[i]
	if -int(delta) <= int(e.ReferenceCount) { // :418
		if int(delta)+int(e.ReferenceCount) > 255 { // :420-421
			m.mu.Unlock()
			return ErrReferenceCount
		}
		e.ReferenceCount = uint8(int(e.ReferenceCount) + int(delta)) //nolint:gosec // 0..255, checked above
	} else {
		e.ReferenceCount = 0 // :426
	}
	err := m.commitLocked(context.Background(), prev)
	m.mu.Unlock()
	if err != nil {
		return err
	}
	m.publish()
	return nil
}

// RootCertificateInUse implements [DependencyChecker]: a provisioned
// endpoint of the fabric names caid. Mirrors chip's example delegate
// RootCertCanBeRemoved (tls-client-management-instance.cpp:364-375).
func (m *ClientManagement) RootCertificateInUse(fabricIndex uint8, caid uint16) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return slices.ContainsFunc(m.rec.Endpoints, func(e endpointEntry) bool { return e.Fabric == fabricIndex && e.CAID == caid })
}

// ClientCertificateInUse implements [DependencyChecker] for a client
// certificate (ClientCertCanBeRemoved, :377-389).
func (m *ClientManagement) ClientCertificateInUse(fabricIndex uint8, ccdid uint16) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return slices.ContainsFunc(m.rec.Endpoints, func(e endpointEntry) bool {
		return e.Fabric == fabricIndex && e.CCDID != nil && *e.CCDID == ccdid
	})
}

// clusterStatus is a TlsClientManagement cluster status: FAILURE with
// the code (chip's ClusterStatusCode::ClusterSpecificFailure).
type clusterStatus struct {
	code tcldef.StatusCodeEnum
	msg  string
}

func (e clusterStatus) Error() string                 { return e.msg }
func (clusterStatus) MatterStatusCode() im.StatusCode { return im.StatusFailure }
func (e clusterStatus) MatterClusterStatus() uint8    { return uint8(e.code) }

var _ im.MatterClusterStatusError = clusterStatus{}

// provision carries out ProvisionEndpoint. Mirrors chip
// HandleProvisionEndpoint (TLSClientManagementCluster.cpp:177-216) and the
// example delegate's ProvisionEndpoint
// (tls-client-management-instance.cpp:262-328): the hostname and CAID
// bounds, the fabric's root certificate (RootCertificateNotFound) and
// client certificate (ClientCertificateNotFound), then a hostname and port
// another endpoint of the fabric holds (EndpointAlreadyInstalled); a null
// EndpointID adds an endpoint within MaxProvisioned, an EndpointID replaces
// the fabric's entry of it (NOT_FOUND without one) with its reference
// count reset.
func (m *ClientManagement) provision(ctx context.Context, fields any) (any, error) {
	req, err := requestOf[tcldef.ProvisionEndpointRequest](fields, "TlsClientManagement", "ProvisionEndpoint")
	if err != nil {
		return nil, err
	}
	switch {
	case len(req.Hostname) < MinHostnameLength || len(req.Hostname) > MaxHostnameLength: // :182-185
		return nil, spec.Errorf(im.StatusConstraintError, "TlsClientManagement: hostname of %d bytes", len(req.Hostname))
	case req.Caid > MaxCertificateID: // :187-190
		return nil, spec.Errorf(im.StatusConstraintError, "TlsClientManagement: CAID %d", req.Caid)
	}
	fabric := accessingFabric(ctx)
	if !m.hasRoot(fabric, req.Caid) { // :193-196
		return nil, clusterStatus{tcldef.StatusCodeRootCertificateNotFound, fmt.Sprintf("TlsClientManagement: root certificate %d not found", req.Caid)}
	}
	if !req.Ccdid.Null && !m.hasClient(fabric, req.Ccdid.Value) { // :197-200
		return nil, clusterStatus{tcldef.StatusCodeClientCertificateNotFound, fmt.Sprintf("TlsClientManagement: client certificate %d not found", req.Ccdid.Value)}
	}
	m.mu.Lock()
	inFabric := 0
	for _, e := range m.rec.Endpoints {
		if e.Fabric != fabric {
			continue
		}
		inFabric++
		if bytes.Equal(e.Hostname, req.Hostname) && e.Port == req.Port && (req.EndpointId.Null || req.EndpointId.Value != e.EndpointID) { // :280-288
			m.mu.Unlock()
			return nil, clusterStatus{tcldef.StatusCodeEndpointAlreadyInstalled, "TlsClientManagement: the hostname and port are provisioned"}
		}
	}
	prev := cloneEndpoints(m.rec)
	var ccdid *uint16
	if !req.Ccdid.Null {
		v := req.Ccdid.Value
		ccdid = &v
	}
	entry := endpointEntry{Fabric: fabric, Hostname: bytes.Clone(req.Hostname), Port: req.Port, CAID: req.Caid, CCDID: ccdid} // :315-321
	if req.EndpointId.Null {
		if inFabric >= int(m.cfg.MaxProvisioned) { // :296-297
			m.mu.Unlock()
			return nil, spec.Errorf(im.StatusResourceExhausted, "TlsClientManagement: %d endpoints", inFabric)
		}
		id, ok := m.ids.allocate(func(id uint16) bool {
			return slices.ContainsFunc(m.rec.Endpoints, func(e endpointEntry) bool { return e.EndpointID == id })
		})
		if !ok { // :302
			m.mu.Unlock()
			return nil, spec.Errorf(im.StatusResourceExhausted, "TlsClientManagement: no free endpoint id")
		}
		entry.EndpointID = id
		m.rec.Endpoints = append(m.rec.Endpoints, entry)
	} else {
		i := m.index(fabric, req.EndpointId.Value)
		if i < 0 { // :309-311
			m.mu.Unlock()
			return nil, spec.Errorf(im.StatusNotFound, "TlsClientManagement: endpoint %d not found", req.EndpointId.Value)
		}
		entry.EndpointID = req.EndpointId.Value
		m.rec.Endpoints[i] = entry
	}
	if err = m.commitLocked(ctx, prev); err != nil {
		m.mu.Unlock()
		return nil, err
	}
	m.mu.Unlock()
	m.publish() // :209
	return tcldef.ProvisionEndpointResponse{EndpointId: entry.EndpointID}, nil
}

func (m *ClientManagement) hasRoot(fabric uint8, caid uint16) bool {
	if m.certs == nil {
		return false
	}
	_, ok := m.certs.RootCertificate(fabric, caid)
	return ok
}

func (m *ClientManagement) hasClient(fabric uint8, ccdid uint16) bool {
	if m.certs == nil {
		return false
	}
	_, ok := m.certs.ClientCertificate(fabric, ccdid)
	return ok
}

// find carries out FindEndpoint. Mirrors chip HandleFindEndpoint
// (:217-248).
func (m *ClientManagement) find(ctx context.Context, fields any) (any, error) {
	req, err := requestOf[tcldef.FindEndpointRequest](fields, "TlsClientManagement", "FindEndpoint")
	if err != nil {
		return nil, err
	}
	if req.EndpointId > MaxEndpointID { // :221
		return nil, spec.Errorf(im.StatusConstraintError, "TlsClientManagement: endpoint id %d", req.EndpointId)
	}
	e, ok := m.ProvisionedEndpoint(accessingFabric(ctx), req.EndpointId)
	if !ok { // :237
		return nil, spec.Errorf(im.StatusNotFound, "TlsClientManagement: endpoint %d not found", req.EndpointId)
	}
	return tcldef.FindEndpointResponse{Endpoint: e}, nil
}

// remove carries out RemoveEndpoint. Mirrors chip HandleRemoveEndpoint
// (:249-271) and the example delegate's RemoveProvisionedEndpointByID
// (tls-client-management-instance.cpp:341-362): an unknown endpoint is
// NOT_FOUND; one in use (a reference count above 0) answers the model's
// cluster status EndpointInUse, where chip's example delegate answers
// INVALID_IN_STATE (:352) — owner decision 2026-10-28.
func (m *ClientManagement) remove(ctx context.Context, fields any) (any, error) {
	req, err := requestOf[tcldef.RemoveEndpointRequest](fields, "TlsClientManagement", "RemoveEndpoint")
	if err != nil {
		return nil, err
	}
	if req.EndpointId > MaxEndpointID { // :254-257
		return nil, spec.Errorf(im.StatusConstraintError, "TlsClientManagement: endpoint id %d", req.EndpointId)
	}
	fabric := accessingFabric(ctx)
	m.mu.Lock()
	i := m.index(fabric, req.EndpointId)
	switch {
	case i < 0: // :350
		m.mu.Unlock()
		return nil, spec.Errorf(im.StatusNotFound, "TlsClientManagement: endpoint %d not found", req.EndpointId)
	case m.rec.Endpoints[i].ReferenceCount != 0: // :352
		m.mu.Unlock()
		return nil, clusterStatus{tcldef.StatusCodeEndpointInUse, fmt.Sprintf("TlsClientManagement: endpoint %d is in use", req.EndpointId)}
	}
	prev := cloneEndpoints(m.rec)
	m.rec.Endpoints = slices.Delete(m.rec.Endpoints, i, i+1)
	err = m.commitLocked(ctx, prev)
	m.mu.Unlock()
	if err != nil {
		return nil, err
	}
	m.publish() // :266
	return nil, nil
}

// MatterFabricRemoved drops the removed fabric's endpoints. Mirrors chip
// OnFabricRemoved (:272-275) and the example delegate's RemoveFabric
// (tls-client-management-instance.cpp:391-408).
func (m *ClientManagement) MatterFabricRemoved(fabricIndex uint8) {
	m.FabricRemoved(context.Background(), fabricIndex)
}

// FabricRemoved is [ClientManagement.MatterFabricRemoved] on ctx, the
// node's call inside RemoveFabric.
func (m *ClientManagement) FabricRemoved(ctx context.Context, fabricIndex uint8) {
	m.mu.Lock()
	prev := cloneEndpoints(m.rec)
	m.rec.Endpoints = slices.DeleteFunc(m.rec.Endpoints, func(e endpointEntry) bool { return e.Fabric == fabricIndex })
	changed := len(prev.Endpoints) != len(m.rec.Endpoints)
	if changed {
		_ = m.commitLocked(ctx, prev)
	}
	m.mu.Unlock()
	if changed {
		m.publish()
	}
}

// MatterDataVersion implements [contract.ClusterDataVersion].
func (m *ClientManagement) MatterDataVersion() uint32 { return m.srv.MatterDataVersion() }

// OnAttributeChange implements [contract.AttributeChangeNotifier].
func (m *ClientManagement) OnAttributeChange(cb func(change contract.Change)) (unsubscribe func()) {
	return m.srv.OnAttributeChange(cb)
}

// MatterRead resolves an attribute for a local caller: ProvisionedEndpoints
// holds every fabric's endpoints.
func (m *ClientManagement) MatterRead(attrID uint32) (any, bool) { return m.srv.MatterRead(attrID) }

// MatterReadFiltered implements [contract.FabricScopedReader]: a peer's
// read of ProvisionedEndpoints sees its own fabric's endpoints only, as
// chip's EncodeProvisionedEndpoints (:141-146) iterates the accessing
// fabric's.
func (m *ClientManagement) MatterReadFiltered(ctx context.Context, attrID uint32) (any, bool) {
	_, fabric, remote := im.LookupFabricFilter(ctx)
	if !remote || attrID != tcldef.AttrProvisionedEndpoints {
		return m.MatterRead(attrID) //nolint:contextcheck // MatterRead is the unfiltered cluster-interface read
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	out := spec.List[Endpoint]{}
	for _, e := range m.rec.Endpoints {
		if fabric != 0 && e.Fabric == fabric {
			out = append(out, endpointStruct(e))
		}
	}
	return out, true
}

// MatterWrite answers every write by the definition: the cluster has no
// writable attribute.
func (m *ClientManagement) MatterWrite(ctx context.Context, attrID uint32, value any) error {
	return m.srv.MatterWrite(ctx, attrID, value)
}

// MatterInvoke dispatches an accepted command; a host's handler runs in
// its place ([spec.SelfHeld.Handle]).
func (m *ClientManagement) MatterInvoke(ctx context.Context, cmdID uint32, fields any) (any, error) {
	return spec.InvokeHeld(ctx, &m.SelfHeld, cmdID, fields)
}

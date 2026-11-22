// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package tls

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"slices"
	"sync"

	"thingfabric.dev/go-fabric/cluster"
	"thingfabric.dev/go-fabric/cluster/spec"
	tcmdef "thingfabric.dev/go-fabric/cluster/spec/tlscertificatemanagement"
	"thingfabric.dev/go-fabric/contract"
	"thingfabric.dev/go-fabric/im"
)

// CertificateManagementClusterID is the TlsCertificateManagement cluster id.
const CertificateManagementClusterID = tcmdef.ClusterID

// Generated types the certificate management speaks.
type (
	// RootCertificate is an entry of ProvisionedRootCertificates.
	RootCertificate = tcmdef.TLSCertStruct
	// ClientCertificate is an entry of ProvisionedClientCertificates.
	ClientCertificate = tcmdef.TLSClientCertificateDetailStruct
	// CertificatePort is the node's TLS key store.
	CertificatePort = tcmdef.CertificatePort
	// DependencyChecker answers whether a certificate is in use.
	DependencyChecker = tcmdef.DependencyChecker
)

// The spec's bounds (connectedhomeip TLSCertificateManagementCluster.cpp
// :43-47, CertificateTable.h:35-36).
const (
	MaxCertificateBytes                = 3000
	MaxFingerprintBytes                = 64
	MaxIntermediateCertificates        = 10
	NonceBytes                         = 32
	MaxSignatureBytes                  = 128
	MaxCertificateID            uint16 = 65534
)

// Configuration errors.
var (
	// ErrCertificateConfig reports a configuration chip's constructor
	// refuses (TLSCertificateManagementCluster.cpp:70-71).
	ErrCertificateConfig = errors.New("tls: TlsCertificateManagement configuration outside the cluster's model")
	// ErrNoCertificatePort reports a server built without its port.
	ErrNoCertificatePort = errors.New("tls: TlsCertificateManagement needs a CertificatePort")
)

// CertificateManagementConfig carries the construction parameters of a
// [CertificateManagement].
type CertificateManagementConfig struct {
	// MaxRootCertificates and MaxClientCertificates are the per-fabric
	// capacities, at least 5 each (chip :70-71; CodegenIntegration.cpp
	// :117-118 passes the per-fabric maxima).
	MaxRootCertificates   uint8
	MaxClientCertificates uint8
	// Port is the key store; required.
	Port CertificatePort
	// Store persists the tables under Key (the device's StableKey); nil
	// keeps them in memory.
	Store Store
	Key   string
	// Checker refuses the removal of a certificate a TLS endpoint names;
	// nil allows every removal ([CertificateManagement.SetDependencyChecker]).
	Checker DependencyChecker
	// DataVersion is an optional host-owned tracker.
	//
	// Deprecated: a host-owned tracker adds nothing — the wire carries the
	// endpoint's DataVersion for the cluster, or the server's own where it
	// serves one (decision 165, F-db021da0-46); leave it nil. Removable in
	// v0.25.0.
	DataVersion *cluster.DataVersionTracker
}

// CertificateManagement implements [contract.ClusterServer] for
// TlsCertificateManagement: connectedhomeip's server logic over the
// generated server — the nine commands with chip's checks and statuses,
// the per-fabric root and client certificate tables with node-wide ids,
// the removal of a fabric's entries — while the [CertificatePort] holds
// the keys. Mirrors TLSCertificateManagementCluster.cpp at 6170af84
// (matter.js's server is the generated behaviour).
//
// A read of ProvisionedRootCertificates or ProvisionedClientCertificates
// serves the full entries, certificates included, as matter.js's generated
// behaviour does (rule 1). chip encodes the ids alone on a transport
// without large payloads (TLSCertificateManagementCluster.cpp:136-166);
// that is not mirrored.
type CertificateManagement struct {
	*spec.Instance
	spec.SelfHeld

	srv   *spec.Server
	port  CertificatePort
	store Store
	key   string
	cfg   CertificateManagementConfig

	// mu guards the state below. Every locked section runs through
	// [CertificateManagement.locked], so a panic in one cannot strand the
	// lock (TestEveryLockIsDeferred). A command checks under mu, calls the
	// port without it, and checks again under mu before it acts: another
	// session of the fabric may have acted in the port's window.
	mu      sync.Mutex
	rec     certificateRecord
	roots   idAllocator
	clients idAllocator
	checker DependencyChecker
	// pending holds the CCDIDs a fresh ClientCSR reserved while the port
	// makes its key, with their fabric: they count against the fabric's
	// capacity and are never handed out twice. A fabric's removal drops
	// its reservations; the CSR then drops its key.
	pending map[uint16]uint8
	// removingRoots and removingClients hold the certificates a removal
	// is deciding on (its dependency check runs without mu): the lookups
	// of [tcmdef.Host] do not see them, so no TLS endpoint can name one
	// in that window.
	removingRoots   map[certRef]int
	removingClients map[certRef]int
}

// certRef names a certificate of a fabric.
type certRef struct {
	fabric uint8
	id     uint16
}

// locked runs f with mu held; the deferred unlock frees the lock however
// f ends, a panic included (R2-P6-conc-1101).
func (c *CertificateManagement) locked(f func()) {
	c.mu.Lock()
	defer c.mu.Unlock()
	f()
}

// Compile-time assertions.
var (
	_ contract.ClusterServer                  = (*CertificateManagement)(nil)
	_ contract.ClusterDataVersion             = (*CertificateManagement)(nil)
	_ contract.ClusterAttributeLister         = (*CertificateManagement)(nil)
	_ contract.ClusterCommandLister           = (*CertificateManagement)(nil)
	_ contract.ClusterAttributeWritePrivilege = (*CertificateManagement)(nil)
	_ contract.ClusterCommandInvokePrivilege  = (*CertificateManagement)(nil)
	_ contract.AttributeChangeNotifier        = (*CertificateManagement)(nil)
	_ contract.FabricScopedReader             = (*CertificateManagement)(nil)
	_ spec.CommandRegistrar                   = (*CertificateManagement)(nil)
	_ tcmdef.Host                             = (*CertificateManagement)(nil)
	_ contract.FabricScopedState              = (*CertificateManagement)(nil)
)

// NewCertificateManagement builds a TlsCertificateManagement server on
// cfg.Port with the tables the store saved for cfg.Key. Mirrors chip's
// constructor and Startup (TLSCertificateManagementCluster.cpp:61-90).
func NewCertificateManagement(ctx context.Context, cfg CertificateManagementConfig) (*CertificateManagement, error) {
	if cfg.Port == nil {
		return nil, ErrNoCertificatePort
	}
	if cfg.MaxRootCertificates < 5 || cfg.MaxClientCertificates < 5 { // :70-71
		return nil, fmt.Errorf("%w: MaxRootCertificates %d, MaxClientCertificates %d", ErrCertificateConfig, cfg.MaxRootCertificates, cfg.MaxClientCertificates)
	}
	c := &CertificateManagement{
		port: cfg.Port, store: cfg.Store, key: settingCertificates + cfg.Key, cfg: cfg, checker: cfg.Checker,
		roots: idAllocator{maxID: MaxCertificateID}, clients: idAllocator{maxID: MaxCertificateID},
		pending: map[uint16]uint8{}, removingRoots: map[certRef]int{}, removingClients: map[certRef]int{},
	}
	if err := load(ctx, cfg.Store, c.key, &c.rec); err != nil {
		return nil, err
	}
	if err := c.rec.validate(c.key); err != nil {
		return nil, err
	}
	c.roots.next, c.clients.next = c.rec.NextRoot, c.rec.NextClient
	handlers := map[uint32]spec.CommandHandler{
		tcmdef.CmdProvisionRootCertificate:   c.provisionRoot,
		tcmdef.CmdFindRootCertificate:        c.findRoot,
		tcmdef.CmdLookupRootCertificate:      c.lookupRoot,
		tcmdef.CmdRemoveRootCertificate:      c.removeRoot,
		tcmdef.CmdClientCsr:                  c.clientCSR,
		tcmdef.CmdProvisionClientCertificate: c.provisionClient,
		tcmdef.CmdFindClientCertificate:      c.findClient,
		tcmdef.CmdLookupClientCertificate:    c.lookupClient,
		tcmdef.CmdRemoveClientCertificate:    c.removeClient,
	}
	srv, err := spec.NewServer(tcmdef.Definition, spec.Options{}, spec.ServerConfig{Handlers: handlers, DataVersion: cfg.DataVersion}) //nolint:staticcheck // the deprecated tracker is passed on for its window (decision 165)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrCertificateConfig, err)
	}
	c.srv, c.Instance, c.SelfHeld = srv, srv.Instance, spec.NewSelfHeld(srv)
	values := c.listValues()
	values[tcmdef.AttrMaxRootCertificates] = cfg.MaxRootCertificates
	values[tcmdef.AttrMaxClientCertificates] = cfg.MaxClientCertificates
	if err := srv.SetAttributes(values); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrCertificateConfig, err)
	}
	return c, nil
}

// SetDependencyChecker implements [tcmdef.Host].
func (c *CertificateManagement) SetDependencyChecker(checker DependencyChecker) {
	c.locked(func() { c.checker = checker })
}

// RootCertificate implements [tcmdef.Host]. A certificate whose removal
// is deciding is not found.
func (c *CertificateManagement) RootCertificate(fabricIndex uint8, caid uint16) (RootCertificate, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if i := c.rootIndex(fabricIndex, caid); i >= 0 && c.removingRoots[certRef{fabricIndex, caid}] == 0 {
		return rootStruct(c.rec.Roots[i]), true
	}
	return RootCertificate{}, false
}

// ClientCertificate implements [tcmdef.Host]. A certificate whose removal
// is deciding is not found.
func (c *CertificateManagement) ClientCertificate(fabricIndex uint8, ccdid uint16) (ClientCertificate, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if i := c.clientIndex(fabricIndex, ccdid); i >= 0 && c.removingClients[certRef{fabricIndex, ccdid}] == 0 {
		return clientStruct(c.rec.Clients[i]), true
	}
	return ClientCertificate{}, false
}

func (c *CertificateManagement) rootIndex(fabric uint8, caid uint16) int {
	return slices.IndexFunc(c.rec.Roots, func(e rootEntry) bool { return e.Fabric == fabric && e.CAID == caid })
}

func (c *CertificateManagement) clientIndex(fabric uint8, ccdid uint16) int {
	return slices.IndexFunc(c.rec.Clients, func(e clientEntry) bool { return e.Fabric == fabric && e.CCDID == ccdid })
}

func rootStruct(e rootEntry) RootCertificate {
	cert := bytes.Clone(e.Certificate)
	return RootCertificate{Caid: e.CAID, Certificate: &cert, FabricIndex: e.Fabric}
}

func clientStruct(e clientEntry) ClientCertificate {
	cert := spec.NullOf[[]byte]()
	if e.Certificate != nil {
		cert = spec.ValueOf(bytes.Clone(e.Certificate))
	}
	out := ClientCertificate{Ccdid: e.CCDID, ClientCertificate: &cert, FabricIndex: e.Fabric}
	// The intermediates are always present, an empty list for none — a
	// CSR-only entry's too: chip's example table encodes the field in
	// both branches (tls-certificate-management-instance.cpp:107-124).
	inter := make([][]byte, len(e.Intermediates))
	for i, b := range e.Intermediates {
		inter[i] = bytes.Clone(b)
	}
	out.IntermediateCertificates = &inter
	return out
}

// listValues is the two lists at the current tables; the caller holds mu
// or owns c alone.
func (c *CertificateManagement) listValues() map[uint32]any {
	roots := make(spec.List[RootCertificate], 0, len(c.rec.Roots))
	for _, e := range c.rec.Roots {
		roots = append(roots, rootStruct(e))
	}
	clients := make(spec.List[ClientCertificate], 0, len(c.rec.Clients))
	for _, e := range c.rec.Clients {
		clients = append(clients, clientStruct(e))
	}
	return map[uint32]any{
		tcmdef.AttrProvisionedRootCertificates:   roots,
		tcmdef.AttrProvisionedClientCertificates: clients,
	}
}

// commitLocked saves the tables and publishes both lists; a failed save restores
// prev and answers FAILURE (chip's table write failing, e.g.
// tls-certificate-management-instance.cpp:158). The caller holds mu.
func (c *CertificateManagement) commitLocked(ctx context.Context, prev certificateRecord) error {
	c.rec.NextRoot, c.rec.NextClient = c.roots.next, c.clients.next
	if err := save(ctx, c.store, c.key, c.rec); err != nil {
		c.rec = prev
		c.roots.next, c.clients.next = prev.NextRoot, prev.NextClient
		return spec.Errorf(im.StatusFailure, "TlsCertificateManagement: %v", err)
	}
	return nil
}

func (c *CertificateManagement) publish() {
	var values map[uint32]any
	c.locked(func() { values = c.listValues() })
	_ = c.srv.SetAttributesFrom(contract.OriginCommand, values)
}

func cloneRecord(r certificateRecord) certificateRecord {
	out := r
	out.Roots = slices.Clone(r.Roots)
	out.Clients = slices.Clone(r.Clients)
	return out
}

// accessingFabric is the invoking fabric; 0 without one (chip's
// kUndefinedFabricIndex).
func accessingFabric(ctx context.Context) uint8 {
	_, fabric, _ := im.LookupFabricFilter(ctx)
	return fabric
}

func requestOf[T any](fields any, clusterName, name string) (T, error) {
	req, ok := spec.RequestOf[T](fields)
	if !ok {
		return req, spec.Errorf(im.StatusInvalidCommand, "%s: %s payload %T", clusterName, name, fields)
	}
	return req, nil
}

func fingerprint(cert []byte) []byte {
	sum := sha256.Sum256(cert)
	return sum[:]
}

// provisionRoot carries out ProvisionRootCertificate. Mirrors chip
// HandleProvisionRootCertificate (TLSCertificateManagementCluster.cpp
// :230-283) and the example delegate's ProvisionRootCert
// (tls-certificate-management-instance.cpp:148-161): a certificate of at
// most 3000 bytes the fabric does not hold yet; a null CAID adds one
// within MaxRootCertificates, a CAID replaces the fabric's entry of it;
// the port judges its validity.
func (c *CertificateManagement) provisionRoot(ctx context.Context, fields any) (any, error) {
	req, err := requestOf[tcmdef.ProvisionRootCertificateRequest](fields, "TlsCertificateManagement", "ProvisionRootCertificate")
	if err != nil {
		return nil, err
	}
	if len(req.Certificate) > MaxCertificateBytes { // :236
		return nil, spec.Errorf(im.StatusConstraintError, "TlsCertificateManagement: certificate of %d bytes", len(req.Certificate))
	}
	fabric := accessingFabric(ctx)
	c.locked(func() { err = c.checkRootLocked(fabric, req) })
	if err != nil {
		return nil, err
	}
	if !c.port.CertificateValid(ctx, req.Certificate) { // :260-264
		return nil, spec.Errorf(im.StatusDynamicConstraintError, "TlsCertificateManagement: the root certificate is not valid now")
	}
	caid := req.Caid.Value
	c.locked(func() {
		// The checks again: a removal of the CAID, or a provision that
		// took the last slot or the same certificate, may have landed in
		// the port's window (R2-P6-conc-1101, -1109).
		if err = c.checkRootLocked(fabric, req); err != nil {
			return
		}
		prev := cloneRecord(c.rec)
		if req.Caid.Null {
			id, ok := c.roots.allocate(func(id uint16) bool {
				return slices.ContainsFunc(c.rec.Roots, func(e rootEntry) bool { return e.CAID == id })
			})
			if !ok { // CertificateTableImpl.cpp:414, instance :158
				err = spec.Errorf(im.StatusFailure, "TlsCertificateManagement: no free CAID")
				return
			}
			caid = id
			c.rec.Roots = append(c.rec.Roots, rootEntry{Fabric: fabric, CAID: caid, Certificate: bytes.Clone(req.Certificate)})
		} else {
			c.rec.Roots[c.rootIndex(fabric, caid)].Certificate = bytes.Clone(req.Certificate)
		}
		err = c.commitLocked(ctx, prev)
	})
	if err != nil {
		return nil, err
	}
	c.publish() // :279
	return tcmdef.ProvisionRootCertificateResponse{Caid: caid}, nil
}

// checkRootLocked is ProvisionRootCertificate's checks before the port
// (:240-257); the caller holds mu.
func (c *CertificateManagement) checkRootLocked(fabric uint8, req tcmdef.ProvisionRootCertificateRequest) error {
	exists := slices.ContainsFunc(c.rec.Roots, func(e rootEntry) bool { // :240-245, LookupRootCert by fingerprint
		return e.Fabric == fabric && bytes.Equal(fingerprint(e.Certificate), fingerprint(req.Certificate))
	})
	count := 0
	for _, e := range c.rec.Roots {
		if e.Fabric == fabric {
			count++
		}
	}
	found := !req.Caid.Null && c.rootIndex(fabric, req.Caid.Value) >= 0
	switch {
	case exists: // :245
		return spec.Errorf(im.StatusAlreadyExists, "TlsCertificateManagement: the root certificate is provisioned")
	case req.Caid.Null && count >= int(c.cfg.MaxRootCertificates): // :251
		return spec.Errorf(im.StatusResourceExhausted, "TlsCertificateManagement: %d root certificates", count)
	case !req.Caid.Null && req.Caid.Value > MaxCertificateID: // :256
		return spec.Errorf(im.StatusConstraintError, "TlsCertificateManagement: CAID %d", req.Caid.Value)
	case !req.Caid.Null && !found: // :257
		return spec.Errorf(im.StatusNotFound, "TlsCertificateManagement: CAID %d not found", req.Caid.Value)
	}
	return nil
}

// findRoot carries out FindRootCertificate. Mirrors chip
// HandleFindRootCertificate (:284-330): a null CAID answers the fabric's
// certificates, NOT_FOUND for none; a CAID the fabric's entry of it.
func (c *CertificateManagement) findRoot(ctx context.Context, fields any) (any, error) {
	req, err := requestOf[tcmdef.FindRootCertificateRequest](fields, "TlsCertificateManagement", "FindRootCertificate")
	if err != nil {
		return nil, err
	}
	if !req.Caid.Null && req.Caid.Value > MaxCertificateID { // :308
		return nil, spec.Errorf(im.StatusConstraintError, "TlsCertificateManagement: CAID %d", req.Caid.Value)
	}
	fabric := accessingFabric(ctx)
	var out []RootCertificate
	c.locked(func() {
		for _, e := range c.rec.Roots {
			if e.Fabric == fabric && (req.Caid.Null || e.CAID == req.Caid.Value) {
				out = append(out, rootStruct(e))
			}
		}
	})
	if len(out) == 0 { // :291-294, :324
		return nil, spec.Errorf(im.StatusNotFound, "TlsCertificateManagement: no root certificate")
	}
	return tcmdef.FindRootCertificateResponse{CertificateDetails: out}, nil
}

// lookupRoot carries out LookupRootCertificate. Mirrors chip
// HandleLookupRootCertificate (:331-356) and the example delegate's
// fingerprint, the SHA-256 of the certificate
// (tls-certificate-management-instance.cpp:139-146, :223-247).
func (c *CertificateManagement) lookupRoot(ctx context.Context, fields any) (any, error) {
	req, err := requestOf[tcmdef.LookupRootCertificateRequest](fields, "TlsCertificateManagement", "LookupRootCertificate")
	if err != nil {
		return nil, err
	}
	if len(req.Fingerprint) > MaxFingerprintBytes { // :337
		return nil, spec.Errorf(im.StatusConstraintError, "TlsCertificateManagement: fingerprint of %d bytes", len(req.Fingerprint))
	}
	fabric := accessingFabric(ctx)
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, e := range c.rec.Roots {
		if e.Fabric == fabric && bytes.Equal(fingerprint(e.Certificate), req.Fingerprint) {
			return tcmdef.LookupRootCertificateResponse{Caid: e.CAID}, nil
		}
	}
	return nil, spec.Errorf(im.StatusNotFound, "TlsCertificateManagement: no root certificate of the fingerprint") // :350
}

// removeRoot carries out RemoveRootCertificate. Mirrors chip
// HandleRemoveRootCertificate (:357-377) and the example delegate's
// RemoveRootCert (tls-certificate-management-instance.cpp:259-272): a
// certificate a TLS endpoint names is INVALID_IN_STATE, an unknown one
// NOT_FOUND.
func (c *CertificateManagement) removeRoot(ctx context.Context, fields any) (any, error) {
	req, err := requestOf[tcmdef.RemoveRootCertificateRequest](fields, "TlsCertificateManagement", "RemoveRootCertificate")
	if err != nil {
		return nil, err
	}
	if req.Caid > MaxCertificateID { // :363
		return nil, spec.Errorf(im.StatusConstraintError, "TlsCertificateManagement: CAID %d", req.Caid)
	}
	fabric := accessingFabric(ctx)
	ref := certRef{fabric, req.Caid}
	var checker DependencyChecker
	c.locked(func() {
		checker = c.checker
		c.removingRoots[ref]++
	})
	marked := true
	defer func() { // the checker panicked
		if marked {
			c.locked(func() { unmark(c.removingRoots, ref) })
		}
	}()
	inUse := checker != nil && checker.RootCertificateInUse(fabric, req.Caid) // :364-366
	c.locked(func() {
		unmark(c.removingRoots, ref)
		marked = false
		if inUse {
			err = spec.Errorf(im.StatusInvalidInState, "TlsCertificateManagement: CAID %d is in use", req.Caid)
			return
		}
		i := c.rootIndex(fabric, req.Caid)
		if i < 0 { // instance :263-267
			err = spec.Errorf(im.StatusNotFound, "TlsCertificateManagement: CAID %d not found", req.Caid)
			return
		}
		prev := cloneRecord(c.rec)
		c.rec.Roots = slices.Delete(c.rec.Roots, i, i+1)
		err = c.commitLocked(ctx, prev)
	})
	if err != nil {
		return nil, err
	}
	c.publish() // :372
	return nil, nil
}

// unmark ends one removal's mark of ref; the caller holds mu.
func unmark(marks map[certRef]int, ref certRef) {
	if marks[ref] <= 1 {
		delete(marks, ref)
		return
	}
	marks[ref]--
}

// clientCSR carries out ClientCSR. Mirrors chip HandleGenerateClientCsr
// (:378-412) and CertificateTableImpl::PrepareClientCertificate
// (CertificateTableImpl.cpp:461-513): a 32-byte nonce; a null CCDID makes
// a new key within MaxClientCertificates and a new entry without a
// certificate, a CCDID signs with the fabric's existing key of it (an
// unknown one is FAILURE, the table's GetTableEntry failing at :475).
func (c *CertificateManagement) clientCSR(ctx context.Context, fields any) (any, error) {
	req, err := requestOf[tcmdef.ClientCsrRequest](fields, "TlsCertificateManagement", "ClientCSR")
	if err != nil {
		return nil, err
	}
	if len(req.Nonce) != NonceBytes { // :383
		return nil, spec.Errorf(im.StatusConstraintError, "TlsCertificateManagement: nonce of %d bytes", len(req.Nonce))
	}
	fabric := accessingFabric(ctx)
	ccdid, fresh := req.Ccdid.Value, req.Ccdid.Null
	var prevNext uint16
	c.locked(func() {
		if !fresh {
			if c.clientIndex(fabric, ccdid) < 0 { // CertificateTableImpl.cpp:475, instance :296
				err = spec.Errorf(im.StatusFailure, "TlsCertificateManagement: CCDID %d not found", ccdid)
			}
			return
		}
		count := 0
		for _, e := range c.rec.Clients {
			if e.Fabric == fabric {
				count++
			}
		}
		for _, f := range c.pending {
			if f == fabric {
				count++
			}
		}
		if count >= int(c.cfg.MaxClientCertificates) { // :392
			err = spec.Errorf(im.StatusResourceExhausted, "TlsCertificateManagement: %d client certificates", count)
			return
		}
		prevNext = c.clients.next
		id, ok := c.clients.allocate(func(id uint16) bool {
			if _, held := c.pending[id]; held {
				return true
			}
			return slices.ContainsFunc(c.rec.Clients, func(e clientEntry) bool { return e.CCDID == id })
		})
		if !ok { // CertificateTableImpl.cpp:502, instance :296
			err = spec.Errorf(im.StatusFailure, "TlsCertificateManagement: no free CCDID")
			return
		}
		ccdid = id
		c.pending[id] = fabric
	})
	if err != nil {
		return nil, err
	}
	csr, sig, err := c.port.GenerateCSR(ctx, fabric, ccdid, fresh, req.Nonce)
	if err == nil && (len(csr) > MaxCertificateBytes || len(sig) > MaxSignatureBytes) { // :397-398 (VerifyOrDie)
		err = spec.Errorf(im.StatusFailure, "TlsCertificateManagement: CSR of %d bytes, signature of %d", len(csr), len(sig))
		if fresh {
			c.port.RemoveClientKey(ctx, fabric, ccdid)
		}
	} else if err != nil {
		err = portError("GenerateCSR", err)
	}
	if err != nil {
		if fresh {
			c.release(ccdid, prevNext)
		}
		return nil, err
	}
	if !fresh {
		// A RemoveClientCertificate of the CCDID in the port's window
		// dropped the key the CSR signed with.
		c.locked(func() {
			if c.clientIndex(fabric, ccdid) < 0 {
				err = spec.Errorf(im.StatusFailure, "TlsCertificateManagement: CCDID %d not found", ccdid)
			}
		})
		if err != nil {
			return nil, err
		}
		return tcmdef.ClientCsrResponse{Ccdid: ccdid, Csr: csr, NonceSignature: sig}, nil
	}
	c.locked(func() {
		if _, held := c.pending[ccdid]; !held { // the fabric left in the port's window
			err = spec.Errorf(im.StatusFailure, "TlsCertificateManagement: fabric %d removed", fabric)
			return
		}
		delete(c.pending, ccdid)
		prev := cloneRecord(c.rec)
		c.rec.Clients = append(c.rec.Clients, clientEntry{Fabric: fabric, CCDID: ccdid}) // :506-512
		err = c.commitLocked(ctx, prev)
	})
	if err != nil {
		// No entry names the key the port made (R2-P6-err-1110).
		c.port.RemoveClientKey(ctx, fabric, ccdid)
		return nil, err
	}
	c.publish()
	return tcmdef.ClientCsrResponse{Ccdid: ccdid, Csr: csr, NonceSignature: sig}, nil
}

// release gives back the CCDID a failed fresh ClientCSR reserved; the
// allocator steps back only when no CSR took an id after it, so an id
// another CSR in flight holds is never handed out again (R2-P6-conc-1109).
func (c *CertificateManagement) release(ccdid, prevNext uint16) {
	c.locked(func() {
		delete(c.pending, ccdid)
		if c.clients.next == ccdid+1 {
			c.clients.next = prevNext
		}
	})
}

// provisionClient carries out ProvisionClientCertificate. Mirrors chip
// HandleProvisionClientCertificate (:413-462) and the example delegate's
// ProvisionClientCert with CertificateTableImpl::UpdateClientCertificateEntry
// (tls-certificate-management-instance.cpp:303-322,
// CertificateTableImpl.cpp:515-534): the bounds, the validity of the
// certificate and each intermediate, a certificate the fabric does not
// hold yet, the fabric's entry of the CCDID, and the entry's key in the
// certificate (DYNAMIC_CONSTRAINT_ERROR otherwise).
func (c *CertificateManagement) provisionClient(ctx context.Context, fields any) (any, error) {
	req, err := requestOf[tcmdef.ProvisionClientCertificateRequest](fields, "TlsCertificateManagement", "ProvisionClientCertificate")
	if err != nil {
		return nil, err
	}
	switch {
	case req.Ccdid > MaxCertificateID: // :419
		return nil, spec.Errorf(im.StatusConstraintError, "TlsCertificateManagement: CCDID %d", req.Ccdid)
	case len(req.ClientCertificate) > MaxCertificateBytes: // :420
		return nil, spec.Errorf(im.StatusConstraintError, "TlsCertificateManagement: certificate of %d bytes", len(req.ClientCertificate))
	case !c.port.CertificateValid(ctx, req.ClientCertificate): // :422-426
		return nil, spec.Errorf(im.StatusDynamicConstraintError, "TlsCertificateManagement: the client certificate is not valid now")
	case len(req.IntermediateCertificates) > MaxIntermediateCertificates: // :431
		return nil, spec.Errorf(im.StatusConstraintError, "TlsCertificateManagement: %d intermediate certificates", len(req.IntermediateCertificates))
	}
	for _, inter := range req.IntermediateCertificates {
		if len(inter) > MaxCertificateBytes { // :437
			return nil, spec.Errorf(im.StatusConstraintError, "TlsCertificateManagement: intermediate certificate of %d bytes", len(inter))
		}
		if !c.port.CertificateValid(ctx, inter) { // :438
			return nil, spec.Errorf(im.StatusDynamicConstraintError, "TlsCertificateManagement: an intermediate certificate is not valid now")
		}
	}
	fabric := accessingFabric(ctx)
	c.locked(func() { err = c.checkClientLocked(fabric, req) })
	if err != nil {
		return nil, err
	}
	if !c.port.ClientKeyMatches(ctx, fabric, req.Ccdid, req.ClientCertificate) { // CertificateTableImpl.cpp:526-529, instance :316-318
		return nil, spec.Errorf(im.StatusDynamicConstraintError, "TlsCertificateManagement: the certificate does not carry CCDID %d's key", req.Ccdid)
	}
	c.locked(func() {
		// The checks again, for a command of the fabric in the port's
		// window (R2-P6-conc-1109).
		if err = c.checkClientLocked(fabric, req); err != nil {
			return
		}
		i := c.clientIndex(fabric, req.Ccdid)
		prev := cloneRecord(c.rec)
		inter := make([][]byte, len(req.IntermediateCertificates))
		for j, b := range req.IntermediateCertificates {
			inter[j] = bytes.Clone(b)
		}
		c.rec.Clients[i].Certificate = bytes.Clone(req.ClientCertificate)
		c.rec.Clients[i].Intermediates = inter
		err = c.commitLocked(ctx, prev)
	})
	if err != nil {
		return nil, err
	}
	c.publish() // :457
	return nil, nil
}

// checkClientLocked is ProvisionClientCertificate's table checks
// (:444-451); the caller holds mu.
func (c *CertificateManagement) checkClientLocked(fabric uint8, req tcmdef.ProvisionClientCertificateRequest) error {
	exists := slices.ContainsFunc(c.rec.Clients, func(e clientEntry) bool { // :444-450, LookupClientCert
		return e.Fabric == fabric && e.Certificate != nil && bytes.Equal(fingerprint(e.Certificate), fingerprint(req.ClientCertificate))
	})
	switch {
	case exists: // :450
		return spec.Errorf(im.StatusAlreadyExists, "TlsCertificateManagement: the client certificate is provisioned")
	case c.clientIndex(fabric, req.Ccdid) < 0: // :451
		return spec.Errorf(im.StatusNotFound, "TlsCertificateManagement: CCDID %d not found", req.Ccdid)
	}
	return nil
}

// findClient carries out FindClientCertificate. Mirrors chip
// HandleFindClientCertificate (:463-506).
func (c *CertificateManagement) findClient(ctx context.Context, fields any) (any, error) {
	req, err := requestOf[tcmdef.FindClientCertificateRequest](fields, "TlsCertificateManagement", "FindClientCertificate")
	if err != nil {
		return nil, err
	}
	if !req.Ccdid.Null && req.Ccdid.Value > MaxCertificateID { // :484
		return nil, spec.Errorf(im.StatusConstraintError, "TlsCertificateManagement: CCDID %d", req.Ccdid.Value)
	}
	fabric := accessingFabric(ctx)
	var out []ClientCertificate
	c.locked(func() {
		for _, e := range c.rec.Clients {
			if e.Fabric == fabric && (req.Ccdid.Null || e.CCDID == req.Ccdid.Value) {
				out = append(out, clientStruct(e))
			}
		}
	})
	if len(out) == 0 { // :472, :500
		return nil, spec.Errorf(im.StatusNotFound, "TlsCertificateManagement: no client certificate")
	}
	return tcmdef.FindClientCertificateResponse{CertificateDetails: out}, nil
}

// lookupClient carries out LookupClientCertificate. Mirrors chip
// HandleLookupClientCertificate (:507-533); an entry without a certificate
// has no fingerprint (tls-certificate-management-instance.cpp:386-414).
func (c *CertificateManagement) lookupClient(ctx context.Context, fields any) (any, error) {
	req, err := requestOf[tcmdef.LookupClientCertificateRequest](fields, "TlsCertificateManagement", "LookupClientCertificate")
	if err != nil {
		return nil, err
	}
	if len(req.Fingerprint) > MaxFingerprintBytes { // :513
		return nil, spec.Errorf(im.StatusConstraintError, "TlsCertificateManagement: fingerprint of %d bytes", len(req.Fingerprint))
	}
	fabric := accessingFabric(ctx)
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, e := range c.rec.Clients {
		if e.Fabric == fabric && e.Certificate != nil && bytes.Equal(fingerprint(e.Certificate), req.Fingerprint) {
			return tcmdef.LookupClientCertificateResponse{Ccdid: e.CCDID}, nil
		}
	}
	return nil, spec.Errorf(im.StatusNotFound, "TlsCertificateManagement: no client certificate of the fingerprint") // :527
}

// removeClient carries out RemoveClientCertificate. Mirrors chip
// HandleRemoveClientCertificate (:534-553) and the example delegate's
// RemoveClientCert (tls-certificate-management-instance.cpp:426-439); the
// port drops the key.
func (c *CertificateManagement) removeClient(ctx context.Context, fields any) (any, error) {
	req, err := requestOf[tcmdef.RemoveClientCertificateRequest](fields, "TlsCertificateManagement", "RemoveClientCertificate")
	if err != nil {
		return nil, err
	}
	if req.Ccdid > MaxCertificateID { // :540
		return nil, spec.Errorf(im.StatusConstraintError, "TlsCertificateManagement: CCDID %d", req.Ccdid)
	}
	fabric := accessingFabric(ctx)
	ref := certRef{fabric, req.Ccdid}
	var checker DependencyChecker
	c.locked(func() {
		checker = c.checker
		c.removingClients[ref]++
	})
	marked := true
	defer func() { // the checker panicked
		if marked {
			c.locked(func() { unmark(c.removingClients, ref) })
		}
	}()
	inUse := checker != nil && checker.ClientCertificateInUse(fabric, req.Ccdid) // :541-543
	c.locked(func() {
		unmark(c.removingClients, ref)
		marked = false
		if inUse {
			err = spec.Errorf(im.StatusInvalidInState, "TlsCertificateManagement: CCDID %d is in use", req.Ccdid)
			return
		}
		i := c.clientIndex(fabric, req.Ccdid)
		if i < 0 { // instance :430-434
			err = spec.Errorf(im.StatusNotFound, "TlsCertificateManagement: CCDID %d not found", req.Ccdid)
			return
		}
		prev := cloneRecord(c.rec)
		c.rec.Clients = slices.Delete(c.rec.Clients, i, i+1)
		err = c.commitLocked(ctx, prev)
	})
	if err != nil {
		return nil, err
	}
	c.port.RemoveClientKey(ctx, fabric, req.Ccdid)
	c.publish() // :549
	return nil, nil
}

// MatterFabricRemoved drops the removed fabric's certificates and their
// keys. Mirrors chip OnFabricRemoved (:571-575) and
// CertificateTableImpl::RemoveFabric (CertificateTableImpl.cpp:572-599).
func (c *CertificateManagement) MatterFabricRemoved(fabricIndex uint8) {
	c.FabricRemoved(context.Background(), fabricIndex)
}

// FabricRemoved is [CertificateManagement.MatterFabricRemoved] on ctx.
func (c *CertificateManagement) FabricRemoved(ctx context.Context, fabricIndex uint8) {
	_ = c.MatterDropFabric(ctx, fabricIndex)
}

// MatterHeldFabrics implements [contract.FabricScopedState]: the fabrics
// of the root and client certificates.
func (c *CertificateManagement) MatterHeldFabrics() []uint8 {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []uint8
	for _, e := range c.rec.Roots {
		out = appendFabric(out, e.Fabric)
	}
	for _, e := range c.rec.Clients {
		out = appendFabric(out, e.Fabric)
	}
	return out
}

// appendFabric appends f to fabrics unless it is there already.
func appendFabric(fabrics []uint8, f uint8) []uint8 {
	if slices.Contains(fabrics, f) {
		return fabrics
	}
	return append(fabrics, f)
}

// MatterDropFabric implements [contract.FabricScopedState]: the fabric's
// certificates and their keys leave ([CertificateManagement.MatterFabricRemoved]);
// the error is the store's.
func (c *CertificateManagement) MatterDropFabric(ctx context.Context, fabricIndex uint8) error {
	var dropped []uint16
	var err error
	changed := false
	c.locked(func() {
		// A fresh ClientCSR of the fabric in flight finds its reservation
		// gone and drops its key.
		for id, f := range c.pending {
			if f == fabricIndex {
				delete(c.pending, id)
			}
		}
		prev := cloneRecord(c.rec)
		for _, e := range c.rec.Clients {
			if e.Fabric == fabricIndex {
				dropped = append(dropped, e.CCDID)
			}
		}
		c.rec.Roots = slices.DeleteFunc(c.rec.Roots, func(e rootEntry) bool { return e.Fabric == fabricIndex })
		c.rec.Clients = slices.DeleteFunc(c.rec.Clients, func(e clientEntry) bool { return e.Fabric == fabricIndex })
		if len(prev.Roots) == len(c.rec.Roots) && len(dropped) == 0 {
			return
		}
		changed = true
		// A refused save keeps the tables as they were (commitLocked); the
		// next reconcile drops them again.
		err = c.commitLocked(ctx, prev)
	})
	if !changed || err != nil {
		return err
	}
	for _, id := range dropped {
		c.port.RemoveClientKey(ctx, fabricIndex, id)
	}
	c.publish()
	return nil
}

// MatterDataVersion implements [contract.ClusterDataVersion].
func (c *CertificateManagement) MatterDataVersion() uint32 { return c.srv.MatterDataVersion() }

// OnAttributeChange implements [contract.AttributeChangeNotifier].
func (c *CertificateManagement) OnAttributeChange(cb func(change contract.Change)) (unsubscribe func()) {
	return c.srv.OnAttributeChange(cb)
}

// MatterRead resolves an attribute for a local caller: the lists hold
// every fabric's entries.
func (c *CertificateManagement) MatterRead(attrID uint32) (any, bool) {
	return c.srv.MatterRead(attrID)
}

// MatterReadFiltered implements [contract.FabricScopedReader]: a peer's
// read of either list sees its own fabric's entries only, as chip's
// ReadAttribute encodes the accessing fabric's certificates
// (TLSCertificateManagementCluster.cpp:102-171).
func (c *CertificateManagement) MatterReadFiltered(ctx context.Context, attrID uint32) (any, bool) {
	_, fabric, remote := im.LookupFabricFilter(ctx)
	if !remote || (attrID != tcmdef.AttrProvisionedRootCertificates && attrID != tcmdef.AttrProvisionedClientCertificates) {
		return c.MatterRead(attrID) //nolint:contextcheck // MatterRead is the unfiltered cluster-interface read
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if attrID == tcmdef.AttrProvisionedRootCertificates {
		out := spec.List[RootCertificate]{}
		for _, e := range c.rec.Roots {
			if fabric != 0 && e.Fabric == fabric {
				out = append(out, rootStruct(e))
			}
		}
		return out, true
	}
	out := spec.List[ClientCertificate]{}
	for _, e := range c.rec.Clients {
		if fabric != 0 && e.Fabric == fabric {
			out = append(out, clientStruct(e))
		}
	}
	return out, true
}

// MatterWrite answers every write by the definition: the cluster has no
// writable attribute.
func (c *CertificateManagement) MatterWrite(ctx context.Context, attrID uint32, value any) error {
	return c.srv.MatterWrite(ctx, attrID, value)
}

// MatterWriteChanged implements [contract.ChangeReportingWriter]: the
// generated server's write and its verdict.
func (c *CertificateManagement) MatterWriteChanged(ctx context.Context, attrID uint32, value any) (bool, error) {
	return c.srv.MatterWriteChanged(ctx, attrID, value)
}

// MatterInvoke dispatches an accepted command; a host's handler runs in
// its place ([spec.SelfHeld.Handle]).
func (c *CertificateManagement) MatterInvoke(ctx context.Context, cmdID uint32, fields any) (any, error) {
	return spec.InvokeHeld(ctx, &c.SelfHeld, cmdID, fields)
}

// portError is a port's refusal: its status when it names one by type
// ([spec.CarriesStatus] — a host's contract.StatusError or an
// im.StatusCodeError), FAILURE otherwise.
func portError(what string, err error) error {
	if spec.CarriesStatus(err) {
		return err
	}
	return spec.Errorf(im.StatusFailure, "tls: %s refused: %v", what, err)
}

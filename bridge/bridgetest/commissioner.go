// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package bridgetest

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/binary"
	"errors"
	"fmt"
	"math/big"
	"net"
	"strconv"
	"sync"
	"testing"
	"time"

	gencomm "github.com/SukramJ/go-fabric/cluster/spec/generalcommissioning"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/mdns"
	"github.com/SukramJ/go-fabric/secure/channel"
	"github.com/SukramJ/go-fabric/secure/sigma"
	"github.com/SukramJ/go-fabric/tlv"
)

// Defaults of [Options].
const (
	// DefaultControllerNodeID is chip-tool's own node id, kTestControllerNodeId
	// (connectedhomeip examples/chip-tool/commands/common/CHIPCommand.h:82).
	DefaultControllerNodeID uint64 = 112233

	// DefaultAdminVendorID is the test vendor id chip-tool commissions
	// with, which AddNOC accepts as an AdminVendorID
	// (cluster/core/operational_credentials.go:2366-2384).
	DefaultAdminVendorID uint16 = 0xFFF1

	// DefaultFailSafeSeconds is chip's kDefaultFailsafeTimeout
	// (connectedhomeip src/controller/CommissioningDelegate.h:208).
	DefaultFailSafeSeconds uint16 = 60

	// defaultTimeout bounds a call whose context carries no deadline.
	defaultTimeout = 10 * time.Second
)

// Root-endpoint cluster and command ids of the commissioning steps.
// GeneralCommissioning's come from its generated definition; the
// OperationalCredentials command ids and request field tags are the ones
// the bridge decodes (bridge/fields_reader.go:41-62, :498-700) and the
// response tags the ones it encodes (bridge/reply.go:1267-1295).
const (
	opCredsCluster          uint32 = 0x003E
	cmdAttestationRequest   uint32 = 0x00
	cmdAttestationResponse  uint32 = 0x01
	cmdCertChainRequest     uint32 = 0x02
	cmdCertChainResponse    uint32 = 0x03
	cmdCSRRequest           uint32 = 0x04
	cmdCSRResponse          uint32 = 0x05
	cmdAddNOC               uint32 = 0x06
	cmdNOCResponse          uint32 = 0x08
	cmdRemoveFabric         uint32 = 0x0A
	cmdAddTrustedRootCert   uint32 = 0x0B
	certTypeDAC             uint8  = 1
	certTypePAI             uint8  = 2
	stageArmFailsafe        uint64 = 3 // CommissioningDelegate.h:45-50, the breadcrumb chip sends (CHIPDeviceController.cpp:1708, :3744)
	stageConfigRegulatory   uint64 = 4 // CommissioningDelegate.h:51
	attestationNonceSize           = 32
	csrNonceSize                   = 32
	nocStatusOK             uint8  = 0
	commissioningErrorOK    uint8  = 0
	regulatoryCountryCodeXX        = "XX" // chip's default country code (CHIPDeviceController.cpp:3920-3930)
)

// Options configures a [Commissioner].
type Options struct {
	// Passcode is the device's setup passcode. Required.
	Passcode uint32
	// Discriminator is the device's discriminator. It is used only with
	// Advertiser, to find the device's commissionable record.
	Discriminator uint16
	// FabricID is the fabric the commissioner creates. Required, non-zero.
	FabricID uint64
	// NodeID is the operational node id the commissioner assigns the
	// device — chip-tool's `pairing onnetwork <node-id>`. Required.
	NodeID uint64
	// ControllerNodeID is the commissioner's own node id on the fabric;
	// 0 is [DefaultControllerNodeID].
	ControllerNodeID uint64
	// CAT are the CASE Authenticated Tags of the commissioner's NOC.
	CAT []uint32
	// AdminSubject is AddNOC's CaseAdminSubject; 0 is ControllerNodeID.
	AdminSubject uint64
	// AdminVendorID is AddNOC's AdminVendorID; 0 is [DefaultAdminVendorID].
	AdminVendorID uint16
	// FailSafeSeconds is ArmFailSafe's ExpiryLengthSeconds; 0 is
	// [DefaultFailSafeSeconds].
	FailSafeSeconds uint16
	// Seed determines every key and certificate the commissioner mints and
	// every nonce it sends. The handshakes' own randomness (the SPAKE2+
	// and Sigma ephemerals, the unsecured session's ephemeral node id and
	// message counter) stays random.
	Seed uint64
	// Advertiser, when set, is the device's DNS-SD advertiser (an
	// mdns.Noop the test passed to the device): PASE then dials the port
	// of the commissionable record with Discriminator, and CASE the port
	// of the operational record of NodeID on the commissioner's fabric —
	// on the host of the address given to [Dial]. Without it both dial
	// that address.
	Advertiser mdns.Advertiser
}

// Commissioner is a Matter commissioner and controller for tests: it
// commissions a device over the wire — PASE, the GeneralCommissioning and
// OperationalCredentials commands, CASE — and then talks to it over CASE
// sessions on a fabric it owns. It proves through the wire what a
// commissioned bridge does with its ACL, its fabrics, its CASE sessions
// and its persisted subscriptions, which bridge.Bridge has no entry point
// for.
//
// This module has no commissioner role (README.md, scope); this one exists
// for tests only. Its steps are the subset of chip-tool's AutoCommissioner
// that a commissioner of an on-network (Ethernet) device runs — no network
// commissioning, time synchronisation or ICD registration — in chip-tool's
// order (connectedhomeip src/controller/AutoCommissioner.cpp:472-545 and
// :672-675, at the harness pin 6170af84). It is safe for concurrent use.
type Commissioner struct {
	opts   Options
	ca     *testCA
	conn   *net.UDPConn
	device *net.UDPAddr

	mu           sync.Mutex
	sessions     map[uint16]*Session
	waiters      map[exchangeKey]chan inbound
	responders   map[uint16]*sigma.Responder
	identities   map[uint64]*caseIdentity
	accepted     chan *Session
	nextSession  uint16
	nextExchange uint16
	counter      uint32
	nonces       uint64
	ephemeral    uint64
	fabricIndex  uint8

	wg sync.WaitGroup
}

// Dial returns a commissioner for the device listening on addr. It listens
// on the loopback address of addr's family and is closed when the test
// ends. Invalid options fail the test.
func Dial(tb testing.TB, addr string, opts Options) *Commissioner {
	tb.Helper()
	switch {
	case opts.Passcode == 0:
		tb.Fatal("bridgetest: Options.Passcode is required")
	case opts.FabricID == 0:
		tb.Fatal("bridgetest: Options.FabricID is required")
	case opts.NodeID == 0:
		tb.Fatal("bridgetest: Options.NodeID is required")
	}
	if opts.ControllerNodeID == 0 {
		opts.ControllerNodeID = DefaultControllerNodeID
	}
	if opts.AdminSubject == 0 {
		opts.AdminSubject = opts.ControllerNodeID
	}
	if opts.AdminVendorID == 0 {
		opts.AdminVendorID = DefaultAdminVendorID
	}
	if opts.FailSafeSeconds == 0 {
		opts.FailSafeSeconds = DefaultFailSafeSeconds
	}
	device, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		tb.Fatalf("bridgetest: resolve %s: %v", addr, err)
	}
	local := net.IPv6loopback
	if device.IP.To4() != nil {
		local = net.IPv4(127, 0, 0, 1)
	}
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: local})
	if err != nil {
		tb.Fatalf("bridgetest: listen: %v", err)
	}
	c := &Commissioner{
		opts:        opts,
		ca:          newTestCA(opts.Seed, opts.FabricID),
		conn:        conn,
		device:      device,
		sessions:    make(map[uint16]*Session),
		waiters:     make(map[exchangeKey]chan inbound),
		responders:  make(map[uint16]*sigma.Responder),
		identities:  make(map[uint64]*caseIdentity),
		accepted:    make(chan *Session, 8),
		nextSession: 0x0100,
	}
	// The unsecured session is keyed by the initiator's ephemeral node id
	// and deduplicated by its message counter (Matter §4.13.2.1): both are
	// random, as chip-tool's are, so two commissioners with one seed do
	// not collide on the device.
	var r [12]byte
	_, _ = rand.Read(r[:])
	c.ephemeral = binary.BigEndian.Uint64(r[:8]) | 1
	c.counter = binary.BigEndian.Uint32(r[8:]) >> 4
	c.wg.Add(1)
	go c.readLoop()
	tb.Cleanup(c.Close)
	return c
}

// Close stops the commissioner. It is idempotent.
func (c *Commissioner) Close() {
	_ = c.conn.Close()
	c.wg.Wait()
}

// Addr is the commissioner's own UDP address.
func (c *Commissioner) Addr() *net.UDPAddr {
	addr, _ := c.conn.LocalAddr().(*net.UDPAddr)
	return addr
}

// FabricIndex is the fabric index AddNOC reported; 0 before Commission.
func (c *Commissioner) FabricIndex() uint8 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.fabricIndex
}

// CompressedFabricID is the compressed id of the commissioner's fabric.
func (c *Commissioner) CompressedFabricID() [8]byte { return c.ca.compressedFabricID() }

// ResolveOperational implements bridge.OperationalResolver: the
// commissioner's own address for a controller of its fabric. A device
// under test attaches it (Bridge.AttachOperationalResolver) to reach the
// commissioner when it re-establishes a former subscription.
func (c *Commissioner) ResolveOperational(_ context.Context, compressedFabricID [8]byte, nodeID uint64) ([]*net.UDPAddr, error) {
	c.mu.Lock()
	_, known := c.identities[nodeID]
	c.mu.Unlock()
	if compressedFabricID != c.ca.compressedFabricID() || !known {
		return nil, fmt.Errorf("bridgetest: no controller %016X-%016X here", compressedFabricID, nodeID)
	}
	return []*net.UDPAddr{c.Addr()}, nil
}

// Commission commissions the device onto the commissioner's fabric and
// returns the CASE session CommissioningComplete went over, as chip-tool
// sends it ("CommissioningComplete command happens over the CASE
// connection", CHIPDeviceController.cpp:4574).
//
// The stages, in AutoCommissioner order (AutoCommissioner.cpp:472-545,
// :672-675): kSecurePairing (PASE), kArmFailsafe, kConfigRegulatory (the
// device's LocationCapability, country "XX"; for an IndoorOutdoor device
// Outdoor, chip's last fallback — its commissioner override and default
// location are not modelled; CHIPDeviceController.cpp:3884-3936),
// kSendPAICertificateRequest,
// kSendDACCertificateRequest, kSendAttestationRequest (the signature is
// checked against the DAC over the elements and the PASE attestation
// challenge; the chain to a PAA is not), kSendOpCertSigningRequest
// (signature and nonce checked), kGenerateNOCChain (no ICAC),
// kSendTrustedRootCert, kSendNOC, kFindOperationalForCommissioningComplete
// (CASE) and kSendComplete.
func (c *Commissioner) Commission(ctx context.Context) (*Session, error) {
	ctx, cancel := withDefaultTimeout(ctx)
	defer cancel()
	pase, err := c.PASE(ctx)
	if err != nil {
		return nil, err
	}
	defer pase.drop()
	st := &stages{ctx: ctx, s: pase}
	c.provision(st)
	if st.err != nil {
		return nil, st.err
	}
	if st.s, st.err = c.CASE(ctx); st.err != nil {
		return nil, st.err
	}
	st.commissioning("CommissioningComplete", gencomm.CmdCommissioningComplete, gencomm.CommissioningCompleteRequest{}.EncodeTLV, gencomm.CmdCommissioningCompleteResponse)
	if st.err != nil {
		return nil, st.err
	}
	return st.s, nil
}

// stages runs commissioning commands over one session and keeps the
// first error: a stage after a failed one does nothing, and the
// commissioner reports the failure that stopped it.
type stages struct {
	ctx context.Context
	s   *Session
	err error
}

// fail records err unless an earlier stage failed.
func (st *stages) fail(err error) {
	if st.err == nil {
		st.err = err
	}
}

// invoke runs one root-endpoint command and returns its response fields.
func (st *stages) invoke(cluster, cmd uint32, fields func(e *tlv.Encoder, tag tlv.Tag), want uint32) TLV {
	if st.err != nil {
		return TLV{}
	}
	resp, err := st.s.command(st.ctx, cluster, cmd, fields, want)
	st.fail(err)
	return resp
}

// commissioning runs one GeneralCommissioning command and checks the
// response's ErrorCode (field 0) and DebugText (field 1).
func (st *stages) commissioning(name string, cmd uint32, fields func(e *tlv.Encoder, tag tlv.Tag), want uint32) {
	resp := st.invoke(gencomm.ClusterID, cmd, fields, want)
	if code, _ := resp.Child(0); code.u8() != commissioningErrorOK {
		debug, _ := resp.Child(1)
		st.fail(fmt.Errorf("bridgetest: %s: ErrorCode %d %q", name, code.Uint, debug.String))
	}
}

// provision runs the stages over the PASE session up to kSendNOC.
func (c *Commissioner) provision(st *stages) {
	st.commissioning("ArmFailSafe", gencomm.CmdArmFailSafe, gencomm.ArmFailSafeRequest{
		ExpiryLengthSeconds: c.opts.FailSafeSeconds, Breadcrumb: stageArmFailsafe,
	}.EncodeTLV, gencomm.CmdArmFailSafeResponse)
	location := gencomm.RegulatoryLocationTypeOutdoor
	if st.err == nil {
		if report, err := st.s.Read(st.ctx, rootPath(gencomm.ClusterID, gencomm.AttrLocationCapability)); err != nil {
			st.fail(err)
		} else if v, ok := report.Value(0, gencomm.ClusterID, gencomm.AttrLocationCapability); ok && v.Uint != uint64(gencomm.RegulatoryLocationTypeIndoorOutdoor) {
			location = gencomm.RegulatoryLocationTypeEnum(v.u8())
		}
	}
	st.commissioning("SetRegulatoryConfig", gencomm.CmdSetRegulatoryConfig, gencomm.SetRegulatoryConfigRequest{
		NewRegulatoryConfig: location, CountryCode: regulatoryCountryCodeXX, Breadcrumb: stageConfigRegulatory,
	}.EncodeTLV, gencomm.CmdSetRegulatoryConfigResponse)
	dac := c.attest(st)
	devicePub := c.requestCSR(st, dac)
	if st.err != nil {
		return
	}
	noc := c.ca.noc(devicePub, c.opts.NodeID, nil)
	st.invoke(opCredsCluster, cmdAddTrustedRootCert, octets(c.ca.rootCert), 0)
	resp := st.invoke(opCredsCluster, cmdAddNOC, func(e *tlv.Encoder, tag tlv.Tag) {
		e.StartStruct(tag)
		e.PutOctets(tlv.ContextTag(0), noc)
		e.PutOctets(tlv.ContextTag(2), c.ca.ipk)
		e.PutUint(tlv.ContextTag(3), c.opts.AdminSubject)
		e.PutUint(tlv.ContextTag(4), uint64(c.opts.AdminVendorID))
		_ = e.EndContainer()
	}, cmdNOCResponse)
	if status, _ := resp.Child(0); status.u8() != nocStatusOK {
		debug, _ := resp.Child(2)
		st.fail(fmt.Errorf("bridgetest: AddNOC: NOCResponse status %d %q", status.Uint, debug.String))
		return
	}
	index, _ := resp.Child(1)
	c.mu.Lock()
	c.fabricIndex = index.u8()
	c.mu.Unlock()
	// The identity CASE will present: minted now, so the device's
	// re-establishment resolver knows the controller from here on.
	c.identity(c.opts.ControllerNodeID, c.opts.CAT)
}

// attest runs kSendPAICertificateRequest, kSendDACCertificateRequest and
// kSendAttestationRequest and returns the DAC.
func (c *Commissioner) attest(st *stages) *x509.Certificate {
	var chain [2][]byte
	for i, kind := range []uint8{certTypePAI, certTypeDAC} {
		cert, _ := st.invoke(opCredsCluster, cmdCertChainRequest, func(e *tlv.Encoder, tag tlv.Tag) {
			e.StartStruct(tag)
			e.PutUint(tlv.ContextTag(0), uint64(kind))
			_ = e.EndContainer()
		}, cmdCertChainResponse).Child(0)
		chain[i] = cert.Octets
	}
	if st.err != nil {
		return nil
	}
	_, paiErr := x509.ParseCertificate(chain[0])
	dac, dacErr := x509.ParseCertificate(chain[1])
	if err := errors.Join(paiErr, dacErr); err != nil {
		st.fail(fmt.Errorf("bridgetest: attestation chain: %w", err))
		return nil
	}
	resp := st.invoke(opCredsCluster, cmdAttestationRequest, octets(c.nonce(attestationNonceSize)), cmdAttestationResponse)
	st.verify("AttestationResponse", dac, resp)
	return dac
}

// verify checks a response whose fields are {0: elements, 1: signature}
// against the DAC: a 64-byte r||s signature over SHA-256(elements ||
// attestation challenge) — the payload the device signs for
// AttestationResponse and CSRResponse
// (cluster/core/operational_credentials.go:1347-1365).
func (st *stages) verify(name string, dac *x509.Certificate, resp TLV) {
	if st.err != nil {
		return
	}
	elements, _ := resp.Child(0)
	sig, _ := resp.Child(1)
	digest := sha256.Sum256(append(append([]byte(nil), elements.Octets...), st.s.challenge...))
	pub, ok := dac.PublicKey.(*ecdsa.PublicKey)
	if !ok || len(sig.Octets) != 64 ||
		!ecdsa.Verify(pub, digest[:], new(big.Int).SetBytes(sig.Octets[:32]), new(big.Int).SetBytes(sig.Octets[32:])) {
		st.fail(fmt.Errorf("bridgetest: %s: signature does not verify against the DAC", name))
	}
}

// requestCSR runs kSendOpCertSigningRequest and kValidateCSR and returns
// the public key the device's CSR carries.
func (c *Commissioner) requestCSR(st *stages, dac *x509.Certificate) []byte {
	nonce := c.nonce(csrNonceSize)
	resp := st.invoke(opCredsCluster, cmdCSRRequest, octets(nonce), cmdCSRResponse)
	st.verify("CSRResponse", dac, resp)
	if st.err != nil {
		return nil
	}
	// NOCSRElements: {1: CSR (PKCS#10 DER), 2: CSRNonce}
	// (cluster/core/operational_credentials.go:1493-1500).
	elements, _ := resp.Child(0)
	tree, err := DecodeTLV(elements.Octets)
	der, _ := tree.Child(1)
	echoed, _ := tree.Child(2)
	csr, csrErr := x509.ParseCertificateRequest(der.Octets)
	if err = errors.Join(err, csrErr); err == nil && !bytes.Equal(echoed.Octets, nonce) {
		err = errors.New("CSRNonce not echoed")
	}
	var pub *ecdsa.PublicKey
	if err == nil {
		err = csr.CheckSignature()
		pub, _ = csr.PublicKey.(*ecdsa.PublicKey)
	}
	if err != nil || pub == nil {
		st.fail(fmt.Errorf("bridgetest: CSRResponse: no valid ECDSA CSR: %w", err))
		return nil
	}
	return publicKeyBytes(pub)
}

// CASE opens a CASE session to the device as the commissioner's own
// controller identity.
func (c *Commissioner) CASE(ctx context.Context) (*Session, error) {
	return c.caseAs(ctx, c.opts.ControllerNodeID, c.opts.CAT)
}

// CASEAs opens a CASE session to the device as another controller of the
// commissioner's fabric: nodeID with cats, under a NOC the commissioner's
// root signs. The ACL decides what that controller may do.
func (c *Commissioner) CASEAs(ctx context.Context, nodeID uint64, cats ...uint32) (*Session, error) {
	return c.caseAs(ctx, nodeID, cats)
}

// Accept returns the next CASE session the device opened to the
// commissioner (see [Commissioner.ResolveOperational]).
func (c *Commissioner) Accept(ctx context.Context) (*Session, error) {
	ctx, cancel := withDefaultTimeout(ctx)
	defer cancel()
	select {
	case s := <-c.accepted:
		return s, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// RemoveFabric removes the commissioner's fabric over s and returns the
// NOCResponse status.
func (c *Commissioner) RemoveFabric(ctx context.Context, s *Session) (uint8, error) {
	resp, err := s.command(ctx, opCredsCluster, cmdRemoveFabric, func(e *tlv.Encoder, tag tlv.Tag) {
		e.StartStruct(tag)
		e.PutUint(tlv.ContextTag(0), uint64(c.FabricIndex()))
		_ = e.EndContainer()
	}, cmdNOCResponse)
	if err != nil {
		return 0, err
	}
	status, _ := resp.Child(0)
	return status.u8(), nil
}

// commissionableAddr is where PASE goes.
func (c *Commissioner) commissionableAddr() (*net.UDPAddr, error) {
	if c.opts.Advertiser == nil {
		return c.device, nil
	}
	want := strconv.FormatUint(uint64(c.opts.Discriminator), 10)
	services := c.opts.Advertiser.Active()
	for i := range services {
		svc := &services[i]
		if svc.ServiceType != mdns.ServiceTypeCommissionable {
			continue
		}
		for _, txt := range svc.TXT {
			if txt.Key == "D" && txt.Value == want {
				return &net.UDPAddr{IP: c.device.IP, Port: int(svc.Port)}, nil
			}
		}
	}
	return nil, fmt.Errorf("bridgetest: no commissionable record with discriminator %d", c.opts.Discriminator)
}

// operationalAddr is where CASE goes.
func (c *Commissioner) operationalAddr() (*net.UDPAddr, error) {
	if c.opts.Advertiser == nil {
		return c.device, nil
	}
	instance := mdns.BuildOperationalService(mdns.OperationalServiceConfig{
		CompressedFabricID: c.ca.compressedFabricID(), NodeID: c.opts.NodeID,
	}).InstanceName
	services := c.opts.Advertiser.Active()
	for i := range services {
		if svc := &services[i]; svc.ServiceType == mdns.ServiceTypeOperational && svc.InstanceName == instance {
			return &net.UDPAddr{IP: c.device.IP, Port: int(svc.Port)}, nil
		}
	}
	return nil, fmt.Errorf("bridgetest: no operational record %s", instance)
}

// allocSession returns a fresh local session id.
func (c *Commissioner) allocSession() uint16 {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.nextSession++
	return c.nextSession
}

// nonce returns size bytes determined by the seed and a call counter.
func (c *Commissioner) nonce(size int) []byte {
	c.mu.Lock()
	c.nonces++
	n := c.nonces
	c.mu.Unlock()
	return deriveBytes(c.opts.Seed, "nonce", n, size)
}

// newSession registers an established session.
func (c *Commissioner) newSession(local, peer uint16, addr *net.UDPAddr, cfg channel.Config) *Session {
	cfg.PeerSessionID = peer
	ch, err := channel.New(cfg)
	mustNot(err)
	s := &Session{c: c, ch: ch, localID: local, peer: addr, reports: make(chan *Report, 64), partial: make(map[uint16]*Report)}
	c.mu.Lock()
	c.sessions[local] = s
	c.mu.Unlock()
	return s
}

// session returns the session with local id id.
func (c *Commissioner) session(id uint16) *Session {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.sessions[id]
}

// octets encodes a request whose only field is the octet string b at tag 0.
func octets(b []byte) func(e *tlv.Encoder, tag tlv.Tag) {
	return func(e *tlv.Encoder, tag tlv.Tag) {
		e.StartStruct(tag)
		e.PutOctets(tlv.ContextTag(0), b)
		_ = e.EndContainer()
	}
}

// rootPath is an attribute path on endpoint 0.
func rootPath(cluster, attribute uint32) im.ConcreteAttributePath {
	return im.ConcreteAttributePath{Endpoint: 0, Cluster: cluster, Attribute: attribute, HasEndpoint: true, HasCluster: true, HasAttribute: true}
}

// withDefaultTimeout bounds ctx by defaultTimeout when it has no deadline.
func withDefaultTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	if _, ok := ctx.Deadline(); ok {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, defaultTimeout)
}

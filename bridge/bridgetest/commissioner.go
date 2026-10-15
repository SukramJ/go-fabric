// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridgetest

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/controller/attestation"
	"github.com/SukramJ/go-fabric/controller/ca"
	"github.com/SukramJ/go-fabric/controller/commissioner"
	"github.com/SukramJ/go-fabric/controller/discovery"
	"github.com/SukramJ/go-fabric/controller/exchange"
	"github.com/SukramJ/go-fabric/controller/handshake"
	"github.com/SukramJ/go-fabric/controller/interaction"
	"github.com/SukramJ/go-fabric/controller/peer"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/mdns"
	"github.com/SukramJ/go-fabric/secure/sigma"
	"github.com/SukramJ/go-fabric/tlv"
	"github.com/SukramJ/go-fabric/transport/udp"
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

	// defaultTimeout bounds an Accept or a NextReport whose context
	// carries no deadline.
	defaultTimeout = 10 * time.Second
)

// OperationalCredentials RemoveFabric and its NOCResponse — the ids of
// the cluster's schema (parity/schema.json, ADR 0013 generates no
// definition for it).
const (
	opCredsCluster  uint32 = 0x003E
	cmdNOCResponse  uint32 = 0x08
	cmdRemoveFabric uint32 = 0x0A
)

// certificateInstant is the issuing instant of every certificate the
// commissioner's CA mints (controller/ca stamps NotBefore one year before
// it and NotAfter ten years after): a fixed instant keeps the certificates
// a function of the CA's keys alone.
var certificateInstant = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

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
	// ControllerNodeID is the commissioner's own node id on the fabric —
	// the fabric's root node id, AddNOC's CaseAdminSubject; 0 is
	// [DefaultControllerNodeID].
	ControllerNodeID uint64
	// CAT are the CASE Authenticated Tags of the commissioner's NOC.
	CAT []uint32
	// AdminSubject has no effect: AddNOC's CaseAdminSubject is
	// ControllerNodeID, the fabric's root node id, as matter.js's
	// commissioning flow sends it (controller/commissioner.FabricConfig
	// RootNodeID). It keeps its default, ControllerNodeID.
	AdminSubject uint64
	// AdminVendorID is AddNOC's AdminVendorID; 0 is [DefaultAdminVendorID].
	AdminVendorID uint16
	// FailSafeSeconds has no effect: the commissioning flow arms the
	// failsafe with matter.js's budget (controller/commissioner
	// DefaultFailSafeTime). It keeps its default, [DefaultFailSafeSeconds].
	FailSafeSeconds uint16
	// Seed seeds the randomness the commissioner draws: the IPK, the
	// commissioning flow's nonces, the keys of the controller identities
	// [Commissioner.CASEAs] mints and the MRP jitter. The handshakes' own
	// randomness (the SPAKE2+ and Sigma ephemerals, the unsecured
	// session's ephemeral node id and message counter) stays random, and
	// so do the root's and the fabric's own key and the certificate
	// signatures: controller/ca and controller/commissioner draw them
	// through crypto/ecdsa, which ignores a caller's reader since Go 1.26.
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
// It is the module's own controller (controller/, ADR 0020) wired for a
// test: one exchange manager on a loopback socket, a certificate
// authority (controller/ca), a fabric, a peer set, a subscription
// registry, the commissioner (controller/commissioner, matter.js's
// commissioning flow) and a CASE responder; the device's operational
// record is fed to the controller's DNS-SD cache from [Options.Advertiser]
// or the dialled address. It is safe for concurrent use.
type Commissioner struct {
	tb     testing.TB
	opts   Options
	device *net.UDPAddr
	rand   *seededStream

	listener *udp.Listener
	mgr      *exchange.Manager
	stop     context.CancelFunc
	served   chan struct{}
	conn     *queryConn
	names    *discovery.Names
	ca       *ca.CA
	fabric   *commissioner.Fabric
	set      *peer.Set
	reg      *interaction.Subscriptions
	com      *commissioner.Commissioner
	server   *handshake.CASEServer
	cfid     [8]byte

	mu          sync.Mutex
	identities  map[uint64]*sigma.Identity
	minted      []uint64 // identity node ids in minting order
	sessions    map[*exchange.Session]*Session
	accepted    chan *Session
	fabricIndex uint8

	closeOnce sync.Once
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
	c := &Commissioner{
		tb:         tb,
		opts:       opts,
		device:     device,
		rand:       newSeededStream(opts.Seed, "rand"),
		identities: make(map[uint64]*sigma.Identity),
		sessions:   make(map[*exchange.Session]*Session),
		accepted:   make(chan *Session, 8),
	}
	if err := c.start(); err != nil {
		c.Close()
		tb.Fatalf("bridgetest: %v", err)
	}
	tb.Cleanup(c.Close)
	return c
}

// start builds the controller's parts, as controller/interaction's live
// test wires them.
func (c *Commissioner) start() error {
	discard := slog.New(slog.DiscardHandler)
	listen, preferIPv4 := "[::1]:0", false
	if c.device.IP.To4() != nil {
		listen, preferIPv4 = "127.0.0.1:0", true
	}
	l, err := udp.New(udp.Config{LocalAddr: listen, PreferIPv4: preferIPv4})
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	c.listener = l
	jitter := newSeededStream(c.opts.Seed, "jitter")
	if c.mgr, err = exchange.NewManager(l, exchange.Config{Logger: discard, Rand: jitter.Float64}); err != nil {
		return fmt.Errorf("exchange manager: %w", err)
	}
	ctx, stop := context.WithCancel(context.Background())
	c.stop, c.served = stop, make(chan struct{})
	go func() {
		defer close(c.served)
		_ = l.Serve(ctx, c.mgr.HandleDatagram)
	}()
	c.conn = &queryConn{out: make(chan mdns.QueryResponse, 64), onQuery: c.feedOperational}
	c.names = discovery.NewNames(c.conn, discard)
	if c.ca, err = ca.New(ca.Config{Rand: c.rand, Now: func() time.Time { return certificateInstant }}); err != nil {
		return fmt.Errorf("certificate authority: %w", err)
	}
	ipk := make([]byte, 16)
	_, _ = c.rand.Read(ipk)
	c.fabric, err = commissioner.NewFabric(c.ca, commissioner.FabricConfig{
		FabricID: c.opts.FabricID, RootNodeID: c.opts.ControllerNodeID, VendorID: c.opts.AdminVendorID,
		IPK: ipk, CATs: c.opts.CAT, Rand: c.rand,
	})
	if err != nil {
		return fmt.Errorf("fabric: %w", err)
	}
	c.cfid = c.fabric.Identity.CompressedFabricID
	c.identities[c.opts.ControllerNodeID] = c.fabric.Identity
	c.minted = append(c.minted, c.opts.ControllerNodeID)
	if c.set, err = peer.NewSet(peer.Context{
		Exchanges: c.mgr, Names: c.names, Fabrics: c.fabric.Lookup,
		Resumption: &peer.MemoryResumptionStore{}, Logger: discard,
	}); err != nil {
		return fmt.Errorf("peer set: %w", err)
	}
	c.reg = interaction.NewSubscriptions(interaction.SubscriptionsConfig{Logger: discard})
	// No Fabrics: the commissioner's own CASE responder would adopt the
	// sessions into the peer set; the one below hands them to Accept.
	if c.com, err = commissioner.New(commissioner.Context{
		Exchanges: c.mgr, Peers: c.set, Attestation: attestation.TestStore(),
		Subscriptions: c.reg, Rand: c.rand, Logger: discard,
	}); err != nil {
		return fmt.Errorf("commissioner: %w", err)
	}
	// No resumption store: a resuming Sigma1 gets the full handshake.
	if c.server, err = handshake.NewCASEServer(c.mgr, handshake.CASEServerConfig{
		Fabrics: identityFabrics{c}, OnSession: c.onSession, Logger: discard,
	}); err != nil {
		return fmt.Errorf("CASE responder: %w", err)
	}
	c.server.Register()
	return nil
}

// Close stops the commissioner. It is idempotent.
func (c *Commissioner) Close() {
	c.closeOnce.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), closeTimeout)
		defer cancel()
		if c.reg != nil {
			_ = c.reg.Close(ctx)
		}
		if c.server != nil {
			c.server.Close()
		}
		if c.com != nil {
			c.com.Close()
		}
		if c.set != nil {
			c.set.Close(ctx)
		}
		if c.mgr != nil {
			c.mgr.Close()
		}
		if c.stop != nil {
			c.stop()
			<-c.served
		}
		if c.listener != nil {
			_ = c.listener.Close()
		}
		if c.names != nil {
			_ = c.names.Close()
		}
	})
}

// Addr is the commissioner's own UDP address.
func (c *Commissioner) Addr() *net.UDPAddr { return c.listener.LocalAddr() }

// FabricIndex is the fabric index AddNOC reported; 0 before Commission.
func (c *Commissioner) FabricIndex() uint8 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.fabricIndex
}

// CompressedFabricID is the compressed id of the commissioner's fabric.
func (c *Commissioner) CompressedFabricID() [8]byte { return c.cfid }

// RootPublicKey is the 65-byte uncompressed public key of the
// commissioner's fabric root.
func (c *Commissioner) RootPublicKey() []byte { return c.ca.RootPublicKey() }

// ResolveOperational implements bridge.OperationalResolver: the
// commissioner's own address for a controller of its fabric. A device
// under test attaches it (Bridge.AttachOperationalResolver) to reach the
// commissioner when it re-establishes a former subscription.
func (c *Commissioner) ResolveOperational(_ context.Context, compressedFabricID [8]byte, nodeID uint64) ([]*net.UDPAddr, error) {
	c.mu.Lock()
	_, known := c.identities[nodeID]
	c.mu.Unlock()
	if compressedFabricID != c.cfid || !known {
		return nil, fmt.Errorf("bridgetest: no controller %016X-%016X here", compressedFabricID, nodeID)
	}
	return []*net.UDPAddr{c.Addr()}, nil
}

// Commission commissions the device onto the commissioner's fabric and
// returns the CASE session CommissioningComplete went over.
//
// The flow is controller/commissioner's, matter.js's
// ControllerCommissioningFlow for an on-network device: PASE, ArmFailSafe,
// the regulatory configuration (the device's LocationCapability, country
// "XX"), device attestation against the CSA test trust store (the zero
// policy: a finding is logged, not fatal), the CSR, AddTrustedRootCertificate,
// AddNOC, then CASE to the operational record and CommissioningComplete
// over it. Its own budgets bound it; ctx may bound it further. A failed
// handshake wraps [ErrHandshake].
func (c *Commissioner) Commission(ctx context.Context) (*Session, error) {
	addr, err := c.commissionableAddr()
	if err != nil {
		return nil, err
	}
	c.feedOperational()
	res, err := c.com.Commission(ctx, commissioner.Options{
		Fabric: c.fabric, Passcode: c.opts.Passcode, NodeID: c.opts.NodeID, Addresses: []*net.UDPAddr{addr},
	})
	if err != nil {
		if handshakeFailure(err) {
			return nil, fmt.Errorf("%w: %w", ErrHandshake, err)
		}
		return nil, err
	}
	c.mu.Lock()
	c.fabricIndex = res.FabricIndexOnPeer
	c.mu.Unlock()
	p := c.set.Get(res.Address)
	if p == nil || p.NewestSession() == nil {
		return nil, errors.New("bridgetest: the commissioned node has no CASE session")
	}
	return c.wrap(p.NewestSession()), nil
}

// handshakeFailure reports whether err is a failed PASE or CASE handshake:
// a step the device answered wrongly, a wrong passcode, or the device's
// error StatusReport.
func handshakeFailure(err error) bool {
	var sr *exchange.StatusReportError
	return errors.Is(err, handshake.ErrUnexpectedData) || errors.Is(err, handshake.ErrPasscodeMismatch) || errors.As(err, &sr)
}

// PASE establishes a PASE session with the passcode of [Options] over the
// device's commissioning window — controller/commissioner's PASE
// connection (matter.js ControllerCommissioner.establishPase). A failure
// wraps [ErrHandshake].
func (c *Commissioner) PASE(ctx context.Context) (*Session, error) {
	addr, err := c.commissionableAddr()
	if err != nil {
		return nil, err
	}
	s, _, err := c.com.EstablishPASE(ctx, commissioner.PASEOptions{Addresses: []*net.UDPAddr{addr}, Passcode: c.opts.Passcode})
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrHandshake, err)
	}
	return c.wrap(s), nil
}

// CASE opens a CASE session to the device as the commissioner's own
// controller identity.
func (c *Commissioner) CASE(ctx context.Context) (*Session, error) {
	return c.CASEAs(ctx, c.opts.ControllerNodeID, c.opts.CAT...)
}

// CASEAs opens a CASE session to the device as another controller of the
// commissioner's fabric: nodeID with cats, under a NOC the commissioner's
// root signs. The ACL decides what that controller may do. Every call is
// a new session without resumption; it does not join the peer set.
func (c *Commissioner) CASEAs(ctx context.Context, nodeID uint64, cats ...uint32) (*Session, error) {
	addr, err := c.operationalAddr()
	if err != nil {
		return nil, err
	}
	c.feedOperational()
	id := c.ControllerIdentity(nodeID, cats...)
	s, _, err := handshake.CASE(ctx, c.mgr, addr, handshake.Fabric{
		Identity: &id, RootPublicKey: c.ca.RootPublicKey(), Verifier: c.ca.Verifier(),
	}, c.opts.NodeID, handshake.CASEOptions{})
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrHandshake, err)
	}
	return c.wrap(s), nil
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
	res, err := s.Invoke(ctx, im.ConcreteCommandPath{Endpoint: 0, Cluster: opCredsCluster, Command: cmdRemoveFabric}, func(e *tlv.Encoder, tag tlv.Tag) {
		e.StartStruct(tag)
		e.PutUint(tlv.ContextTag(0), uint64(c.FabricIndex()))
		_ = e.EndContainer()
	})
	if err != nil {
		return 0, err
	}
	if res.Command != cmdNOCResponse || !res.Status.IsSuccess() {
		return 0, fmt.Errorf("bridgetest: command 0x%04X:0x%02X answered %d with status %s", opCredsCluster, cmdRemoveFabric, res.Command, res.Status)
	}
	status, _ := res.Fields.Child(0)
	return status.u8(), nil
}

// ControllerIdentity is the operational identity of controller nodeID on
// the commissioner's fabric — its key and NOC with cats, minted on first
// use as [Commissioner.CASEAs] mints them — for a CASE initiator outside
// this package (controller/handshake's tests). The IPK is the operational
// one, the fabric index 1. ControllerNodeID's is the fabric's own identity,
// the NOC the commissioning flow presents.
func (c *Commissioner) ControllerIdentity(nodeID uint64, cats ...uint32) sigma.Identity {
	c.mu.Lock()
	defer c.mu.Unlock()
	if id, ok := c.identities[nodeID]; ok {
		return *id
	}
	key := c.rand.key()
	pub, err := key.PublicKey.Bytes()
	mustNot(err)
	noc, err := c.ca.GenerateNOC(pub, c.opts.FabricID, nodeID, cats)
	mustNot(err)
	own := c.fabric.Identity
	id := &sigma.Identity{
		NOC: noc, ICAC: c.ca.ICACCert(), PrivateKey: key,
		NodeID: nodeID, FabricID: c.opts.FabricID, CompressedFabricID: c.cfid,
		IPK: own.IPK, FabricIndex: own.FabricIndex,
	}
	c.identities[nodeID] = id
	c.minted = append(c.minted, nodeID)
	return *id
}

// fabrics is every identity minted so far as a CASE responder's fabric
// list, in minting order.
func (c *Commissioner) fabrics() handshake.FabricList {
	c.mu.Lock()
	defer c.mu.Unlock()
	list := make(handshake.FabricList, 0, len(c.minted))
	for _, nodeID := range c.minted {
		list = append(list, handshake.Fabric{Identity: c.identities[nodeID], RootPublicKey: c.ca.RootPublicKey(), Verifier: c.ca.Verifier()})
	}
	return list
}

// ResolveSigma1Destination implements [sigma.IdentityResolver] for a
// responder: the controller identity the device's Sigma1 addresses.
func (c *Commissioner) ResolveSigma1Destination(destinationID [32]byte, initiatorRandom [sigma.RandomSize]byte) (*sigma.Identity, sigma.PeerVerifier, bool) {
	f, ok := c.fabrics().FabricForDestination(destinationID, initiatorRandom)
	if !ok {
		return nil, nil, false
	}
	return f.Identity, f.Verifier, true
}

// identityFabrics is the CASE responder's fabric resolver: every
// controller identity the commissioner minted, so a device reaches any
// controller [Commissioner.ResolveOperational] answers for.
type identityFabrics struct{ c *Commissioner }

func (r identityFabrics) FabricForDestination(destinationID [32]byte, initiatorRandom [sigma.RandomSize]byte) (handshake.Fabric, bool) {
	return r.c.fabrics().FabricForDestination(destinationID, initiatorRandom)
}

func (r identityFabrics) FabricByIndex(fabricIndex uint8) (handshake.Fabric, bool) {
	return r.c.fabric.Lookup(handshake.PeerAddress{FabricIndex: fabricIndex})
}

// onSession takes a session the device opened to the commissioner.
func (c *Commissioner) onSession(s *exchange.Session, _ uint8) {
	w := c.wrap(s)
	select {
	case c.accepted <- w:
	default:
	}
}

// wrap returns the Session of s, registering it on first use so the
// reports of its subscriptions find its queue.
func (c *Commissioner) wrap(s *exchange.Session) *Session {
	c.mu.Lock()
	defer c.mu.Unlock()
	if w, ok := c.sessions[s]; ok {
		return w
	}
	w := &Session{
		c: c, s: s, reports: make(chan *Report, 64),
		ix: interaction.Bind(interaction.SessionProvider(c.mgr, s), interaction.Config{Subscriptions: c.reg, Logger: slog.New(slog.DiscardHandler)}),
	}
	c.sessions[s] = w
	return w
}

// forget drops a closed session's registration.
func (c *Commissioner) forget(w *Session) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.sessions[w.s] == w {
		delete(c.sessions, w.s)
	}
}

// route queues report on the Session of the session it arrived over. A
// session the device opened may deliver its first report before the CASE
// responder handed the session over (it does so after its Success
// StatusReport), so an unknown session is registered here and Accept
// returns the same Session.
func (c *Commissioner) route(s *exchange.Session, report *Report) {
	if s == nil {
		c.tb.Logf("bridgetest: a report of subscription %d without a session dropped", report.SubscriptionID)
		return
	}
	select {
	case c.wrap(s).reports <- report:
	default:
	}
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
		CompressedFabricID: c.cfid, NodeID: c.opts.NodeID,
	}).InstanceName
	services := c.opts.Advertiser.Active()
	for i := range services {
		if svc := &services[i]; svc.ServiceType == mdns.ServiceTypeOperational && svc.InstanceName == instance {
			return &net.UDPAddr{IP: c.device.IP, Port: int(svc.Port)}, nil
		}
	}
	return nil, fmt.Errorf("bridgetest: no operational record %s", instance)
}

// withDefaultTimeout bounds ctx by defaultTimeout when it has no deadline.
func withDefaultTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	if _, ok := ctx.Deadline(); ok {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, defaultTimeout)
}

// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package commissioner

import (
	"cmp"
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/netip"
	"slices"
	"time"

	"github.com/SukramJ/go-fabric/client/discovery"
	"github.com/SukramJ/go-fabric/client/exchange"
	"github.com/SukramJ/go-fabric/client/handshake"
	"github.com/SukramJ/go-fabric/client/interaction"
	"github.com/SukramJ/go-fabric/client/peer"
	gencomm "github.com/SukramJ/go-fabric/cluster/spec/generalcommissioning"
	"github.com/SukramJ/go-fabric/controller/attestation"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/secure/sessionparams"
	"github.com/SukramJ/go-fabric/transport/mrp"
)

// Defaults of the commissioner. Mirrors matter.js
// packages/protocol/src/peer/ControllerCommissioner.ts:123
// (DEFAULT_CASE_CONNECTION_TIMEOUT = 255 s), :264 (the PASE budget,
// 30 s), :603-604 (regulatory location Outdoor, country "XX") and :706
// (a NoSharedTrustRoots retry after 15 s) at 0ceb202d.
const (
	DefaultCASEConnectionTimeout = 255 * time.Second
	DefaultPASETimeout           = 30 * time.Second
	DefaultRegulatoryLocation    = gencomm.RegulatoryLocationTypeOutdoor
	DefaultRegulatoryCountryCode = "XX"
	noSharedTrustRootsRetry      = 15 * time.Second
)

// Context is what a [Commissioner] works with — matter.js
// ControllerCommissionerContext (ControllerCommissioner.ts:218-225).
type Context struct {
	// Exchanges is the controller's exchange manager. Required.
	Exchanges *exchange.Manager
	// Peers is the peer set the commissioned node joins. Required.
	Peers *peer.Set
	// Attestation is the trust store of the attestation check; nil checks
	// without one (matter.js without a DclCertificateService).
	Attestation attestation.PAAStore
	// Logger receives the commissioning log; nil discards it.
	Logger *slog.Logger
	// Clock paces the PASE connection and the failsafe; nil is the system
	// clock.
	Clock exchange.Clock
	// Rand is the randomness of the nonces and the node ids; nil is
	// crypto/rand.
	Rand io.Reader
	// Subscriptions is the controller's subscription registry: when set,
	// New registers its handler of the reports peers push on Exchanges
	// (matter.js Peers.#configureInteractionServer,
	// packages/node/src/node/client/Peers.ts:546-563) and [Commissioner.Interaction]
	// binds it.
	Subscriptions *interaction.Subscriptions
	// Fabrics are the fabrics a peer may open CASE to us on: when set, New
	// registers a CASE responder answering for them (matter.js's
	// SecureChannelProtocol creates the CaseServer,
	// securechannel/SecureChannelProtocol.ts:96-100), and every session it
	// establishes joins Peers (Set.Adopt) — the session a restarted node
	// re-establishes its subscriptions on.
	Fabrics []*Fabric
	// Resumption holds the CASE responder's resumption records, usually the
	// peer set's store; nil keeps none.
	Resumption handshake.ResumptionStore
	// Scanners are the scanners that may still hold a commissioned
	// device's advertisement — matter.js's ScannerSet
	// (ControllerCommissioner.ts:321-341 at 862ad75f); nil is none.
	Scanners []CommissionedScanner
}

// CommissionedScanner is a scanner that forgets a commissioned device:
// its advertisement described a commissioning window that is now closed —
// matter.js Scanner.forgetCommissionedDevice (common/Scanner.ts), which
// its BLE scanner implements (controller/ble.Scanner.ForgetCommissioned)
// and its mDNS scanner does not.
type CommissionedScanner interface {
	ForgetCommissioned(peripheralAddresses []string, identity *discovery.CommissionedIdentity)
}

// Channel is a commissioning channel other than the controller's UDP
// socket: a BLE connection's BTP session (controller/ble.Channel) —
// matter.js's BLE paseChannel (ControllerCommissioner.ts:442-449). PASE
// runs on its exchange manager, whose carrier is reliable, so no MRP runs
// (exchange.Config.Reliable).
type Channel interface {
	// Exchanges is the manager whose sessions run over the channel.
	Exchanges() *exchange.Manager
	// PeerAddress is the address that manager names the peer by.
	PeerAddress() *net.UDPAddr
	// Closed is closed when the channel is gone.
	Closed() <-chan struct{}
	// ClosedCause is the error the PASE session over a lost channel ends
	// with (matter.js BleChannelClosedError, packages/protocol/src/ble/Ble.ts:17-22).
	ClosedCause() error
	// Close closes the channel.
	Close() error
}

// Options configures [Commissioner.Commission] — matter.js
// LocatedNodeCommissioningOptions (ControllerCommissioner.ts:134-170)
// with CommissioningOptions (:62-118), without the OTA provider option.
type Options struct {
	// Fabric is the fabric to commission into. Required.
	Fabric *Fabric
	// Passcode is the device's setup passcode. Required.
	Passcode uint32
	// NodeID is the node id to assign; 0 draws a random operational node
	// id no peer of the fabric has (:569-582).
	NodeID uint64
	// Addresses are the device's addresses. Required unless Channel is set.
	Addresses []*net.UDPAddr
	// Channel, when set, is the commissioning channel PASE runs over
	// instead of Addresses: a BLE peripheral's (matter.js's BLE address,
	// ControllerCommissioner.ts:442-449). The flow then configures the
	// operational network (ControllerCommissioningFlow.ts:595-620), and
	// Commission closes the channel with the PASE session.
	Channel Channel
	// WiFiNetwork, when set, is the Wi-Fi network the device is to join
	// (wifiNetwork, ControllerCommissioningFlow.ts:102-105).
	WiFiNetwork *WiFiNetwork
	// ThreadNetwork, when set, is the Thread network the device is to
	// join (threadNetwork, :110-113).
	ThreadNetwork *ThreadNetwork
	// DiscoveryData is the device as discovery found it, nil when
	// unknown: its TXT parameters seed the PASE session (:454-463) and its
	// instance id makes the addresses one device's (:547-566).
	DiscoveryData *discovery.Device
	// Timeout is the PASE budget across every address; 0 is
	// [DefaultPASETimeout].
	Timeout time.Duration
	// ClaimNodeIDAfterPASE, when set, returns the node id once PASE is
	// established; false cancels the commissioning (:283-292).
	ClaimNodeIDAfterPASE func() (uint64, bool)
	// RegulatoryLocation is used when the device is IndoorOutdoor; nil is
	// [DefaultRegulatoryLocation].
	RegulatoryLocation *gencomm.RegulatoryLocationTypeEnum
	// RegulatoryCountryCode is the country; "" is
	// [DefaultRegulatoryCountryCode].
	RegulatoryCountryCode string
	// OnAttestationFailure is the attestation policy (DAV:156).
	OnAttestationFailure attestation.Policy
	// FinalizeCommissioning, when set, replaces the built-in operational
	// CASE connection; the flow then stops before CommissioningComplete
	// (:687-690, ControllerCommissioningFlow.ts:2035-2041).
	FinalizeCommissioning func(ctx context.Context, addr handshake.PeerAddress, dd *discovery.Device, params *sessionparams.Parameters) error
	// CASEConnectionTimeout bounds the operational CASE connection; 0 is
	// [DefaultCASEConnectionTimeout]; it must be finite (:507-515).
	CASEConnectionTimeout time.Duration
	// CASEConnectionTiming overrides the peer set's timing for that
	// connection (:95-101).
	CASEConnectionTiming *peer.Timing
}

// Result is a successful commissioning — matter.js CommissionResult
// (ControllerCommissioner.ts:740-746).
type Result struct {
	// Address is the node's peer address.
	Address handshake.PeerAddress
	// FabricIndexOnPeer is the fabric index the node assigned us, 0 when
	// unknown.
	FabricIndexOnPeer uint8
}

// PASEOptions configures [Commissioner.EstablishPASE] — matter.js
// EstablishPaseOptions (:178-207).
type PASEOptions struct {
	Addresses     []*net.UDPAddr
	DiscoveryData *discovery.Device
	Passcode      uint32
	Timeout       time.Duration
	// ContinueAfterPASE, when set, decides whether the session is kept
	// (:197-206).
	ContinueAfterPASE func() bool
}

// Commissioner commissions nodes onto a fabric — matter.js
// ControllerCommissioner (ControllerCommissioner.ts:230-737) at 0ceb202d.
// It is safe for concurrent use.
type Commissioner struct {
	ctx        Context
	log        *slog.Logger
	caseServer *handshake.CASEServer
}

// New returns a commissioner over c.
func New(c Context) (*Commissioner, error) {
	if c.Exchanges == nil || c.Peers == nil {
		return nil, errors.New("commissioner: Context needs Exchanges and Peers")
	}
	if c.Logger == nil {
		c.Logger = slog.New(slog.DiscardHandler)
	}
	if c.Clock == nil {
		c.Clock = exchange.SystemClock{}
	}
	if c.Rand == nil {
		c.Rand = rand.Reader
	}
	com := &Commissioner{ctx: c, log: c.Logger}
	if c.Subscriptions != nil {
		c.Subscriptions.Register(c.Exchanges)
	}
	if len(c.Fabrics) > 0 {
		fabrics := make(handshake.FabricList, 0, len(c.Fabrics))
		for _, f := range c.Fabrics {
			fabrics = append(fabrics, f.Fabric)
		}
		server, err := handshake.NewCASEServer(c.Exchanges, handshake.CASEServerConfig{
			Fabrics:    fabrics,
			Resumption: c.Resumption,
			OnSession:  func(s *exchange.Session, _ uint8) { c.Peers.Adopt(s) },
			Logger:     c.Logger,
		})
		if err != nil {
			return nil, err
		}
		server.Register()
		com.caseServer = server
	}
	return com, nil
}

// Close removes the handlers New registered: the CASE responder (its
// handshakes in flight ended) and the report handler.
func (c *Commissioner) Close() {
	if c.caseServer != nil {
		c.caseServer.Close()
	}
	if c.ctx.Subscriptions != nil {
		c.ctx.Exchanges.SetProtocolHandler(im.InteractionModelProtocolID, nil)
	}
}

// Interaction is the interaction with peer p over its sessions, bound to
// the commissioner's subscription registry (interaction.ForPeer).
func (c *Commissioner) Interaction(p *peer.Peer) *interaction.Interaction {
	return interaction.ForPeer(p, interaction.Config{Subscriptions: c.ctx.Subscriptions, Logger: c.log, Clock: c.ctx.Clock})
}

// Commission commissions the node at opts.Addresses. Mirrors matter.js
// ControllerCommissioner.commission (ControllerCommissioner.ts:255-308):
// the requested node id must not be a peer (:267, :495-505), the options
// are validated (:268), the addresses become candidates (:270); PASE over
// the connection with suppressed peer loss (:272-280); the node id claimed
// (:283-292); the connected node commissioned (:294-298); the PASE session
// closed on every path, with a CommissioningTransitionError on success and
// the error on failure (:299-307, #closePaseSession :313-319).
func (c *Commissioner) Commission(ctx context.Context, opts Options) (Result, error) {
	if opts.Fabric == nil {
		return Result{}, errors.New("commissioner: Options.Fabric is required")
	}
	if opts.NodeID != 0 {
		if err := c.assertPeerAddress(opts.Fabric.AddressOf(opts.NodeID)); err != nil {
			return Result{}, err
		}
	}
	if opts.CASEConnectionTimeout < 0 {
		return Result{}, fmt.Errorf("commissioner: CASEConnectionTimeout must be finite, not %v", opts.CASEConnectionTimeout)
	}
	if opts.Timeout == 0 {
		opts.Timeout = DefaultPASETimeout
	}
	var session *exchange.Session
	if opts.Channel != nil {
		s, err := c.establishOverChannel(ctx, opts.Channel, opts.Timeout, opts.Passcode)
		if err != nil {
			_ = opts.Channel.Close()
			return Result{}, err
		}
		session = s
	} else {
		res, err := c.establishFromCandidates(ctx, addressesToCandidates(opts.Addresses, opts.DiscoveryData), opts.Timeout, opts.Passcode, true)
		if err != nil {
			return Result{}, err
		}
		session = res.session
	}
	nodeID := opts.NodeID
	if opts.ClaimNodeIDAfterPASE != nil {
		id, ok := opts.ClaimNodeIDAfterPASE()
		if !ok {
			err := newError(ErrCommissioning, "Commissioning cancelled: another device was already successfully connected", nil)
			c.closePASE(session, opts.Channel, err)
			return Result{}, err
		}
		nodeID = id
	}
	opts.NodeID = nodeID
	result, err := c.commissionConnectedNode(ctx, session, opts)
	if err != nil {
		c.closePASE(session, opts.Channel, err)
		return Result{}, err
	}
	c.closePASE(session, opts.Channel, &CommissioningTransitionError{Message: "Commissioning over PASE completed"})
	return result, nil
}

// closePASESession ends the PASE session as matter.js's initiateForceClose
// does — given up without a CloseSession on the wire (NodeSession.ts:349-352,
// the exchange Session.HandlePeerLoss) — so it never masks the error
// that ended commissioning (:313-319).
func (c *Commissioner) closePASESession(s *exchange.Session, cause error) {
	s.HandlePeerLoss(cause)
}

// closePASE closes the PASE session and the channel it ran over, as
// closing a BLE session's MessageChannel closes its BLE channel
// (matter.js packages/protocol/src/protocol/MessageChannel.ts:179-191).
func (c *Commissioner) closePASE(s *exchange.Session, ch Channel, cause error) {
	c.closePASESession(s, cause)
	if ch != nil {
		_ = ch.Close()
	}
}

// establishOverChannel is PASE over a commissioning channel
// (ControllerCommissioner.ts:442-492 for a BLE address): the unsecured
// session on the channel's manager, PASE with the passcode within
// timeout, peer loss suppressed; a lost channel force-closes the PASE
// session so its exchanges end at once (Ble.ts:17-22, :44-55).
func (c *Commissioner) establishOverChannel(ctx context.Context, ch Channel, timeout time.Duration, passcode uint32) (*exchange.Session, error) {
	c.log.Info("matter.controller.commissioning.establish_pase", slog.String("peer", ch.PeerAddress().String()), slog.String("channel", "ble"))
	pctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	s, err := handshake.PASE(pctx, ch.Exchanges(), ch.PeerAddress(), passcode, handshake.PASEOptions{SuppressPeerLoss: true, Logger: c.log})
	if err != nil {
		return nil, err
	}
	go func() {
		select {
		case <-ch.Closed():
			s.HandlePeerLoss(ch.ClosedCause())
		case <-s.Closing():
		}
	}()
	return s, nil
}

// EstablishPASE establishes a PASE session without running the flow.
// Mirrors matter.js ControllerCommissioner.establishPase (:352-376): the
// candidates, the connection, then ContinueAfterPASE's decision.
func (c *Commissioner) EstablishPASE(ctx context.Context, opts PASEOptions) (*exchange.Session, *discovery.Device, error) {
	if opts.Timeout == 0 {
		opts.Timeout = DefaultPASETimeout
	}
	res, err := c.establishFromCandidates(ctx, addressesToCandidates(opts.Addresses, opts.DiscoveryData), opts.Timeout, opts.Passcode, false)
	if err != nil {
		return nil, nil, err
	}
	if opts.ContinueAfterPASE != nil && !opts.ContinueAfterPASE() {
		res.session.HandlePeerLoss(newError(ErrCommissioning, "PASE established but another candidate already won", nil))
		return nil, nil, newError(ErrCommissioning, "PASE cancelled: another candidate was already selected", nil)
	}
	return res.session, opts.DiscoveryData, nil
}

// establishFromCandidates is #establishPaseFromCandidates (:378-410): the
// connection, a PairRetransmissionLimitReachedError becoming
// PeerCommunicationError("Could not connect to device").
func (c *Commissioner) establishFromCandidates(ctx context.Context, devices []*candidate, timeout time.Duration, passcode uint32, suppressPeerLoss bool) (connectionResult, error) {
	res, err := connectPASE(ctx, connectionConfig{
		devices: devices,
		timeout: timeout,
		clock:   c.ctx.Clock,
		log:     c.log,
		establish: func(actx context.Context, addr *net.UDPAddr, cand *candidate, onPBKDF func()) (*exchange.Session, error) {
			return c.establishEphemeralNodeSession(actx, addr, passcode, cand, suppressPeerLoss, onPBKDF)
		},
	})
	var pr *PairRetransmissionLimitReachedError
	if errors.As(err, &pr) {
		return connectionResult{}, &PeerCommunicationError{Message: "Could not connect to device", Cause: err}
	}
	return res, err
}

// establishEphemeralNodeSession is #establishEphemeralNodeSession
// (:413-492): PASE over UDP with the device's TXT session parameters
// (:454-463); a wrong passcode is handshake.ErrPasscodeMismatch.
func (c *Commissioner) establishEphemeralNodeSession(ctx context.Context, addr *net.UDPAddr, passcode uint32, cand *candidate, suppressPeerLoss bool, onPBKDF func()) (*exchange.Session, error) {
	c.log.Info("matter.controller.commissioning.establish_pase", slog.String("peer", addr.String()), slog.String("candidate", cand.identifier))
	return handshake.PASE(ctx, c.ctx.Exchanges, addr, passcode, handshake.PASEOptions{
		PeerParameters:       txtParameters(cand.device.TXTData),
		SuppressPeerLoss:     suppressPeerLoss,
		OnPBKDFParamResponse: onPBKDF,
		Logger:               c.log,
	})
}

// txtParameters are the session intervals a TXT record announces (:457-461).
func txtParameters(t discovery.TXTData) *sessionparams.Parameters {
	var p sessionparams.Parameters
	if t.SII != nil {
		p.SessionIdleInterval = uint32(t.SII.Milliseconds()) //nolint:gosec // TXTData bounds SII to one hour
	}
	if t.SAI != nil {
		p.SessionActiveInterval = uint32(t.SAI.Milliseconds()) //nolint:gosec // TXTData bounds SAI to one hour
	}
	if t.SAT != nil {
		p.SessionActiveThreshold = uint16(t.SAT.Milliseconds()) //nolint:gosec // TXTData bounds SAT to 65535 ms
	}
	if p.IsEmpty() {
		return nil
	}
	return &p
}

// assertPeerAddress is #assertPeerAddress (:495-499).
func (c *Commissioner) assertPeerAddress(a handshake.PeerAddress) error {
	if c.ctx.Peers.Has(a) {
		return newError(ErrNodeIDConflict, fmt.Sprintf("Node ID %d is already commissioned and can not be reused", a.NodeID), nil)
	}
	return nil
}

// addressesToCandidates is #addressesToCandidates (:547-566): the distinct
// addresses sorted by desirability (stable); one candidate carrying every
// address when the device identifier is known, else one per address with
// a synthetic identifier.
func addressesToCandidates(addrs []*net.UDPAddr, dd *discovery.Device) []*candidate {
	type entry struct {
		udp *net.UDPAddr
		da  discovery.Address
	}
	seen := map[string]bool{}
	var distinct []entry
	for _, a := range addrs {
		if a == nil || seen[a.String()] {
			continue
		}
		seen[a.String()] = true
		ip, _ := netip.AddrFromSlice(a.IP)
		ip = ip.Unmap().WithZone(a.Zone)
		distinct = append(distinct, entry{udp: a, da: discovery.Address{IP: ip, Port: uint16(a.Port)}}) //nolint:gosec // a UDP port fits 16 bits
	}
	slices.SortStableFunc(distinct, func(x, y entry) int { return cmp.Compare(discovery.CompareDesirability(x.da, y.da), 0) })
	var device discovery.Device
	if dd != nil {
		device = *dd
	}
	if dd != nil && dd.InstanceID != "" {
		c := &candidate{identifier: dd.InstanceID, device: device}
		for _, e := range distinct {
			c.addrs = append(c.addrs, e.udp)
		}
		return []*candidate{c}
	}
	out := make([]*candidate, 0, len(distinct))
	for i, e := range distinct {
		out = append(out, &candidate{identifier: fmt.Sprintf("known-address-%d-%s", i, e.udp), device: device, addrs: []*net.UDPAddr{e.udp}})
	}
	return out
}

// determineAddress is #determineAddress (:569-582): the requested node id,
// or a random operational node id no peer of the fabric has.
func (c *Commissioner) determineAddress(f *Fabric, nodeID uint64) (handshake.PeerAddress, error) {
	for {
		id := nodeID
		if id == 0 {
			var b [8]byte
			if _, err := io.ReadFull(c.ctx.Rand, b[:]); err != nil {
				return handshake.PeerAddress{}, err
			}
			if id = binary.BigEndian.Uint64(b[:]); !isOperationalNodeID(id) {
				continue
			}
		}
		addr := f.AddressOf(id)
		if err := c.assertPeerAddress(addr); err != nil {
			if nodeID != 0 {
				return handshake.PeerAddress{}, err
			}
			continue
		}
		return addr, nil
	}
}

// commissionConnectedNode is #commissionConnectedNode (:588-736): the
// defaults (:601-605), the address (:633), the flow over the PASE session
// with the attestation context (:653-670) and the CASE transition
// (:671-716); after the flow the fabric index (:719-726); on any error
// the peer at the address deleted (:727-730).
func (c *Commissioner) commissionConnectedNode(ctx context.Context, pase *exchange.Session, opts Options) (Result, error) {
	if opts.RegulatoryLocation == nil {
		l := DefaultRegulatoryLocation
		opts.RegulatoryLocation = &l
	}
	if opts.RegulatoryCountryCode == "" {
		opts.RegulatoryCountryCode = DefaultRegulatoryCountryCode
	}
	if opts.CASEConnectionTimeout == 0 {
		opts.CASEConnectionTimeout = DefaultCASEConnectionTimeout
	}
	address, err := c.determineAddress(opts.Fabric, opts.NodeID)
	if err != nil {
		return Result{}, err
	}
	c.log.Info("matter.controller.commissioning.start", slog.Uint64("node_id", address.NodeID), slog.Uint64("fabric_id", opts.Fabric.FabricID))
	mgr := c.ctx.Exchanges
	if opts.Channel != nil {
		mgr = opts.Channel.Exchanges()
	}
	f := newFlow(c, interaction.New(mgr, pase, c.log), opts, address, pase.AttestationChallenge(),
		func(ctx context.Context, addr handshake.PeerAddress, concurrent bool) (*interaction.Interaction, error) {
			return c.transitionToCase(ctx, pase, addr, concurrent, opts)
		})
	if err := f.execute(ctx); err != nil {
		if p := c.ctx.Peers.Get(address); p != nil {
			p.Delete(context.WithoutCancel(ctx))
		}
		return Result{}, err
	}
	// The device's commissioning window is closed now (:721-723).
	c.forgetCommissioned(opts)
	return Result{Address: address, FabricIndexOnPeer: f.data.fabricIndex}, nil
}

// forgetCommissioned is #forgetCommissionedDevice (:321-341): every
// scanner forgets the device by the BLE address it was reached at and,
// with discovery data that carries D and VP, by its identity. The mDNS
// scanner implements none of it; the node withdraws its record.
func (c *Commissioner) forgetCommissioned(opts Options) {
	if len(c.ctx.Scanners) == 0 {
		return
	}
	var addresses []string
	if ch, ok := opts.Channel.(interface{ PeripheralAddress() string }); ok {
		addresses = append(addresses, ch.PeripheralAddress())
	}
	var identity *discovery.CommissionedIdentity
	if opts.DiscoveryData != nil {
		if id, ok := opts.DiscoveryData.Identity(); ok {
			identity = &id
		}
	}
	for _, s := range c.ctx.Scanners {
		s.ForgetCommissioned(addresses, identity)
	}
}

// transitionToCase is the flow's transition (:671-716): a non-concurrent
// device's PASE session closes first; FinalizeCommissioning when given,
// the flow then stopping; else the peer — its discovery data and the
// parameters the PASE session reported — connects over CASE with the
// connection timeout and timing, a NoSharedTrustRoots refusal retried
// after 15 s, and the CASE session is the interaction's from then on.
func (c *Commissioner) transitionToCase(ctx context.Context, pase *exchange.Session, addr handshake.PeerAddress, concurrent bool, opts Options) (*interaction.Interaction, error) {
	if !concurrent {
		c.closePASE(pase, opts.Channel, &CommissioningTransitionError{Message: "Commissioning session closed because node has now joined fabric"})
	}
	if opts.FinalizeCommissioning != nil {
		return nil, opts.FinalizeCommissioning(ctx, addr, opts.DiscoveryData, pase.ReportedParameters())
	}
	// The peer with the discovery data and the parameters the PASE session
	// reported (:692-696). The peer is new (the address was checked), so
	// AddKnownPeer's merge is matter.js's assignment.
	d := peer.Descriptor{Address: addr, ReportedSessionParameters: pase.ReportedParameters()}
	if opts.DiscoveryData != nil {
		txt := opts.DiscoveryData.TXTData
		d.DiscoveryData = &txt
	}
	p, err := c.ctx.Peers.AddKnownPeer(d) //nolint:contextcheck // the peer's service resolves under the DNS-SD cache's context
	if err != nil {
		return nil, err
	}
	s, err := p.Connect(ctx, peer.ConnectOptions{
		ConnectionTimeout: opts.CASEConnectionTimeout,
		Timing:            opts.CASEConnectionTiming,
		HandleError:       c.handleNoSharedTrustRoots,
	})
	if err != nil {
		return nil, err
	}
	return interaction.New(c.ctx.Exchanges, s, c.log), nil
}

// handleNoSharedTrustRoots is the CASE connection's error handler during
// commissioning: a device that answers NoSharedTrustRoots may not know the
// new fabric yet, so the attempt retries after 15 s instead of the peer's
// default delay; any other error keeps that default. Mirrors matter.js
// ControllerCommissioner.transitionToCase's handleError
// (packages/protocol/src/peer/ControllerCommissioner.ts:701-708) at 862ad75f.
func (c *Commissioner) handleNoSharedTrustRoots(err error) (time.Duration, bool, error) {
	var sr *exchange.StatusReportError
	if errors.As(err, &sr) && sr.Report.ProtocolCode == mrp.SCStatusProtocolNoSharedTrustRoots {
		c.log.Warn("matter.controller.commissioning.no_shared_trust_roots", slog.String("retry", noSharedTrustRootsRetry.String()))
		return noSharedTrustRootsRetry, true, nil
	}
	return 0, false, nil
}

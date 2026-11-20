// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package handshake

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"time"

	"github.com/SukramJ/go-fabric/client/exchange"
	"github.com/SukramJ/go-fabric/clock"
	"github.com/SukramJ/go-fabric/internal/exchangeseam"
	"github.com/SukramJ/go-fabric/secure/channel"
	"github.com/SukramJ/go-fabric/secure/sigma"
	"github.com/SukramJ/go-fabric/transport/mrp"
)

// FabricResolver gives a CASE responder our side of the fabric a Sigma1
// addresses.
type FabricResolver interface {
	// FabricForDestination returns the first fabric one of whose
	// candidate destination ids equals destinationID for initiatorRandom,
	// false for none — matter.js FabricManager.findFabricFromDestinationId
	// (packages/protocol/src/fabric/FabricManager.ts:325-353 at db021da0).
	// A fabric being deleted is not returned: matter.js throws
	// FabricNotFoundError for it (:331-333) as for no match (:352).
	FabricForDestination(destinationID [32]byte, initiatorRandom [sigma.RandomSize]byte) (Fabric, bool)
	// FabricByIndex returns the fabric of a resumption record — matter.js
	// keeps the fabric in the record (SessionManager.ts:837-839,
	// CaseServer.ts:155 at b25ed960).
	FabricByIndex(fabricIndex uint8) (Fabric, bool)
}

// FabricList is a [FabricResolver] over a fixed list of fabrics, in the
// order matter.js's FabricManager iterates them (FabricManager.ts:309).
type FabricList []Fabric

// FabricForDestination implements [FabricResolver]: the candidate is the
// destination id of the fabric's operational IPK, root public key, fabric
// id and our node id (Fabric.destinationIdsFor with the one IPK a
// controller fabric holds).
func (l FabricList) FabricForDestination(destinationID [32]byte, initiatorRandom [sigma.RandomSize]byte) (Fabric, bool) {
	for _, f := range l {
		if f.Identity == nil {
			continue
		}
		id := f.Identity
		if sigma.ComputeDestinationID(id.IPK, initiatorRandom, f.RootPublicKey, id.FabricID, id.NodeID) == destinationID {
			return f, true
		}
	}
	return Fabric{}, false
}

// FabricByIndex implements [FabricResolver].
func (l FabricList) FabricByIndex(fabricIndex uint8) (Fabric, bool) {
	for _, f := range l {
		if f.Identity != nil && f.Identity.FabricIndex == fabricIndex {
			return f, true
		}
	}
	return Fabric{}, false
}

// CASEServerConfig configures [NewCASEServer].
type CASEServerConfig struct {
	// Fabrics resolves the fabric a Sigma1 addresses. Required.
	Fabrics FabricResolver
	// Resumption holds the resumption records: a resuming Sigma1 is
	// looked up by its resumption id, and every established session's
	// record is saved. Nil neither resumes nor saves.
	Resumption ResumptionStore
	// OnSession is called with every session the responder established
	// and its fabric index, as its establishment completes — matter.js's
	// session joins SessionManager.sessions on its established event
	// (SessionManager.ts:1274-1280 at b25ed960; CaseServer no longer adds
	// it), the establishment completed at CaseServer.ts:206 (resumed, once
	// the initiator's Success arrived) or :331 (full, at the first
	// transmission of our Success, before its acknowledgement is awaited)
	// — which a peer set observes to adopt the session
	// (packages/protocol/src/peer/PeerSet.ts:104-110). It runs before the
	// record is saved, as matter.js saves it after (:216-217, :356-357).
	// Nil ignores it.
	OnSession func(s *exchange.Session, fabricIndex uint8)
	// Logger receives the diagnostics; nil is slog.Default().
	Logger *slog.Logger
}

// CASEServer is the controller's CASE responder: a peer that opens CASE to
// us — a node re-establishing its subscriptions after a restart — is
// answered as a device answers a controller. It is the port of matter.js
// packages/protocol/src/session/case/CaseServer.ts:50-360 at b25ed960
// over secure/sigma's Responder, which holds the cryptography; this type
// holds the exchange loop, the status reports, the session and the
// record.
type CASEServer struct {
	mgr *exchange.Manager
	cfg CASEServerConfig
	log *slog.Logger

	ctx    context.Context
	cancel context.CancelFunc

	// mu guards closed and wg.
	mu     sync.Mutex
	closed bool
	wg     sync.WaitGroup

	// seen are the initiator randoms of the Sigma1s that started a
	// handshake, pending those of the Sigma1s being checked
	// ([CASEServer.claim]); guarded by seenMu. pending holds one entry per
	// handshake in flight, so the manager's inbound sessions bound it.
	seenMu    sync.Mutex
	seen      map[[sigma.RandomSize]byte]time.Duration
	seenOrder [][sigma.RandomSize]byte
	pending   map[[sigma.RandomSize]byte]bool
	clock     clock.Clock
}

// Bounds of the initiator randoms the responder remembers (review II,
// R2-P3-ctl-lan-02; module's own — neither matter.js's CaseServer nor
// connectedhomeip's CASESession remembers them). The lifetime is the
// longest an inbound unsecured session lives and so the longest a
// handshake a Sigma1 opened can hold its slot (client/exchange/manager.go
// inboundUnsecuredLifetime = 60 s); the count is the number of such
// sessions the manager holds at once (maxInboundUnsecuredSessions = 256).
const (
	seenSigma1Lifetime = 60 * time.Second
	seenSigma1Limit    = 256
)

// errRepeatedSigma1 ends a handshake whose Sigma1 started one before: it
// is answered with nothing.
var errRepeatedSigma1 = errors.New("handshake: a Sigma1 with this initiator random started a handshake before")

// NewCASEServer returns the responder over mgr. [CASEServer.Register]
// installs it.
func NewCASEServer(mgr *exchange.Manager, cfg CASEServerConfig) (*CASEServer, error) {
	if mgr == nil || cfg.Fabrics == nil {
		return nil, errors.New("handshake: a CASE server needs a manager and a FabricResolver")
	}
	log := cfg.Logger
	if log == nil {
		log = slog.Default()
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &CASEServer{
		mgr: mgr, cfg: cfg, log: log, ctx: ctx, cancel: cancel,
		seen: map[[sigma.RandomSize]byte]time.Duration{}, pending: map[[sigma.RandomSize]byte]bool{}, clock: clock.System{},
	}, nil
}

// claim takes random for a Sigma1 about to be checked, false when a Sigma1
// with it is being checked or started a handshake within
// [seenSigma1Lifetime]. A Sigma1 is fresh for its initiator: each
// handshake draws a new random (matter.js CaseClient.ts:120 at db021da0,
// connectedhomeip CASESession.cpp:750-752 at 6170af84), and a retransmission
// rides the session it came on, whose duplicate detection answers it. So
// a repeat under another source node id is a copy, and the work of a
// Sigma2 for it would hold a session for nobody. The claim ends with
// [CASEServer.remember] or [CASEServer.release].
func (c *CASEServer) claim(random [sigma.RandomSize]byte) bool {
	now := clock.MonotonicOf(c.clock)
	c.seenMu.Lock()
	defer c.seenMu.Unlock()
	if at, ok := c.seen[random]; c.pending[random] || ok && now-at < seenSigma1Lifetime {
		return false
	}
	c.pending[random] = true
	return true
}

// release ends a claim whose Sigma1 started no handshake: a Sigma1 that
// addressed none of our fabrics costs nothing and is not remembered, so
// it cannot crowd out the randoms that did.
func (c *CASEServer) release(random [sigma.RandomSize]byte) {
	c.seenMu.Lock()
	defer c.seenMu.Unlock()
	delete(c.pending, random)
}

// remember ends a claim whose Sigma1 started a handshake: random is
// remembered, the oldest forgotten beyond [seenSigma1Limit] or past
// [seenSigma1Lifetime].
func (c *CASEServer) remember(random [sigma.RandomSize]byte) {
	now := clock.MonotonicOf(c.clock)
	c.seenMu.Lock()
	defer c.seenMu.Unlock()
	delete(c.pending, random)
	for len(c.seenOrder) > 0 {
		oldest := c.seenOrder[0]
		if len(c.seenOrder) < seenSigma1Limit && now-c.seen[oldest] < seenSigma1Lifetime {
			break
		}
		delete(c.seen, oldest)
		c.seenOrder = c.seenOrder[1:]
	}
	if _, ok := c.seen[random]; !ok {
		c.seenOrder = append(c.seenOrder, random)
	}
	c.seen[random] = now
}

// forget drops random from the remembered randoms: its handshake ended
// before its first answer reached the wire ([CASEServer.sendAnswer]), so
// the initiator's retransmission of the Sigma1 is not a copy but the only
// way that handshake can go on (review II, R2-P3-dr-ctl-06).
func (c *CASEServer) forget(random [sigma.RandomSize]byte) {
	c.seenMu.Lock()
	defer c.seenMu.Unlock()
	if _, ok := c.seen[random]; !ok {
		return
	}
	delete(c.seen, random)
	if i := slices.Index(c.seenOrder, random); i >= 0 {
		c.seenOrder = slices.Delete(c.seenOrder, i, i+1)
	}
}

// sendAnswer sends the handshake's first answer to the Sigma1 with random,
// Sigma2 or Sigma2_Resume. A send whose first transmission the socket
// refused put nothing on the wire — the exchange returns
// [exchange.ErrNetwork] for that transmission only and logs a refused
// retransmission (client/exchange/exchange.go Send, transmitLocked and
// retransmit) — so the Sigma1 is forgotten: the initiator's MRP
// retransmission of it, on a new unsecured session once this one closed,
// starts the handshake again. A Sigma2 that left and was not
// acknowledged keeps the Sigma1 remembered, as a copy of it would.
func (c *CASEServer) sendAnswer(ctx context.Context, x *exchange.Exchange, random [sigma.RandomSize]byte, opcode uint8, payload []byte) error {
	err := x.Send(ctx, opcode, payload, exchange.SendOptions{ExpectedProcessingTime: ExpectedCryptoProcessingTime})
	if errors.Is(err, exchange.ErrNetwork) {
		c.forget(random)
	}
	return err
}

// Register installs the responder as the manager's Secure Channel handler,
// which receives the Sigma1 of an unsecured session a peer opens —
// matter.js SecureChannelProtocol.ts:96-100 (the CaseServer it creates)
// and :146-148 (Sigma1 dispatched to it).
func (c *CASEServer) Register() {
	c.mgr.SetProtocolHandler(mrp.SecureChannelProtocolID, c.Serve)
}

// Close removes the responder from the manager, ends the handshakes in
// flight and waits for them.
func (c *CASEServer) Close() {
	c.mgr.SetProtocolHandler(mrp.SecureChannelProtocolID, nil)
	c.mu.Lock()
	c.closed = true
	c.mu.Unlock()
	c.cancel()
	c.wg.Wait()
}

// Serve answers the CASE handshake the peer opened on x with first, its
// Sigma1. It is the responder's exchange.ProtocolHandler. Mirrors matter.js
// CaseServer.onNewExchange (CaseServer.ts:62-102 at b25ed960): the handshake; on an
// error a StatusReport unless the exchange counts as closed or a message
// we sent is unacknowledged (:84-88) — NoSharedTrustRoots when no fabric
// matched (:90-92), nothing when the peer reported the failure itself or
// stopped answering (:93-94), InvalidParam otherwise (:95); and in every
// case the unsecured session closes (:97-100). A handshake the server's
// Close cut short reports nothing.
func (c *CASEServer) Serve(x *exchange.Exchange, first *exchange.Message) {
	defer x.Session().Close()
	defer x.Close()
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.wg.Add(1)
	c.mu.Unlock()
	defer c.wg.Done()
	err := c.handleSigma1(c.ctx, x, first)
	if err == nil {
		return
	}
	if errors.Is(err, errRepeatedSigma1) {
		c.log.Debug("matter.controller.case_server.failed", slog.Any("err", err))
		return
	}
	c.log.Info("matter.controller.case_server.failed", slog.Any("err", err))
	if c.ctx.Err() != nil || x.ConsiderClosed() || x.HasUnackedMessage() {
		return
	}
	var code uint16
	switch {
	case errors.Is(err, sigma.ErrNoSharedTrustRoots):
		code = mrp.SCStatusProtocolNoSharedTrustRoots
	case causedByPeerStatusOrUnresponsive(err):
		return
	default:
		code = statusInvalidParam
	}
	if serr := c.sendStatus(c.ctx, x, mrp.SCStatusGeneralFailure, code); serr != nil {
		c.log.Debug("matter.controller.case_server.status_failed", slog.Any("err", serr))
	}
}

// causedByPeerStatusOrUnresponsive is CaseServer.ts:94's (at b25ed960)
// causedBy(error, ChannelStatusResponseError, PeerUnresponsiveError): the
// peer's own StatusReport, or a peer that did not answer
// (PeerMessageMissingError extends PeerUnresponsiveError).
func causedByPeerStatusOrUnresponsive(err error) bool {
	var status *exchange.StatusReportError
	var unresponsive *exchange.PeerUnresponsiveError
	return errors.As(err, &status) || errors.As(err, &unresponsive) || errors.Is(err, &exchange.PeerUnresponsiveError{})
}

// sendStatus sends a Secure Channel StatusReport with the messenger's
// crypto processing time (SecureChannelMessenger.ts:160-172).
func (c *CASEServer) sendStatus(ctx context.Context, x *exchange.Exchange, general, code uint16) error {
	report := exchange.StatusReport{GeneralCode: general, ProtocolID: uint32(mrp.SecureChannelProtocolID), ProtocolCode: code}
	return x.Send(ctx, mrp.SCOpcodeStatusReport, report.Encode(), exchange.SendOptions{ExpectedProcessingTime: ExpectedCryptoProcessingTime})
}

// responderFabrics adapts a FabricResolver to sigma's resolvers and
// remembers the fabric the handshake landed on.
type responderFabrics struct {
	fabrics FabricResolver
	chosen  Fabric
	found   bool
}

// ResolveSigma1Destination implements sigma.IdentityResolver.
func (r *responderFabrics) ResolveSigma1Destination(destinationID [32]byte, initiatorRandom [sigma.RandomSize]byte) (*sigma.Identity, sigma.PeerVerifier, bool) {
	f, ok := r.fabrics.FabricForDestination(destinationID, initiatorRandom)
	if !ok || f.Identity == nil || f.Verifier == nil {
		return nil, nil, false
	}
	r.chosen, r.found = f, true
	return f.Identity, f.Verifier, true
}

// ResolveFabricIndex implements sigma.FabricIndexResolver for the resume
// path: the record's fabric (CaseServer.ts:155 at b25ed960).
func (r *responderFabrics) ResolveFabricIndex(fabricIndex uint8) (*sigma.Identity, sigma.PeerVerifier, bool) {
	f, ok := r.fabrics.FabricByIndex(fabricIndex)
	if !ok || f.Identity == nil || f.Verifier == nil {
		return nil, nil, false
	}
	r.chosen, r.found = f, true
	return f.Identity, f.Verifier, true
}

// recordLookup serves sigma's resume probe the one record the Sigma1's
// resumption id named (CaseServer.ts:115-118 at b25ed960).
type recordLookup struct {
	rec *ResumptionRecord
}

// GetByID implements sigma.ResumptionStore.
func (l recordLookup) GetByID(id []byte) (*sigma.ResumptionRecord, error) {
	if l.rec == nil {
		return nil, errors.New("handshake: no resumption record")
	}
	return &sigma.ResumptionRecord{
		SharedSecret: l.rec.SharedSecret,
		ResumptionID: id,
		FabricIndex:  l.rec.Peer.FabricIndex,
		PeerNodeID:   l.rec.Peer.NodeID,
		PeerCATs:     l.rec.CaseAuthenticatedTags,
	}, nil
}

// handleSigma1 is CaseServer.#handleSigma1 (CaseServer.ts:104-148 at b25ed960).
func (c *CASEServer) handleSigma1(ctx context.Context, x *exchange.Exchange, first *exchange.Message) error {
	// readSigma1 (CaseMessenger.ts:22-25): a malformed Sigma1 is data the
	// peer sent (SecureChannelMessenger.ts:117-139).
	sigma1, err := sigma.UnmarshalSigma1(first.Payload)
	if err != nil {
		return fmt.Errorf("%w: malformed Sigma1: %w", ErrUnexpectedData, err)
	}
	// Both or neither of resumptionId and initiatorResumeMic (:108-111).
	if (len(sigma1.ResumptionID) == 0) != (len(sigma1.InitiatorResumeMIC) == 0) {
		return fmt.Errorf("%w: Sigma1 must carry both resumptionId and initiatorResumeMic or neither", ErrUnexpectedData)
	}
	// A Sigma1 that started a handshake before, or is being checked, is
	// answered with nothing, before any work for it (review II,
	// R2-P3-ctl-lan-02).
	if !c.claim(sigma1.InitiatorRandom) {
		return errRepeatedSigma1
	}
	started := false
	defer func() {
		if !started {
			c.release(sigma1.InitiatorRandom)
		}
	}()
	// The record the resumption id names, if any (:113-116).
	var record *ResumptionRecord
	if len(sigma1.ResumptionID) > 0 && c.cfg.Resumption != nil {
		if rec, ok := c.cfg.Resumption.FindByResumptionID(sigma1.ResumptionID); ok {
			record = &rec
		}
	}
	us := x.Session()
	// The initiator's parameters time the unsecured session (:120-123).
	if sigma1.InitiatorSessionParams != nil {
		us.SetTimingParameters(*sigma1.InitiatorSessionParams)
	}
	// The responder session id is taken before Sigma2 (or Sigma2_Resume)
	// announces it (:173, :266).
	sessionID, err := c.mgr.AllocateSessionID(ctx)
	if err != nil {
		return err
	}
	fabrics := &responderFabrics{fabrics: c.cfg.Fabrics}
	responder := sigma.NewResponder(nil, nil, sessionID)
	responder.SetIdentityResolver(fabrics)
	local := c.mgr.LocalParameters()
	// The responder's session parameters ride in Sigma2 and Sigma2_Resume
	// (:195-200, :272).
	responder.SetSessionParameters(&local)
	if record != nil {
		responder.SetResumptionStore(recordLookup{rec: record})
	}
	// Resume first; a MIC that does not decrypt falls back to the full
	// handshake (:125-133, :161-170) — secure/sigma's
	// ProcessSigma1WithResume does both. The third outcome of :135-145
	// cannot arise: with both fields present and a record, the resume
	// either succeeds or clears them for the full handshake.
	result, err := responder.ProcessSigma1WithResume(first.Payload)
	if err != nil {
		return err
	}
	// The Sigma1 addressed one of our fabrics (or a record of ours): its
	// handshake starts.
	started = true
	c.remember(sigma1.InitiatorRandom)
	if result.IsResume() {
		return c.resume(ctx, x, responder, fabrics, record, sigma1, result)
	}
	return c.full(ctx, x, responder, fabrics, sigma1, result)
}

// resume is CaseServer.#resume after the MIC check (CaseServer.ts:150-220
// at b25ed960): the session from the record, registered establishing
// (:174-190); Sigma2_Resume with a fresh resumption id, our session id and
// parameters (:192-202); the initiator's Success awaited and the session's
// establishment completed then (:204-206) — a session closed during the
// handshake is refused with errClosedBeforeEstablished (:367-376); any
// failure in between closes the session without a CloseSession and ends
// the handshake (:207-211); the record's id replaced and the record saved
// after (:214-217).
//
// The session is registered before Sigma2_Resume goes out and, while it
// establishes, processes what the initiator sends on it
// (NodeSession.ts:223-225 at b25ed960): the initiator holds the session once
// Sigma2_Resume reached it and sends on it at once, its Success first and
// then its first request, which this manager reads on another goroutine
// while this one still returns from the send
// (BD-Matter-CASEServer-ResumedSessionBeforeSigma2Resume).
func (c *CASEServer) resume(ctx context.Context, x *exchange.Exchange, responder *sigma.Responder, fabrics *responderFabrics, record *ResumptionRecord, sigma1 sigma.Sigma1, result sigma.Sigma1ProcessResult) error {
	if !fabrics.found {
		return fmt.Errorf("%w: resumption record of an unknown fabric", ErrUnexpectedData)
	}
	fabric := fabrics.chosen
	keys := result.ResumeKeys
	s, err := c.newEstablishingSession(exchange.SecureSessionConfig{
		ID:   responder.SessionID(),
		Addr: x.Session().Addr(),
		Channel: channel.Config{
			EncryptKey:    keys.R2IKey[:],
			DecryptKey:    keys.I2RKey[:],
			LocalNodeID:   fabric.Identity.NodeID,
			PeerNodeID:    record.Peer.NodeID,
			PeerCATs:      responder.PeerCATs(),
			PeerSessionID: sigma1.InitiatorSessionID,
		},
		PeerParameters:        exchange.PeerParameters(sigma1.InitiatorSessionParams),
		ReportedParameters:    sigma1.InitiatorSessionParams,
		CaseAuthenticatedTags: record.CaseAuthenticatedTags,
		AttestationChallenge:  keys.AttestationChallenge[:],
		Resumed:               true,
		FabricIndex:           fabric.Identity.FabricIndex,
		Responder:             true,
	})
	if err != nil {
		return err
	}
	if err := c.sendAnswer(ctx, x, sigma1.InitiatorRandom, mrp.SCOpcodeSigma2Resume, sigma.MarshalSigma2Resume(*result.Sigma2Resume)); err != nil {
		s.Close()
		return err
	}
	if _, err := nextMessage(ctx, x, mrp.SCOpcodeStatusReport, ExpectedCryptoProcessingTime); err != nil {
		s.Close()
		return err
	}
	if err := completeEstablishmentOf(s); err != nil {
		s.Close()
		return err
	}
	c.handOver(s, fabric)
	rec := *record
	rec.ResumptionID = append([]byte(nil), result.Sigma2Resume.ResumptionID...)
	c.established(s, fabric, rec)
	return nil
}

// full is CaseServer.#generateSigma2 (CaseServer.ts:222-360 at b25ed960)
// around secure/sigma's crypto: Sigma2 sent, Sigma3 read and verified (a
// fabric id mismatch is UnexpectedData, :303-305, an InvalidParam), the
// session registered establishing (:309-327), Success sent with the
// session established — and handed over ([CASEServerConfig.OnSession]) —
// at its first transmission (:329-331, the send's onTransmitted through
// exchangeseam.SendNotifyingTransmission), the record built (:347-354) and
// saved (:357). A Success whose first
// transmission fails — or that went out while the session was already
// closing — closes the session and ends the handshake (:333-336); one that
// went out and then fails (no acknowledgement) keeps the established
// session and the handshake completes, since the initiator may use the
// session from that moment (:337-342). A session closed before its
// establishment completed is refused with errClosedBeforeEstablished
// (:344, :367-376) and not closed again: it is closing already.
func (c *CASEServer) full(ctx context.Context, x *exchange.Exchange, responder *sigma.Responder, fabrics *responderFabrics, sigma1 sigma.Sigma1, result sigma.Sigma1ProcessResult) error {
	fabric := fabrics.chosen
	if err := c.sendAnswer(ctx, x, sigma1.InitiatorRandom, mrp.SCOpcodeSigma2, result.Sigma2.Marshal()); err != nil {
		return err
	}
	msg, err := nextMessage(ctx, x, mrp.SCOpcodeSigma3, ExpectedCryptoProcessingTime)
	if err != nil {
		return err
	}
	if err := responder.ProcessSigma3(msg.Payload); err != nil {
		return err
	}
	keys, _ := responder.SessionKeys()
	peerNodeID := responder.PeerNodeID()
	cats := responder.PeerCATs()
	s, err := c.newEstablishingSession(exchange.SecureSessionConfig{
		ID:   responder.SessionID(),
		Addr: x.Session().Addr(),
		Channel: channel.Config{
			EncryptKey:    keys.R2IKey[:],
			DecryptKey:    keys.I2RKey[:],
			LocalNodeID:   fabric.Identity.NodeID,
			PeerNodeID:    peerNodeID,
			PeerCATs:      cats,
			PeerSessionID: responder.PeerSessionID(),
		},
		PeerParameters:        exchange.PeerParameters(sigma1.InitiatorSessionParams),
		ReportedParameters:    sigma1.InitiatorSessionParams,
		CaseAuthenticatedTags: cats,
		AttestationChallenge:  keys.AttestationChallenge[:],
		FabricIndex:           fabric.Identity.FabricIndex,
		Responder:             true,
	})
	if err != nil {
		return err
	}
	established := false
	report := exchange.StatusReport{GeneralCode: mrp.SCStatusGeneralSuccess, ProtocolID: uint32(mrp.SecureChannelProtocolID), ProtocolCode: mrp.SCStatusProtocolSessionEstablishmentSuccess}
	if err := exchangeseam.SendNotifyingTransmission(ctx, x, mrp.SCOpcodeStatusReport, report.Encode(),
		exchange.SendOptions{ExpectedProcessingTime: ExpectedCryptoProcessingTime},
		func() {
			if established = exchangeseam.CompleteSecureSessionEstablishment(s); established {
				c.handOver(s, fabric)
			}
		}); err != nil {
		if !established {
			s.Close()
			return err
		}
		c.log.Debug("matter.controller.case_server.success_failed_after_transmission",
			slog.Int("session_id", int(s.ID())), slog.Any("err", err))
	}
	if err := completeEstablishmentOf(s); err != nil {
		return err
	}
	c.established(s, fabric, ResumptionRecord{
		Peer:                  PeerAddress{FabricIndex: fabric.Identity.FabricIndex, NodeID: peerNodeID},
		SharedSecret:          responder.ECDHSharedSecret(),
		ResumptionID:          responder.ResumptionID(),
		SessionParameters:     s.Parameters(),
		CaseAuthenticatedTags: cats,
	})
	return nil
}

// errClosedBeforeEstablished is a responder session closed during its
// handshake — closed with its peer, its fabric's sessions or the manager —
// which therefore never becomes established: matter.js's SessionClosedError
// of completeEstablishmentOf (CaseServer.ts:367-376 at b25ed960). The
// handshake ends without handing the session over or saving a record, and
// Serve answers it with InvalidParam unless the exchange counts as closed
// or a message is unacknowledged (:84-96).
var errClosedBeforeEstablished = fmt.Errorf("%w before its establishment completed", exchange.ErrSessionClosed)

// newEstablishingSession registers the responder session establishing
// (CaseServer.ts:177-190, :314-327 at b25ed960, establishing: true).
func (c *CASEServer) newEstablishingSession(cfg exchange.SecureSessionConfig) (*exchange.Session, error) {
	registered, err := exchangeseam.NewEstablishingSecureSession(c.mgr, cfg)
	if err != nil {
		return nil, err
	}
	s, ok := registered.(*exchange.Session)
	if !ok {
		return nil, errors.New("handshake: establishing session of a foreign type")
	}
	return s, nil
}

// completeEstablishmentOf ends the establishment of s, refusing a session
// that closed during its handshake. Mirrors matter.js CaseServer.ts:367-376
// at b25ed960 (completeEstablishmentOf).
func completeEstablishmentOf(s *exchange.Session) error {
	if !exchangeseam.CompleteSecureSessionEstablishment(s) {
		return fmt.Errorf("handshake: session %d: %w", s.ID(), errClosedBeforeEstablished)
	}
	return nil
}

// handOver hands the established session to [CASEServerConfig.OnSession]:
// matter.js's established event, which adds the session to
// SessionManager.sessions (SessionManager.ts:1274-1280 at b25ed960) where
// the peer set adopts it (PeerSet.ts:104-110).
func (c *CASEServer) handOver(s *exchange.Session, fabric Fabric) {
	if c.cfg.OnSession != nil {
		c.cfg.OnSession(s, fabric.Identity.FabricIndex)
	}
}

// established logs the session and saves its record — the save of
// CaseServer.ts:217 and :357 at b25ed960, a failure only logged as the CASE
// client logs it (CaseClient.ts:369-381). The session was handed over
// already ([CASEServer.handOver]).
func (c *CASEServer) established(s *exchange.Session, fabric Fabric, rec ResumptionRecord) {
	c.log.Info("matter.controller.case_server.session",
		slog.Int("session_id", int(s.ID())), slog.Bool("resumed", s.Resumed()),
		slog.Int("fabric_index", int(fabric.Identity.FabricIndex)), slog.Uint64("node_id", s.PeerNodeID()))
	if c.cfg.Resumption != nil {
		// Saved in the responder's role, also when resumed from a record
		// the client saved (R2-P7-sec-01).
		rec.Initiator = false
		if err := c.cfg.Resumption.SaveResumptionRecord(rec); err != nil {
			c.log.Warn("matter.controller.case_server.resumption_save_failed", slog.Any("err", err))
		}
	}
}

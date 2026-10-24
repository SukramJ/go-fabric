// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package handshake

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"github.com/SukramJ/go-fabric/client/exchange"
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
	// (packages/protocol/src/fabric/FabricManager.ts:306-334 at 0ceb202d).
	// A fabric being deleted is not returned: matter.js throws
	// FabricNotFoundError for it (:312-314) as for no match (:333).
	FabricForDestination(destinationID [32]byte, initiatorRandom [sigma.RandomSize]byte) (Fabric, bool)
	// FabricByIndex returns the fabric of a resumption record — matter.js
	// keeps the fabric in the record (SessionManager.ts:837-839,
	// CaseServer.ts:153).
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
	// and its fabric index — matter.js `this.#sessions.sessions.add`
	// (CaseServer.ts:211, and createSecureSession's registration at
	// :316-328), which a peer set observes to adopt the session. Nil
	// ignores it.
	OnSession func(s *exchange.Session, fabricIndex uint8)
	// Logger receives the diagnostics; nil is slog.Default().
	Logger *slog.Logger
}

// CASEServer is the controller's CASE responder: a peer that opens CASE to
// us — a node re-establishing its subscriptions after a restart — is
// answered as a device answers a controller. It is the port of matter.js
// packages/protocol/src/session/case/CaseServer.ts:49-347 at 0ceb202d
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
}

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
	return &CASEServer{mgr: mgr, cfg: cfg, log: log, ctx: ctx, cancel: cancel}, nil
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
// CaseServer.onNewExchange (CaseServer.ts:61-100): the handshake; on an
// error a StatusReport unless the exchange counts as closed or a message
// we sent is unacknowledged (:82-86) — NoSharedTrustRoots when no fabric
// matched (:88-90), nothing when the peer reported the failure itself or
// stopped answering (:91-92), InvalidParam otherwise (:93); and in every
// case the unsecured session closes (:95-99). A handshake the server's
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

// causedByPeerStatusOrUnresponsive is CaseServer.ts:92's
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
// path: the record's fabric (CaseServer.ts:153).
func (r *responderFabrics) ResolveFabricIndex(fabricIndex uint8) (*sigma.Identity, sigma.PeerVerifier, bool) {
	f, ok := r.fabrics.FabricByIndex(fabricIndex)
	if !ok || f.Identity == nil || f.Verifier == nil {
		return nil, nil, false
	}
	r.chosen, r.found = f, true
	return f.Identity, f.Verifier, true
}

// recordLookup serves sigma's resume probe the one record the Sigma1's
// resumption id named (CaseServer.ts:113-116).
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
		PeerCATs:     l.rec.PeerCATs,
	}, nil
}

// handleSigma1 is CaseServer.#handleSigma1 (CaseServer.ts:102-146).
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
	if result.IsResume() {
		return c.resume(ctx, x, responder, fabrics, record, sigma1, result)
	}
	return c.full(ctx, x, responder, fabrics, sigma1, result)
}

// resume is CaseServer.#resume (CaseServer.ts:148-222) after the MIC
// check: Sigma2_Resume with a fresh resumption id, our session id and
// parameters (:190-201); the session from the record, registered once
// Sigma2_Resume is out (matter.js creates it before and closes it when
// the send fails, :172-206); the record's id replaced (:213); the
// initiator's Success awaited (:216); the record saved (:218-219). A
// Success that does not arrive closes the session: matter.js keeps it
// registered (:211, before :216), a divergence the report names.
func (c *CASEServer) resume(ctx context.Context, x *exchange.Exchange, responder *sigma.Responder, fabrics *responderFabrics, record *ResumptionRecord, sigma1 sigma.Sigma1, result sigma.Sigma1ProcessResult) error {
	if !fabrics.found {
		return fmt.Errorf("%w: resumption record of an unknown fabric", ErrUnexpectedData)
	}
	fabric := fabrics.chosen
	if err := x.Send(ctx, mrp.SCOpcodeSigma2Resume, sigma.MarshalSigma2Resume(*result.Sigma2Resume), exchange.SendOptions{ExpectedProcessingTime: ExpectedCryptoProcessingTime}); err != nil {
		return err
	}
	keys := result.ResumeKeys
	s, err := c.mgr.NewSecureSession(exchange.SecureSessionConfig{
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
	})
	if err != nil {
		return err
	}
	if _, err := nextMessage(ctx, x, mrp.SCOpcodeStatusReport, ExpectedCryptoProcessingTime); err != nil {
		s.Close()
		return err
	}
	rec := *record
	rec.ResumptionID = append([]byte(nil), result.Sigma2Resume.ResumptionID...)
	c.established(s, fabric, rec)
	return nil
}

// full is CaseServer.#generateSigma2 (CaseServer.ts:224-347) around
// secure/sigma's crypto: Sigma2 sent (:241-273), Sigma3 read and verified
// (:276-309; a fabric id mismatch is UnexpectedData, :305-307, an
// InvalidParam), the session registered (:311-328), Success sent (:332),
// the record saved (:334-344). A Success that cannot be sent closes the
// session: matter.js keeps it (:316-332), a divergence the report names.
func (c *CASEServer) full(ctx context.Context, x *exchange.Exchange, responder *sigma.Responder, fabrics *responderFabrics, sigma1 sigma.Sigma1, result sigma.Sigma1ProcessResult) error {
	fabric := fabrics.chosen
	if err := x.Send(ctx, mrp.SCOpcodeSigma2, result.Sigma2.Marshal(), exchange.SendOptions{ExpectedProcessingTime: ExpectedCryptoProcessingTime}); err != nil {
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
	s, err := c.mgr.NewSecureSession(exchange.SecureSessionConfig{
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
	})
	if err != nil {
		return err
	}
	if err := c.sendStatus(ctx, x, mrp.SCStatusGeneralSuccess, mrp.SCStatusProtocolSessionEstablishmentSuccess); err != nil {
		s.Close()
		return err
	}
	c.established(s, fabric, ResumptionRecord{
		Peer:                  PeerAddress{FabricIndex: fabric.Identity.FabricIndex, NodeID: peerNodeID},
		SharedSecret:          responder.ECDHSharedSecret(),
		ResumptionID:          responder.ResumptionID(),
		SessionParameters:     s.Parameters(),
		CaseAuthenticatedTags: cats,
		PeerCATs:              cats,
	})
	return nil
}

// established hands the session over and saves its record — the save of
// CaseServer.ts:218-219 and :343-344, a failure only logged as the CASE
// client logs it (CaseClient.ts:369-381).
func (c *CASEServer) established(s *exchange.Session, fabric Fabric, rec ResumptionRecord) {
	c.log.Info("matter.controller.case_server.session",
		slog.Int("session_id", int(s.ID())), slog.Bool("resumed", s.Resumed()),
		slog.Int("fabric_index", int(fabric.Identity.FabricIndex)), slog.Uint64("node_id", s.PeerNodeID()))
	if c.cfg.Resumption != nil {
		if err := c.cfg.Resumption.SaveResumptionRecord(rec); err != nil {
			c.log.Warn("matter.controller.case_server.resumption_save_failed", slog.Any("err", err))
		}
	}
	if c.cfg.OnSession != nil {
		c.cfg.OnSession(s, fabric.Identity.FabricIndex)
	}
}

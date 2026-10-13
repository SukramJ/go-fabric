// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package handshake

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"time"

	"github.com/SukramJ/go-fabric/controller/exchange"
	"github.com/SukramJ/go-fabric/secure/channel"
	"github.com/SukramJ/go-fabric/secure/mattercert"
	"github.com/SukramJ/go-fabric/secure/sessionparams"
	"github.com/SukramJ/go-fabric/secure/sigma"
	"github.com/SukramJ/go-fabric/transport/mrp"
)

// Fabric is our side of a fabric for CASE: the operational identity (NOC,
// ICAC, key, node id, fabric id, fabric index and the operational IPK,
// Identity.IPK), the fabric's root public key — an input of the
// destination id — and the verifier of the peer's NOC chain.
type Fabric struct {
	// Identity is our operational identity on the fabric.
	Identity *sigma.Identity
	// RootPublicKey is the 65-byte uncompressed root public key.
	RootPublicKey []byte
	// Verifier checks the peer's NOC chain against the root.
	Verifier *mattercert.Verifier
}

// CASEOptions configures [CASE] — matter.js CaseClient.PairOptions
// (CaseClient.ts:393-401).
type CASEOptions struct {
	// PeerParameters is what is known of the peer's session parameters
	// before the handshake (its operational DNS-SD record), nil for
	// nothing.
	PeerParameters *sessionparams.Parameters
	// CaseAuthenticatedTags are the session's CATs; nil takes the
	// resumption record's (CaseClient.ts:333-334).
	CaseAuthenticatedTags []uint32
	// Resumption holds the resumption records; nil neither offers nor
	// saves one.
	Resumption ResumptionStore
	// ExpectedProcessingTime is the peer's processing time of each step;
	// 0 is [ExpectedCryptoProcessingTime] (CaseClient.ts:64).
	ExpectedProcessingTime time.Duration
	// MaxInitialRetransmissions, MaxInitialRetransmissionTime and
	// InitialRetransmissionTime apply to Sigma1 only (CaseClient.ts:60-80,
	// :145, :159-160), as exchange.SendOptions MaxRetransmissions,
	// MaxRetransmissionTime and InitialRetransmissionTime.
	MaxInitialRetransmissions    int
	MaxInitialRetransmissionTime time.Duration
	InitialRetransmissionTime    time.Duration
	// Logger receives the warnings and debug diagnostics; nil discards them.
	Logger *slog.Logger
}

// CASE establishes a CASE session with node peerNodeID of fabric at addr,
// on a fresh exchange over a new unsecured session of mgr, and returns
// the secure session, registered with mgr, and whether it was resumed.
// Mirrors matter.js packages/protocol/src/session/case/CaseClient.ts:54-382
// (pair, #doPair) at 0ceb202d:
//
//  1. a free session id; with a resumption record for (fabric, peer)
//     Sigma1 offers resumption, otherwise it is the plain Sigma1, the
//     destination id the fabric's for the peer and the random
//     (:118-164) — secure/sigma's peer initiator builds both;
//  2. Sigma2Resume (:168-233): refused without a record; the peer's
//     parameters are the unsecured session's overlaid by the record's
//     and by the responder's; the resume MIC is checked; the session
//     salt uses the old resumption id and the record takes the new one;
//     Success is sent and a failure to send it only logged;
//  3. Sigma2 (:234-367): the responder's parameters become the unsecured
//     session's timing; the NOC must name the expected node on our
//     fabric and its chain verify (an ICAC's fabric must match the NOC's,
//     mattercert's chain check, so the fabric's); Sigma3 and the Success
//     status; the session's CATs are the option's or the record's; a new
//     record with the responder's resumption id;
//  4. the record is saved after the session exists, a failure only
//     logged (:369-381).
//
// On an error InvalidParam is sent unless ctx was cancelled, our last
// message is unacknowledged, or the error is a network, transient or
// retransmission-limit one (:83-99).
func CASE(ctx context.Context, mgr *exchange.Manager, addr *net.UDPAddr, fabric Fabric, peerNodeID uint64, opts CASEOptions) (*exchange.Session, bool, error) {
	log := opts.Logger
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	if fabric.Identity == nil || fabric.Verifier == nil {
		return nil, false, errors.New("handshake: CASE needs an identity and a verifier")
	}
	us, err := mgr.NewUnsecuredSession(addr, opts.PeerParameters)
	if err != nil {
		return nil, false, err
	}
	defer us.Close()
	x, err := mgr.Initiate(us, mrp.SecureChannelProtocolID, exchange.Options{})
	if err != nil {
		return nil, false, err
	}
	c := &caseClient{mgr: mgr, us: us, x: x, fabric: fabric, peer: peerNodeID, opts: opts, log: log}
	s, rec, resumed, err := c.pair(ctx)
	if err != nil {
		if ctx.Err() == nil && reportsFailureToPeer(x, err) {
			if serr := sendStatus(context.WithoutCancel(ctx), x, mrp.SCStatusGeneralFailure, statusInvalidParam); serr != nil {
				log.Debug("matter.controller.case.invalid_param_failed", slog.String("err", serr.Error()))
			}
		}
		x.Close()
		return nil, false, err
	}
	x.Close()
	if opts.Resumption != nil {
		if err := opts.Resumption.SaveResumptionRecord(rec); err != nil {
			log.Warn("matter.controller.case.resumption_save_failed", slog.String("err", err.Error()))
		}
	}
	return s, resumed, nil
}

// caseClient is one CASE handshake.
type caseClient struct {
	mgr    *exchange.Manager
	us     *exchange.Session
	x      *exchange.Exchange
	fabric Fabric
	peer   uint64
	opts   CASEOptions
	log    *slog.Logger
}

func (c *caseClient) ept() time.Duration {
	if c.opts.ExpectedProcessingTime > 0 {
		return c.opts.ExpectedProcessingTime
	}
	return ExpectedCryptoProcessingTime
}

// pair is #doPair up to the record save.
func (c *caseClient) pair(ctx context.Context) (*exchange.Session, ResumptionRecord, bool, error) {
	sessionID, err := c.mgr.AllocateSessionID(ctx)
	if err != nil {
		return nil, ResumptionRecord{}, false, err
	}
	address := PeerAddress{FabricIndex: c.fabric.Identity.FabricIndex, NodeID: c.peer}
	var record *ResumptionRecord
	if c.opts.Resumption != nil {
		if rec, ok := c.opts.Resumption.FindResumptionRecord(address); ok {
			record = &rec
		}
	}
	local := c.mgr.LocalParameters()
	cfg := sigma.InitiatorConfig{
		Identity:      c.fabric.Identity,
		Verifier:      c.fabric.Verifier,
		SessionID:     sessionID,
		PeerNodeID:    c.peer,
		RootPublicKey: c.fabric.RootPublicKey,
		SessionParams: &local,
	}
	if record != nil {
		cfg.Resumption = &sigma.ResumptionRecord{
			SharedSecret: record.SharedSecret,
			ResumptionID: record.ResumptionID,
			FabricIndex:  record.Peer.FabricIndex,
			PeerNodeID:   record.Peer.NodeID,
			PeerCATs:     record.CaseAuthenticatedTags,
		}
	}
	init, err := sigma.NewPeerInitiator(cfg)
	if err != nil {
		return nil, ResumptionRecord{}, false, err
	}
	sigma1, err := init.GenerateSigma1()
	if err != nil {
		return nil, ResumptionRecord{}, false, err
	}
	if err := c.x.Send(ctx, mrp.SCOpcodeSigma1, sigma1, exchange.SendOptions{
		ExpectedProcessingTime:    c.ept(),
		MaxRetransmissions:        c.opts.MaxInitialRetransmissions,
		MaxRetransmissionTime:     c.opts.MaxInitialRetransmissionTime,
		InitialRetransmissionTime: c.opts.InitialRetransmissionTime,
	}); err != nil {
		return nil, ResumptionRecord{}, false, err
	}
	msg, err := c.x.Next(ctx, exchange.ReceiveOptions{ExpectedProcessingTime: c.ept()})
	if err != nil {
		return nil, ResumptionRecord{}, false, err
	}
	if err := checkStatus(msg); err != nil {
		return nil, ResumptionRecord{}, false, err
	}
	switch msg.Opcode {
	case mrp.SCOpcodeSigma2Resume:
		s, rec, err := c.resume(ctx, init, record, sessionID, msg.Payload)
		return s, rec, true, err
	case mrp.SCOpcodeSigma2:
		s, rec, err := c.full(ctx, init, record, sessionID, msg.Payload)
		return s, rec, false, err
	default:
		// CaseMessenger.ts:46-59 (readSigma2).
		return nil, ResumptionRecord{}, false, fmt.Errorf("%w: message type 0x%02X while expecting Sigma2 or Sigma2Resume", ErrUnexpectedData, msg.Opcode)
	}
}

// resume is the Sigma2Resume branch, CaseClient.ts:168-233.
func (c *caseClient) resume(ctx context.Context, init *sigma.Initiator, record *ResumptionRecord, sessionID uint16, raw []byte) (*exchange.Session, ResumptionRecord, error) {
	if record == nil {
		return nil, ResumptionRecord{}, sigma.ErrUnexpectedSigma2Resume
	}
	if err := init.ProcessSigma2Resume(raw); err != nil {
		return nil, ResumptionRecord{}, err
	}
	res, _ := init.Result()
	// Sigma2Resume's parameters are more current than the record's
	// (:181-188).
	recordParams := record.SessionParameters
	params := exchange.PeerParameters(ptr(exchange.Overlay(exchange.Overlay(c.us.Parameters(), &recordParams), res.PeerSessionParams)))
	cats := record.CaseAuthenticatedTags
	s, err := c.mgr.NewSecureSession(exchange.SecureSessionConfig{
		ID:   sessionID,
		Addr: c.us.Addr(),
		Channel: channel.Config{
			EncryptKey:    res.Keys.I2RKey[:],
			DecryptKey:    res.Keys.R2IKey[:],
			LocalNodeID:   c.fabric.Identity.NodeID,
			PeerNodeID:    c.peer,
			PeerCATs:      cats,
			PeerSessionID: res.PeerSessionID,
		},
		PeerParameters:        params,
		ReportedParameters:    res.PeerSessionParams,
		CaseAuthenticatedTags: cats,
		AttestationChallenge:  res.Keys.AttestationChallenge[:],
		Resumed:               true,
	})
	if err != nil {
		return nil, ResumptionRecord{}, err
	}
	rec := *record
	rec.ResumptionID = res.ResumptionID
	rec.SessionParameters = s.Parameters()
	// The session is registered before Success goes out; a failure to
	// send Success leaves it valid (:220-233).
	if err := sendStatus(context.WithoutCancel(ctx), c.x, mrp.SCStatusGeneralSuccess, mrp.SCStatusProtocolSessionEstablishmentSuccess); err != nil {
		c.log.Info("matter.controller.case.resume_success_failed", slog.String("err", err.Error()))
	}
	return s, rec, nil
}

// full is the Sigma2 branch, CaseClient.ts:234-367.
func (c *caseClient) full(ctx context.Context, init *sigma.Initiator, record *ResumptionRecord, sessionID uint16, raw []byte) (*exchange.Session, ResumptionRecord, error) {
	sigma2, err := sigma.UnmarshalSigma2(raw)
	if err != nil {
		return nil, ResumptionRecord{}, fmt.Errorf("%w: malformed Sigma2: %w", ErrUnexpectedData, err)
	}
	if sigma2.SessionParams != nil {
		c.us.SetTimingParameters(*sigma2.SessionParams)
	}
	params := exchange.PeerParameters(ptr(exchange.Overlay(c.us.Parameters(), sigma2.SessionParams)))
	sigma3, err := init.ProcessSigma2Bytes(raw)
	if err != nil {
		return nil, ResumptionRecord{}, err
	}
	if err := c.x.Send(ctx, mrp.SCOpcodeSigma3, sigma3, exchange.SendOptions{ExpectedProcessingTime: c.ept()}); err != nil {
		return nil, ResumptionRecord{}, err
	}
	if _, err := nextMessage(ctx, c.x, mrp.SCOpcodeStatusReport, c.ept()); err != nil {
		return nil, ResumptionRecord{}, err
	}
	cats := c.opts.CaseAuthenticatedTags
	if cats == nil && record != nil {
		cats = record.CaseAuthenticatedTags
	}
	res, _ := init.Result()
	s, err := c.mgr.NewSecureSession(exchange.SecureSessionConfig{
		ID:   sessionID,
		Addr: c.us.Addr(),
		Channel: channel.Config{
			EncryptKey:    res.Keys.I2RKey[:],
			DecryptKey:    res.Keys.R2IKey[:],
			LocalNodeID:   c.fabric.Identity.NodeID,
			PeerNodeID:    c.peer,
			PeerCATs:      cats,
			PeerSessionID: res.PeerSessionID,
		},
		PeerParameters:        params,
		ReportedParameters:    sigma2.SessionParams,
		CaseAuthenticatedTags: cats,
		AttestationChallenge:  res.Keys.AttestationChallenge[:],
	})
	if err != nil {
		return nil, ResumptionRecord{}, err
	}
	return s, ResumptionRecord{
		Peer:                  PeerAddress{FabricIndex: c.fabric.Identity.FabricIndex, NodeID: c.peer},
		SharedSecret:          res.SharedSecret,
		ResumptionID:          res.ResumptionID,
		SessionParameters:     s.Parameters(),
		CaseAuthenticatedTags: cats,
	}, nil
}

func ptr[T any](v T) *T { return &v }

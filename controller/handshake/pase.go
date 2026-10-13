// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package handshake

import (
	"context"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"log/slog"
	"net"

	"github.com/SukramJ/go-fabric/controller/exchange"
	"github.com/SukramJ/go-fabric/secure/channel"
	"github.com/SukramJ/go-fabric/secure/sessionparams"
	"github.com/SukramJ/go-fabric/secure/spake2"
	"github.com/SukramJ/go-fabric/tlv"
	"github.com/SukramJ/go-fabric/transport/mrp"
)

// DefaultPasscodeID is the passcode id of the PBKDFParamRequest — matter.js
// packages/protocol/src/session/pase/PaseMessenger.ts:24 (DEFAULT_PASSCODE_ID).
const DefaultPasscodeID uint16 = 0

// sessionKeysInfo is the HKDF info of the session keys — matter.js
// session/NodeSession.ts:45 (SESSION_KEYS_INFO).
const sessionKeysInfo = "SessionKeys"

// PASEOptions configures [PASE].
type PASEOptions struct {
	// PeerParameters is what is known of the device's session parameters
	// before the handshake (its commissionable DNS-SD record), nil for
	// nothing: the timing of the unsecured session until the
	// PBKDFParamResponse reports the device's own.
	PeerParameters *sessionparams.Parameters
	// SuppressPeerLoss keeps the PASE session open when one of its
	// exchanges fails — the commissioning case (matter.js
	// PaseClient.PairOptions.suppressPeerLoss, PaseClient.ts:223-224).
	SuppressPeerLoss bool
	// OnPBKDFParamResponse is called once the device answered the
	// PBKDFParamRequest (PaseClient.ts:156, :226-227).
	OnPBKDFParamResponse func()
	// Logger receives debug diagnostics; nil discards them.
	Logger *slog.Logger
}

// PASE establishes a PASE session with the device at addr using passcode,
// on a fresh exchange over a new unsecured session of mgr, and returns
// the secure session, registered with mgr. Mirrors matter.js
// packages/protocol/src/session/pase/PaseClient.ts:73-216 (pair, #doPair)
// at 0ceb202d:
//
//  1. a 32-byte random and a free session id (:136-137);
//     PBKDFParamRequest with passcode id 0, hasPbkdfParameters false and
//     our session parameters (:139-151);
//  2. the PBKDFParamResponse must carry the PBKDF parameters (:158-160);
//     the device's session parameters become the unsecured session's
//     timing (:162-165), and the peer's parameters are the session's
//     overlaid by them (:167-171);
//  3. SPAKE2+ over SHA-256("CHIP PAKE V1 Commissioning" ‖ request ‖
//     response) (:174-178, PaseMessenger.ts:25): Pake1, Pake2 — a key
//     confirmation that does not verify is [ErrPasscodeMismatch]
//     (:186-190) — then Pake3, sent and awaited without regard to ctx
//     because the device has committed its state (:192-197);
//  4. the secure session: no fabric, peer node id unspecified, the
//     device's session id, Ke as the shared secret with an empty salt
//     (:199-212; the key split of NodeSession.ts:78-88).
//
// On an error after which the device can be told — our last message
// acknowledged, the error not a network, transient or
// retransmission-limit one — a StatusReport InvalidParam goes out even
// when ctx was cancelled, so the device does not stay locked for 60 s;
// a failure to send it is ignored (:94-115).
func PASE(ctx context.Context, mgr *exchange.Manager, addr *net.UDPAddr, passcode uint32, opts PASEOptions) (*exchange.Session, error) {
	log := opts.Logger
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	us, err := mgr.NewUnsecuredSession(addr, opts.PeerParameters)
	if err != nil {
		return nil, err
	}
	defer us.Close()
	x, err := mgr.Initiate(us, mrp.SecureChannelProtocolID, exchange.Options{})
	if err != nil {
		return nil, err
	}
	s, err := pairPASE(ctx, mgr, us, x, passcode, opts)
	if err != nil && reportsFailureToPeer(x, err) {
		if serr := sendStatus(context.WithoutCancel(ctx), x, mrp.SCStatusGeneralFailure, statusInvalidParam); serr != nil {
			log.Debug("matter.controller.pase.invalid_param_failed", slog.String("err", serr.Error()))
		}
	}
	x.Close()
	return s, err
}

// pairPASE is #doPair.
func pairPASE(ctx context.Context, mgr *exchange.Manager, us *exchange.Session, x *exchange.Exchange, passcode uint32, opts PASEOptions) (*exchange.Session, error) {
	var random [spake2.PBKDFRandomSize]byte
	if _, err := rand.Read(random[:]); err != nil {
		return nil, fmt.Errorf("handshake: random: %w", err)
	}
	sessionID, err := mgr.AllocateSessionID(ctx)
	if err != nil {
		return nil, err
	}
	local := mgr.LocalParameters()
	req := encodePBKDFParamRequest(random[:], sessionID, &local)
	if err := x.Send(ctx, mrp.SCOpcodePBKDFParamRequest, req, exchange.SendOptions{ExpectedProcessingTime: DefaultNormalProcessingTime}); err != nil {
		return nil, err
	}
	msg, err := nextMessage(ctx, x, mrp.SCOpcodePBKDFParamResponse, DefaultNormalProcessingTime)
	if err != nil {
		return nil, err
	}
	respBytes := msg.Payload
	resp, err := spake2.DecodePBKDFParamResponse(respBytes)
	if err != nil {
		return nil, fmt.Errorf("%w: malformed PBKDFParamResponse: %w", ErrUnexpectedData, err)
	}
	if opts.OnPBKDFParamResponse != nil {
		opts.OnPBKDFParamResponse()
	}
	if resp.Parameters == nil {
		return nil, fmt.Errorf("%w: missing requested PbkdfParameters in the response", ErrUnexpectedData)
	}
	if resp.ResponderSessionParams != nil {
		us.SetTimingParameters(*resp.ResponderSessionParams)
	}
	peerParams := exchange.Overlay(us.Parameters(), resp.ResponderSessionParams)

	h := sha256.New()
	h.Write([]byte(spake2.MatterContext))
	h.Write(req)
	h.Write(respBytes)
	prover, err := spake2.NewProver(passcode, resp.Parameters.Salt, int(resp.Parameters.Iterations), nil, nil, h.Sum(nil))
	if err != nil {
		return nil, fmt.Errorf("handshake: SPAKE2+ prover: %w", err)
	}
	pA, err := prover.GeneratePake1()
	if err != nil {
		return nil, fmt.Errorf("handshake: Pake1: %w", err)
	}
	if err := x.Send(ctx, mrp.SCOpcodePake1, spake2.EncodePake1(pA), exchange.SendOptions{ExpectedProcessingTime: ExpectedCryptoProcessingTime}); err != nil {
		return nil, err
	}
	msg, err = nextMessage(ctx, x, mrp.SCOpcodePake2, ExpectedCryptoProcessingTime)
	if err != nil {
		return nil, err
	}
	pB, cB, err := spake2.DecodePake2(msg.Payload)
	if err != nil {
		return nil, fmt.Errorf("%w: malformed Pake2: %w", ErrUnexpectedData, err)
	}
	cA, err := prover.ProcessPake2(pB, cB)
	switch {
	case errors.Is(err, spake2.ErrConfirmationFailed):
		return nil, ErrPasscodeMismatch
	case err != nil:
		return nil, fmt.Errorf("%w: Pake2: %w", ErrUnexpectedData, err)
	}

	// From here on the device has committed its PASE state: ctx no longer
	// cancels (PaseClient.ts:192-197).
	committed := context.WithoutCancel(ctx)
	if err := x.Send(committed, mrp.SCOpcodePake3, spake2.EncodePake3(cA), exchange.SendOptions{ExpectedProcessingTime: ExpectedCryptoProcessingTime}); err != nil {
		return nil, err
	}
	if _, err := nextMessage(committed, x, mrp.SCOpcodeStatusReport, ExpectedCryptoProcessingTime); err != nil {
		return nil, err
	}

	keys, err := hkdf.Key(sha256.New, prover.SharedSecret(), nil, sessionKeysInfo, 48)
	if err != nil {
		return nil, fmt.Errorf("handshake: session keys: %w", err)
	}
	return mgr.NewSecureSession(exchange.SecureSessionConfig{
		ID:   sessionID,
		Addr: us.Addr(),
		Channel: channel.Config{
			EncryptKey:    keys[0:16],
			DecryptKey:    keys[16:32],
			PeerSessionID: resp.ResponderSessionID,
		},
		PeerParameters:       peerParams,
		ReportedParameters:   resp.ResponderSessionParams,
		AttestationChallenge: keys[32:48],
		SuppressPeerLoss:     opts.SuppressPeerLoss,
	})
}

// encodePBKDFParamRequest is the PBKDFParamRequest TLV of matter.js
// packages/protocol/src/session/pase/PaseMessages.ts:68-75
// (TlvPbkdfParamRequest): [1] initiatorRandom, [2] initiatorSessionId,
// [3] passcodeId, [4] hasPbkdfParameters, [5] initiatorSessionParams.
func encodePBKDFParamRequest(random []byte, sessionID uint16, params *sessionparams.Parameters) []byte {
	enc := tlv.NewEncoder()
	enc.StartStruct(tlv.AnonymousTag())
	enc.PutOctets(tlv.ContextTag(1), random)
	enc.PutUint(tlv.ContextTag(2), uint64(sessionID))
	enc.PutUint(tlv.ContextTag(3), uint64(DefaultPasscodeID))
	enc.PutBool(tlv.ContextTag(4), false)
	if params != nil && !params.IsEmpty() {
		params.Encode(enc, tlv.ContextTag(5))
	}
	_ = enc.EndContainer()
	out, _ := enc.Bytes()
	return out
}

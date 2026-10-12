// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package bridgetest

// The session handshakes: PASE as the initiator, CASE as the initiator and
// as the responder (a device re-establishing a former subscription opens
// CASE towards its controller, docs/adr/0008).

import (
	"context"
	"crypto/hkdf"
	"crypto/sha256"
	"errors"
	"fmt"
	"net"

	"github.com/SukramJ/go-fabric/secure/channel"
	"github.com/SukramJ/go-fabric/secure/mattercert"
	"github.com/SukramJ/go-fabric/secure/sigma"
	"github.com/SukramJ/go-fabric/secure/spake2"
	"github.com/SukramJ/go-fabric/tlv"
	"github.com/SukramJ/go-fabric/transport/message"
	"github.com/SukramJ/go-fabric/transport/mrp"
)

// ErrHandshake is returned when the device ends a PASE or CASE handshake
// with an error StatusReport or answers a step with the wrong message.
var ErrHandshake = errors.New("bridgetest: session handshake failed")

// handshakeStep sends one handshake message on the unsecured exchange xid
// and returns the device's answer, which must carry opcode want.
func (c *Commissioner) handshakeStep(ctx context.Context, to *net.UDPAddr, xid uint16, ch <-chan inbound, opcode uint8, payload []byte, want uint8) ([]byte, error) {
	proto := message.ProtocolHeader{Initiator: true, NeedsAck: true, Opcode: opcode, ExchangeID: xid, ProtocolID: mrp.SecureChannelProtocolID}
	c.sendUnsecured(to, proto, payload, 0)
	msg, err := await(ctx, ch)
	if err != nil {
		return nil, err
	}
	if msg.proto.Opcode != want {
		return nil, fmt.Errorf("%w: opcode 0x%02X after 0x%02X, want 0x%02X (payload %x)", ErrHandshake, msg.proto.Opcode, opcode, want, msg.payload)
	}
	if want == mrp.SCOpcodeStatusReport {
		if general, _, code, _ := decodeStatusReport(msg.payload); general != mrp.SCStatusGeneralSuccess || code != mrp.SCStatusProtocolSessionEstablishmentSuccess {
			return nil, fmt.Errorf("%w: StatusReport general 0x%04X protocol 0x%04X", ErrHandshake, general, code)
		}
	}
	return msg.payload, nil
}

// PASE establishes a PASE session with the passcode of [Options] over the
// device's commissioning window — the commissioner's kSecurePairing stage
// (connectedhomeip src/controller/CommissioningDelegate.h:48).
//
// The steps are the client half of the PASE exchange the device's
// responder serves: PBKDFParamRequest {initiatorRandom, initiatorSessionId,
// passcodeId 0, hasPBKDFParameters false}, Pake1-3 over a SPAKE2+ prover
// keyed by the "CHIP PAKE V1 Commissioning" transcript, and the session
// keys HKDF(Ke, "SessionKeys") as I2R | R2I | AttestationChallenge — moved
// from the node package's PASE test client (node/client_test.go, wave 6).
func (c *Commissioner) PASE(ctx context.Context) (*Session, error) {
	ctx, cancel := withDefaultTimeout(ctx)
	defer cancel()
	to, err := c.commissionableAddr()
	if err != nil {
		return nil, err
	}
	local := c.allocSession()
	xid, ch, release := c.openExchange(0)
	defer release()

	enc := tlv.NewEncoder()
	enc.StartStruct(tlv.AnonymousTag())
	enc.PutOctets(tlv.ContextTag(1), c.nonce(spake2.PBKDFRandomSize))
	enc.PutUint(tlv.ContextTag(2), uint64(local))
	enc.PutUint(tlv.ContextTag(3), 0)
	enc.PutBool(tlv.ContextTag(4), false)
	_ = enc.EndContainer()
	req := mustBytes(enc)
	respBytes, err := c.handshakeStep(ctx, to, xid, ch, mrp.SCOpcodePBKDFParamRequest, req, mrp.SCOpcodePBKDFParamResponse)
	if err != nil {
		return nil, err
	}
	resp, err := spake2.DecodePBKDFParamResponse(respBytes)
	if err != nil {
		return nil, fmt.Errorf("%w: PBKDFParamResponse: %w", ErrHandshake, err)
	}
	if resp.Parameters == nil {
		return nil, fmt.Errorf("%w: PBKDFParamResponse without PBKDF parameters", ErrHandshake)
	}
	h := sha256.New()
	h.Write([]byte("CHIP PAKE V1 Commissioning"))
	h.Write(req)
	h.Write(respBytes)
	prover, err := spake2.NewProver(c.opts.Passcode, resp.Parameters.Salt, int(resp.Parameters.Iterations), nil, nil, h.Sum(nil))
	if err != nil {
		return nil, fmt.Errorf("%w: SPAKE2+ prover: %w", ErrHandshake, err)
	}
	pA, err := prover.GeneratePake1()
	mustNot(err)
	pake2, err := c.handshakeStep(ctx, to, xid, ch, mrp.SCOpcodePake1, spake2.EncodePake1(pA), mrp.SCOpcodePake2)
	if err != nil {
		return nil, err
	}
	pB, cB, err := spake2.DecodePake2(pake2)
	if err != nil {
		return nil, fmt.Errorf("%w: Pake2: %w", ErrHandshake, err)
	}
	cA, err := prover.ProcessPake2(pB, cB)
	if err != nil {
		return nil, fmt.Errorf("%w: Pake2: %w", ErrHandshake, err)
	}
	if _, err := c.handshakeStep(ctx, to, xid, ch, mrp.SCOpcodePake3, spake2.EncodePake3(cA), mrp.SCOpcodeStatusReport); err != nil {
		return nil, err
	}
	keys, err := hkdf.Key(sha256.New, prover.SharedSecret(), nil, "SessionKeys", 48)
	mustNot(err)
	s := c.newSession(local, resp.ResponderSessionID, to, channel.Config{EncryptKey: keys[0:16], DecryptKey: keys[16:32]})
	s.challenge = keys[32:48]
	return s, nil
}

// caseIdentity is one controller's operational identity on the fabric.
type caseIdentity struct {
	sigma.Identity
	cats []uint32
}

// identity returns the operational identity of controller nodeID, minting
// its key and NOC on first use.
func (c *Commissioner) identity(nodeID uint64, cats []uint32) *caseIdentity {
	c.mu.Lock()
	defer c.mu.Unlock()
	if id, ok := c.identities[nodeID]; ok {
		return id
	}
	key := deriveKey(c.opts.Seed, "controller", nodeID)
	ipk, err := sigma.DeriveOperationalIPK(c.ca.ipk, c.ca.compressedFabricID())
	mustNot(err)
	id := &caseIdentity{
		Identity: sigma.Identity{
			NOC:                c.ca.noc(publicKeyBytes(&key.PublicKey), nodeID, cats),
			PrivateKey:         key,
			NodeID:             nodeID,
			FabricID:           c.opts.FabricID,
			CompressedFabricID: c.ca.compressedFabricID(),
			IPK:                ipk,
		},
		cats: append([]uint32(nil), cats...),
	}
	c.identities[nodeID] = id
	return id
}

// verifier checks a peer NOC against the commissioner's root.
func (c *Commissioner) verifier() *mattercert.Verifier {
	v, err := mattercert.NewVerifier(c.ca.rootPub, mattercert.SystemTime{})
	mustNot(err)
	return v
}

// caseAs opens a CASE session to the device as controller nodeID — the
// commissioner's kFindOperationalForCommissioningComplete stage
// (CommissioningDelegate.h:84-85; CHIPDeviceController.cpp:4488-4497
// FindOrEstablishSession), driven by the module's own initiator
// (sigma.NewPeerInitiator, which binds the responder's NOC to the node id
// and fabric it set out to reach).
func (c *Commissioner) caseAs(ctx context.Context, nodeID uint64, cats []uint32) (*Session, error) {
	ctx, cancel := withDefaultTimeout(ctx)
	defer cancel()
	to, err := c.operationalAddr()
	if err != nil {
		return nil, err
	}
	id := c.identity(nodeID, cats)
	local := c.allocSession()
	init, err := sigma.NewPeerInitiator(sigma.InitiatorConfig{
		Identity:      &id.Identity,
		Verifier:      c.verifier(),
		SessionID:     local,
		PeerNodeID:    c.opts.NodeID,
		RootPublicKey: c.ca.rootPub,
	})
	mustNot(err)
	sigma1, err := init.GenerateSigma1()
	mustNot(err)
	xid, ch, release := c.openExchange(0)
	defer release()
	sigma2, err := c.handshakeStep(ctx, to, xid, ch, mrp.SCOpcodeSigma1, sigma1, mrp.SCOpcodeSigma2)
	if err != nil {
		return nil, err
	}
	sigma3, err := init.ProcessSigma2Bytes(sigma2)
	if err != nil {
		return nil, fmt.Errorf("%w: Sigma2: %w", ErrHandshake, err)
	}
	if _, err := c.handshakeStep(ctx, to, xid, ch, mrp.SCOpcodeSigma3, sigma3, mrp.SCOpcodeStatusReport); err != nil {
		return nil, err
	}
	res, _ := init.Result()
	return c.newSession(local, res.PeerSessionID, to, channel.Config{
		EncryptKey:  res.Keys.I2RKey[:],
		DecryptKey:  res.Keys.R2IKey[:],
		LocalNodeID: nodeID,
		PeerNodeID:  res.PeerNodeID,
		PeerCATs:    res.PeerCATs,
	}), nil
}

// ResolveSigma1Destination implements [sigma.IdentityResolver] for the
// responder: the controller identity the device's Sigma1 addresses.
func (c *Commissioner) ResolveSigma1Destination(destinationID [32]byte, initiatorRandom [sigma.RandomSize]byte) (*sigma.Identity, sigma.PeerVerifier, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, id := range c.identities {
		if sigma.ComputeDestinationID(id.IPK, initiatorRandom, c.ca.rootPub, id.FabricID, id.NodeID) == destinationID {
			return &id.Identity, c.verifier(), true
		}
	}
	return nil, nil, false
}

// respondCASE answers a CASE handshake the device opens: Sigma1 with
// Sigma2, Sigma3 with a success StatusReport, and the established session
// is queued for [Commissioner.Accept]. Resumption is not offered — the
// responder holds no resumption record, so a resuming Sigma1 gets the
// full handshake (secure/sigma/protocol.go:473-525).
func (c *Commissioner) respondCASE(msg inbound) {
	if msg.proto.ProtocolID != mrp.SecureChannelProtocolID {
		return
	}
	c.mu.Lock()
	resp := c.responders[msg.proto.ExchangeID]
	c.mu.Unlock()
	reply := message.ProtocolHeader{NeedsAck: true, ExchangeID: msg.proto.ExchangeID, ProtocolID: mrp.SecureChannelProtocolID}
	switch {
	case msg.proto.Opcode == mrp.SCOpcodeSigma1 && resp == nil:
		local := c.allocSession()
		// The baseline identity is never used: the resolver picks the
		// controller the Sigma1 addresses, or the handshake fails.
		resp = sigma.NewResponder(&sigma.Identity{PrivateKey: deriveKey(c.opts.Seed, "unaddressed", 0)}, c.verifier(), local)
		resp.SetIdentityResolver(c)
		sigma2, err := resp.ProcessSigma1(msg.payload)
		if err != nil {
			return
		}
		c.mu.Lock()
		c.responders[msg.proto.ExchangeID] = resp
		c.mu.Unlock()
		reply.Opcode = mrp.SCOpcodeSigma2
		c.sendUnsecured(msg.from, reply, sigma2.Marshal(), msg.srcNode)
	case msg.proto.Opcode == mrp.SCOpcodeSigma3 && resp != nil:
		c.mu.Lock()
		delete(c.responders, msg.proto.ExchangeID)
		c.mu.Unlock()
		if resp.ProcessSigma3(msg.payload) != nil {
			return
		}
		keys, _ := resp.SessionKeys()
		_, localNode, _ := resp.SessionIdentity()
		s := c.newSession(resp.SessionID(), resp.PeerSessionID(), msg.from, channel.Config{
			EncryptKey:  keys.R2IKey[:],
			DecryptKey:  keys.I2RKey[:],
			LocalNodeID: localNode,
			PeerNodeID:  resp.PeerNodeID(),
			PeerCATs:    resp.PeerCATs(),
		})
		reply.Opcode = mrp.SCOpcodeStatusReport
		c.sendUnsecured(msg.from, reply, mrp.EncodeStatusReport(mrp.SCStatusGeneralSuccess, uint32(mrp.SecureChannelProtocolID), mrp.SCStatusProtocolSessionEstablishmentSuccess, nil), msg.srcNode)
		select {
		case c.accepted <- s:
		default:
		}
	}
}

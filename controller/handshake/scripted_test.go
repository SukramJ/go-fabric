// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package handshake_test

// Handshake failures a real node does not produce, against a scripted
// responder on the unsecured session.

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/bridge/bridgetest"
	"github.com/SukramJ/go-fabric/controller/exchange"
	"github.com/SukramJ/go-fabric/controller/handshake"
	"github.com/SukramJ/go-fabric/secure/mattercert"
	"github.com/SukramJ/go-fabric/secure/sigma"
	"github.com/SukramJ/go-fabric/secure/spake2"
	"github.com/SukramJ/go-fabric/transport/message"
	"github.com/SukramJ/go-fabric/transport/mrp"
)

// reply is one scripted answer: opcode and payload, acknowledging the
// message it answers.
type reply struct {
	opcode  uint8
	payload []byte
}

// scripted is a Conn whose far side answers each non-ack unsecured
// message with what script returns (nothing for ok false).
type scripted struct {
	m       *exchange.Manager
	script  func(opcode uint8, payload []byte) (reply, bool)
	mu      sync.Mutex
	counter uint32
	sent    []uint8
	reports []exchange.StatusReport
}

func (c *scripted) Send(_ *net.UDPAddr, d []byte) error {
	hdr, hl, err := message.UnmarshalHeader(d)
	if err != nil {
		return err
	}
	proto, pl, err := message.UnmarshalProtocolHeader(d[hl:])
	if err != nil {
		return err
	}
	if proto.Opcode == mrp.StandaloneAckOpcode {
		return nil
	}
	c.mu.Lock()
	c.sent = append(c.sent, proto.Opcode)
	if proto.Opcode == mrp.SCOpcodeStatusReport {
		if r, ok := exchange.DecodeStatusReport(d[hl+pl:]); ok {
			c.reports = append(c.reports, r)
		}
	}
	c.counter++
	counter := c.counter
	c.mu.Unlock()
	r, ok := c.script(proto.Opcode, d[hl+pl:])
	if !ok {
		// Acknowledge only.
		r = reply{opcode: mrp.StandaloneAckOpcode}
	}
	out := message.Header{MessageCounter: counter, DestSize: message.DestNodeID, DestNodeID: hdr.SourceNodeID}
	p := message.ProtocolHeader{
		NeedsAck: r.opcode != mrp.StandaloneAckOpcode, HasAck: true, AckCounter: hdr.MessageCounter,
		Opcode: r.opcode, ExchangeID: proto.ExchangeID, ProtocolID: mrp.SecureChannelProtocolID,
	}
	datagram := append(append(out.Marshal(), p.Marshal()...), r.payload...)
	go c.m.HandleDatagram(datagram, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 5540})
	return nil
}

func newScripted(t *testing.T, script func(opcode uint8, payload []byte) (reply, bool)) (*exchange.Manager, *scripted) {
	t.Helper()
	c := &scripted{script: script}
	m, err := exchange.NewManager(c, exchange.Config{})
	if err != nil {
		t.Fatal(err)
	}
	c.m = m
	t.Cleanup(m.Close)
	return m, c
}

func (c *scripted) invalidParams() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return invalidParams(c.reports)
}

var scriptAddr = &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 5540}

func pbkdfResponse(withParams bool) []byte {
	resp := spake2.PBKDFParamResponse{
		InitiatorRandom: make([]byte, 32), ResponderRandom: make([]byte, 32), ResponderSessionID: 0x4444,
	}
	if withParams {
		resp.Parameters = &spake2.PBKDFParameters{Iterations: 1000, Salt: []byte("SPAKE2P Key Salt")}
	}
	return resp.Marshal()
}

// TestPASEScriptedFailures: the PBKDF exchange's failure modes and which
// of them the device is told of with InvalidParam (PaseClient.ts:94-115,
// :158-160; SecureChannelMessenger.ts:83-100).
func TestPASEScriptedFailures(t *testing.T) {
	t.Parallel()
	failure := exchange.StatusReport{GeneralCode: mrp.SCStatusGeneralFailure, ProtocolCode: mrp.SCStatusProtocolInvalidParameter}.Encode()
	for _, tc := range []struct {
		name         string
		answer       reply
		want         error
		invalidParam int
	}{
		{"no PBKDF parameters", reply{mrp.SCOpcodePBKDFParamResponse, pbkdfResponse(false)}, handshake.ErrUnexpectedData, 1},
		{"malformed response", reply{mrp.SCOpcodePBKDFParamResponse, []byte{0x15, 0x18}}, handshake.ErrUnexpectedData, 1},
		{"wrong message", reply{mrp.SCOpcodePake2, nil}, handshake.ErrUnexpectedData, 1},
		{"error status", reply{mrp.SCOpcodeStatusReport, failure}, exchange.ErrTransient, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m, c := newScripted(t, func(opcode uint8, _ []byte) (reply, bool) {
				if opcode == mrp.SCOpcodePBKDFParamRequest {
					return tc.answer, true
				}
				return reply{}, false
			})
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			if _, err := handshake.PASE(ctx, m, scriptAddr, testPasscode, handshake.PASEOptions{}); !errors.Is(err, tc.want) {
				t.Fatalf("PASE = %v, want %v", err, tc.want)
			}
			if n := c.invalidParams(); n != tc.invalidParam {
				t.Fatalf("%d InvalidParam reports, want %d", n, tc.invalidParam)
			}
		})
	}
}

// TestPASEScriptedPake2: a malformed Pake2 and an invalid Y point are
// data errors the device is told of.
func TestPASEScriptedPake2(t *testing.T) {
	t.Parallel()
	for _, pake2 := range [][]byte{{0x15, 0x18}, spake2.EncodePake2(&spake2.Pake2Output{Y: make([]byte, 65), CB: make([]byte, 32)})} {
		m, c := newScripted(t, func(opcode uint8, _ []byte) (reply, bool) {
			switch opcode {
			case mrp.SCOpcodePBKDFParamRequest:
				return reply{mrp.SCOpcodePBKDFParamResponse, pbkdfResponse(true)}, true
			case mrp.SCOpcodePake1:
				return reply{mrp.SCOpcodePake2, pake2}, true
			}
			return reply{}, false
		})
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		_, err := handshake.PASE(ctx, m, scriptAddr, testPasscode, handshake.PASEOptions{})
		cancel()
		if !errors.Is(err, handshake.ErrUnexpectedData) || c.invalidParams() != 1 {
			t.Fatalf("PASE = %v with %d InvalidParam, want ErrUnexpectedData and one", err, c.invalidParams())
		}
	}
}

// TestCASEScriptedFailures: an unexpected Sigma2Resume (no record was
// offered, CaseClient.ts:168-172), a message that is neither Sigma2 nor
// Sigma2Resume (CaseMessenger.ts:55-58) and a malformed Sigma2 are data
// errors the peer is told of.
func TestCASEScriptedFailures(t *testing.T) {
	t.Parallel()
	_, fabric := commissionedFabricOffline(t)
	for _, tc := range []struct {
		name   string
		answer reply
		want   error
	}{
		{"unexpected Sigma2Resume", reply{mrp.SCOpcodeSigma2Resume, nil}, sigma.ErrUnexpectedSigma2Resume},
		{"wrong message", reply{mrp.SCOpcodePake2, nil}, handshake.ErrUnexpectedData},
		{"malformed Sigma2", reply{mrp.SCOpcodeSigma2, []byte{0x15, 0x18}}, handshake.ErrUnexpectedData},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m, c := newScripted(t, func(opcode uint8, _ []byte) (reply, bool) {
				if opcode == mrp.SCOpcodeSigma1 {
					return tc.answer, true
				}
				return reply{}, false
			})
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			_, _, err := handshake.CASE(ctx, m, scriptAddr, fabric, testNodeID, handshake.CASEOptions{ExpectedProcessingTime: time.Second})
			if !errors.Is(err, tc.want) {
				t.Fatalf("CASE = %v, want %v", err, tc.want)
			}
			if n := c.invalidParams(); n != 1 {
				t.Fatalf("%d InvalidParam reports, want 1", n)
			}
		})
	}
}

// commissionedFabricOffline is our side of the test commissioner's fabric
// without a node to talk to.
func commissionedFabricOffline(t *testing.T) (*bridgetest.Commissioner, handshake.Fabric) {
	t.Helper()
	c := bridgetest.Dial(t, "127.0.0.1:9", bridgetest.Options{Passcode: testPasscode, FabricID: testFabricID, NodeID: testNodeID, Seed: 7})
	id := c.ControllerIdentity(bridgetest.DefaultControllerNodeID)
	verifier, err := mattercert.NewVerifier(c.RootPublicKey(), mattercert.SystemTime{})
	if err != nil {
		t.Fatal(err)
	}
	return c, handshake.Fabric{Identity: &id, RootPublicKey: c.RootPublicKey(), Verifier: verifier}
}

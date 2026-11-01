// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/im"
)

// TestParityMatterJS_IMOnUnsecuredSessionIsDropped pins that no
// Interaction Model request is served on the unsecured session 0: Read,
// Write, Invoke, Subscribe and Timed each surface ErrUnsecuredIM and
// nothing goes back to the sender. matter.js
// packages/node/src/node/server/InteractionServer.ts:161 declares
// `requiresSecureSession = true`, and
// packages/protocol/src/protocol/ExchangeManager.ts:391-412 logs
// "Ignore … not matching the security requirements" and opens no
// exchange for the message.
func TestParityMatterJS_IMOnUnsecuredSessionIsDropped(t *testing.T) {
	t.Parallel()
	b := newStartedBridge(t)
	peerConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatalf("ListenUDP: %v", err)
	}
	t.Cleanup(func() { _ = peerConn.Close() })
	peerAddr, ok := peerConn.LocalAddr().(*net.UDPAddr)
	if !ok {
		t.Fatalf("unexpected peer addr type %T", peerConn.LocalAddr())
	}
	cases := []struct {
		name    string
		opcode  uint8
		payload []byte
	}{
		{"read", im.OpcodeReadRequest, buildIMReadRequestPayload(t)},
		{"subscribe", im.OpcodeSubscribeRequest, buildIMSubscribeRequestPayload(t)},
		{"timed", im.OpcodeTimedRequest, encodeReliabilityTestTimedRequest(t, 5000)},
		{"invoke", im.OpcodeInvokeRequest, encodeTimedTestInvokeRequest(t, false, im.ConcreteCommandPath{
			Endpoint: 0, Cluster: 0x003C, Command: 0x02, // AdministratorCommissioning.RevokeCommissioning
			HasEndpoint: true, HasCluster: true, HasCommand: true,
		})},
		{"write", im.OpcodeWriteRequest, mustScenarioWrite(t)},
	}
	for i, c := range cases {
		//nolint:gosec // test fixture, a handful of cases.
		buf := buildDatagram(buildHeader(0, uint32(100+i)), buildProtocolHeader(im.InteractionModelProtocolID, c.opcode), c.payload)
		if err := b.dispatch(context.Background(), buf, peerAddr); !errors.Is(err, ErrUnsecuredIM) {
			t.Errorf("%s on session 0: err = %v, want ErrUnsecuredIM", c.name, err)
		}
	}
	_ = peerConn.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
	rbuf := make([]byte, 1500)
	if n, _, err := peerConn.ReadFromUDP(rbuf); err == nil {
		t.Errorf("an IM request on session 0 was answered (%d-byte datagram); want every one dropped", n)
	}
}

func mustScenarioWrite(t *testing.T) []byte {
	t.Helper()
	payload, err := encodeScenarioWriteRequest(im.ConcreteAttributePath{
		Endpoint: 0, Cluster: 0x0028, Attribute: 0x0005, // BasicInformation.NodeLabel
		HasEndpoint: true, HasCluster: true, HasAttribute: true,
	}, true)
	if err != nil {
		t.Fatalf("encodeScenarioWriteRequest: %v", err)
	}
	return payload
}

// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/cluster/spec"
	tdef "github.com/SukramJ/go-fabric/cluster/spec/thermostat"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
	"github.com/SukramJ/go-fabric/transport/message"
)

// TestDispatchInvoke_ExistenceBeforePayload drives Bridge.dispatch with a
// Thermostat SetpointRaiseLower whose Mode is a string — a payload the
// generated definition's decoder refuses with INVALID_COMMAND — on an
// endpoint that does not serve Thermostat and on one that does not exist.
// The path owes UNSUPPORTED_CLUSTER / UNSUPPORTED_ENDPOINT, not the decode
// status: matter.js CommandInvokeResponse.ts #processConcrete checks
// existence before it validates the request (TC-IDM-1.2 sends the first
// command of an unsupported cluster and asserts UnsupportedCluster).
func TestDispatchInvoke_ExistenceBeforePayload(t *testing.T) {
	t.Parallel()
	if spec.Lookup(tdef.ClusterID) == nil {
		t.Fatal("the Thermostat definition is not registered; the test would not reach the generated decoder")
	}
	for _, c := range []struct {
		endpoint uint16
		want     im.StatusCode
	}{
		{1, im.StatusUnsupportedCluster},
		{9, im.StatusUnsupportedEndpoint},
	} {
		b := newStartedBridge(t)
		peerConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
		if err != nil {
			t.Fatalf("ListenUDP: %v", err)
		}
		t.Cleanup(func() { _ = peerConn.Close() })
		peerAddr, _ := peerConn.LocalAddr().(*net.UDPAddr)

		enc := tlv.NewEncoder()
		enc.StartStruct(tlv.AnonymousTag())
		enc.PutBool(tlv.ContextTag(0), false)
		enc.PutBool(tlv.ContextTag(1), false)
		enc.StartArray(tlv.ContextTag(2))
		enc.StartStruct(tlv.AnonymousTag())
		im.ConcreteCommandPath{
			Endpoint: c.endpoint, Cluster: tdef.ClusterID, Command: tdef.CmdSetpointRaiseLower,
			HasEndpoint: true, HasCluster: true, HasCommand: true,
		}.MarshalTLV(enc, tlv.ContextTag(0))
		enc.StartStruct(tlv.ContextTag(1)) // CommandFields
		enc.PutUTF8(tlv.ContextTag(0), "heat")
		enc.PutInt(tlv.ContextTag(1), 5)
		_ = enc.EndContainer()
		_ = enc.EndContainer()
		_ = enc.EndContainer()
		_ = enc.EndContainer()
		payload, err := enc.Bytes()
		if err != nil {
			t.Fatal(err)
		}
		buf := buildDatagram(buildHeader(0, 21), buildProtocolHeader(im.InteractionModelProtocolID, im.OpcodeInvokeRequest), payload)
		if err := b.dispatch(context.Background(), buf, peerAddr); err != nil {
			t.Fatalf("dispatch: %v", err)
		}
		_ = peerConn.SetReadDeadline(time.Now().Add(2 * time.Second))
		rbuf := make([]byte, 1500)
		n, _, err := peerConn.ReadFromUDP(rbuf)
		if err != nil {
			t.Fatalf("ReadFromUDP: %v", err)
		}
		got := rbuf[:n]
		_, hdrLen, err := message.UnmarshalHeader(got)
		if err != nil {
			t.Fatal(err)
		}
		_, protoLen, err := message.UnmarshalProtocolHeader(got[hdrLen:])
		if err != nil {
			t.Fatal(err)
		}
		if status, found := firstStatusIBCode(t, got[hdrLen+protoLen:]); !found || status != c.want {
			t.Errorf("endpoint %d: path status = %v (found %v), want %v", c.endpoint, status, found, c.want)
		}
	}
}

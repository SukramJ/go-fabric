// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package interaction_test

// The server half of matter.js packages/protocol/test/action/InvokeLargeMessageTest.ts
// by meaning: the client stack's server states whether the session an invoke
// arrived on carries Large Messages, and the invoke path refuses a Large
// Message command on one that does not with InvalidTransportType —
// matter.js CommandInvokeResponse.ts:281-285 at db021da0 (no matter.js unit
// test of the server gate exists at the pin; review II, R2-P4-imwrite-01).

import (
	"bytes"
	"context"
	"sync"
	"testing"
	"testing/synctest"

	"thingfabric.dev/go-fabric/client/exchange"
	"thingfabric.dev/go-fabric/client/interaction"
	"thingfabric.dev/go-fabric/cluster/spec"
	wpdef "thingfabric.dev/go-fabric/cluster/spec/webrtctransportprovider"
	"thingfabric.dev/go-fabric/cluster/wire"
	"thingfabric.dev/go-fabric/im"
	"thingfabric.dev/go-fabric/secure/channel"
	"thingfabric.dev/go-fabric/tlv"
	"thingfabric.dev/go-fabric/transport/message"
)

// factDispatcher runs every command and records the Large Message fact of
// each invoke's context.
type factDispatcher struct {
	fakeDispatcher
	mu    sync.Mutex
	facts []bool
}

func (d *factDispatcher) Invoke(ctx context.Context, p im.ConcreteCommandPath, f any) im.InvokeResult {
	large, known := im.LargeMessageSessionFromContext(ctx)
	d.mu.Lock()
	d.facts = append(d.facts, large && known)
	d.mu.Unlock()
	return im.InvokeResult{Path: p, Status: im.StatusSuccess}
}

func (d *factDispatcher) ran() []bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]bool(nil), d.facts...)
}

// provideOffer is an InvokeRequest of WebRtcTransportProvider.ProvideOffer
// (0x0553/0x02), a command with the Large Message quality.
func provideOffer(t *testing.T) []byte {
	t.Helper()
	req := im.InvokeRequest{Invokes: []im.CommandInvocation{{
		Path:   im.ConcreteCommandPath{Endpoint: 1, Cluster: 0x0553, Command: 0x02, HasEndpoint: true, HasCluster: true, HasCommand: true},
		Fields: wpdef.ProvideOfferRequest{WebRtcSessionId: spec.NullOf[uint16](), Sdp: "v=0"},
	}}}
	return encode(t, func(e *tlv.Encoder) { im.MarshalInvokeRequestTLV(e, req, wire.CommandFieldsWriter) })
}

func TestServeInvokeLargeMessageGateParity(t *testing.T) {
	t.Parallel()
	t.Run("UDP refuses", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			s, reg, _, _ := handlerRig(t)
			d := &factDispatcher{}
			reg.Serve(interaction.ServerConfig{Dispatcher: d, Qualities: plainQualities{}})
			s.push(t, 0x7301, im.OpcodeInvokeRequest, provideOffer(t))
			m := expectOpcode(t, s, im.OpcodeInvokeResponse)
			resp := decodeInvokeResponse(t, m.payload)
			if len(resp.Responses) != 1 || !resp.Responses[0].IsStatus || resp.Responses[0].Status.Status != im.StatusInvalidTransportType {
				t.Fatalf("ProvideOffer over UDP answered %+v, want InvalidTransportType", resp.Responses)
			}
			s.ackPushed(t, m)
			synctest.Wait()
			if ran := d.ran(); len(ran) != 0 {
				t.Errorf("the command ran %d times over UDP", len(ran))
			}
		})
	})
	t.Run("TCP runs", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			s, reg, _, _ := handlerRig(t)
			d := &factDispatcher{}
			reg.Serve(interaction.ServerConfig{Dispatcher: d, Qualities: plainQualities{}})
			key := bytes.Repeat([]byte{0x44}, 16)
			ch := &tcpChannel{closed: make(chan struct{})}
			t.Cleanup(func() { close(ch.closed) })
			if _, err := s.mgr.NewSecureSession(exchange.SecureSessionConfig{
				ID: 0x0106, Addr: s.mgr.AttachChannel(ch), FabricIndex: 1,
				Channel: channel.Config{EncryptKey: key, DecryptKey: key, LocalNodeID: 0x1111, PeerNodeID: 0x2222, PeerSessionID: 0x7106},
			}); err != nil {
				t.Fatal(err)
			}
			peer, err := channel.New(channel.Config{EncryptKey: key, DecryptKey: key, LocalNodeID: 0x2222, PeerNodeID: 0x1111, PeerSessionID: 0x0106})
			if err != nil {
				t.Fatal(err)
			}
			hdr := message.Header{SessionID: 0x0106, MessageCounter: 0x500}
			proto := message.ProtocolHeader{Initiator: true, Opcode: im.OpcodeInvokeRequest, ExchangeID: 0x7302, ProtocolID: im.InteractionModelProtocolID}
			sealed, err := peer.Encrypt(&hdr, hdr.SecurityFlags(), append(proto.Marshal(), provideOffer(t)...))
			if err != nil {
				t.Fatal(err)
			}
			s.mgr.HandleChannelMessage(ch, append(hdr.Marshal(), sealed.Ciphertext...))
			synctest.Wait()
			if ran := d.ran(); len(ran) != 1 || !ran[0] {
				t.Errorf("over TCP the command ran %v, want once with the session stated as carrying large messages", ran)
			}
		})
	})
}

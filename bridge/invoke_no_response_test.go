// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"errors"
	"net"
	"os"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
	"github.com/SukramJ/go-fabric/transport/message"
)

// noResponseInvoker answers every command with im.NoResponse.
type noResponseInvoker struct{ im.Dispatcher }

func (noResponseInvoker) Invoke(_ context.Context, p im.ConcreteCommandPath, _ any) im.InvokeResult {
	return im.InvokeResult{Path: p, Status: im.StatusSuccess, Response: im.NoResponse{}}
}

// wildcardNoResponseInvoker also expands a wildcard-endpoint command to
// endpoints 0 and 1 and answers each with im.NoResponse.
type wildcardNoResponseInvoker struct{ noResponseInvoker }

func (wildcardNoResponseInvoker) InvokeAuthorized(_ context.Context, p im.ConcreteCommandPath, _ any, _ im.CommandAuthorizer) []im.InvokeResult {
	var out []im.InvokeResult
	for _, ep := range []uint16{0, 1} {
		q := p
		q.Endpoint, q.HasEndpoint = ep, true
		out = append(out, im.InvokeResult{Path: q, Status: im.StatusSuccess, Response: im.NoResponse{}})
	}
	return out
}

// TestInvokeAnsweredWithNothingSendsNoResponse (review II,
// R2-P6-fsm-1001, F-db021da0-10; decision 182): an InvokeRequest whose
// every concrete command a handler answers with im.NoResponse gets no
// InvokeResponse; a wildcard-endpoint command is always answered, every
// target that returned im.NoResponse left out — an empty InvokeResponse
// when all did.
func TestInvokeAnsweredWithNothingSendsNoResponse(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		wildcard bool
	}{{"concrete", false}, {"wildcard", true}} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			b := newStartedBridge(t)
			peerConn, peerAddr := newSubscribeTestPeer(t)
			req := im.InvokeRequest{Invokes: []im.CommandInvocation{{Path: im.ConcreteCommandPath{
				Endpoint: 0, Cluster: 0x0031, Command: 0x06, HasEndpoint: !tc.wildcard, HasCluster: true, HasCommand: true,
			}}}}
			proto := message.ProtocolHeader{ProtocolID: im.InteractionModelProtocolID, Opcode: im.OpcodeInvokeRequest, ExchangeID: 0x31, Initiator: true}
			if err := b.dispatchInvokeRequest(im.WithAuthModePASE(context.Background()), peerAddr, &message.Header{MessageCounter: 7, HasSourceNodeID: true, SourceNodeID: 0x1234}, proto, wildcardNoResponseInvoker{}, req); err != nil {
				t.Fatal(err)
			}
			buf := make([]byte, 1500)
			for {
				_ = peerConn.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
				n, _, err := peerConn.ReadFrom(buf)
				var ne net.Error
				if errors.Is(err, os.ErrDeadlineExceeded) || (errors.As(err, &ne) && ne.Timeout()) {
					if tc.wildcard {
						t.Fatal("a wildcard command answered with nothing got no InvokeResponse")
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				_, hl, err := message.UnmarshalHeader(buf[:n])
				if err != nil {
					t.Fatal(err)
				}
				p, pl, err := message.UnmarshalProtocolHeader(buf[hl:n])
				if err != nil || p.ProtocolID != im.InteractionModelProtocolID {
					continue
				}
				if !tc.wildcard {
					t.Fatalf("a request answered with nothing got IM opcode 0x%02X", p.Opcode)
				}
				if p.Opcode != im.OpcodeInvokeResponse {
					t.Fatalf("wildcard: IM opcode 0x%02X, want InvokeResponse", p.Opcode)
				}
				resp, err := im.UnmarshalInvokeResponseTLV(tlv.NewDecoder(buf[hl+pl:n]), nil)
				if err != nil {
					t.Fatal(err)
				}
				if len(resp.Responses) != 0 {
					t.Fatalf("wildcard: %d entries, want none (every target left out)", len(resp.Responses))
				}
				return
			}
		})
	}
}

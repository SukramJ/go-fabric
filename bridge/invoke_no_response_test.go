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
	"github.com/SukramJ/go-fabric/transport/message"
)

// noResponseInvoker answers every command with im.NoResponse.
type noResponseInvoker struct{ im.Dispatcher }

func (noResponseInvoker) Invoke(_ context.Context, p im.ConcreteCommandPath, _ any) im.InvokeResult {
	return im.InvokeResult{Path: p, Status: im.StatusSuccess, Response: im.NoResponse{}}
}

// TestInvokeAnsweredWithNothingSendsNoResponse (review II,
// R2-P6-fsm-1001, F-db021da0-10): an InvokeRequest whose every command
// a handler answers with im.NoResponse gets no InvokeResponse.
func TestInvokeAnsweredWithNothingSendsNoResponse(t *testing.T) {
	t.Parallel()
	b := newStartedBridge(t)
	peerConn, peerAddr := newSubscribeTestPeer(t)
	req := im.InvokeRequest{Invokes: []im.CommandInvocation{{Path: im.ConcreteCommandPath{
		Endpoint: 0, Cluster: 0x0031, Command: 0x06, HasEndpoint: true, HasCluster: true, HasCommand: true,
	}}}}
	proto := message.ProtocolHeader{ProtocolID: im.InteractionModelProtocolID, Opcode: im.OpcodeInvokeRequest, ExchangeID: 0x31, Initiator: true}
	if err := b.dispatchInvokeRequest(im.WithAuthModePASE(context.Background()), peerAddr, &message.Header{MessageCounter: 7, HasSourceNodeID: true, SourceNodeID: 0x1234}, proto, noResponseInvoker{}, req); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 1500)
	for {
		_ = peerConn.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
		n, _, err := peerConn.ReadFrom(buf)
		var ne net.Error
		if errors.Is(err, os.ErrDeadlineExceeded) || (errors.As(err, &ne) && ne.Timeout()) {
			return
		}
		if err != nil {
			t.Fatal(err)
		}
		_, hl, err := message.UnmarshalHeader(buf[:n])
		if err != nil {
			t.Fatal(err)
		}
		p, _, err := message.UnmarshalProtocolHeader(buf[hl:n])
		if err == nil && p.ProtocolID == im.InteractionModelProtocolID {
			t.Fatalf("a request answered with nothing got IM opcode 0x%02X", p.Opcode)
		}
	}
}

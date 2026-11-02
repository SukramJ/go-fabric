// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package client_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/client/interaction"
	"github.com/SukramJ/go-fabric/cluster/spec"
	basicinfo "github.com/SukramJ/go-fabric/cluster/spec/basicinformation"
	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
	"github.com/SukramJ/go-fabric/transport/message"
)

// TestBlockingHookDoesNotDelayStatusResponse (W4-ctlim-04): a subscription
// report's final StatusResponse goes out before the image, the store and
// the host hooks see the report, so a blocking Hooks.Changed does not hold
// it. Mirrors matter.js ClientStructure.ts:417-439 ("Serial FIFO for
// deferred persist/emit so Status.Success is not gated on internal and
// consumer data processing") and InteractionMessenger.ts:1020-1045.
func TestBlockingHookDoesNotDelayStatusResponse(t *testing.T) {
	t.Parallel()
	var armed atomic.Bool
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	sr := newScriptedRegistry(t, seedCommissioned(3), func(c *client.RegistryConfig) {
		c.HooksFor = func(string) client.Hooks {
			return client.Hooks{Changed: func() {
				if !armed.Load() {
					return
				}
				select {
				case entered <- struct{}{}:
				default:
				}
				<-release
			}}
		}
	})
	n := sr.reg.Get("peer1")
	const id = 0x51
	sr.subscribe(t, n, sr.imageWithOpCreds("ours"), id)
	client.WaitRegistry(sr.reg)
	armed.Store(true)

	sr.send(t, message.ProtocolHeader{Initiator: true, NeedsAck: true, Opcode: im.OpcodeReportData, ExchangeID: 0x7101, ProtocolID: im.InteractionModelProtocolID},
		encode(t, func(e *tlv.Encoder) {
			// A report that asks for its StatusResponse (dataReport suppresses it).
			im.ReportData{SubscriptionID: id, HasSubscription: true, Reports: []im.AttributeReport{value(0, basicinfo.ClusterID, 2, 8, tlvNode(uintNode(0xFFF2)))}}.MarshalTLV(e,
				func(e *tlv.Encoder, tag tlv.Tag, v im.AttributeValue) {
					interaction.Raw(v.Value.(spec.Node)).EncodeTLV(e, tag)
				})
		}))
	status := sr.next(t)
	// The peer acknowledges it, as a device does (the final status is a
	// reliable message whose Send waits for the ack).
	sr.send(t, message.ProtocolHeader{Initiator: true, HasAck: true, AckCounter: status.hdr.MessageCounter, Opcode: 0x10, ExchangeID: 0x7101}, nil)
	if status.proto.Opcode != im.OpcodeStatusResponse || status.proto.ExchangeID != 0x7101 {
		t.Fatalf("message 0x%02X on exchange 0x%04X, want the report's StatusResponse", status.proto.Opcode, status.proto.ExchangeID)
	}
	select {
	case <-entered:
	case <-time.After(scriptedHangBound):
		t.Fatal("the Changed hook did not run")
	}
	close(release)
	if err := n.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
}

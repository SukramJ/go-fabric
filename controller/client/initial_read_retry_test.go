// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package client_test

import (
	"context"
	"testing"
	"testing/synctest"

	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
)

// TestNewlyCommissionedReadIsMadeAgainAfterAFailure (review II,
// R2-P6-err-2363): a newly commissioned node whose initial read fails
// makes that read again on the next start — the same call again — before
// it subscribes. Before, the flag was cleared ahead of the read, so the
// next start went straight to the subscription's bootstrap.
func TestNewlyCommissionedReadIsMadeAgainAfterAFailure(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		ctx := context.Background()
		sn := newScriptedNode(t, client.Config{ID: "peer1", Store: &client.MemoryStore{}, AutoSubscribe: true, NewlyCommissioned: true})
		started := make(chan error, 1)
		go func() { started <- sn.node.Start(ctx) }()
		read := sn.next(t)
		first := readRequestOf(t, read)
		sn.reply(t, read, im.InteractionModelProtocolID, im.OpcodeStatusResponse, encode(t, func(e *tlv.Encoder) { im.StatusResponse{Status: im.StatusFailure}.MarshalTLV(e) }))
		if err := <-started; err == nil {
			t.Fatal("Start with a failed initial read succeeded")
		}
		if err := sn.node.Stop(ctx); err != nil {
			t.Fatal(err)
		}
		go func() { started <- sn.node.Start(ctx) }()
		again := sn.next(t)
		// The newly-commissioned read takes every event from the start; the
		// subscription's bootstrap read only those past the cursor.
		if second := readRequestOf(t, again); len(second.EventFilters) != len(first.EventFilters) {
			t.Fatalf("the next start's read has event filters %+v, want the newly-commissioned read's %+v", second.EventFilters, first.EventFilters)
		}
		sn.reply(t, again, im.InteractionModelProtocolID, im.OpcodeReportData, dataReport(t, 0, image(5)))
		if err := <-started; err != nil {
			t.Fatal(err)
		}
		if sub := sn.next(t); sub.proto.Opcode != im.OpcodeSubscribeRequest {
			t.Fatalf("after the read opcode %#x, want the SubscribeRequest", sub.proto.Opcode)
		}
	})
}

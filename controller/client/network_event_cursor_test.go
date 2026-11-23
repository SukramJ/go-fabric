// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package client_test

import (
	"context"
	"math"
	"testing"
	"testing/synctest"

	"thingfabric.dev/go-fabric/controller/client"
	"thingfabric.dev/go-fabric/im"
)

// TestEventCursorAtTheTopOfTheRange holds R2-C2-wire-2351: a node that
// reports event number 2^64-1 raises the persisted cursor to it (as any
// higher number), and the next node's bootstrap read and default
// subscription ask from that number, not from 0 — the cursor + 1
// saturates instead of wrapping. (A sustained subscription's own
// re-subscription raises its filter in client/interaction, sustained.go,
// outside this test.)
func TestEventCursorAtTheTopOfTheRange(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		ctx := context.Background()
		st := &client.MemoryStore{}
		hl := newHookLog()
		sn := newScriptedNode(t, client.Config{ID: "peer1", Store: st, AutoSubscribe: true, NewlyCommissioned: true, Hooks: hl.hooks()})
		started := make(chan error, 1)
		go func() { started <- sn.node.Start(ctx) }()
		read := sn.next(t)
		sn.reply(t, read, im.InteractionModelProtocolID, im.OpcodeReportData, dataReport(t, 0, image(5)))
		if err := <-started; err != nil {
			t.Fatal(err)
		}
		sub := sn.next(t)
		sn.reply(t, sub, im.InteractionModelProtocolID, im.OpcodeReportData, dataReport(t, 33, nil, startUp(math.MaxUint64)))
		sn.reply(t, sub, im.InteractionModelProtocolID, im.OpcodeSubscribeResponse, subscribeResponse(t, 33))
		hl.wait(t, client.Connected)
		waitUntil(t, "the event", func() bool {
			hl.mu.Lock()
			defer hl.mu.Unlock()
			return len(hl.events) > 0
		})

		if err := sn.node.Close(ctx); err != nil {
			t.Fatal(err)
		}
		hl.wait(t, client.Disconnected)

		sn2 := newScriptedNode(t, client.Config{ID: "peer1", Store: st, Hooks: newHookLog().hooks()})
		if err := sn2.node.Start(ctx); err != nil {
			t.Fatal(err)
		}
		boot := sn2.next(t)
		br := readRequestOf(t, boot)
		if len(br.EventFilters) != 1 || br.EventFilters[0].EventMin != math.MaxUint64 {
			t.Fatalf("bootstrap read event filters %+v, want EventMin 2^64-1", br.EventFilters)
		}
		sn2.reply(t, boot, im.InteractionModelProtocolID, im.OpcodeReportData, dataReport(t, 0, nil))
		after := subscribeRequestOf(t, sn2.next(t))
		if len(after.EventFilters) != 1 || after.EventFilters[0].EventMin != math.MaxUint64 {
			t.Fatalf("default subscription event filters %+v, want EventMin 2^64-1", after.EventFilters)
		}
	})
}

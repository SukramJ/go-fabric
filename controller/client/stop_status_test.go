// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package client_test

import (
	"context"
	"slices"
	"testing"
	"testing/synctest"

	"thingfabric.dev/go-fabric/controller/client"
	"thingfabric.dev/go-fabric/im"
)

// TestStopAnnouncesTheSubscriptionEnded (review II, R2-P6-fsm-2350): a
// node taken offline by Stop, Disable or Close while its default
// subscription was reported subscribed announces it not subscribed, once.
// Before, the subscription's own ActiveChanged(false) found another owner
// and the hook never got false.
func TestStopAnnouncesTheSubscriptionEnded(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		stop func(context.Context, *client.Node) error
	}{
		{"Stop", func(ctx context.Context, n *client.Node) error { return n.Stop(ctx) }},
		{"Disable", func(ctx context.Context, n *client.Node) error { return n.Disable(ctx) }},
		{"Close", func(ctx context.Context, n *client.Node) error { return n.Close(ctx) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			synctest.Test(t, func(t *testing.T) {
				ctx := context.Background()
				hl := newHookLog()
				sn := newScriptedNode(t, client.Config{ID: "peer1", Store: &client.MemoryStore{}, AutoSubscribe: true, NewlyCommissioned: true, Hooks: hl.hooks()})
				started := make(chan error, 1)
				go func() { started <- sn.node.Start(ctx) }()
				read := sn.next(t)
				sn.reply(t, read, im.InteractionModelProtocolID, im.OpcodeReportData, dataReport(t, 0, image(5)))
				if err := <-started; err != nil {
					t.Fatal(err)
				}
				sub := sn.next(t)
				sn.reply(t, sub, im.InteractionModelProtocolID, im.OpcodeReportData, dataReport(t, 33, nil))
				sn.reply(t, sub, im.InteractionModelProtocolID, im.OpcodeSubscribeResponse, subscribeResponse(t, 33))
				hl.wait(t, client.Connected)
				if err := tc.stop(ctx, sn.node); err != nil {
					t.Fatal(err)
				}
				synctest.Wait()
				hl.mu.Lock()
				status := slices.Clone(hl.status)
				hl.mu.Unlock()
				if !slices.Equal(status, []bool{true, false}) {
					t.Fatalf("SubscriptionStatusChanged calls %v, want [true false]", status)
				}
			})
		})
	}
}

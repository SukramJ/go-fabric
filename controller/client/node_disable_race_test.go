// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package client_test

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"

	"thingfabric.dev/go-fabric/controller/client"
	"thingfabric.dev/go-fabric/im"
)

// TestStartDuringDisableIsRefused holds R2-C2-conc-2353: Disable marks the
// node disabled before it stops it, so a Start that runs while Disable's
// Stop is under way — here from the SubscriptionStatusChanged(false) hook
// Stop calls, on another goroutine — is refused with ErrDisabled instead
// of starting a node that ends up persisted as disabled but online.
func TestStartDuringDisableIsRefused(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		ctx := context.Background()
		hl := newHookLog()
		hooks := hl.hooks()
		var sn *scriptedNode
		armed := false
		restarted := make(chan error, 1)
		gate := make(chan struct{})
		status := hooks.SubscriptionStatusChanged
		hooks.SubscriptionStatusChanged = func(active bool) {
			status(active)
			if active || !armed {
				return
			}
			armed = false
			go func() { restarted <- sn.node.Start(ctx) }()
			<-gate
		}
		sn = newScriptedNode(t, client.Config{ID: "peer1", Store: &client.MemoryStore{}, AutoSubscribe: true, NewlyCommissioned: true, Hooks: hooks})
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

		armed = true
		disabled := make(chan error, 1)
		go func() { disabled <- sn.node.Disable(ctx) }()
		synctest.Wait()
		select {
		case err := <-restarted:
			close(gate)
			if !errors.Is(err, client.ErrDisabled) {
				t.Fatalf("Start during Disable = %v, want ErrDisabled", err)
			}
		default:
			// The Start passed its decision and is bringing the node online
			// while Disable's Stop runs.
			close(gate)
			if err := <-disabled; err != nil {
				t.Fatal(err)
			}
			t.Fatalf("a Start during Disable started the node (disabled %v, state %v)", sn.node.Disabled(), sn.node.ConnectionState())
		}
		if err := <-disabled; err != nil {
			t.Fatal(err)
		}
		if !sn.node.Disabled() || sn.node.ConnectionState() != client.Disconnected {
			t.Fatalf("after Disable: disabled %v, state %v", sn.node.Disabled(), sn.node.ConnectionState())
		}
	})
}

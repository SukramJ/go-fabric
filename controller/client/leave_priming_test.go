// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package client_test

import (
	"testing"
	"time"

	"thingfabric.dev/go-fabric/controller/client"
	"thingfabric.dev/go-fabric/fabrictest"
	"thingfabric.dev/go-fabric/node"
)

// TestLeaveOfPrimingReportReactedLate: a Leave the device holds from
// before commissioning arrives in the default subscription's priming
// report and is ignored, also when its reaction runs only after the
// subscription became established. The reaction runs on a goroutine of
// its own after the report's hooks ran on the node's job queue; under a
// loaded scheduler it ran late, saw an active subscription, and deleted
// the node it had just commissioned — the default subscription never
// came up again (issue #628, the CI failure of TestClientNodeParity's
// leave-event case). The gate holds the reaction until the subscription
// is active, the interleaving that run had.
func TestLeaveOfPrimingReportReactedLate(t *testing.T) {
	t.Parallel()
	site := quietSite(t)
	p := site.UncommissionedPair(fabrictest.PairOptions{})
	reg := p.Controller.Registry()
	held := make(chan bool, 1)
	client.SetLeaveGate(reg, func(n *client.Node) {
		deadline := time.Now().Add(30 * time.Second)
		for !n.SubscriptionActive() && time.Now().Before(deadline) {
			time.Sleep(time.Millisecond)
		}
		select {
		case held <- n.SubscriptionActive():
		default:
		}
	})
	if err := p.Device.Act(func(n *node.Node) error {
		n.Root().BasicInformation.EmitLeave(1)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	n, err := p.Controller.CommissionNode(parityCtx(t), p.Device, client.CommissionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case active := <-held:
		if !active {
			t.Fatal("the subscription did not become active while the reaction was held")
		}
	case <-time.After(time.Minute):
		t.Fatal("no Leave reaction ran")
	}
	client.WaitRegistry(reg)
	if reg.Get(n.ID()) == nil || reg.Size() != 1 {
		t.Fatalf("a stale Leave of the priming report deleted the node (registry size %d)", reg.Size())
	}
	if !n.SubscriptionActive() {
		t.Fatal("the default subscription is not active")
	}
}

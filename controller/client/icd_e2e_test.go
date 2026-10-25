// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package client_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	icddef "github.com/SukramJ/go-fabric/cluster/spec/icdmanagement"
	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/node"
)

// TestICDEndToEnd is the bundle's end-to-end proof (brief CH §2.8): a LIT
// device of this module (node.Config.ICD, idle 30 s, a DSLS device put
// into LIT before commissioning as IcdClientTest's litOperatingPair) and this module's
// controller on a fabrictest site on the fake clock. The controller
// commissions it with its sustained default subscription and auto-registers
// on the LIT peer; the device goes through three idle cycles and the
// subscription survives them; re-registered under a subject the
// subscription does not cover, the sleeping peer's read parks and completes
// on its Check-In; Unregister ends the registration on both sides.
func TestICDEndToEnd(t *testing.T) {
	const idle = 30 * time.Second
	p := newICDPair(t, icdPairOptions{cfg: node.ICDConfig{
		Mode: node.ICDModeLIT, ActiveModeThreshold: 5 * time.Second, IdleModeDuration: idle,
		ActiveModeDuration: time.Second, MaximumCheckInBackoff: idle,
		UserActiveModeTrigger: &node.ICDUserActiveModeTrigger{Hint: icddef.UserActiveModeTriggerPowerCycle},
		DynamicSITLIT:         true,
	}, forceLIT: true})
	dev := p.device().ICD()
	if dev.Mode() != node.ICDModeLIT {
		t.Fatalf("device operating mode %v", dev.Mode())
	}

	// The client auto-registers on the LIT peer (IcdClient.ts LIT auto-registration).
	p.eventually(t, "the LIT auto-registration", p.icd().Registered)
	if regs := p.deviceServerRegistrations(t); len(regs) != 1 {
		t.Fatalf("device registrations %+v", regs)
	}

	// Three idle cycles of 30 s; the subscription stands throughout.
	var mu sync.Mutex
	wakes := 0
	defer dev.OnModeChange(func(e node.ICDModeEvent) {
		if e == node.ICDActiveModeEntered {
			mu.Lock()
			wakes++
			mu.Unlock()
		}
	})()
	subID := func() uint32 {
		active := p.pair.Controller.Subscriptions().Active()
		if len(active) != 1 {
			t.Fatalf("%d active subscriptions", len(active))
		}
		return active[0].ID()
	}
	before := subID()
	for cycle := 1; cycle <= 3; cycle++ {
		// The device's mode never cycles on its own (icd.ModeState,
		// IcdMode.ts): the host puts it to sleep and wakes it.
		dev.EnterIdleMode()
		p.advance(t, idle)
		dev.RequestActiveMode()
		if err := p.site.Settle(parityCtx(t)); err != nil {
			t.Fatal(err)
		}
		if !p.node.SubscriptionActive() {
			t.Fatalf("cycle %d: the subscription is not active", cycle)
		}
		if id := subID(); id != before {
			t.Fatalf("cycle %d: subscription %d replaced by %d", cycle, before, id)
		}
	}
	mu.Lock()
	n := wakes
	mu.Unlock()
	if n < 3 {
		t.Fatalf("%d wakes in three idle cycles", n)
	}

	// Under a subject the subscription does not cover, the device checks in
	// and the controller awaits the sleeping peer.
	if err := p.do(t, func(ctx context.Context) error { return p.icd().Unregister(ctx) }); err != nil {
		t.Fatal(err)
	}
	if err := p.do(t, func(ctx context.Context) error {
		if err := p.icd().Register(ctx, uncovered); err != nil && !errors.Is(err, client.ErrICDOperation) {
			return err
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	p.eventually(t, "registered", p.icd().Registered)
	p.eventually(t, "the peer asleep", func() bool { w := p.wakefulness(t); return w != nil && w.RequiresAwait() && !w.Awake() })
	got := &checkIns{}
	p.icd().OnCheckIn(got.record)
	read := p.readAsync(parityCtx(t), descriptorRead)
	time.Sleep(20 * time.Millisecond)
	select {
	case err := <-read:
		t.Fatalf("the read went out to a sleeping peer: %v", err)
	default:
	}
	p.wakeDevice(t)
	var err error
	p.eventually(t, "the parked read", func() bool {
		select {
		case err = <-read:
			return true
		default:
			return false
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.n() == 0 {
		t.Fatal("the read completed without a Check-In")
	}

	// Unregister ends it: no registration on either side, a read passes.
	if err := p.do(t, func(ctx context.Context) error { return p.icd().Unregister(ctx) }); err != nil {
		t.Fatal(err)
	}
	if p.icd().Registered() || p.wakefulness(t) != nil {
		t.Fatal("still registered on the controller")
	}
	if regs := p.deviceServerRegistrations(t); len(regs) != 0 {
		t.Fatalf("device registrations %+v", regs)
	}
	if err := p.do(t, func(ctx context.Context) error { _, err := p.node.Read(ctx, descriptorRead); return err }); err != nil {
		t.Fatal(err)
	}
}

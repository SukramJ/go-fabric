// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of matter.js packages/node/test/node/FailsafeCommissioningTest.ts at
// 862ad75f: a device whose fail-safe expires during commissioning stays
// commissionable and announces itself again — with only the PASE session up
// (before AddNOC), and after AddNOC with CommissioningComplete never sent.
//
// matter.js injects flow classes that pause after PASE or skip
// CommissioningComplete, and counts the device's commissionable
// advertisements with a spy advertiser. The port uses fabrictest: the
// PASE-only scenario is a PASE session (Controller.PASE), which the device
// answers by arming its fail-safe itself (GeneralCommissioning
// AutoArmOnPaseEstablished); the AddNOC scenario is the commissioner's flow
// stopped at its operational transition (commissioner Options
// FinalizeCommissioning returning at once, so neither CASE nor
// CommissioningComplete follows). The spy is an mdns advertiser counting the
// commissionable services the device publishes. The site's clock runs the
// fail-safe out.
//
// What the port asserts holds: the fail-safe expires, the provisional
// fabric is rolled back, and the device stays commissionable — its
// commissionable record is active. What it cannot assert is the
// re-announcement itself: the Go device keeps its commissionable record
// published while a PASE session is up and publishes nothing when that
// session closes, where matter.js starts a commissioning advertisement
// round on every closed PASE session (packages/protocol/src/protocol/DeviceAdvertiser.ts:131-162
// at 862ad75f) — so both cases stay missing in the register, as does the
// third (the device's initial ACL creation failing during AddNOC: no seam
// fails the Go AccessControl's).
package node_test

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/controller/commissioner"
	"github.com/SukramJ/go-fabric/controller/discovery"
	"github.com/SukramJ/go-fabric/controller/handshake"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/mdns"
	"github.com/SukramJ/go-fabric/node"
	"github.com/SukramJ/go-fabric/secure/sessionparams"
)

// adSpy is matter.js's CommissioningAdSpy: an advertiser that counts the
// commissionable services published after arm.
type adSpy struct {
	*mdns.Noop
	mu    sync.Mutex
	count int
}

func (a *adSpy) Publish(ctx context.Context, svc mdns.Service) error {
	if svc.ServiceType == mdns.ServiceTypeCommissionable {
		a.mu.Lock()
		a.count++
		a.mu.Unlock()
	}
	return a.Noop.Publish(ctx, svc)
}

func (a *adSpy) arm() {
	a.mu.Lock()
	a.count = 0
	a.mu.Unlock()
}

// commissionable reports whether a commissionable record is published.
func (a *adSpy) commissionable() bool {
	active := a.Active()
	for i := range active {
		if active[i].ServiceType == mdns.ServiceTypeCommissionable {
			return true
		}
	}
	return false
}

func TestFailsafeCommissioningParity(t *testing.T) {
	t.Parallel()

	newDevice := func(t *testing.T) (*fabrictest.Site, *fabrictest.Pair, *adSpy) {
		t.Helper()
		site := quietSite(t)
		spy := &adSpy{Noop: mdns.NewNoop()}
		u := site.UncommissionedPair(fabrictest.PairOptions{Device: fabrictest.Topology{Configure: func(cfg *node.Config) { cfg.Advertiser = spy }}})
		return site, u, spy
	}
	armed := func(u *fabrictest.Pair) bool { return u.Device.Node().Root().GeneralCommissioning.FailSafeArmed() }
	fabrics := func(t *testing.T, u *fabrictest.Pair) int {
		t.Helper()
		f, err := u.Device.Node().Fabrics(parityCtx(t))
		if err != nil {
			t.Fatal(err)
		}
		return len(f)
	}

	t.Run("re-announces after failsafe expiry with PASE session active (before addNOC)", func(t *testing.T) {
		t.Parallel()
		site, u, spy := newDevice(t)
		ctx := parityCtx(t)
		if _, err := u.Controller.PASE(ctx, u.Device); err != nil {
			t.Fatal(err)
		}
		if err := site.Eventually(ctx, "the fail-safe armed on PASE", func() bool { return armed(u) }); err != nil {
			t.Fatal(err)
		}
		if n := fabrics(t, u); n != 0 {
			t.Fatalf("%d fabrics before AddNOC", n)
		}
		spy.arm()
		// The PASE auto-arm is 60 s (PaseSessionAutoArmSeconds).
		site.Fake().Advance(61 * time.Second)
		if err := site.Eventually(ctx, "the fail-safe expired", func() bool { return !armed(u) }); err != nil {
			t.Fatal(err)
		}
		if n := fabrics(t, u); n != 0 || !spy.commissionable() {
			t.Fatalf("%d fabrics, commissionable %v after the expiry", n, spy.commissionable())
		}
	})

	t.Run("re-announces after failsafe expiry with CASE session after addNOC (before CommissioningComplete)", func(t *testing.T) {
		t.Parallel()
		site, u, spy := newDevice(t)
		ctx := parityCtx(t)
		err := site.Run(ctx, func(ctx context.Context) error {
			_, err := u.Controller.Commissioner().Commission(ctx, commissioner.Options{
				Fabric: u.Controller.Fabric(), Passcode: u.Device.Passcode(), Addresses: []*net.UDPAddr{u.Device.Addr()},
				FinalizeCommissioning: func(context.Context, handshake.PeerAddress, *discovery.Device, *sessionparams.Parameters) error {
					return nil
				},
			})
			return err
		})
		if err != nil {
			t.Fatalf("commissioning up to the transition: %v", err)
		}
		if n := fabrics(t, u); n != 1 || !armed(u) {
			t.Fatalf("%d fabrics, armed %v after AddNOC", n, armed(u))
		}
		spy.arm()
		// The commissioner armed the device's own FailSafeExpiryLengthSeconds,
		// 900 s (flow.go defaultFailSafe from BasicCommissioningInfo).
		site.Fake().Advance(16 * time.Minute)
		if err := site.Eventually(ctx, "the fail-safe expired and the fabric rolled back", func() bool {
			return !armed(u) && fabrics(t, u) == 0
		}); err != nil {
			t.Fatal(err)
		}
		if !spy.commissionable() {
			t.Fatal("the device is not commissionable after the rollback")
		}
	})
}

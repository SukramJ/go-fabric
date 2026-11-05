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
// The port asserts what matter.js asserts: the fail-safe expires, the
// provisional fabric is rolled back, the device stays commissionable — its
// commissionable record is active — and it announces itself again: the
// closed PASE session starts a new announcement round of the record
// (node commissioningHost.reannounceAfterPaseClose, as matter.js's
// DeviceAdvertiser does on `sessions.deleted` for a session with `isPase`,
// packages/protocol/src/protocol/DeviceAdvertiser.ts:131-162 at 862ad75f),
// exactly once in the AddNOC case — the CASE session's close announces
// nothing (FailsafeCommissioningTest.ts:290-300). The third
// (the device's initial ACL creation failing during AddNOC) wraps the
// device's credential store (node.Config.Store.Credentials) so that the
// ReplaceACL of the initial entry fails, and asserts the commissionable
// record where matter.js awaits the announcement.
package node_test

import (
	"context"
	"errors"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/client/discovery"
	"github.com/SukramJ/go-fabric/client/handshake"
	"github.com/SukramJ/go-fabric/controller/commissioner"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/mdns"
	"github.com/SukramJ/go-fabric/node"
	"github.com/SukramJ/go-fabric/secure/sessionparams"
	"github.com/SukramJ/go-fabric/store"
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

// announced is the number of commissionable publishes since arm — matter.js's
// adSpy.count.
func (a *adSpy) announced() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.count
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
		f, err := u.Device.Node().FabricInfos(parityCtx(t))
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
		if err := site.Step(ctx, 61*time.Second); err != nil {
			t.Fatal(err)
		}
		if err := site.Eventually(ctx, "the fail-safe expired", func() bool { return !armed(u) }); err != nil {
			t.Fatal(err)
		}
		if n := fabrics(t, u); n != 0 || !spy.commissionable() {
			t.Fatalf("%d fabrics, commissionable %v after the expiry", n, spy.commissionable())
		}
		// FailsafeCommissioningTest.ts:175-186: the expiry closes the PASE
		// session, and the device announces itself again.
		if err := site.Eventually(ctx, "the commissionable re-announcement", func() bool { return spy.announced() > 0 }); err != nil {
			t.Fatal(err)
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
		if err := site.Step(ctx, 16*time.Minute); err != nil {
			t.Fatal(err)
		}
		if err := site.Eventually(ctx, "the fail-safe expired and the fabric rolled back", func() bool {
			return !armed(u) && fabrics(t, u) == 0
		}); err != nil {
			t.Fatal(err)
		}
		if !spy.commissionable() {
			t.Fatal("the device is not commissionable after the rollback")
		}
		// FailsafeCommissioningTest.ts:290-300: the PASE session's close
		// during the rollback announced once; the CASE session's close
		// (fabric.delete) announced nothing — the count is 1, not 2.
		if err := site.Eventually(ctx, "the commissionable re-announcement", func() bool { return spy.announced() > 0 }); err != nil {
			t.Fatal(err)
		}
		if err := site.Settle(ctx); err != nil {
			t.Fatal(err)
		}
		if n := spy.announced(); n != 1 {
			t.Fatalf("%d commissionable announcements after the rollback, want 1 (the CASE close announces nothing)", n)
		}
	})

	// FailsafeCommissioningTest.ts:305-346: the device's initial ACL
	// cannot be written during AddNOC (matter.js's FailingAccessControlServer
	// throws from addDefaultCaseAcl, :100-104; here the credential store
	// refuses the ReplaceACL AddNOC installs the CaseAdminSubject entry
	// with): the commissioning fails, no fabric stays, the device stays up
	// and commissionable. matter.js also awaits a commissioning
	// announcement (adSpy.announced); that is the re-announcement the two
	// cases above leave missing, so the subtest asserts the commissionable
	// record instead and is named for it.
	t.Run("recovers cleanly and stays commissionable when initial ACL creation fails during addNOC", func(t *testing.T) {
		t.Parallel()
		site := quietSite(t)
		spy := &adSpy{Noop: mdns.NewNoop()}
		creds := &failingACL{}
		u := site.UncommissionedPair(fabrictest.PairOptions{Device: fabrictest.Topology{Configure: func(cfg *node.Config) {
			cfg.Advertiser = spy
			creds.Port = cfg.Store.Credentials
			cfg.Store.Credentials = creds
		}}})
		ctx := parityCtx(t)
		spy.arm()
		err := site.Run(ctx, func(ctx context.Context) error {
			_, err := u.Controller.Commissioner().Commission(ctx, commissioner.Options{
				Fabric: u.Controller.Fabric(), Passcode: u.Device.Passcode(), Addresses: []*net.UDPAddr{u.Device.Addr()},
			})
			return err
		})
		if err == nil {
			t.Fatal("the commissioning succeeded without its initial ACL")
		}
		// The device answers AddNOC InvalidNOC with the store's error as
		// its debug text.
		if !creds.failed() || !strings.Contains(err.Error(), "addNoc") || !strings.Contains(err.Error(), "simulated ACL transaction failure") {
			t.Fatalf("commissioning: %v, want AddNOC refused for the initial ACL", err)
		}
		if err := site.Settle(ctx); err != nil {
			t.Fatal(err)
		}
		if n := fabrics(t, u); n != 0 || !u.Device.Running() || !spy.commissionable() {
			t.Fatalf("%d fabrics, running %v, commissionable %v after the failed AddNOC", n, u.Device.Running(), spy.commissionable())
		}
	})
}

// failingACL is a credential store whose first ReplaceACL fails — the
// write of the initial CaseAdminSubject entry AddNOC makes.
type failingACL struct {
	store.Port
	mu    sync.Mutex
	calls int
}

func (f *failingACL) ReplaceACL(ctx context.Context, fabricIndex uint8, entries []store.ACLEntry) error {
	f.mu.Lock()
	f.calls++
	first := f.calls == 1
	f.mu.Unlock()
	if first {
		return errors.New("simulated ACL transaction failure (test)")
	}
	return f.Port.ReplaceACL(ctx, fabricIndex, entries)
}

func (f *failingACL) failed() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls > 0
}

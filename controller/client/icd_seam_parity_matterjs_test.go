// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package client_test

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/client/interaction"
	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/internal/coreseam"
	"github.com/SukramJ/go-fabric/node"
)

// icdCommands counts the RegisterClient and UnregisterClient commands the
// device's ICDManagement server receives and refuses RegisterClient while
// refuse is set — matter.js's CountingIcdServer and RefusingIcdServer
// (IcdClientTest.ts:265-274, :298-312 at 862ad75f), installed through
// internal/coreseam.ObserveICDRegistrations.
type icdCommands struct {
	mu                             sync.Mutex
	registrations, unregistrations int
	refuse                         bool
}

func (p *icdPair) countICDCommands(t *testing.T) *icdCommands {
	t.Helper()
	c := &icdCommands{}
	if !coreseam.ObserveICDRegistrations(p.device().Root().ICDManagement, func(register bool) error {
		c.mu.Lock()
		defer c.mu.Unlock()
		if register {
			c.registrations++
			if c.refuse {
				return errors.New("refusing registration")
			}
			return nil
		}
		c.unregistrations++
		return nil
	}) {
		t.Fatal("the device has no ICDManagement server")
	}
	return c
}

func (c *icdCommands) counts() (registrations, unregistrations int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.registrations, c.unregistrations
}

func (c *icdCommands) reset() {
	c.mu.Lock()
	c.registrations, c.unregistrations = 0, 0
	c.mu.Unlock()
}

func (c *icdCommands) setRefuse(on bool) {
	c.mu.Lock()
	c.refuse = on
	c.mu.Unlock()
}

// sleepingLITPeer is IcdClientTest.ts:318-336 sleepingLitPeer: a LIT peer
// registered under a subject the subscription does not cover, asleep and
// silenced, its command counts reset.
func sleepingLITPeer(t *testing.T) (*icdPair, *icdCommands) {
	t.Helper()
	p := registeredLITPair(t)
	cmds := p.countICDCommands(t)
	p.silence(true)
	p.advance(t, 6*time.Second)
	cmds.reset()
	return p, cmds
}

// restored is the registry's node of p after a controller restart.
func (p *icdPair) restarted(t *testing.T) *client.Node {
	t.Helper()
	if err := p.pair.Controller.Restart(parityCtx(t)); err != nil {
		t.Fatal(err)
	}
	n := p.pair.Controller.Registry().Get(p.node.ID())
	if n == nil {
		t.Fatal("the node was not restored")
	}
	return n
}

// TestIcdClientSeamParity holds the IcdClientTest.ts cases of the seam
// wave (issue #396): a device that refuses or counts its registration
// commands (internal/coreseam.ObserveICDRegistrations), a controller
// restarted across a pending key or an abandoned unregister
// (fabrictest.Controller.Restart), a sleeping peer (fabrictest.Network.Filter
// dropping the device's datagrams). A port of
// packages/node/test/behavior/system/icd/IcdClientTest.ts at 862ad75f; each
// case names its lines.
func TestIcdClientSeamParity(t *testing.T) {
	t.Parallel()

	t.Run("keeps the pending key when the device refuses the refresh, and offers it again", func(t *testing.T) {
		t.Parallel()
		// :982-1018.
		p, log := icdLogPair(t)
		p.register(t, uncovered)
		cmds := p.countICDCommands(t)
		p.requestRefreshOnNextCheckIn(t)
		oldKey := p.deviceKey(t)
		cmds.setRefuse(true)
		p.wakeDevice(t)
		p.eventually(t, "the refused refresh", func() bool { r, _ := cmds.counts(); return r > 0 && len(log.icdWarnings()) > 0 })
		cmds.setRefuse(false)
		reg, _ := p.icd().Registration()
		if !bytes.Equal(p.deviceKey(t), oldKey) || !bytes.Equal(reg.Key, oldKey) || reg.PendingKey == nil {
			t.Fatalf("after the refusal: device key changed %v, registration %+v", !bytes.Equal(p.deviceKey(t), oldKey), reg)
		}
		pending := reg.PendingKey
		if fed := p.fedPeer(t); !bytes.Equal(fed.PendingKey, pending) {
			t.Fatal("the fabric feed does not hold the pending key")
		}
		refreshed := &counter{}
		p.icd().OnKeyRefreshed(refreshed.inc)
		p.wakeDevice(t)
		p.eventually(t, "the refresh offered again", func() bool {
			r, _ := p.icd().Registration()
			return refreshed.get() > 0 && r.PendingKey == nil
		})
		after, _ := p.icd().Registration()
		if !bytes.Equal(p.deviceKey(t), pending) || !bytes.Equal(after.Key, pending) {
			t.Fatal("the device or the client does not hold the pending key offered again")
		}
	})

	t.Run("decrypts Check-Ins under the pending key after a controller restart", func(t *testing.T) {
		t.Parallel()
		// :1020-1051: the refresh's response is lost (every datagram of the
		// device dropped once its key changed), the controller restarts
		// with the pending key stored, and a Check-In under the device's
		// new key confirms it.
		p := newICDPair(t, icdPairOptions{controller: fabrictest.ControllerOptions{
			Registry: func(rc *client.RegistryConfig) { rc.AutoStart = true },
		}})
		p.register(t, uncovered)
		p.requestRefreshOnNextCheckIn(t)
		oldKey := p.deviceKey(t)
		dev := p.pair.Device.Addr()
		p.site.Net().Filter(func(d fabrictest.Datagram) bool {
			return !sameUDP(d.From, dev) || bytes.Equal(p.deviceKeyNoT(), oldKey)
		})
		p.wakeDevice(t)
		p.eventually(t, "the pending key", func() bool { r, _ := p.icd().Registration(); return r.PendingKey != nil })
		p.site.Net().Filter(nil)
		n := p.restarted(t)
		p.run(t, p.pair.Controller.Registry().Start)
		reg, _ := n.ICD().Registration()
		a, _ := n.PeerAddress()
		fed, ok := client.ICDTable(p.pair.Controller.Registry(), a.FabricIndex).PeerFor(a.NodeID)
		if !ok || !bytes.Equal(fed.PendingKey, p.deviceKey(t)) || !bytes.Equal(reg.PendingKey, p.deviceKey(t)) {
			t.Fatalf("restored registration %+v, fed %v", reg, ok)
		}
		p.eventually(t, "the restored subscription", n.SubscriptionActive)
		got := &checkIns{}
		n.ICD().OnCheckIn(got.record)
		for i := 0; got.n() == 0; i++ {
			// The device's Check-In back-off skips a wake after its
			// unanswered Check-Ins (:1043).
			if i == 4 {
				t.Fatal("no Check-In under the pending key")
			}
			p.wakeDevice(t)
		}
		p.eventually(t, "the pending key confirmed", func() bool { r, _ := n.ICD().Registration(); return r.PendingKey == nil })
		if r, _ := n.ICD().Registration(); !bytes.Equal(r.Key, p.deviceKey(t)) {
			t.Fatal("the confirmed key is not the device's")
		}
	})

	t.Run("keeps the registration when unregister() gives up on a peer that does not wake", func(t *testing.T) {
		t.Parallel()
		// :1529-1543.
		p, _ := sleepingLITPeer(t)
		ending := start(func() error { return p.icd().Unregister(parityCtx(t)) })
		p.advance(t, 2*time.Hour)
		p.eventually(t, "the unregister given up", ending.done.Load)
		var asleep *client.ICDPeerAsleepError
		if !errors.As(ending.error(), &asleep) {
			t.Fatalf("unregister: %v, want ICDPeerAsleepError", ending.error())
		}
		if !p.icd().Registered() || len(p.deviceServerRegistrations(t)) != 1 {
			t.Fatal("the registration was not kept")
		}
		if err := p.do(t, func(ctx context.Context) error { return p.icd().Register(ctx, client.ICDRegisterOptions{}) }); !errors.Is(err, client.ErrICDOperation) {
			t.Fatalf("register after the give-up: %v, want already registered", err)
		}
	})

	t.Run("keeps the stored registration when closing abandons an unregister() parked on a sleeping peer", func(t *testing.T) {
		t.Parallel()
		// :1585-1599.
		p, _ := sleepingLITPeer(t)
		reg, _ := p.icd().Registration()
		ending := start(func() error { return p.icd().Unregister(parityCtx(t)) })
		p.settle(t)
		n := p.restarted(t)
		p.eventually(t, "the abandoned unregister", ending.done.Load)
		restored, ok := n.ICD().Registration()
		if !ok || !n.ICD().Registered() || !bytes.Equal(restored.Key, reg.Key) {
			t.Fatalf("restored %+v (%v), want the registration kept", restored, ok)
		}
	})

	t.Run("forget() ends an unregister() parked on a sleeping peer", func(t *testing.T) {
		t.Parallel()
		// :1636-1648.
		p, cmds := sleepingLITPeer(t)
		ending := start(func() error { return p.icd().Unregister(parityCtx(t)) })
		p.settle(t)
		if err := p.do(t, func(ctx context.Context) error { return p.icd().Forget(ctx) }); err != nil {
			t.Fatal(err)
		}
		p.eventually(t, "the parked unregister ended", ending.done.Load)
		if ending.error() == nil {
			t.Fatal("the parked unregister succeeded")
		}
		if p.icd().Registered() {
			t.Fatal("still registered")
		}
		p.silence(false)
		p.wakeDevice(t)
		if _, u := cmds.counts(); u != 0 {
			t.Fatalf("%d UnregisterClient after the forget, want none", u)
		}
	})

	t.Run("clears the registration when closing abandons an unregister() whose request went out", func(t *testing.T) {
		t.Parallel()
		// :1677-1694.
		p := newICDPair(t, icdPairOptions{})
		p.register(t, client.ICDRegisterOptions{})
		p.silence(true)
		ending := start(func() error { return p.icd().Unregister(parityCtx(t)) })
		p.eventually(t, "the device's registration removed", func() bool { return len(p.deviceServerRegistrations(t)) == 0 })
		n := p.restarted(t)
		p.eventually(t, "the abandoned unregister", ending.done.Load)
		if r, ok := n.ICD().Registration(); ok || n.ICD().Registered() || r.Key != nil {
			t.Fatalf("restored %+v (%v), want no registration", r, ok)
		}
	})

	t.Run("forget() after unregister() cleared locally leaves its UnregisterClient running", func(t *testing.T) {
		t.Parallel()
		// :1803-1818.
		p := newICDPair(t, icdPairOptions{})
		p.register(t, client.ICDRegisterOptions{})
		p.silence(true)
		ending := start(func() error { return p.icd().Unregister(parityCtx(t)) })
		p.eventually(t, "the local clear", func() bool { r, _ := p.icd().Registration(); return r.Key == nil })
		if err := p.do(t, func(ctx context.Context) error { return p.icd().Forget(ctx) }); err != nil {
			t.Fatal(err)
		}
		p.silence(false)
		p.eventually(t, "the unregister", ending.done.Load)
		if n := len(p.deviceServerRegistrations(t)); n != 0 {
			t.Fatalf("%d registrations on the device, want none", n)
		}
	})

	t.Run("joins a second unregister() of a sleeping peer and sends one UnregisterClient", func(t *testing.T) {
		t.Parallel()
		// :1856-1870.
		p, cmds := sleepingLITPeer(t)
		first := start(func() error { return p.icd().Unregister(parityCtx(t)) })
		second := start(func() error { return p.icd().Unregister(parityCtx(t)) })
		p.settle(t)
		if first.done.Load() || second.done.Load() {
			t.Fatal("an unregister ended while the peer sleeps")
		}
		p.silence(false)
		p.wakeDevice(t)
		p.eventually(t, "both unregisters", func() bool { return first.done.Load() && second.done.Load() })
		if first.error() != nil || second.error() != nil {
			t.Fatalf("unregister: %v / %v", first.error(), second.error())
		}
		if _, u := cmds.counts(); u != 1 {
			t.Fatalf("%d UnregisterClient, want one", u)
		}
	})

	t.Run("stores the offset of an old-key Check-In that arrives while a refresh waits", func(t *testing.T) {
		t.Parallel()
		// :1872-1893: every datagram to the device is dropped while a key is
		// pending, so the refresh waits; a second Check-In under the old key
		// moves the stored offset to the fabric feed's.
		p, log := icdLogPair(t)
		p.register(t, uncovered)
		p.requestRefreshOnNextCheckIn(t)
		dev := p.pair.Device.Addr()
		p.site.Net().Filter(func(d fabrictest.Datagram) bool {
			r, _ := p.icd().Registration()
			return !sameUDP(d.To, dev) || r.PendingKey == nil
		})
		p.wakeDevice(t)
		p.eventually(t, "the pending key", func() bool { r, _ := p.icd().Registration(); return r.PendingKey != nil })
		before, _ := p.icd().Registration()
		p.wakeDevice(t)
		p.eventually(t, "the old-key Check-In stored", func() bool {
			r, _ := p.icd().Registration()
			return p.fedPeer(t).LastOffset != before.LastOffset && r.LastOffset != before.LastOffset
		})
		r, _ := p.icd().Registration()
		if r.LastOffset != p.fedPeer(t).LastOffset || r.PendingKey == nil {
			t.Fatalf("registration %+v, fed offset %d", r, p.fedPeer(t).LastOffset)
		}
		if w := log.icdWarnings(); len(w) != 0 {
			t.Fatalf("warnings %q, want none", w)
		}
	})

	t.Run("recreates the running subscription on the first registration-induced SIT→LIT flip", func(t *testing.T) {
		t.Parallel()
		// :2558-2594: a DSLS peer subscribed while SIT holds no
		// wakefulness; its runtime SIT→LIT flip auto-registers, and the
		// running subscription is recreated under a new id, active.
		p := newICDPair(t, icdPairOptions{cfg: icdLITConfig(true)})
		p.settle(t)
		if p.icd().Registered() || p.icd().Wakefulness() != nil {
			t.Fatal("registered or fed before the flip")
		}
		before := p.subscriptionID()
		if err := p.device().ICD().SetOperatingMode(node.ICDModeLIT); err != nil {
			t.Fatal(err)
		}
		p.eventually(t, "the auto-registration", p.icd().Registered)
		for range 5 {
			p.advance(t, 200*time.Millisecond)
		}
		p.eventually(t, "the recreated subscription", func() bool {
			w := p.icd().Wakefulness()
			id := p.subscriptionID()
			return w != nil && w.RequiresAwait() && p.node.SubscriptionActive() && id != before && id != interaction.NoSubscription
		})
	})
}

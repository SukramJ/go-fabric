// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of matter.js packages/node/test/behavior/system/icd/IcdClientTest.ts
// at 862ad75f: the controller's ICD client against a fabrictest device with
// node.Config.ICD — registration, the LIT auto-registration, Check-In
// receipt, the key refresh, unregister, forget, stay-active, the feature
// getters, the restore after a controller restart, availability, the awake
// getter, decommission, the sustained subscription's routing, the peer's
// timings, the MRP idle-interval floor and the address monitor.
//
// matter.js's MockSite pair is a commissioned fabrictest pair through the
// controller's registry (controller.peers is client.Registry, peer1 its
// node, subscribedPeer the node with its default subscription active); the
// device's IcdManagementServer variants are node.ICDConfig (CIP-only is SIT,
// the LIT server is Mode LIT, DSLS adds DynamicSITLIT); FabricManager.for(i).icd
// is the registry's ICD peer table; deviceLink(device).silence() a network
// filter dropping the device's datagrams; MockTime.advance the site's clock.
// Go's keys come from crypto/rand, so matter.js's mintDistinctKeys has no
// counterpart. What has no Go subject is skipped with its reason in the
// register: the "ICD state held by another transaction" group (the Go
// client has no transaction model), a failing client store, and the
// operation interleavings at matter.js's await points.
package client_test

import (
	"bytes"
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	icddef "github.com/SukramJ/go-fabric/cluster/spec/icdmanagement"
	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/icd"
	"github.com/SukramJ/go-fabric/node"
)

// icdLITConfig is icd-helpers.ts LIT_CONFIG (threshold 5 s, idle 3600 s,
// active 1 s, back-off 3600 s) on the LIT server; the trigger LIT requires.
func icdLITConfig(dsls bool) node.ICDConfig {
	return node.ICDConfig{
		Mode: node.ICDModeLIT, ActiveModeThreshold: 5 * time.Second, IdleModeDuration: 3600 * time.Second,
		ActiveModeDuration: time.Second, MaximumCheckInBackoff: 3600 * time.Second,
		UserActiveModeTrigger: &node.ICDUserActiveModeTrigger{Hint: icddef.UserActiveModeTriggerPowerCycle},
		DynamicSITLIT:         dsls,
	}
}

// icdPair is a commissioned pair of a device with cfg as its ICD
// configuration; forceLIT puts a DSLS device in LIT before commissioning
// (IcdClientTest.ts:73-83, litOperatingPair).
type icdPair struct {
	*clientPair
	reg *client.Registry
}

type icdPairOptions struct {
	cfg        node.ICDConfig
	forceLIT   bool
	noICD      bool
	commission client.CommissionOptions
	configure  func(*node.Config)
}

func newICDPair(t *testing.T, o icdPairOptions) *icdPair {
	t.Helper()
	site := quietSite(t)
	cp := &clientPair{site: site}
	cfg := o.cfg
	topo := fabrictest.Topology{
		Configure: func(c *node.Config) {
			if !o.noICD {
				c.ICD = &cfg
			}
			if o.configure != nil {
				o.configure(c)
			}
		},
		Prepare: func(n *node.Node) {
			if o.forceLIT {
				if err := n.ICD().SetOperatingMode(node.ICDModeLIT); err != nil {
					t.Error(err)
				}
			}
		},
	}
	cp.pair = site.UncommissionedPair(fabrictest.PairOptions{Device: topo})
	n, err := cp.pair.Controller.CommissionNode(parityCtx(t), cp.pair.Device, o.commission)
	if err != nil {
		t.Fatal(err)
	}
	cp.node = n
	if o.commission.AutoSubscribe == nil || *o.commission.AutoSubscribe {
		cp.eventually(t, "the default subscription", n.SubscriptionActive)
	}
	return &icdPair{clientPair: cp, reg: cp.pair.Controller.Registry()}
}

// litOperatingPair is IcdClientTest.ts:73-83: a DSLS device forced into LIT
// before commissioning, auto-registered.
func litOperatingPair(t *testing.T) *icdPair {
	t.Helper()
	p := newICDPair(t, icdPairOptions{cfg: icdLITConfig(true), forceLIT: true})
	p.eventually(t, "the LIT auto-registration", p.node.ICD().Registered)
	return p
}

func (p *icdPair) icd() *client.ICDClient { return p.node.ICD() }

func (p *icdPair) peerAddr(t *testing.T) client.PeerAddress {
	t.Helper()
	a, ok := p.node.PeerAddress()
	if !ok {
		t.Fatal("not commissioned")
	}
	return a
}

func (p *icdPair) table(t *testing.T) *icd.PeerTable {
	t.Helper()
	return client.ICDTable(p.reg, p.peerAddr(t).FabricIndex)
}

func (p *icdPair) fedPeer(t *testing.T) icd.Peer {
	t.Helper()
	e, ok := p.table(t).PeerFor(p.peerAddr(t).NodeID)
	if !ok {
		t.Fatal("ICD peer not fed")
	}
	return e
}

func (p *icdPair) wakefulness(t *testing.T) *icd.PeerWakefulness {
	t.Helper()
	return p.table(t).WakefulnessFor(p.peerAddr(t).NodeID)
}

// run runs a call of the client under the site.
func (p *icdPair) do(t *testing.T, fn func(ctx context.Context) error) error {
	t.Helper()
	var err error
	if rerr := p.site.Run(parityCtx(t), func(ctx context.Context) error { err = fn(ctx); return nil }); rerr != nil {
		t.Fatal(rerr)
	}
	return err
}

func (p *icdPair) register(t *testing.T, o client.ICDRegisterOptions) {
	t.Helper()
	if err := p.do(t, func(ctx context.Context) error { return p.icd().Register(ctx, o) }); err != nil {
		t.Fatal(err)
	}
}

func subject(s uint64) *uint64 { return &s }

// uncovered is the monitored subject IcdClientTest registers under so the
// device's suppression does not fire (:165-173, 0xabcd).
var uncovered = client.ICDRegisterOptions{MonitoredSubject: subject(0xabcd)}

func (p *icdPair) device() *node.Node { return p.pair.Device.Node() }

func (p *icdPair) deviceServerRegistrations(t *testing.T) []icd.Registration {
	t.Helper()
	fabrics, err := p.device().FabricInfos(parityCtx(t))
	if err != nil || len(fabrics) == 0 {
		t.Fatalf("device fabrics %v %v", fabrics, err)
	}
	return p.device().Root().ICDManagement.Registrations(fabrics[0].FabricIndex)
}

// wakeDevice is icd-helpers.ts wakeDevice: one idle→active wake, its
// Check-In pass run to completion.
func (p *icdPair) wakeDevice(t *testing.T) {
	t.Helper()
	h := p.device().ICD()
	h.EnterIdleMode()
	h.RequestActiveMode()
	if err := p.site.Settle(parityCtx(t)); err != nil {
		t.Fatal(err)
	}
}

// silence drops the device's datagrams (icd-helpers.ts deviceLink).
func (p *icdPair) silence(silent bool) {
	if !silent {
		p.site.Net().Filter(nil)
		return
	}
	dev := p.pair.Device.Addr()
	p.site.Net().Filter(func(d fabrictest.Datagram) bool { return !sameUDP(d.From, dev) })
}

func sameUDP(a, b *net.UDPAddr) bool {
	return a != nil && b != nil && a.IP.Equal(b.IP) && a.Port == b.Port
}

// silentPastWindow is IcdClientTest.ts:375 (SILENT_PAST_WINDOW).
const silentPastWindow = 70 * time.Minute

type checkIns struct {
	mu  sync.Mutex
	got []client.ICDCheckIn
}

func (c *checkIns) record(in client.ICDCheckIn) {
	c.mu.Lock()
	c.got = append(c.got, in)
	c.mu.Unlock()
}

func (c *checkIns) n() int { c.mu.Lock(); defer c.mu.Unlock(); return len(c.got) }

func (c *checkIns) last() client.ICDCheckIn {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.got[len(c.got)-1]
}

type counter struct {
	mu sync.Mutex
	v  int
}

func (c *counter) inc()     { c.mu.Lock(); c.v++; c.mu.Unlock() }
func (c *counter) get() int { c.mu.Lock(); defer c.mu.Unlock(); return c.v }

func TestIcdClientParity(t *testing.T) {
	t.Run("auto-installs on a ClientNode when the peer exposes IcdManagement", func(t *testing.T) {
		p := newICDPair(t, icdPairOptions{})
		if p.icd() == nil {
			t.Fatal("no ICD client")
		}
	})
	t.Run("defaults to unregistered with no Check-In timestamp", func(t *testing.T) {
		p := newICDPair(t, icdPairOptions{})
		if _, ok := p.icd().LastCheckInAt(); p.icd().Registered() || ok {
			t.Fatal("registered or checked in")
		}
	})
	t.Run("exposes isRegistered reflecting state.registered", func(t *testing.T) {
		p := newICDPair(t, icdPairOptions{})
		if _, ok := p.icd().Registration(); ok || p.icd().Registered() {
			t.Fatal("registered")
		}
	})

	t.Run("register", func(t *testing.T) {
		t.Run("registers the controller on the peer and records the counter baseline", func(t *testing.T) {
			p := newICDPair(t, icdPairOptions{})
			p.register(t, client.ICDRegisterOptions{})
			reg, ok := p.icd().Registration()
			if !ok || reg.CounterStart != p.device().Root().ICDManagement.Counter().Value() || reg.LastOffset != 0 || reg.Key == nil {
				t.Fatalf("registration %+v", reg)
			}
			regs := p.deviceServerRegistrations(t)
			if len(regs) != 1 || regs[0].CheckInNodeID != p.pair.Controller.Fabric().Identity.NodeID {
				t.Fatalf("device registrations %+v", regs)
			}
			if !p.table(t).HasPeers() {
				t.Fatal("the receive path is not armed")
			}
		})
		t.Run("issues RegisterClient with a 16-byte key and the requested monitored subject", func(t *testing.T) {
			p := newICDPair(t, icdPairOptions{})
			p.register(t, uncovered)
			reg, _ := p.icd().Registration()
			regs := p.deviceServerRegistrations(t)
			if len(reg.Key) != 16 || len(regs) != 1 || regs[0].MonitoredSubject != 0xabcd || regs[0].ClientType != icd.ClientPermanent ||
				regs[0].CheckInNodeID != p.pair.Controller.Fabric().Identity.NodeID {
				t.Fatalf("key %d bytes, device %+v", len(reg.Key), regs)
			}
		})
		t.Run("rejects a multi-admin peer and succeeds when allowMultiAdmin is set", func(t *testing.T) {
			p := newICDPair(t, icdPairOptions{})
			// A second administrator under another vendor: a second controller
			// commissions the device onto its fabric.
			second := p.site.AddController(fabrictest.ControllerOptions{FabricID: 2, VendorID: 0xfff2})
			if err := p.pair.Device.OpenWindow(parityCtx(t), 3*time.Minute); err != nil {
				t.Fatal(err)
			}
			if _, err := second.Commission(parityCtx(t), p.pair.Device, fabrictest.CommissionOptions{}); err != nil {
				t.Fatal(err)
			}
			err := p.do(t, func(ctx context.Context) error { return p.icd().Register(ctx, client.ICDRegisterOptions{}) })
			var multi *client.ICDMultiAdminError
			if !errors.As(err, &multi) || !containsVendor(multi.AdminVendorIDs, 0xfff2) || p.icd().Registered() {
				t.Fatalf("Register = %v", err)
			}
			p.register(t, client.ICDRegisterOptions{AllowMultiAdmin: true})
			if !p.icd().Registered() {
				t.Fatal("not registered")
			}
		})
		t.Run("registers against a CIP-capable peer (the only ICD shape this harness can build)", func(t *testing.T) {
			p := newICDPair(t, icdPairOptions{})
			p.register(t, client.ICDRegisterOptions{})
			if !p.icd().Registered() || len(p.deviceServerRegistrations(t)) != 1 {
				t.Fatal("not registered")
			}
		})
		t.Run("refuses register() without an established subscription", func(t *testing.T) {
			off := false
			p := newICDPair(t, icdPairOptions{commission: client.CommissionOptions{AutoSubscribe: &off}})
			err := p.do(t, func(ctx context.Context) error { return p.icd().Register(ctx, client.ICDRegisterOptions{}) })
			if !errors.Is(err, client.ErrICDOperation) || p.icd().Registered() {
				t.Fatalf("Register = %v", err)
			}
		})
		t.Run("throws when already registered", func(t *testing.T) {
			p := newICDPair(t, icdPairOptions{})
			p.register(t, client.ICDRegisterOptions{})
			err := p.do(t, func(ctx context.Context) error { return p.icd().Register(ctx, client.ICDRegisterOptions{}) })
			if !errors.Is(err, client.ErrICDOperation) || !p.icd().Registered() {
				t.Fatalf("Register = %v", err)
			}
		})
	})

	t.Run("auto-register on LIT", func(t *testing.T) {
		t.Run("auto-registers a LIT-operating peer at commissioning without an explicit register()", func(t *testing.T) {
			p := litOperatingPair(t)
			if reg, ok := p.icd().Registration(); !ok || reg.Key == nil {
				t.Fatal("not registered")
			}
		})
		t.Run("does NOT auto-register a LIT-advertising peer below spec 1.4.0", func(t *testing.T) {
			t.Skip("the device's BasicInformation SpecificationVersion is fixed at 1.6.1 (cluster.SpecificationVersion); a node of this module cannot report 1.3.0 — litSupported's version gate is held by the physical-properties port (TestClientNodePhysicalPropertiesIcdParity) and TestProperties")
		})
		t.Run("does NOT auto-register a DSLS peer operating in SIT", func(t *testing.T) {
			p := newICDPair(t, icdPairOptions{cfg: icdLITConfig(true)})
			p.advance(t, 2*time.Second)
			if p.icd().Registered() {
				t.Fatal("registered")
			}
		})
		t.Run("does not auto-register when the fresh operating mode is Sit despite a persisted Lit", func(t *testing.T) {
			t.Skip("matter.js seeds the client's cache with a stale LIT (seedPeerCache); the Go image has no seam to plant a value the peer did not report, and the fresh read before registering is held by the SIT case above")
		})
		t.Run("auto-registers on a runtime SIT->LIT flip", func(t *testing.T) {
			p := newICDPair(t, icdPairOptions{cfg: icdLITConfig(true)})
			if p.icd().Registered() {
				t.Fatal("registered before the flip")
			}
			if err := p.device().ICD().SetOperatingMode(node.ICDModeLIT); err != nil {
				t.Fatal(err)
			}
			p.eventually(t, "the auto-registration", p.icd().Registered)
		})
	})

	t.Run("check-in receipt", func(t *testing.T) {
		t.Run("receives a real device Check-In, emits checkedIn, and advances counter state", func(t *testing.T) {
			p := newICDPair(t, icdPairOptions{})
			p.register(t, uncovered)
			threshold := uint16(300)
			before := p.device().Root().ICDManagement.Counter().Value()
			got := &checkIns{}
			p.icd().OnCheckIn(got.record)
			p.wakeDevice(t)
			p.eventually(t, "the Check-In", func() bool { return got.n() > 0 })
			in := got.last()
			if in.ActiveModeThreshold != threshold || in.Counter != before+1 {
				t.Fatalf("Check-In %+v, counter before %d", in, before)
			}
			reg, _ := p.icd().Registration()
			if _, ok := p.icd().LastCheckInAt(); !ok || reg.LastOffset != in.Counter-reg.CounterStart || reg.LastOffset == 0 {
				t.Fatalf("registration %+v", reg)
			}
		})
		t.Run("drops a replayed Check-In without emitting checkedIn or advancing offset", func(t *testing.T) {
			p := newICDPair(t, icdPairOptions{})
			p.register(t, uncovered)
			got := &checkIns{}
			p.icd().OnCheckIn(got.record)
			p.wakeDevice(t)
			p.eventually(t, "the first Check-In", func() bool { return got.n() == 1 })
			reg, _ := p.icd().Registration()
			at, _ := p.icd().LastCheckInAt()
			// The replay floor far above the near-term counters.
			client.SetICDFedOffset(p.node, 0x10000000)
			p.wakeDevice(t)
			p.advance(t, 2*time.Second)
			after, _ := p.icd().Registration()
			at2, _ := p.icd().LastCheckInAt()
			if got.n() != 1 || after.LastOffset != reg.LastOffset || !at2.Equal(at) {
				t.Fatalf("%d Check-Ins, offset %d (was %d)", got.n(), after.LastOffset, reg.LastOffset)
			}
		})
	})

	t.Run("key refresh", func(t *testing.T) {
		t.Run("refreshes the key when a Check-In counter crosses the refresh threshold", func(t *testing.T) {
			p := newICDPair(t, icdPairOptions{})
			p.register(t, uncovered)
			original, _ := p.icd().Registration()
			p.requestRefreshOnNextCheckIn(t)
			w := p.wakefulness(t)
			refreshed := &counter{}
			p.icd().OnKeyRefreshed(refreshed.inc)
			p.wakeDevice(t)
			p.eventually(t, "the key refresh", func() bool {
				r, _ := p.icd().Registration()
				return refreshed.get() > 0 && r.PendingKey == nil && !bytes.Equal(r.Key, original.Key)
			})
			reg, _ := p.icd().Registration()
			if reg.LastOffset != 0 || reg.CounterStart == original.CounterStart || len(p.deviceServerRegistrations(t)) != 1 || p.wakefulness(t) != w {
				t.Fatalf("registration %+v", reg)
			}
			got := &checkIns{}
			p.icd().OnCheckIn(got.record)
			p.wakeDevice(t)
			p.eventually(t, "a Check-In under the new key", func() bool { return got.n() > 0 })
			after, _ := p.icd().Registration()
			if got.last().Counter <= after.CounterStart || after.LastOffset == 0 {
				t.Fatalf("Check-In %+v, registration %+v", got.last(), after)
			}
		})
		t.Run("reports the refreshed starting counter once it is committed", func(t *testing.T) {
			p := newICDPair(t, icdPairOptions{})
			p.register(t, uncovered)
			original, _ := p.icd().Registration()
			pulledBack := p.requestRefreshOnNextCheckIn(t)
			var mu sync.Mutex
			var committed *client.ICDRegistration
			p.icd().OnKeyRefreshed(func() {
				r, _ := p.icd().Registration()
				mu.Lock()
				committed = &r
				mu.Unlock()
			})
			p.wakeDevice(t)
			p.eventually(t, "the refreshed key", func() bool { mu.Lock(); defer mu.Unlock(); return committed != nil })
			mu.Lock()
			defer mu.Unlock()
			if committed.CounterStart == pulledBack || committed.CounterStart == original.CounterStart {
				t.Fatalf("counter start %d (pulled back %d, original %d)", committed.CounterStart, pulledBack, original.CounterStart)
			}
		})
	})

	t.Run("key refresh outcome", func(t *testing.T) {
		t.Run("takes the new key from the first Check-In when the response was lost", func(t *testing.T) {
			p := newICDPair(t, icdPairOptions{})
			p.register(t, uncovered)
			p.requestRefreshOnNextCheckIn(t)
			oldKey := p.deviceKey(t)
			dev := p.pair.Device.Addr()
			// Every datagram of the device once its key changed is dropped: the
			// RegisterClientResponse of the refresh is lost.
			p.site.Net().Filter(func(d fabrictest.Datagram) bool {
				return !sameUDP(d.From, dev) || bytes.Equal(p.deviceKeyNoT(), oldKey)
			})
			got := &checkIns{}
			p.icd().OnCheckIn(got.record)
			p.wakeDevice(t)
			p.eventually(t, "the device's new key", func() bool { return !bytes.Equal(p.deviceKeyNoT(), oldKey) })
			lastOld := got.last().Counter
			reg, _ := p.icd().Registration()
			if !bytes.Equal(reg.Key, oldKey) || !bytes.Equal(reg.PendingKey, p.deviceKey(t)) {
				t.Fatalf("registration %+v", reg)
			}
			p.site.Net().Filter(nil)
			p.wakeDevice(t)
			p.eventually(t, "the promotion", func() bool {
				r, _ := p.icd().Registration()
				return r.PendingKey == nil && bytes.Equal(r.Key, p.deviceKeyNoT())
			})
			after, _ := p.icd().Registration()
			if after.CounterStart != lastOld || after.LastOffset != got.last().Counter-lastOld {
				t.Fatalf("registration %+v, last old %d, last %d", after, lastOld, got.last().Counter)
			}
		})
		t.Run("offers the same pending key again after the request was lost", func(t *testing.T) {
			p := newICDPair(t, icdPairOptions{})
			p.register(t, uncovered)
			p.requestRefreshOnNextCheckIn(t)
			oldKey := p.deviceKey(t)
			dev := p.pair.Device.Addr()
			// Every datagram to the device while a key is pending is dropped:
			// the refresh's RegisterClient never arrives.
			p.site.Net().Filter(func(d fabrictest.Datagram) bool {
				r, _ := p.icd().Registration()
				return !sameUDP(d.To, dev) || r.PendingKey == nil
			})
			p.wakeDevice(t)
			p.eventually(t, "the pending key", func() bool { r, _ := p.icd().Registration(); return r.PendingKey != nil })
			reg, _ := p.icd().Registration()
			pending := reg.PendingKey
			p.advance(t, 30*time.Second) // the refresh's RegisterClient runs out of retransmissions
			if !bytes.Equal(p.deviceKey(t), oldKey) {
				t.Fatal("the device took the key")
			}
			p.site.Net().Filter(nil)
			// The pending key is offered again on a check-in once the
			// device is reachable. The blackout may have made the device
			// reap the session its unacknowledged reports ran on; the
			// refresh on that session then fails and waits for the next
			// check-in, the peer reconnecting meanwhile — so the device
			// checks in until the refresh lands.
			refreshed := func() bool {
				r, _ := p.icd().Registration()
				return r.PendingKey == nil && bytes.Equal(r.Key, pending)
			}
			for i := 0; !refreshed(); i++ {
				if i == 4 {
					t.Fatal("the pending key was not offered again")
				}
				p.wakeDevice(t)
				p.advance(t, 20*time.Second)
			}
			if !bytes.Equal(p.deviceKey(t), pending) {
				t.Fatal("the device holds another key")
			}
		})
	})

	t.Run("unregister", func(t *testing.T) {
		t.Run("removes the peer registration, clears state, drops the fabric feed, and emits unregistered", func(t *testing.T) {
			p := newICDPair(t, icdPairOptions{})
			p.register(t, client.ICDRegisterOptions{})
			unregistered := &counter{}
			p.icd().OnUnregistered(unregistered.inc)
			if err := p.do(t, func(ctx context.Context) error { return p.icd().Unregister(ctx) }); err != nil {
				t.Fatal(err)
			}
			if p.icd().Registered() || unregistered.get() != 1 || len(p.deviceServerRegistrations(t)) != 0 || p.table(t).HasPeers() {
				t.Fatalf("registered %v, %d events", p.icd().Registered(), unregistered.get())
			}
		})
		t.Run("clears the stored registration when the peer cannot be reached", func(t *testing.T) {
			p := newICDPair(t, icdPairOptions{})
			p.register(t, client.ICDRegisterOptions{})
			dev := p.pair.Device.Addr()
			p.site.Net().Filter(func(d fabrictest.Datagram) bool { return !sameUDP(d.To, dev) })
			if err := p.do(t, func(ctx context.Context) error { return p.icd().Unregister(ctx) }); err == nil {
				t.Fatal("Unregister succeeded")
			}
			if p.icd().Registered() {
				t.Fatal("registered")
			}
		})
		t.Run("is a no-op when not registered", func(t *testing.T) {
			p := newICDPair(t, icdPairOptions{})
			if err := p.do(t, func(ctx context.Context) error { return p.icd().Unregister(ctx) }); err != nil || p.icd().Registered() {
				t.Fatal(err)
			}
		})
	})

	t.Run("forget", func(t *testing.T) {
		t.Run("clears local registration and drops the fabric feed WITHOUT a peer UnregisterClient", func(t *testing.T) {
			p := newICDPair(t, icdPairOptions{})
			p.register(t, client.ICDRegisterOptions{})
			unregistered := &counter{}
			p.icd().OnUnregistered(unregistered.inc)
			if err := p.do(t, func(ctx context.Context) error { return p.icd().Forget(ctx) }); err != nil {
				t.Fatal(err)
			}
			if p.icd().Registered() || p.icd().Available() || unregistered.get() != 1 || p.table(t).HasPeers() || len(p.deviceServerRegistrations(t)) != 1 {
				t.Fatal("not forgotten locally alone")
			}
		})
		t.Run("is a no-op when not registered", func(t *testing.T) {
			p := newICDPair(t, icdPairOptions{})
			if err := p.do(t, func(ctx context.Context) error { return p.icd().Forget(ctx) }); err != nil || p.icd().Registered() {
				t.Fatal(err)
			}
		})
	})

	t.Run("stayActive", func(t *testing.T) {
		t.Run("requests a stay-active window and returns the promised duration", func(t *testing.T) {
			p := newICDPair(t, icdPairOptions{})
			var promised time.Duration
			if err := p.do(t, func(ctx context.Context) (err error) {
				promised, err = p.icd().StayActive(ctx, time.Minute)
				return err
			}); err != nil || promised <= 0 {
				t.Fatalf("promised %v, %v", promised, err)
			}
		})
	})

	t.Run("feature getters", func(t *testing.T) {
		t.Run("reports peerSupportsLit false for a CIP-only peer", func(t *testing.T) {
			if newICDPair(t, icdPairOptions{}).icd().PeerSupportsLIT() {
				t.Fatal("LIT")
			}
		})
		t.Run("reports peerSupportsLit true for a LIT peer", func(t *testing.T) {
			if !newICDPair(t, icdPairOptions{cfg: icdLITConfig(false)}).icd().PeerSupportsLIT() {
				t.Fatal("no LIT")
			}
		})
	})

	t.Run("init restore", func(t *testing.T) {
		t.Run("re-arms the Check-In receive path after a controller restart", func(t *testing.T) {
			p := newICDPair(t, icdPairOptions{})
			p.register(t, client.ICDRegisterOptions{})
			if err := p.pair.Controller.Restart(parityCtx(t)); err != nil {
				t.Fatal(err)
			}
			reg := p.pair.Controller.Registry()
			n := reg.Get(p.node.ID())
			if n == nil || !n.ICD().Registered() {
				t.Fatal("not restored")
			}
			a, _ := n.PeerAddress()
			if !client.ICDTable(reg, a.FabricIndex).HasPeers() {
				t.Fatal("the receive path is not armed")
			}
		})
		t.Run("restores the replay floor (lastOffset) after a controller restart", func(t *testing.T) {
			p := newICDPair(t, icdPairOptions{})
			p.register(t, uncovered)
			got := &checkIns{}
			p.icd().OnCheckIn(got.record)
			p.wakeDevice(t)
			p.eventually(t, "the Check-In", func() bool { return got.n() > 0 })
			persisted, _ := p.icd().Registration()
			if persisted.LastOffset == 0 {
				t.Fatal("no offset")
			}
			if err := p.pair.Controller.Restart(parityCtx(t)); err != nil {
				t.Fatal(err)
			}
			reg := p.pair.Controller.Registry()
			n := reg.Get(p.node.ID())
			r, _ := n.ICD().Registration()
			a, _ := n.PeerAddress()
			e, ok := client.ICDTable(reg, a.FabricIndex).PeerFor(a.NodeID)
			if r.LastOffset != persisted.LastOffset || !ok || e.LastOffset != persisted.LastOffset {
				t.Fatalf("restored %d / %d, want %d", r.LastOffset, e.LastOffset, persisted.LastOffset)
			}
		})
	})

	t.Run("availability", func(t *testing.T) {
		reRegister := func(t *testing.T, p *icdPair) {
			t.Helper()
			if err := p.do(t, func(ctx context.Context) error { return p.icd().Unregister(ctx) }); err != nil {
				t.Fatal(err)
			}
			if err := p.do(t, func(ctx context.Context) error {
				err := p.icd().Register(ctx, uncovered)
				if errors.Is(err, client.ErrICDOperation) && p.icd().Registered() {
					// The auto-registration came first: re-register under the subject.
					if err := p.icd().Unregister(ctx); err != nil {
						return err
					}
					return p.icd().Register(ctx, uncovered)
				}
				return err
			}); err != nil {
				t.Fatal(err)
			}
		}
		t.Run("seeds available true on register and expires it once the peer goes silent", func(t *testing.T) {
			p := litOperatingPair(t)
			reRegister(t, p)
			if !p.icd().Available() {
				t.Fatal("unavailable after register")
			}
			p.silence(true)
			p.advance(t, silentPastWindow)
			if p.icd().Available() || p.wakefulness(t).Available() {
				t.Fatal("available while silent")
			}
		})
		t.Run("restores available true on a check-in after expiry", func(t *testing.T) {
			p := litOperatingPair(t)
			reRegister(t, p)
			p.silence(true)
			p.advance(t, silentPastWindow)
			if p.icd().Available() {
				t.Fatal("available while silent")
			}
			p.silence(false)
			p.wakeDevice(t)
			p.eventually(t, "available again", p.icd().Available)
		})
		t.Run("keeps a SIT/non-LIT registered peer available at all times", func(t *testing.T) {
			p := newICDPair(t, icdPairOptions{})
			p.register(t, client.ICDRegisterOptions{})
			if !p.icd().Available() || p.wakefulness(t).RequiresAwait() {
				t.Fatal("unavailable or awaited")
			}
			p.advance(t, 3700*time.Second)
			if !p.icd().Available() {
				t.Fatal("unavailable")
			}
		})
		t.Run("has no nextExpectedCheckin while subscribed", func(t *testing.T) {
			p := litOperatingPair(t)
			// Let the subscription recreated for the registration establish:
			// ten awaited 200 ms advances (IcdClientTest.ts:2272-2276), each
			// one a Step that waits for the site to go quiet, so the
			// recreate's exchanges finish between the clock moves. One 2 s
			// step fired the timers in a row and the assertion saw the gap
			// between the released and the new hold.
			for range 10 {
				p.advance(t, 200*time.Millisecond)
			}
			// Under load the recreate's exchanges may still be in flight
			// after the tenth step (seen in the race job of #346): wait for
			// the subscription to be established — matter.js's settled()
			// after each advance — before reading the deadline.
			p.eventually(t, "the recreated subscription", p.node.SubscriptionActive)
			if _, ok := p.icd().NextExpectedCheckIn(); ok {
				t.Fatal("a deadline while subscribed")
			}
		})
		t.Run("exposes nextExpectedCheckin on the wall clock once no subscription is held", func(t *testing.T) {
			t.Skip("MockTime.stepWallClock: the Go wakefulness runs on one clock reading, its deadline already a time.Time; the no-subscription deadline is held by TestIcdPeerWakefulness's subscription cases")
		})
		t.Run("has no nextExpectedCheckin for an unregistered peer", func(t *testing.T) {
			if _, ok := newICDPair(t, icdPairOptions{}).icd().NextExpectedCheckIn(); ok {
				t.Fatal("a deadline")
			}
		})
		t.Run("has no nextExpectedCheckin for a SIT/non-LIT registered peer", func(t *testing.T) {
			p := newICDPair(t, icdPairOptions{})
			p.register(t, client.ICDRegisterOptions{})
			if _, ok := p.icd().NextExpectedCheckIn(); ok {
				t.Fatal("a deadline")
			}
		})
		t.Run("emits checkInMissed when a registered LIT peer goes silent", func(t *testing.T) {
			p := litOperatingPair(t)
			reRegister(t, p)
			missed := &counter{}
			p.icd().OnCheckInMissed(missed.inc)
			p.silence(true)
			p.advance(t, silentPastWindow)
			if missed.get() != 1 || p.icd().Available() {
				t.Fatalf("%d missed", missed.get())
			}
		})
		t.Run("does not emit checkInMissed when the registration is dropped", func(t *testing.T) {
			p := litOperatingPair(t)
			reRegister(t, p)
			missed := &counter{}
			p.icd().OnCheckInMissed(missed.inc)
			if err := p.do(t, func(ctx context.Context) error { return p.icd().Forget(ctx) }); err != nil {
				t.Fatal(err)
			}
			if missed.get() != 0 {
				t.Fatalf("%d missed", missed.get())
			}
		})
		t.Run("does not emit checkInMissed on a runtime SIT→LIT operating-mode flip", func(t *testing.T) {
			p := newICDPair(t, icdPairOptions{cfg: icdLITConfig(true)})
			p.register(t, uncovered)
			missed := &counter{}
			p.icd().OnCheckInMissed(missed.inc)
			if err := p.device().ICD().SetOperatingMode(node.ICDModeLIT); err != nil {
				t.Fatal(err)
			}
			p.eventually(t, "the flip", func() bool { return p.wakefulness(t).RequiresAwait() })
			if missed.get() != 0 {
				t.Fatalf("%d missed", missed.get())
			}
		})
		t.Run("extends the awake window when stayActive promises a longer duration", func(t *testing.T) {
			p := litOperatingPair(t)
			reRegister(t, p)
			w := p.wakefulness(t)
			var promised time.Duration
			if err := p.do(t, func(ctx context.Context) (err error) {
				promised, err = p.icd().StayActive(ctx, 120*time.Second)
				return err
			}); err != nil || promised <= 0 {
				t.Fatalf("promised %v, %v", promised, err)
			}
			p.advance(t, 30*time.Second)
			if !w.Awake() {
				t.Fatal("asleep")
			}
		})
		t.Run("flips requiresAwait when a registered DSLS peer switches SIT→LIT at runtime", func(t *testing.T) {
			p := newICDPair(t, icdPairOptions{cfg: icdLITConfig(true)})
			forceSIT(t, p)
			p.register(t, uncovered)
			if p.wakefulness(t).RequiresAwait() {
				t.Fatal("awaited before the flip")
			}
			if err := p.device().ICD().SetOperatingMode(node.ICDModeLIT); err != nil {
				t.Fatal(err)
			}
			p.eventually(t, "the flip", func() bool { return p.wakefulness(t).RequiresAwait() })
		})
		t.Run("arms the awake window on a report-driven SIT→LIT flip so the recreate lands in-window", func(t *testing.T) {
			p := newICDPair(t, icdPairOptions{cfg: icdLITConfig(true)})
			forceSIT(t, p)
			p.register(t, uncovered)
			w := p.wakefulness(t)
			if err := p.device().ICD().SetOperatingMode(node.ICDModeLIT); err != nil {
				t.Fatal(err)
			}
			p.eventually(t, "the flip", w.RequiresAwait)
			if !w.Awake() {
				t.Fatal("asleep after a report-driven flip")
			}
		})
	})

	t.Run("awake getter", func(t *testing.T) {
		t.Run("defaults true unregistered, mirrors the wakefulness once registered", func(t *testing.T) {
			p := litOperatingPair(t)
			if err := p.do(t, func(ctx context.Context) error { return p.icd().Forget(ctx) }); err != nil {
				t.Fatal(err)
			}
			if !p.icd().Awake() {
				t.Fatal("asleep unregistered")
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
			if !p.icd().Awake() {
				t.Fatal("asleep after register")
			}
			p.silence(true)
			p.advance(t, silentPastWindow)
			if p.icd().Awake() {
				t.Fatal("awake while silent")
			}
			p.silence(false)
			p.wakeDevice(t)
			p.eventually(t, "awake", p.icd().Awake)
		})
	})

	t.Run("decommission cleanup", func(t *testing.T) {
		t.Run("unregisters locally when the node is decommissioned", func(t *testing.T) {
			p := newICDPair(t, icdPairOptions{})
			p.register(t, client.ICDRegisterOptions{})
			addr := p.peerAddr(t)
			table := p.table(t)
			w := table.WakefulnessFor(addr.NodeID)
			unregistered := &counter{}
			p.icd().OnUnregistered(unregistered.inc)
			if err := p.do(t, func(ctx context.Context) error { return p.reg.Decommission(ctx, p.node) }); err != nil {
				t.Fatal(err)
			}
			if p.icd().Registered() || unregistered.get() != 1 || table.HasPeers() {
				t.Fatal("not cleaned up")
			}
			table.AddPeer(icd.Peer{PeerNodeID: addr.NodeID, Key: make([]byte, 16)}, func(icd.ReceivedCheckIn) {})
			if table.WakefulnessFor(addr.NodeID) == w {
				t.Fatal("the departed peer's wakefulness was inherited")
			}
		})
	})

	t.Run("peer removal", func(t *testing.T) {
		t.Run("drops the wakefulness of a peer decommissioned after it was unregistered", func(t *testing.T) {
			p := newICDPair(t, icdPairOptions{})
			p.register(t, client.ICDRegisterOptions{})
			addr := p.peerAddr(t)
			table := p.table(t)
			w := table.WakefulnessFor(addr.NodeID)
			if err := p.do(t, func(ctx context.Context) error { return p.icd().Unregister(ctx) }); err != nil {
				t.Fatal(err)
			}
			if err := p.do(t, func(ctx context.Context) error { return p.reg.Decommission(ctx, p.node) }); err != nil {
				t.Fatal(err)
			}
			table.AddPeer(icd.Peer{PeerNodeID: addr.NodeID, Key: make([]byte, 16)}, func(icd.ReceivedCheckIn) {})
			if table.WakefulnessFor(addr.NodeID) == w {
				t.Fatal("the departed peer's wakefulness was inherited")
			}
		})
	})

	t.Run("peer timings", func(t *testing.T) {
		t.Run("takes the peer's ActiveModeDuration from its ICD Management attributes", func(t *testing.T) {
			cfg := icdLITConfig(true)
			cfg.ActiveModeDuration = 60 * time.Second
			p := newICDPair(t, icdPairOptions{cfg: cfg, forceLIT: true})
			p.eventually(t, "the LIT auto-registration", p.icd().Registered)
			w := p.wakefulness(t)
			p.silence(true)
			w.NoteCheckIn(nil)
			p.advance(t, 30*time.Second)
			if !w.Awake() {
				t.Fatal("asleep")
			}
		})
	})
}

func containsVendor(vs []uint16, v uint16) bool {
	for _, x := range vs {
		if x == v {
			return true
		}
	}
	return false
}

// requestRefreshOnNextCheckIn pulls the baseline back by 2^31 so the next
// Check-In asks for a key refresh (IcdClientTest.ts:188-194); it returns
// the pulled-back counter start.
func (p *icdPair) requestRefreshOnNextCheckIn(t *testing.T) uint32 {
	t.Helper()
	start := p.fedPeer(t).CounterStart - 0x80000000
	client.RebaseICDPeer(p.node, start, p.fedPeer(t).LastOffset)
	return start
}

// deviceKey is the key the device holds for the controller (IcdClientTest.ts:209-212).
func (p *icdPair) deviceKey(t *testing.T) []byte {
	t.Helper()
	regs := p.deviceServerRegistrations(t)
	if len(regs) == 0 {
		t.Fatal("the device holds no registration")
	}
	return regs[0].Key
}

func (p *icdPair) deviceKeyNoT() []byte {
	fabrics, err := p.device().FabricInfos(context.Background())
	if err != nil || len(fabrics) == 0 {
		return nil
	}
	regs := p.device().Root().ICDManagement.Registrations(fabrics[0].FabricIndex)
	if len(regs) == 0 {
		return nil
	}
	return regs[0].Key
}

// forceSIT holds a DSLS device in SIT, so the registration that follows
// does not drive it to LIT on its own: matter.js asserts the SIT state
// right after register() on MockTime, before the registration's mode
// report arrives; on the site that report races the assertion.
func forceSIT(t *testing.T, p *icdPair) {
	t.Helper()
	if err := p.device().ICD().SetOperatingMode(node.ICDModeSIT); err != nil {
		t.Fatal(err)
	}
}

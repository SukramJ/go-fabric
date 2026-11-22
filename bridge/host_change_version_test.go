// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"sync/atomic"
	"testing"

	"thingfabric.dev/go-fabric/cluster/levelcontrol"
	"thingfabric.dev/go-fabric/cluster/light"
	"thingfabric.dev/go-fabric/cluster/lock"
	"thingfabric.dev/go-fabric/cluster/spec"
	ccdef "thingfabric.dev/go-fabric/cluster/spec/colorcontrol"
	lockdef "thingfabric.dev/go-fabric/cluster/spec/doorlock"
	hepa "thingfabric.dev/go-fabric/cluster/spec/hepafiltermonitoring"
	lvldef "thingfabric.dev/go-fabric/cluster/spec/levelcontrol"
	"thingfabric.dev/go-fabric/contract"
)

// movingLevelPort is a LevelControl port whose CurrentLevel the device
// moves by itself (move), notifying its change.
type movingLevelPort struct {
	*notifyingLevelPort
	level atomic.Uint32
}

func (p *movingLevelPort) CurrentLevel() (uint8, bool) { return uint8(p.level.Load()), true } //nolint:gosec // set below to a uint8

func (p *movingLevelPort) move(level uint8) func() {
	return func() {
		p.level.Store(uint32(level))
		p.fire()
	}
}

// TestHostChangeAdvancesOnceOnAHandOffServer: a host's change of a value
// outside any write or command — a hand-off server over a port that is a
// [contract.ChangeNotifier] — advances its cluster's DataVersion once and
// is reported once. matter.js advances the version once per commit that
// changed a value (packages/node/src/behavior/state/managed/
// Datasource.ts:1456-1457 at db021da0); a notification that moved nothing
// advances nothing.
func TestHostChangeAdvancesOnceOnAHandOffServer(t *testing.T) {
	t.Parallel()
	t.Run("LevelControl CurrentLevel", func(t *testing.T) {
		t.Parallel()
		port := &movingLevelPort{notifyingLevelPort: &notifyingLevelPort{}}
		port.level.Store(100)
		srv := levelcontrol.NewServer(levelcontrol.Config{Source: port})
		h := newAppHarness(t, appDevice{deviceType: 0x0101, servers: []contract.ClusterServer{srv}})
		epID := h.endpoints[0x0101]
		w := watchCluster(t, h, epID, lvldef.ClusterID)
		w.expect("a host change outside a write", port.move(40), lvldef.AttrCurrentLevel, 1, 1)
		// The second change within the quieter's interval is reported by
		// its throttle later; its advance is at once.
		if advanced, _ := w.step(port.move(41), lvldef.AttrCurrentLevel); advanced != 1 {
			t.Errorf("a second host change: DataVersion advanced %d, want 1", advanced)
		}
		w.expect("a notification that moved nothing", port.fire, lvldef.AttrCurrentLevel, 0, 0)
	})

	// F-db021da0-44: the port is the server's Source and its notifier is
	// the endpoint source's too, so one host notification reaches the
	// server's forward and the bridge's own subscription; the notifier's
	// callbacks run in map order, both orders over repeated runs.
	t.Run("LevelControl CurrentLevel, the port's notifier also the endpoint source's", func(t *testing.T) {
		t.Parallel()
		port := &movingLevelPort{notifyingLevelPort: &notifyingLevelPort{}}
		port.level.Store(100)
		srv := levelcontrol.NewServer(levelcontrol.Config{Source: port})
		d := notifyingAppDevice{appDevice: appDevice{deviceType: 0x0101, servers: []contract.ClusterServer{srv}}, hostNotifier: &port.hostNotifier}
		h := newNotifyingAppHarness(t, d)
		epID := h.endpoints[0x0101]
		w := watchCluster(t, h, epID, lvldef.ClusterID)
		w.expect("a host change outside a write", port.move(40), lvldef.AttrCurrentLevel, 1, 1)
		w.expect("a notification that moved nothing", port.fire, lvldef.AttrCurrentLevel, 0, 0)
	})

	t.Run("DoorLock LockState, a pull server over a notifying source", func(t *testing.T) {
		t.Parallel()
		src := &movingLock{hostNotifier: &hostNotifier{}}
		srv := lock.NewDoorLockServer(lock.DoorLockConfig{Source: src})
		h := newAppHarness(t, appDevice{deviceType: 0x000A, servers: []contract.ClusterServer{srv}})
		epID := h.endpoints[0x000A]
		w := watchCluster(t, h, epID, lockdef.ClusterID)
		w.expect("a host change outside a write", src.set(false), lockdef.AttrLockState, 1, 1)
		w.expect("a notification that moved nothing", src.fire, lockdef.AttrLockState, 0, 0)
	})

	t.Run("ColorControl, the host's Set beside the endpoint source's notifier", func(t *testing.T) {
		t.Parallel()
		srv := light.NewColorControlServer(light.ColorControlServerConfig{InitialMireds: 250})
		d := notifyingAppDevice{appDevice: appDevice{deviceType: 0x010C, servers: []contract.ClusterServer{srv}}, hostNotifier: &hostNotifier{}}
		h := newNotifyingAppHarness(t, d)
		epID := h.endpoints[0x010C]
		w := watchCluster(t, h, epID, ccdef.ClusterID)
		set := func() {
			srv.Set(light.Color{Mode: light.ColorModeColorTemperature, ColorTemperatureMireds: 300})
			d.fire()
		}
		w.expect("the host's Set, then its notifier", set, ccdef.AttrColorTemperatureMireds, 1, 1)
		w.expect("a notification that moved nothing", d.fire, ccdef.AttrColorTemperatureMireds, 0, 0)
	})

	t.Run("a generated server's Set beside the endpoint source's notifier", func(t *testing.T) {
		t.Parallel()
		srv, err := spec.NewServer(hepa.Definition, spec.Options{Features: uint32(hepa.FeatureCondition)},
			spec.ServerConfig{Initial: map[uint32]any{hepa.AttrCondition: uint8(40), hepa.AttrChangeIndication: hepa.ChangeIndicationOk}})
		if err != nil {
			t.Fatal(err)
		}
		d := notifyingAppDevice{appDevice: appDevice{deviceType: 0x002D, servers: []contract.ClusterServer{srv}}, hostNotifier: &hostNotifier{}}
		h := newNotifyingAppHarness(t, d)
		epID := h.endpoints[0x002D]
		w := watchCluster(t, h, epID, hepa.ClusterID)
		set := func(v uint8) func() { return func() { _ = srv.Set(hepa.AttrCondition, v) } }
		w.expect("the host's Set, then its notifier", func() { set(30)(); d.fire() }, hepa.AttrCondition, 1, 1)
		w.expect("the host's Set alone", set(20), hepa.AttrCondition, 1, 1)
		w.expect("its notifier later", d.fire, hepa.AttrCondition, 0, 0)
	})
}

// movingLock is a lock source whose bolt the device moves by itself.
type movingLock struct {
	*hostNotifier
	unlocked atomic.Bool
}

func (*movingLock) IsJammed() bool                           { return false }
func (l *movingLock) IsLocked() (locked, observed bool)      { return !l.unlocked.Load(), true }
func (*movingLock) LockInvoke(context.Context, uint32) error { return nil }

func (l *movingLock) set(locked bool) func() {
	return func() {
		l.unlocked.Store(!locked)
		l.fire()
	}
}

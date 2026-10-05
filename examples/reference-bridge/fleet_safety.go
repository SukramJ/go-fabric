// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package main

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/cluster/alarm"
	"github.com/SukramJ/go-fabric/cluster/cover"
	"github.com/SukramJ/go-fabric/cluster/lock"
	"github.com/SukramJ/go-fabric/cluster/wire"
	"github.com/SukramJ/go-fabric/contract"
)

// --- device: a smoke and CO alarm ---------------------------------------------

// smokeExpiryDate is the alarm's end of service, 2036-01-01, in Matter
// epoch seconds (seconds since 2000-01-01 UTC).
var smokeExpiryDate = uint32(time.Date(2036, 1, 1, 0, 0, 0, 0, time.UTC).Sub(time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)) / time.Second)

// demoSmokeAlarm is a battery-powered smoke and CO alarm.
//
// The SmokeCoAlarm server derives ExpressedState and the alarm events from
// the State the device reports; the device calls [alarm.Server.Refresh]
// after every change so the transition events go out, and fires its
// [notifier] so a subscriber sees the new attribute values.
//
// SelfTestRequest is accepted: the test "runs" until the device reports it
// finished (control hook `smoke selftest-done`), which is the transition
// that emits SelfTestComplete.
type demoSmokeAlarm struct {
	name string
	notifier
	version cluster.DataVersionTracker
	battery *demoReading

	mu    sync.Mutex
	state alarm.State
	srv   *alarm.Server
}

var (
	_ contract.EndpointSource = (*demoSmokeAlarm)(nil)
	_ alarm.StateSource       = (*demoSmokeAlarm)(nil)
	_ alarm.SelfTester        = (*demoSmokeAlarm)(nil)
	_ alarm.SensitivitySetter = (*demoSmokeAlarm)(nil)
)

func newDemoSmokeAlarm(name string) *demoSmokeAlarm {
	return &demoSmokeAlarm{
		name:    name,
		battery: newDemoReading(name+" battery", contract.MeasurementBattery, 87),
		// A device expires some years after it was made; TC-SMOKECO-2.1
		// reads an ExpiryDate in the future (epoch-s, from 2000-01-01).
		state: alarm.State{SmokeSensitivityLevel: alarm.SensitivityStandard, ExpiryDate: smokeExpiryDate},
	}
}

// MatterDeviceType implements [contract.EndpointSource].
func (a *demoSmokeAlarm) MatterDeviceType() uint16 { return alarm.DeviceTypeSmokeCoAlarm }

// MatterClusterServers implements [contract.EndpointSource]. PowerSource,
// which SmokeCoAlarm also mandates, comes from the Spec's PowerSource
// reading (the battery) rather than from here.
//
// The server is built once: it carries the event emitter the bridge wires
// at reassembly, and Refresh must emit through that same instance — a
// server rebuilt per call left the alarm events unsent (TC-SMOKECO-2.2).
func (a *demoSmokeAlarm) MatterClusterServers() []contract.ClusterServer {
	a.mu.Lock()
	if a.srv != nil {
		srv := a.srv
		a.mu.Unlock()
		return []contract.ClusterServer{srv}
	}
	a.mu.Unlock()
	srv, err := alarm.NewServer(alarm.Config{
		Source:   a,
		Features: alarm.FeatureSmokeAlarm | alarm.FeatureCOAlarm,
		// Every optional attribute the server offers, so the CHIP SMOKECO
		// cases have the whole surface to read.
		Optional: alarm.OptionalDeviceMuted | alarm.OptionalInterconnectSmokeAlarm |
			alarm.OptionalInterconnectCOAlarm | alarm.OptionalContaminationState |
			alarm.OptionalSmokeSensitivityLevel | alarm.OptionalExpiryDate | alarm.OptionalUnmounted,
		DataVersion: &a.version,
	})
	if err != nil {
		panic(fmt.Sprintf("smoke alarm SmokeCoAlarm: %v", err))
	}
	a.mu.Lock()
	if a.srv == nil {
		a.srv = srv
	}
	srv = a.srv
	a.mu.Unlock()
	return []contract.ClusterServer{srv}
}

// SmokeCOState implements [alarm.StateSource].
func (a *demoSmokeAlarm) SmokeCOState() alarm.State {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.state
}

// smokeSelfTestDuration is how long the alarm's self-test runs.
const smokeSelfTestDuration = 5 * time.Second

// SelfTest implements [alarm.SelfTester]: the test runs for
// [smokeSelfTestDuration] and then ends on its own — TestInProgress goes
// back to false, which is what emits SelfTestComplete and, with
// ExpressedState back at Normal, AllClear (TC-SMOKECO-2.4 waits for both).
func (a *demoSmokeAlarm) SelfTest(context.Context) error {
	slog.Info("smoke.selftest", slog.String("device", a.name))
	a.update(func(st *alarm.State) { st.TestInProgress = true })
	time.AfterFunc(smokeSelfTestDuration, func() {
		slog.Info("smoke.selftest_done", slog.String("device", a.name))
		a.update(func(st *alarm.State) { st.TestInProgress = false })
	})
	return nil
}

// SetSmokeSensitivityLevel implements [alarm.SensitivitySetter].
func (a *demoSmokeAlarm) SetSmokeSensitivityLevel(_ context.Context, level alarm.Sensitivity) error {
	slog.Info("smoke.sensitivity", slog.String("device", a.name), slog.Int("level", int(level)))
	a.update(func(st *alarm.State) { st.SmokeSensitivityLevel = level })
	return nil
}

// update applies a change the device observed, then lets the server emit
// the transition events and pushes the attributes.
func (a *demoSmokeAlarm) update(change func(*alarm.State)) {
	a.mu.Lock()
	change(&a.state)
	srv := a.srv
	a.mu.Unlock()
	if srv != nil {
		srv.Refresh()
	}
	a.notify()
}

// --- device: a front-door lock ---------------------------------------------------

// demoLock is a motorised door lock. The DoorLock server emits LockOperation
// after every successful remote command; the device reports a jam on its own
// (control hook `lock jam`), which the server reads back as LockState
// NotFullyLocked. (DoorLockAlarm has no emission path without PIN
// credentials, in the module's server as in matter.js.) A jammed bolt
// refuses LockDoor / UnlockDoor, which the controller sees as FAILURE.
type demoLock struct {
	name string
	notifier
	version cluster.DataVersionTracker

	mu     sync.Mutex
	locked bool
	jammed bool

	// srv is built once: the bridge asks for the cluster servers on every
	// dispatch, and the server holds OperatingMode and the event emitter
	// the bridge wired at reassembly (TC-DRLK-2.1).
	srvOnce sync.Once
	srv     *lock.DoorLockServer
}

var (
	_ contract.EndpointSource = (*demoLock)(nil)
	_ lock.StateSource        = (*demoLock)(nil)
)

// deviceTypeDoorLock is DoorLock (matter.js door-lock.element.ts).
const deviceTypeDoorLock uint16 = 0x000A

func newDemoLock(name string) *demoLock { return &demoLock{name: name, locked: true} }

// MatterDeviceType implements [contract.EndpointSource].
func (l *demoLock) MatterDeviceType() uint16 { return deviceTypeDoorLock }

// MatterClusterServers implements [contract.EndpointSource].
func (l *demoLock) MatterClusterServers() []contract.ClusterServer {
	l.srvOnce.Do(func() {
		l.srv = lock.NewDoorLockServer(lock.DoorLockConfig{Source: l, DataVersion: &l.version})
	})
	return []contract.ClusterServer{l.srv}
}

// IsJammed implements [lock.StateSource].
func (l *demoLock) IsJammed() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.jammed
}

// IsLocked implements [lock.StateSource].
func (l *demoLock) IsLocked() (bool, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.locked, true
}

// LockInvoke implements [lock.StateSource]. A jammed bolt refuses to move.
func (l *demoLock) LockInvoke(_ context.Context, cmdID uint32) error {
	l.mu.Lock()
	if l.jammed {
		l.mu.Unlock()
		return fmt.Errorf("lock %s: bolt jammed", l.name)
	}
	l.locked = cmdID == wire.DoorLockCmdLockDoor
	locked := l.locked
	l.mu.Unlock()
	slog.Info("lock.set", slog.String("device", l.name), slog.Bool("locked", locked))
	l.notify()
	return nil
}

// reportJam is the bolt jamming — or freeing again.
func (l *demoLock) reportJam(jammed bool) {
	l.mu.Lock()
	l.jammed = jammed
	l.mu.Unlock()
	slog.Info("lock.jam", slog.String("device", l.name), slog.Bool("jammed", jammed))
	l.notify()
}

// --- device: a roller blind -------------------------------------------------------

// demoBlind is a roller blind. The module's WindowCovering server holds the
// position itself (cluster/cover is a conformance reference that drives no
// device), so the endpoint keeps one instance across reassemblies.
type demoBlind struct {
	name string
	once sync.Once
	srv  *cover.WindowCoveringServer
}

var _ contract.EndpointSource = (*demoBlind)(nil)

// deviceTypeWindowCovering is WindowCovering (window-covering.element.ts).
const deviceTypeWindowCovering uint16 = 0x0202

// WindowCovering FeatureMap bits (window-covering-cluster.element.ts):
// Lift and PositionAwareLift.
const (
	coverFeatureLift              uint32 = 1 << 0
	coverFeaturePositionAwareLift uint32 = 1 << 2
)

func newDemoBlind(name string) *demoBlind { return &demoBlind{name: name} }

// MatterDeviceType implements [contract.EndpointSource].
func (b *demoBlind) MatterDeviceType() uint16 { return deviceTypeWindowCovering }

// MatterClusterServers implements [contract.EndpointSource].
func (b *demoBlind) MatterClusterServers() []contract.ClusterServer {
	b.once.Do(func() {
		b.srv = cover.NewWindowCoveringServer(cover.Config{
			Type:                         0, // Rollershade
			EndProductType:               0, // RollerShade
			FeatureMap:                   coverFeatureLift | coverFeaturePositionAwareLift,
			InitialPositionPercent100ths: 0,
		})
	})
	return []contract.ClusterServer{b.srv}
}

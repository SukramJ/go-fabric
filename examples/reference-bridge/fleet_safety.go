// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"time"

	lockdef "github.com/SukramJ/go-fabric/cluster/spec/doorlock"
	smokedef "github.com/SukramJ/go-fabric/cluster/spec/smokecoalarm"
	wcdef "github.com/SukramJ/go-fabric/cluster/spec/windowcovering"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/device"
)

// --- device: a smoke and CO alarm ---------------------------------------------

// smokeExpiryDate is the alarm's end of service, 2036-01-01, in Matter
// epoch seconds (seconds since 2000-01-01 UTC).
var smokeExpiryDate = uint32(time.Date(2036, 1, 1, 0, 0, 0, 0, time.UTC).Sub(time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)) / time.Second)

// demoSmokeAlarm is a battery-powered smoke and CO alarm.
//
// The device kit's SmokeCoAlarm server derives ExpressedState and the
// alarm events from the State the device reports: the alarm fires its
// [notifier] after every change, and the server, a change notifier over
// it, emits the transition events before a subscriber sees the new
// attribute values.
//
// SelfTestRequest is accepted: the test "runs" until the device reports it
// finished (control hook `smoke selftest-done`), which is the transition
// that emits SelfTestComplete.
type demoSmokeAlarm struct {
	name string
	notifier
	battery float64

	mu    sync.Mutex
	state smokedef.State
	srv   *smokedef.Server
}

var (
	_ smokedef.StateSource       = (*demoSmokeAlarm)(nil)
	_ smokedef.SelfTester        = (*demoSmokeAlarm)(nil)
	_ smokedef.SensitivitySetter = (*demoSmokeAlarm)(nil)
)

func newDemoSmokeAlarm(name string) *demoSmokeAlarm {
	return &demoSmokeAlarm{
		name:    name,
		battery: 87,
		// A device expires some years after it was made; TC-SMOKECO-2.1
		// reads an ExpiryDate in the future (epoch-s, from 2000-01-01).
		state: smokedef.State{SmokeSensitivityLevel: smokedef.SensitivityStandard, ExpiryDate: smokeExpiryDate},
	}
}

// deviceTypePowerSource is the PowerSource device type
// (power-source.element.ts, 0x0011), the component the alarm's battery is.
const deviceTypePowerSource uint16 = 0x0011

// declaration declares the alarm's SmokeCoAlarm: smoke and CO, with every
// optional attribute the server offers, so the CHIP SMOKECO cases have the
// whole surface to read; the alarm is the kit's server's StateSource (it
// reports their values). The PowerSource component SmokeCoAlarm requires
// is a part of the alarm's endpoint carrying the battery (fleet.go).
func (a *demoSmokeAlarm) declaration() device.Declaration {
	var st smokedef.State
	return smokedef.Declare{
		Features:    smokedef.FeatureSmokeAlarm | smokedef.FeatureCoAlarm,
		StateSource: a,
		Initial: smokedef.Patch{
			DeviceMuted: &st.DeviceMuted, InterconnectSmokeAlarm: &st.InterconnectSmokeAlarm,
			InterconnectCoAlarm: &st.InterconnectCoAlarm, ContaminationState: &st.ContaminationState,
			SmokeSensitivityLevel: &st.SmokeSensitivityLevel, ExpiryDate: &st.ExpiryDate, Unmounted: &st.Unmounted,
		},
	}
}

// bind keeps the alarm's server.
func (a *demoSmokeAlarm) bind(d *device.SmokeCoAlarm) {
	a.mu.Lock()
	a.srv = d.SmokeCoAlarm()
	a.mu.Unlock()
}

// SmokeCOState implements [smokedef.StateSource].
func (a *demoSmokeAlarm) SmokeCOState() smokedef.State {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.state
}

// smokeSelfTestDuration is how long the alarm's self-test runs.
const smokeSelfTestDuration = 5 * time.Second

// SelfTest implements [smokedef.SelfTester]: the test runs for
// [smokeSelfTestDuration] and then ends on its own — TestInProgress goes
// back to false, which is what emits SelfTestComplete and, with
// ExpressedState back at Normal, AllClear (TC-SMOKECO-2.4 waits for both).
func (a *demoSmokeAlarm) SelfTest(context.Context) error {
	slog.Info("smoke.selftest", slog.String("device", a.name))
	a.update(func(st *smokedef.State) { st.TestInProgress = true })
	time.AfterFunc(smokeSelfTestDuration, func() {
		slog.Info("smoke.selftest_done", slog.String("device", a.name))
		a.update(func(st *smokedef.State) { st.TestInProgress = false })
	})
	return nil
}

// selfTestBusy are the ExpressedStates in which the alarm refuses a self
// test (resource :201-204: SmokeAlarm, CoAlarm, Testing,
// InterconnectSmoke, InterconnectCO — BUSY to a SelfTestRequest).
var selfTestBusy = []smokedef.ExpressedStateEnum{
	smokedef.ExpressedStateSmokeAlarm, smokedef.ExpressedStateCoAlarm, smokedef.ExpressedStateTesting,
	smokedef.ExpressedStateInterconnectSmoke, smokedef.ExpressedStateInterconnectCo,
}

// pressTestButton is the test button on the alarm (CHIP's smoke-co-alarm
// app's LongPress). It starts a self-test by the same rule a
// SelfTestRequest follows — refused while the alarm sounds or already
// tests (TC-SMOKECO-2.2 step 9) — and a refused press does nothing, as on
// the device.
func (a *demoSmokeAlarm) pressTestButton(ctx context.Context) error {
	a.mu.Lock()
	srv := a.srv
	a.mu.Unlock()
	if srv == nil {
		return errNotBuilt
	}
	if expressed := srv.Get().ExpressedState; slices.Contains(selfTestBusy, expressed) {
		slog.Info("smoke.selftest_refused", slog.String("device", a.name), slog.Int("expressed_state", int(expressed)))
		return nil
	}
	return a.SelfTest(ctx)
}

// SetSmokeSensitivityLevel implements [smokedef.SensitivitySetter].
func (a *demoSmokeAlarm) SetSmokeSensitivityLevel(_ context.Context, level smokedef.SensitivityEnum) error {
	slog.Info("smoke.sensitivity", slog.String("device", a.name), slog.Int("level", int(level)))
	a.update(func(st *smokedef.State) { st.SmokeSensitivityLevel = level })
	return nil
}

// update applies a change the device observed and tells the server, which
// emits the transition events, and the bridge.
func (a *demoSmokeAlarm) update(change func(*smokedef.State)) {
	a.mu.Lock()
	change(&a.state)
	a.mu.Unlock()
	a.notify()
}

// --- device: a front-door lock ---------------------------------------------------

// demoLock is a motorised door lock with a user database: the device kit's
// DoorLock server serves users with PIN credentials, the three schedule
// kinds, PINs over the air and Unbolting, with the limits matter.js's CHIP
// test lock states (support/chip-testing/src/AllClustersTestInstance.ts:
// 704-724 at 862ad75f), and keeps its users, credentials and schedules in
// the node's store under the lock's StableKey. It emits LockOperation after
// every successful remote command and LockOperationError for a refused
// one; the device reports a jam on its own (control hook `lock jam`), which
// the server reads back as LockState NotFullyLocked. A jammed bolt refuses
// LockDoor / UnlockDoor, which the controller sees as FAILURE.
type demoLock struct {
	name string
	notifier

	mu     sync.Mutex
	locked bool
	jammed bool

	// auth is the node's store, set before the device is built
	// (main.go); nil keeps the records in memory.
	auth lockdef.AuthStore
}

var _ lockdef.StateSource = (*demoLock)(nil)

func newDemoLock(name string) *demoLock { return &demoLock{name: name, locked: true} }

// declaration declares the lock's DoorLock.
func (l *demoLock) declaration() device.Declaration {
	users, pinUsers, perUser := uint16(10), uint16(10), uint8(5)
	rules, maxPIN, minPIN := lockdef.CredentialRulesSingle, uint8(8), uint8(4)
	weekDay, yearDay, holiday := uint8(10), uint8(10), uint8(10)
	wrongCodes, disable, relock := uint8(3), uint8(10), uint32(60)
	return lockdef.Declare{
		Features: lockdef.FeatureUser | lockdef.FeaturePinCredential | lockdef.FeatureCredentialOverTheAirAccess |
			lockdef.FeatureWeekDayAccessSchedules | lockdef.FeatureYearDayAccessSchedules | lockdef.FeatureHolidaySchedules |
			lockdef.FeatureUnbolting,
		StateSource: l,
		AuthStore:   l.auth,
		Initial: lockdef.Patch{
			NumberOfTotalUsersSupported: &users, NumberOfPinUsersSupported: &pinUsers,
			NumberOfCredentialsSupportedPerUser: &perUser, CredentialRulesSupport: &rules,
			MaxPinCodeLength: &maxPIN, MinPinCodeLength: &minPIN,
			NumberOfWeekDaySchedulesSupportedPerUser: &weekDay, NumberOfYearDaySchedulesSupportedPerUser: &yearDay,
			NumberOfHolidaySchedulesSupported: &holiday,
			WrongCodeEntryLimit:               &wrongCodes, UserCodeTemporaryDisableTime: &disable,
			AutoRelockTime: &relock,
		},
	}
}

// IsJammed implements [lockdef.StateSource].
func (l *demoLock) IsJammed() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.jammed
}

// IsLocked implements [lockdef.StateSource].
func (l *demoLock) IsLocked() (locked, observed bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.locked, true
}

// LockInvoke implements [lockdef.StateSource]. A jammed bolt refuses to
// move.
func (l *demoLock) LockInvoke(_ context.Context, cmdID uint32) error {
	l.mu.Lock()
	if l.jammed {
		l.mu.Unlock()
		return fmt.Errorf("lock %s: bolt jammed", l.name)
	}
	l.locked = cmdID == lockdef.CmdLockDoor
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

// demoBlind is a roller blind. The device kit's WindowCovering server holds
// the position itself (cluster/cover is a conformance reference that drives
// no device) and travels a movement: six steps of 950 ms, as matter.js's
// CHIP test node (TestWindowCoveringServer.ts) — the TC-WNCV-3.x cases read
// OperationalStatus while it moves.
type demoBlind struct {
	name string

	// position is where the blind starts (0, open, for a new one), and
	// persist is told every position it reaches — WindowCovering state is
	// non-volatile in matter.js, and TC-WNCV-4.5 reads the lift position
	// back after a reboot. Both are set before the device is built
	// (restoreBlind, before fleet.endpoints); persist may be nil.
	position uint16
	persist  func(position uint16)
}

func newDemoBlind(name string) *demoBlind { return &demoBlind{name: name} }

// device builds the blind: a roller shade with Lift and PositionAwareLift.
func (b *demoBlind) device(opts device.Options) (*device.WindowCovering, error) {
	kind, product, position := wcdef.TypeRollershade, wcdef.EndProductTypeRollerShade, b.position
	d, err := device.NewWindowCovering(opts, wcdef.Declare{
		Features:    wcdef.FeatureLift | wcdef.FeaturePositionAwareLift,
		Transitions: true,
		Initial:     wcdef.Patch{Type: &kind, EndProductType: &product, CurrentPositionLiftPercent100ths: &position},
	})
	if err != nil {
		return nil, err
	}
	if b.persist != nil {
		d.WindowCovering().OnChanged(func(st wcdef.State, change contract.Change) {
			if slices.Contains(change.Attributes, wcdef.AttrCurrentPositionLiftPercent100ths) && st.CurrentPositionLiftPercent100ths != nil {
				b.persist(*st.CurrentPositionLiftPercent100ths)
			}
		})
	}
	return d, nil
}

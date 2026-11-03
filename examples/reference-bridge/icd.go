// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/SukramJ/go-fabric/cluster/spec/icdmanagement"
	"github.com/SukramJ/go-fabric/node"
)

// --icd-lit (TESTING ONLY) makes the daemon a Long Idle Time ICD after
// matter.js's lit-icd test app (support/chip-testing/src/IcdTestInstance.ts
// at 862ad75f), for the ICD client certification cases the Go controller
// runs as the DUT (TC-ICDM-6.1, TC-ICDB-1.3): the root serves ICD
// Management with CheckInProtocolSupport, UserActiveModeTrigger,
// LongIdleTimeSupport and DynamicSitLitSupport (:67-72), the app's
// timings (:107-108, :171-190), a fixed idle/active cadence so a
// registered client receives unsolicited Check-Ins, and the ICD test event
// triggers on TestEventTrigger (support/chip-testing/src/cluster/
// IcdTestEventServer.ts:15-20, :46-83). Off by default: the daemon's
// composition and its chip-tool goldens do not move.

// ICD test event triggers (IcdTestEventServer.ts:15-20): cluster 0x46 in
// the high 16 bits.
const (
	triggerICDAddActiveMode             uint64 = 0x0046000000000001
	triggerICDRemoveActiveMode          uint64 = 0x0046000000000002
	triggerICDInvalidateHalfCounterVals uint64 = 0x0046000000000003
	triggerICDInvalidateAllCounterVals  uint64 = 0x0046000000000004
	triggerICDDSLSForceSIT              uint64 = 0x0046000000000006
	triggerICDDSLSWithdrawSIT           uint64 = 0x0046000000000007
)

// The app's defaults (IcdTestInstance.ts:107-108): idle 3600 s, active
// 10000 ms; --icd-idle-mode-duration and --icd-active-mode-duration-ms
// override them as the app's --icdIdleModeDuration and
// --icdActiveModeDurationMs do (TC-ICDB-1.3 runs 5 s and 1000 ms).
const (
	defaultICDIdleSeconds  = 3600
	defaultICDActiveMillis = 10000
)

// icdConfig is the node's ICD configuration of the flags
// (IcdTestInstance.ts:171-190): LIT with DynamicSitLit — registration
// driven, SIT until a client registers —, the LIT threshold of 5 s,
// two clients per fabric, no Check-In back-off beyond the idle duration,
// and the app's User Active Mode Trigger hint and instruction.
func icdConfig(idleSeconds, activeMillis uint) *node.ICDConfig {
	idle := time.Duration(idleSeconds) * time.Second //nolint:gosec // G115: checkFlags bounds it to 64800 s
	return &node.ICDConfig{
		Mode:                      node.ICDModeLIT,
		IdleModeDuration:          idle,
		ActiveModeDuration:        time.Duration(activeMillis) * time.Millisecond, //nolint:gosec // G115: checkFlags bounds it to the idle duration
		ActiveModeThreshold:       5 * time.Second,
		MaximumCheckInBackoff:     idle,
		ClientsSupportedPerFabric: 2,
		DynamicSITLIT:             true,
		UserActiveModeTrigger: &node.ICDUserActiveModeTrigger{
			Hint: icdmanagement.UserActiveModeTriggerPowerCycle | icdmanagement.UserActiveModeTriggerCustomInstruction |
				icdmanagement.UserActiveModeTriggerDeviceManual | icdmanagement.UserActiveModeTriggerActuateSensor |
				icdmanagement.UserActiveModeTriggerResetButton | icdmanagement.UserActiveModeTriggerSetupButton,
			Instruction: "Restart the application",
		},
	}
}

// demoICD drives the ICD node's mode machine on a fixed cadence and maps
// the ICD test event triggers onto it (IcdTestEventServer.ts): a node of
// this module never sleeps on its own (node.ICDConfig), so — as matter.js's
// test app does for its own — the active window and the idle window run on
// the daemon's timers, and an idle window's end wakes the device, which
// sends its Check-Ins.
type demoICD struct {
	log *slog.Logger

	mu         sync.Mutex
	icd        *node.ICD
	counter    func(by int64) error
	active     time.Duration
	idle       time.Duration
	keepActive bool
	timer      *time.Timer
}

// newDemoICD is the cadence of the configured timings.
func newDemoICD(cfg *node.ICDConfig, log *slog.Logger) *demoICD {
	return &demoICD{log: log, active: cfg.ActiveModeDuration, idle: cfg.IdleModeDuration}
}

// attachICD hooks the cadence onto the node's mode events before the node
// starts, so the first ActiveModeEntered arms the first active window
// (IcdTestEventServer.ts:35-44).
func attachICD(d *demoICD, n *node.Node) {
	if d == nil || n.ICD() == nil {
		return
	}
	d.mu.Lock()
	d.icd = n.ICD()
	if srv := n.Root().ICDManagement; srv != nil {
		d.counter = func(by int64) error { _, err := srv.Counter().Advance(by); return err }
	}
	d.mu.Unlock()
	d.icd.OnModeChange(func(e node.ICDModeEvent) {
		switch e {
		case node.ICDActiveModeEntered:
			d.arm(d.active, d.activeWindowDone)
		case node.ICDIdleModeEntered:
			d.arm(d.idle, d.idleWindowDone)
		case node.ICDMayEnterIdleMode:
		}
	})
}

// arm replaces the running window's timer (IcdTestEventServer.ts:89-98,
// :109-118, #armActiveWindow, #onIdleEntered).
func (d *demoICD) arm(after time.Duration, fn func()) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.timer != nil {
		d.timer.Stop()
	}
	d.timer = time.AfterFunc(after, fn)
}

// activeWindowDone is #onActiveWindowDone (:100-107): AddActiveMode holds
// the device active, otherwise it goes idle.
func (d *demoICD) activeWindowDone() {
	d.mu.Lock()
	keep, icd := d.keepActive, d.icd
	d.mu.Unlock()
	if keep {
		d.arm(d.active, d.activeWindowDone)
		return
	}
	icd.EnterIdleMode()
}

// idleWindowDone is #onIdleWindowDone (:120-122): the device wakes.
func (d *demoICD) idleWindowDone() {
	d.mu.Lock()
	icd := d.icd
	d.mu.Unlock()
	icd.RequestActiveMode()
}

// stop ends the cadence (IcdTestEventServer.ts:124-129, #stopTimers).
func (d *demoICD) stop() {
	if d == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.timer != nil {
		d.timer.Stop()
		d.timer = nil
	}
}

// errICDCounter is a counter trigger on a node without the Check-In
// counter (IcdTestEventServer.ts:77-83).
var errICDCounter = errors.New("ICD counter triggers need the Check-In Protocol feature")

// testEventTrigger is triggerTestEvent (IcdTestEventServer.ts:46-75) for
// the ICD triggers; handled is false for any other, which the fleet's
// dispatch goes on with. The back-off trigger (…05) is out of scope, as
// in matter.js (:71-73).
func (d *demoICD) testEventTrigger(trigger uint64) (handled bool, err error) {
	if d == nil {
		return false, nil
	}
	d.mu.Lock()
	icd, counter := d.icd, d.counter
	d.mu.Unlock()
	if icd == nil {
		return false, nil
	}
	switch trigger {
	case triggerICDAddActiveMode:
		d.mu.Lock()
		d.keepActive = true
		d.mu.Unlock()
		icd.RequestActiveMode()
	case triggerICDRemoveActiveMode:
		d.mu.Lock()
		d.keepActive = false
		d.mu.Unlock()
	// The amounts CHIP's CheckInCounter advances by: the next Check-In
	// shows half the range used, or repeats the counter of the one before
	// (:56-63).
	case triggerICDInvalidateHalfCounterVals, triggerICDInvalidateAllCounterVals:
		if counter == nil {
			return true, errICDCounter
		}
		by := int64(0x7fffffff)
		if trigger == triggerICDInvalidateAllCounterVals {
			by = 0xffffffff
		}
		d.log.Info("icd.test_event.counter_advance", slog.Int64("by", by))
		return true, counter(by)
	case triggerICDDSLSForceSIT:
		return true, icd.SetOperatingMode(node.ICDModeSIT)
	case triggerICDDSLSWithdrawSIT:
		return true, icd.WithdrawForcedOperatingMode()
	default:
		return false, nil
	}
	return true, nil
}

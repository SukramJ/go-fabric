// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of matter.js packages/node/test/behaviors/general-diagnostics/GeneralDiagnosticsOperationalHoursTest.ts
// at 862ad75f: UpTime and TotalOperationalHours across clock steps and a
// restart (GeneralDiagnosticsServer.ts elapsedSince :60-68, #online
// :351-375, #updateTotalOperationalHoursCounter :384-392, the upTime and
// totalOperationalHours getters :518-553).
//
// matter.js runs a MockServerNode on MockTime and reads the root's state
// with stateOf; the port runs a fabrictest device on the site's
// clocktest.Fake and reads the root's GeneralDiagnostics server locally.
// MockTime keeps a monotonic and a wall clock: atTime moves both, as a
// clocktest.Fake's Set does, stepWallClock the wall clock alone, which a
// Fake cannot. The one wall-only case ported here drives the server's
// UpTime fold with the two elapsed times (UpTimeFrom); "keeps the
// operational time counted across a backward wall-clock step" stays
// missing. "counts elapsed online time normally", "folds a forward
// wall-clock step (a suspend) into upTime" and "never lowers upTime when a
// backward step follows a forward one" are
// TestGeneralDiagnosticsUpTimeCountsSuspendNeverLowered and
// TestGenDiag_UpTimeMonotonicallyNonDecreasing; operational hours across a
// restart is node:TestOperationalHoursSurviveARestart.
package core_test

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/fabrictest"
)

// operationalTimeSetting is the store key node persists the operational
// time under, in milliseconds (node/persist.go operationalTimeSetting) —
// matter.js's persisted totalOperationalHoursCounter.
const operationalTimeSetting = "gendiag.operational_ms"

// genDiagOf is the device's root GeneralDiagnostics server, the one
// node.stateOf(GeneralDiagnosticsServer) reads.
func genDiagOf(t *testing.T, d *fabrictest.Device) *core.GeneralDiagnostics {
	t.Helper()
	root := d.Bridge().Topology().FindByID(0)
	if root == nil {
		t.Fatal("no root endpoint")
	}
	for _, srv := range endpoint.ClusterServers(root) {
		if g, ok := srv.(*core.GeneralDiagnostics); ok {
			return g
		}
	}
	t.Fatal("no GeneralDiagnostics server on the root endpoint")
	return nil
}

// upTimeOf reads UpTime (0x0002) in seconds.
func upTimeOf(t *testing.T, g *core.GeneralDiagnostics) uint64 {
	t.Helper()
	v, _ := g.MatterRead(0x0002)
	u, ok := v.(uint64)
	if !ok {
		t.Fatalf("UpTime = %T", v)
	}
	return u
}

// totalOperationalHoursOf reads TotalOperationalHours (0x0003).
func totalOperationalHoursOf(t *testing.T, g *core.GeneralDiagnostics) uint32 {
	t.Helper()
	v, _ := g.MatterRead(0x0003)
	h, ok := v.(uint32)
	if !ok {
		t.Fatalf("TotalOperationalHours = %T", v)
	}
	return h
}

// persistedOperationalMs is the counter the node persisted, parsed as the
// unsigned integer node reads back at the next boot.
func persistedOperationalMs(t *testing.T, d *fabrictest.Device) uint64 {
	t.Helper()
	raw, ok, err := d.Store().GetSetting(context.Background(), operationalTimeSetting)
	if err != nil || !ok {
		t.Fatalf("persisted operational time: %q, %v, %v", raw, ok, err)
	}
	ms, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		t.Fatalf("persisted operational time %q is not an unsigned integer: %v", raw, err)
	}
	return ms
}

// onlineDevice is MockServerNode.createOnline: a device with no bridged
// endpoints on a fresh site.
func onlineDevice(t *testing.T) (*fabrictest.Site, *fabrictest.Device) {
	t.Helper()
	site := fabrictest.NewSite(t, fabrictest.Options{})
	return site, site.AddDevice(fabrictest.Topology{})
}

// TestGeneralDiagnosticsUpTimeCountsAcrossABackwardWallClockStep ports
// "keeps upTime counting across a backward wall-clock step"
// (GeneralDiagnosticsOperationalHoursTest.ts:42-53): 5 min, a wall-clock
// step of -2 h, UpTime 300; another minute, 360. The monotonic clock keeps
// counting, the stepped wall clock elapses less.
func TestGeneralDiagnosticsUpTimeCountsAcrossABackwardWallClockStep(t *testing.T) {
	t.Parallel()
	g := core.NewGeneralDiagnostics(core.BootReasonPowerOnReboot)
	step := -2 * time.Hour
	if got := g.UpTimeFrom(5*time.Minute, 5*time.Minute+step); got != 300*time.Second {
		t.Errorf("UpTime after the step = %v, want 300s", got)
	}
	if got := g.UpTimeFrom(6*time.Minute, 6*time.Minute+step); got != 360*time.Second {
		t.Errorf("UpTime a minute later = %v, want 360s", got)
	}
}

// TestGeneralDiagnosticsUpTimeHighWaterResetsAcrossARestart ports "resets
// upTime's high-water mark across a restart"
// (GeneralDiagnosticsOperationalHoursTest.ts:82-96): 5 min, a forward step
// of 10 min, UpTime 900; after stop and start, 0. matter.js resets the mark
// in #online (:364); a Go restart builds a new server.
func TestGeneralDiagnosticsUpTimeHighWaterResetsAcrossARestart(t *testing.T) {
	t.Parallel()
	site, d := onlineDevice(t)
	fake := site.Fake()
	fake.Advance(5 * time.Minute)
	fake.Set(fake.Now().Add(10 * time.Minute))
	if got := upTimeOf(t, genDiagOf(t, d)); got != 900 {
		t.Fatalf("UpTime = %d, want 900", got)
	}

	if err := d.Restart(context.Background()); err != nil {
		t.Fatal(err)
	}

	if got := upTimeOf(t, genDiagOf(t, d)); got != 0 {
		t.Errorf("UpTime after the restart = %d, want 0", got)
	}
}

// TestGeneralDiagnosticsTotalOperationalHoursNonNegativeOnABackwardStep
// ports "keeps totalOperationalHours non-negative when the clock steps
// backwards" (GeneralDiagnosticsOperationalHoursTest.ts:98-108): 10 min,
// then a step of -2 h on both clocks. matter.js asserts at least 0; the Go
// attribute is a uint32, so the assertion is that it does not wrap: the
// 10 min counted read 0 hours (elapsedSince floors the step at zero).
func TestGeneralDiagnosticsTotalOperationalHoursNonNegativeOnABackwardStep(t *testing.T) {
	t.Parallel()
	site, d := onlineDevice(t)
	fake := site.Fake()
	fake.Advance(10 * time.Minute)
	g := genDiagOf(t, d)

	now := fake.Now()
	fake.Set(now.Add(-2 * time.Hour))
	got := totalOperationalHoursOf(t, g)
	fake.Set(now)

	if got != 0 {
		t.Errorf("TotalOperationalHours = %d after a backward step, want 0", got)
	}
}

// TestGeneralDiagnosticsUpTimeNonNegativeOnABackwardStep ports "keeps upTime
// non-negative when the clock steps backwards"
// (GeneralDiagnosticsOperationalHoursTest.ts:110-122): 5 min, UpTime 300,
// then a step of -2 h on both clocks. matter.js asserts at least 0; on the
// uint64 attribute the assertion is no wrap, and the high-water mark holds
// it at 300 in both.
func TestGeneralDiagnosticsUpTimeNonNegativeOnABackwardStep(t *testing.T) {
	t.Parallel()
	site, d := onlineDevice(t)
	fake := site.Fake()
	fake.Advance(5 * time.Minute)
	g := genDiagOf(t, d)
	if got := upTimeOf(t, g); got != 300 {
		t.Fatalf("UpTime = %d, want 300", got)
	}

	now := fake.Now()
	fake.Set(now.Add(-2 * time.Hour))
	got := upTimeOf(t, g)
	fake.Set(now)

	if got != 300 {
		t.Errorf("UpTime = %d after a backward step, want 300 (not negative, not wrapped)", got)
	}
}

// TestGeneralDiagnosticsPersistedCounterIsAnInteger ports "keeps the
// persisted counter an integer despite a fractional Time.nowUs reading"
// (GeneralDiagnosticsOperationalHoursTest.ts:124-146): after 10 min the
// persisted counter is above 0 and an integer. A Go clock reads whole
// nanoseconds, so no reading is fractional; the assertion is on what the
// node persisted: an unsigned integer millisecond count above 0.
func TestGeneralDiagnosticsPersistedCounterIsAnInteger(t *testing.T) {
	t.Parallel()
	site, d := onlineDevice(t)
	ctx := context.Background()
	// The node persists every minute on a ticker its keeper goroutine arms.
	if err := site.Settle(ctx); err != nil {
		t.Fatal(err)
	}
	site.Fake().Advance(10 * time.Minute)
	if err := site.Settle(ctx); err != nil {
		t.Fatal(err)
	}
	if ms := persistedOperationalMs(t, d); ms == 0 {
		t.Error("persisted operational time = 0 ms after 10 min, want more")
	}
}

// TestGeneralDiagnosticsGoesOfflineCleanlyOnABackwardStep ports "takes the
// node offline cleanly when the clock steps backwards"
// (GeneralDiagnosticsOperationalHoursTest.ts:148-156): 10 min, then the node
// closes at a time 2 h earlier and is offline. #goingOffline updates the
// counter once more (:377-382); floored at zero, the step adds nothing to
// the 10 min counted. The Go assertion adds what "cleanly" protects: the
// counter Stop persisted is that 10 min, so the next boot loads it.
func TestGeneralDiagnosticsGoesOfflineCleanlyOnABackwardStep(t *testing.T) {
	t.Parallel()
	site, d := onlineDevice(t)
	fake := site.Fake()
	fake.Advance(10 * time.Minute)
	if got := genDiagOf(t, d).TotalOperationalTime(); got != 10*time.Minute {
		t.Fatalf("operational time = %v, want 10m", got)
	}

	fake.Set(fake.Now().Add(-2 * time.Hour))
	if err := d.Stop(context.Background()); err != nil {
		t.Fatalf("Stop after a backward step: %v", err)
	}

	if ms := persistedOperationalMs(t, d); ms != uint64((10 * time.Minute).Milliseconds()) {
		t.Errorf("persisted operational time = %d ms, want %d", ms, (10 * time.Minute).Milliseconds())
	}
	if err := d.Start(context.Background()); err != nil {
		t.Errorf("the next boot after a backward step: %v", err)
	}
}

// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"context"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/node"
)

// TestICDLITFlag: with --icd-lit the node is a LIT-capable ICD that starts
// in SIT (registration driven, IcdTestInstance.ts:189 at 862ad75f), the
// cadence takes it idle after the active window and wakes it after the
// idle window (IcdTestEventServer.ts:89-122), and the ICD test event
// triggers move the Check-In counter by CHIP's amounts and switch the
// Dynamic SIT/LIT mode (:46-75); any other trigger is left to the fleet.
func TestICDLITFlag(t *testing.T) {
	f := newFleet()
	cfg := icdConfig(1, 300)
	f.icd = newDemoICD(cfg, slog.New(slog.DiscardHandler))
	n := testNodeWith(t, f, filepath.Join(t.TempDir(), "icd.db"), func(c *node.Config) { c.ICD = cfg })
	t.Cleanup(f.icd.stop)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	if err := n.Start(ctx); err != nil {
		t.Fatal(err)
	}
	icd := n.ICD()
	if icd == nil {
		t.Fatal("--icd-lit built a node that is no ICD")
	}
	if icd.Mode() != node.ICDModeSIT {
		t.Errorf("operating mode %v at start, want SIT until a client registers", icd.Mode())
	}
	wait := func(what string, cond func() bool) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for !cond() {
			if time.Now().After(deadline) {
				t.Fatalf("no %s within 5s", what)
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	wait("idle mode after the active window", func() bool { return !icd.Active() })
	wait("wake after the idle window", icd.Active)

	counter := n.Root().ICDManagement.Counter()
	before := counter.Value()
	for _, c := range []struct {
		trigger uint64
		by      uint32
	}{{triggerICDInvalidateHalfCounterVals, 0x7fffffff}, {triggerICDInvalidateAllCounterVals, 0xffffffff}} {
		handled, err := f.icd.testEventTrigger(c.trigger)
		if !handled || err != nil {
			t.Fatalf("trigger 0x%016x: handled %v, %v", c.trigger, handled, err)
		}
		if got := counter.Value(); got-before < c.by {
			t.Errorf("trigger 0x%016x moved the counter by %d, want at least %d", c.trigger, got-before, c.by)
		}
		before = counter.Value()
	}
	for _, trig := range []uint64{triggerICDAddActiveMode, triggerICDRemoveActiveMode, triggerICDDSLSForceSIT, triggerICDDSLSWithdrawSIT} {
		if handled, err := f.icd.testEventTrigger(trig); !handled || err != nil {
			t.Errorf("trigger 0x%016x: handled %v, %v", trig, handled, err)
		}
	}
	if handled, _ := f.icd.testEventTrigger(0x0046000000000005); handled {
		t.Error("the back-off trigger is handled, matter.js leaves it out (:71-73)")
	}
	if err := f.testEventTrigger(ctx, triggerICDRemoveActiveMode); err != nil {
		t.Errorf("the fleet's dispatch: %v", err)
	}
	var none *demoICD
	if handled, _ := none.testEventTrigger(triggerICDAddActiveMode); handled {
		t.Error("an ICD trigger handled without --icd-lit")
	}
	none.stop()
}

// TestICDFlagsNeedICDLIT: the timing flags without --icd-lit stop the
// daemon.
func TestICDFlagsNeedICDLIT(t *testing.T) {
	f := flags{passcode: 20202021, iterations: 1000, icdIdleSeconds: 5, icdActiveMillis: defaultICDActiveMillis}
	if _, _, err := checkFlags(f); err == nil {
		t.Error("--icd-idle-mode-duration accepted without --icd-lit")
	}
	f.icdLIT = true
	if _, _, err := checkFlags(f); err == nil {
		t.Error("an active window of 10 s accepted over an idle window of 5 s")
	}
	f.icdActiveMillis = 1000
	if _, _, err := checkFlags(f); err != nil {
		t.Errorf("with --icd-lit: %v", err)
	}
	f.icdIdleSeconds = 64801
	if _, _, err := checkFlags(f); err == nil {
		t.Error("an idle window past 18 h accepted")
	}
}

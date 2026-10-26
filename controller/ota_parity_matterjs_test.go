// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controller_test

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	mattercore "github.com/SukramJ/go-fabric/cluster/core"
	otareq "github.com/SukramJ/go-fabric/cluster/spec/otasoftwareupdaterequestor"
	"github.com/SukramJ/go-fabric/controller"
	"github.com/SukramJ/go-fabric/controller/softwareupdate"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/node"
)

// The SoftwareUpdateManager cases of matter.js's OtaTest
// (packages/node/test/behaviors/ota/OtaTest.ts at 862ad75f) that run a
// device: the transfer options of a consent, the queued path and the
// cancel by removing the consent. The cases that set the queue's state
// directly are the manager's (controller/softwareupdate).

func u16(v uint16) *uint16 { return &v }

func dur(d time.Duration) *time.Duration { return &d }

func eqU16(a, b *uint16) bool { return (a == nil) == (b == nil) && (a == nil || *a == *b) }

func eqDur(a, b *time.Duration) bool { return (a == nil) == (b == nil) && (a == nil || *a == *b) }

// optionsRig is otaSiteWithImage (OtaTest.ts:1126-1155): a commissioned
// pair whose device is isolated, so a consent's update stays queued.
func optionsRig(t *testing.T, edit func(*controller.OTAConfig)) (r *otaRig, consentTo func(softwareupdate.Target) error, expect func(maxBlock *uint16, delay *time.Duration)) {
	t.Helper()
	r = newOTARig(t, edit)
	r.storeImage(t, 2)
	r.isolate(true)
	mgr := r.c.SoftwareUpdates()
	consentTo = func(tg softwareupdate.Target) error {
		tg.VendorID, tg.ProductID, tg.TargetSoftwareVersion = 0xFFF1, 0x8001, 2
		_, err := mgr.AddUpdateConsent(context.Background(), r.peer(t), tg)
		return err
	}
	expect = func(maxBlock *uint16, delay *time.Duration) {
		t.Helper()
		got := mgr.TransferOptionsFor(r.peer(t))
		if !eqU16(got.MaxBlockSize, maxBlock) || !eqDur(got.AdditionalMrpDelay, delay) {
			t.Errorf("transfer options %v/%v, want %v/%v", got.MaxBlockSize, got.AdditionalMrpDelay, maxBlock, delay)
		}
	}
	return r, consentTo, expect
}

// TestOTATransferOptionsParity: nothing configured leaves the options to
// the peer and its medium; the defaults, then a consent's options, decide
// — OtaTest.ts:1157-1174, bdxTransferOptionsFor
// (SoftwareUpdateManager.ts:1133-1144). matter.js sets the defaults on the
// running manager's state; here they are the configuration of a second
// rig.
func TestOTATransferOptionsParity(t *testing.T) {
	t.Parallel()
	t.Run("nothing configured", func(t *testing.T) {
		t.Parallel()
		_, _, expect := optionsRig(t, nil)
		expect(nil, nil)
	})
	t.Run("defaults, then the consent's", func(t *testing.T) {
		t.Parallel()
		_, consentTo, expect := optionsRig(t, func(o *controller.OTAConfig) {
			o.MaxBDXBlockSize, o.BDXAdditionalMrpDelay = 512, dur(2*time.Second)
		})
		expect(u16(512), dur(2*time.Second))
		if err := consentTo(softwareupdate.Target{MaxBDXBlockSize: u16(256), BDXAdditionalMrpDelay: dur(500 * time.Millisecond)}); err != nil {
			t.Fatal(err)
		}
		expect(u16(256), dur(500*time.Millisecond))
	})
}

// TestOTAConsentChangesQueuedOptionsParity: a later consent changes and
// clears the options of a queued update — OtaTest.ts:1176-1188,
// #queueUpdate (:892-920).
func TestOTAConsentChangesQueuedOptionsParity(t *testing.T) {
	t.Parallel()
	r, consentTo, expect := optionsRig(t, nil)
	for _, step := range []struct {
		tg       softwareupdate.Target
		maxBlock *uint16
		delay    *time.Duration
	}{
		{softwareupdate.Target{MaxBDXBlockSize: u16(256), BDXAdditionalMrpDelay: dur(500 * time.Millisecond)}, u16(256), dur(500 * time.Millisecond)},
		{softwareupdate.Target{MaxBDXBlockSize: u16(128)}, u16(128), nil},
		{softwareupdate.Target{}, nil, nil},
	} {
		if err := consentTo(step.tg); err != nil {
			t.Fatal(err)
		}
		expect(step.maxBlock, step.delay)
	}
	if q := r.c.SoftwareUpdates().QueuedUpdates(); len(q) != 1 || q[0].Status != softwareupdate.PendingQueued {
		t.Errorf("queue %+v, want the one queued entry", q)
	}
}

// TestOTARejectsInvalidTransferOptionsParity: invalid options are refused
// before any state moves, by AddUpdateConsent and by ForceUpdate —
// OtaTest.ts:1190-1214, assertTransferOptions. A negative, fractional or
// NaN block size and a NaN delay are not representable in Go's types;
// the cases left are a block size of 0 and a negative delay.
func TestOTARejectsInvalidTransferOptionsParity(t *testing.T) {
	t.Parallel()
	r, consentTo, _ := optionsRig(t, nil)
	for _, tg := range []softwareupdate.Target{
		{MaxBDXBlockSize: u16(0)},
		{BDXAdditionalMrpDelay: dur(-time.Millisecond)},
	} {
		if err := consentTo(tg); !errors.Is(err, softwareupdate.ErrTransferOptions) {
			t.Errorf("consent %+v: %v, want ErrTransferOptions", tg, err)
		}
	}
	mgr := r.c.SoftwareUpdates()
	err := mgr.ForceUpdate(context.Background(), r.peer(t), softwareupdate.Target{TargetSoftwareVersion: 1, MaxBDXBlockSize: u16(0)})
	if !errors.Is(err, softwareupdate.ErrTransferOptions) {
		t.Errorf("ForceUpdate: %v, want ErrTransferOptions", err)
	}
	if q := mgr.QueuedUpdates(); len(q) != 0 {
		t.Errorf("queue %+v, want empty", q)
	}
	if mgr.HasConsent(r.peer(t), 0) {
		t.Error("a consent was added")
	}
}

// TestOTAQueuedUpdateViaConsentParity: AddUpdateConsent queues the
// update — one entry for the target version — which goes in progress with
// the announcement and leaves the queue once the device runs the new
// version — OtaTest.ts:1216-1291, addUpdateConsent (:1153-1196).
func TestOTAQueuedUpdateViaConsentParity(t *testing.T) {
	t.Parallel()
	r := newOTARig(t, nil)
	r.storeImage(t, 2)
	r.isolate(true)
	mgr := r.c.SoftwareUpdates()
	var added bool
	r.run(t, func(ctx context.Context) (err error) {
		added, err = mgr.AddUpdateConsent(ctx, r.peer(t), softwareupdate.Target{VendorID: 0xFFF1, ProductID: 0x8001, TargetSoftwareVersion: 2})
		return err
	})
	if !added {
		t.Fatal("not queued")
	}
	if q := mgr.QueuedUpdates(); len(q) != 1 || q[0].TargetSoftwareVersion != 2 {
		t.Fatalf("queue %+v, want one entry for version 2", q)
	}
	r.isolate(false)
	r.eventually(t, "the update in progress", func() bool {
		q := mgr.QueuedUpdates()
		return len(q) == 1 && q[0].Status == softwareupdate.PendingInProgress || len(q) == 0
	})
	r.eventually(t, "the update done", func() bool { _, done, _ := r.snapshot(); return done > 0 })
	if q := mgr.QueuedUpdates(); len(q) != 0 {
		t.Errorf("queue %+v after the update, want empty", q)
	}
}

// TestOTANoUpdatesWithoutRequestorParity: a node without an OTA requestor
// is no update target: the check reports nothing — OtaTest.ts:1293-1347.
// The rule is #preparePeerForUpdate (SoftwareUpdateManager.ts:703-776).
func TestOTANoUpdatesWithoutRequestorParity(t *testing.T) {
	t.Parallel()
	r := newOTARigWith(t, nil, nil)
	r.storeImage(t, 2)
	var got []softwareupdate.PeerUpdate
	r.run(t, func(ctx context.Context) (err error) {
		got, err = r.c.SoftwareUpdates().QueryUpdates(ctx, nil, false)
		return err
	})
	if len(got) != 0 {
		t.Errorf("updates %+v, want none", got)
	}
	r.run(t, func(ctx context.Context) error {
		if ok, err := r.c.SoftwareUpdates().AddUpdateConsent(ctx, r.peer(t), softwareupdate.Target{VendorID: 0xFFF1, ProductID: 0x8001, TargetSoftwareVersion: 2}); ok || err != nil {
			t.Errorf("AddUpdateConsent = %v, %v; want not applicable", ok, err)
		}
		return nil
	})
}

// TestOTACancelByRemovingConsentParity: a forced update whose consent is
// removed before the device queries is not downloaded: the device returns
// to Idle on the old version and the manager reports the update failed —
// OtaTest.ts:1049-1124, removeConsent (:1217) and #cancelUpdate
// (:1091-1123). The device is isolated while the consent comes and goes,
// so its query follows the removal, as MockTime orders it there.
func TestOTACancelByRemovingConsentParity(t *testing.T) {
	t.Parallel()
	// A requestor that cannot consent, as matter.js's requestUserConsent:
	// false (outside harness mode, node/ota.go).
	r := newOTARigWith(t, nil, &node.OtaConfig{Dir: filepath.Join(t.TempDir(), "ota")})
	r.storeImage(t, 2)
	mgr := r.c.SoftwareUpdates()
	r.isolate(true)
	tg := softwareupdate.Target{VendorID: 0xFFF1, ProductID: 0x8001, TargetSoftwareVersion: 2}
	r.run(t, func(ctx context.Context) error { return mgr.ForceUpdate(ctx, r.peer(t), tg) })
	mgr.RemoveConsent(r.peer(t), 0)
	_, _, provider := r.c.OwnNode()
	dev := r.dev.Addr().String()
	var queried atomic.Bool
	r.site.Net().Filter(func(d fabrictest.Datagram) bool {
		if d.From.String() == dev && d.To.String() == provider.String() {
			queried.Store(true)
		}
		return true
	})
	r.eventually(t, "the update failed", func() bool { _, _, failed := r.snapshot(); return failed > 0 })
	if q := mgr.QueuedUpdates(); len(q) != 0 {
		t.Errorf("queue %+v, want empty", q)
	}
	// The requestor queries after its announcement jitter: the site's
	// clock is stepped over it rather than walked timer by timer.
	r.run(t, func(ctx context.Context) error { return r.site.Step(ctx, 15*time.Minute) })
	r.eventually(t, "the device's query", queried.Load)
	r.eventually(t, "the requestor idle", func() bool {
		v, _ := r.dev.Node().Root().OTARequestor.MatterRead(otareq.AttrUpdateState)
		return fmt.Sprint(v) == strconv.Itoa(int(otareq.UpdateStateIdle))
	})
	statuses, done, _ := r.snapshot()
	if done != 0 || slices.Contains(statuses, mattercore.OTAUpdateDownloading) {
		t.Errorf("statuses %v, done %d: the image was downloaded", statuses, done)
	}
	if v, _ := r.dev.Node().Root().BasicInformation.MatterRead(0x0009); v != uint32(1) {
		t.Errorf("device SoftwareVersion %v, want 1", v)
	}
}

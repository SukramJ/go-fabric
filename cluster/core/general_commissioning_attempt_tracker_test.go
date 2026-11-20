// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core_test

import (
	"context"
	"slices"
	"sync"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/im"
)

// trackerLog records the attempt tracker's calls and the closer's, in order.
type trackerLog struct {
	mu     sync.Mutex
	events []string
}

func (l *trackerLog) add(e string) {
	l.mu.Lock()
	l.events = append(l.events, e)
	l.mu.Unlock()
}

func (l *trackerLog) get() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.Clone(l.events)
}

func newTrackedGC(t *testing.T) (*core.GeneralCommissioning, *trackerLog) {
	t.Helper()
	gc, err := core.NewGeneralCommissioning(core.GeneralCommissioningConfig{
		LocationCapability: core.RegulatoryIndoor, FailSafeMaxSeconds: 900,
	})
	if err != nil {
		t.Fatal(err)
	}
	log := &trackerLog{}
	gc.SetFailSafeAttemptTracker(func(_ context.Context, started bool) {
		if started {
			log.add("start")
		} else {
			log.add("closed")
		}
	})
	gc.SetFailSafeSessionCloser(func(context.Context) []uint16 {
		log.add("end")
		return nil
	})
	return gc, log
}

func armOver(ctx context.Context, t *testing.T, gc *core.GeneralCommissioning, expiry uint16) {
	t.Helper()
	resp, err := gc.MatterInvoke(ctx, 0x00, core.ArmFailSafeRequest{ExpiryLengthSeconds: expiry})
	if err != nil {
		t.Fatal(err)
	}
	if code := resp.(core.ArmFailSafeResponse).ErrorCode; code != core.CommissioningErrorOK {
		t.Fatalf("ArmFailSafe(%d) = %d", expiry, code)
	}
}

// TestGeneralCommissioning_FailSafeAttemptTracker: the tracker hears every
// start of a fail-safe context — where matter.js
// GeneralCommissioningServer.ts:101-113 at b25ed960 calls
// DeviceCommissioner.beginTimed — and no extend, before the context's end
// reaches the session closer, and the server's Close.
func TestGeneralCommissioning_FailSafeAttemptTracker(t *testing.T) {
	t.Parallel()
	pase := im.WithAuthModePASE(context.Background())
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	t.Run("ArmFailSafe starts a context once; an extend starts none", func(t *testing.T) {
		t.Parallel()
		gc, log := newTrackedGC(t)
		armOver(pase, t, gc, 60)
		armOver(pase, t, gc, 120) // extend
		armOver(pase, t, gc, 0)   // ends it
		if got, want := log.get(), []string{"start", "end"}; !slices.Equal(got, want) {
			t.Fatalf("events = %v, want %v", got, want)
		}
	})

	t.Run("the internal arms start a context once", func(t *testing.T) {
		t.Parallel()
		gc, log := newTrackedGC(t)
		if err := gc.ArmOnPaseSession(ctx, 7); err != nil {
			t.Fatal(err)
		}
		if err := gc.ArmOnPaseSession(ctx, 7); err != nil { // extend
			t.Fatal(err)
		}
		gc.ExpireFailSafe(ctx)
		if err := gc.ArmFailSafeFor(ctx, 60, 0); err != nil {
			t.Fatal(err)
		}
		gc.ExpireFailSafe(ctx)
		if got, want := log.get(), []string{"start", "end", "start", "end"}; !slices.Equal(got, want) {
			t.Fatalf("events = %v, want %v", got, want)
		}
	})

	t.Run("Close tells the tracker before the stop's expiry", func(t *testing.T) {
		t.Parallel()
		gc, log := newTrackedGC(t)
		armOver(pase, t, gc, 60)
		if err := gc.Close(ctx); err != nil {
			t.Fatal(err)
		}
		gc.ExpireFailSafe(ctx)
		if got, want := log.get(), []string{"start", "closed", "end"}; !slices.Equal(got, want) {
			t.Fatalf("events = %v, want %v", got, want)
		}
	})

	t.Run("an arm refused over CASE with a window open starts nothing", func(t *testing.T) {
		t.Parallel()
		gc, log := newTrackedGC(t)
		gc.SetIsCommissioningWindowOpen(func() bool { return true })
		resp, err := gc.MatterInvoke(im.WithFabricFilter(context.Background(), true, 1), 0x00, core.ArmFailSafeRequest{ExpiryLengthSeconds: 60})
		if err != nil {
			t.Fatal(err)
		}
		if code := resp.(core.ArmFailSafeResponse).ErrorCode; code != core.CommissioningErrorBusyWithOtherAdmin {
			t.Fatalf("ArmFailSafe over CASE = %d, want BusyWithOtherAdmin", code)
		}
		if got := log.get(); len(got) != 0 {
			t.Fatalf("events = %v, want none", got)
		}
	})
}

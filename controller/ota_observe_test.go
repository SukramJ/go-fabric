// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controller_test

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"sync"
	"testing"

	"github.com/SukramJ/go-fabric/controller/softwareupdate"
	"github.com/SukramJ/go-fabric/node"
)

// updateEvents collects softwareupdate.Manager.Observe's events.
type updateEvents struct {
	mu     sync.Mutex
	events []softwareupdate.UpdateEvent
}

func (u *updateEvents) add(ev softwareupdate.UpdateEvent) {
	u.mu.Lock()
	u.events = append(u.events, ev)
	u.mu.Unlock()
}

func (u *updateEvents) snapshot() []softwareupdate.UpdateEvent {
	u.mu.Lock()
	defer u.mu.Unlock()
	return slices.Clone(u.events)
}

func (u *updateEvents) kinds() []string {
	evs := u.snapshot()
	out := make([]string, len(evs))
	for i := range evs {
		out[i] = evs[i].Kind.String()
	}
	return out
}

func (u *updateEvents) has(k softwareupdate.UpdateEventKind) func() bool {
	return func() bool {
		return slices.ContainsFunc(u.snapshot(), func(e softwareupdate.UpdateEvent) bool { return e.Kind == k })
	}
}

// TestOTAObserveAppliedUpdate: the observer of a queued and applied
// update — consent, announcement, the BDX download, apply, the applied
// notification (OtaTest.ts:91 at 862ad75f) — sees the consent, the
// update queued (#queueUpdate, SoftwareUpdateManager.ts:892-920), its
// transfer started (the provider's Downloading at the BDX session start,
// OtaSoftwareUpdateProviderServer.ts:308-327), then the update applying
// and done (SoftwareUpdateManager.ts:1317, :1279), each for the device's
// peer and the target.
func TestOTAObserveAppliedUpdate(t *testing.T) {
	t.Parallel()
	r := newOTARig(t, nil)
	r.storeImage(t, 2)
	mgr := r.c.SoftwareUpdates()
	u := &updateEvents{}
	defer mgr.Observe(u.add)()
	tg := softwareupdate.Target{VendorID: 0xFFF1, ProductID: 0x8001, TargetSoftwareVersion: 2}
	r.run(t, func(ctx context.Context) error {
		_, err := mgr.AddUpdateConsent(ctx, r.peer(t), tg)
		return err
	})
	r.eventually(t, "the done event", u.has(softwareupdate.UpdateDone))
	if got, want := u.kinds(), []string{"consent_changed", "queued", "transfer_started", "applying", "done"}; !slices.Equal(got, want) {
		t.Fatalf("events %q, want %q", got, want)
	}
	p := r.peer(t)
	for _, e := range u.snapshot() {
		if e.Peer != p || e.Target.TargetSoftwareVersion != 2 || e.Err != nil {
			t.Errorf("event %+v", e)
		}
	}
	if !u.snapshot()[0].Consented {
		t.Error("the consent event does not report the consent held")
	}
}

// TestOTAObserveRemovedConsent: a forced update whose consent is removed
// before the device fetched anything is reported as the consent added,
// the update queued, the consent removed and the update failed as
// cancelled (removeConsent
// → #cancelUpdate, SoftwareUpdateManager.ts:1217-1232, :1091-1123).
func TestOTAObserveRemovedConsent(t *testing.T) {
	t.Parallel()
	r := newOTARigWith(t, nil, &node.OtaConfig{Dir: filepath.Join(t.TempDir(), "ota")})
	r.storeImage(t, 2)
	mgr := r.c.SoftwareUpdates()
	u := &updateEvents{}
	defer mgr.Observe(u.add)()
	r.isolate(true)
	tg := softwareupdate.Target{VendorID: 0xFFF1, ProductID: 0x8001, TargetSoftwareVersion: 2}
	r.run(t, func(ctx context.Context) error { return mgr.ForceUpdate(ctx, r.peer(t), tg) })
	mgr.RemoveConsent(r.peer(t), 0)
	r.eventually(t, "the failed event", u.has(softwareupdate.UpdateFailed))
	r.isolate(false)
	evs := u.snapshot()
	if got, want := u.kinds(), []string{"consent_changed", "queued", "consent_changed", "failed"}; !slices.Equal(got, want) {
		t.Fatalf("events %q, want %q", got, want)
	}
	if !evs[0].Consented || evs[2].Consented || !errors.Is(evs[3].Err, softwareupdate.ErrUpdateCancelled) {
		t.Errorf("events %+v", evs)
	}
}

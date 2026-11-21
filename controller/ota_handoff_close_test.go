// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controller_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/controller"
	"github.com/SukramJ/go-fabric/controller/softwareupdate"
)

// deletedWithQueuedUpdate is queuedWithHeldQuery with the node forgotten:
// the update manager's NodeDeleted runs on its hand-off goroutine.
func deletedWithQueuedUpdate(t *testing.T, onFailed func(r *otaRig, p softwareupdate.Peer)) (r *otaRig, release func()) {
	t.Helper()
	r, release = queuedWithHeldQuery(t, onFailed)
	reg := r.c.Registry()
	r.run(t, func(ctx context.Context) error { return reg.Forget(ctx, r.n) })
	return r, release
}

// queuedWithHeldQuery is an OTA rig whose node has an update queued — its
// QueryImage held in the provider until the returned release — with
// onFailed as the host's OnUpdateFailed.
func queuedWithHeldQuery(t *testing.T, onFailed func(r *otaRig, p softwareupdate.Peer)) (r *otaRig, release func()) {
	t.Helper()
	entered, held := make(chan struct{}), make(chan struct{})
	var hold, once sync.Once
	release = func() { once.Do(func() { close(held) }) }
	t.Cleanup(release)
	var rig *otaRig
	r = newOTARig(t, func(o *controller.OTAConfig) {
		o.ScriptQueryImage = func(softwareupdate.Peer, softwareupdate.QueryImageRequest) (softwareupdate.QueryImageScript, bool) {
			hold.Do(func() {
				close(entered)
				<-held
			})
			return softwareupdate.QueryImageScript{}, false
		}
		o.OnUpdateFailed = func(p softwareupdate.Peer) { onFailed(rig, p) }
	})
	rig = r
	r.storeImage(t, 2)
	peer := r.peer(t)
	r.run(t, func(ctx context.Context) error {
		_, err := r.c.SoftwareUpdates().AddUpdateConsent(ctx, peer, softwareupdate.Target{VendorID: 0xFFF1, ProductID: 0x8001, TargetSoftwareVersion: 2})
		return err
	})
	r.run(t, func(ctx context.Context) error {
		select {
		case <-entered:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})
	return r, release
}

// TestCloseAfterADeletedNodeWithAQueuedUpdate (review II, fable-read-6b-ctl
// item 5; C-17): the update manager's NodeDeleted, run on its hand-off
// goroutine for a node the registry deleted with an update queued, ends
// the update without an event (#forgetPeer, SoftwareUpdateManager.ts:312-341
// at b25ed960), so the hand-off reaches no host OnUpdateFailed — one that
// would block or close the controller is never called — and Close returns.
// Before C-17 NodeDeleted reported the update failed from the hand-off,
// and these were two cases: a blocking callback and one that closes the
// controller.
func TestCloseAfterADeletedNodeWithAQueuedUpdate(t *testing.T) {
	t.Parallel()
	called := make(chan softwareupdate.Peer, 4)
	r, release := deletedWithQueuedUpdate(t, func(_ *otaRig, p softwareupdate.Peer) {
		called <- p
		select {} // a host callback that never returns
	})
	release()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if err := r.c.Close(ctx); err != nil {
		t.Fatalf("Close after the deleted node: %v", err)
	}
	select {
	case p := <-called:
		t.Fatalf("OnUpdateFailed for %v: the deleted node's update ends without an event", p)
	default:
	}
}

// TestOnUpdateFailedMayCloseTheController (review II, fable-read-6b-ctl
// item 5; review-a L1): a host OnUpdateFailed that closes the controller
// returns, and so does the Close it called and the RemoveConsent that
// reported the failure: a change's events run on its own caller, after its
// queue run, never on an announcement goroutine Close waits for
// (#mutate, SoftwareUpdateManager.ts:539-561 at b25ed960).
func TestOnUpdateFailedMayCloseTheController(t *testing.T) {
	t.Parallel()
	closed := make(chan error, 1)
	var once sync.Once
	var release func()
	set := make(chan struct{})
	r, release := queuedWithHeldQuery(t, func(r *otaRig, _ softwareupdate.Peer) {
		once.Do(func() {
			<-set // release is assigned
			release()
			closed <- r.c.Close(context.Background())
		})
	})
	close(set)
	peer := r.peer(t)
	removed := make(chan struct{})
	go func() {
		defer close(removed)
		r.c.SoftwareUpdates().RemoveConsent(peer, 0)
	}()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	select {
	case err := <-closed:
		if err != nil {
			t.Fatalf("Close from OnUpdateFailed: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("Close from OnUpdateFailed did not return")
	}
	select {
	case <-removed:
	case <-ctx.Done():
		t.Fatal("RemoveConsent did not return after its OnUpdateFailed closed the controller")
	}
}

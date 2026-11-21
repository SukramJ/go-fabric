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

// deletedWithQueuedUpdate is an OTA rig whose node has an update queued —
// its QueryImage held in the provider until the returned release — with
// onFailed as the host's OnUpdateFailed, and the node forgotten: the
// update manager's NodeDeleted runs on its hand-off goroutine.
func deletedWithQueuedUpdate(t *testing.T, onFailed func(r *otaRig, p softwareupdate.Peer)) (r *otaRig, release func()) {
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
	reg := r.c.Registry()
	r.run(t, func(ctx context.Context) error { return reg.Forget(ctx, r.n) })
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

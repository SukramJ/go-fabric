// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controller_test

import (
	"context"
	"sync"
	"testing"

	"github.com/SukramJ/go-fabric/controller"
	"github.com/SukramJ/go-fabric/controller/softwareupdate"
)

// deletedWithQueuedUpdate is an OTA rig whose node has an update queued —
// its QueryImage held in the provider until the returned release — with
// onFailed as the host's OnUpdateFailed, and the node forgotten: the
// update manager's NodeDeleted runs on its hand-off goroutine and calls
// onFailed.
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

// TestCloseDoesNotWaitForABlockedOnUpdateFailed (review II, fable-read-6b-ctl
// item 5): a host OnUpdateFailed that blocks, called for a node the
// registry deleted, does not hold Close: Close waits for the hand-off only
// until it reaches the host's callback. Before, Close waited for the
// whole NodeDeleted call without its ctx, and the test hung.
func TestCloseDoesNotWaitForABlockedOnUpdateFailed(t *testing.T) {
	t.Parallel()
	inHost, unblock := make(chan struct{}), make(chan struct{})
	var entered sync.Once
	r, release := deletedWithQueuedUpdate(t, func(_ *otaRig, _ softwareupdate.Peer) {
		entered.Do(func() { close(inHost) })
		<-unblock
	})
	defer close(unblock)
	<-inHost
	release()
	if err := r.c.Close(context.Background()); err != nil {
		t.Fatalf("Close with a blocked OnUpdateFailed: %v", err)
	}
}

// TestOnUpdateFailedMayCloseTheController (review II, fable-read-6b-ctl
// item 5): a host OnUpdateFailed that closes the controller, called for a
// node the registry deleted, returns, and so does the Close it called.
// Before, that Close waited for the hand-off goroutine that ran it.
func TestOnUpdateFailedMayCloseTheController(t *testing.T) {
	t.Parallel()
	closed := make(chan error, 1)
	var once sync.Once
	var release func()
	var mu sync.Mutex
	mu.Lock()
	_, release = deletedWithQueuedUpdate(t, func(r *otaRig, _ softwareupdate.Peer) {
		once.Do(func() {
			mu.Lock() // release is set
			mu.Unlock()
			release()
			closed <- r.c.Close(context.Background())
		})
	})
	mu.Unlock()
	if err := <-closed; err != nil {
		t.Fatalf("Close from OnUpdateFailed: %v", err)
	}
}

// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controller_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/controller"
	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/controller/softwareupdate"
)

// TestOTAForgetsANodeTheRegistryDeleted (review II, R2-P6-bound-1914,
// decision 219): a node the registry deletes is forgotten by the update
// manager at once — its queued update is reported failed through the
// host's OnUpdateFailed and leaves the queue. Erase deletes the node inside
// the registry's fabric operation; OnUpdateFailed runs after it, so a host
// callback that starts a registry operation (here ForDevice, which takes
// the operation lock) returns. Before, nothing told the manager.
func TestOTAForgetsANodeTheRegistryDeleted(t *testing.T) {
	t.Parallel()
	entered, release := make(chan struct{}), make(chan struct{})
	var hold sync.Once
	var reg *client.Registry
	type probe struct {
		peer     softwareupdate.Peer
		returned bool
	}
	failed := make(chan probe, 4)
	r := newOTARig(t, func(o *controller.OTAConfig) {
		o.ScriptQueryImage = func(softwareupdate.Peer, softwareupdate.QueryImageRequest) (softwareupdate.QueryImageScript, bool) {
			hold.Do(func() {
				close(entered)
				<-release
			})
			return softwareupdate.QueryImageScript{}, false
		}
		o.OnUpdateFailed = func(p softwareupdate.Peer) {
			done := make(chan struct{})
			go func() {
				_, _ = reg.ForDevice(context.Background(), client.Device{InstanceID: "probe-2B4E"}, "")
				close(done)
			}()
			select {
			case <-done:
				failed <- probe{p, true}
			case <-time.After(10 * time.Second): // a held lock never releases: this bound only names the failure
				failed <- probe{p, false}
			}
		}
	})
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	reg = r.c.Registry()
	peer := r.peer(t)
	r.storeImage(t, 2)
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
	if len(r.c.SoftwareUpdates().QueuedUpdates()) == 0 {
		t.Fatal("no queued update before the erase: the test shows nothing")
	}
	r.run(t, func(ctx context.Context) error { reg.Erase(ctx); return nil })
	select {
	case got := <-failed:
		if got.peer != peer {
			t.Fatalf("OnUpdateFailed for %v, want %v", got.peer, peer)
		}
		if !got.returned {
			t.Fatal("OnUpdateFailed ran inside the registry's fabric operation: a registry call from it did not return")
		}
	case <-time.After(30 * time.Second):
		t.Fatal("the deleted node's queued update was not reported failed")
	}
	for _, u := range r.c.SoftwareUpdates().QueuedUpdates() {
		if u.Peer == peer {
			t.Fatalf("the deleted node's update is still queued: %+v", u)
		}
	}
}

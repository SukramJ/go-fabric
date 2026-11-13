// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controller_test

import (
	"context"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/controller"
	"github.com/SukramJ/go-fabric/controller/softwareupdate"
)

// TestCloseStopsTheOTAProviderAfterServedRequests: Close first stops
// dispatching requests for the endpoints the controller serves and waits
// for the one in flight, and only then stops the OTA provider, so no
// QueryImage reaches a stopped provider. The device's QueryImage is held
// in the provider while Close runs: the served gate is closed and the
// provider still runs until the QueryImage returned. Before, Close
// stopped the provider first and closed the gate only after the
// registry, so a QueryImage arriving in between was dispatched to the
// stopped provider (review II, reported with R2-P1-net-04).
func TestCloseStopsTheOTAProviderAfterServedRequests(t *testing.T) {
	t.Parallel()
	entered, release := make(chan struct{}), make(chan struct{})
	var hold sync.Once
	r := newOTARig(t, func(o *controller.OTAConfig) {
		o.ScriptQueryImage = func(softwareupdate.Peer, softwareupdate.QueryImageRequest) (softwareupdate.QueryImageScript, bool) {
			hold.Do(func() {
				close(entered)
				<-release
			})
			return softwareupdate.QueryImageScript{}, false
		}
	})
	r.storeImage(t, 2)
	r.run(t, func(ctx context.Context) error {
		_, err := r.c.SoftwareUpdates().AddUpdateConsent(ctx, r.peer(t), softwareupdate.Target{VendorID: 0xFFF1, ProductID: 0x8001, TargetSoftwareVersion: 2})
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
	stopped := controller.OTAProviderDone(r.c)
	blocked := controller.ServedRequestsBlocked(r.c)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	closed := make(chan error, 1)
	go func() { closed <- r.site.Run(ctx, r.c.Close) }()
	for !blocked() {
		select {
		case <-ctx.Done():
			close(release)
			t.Fatal("Close did not block the served requests")
		case err := <-closed:
			close(release)
			t.Fatalf("Close returned (%v) with a QueryImage in flight", err)
		default:
			runtime.Gosched()
		}
	}
	select {
	case <-stopped:
		close(release)
		t.Fatal("the OTA provider was stopped before the served requests were blocked and waited for")
	default:
	}
	close(release)
	if err := <-closed; err != nil {
		t.Fatalf("Close: %v", err)
	}
	select {
	case <-stopped:
	default:
		t.Fatal("Close did not stop the OTA provider")
	}
}

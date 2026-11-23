// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package camera

import (
	"context"
	"errors"
	"testing"

	csdef "thingfabric.dev/go-fabric/cluster/spec/cameraavstreammanagement"
)

// TestNewRefusesASecondServiceOverOneController (review II,
// R2-C2-api-2201): a second Service over a controller whose Service is not
// closed is refused with ErrServiceInUse, which also matches
// ErrInvalidArgument, even with its own Reports — its Close would
// otherwise end the first Service's sessions. The refused New leaves its
// Reports free, and once the first Service closed the controller serves a
// new one.
func TestNewRefusesASecondServiceOverOneController(t *testing.T) {
	t.Parallel()
	d := newDevSite(t)
	d.startService()
	var tracked *Session
	d.run(func(ctx context.Context) (err error) {
		tracked, err = d.svc.StartStream(ctx, StartRequest{Node: d.n, Endpoint: d.ep, Usage: csdef.StreamUsageLiveView, DeclineVideo: true, Owner: "host-a", Key: "a"})
		return err
	})
	cfg := d.cfg
	own := &Reports{}
	own.Install(&cfg)
	if svc, err := New(context.Background(), d.c, Config{Reports: own}); !errors.Is(err, ErrServiceInUse) || !errors.Is(err, ErrInvalidArgument) {
		if svc != nil {
			d.run(svc.Close)
		}
		t.Fatalf("New over a controller with a live Service = %v, want ErrServiceInUse and ErrInvalidArgument", err)
	}
	if n := len(d.c.CameraRequestor().Sessions()); n != 1 {
		t.Fatalf("requestor holds %d sessions after the refused New, want the first Service's one", n)
	}
	d.svc.mu.Lock()
	held := d.svc.sessions[sessionKey{tracked.t.nodeID, tracked.t.endpointID, tracked.ID}] != nil
	d.svc.mu.Unlock()
	if !held {
		t.Fatal("the first Service lost its session to the refused New")
	}
	if !own.claim() {
		t.Fatal("the refused New kept its Reports claimed")
	}
	own.unclaim()
	d.run(d.svc.Close)
	svc, err := New(context.Background(), d.c, Config{Reports: own})
	if err != nil {
		t.Fatalf("New once the first Service closed: %v", err)
	}
	d.run(svc.Close)
}

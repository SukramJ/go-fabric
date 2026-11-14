// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package device

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock/clocktest"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/internal/deviceseam"
)

// slowReachableHost holds the notification of an unreachable device until
// a notification of a reachable one arrived or ctx ends, so a second
// SetReachable can run while the first is still telling the host.
type slowReachableHost struct {
	*fakeHost
	ctx       context.Context //nolint:containedctx // the test's own deadline for the held notification
	entered   chan struct{}
	reachable chan struct{}
}

func (h *slowReachableHost) NotifyReachable(ctx context.Context, key endpoint.SourceKey, reachable bool) {
	if !reachable {
		close(h.entered)
		select {
		case <-h.reachable:
		case <-h.ctx.Done():
		}
	} else {
		defer close(h.reachable)
	}
	h.fakeHost.NotifyReachable(ctx, key, reachable)
}

// TestSetReachableNotifiesInSwapOrder: two concurrent SetReachable calls
// tell the host in the order they changed the state, so the last value the
// host hears — the endpoint's Reachable attribute — is the device's
// (review II, R2-P1-misc-03).
func TestSetReachableNotifiesInSwapOrder(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	held, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	defer cancel()
	host := &slowReachableHost{
		fakeHost: &fakeHost{clk: clocktest.New(time.Unix(1_000_000, 0)), numbers: map[string]uint16{"test:lamp": 7}},
		ctx:      held, entered: make(chan struct{}), reachable: make(chan struct{}),
	}
	lamp, err := NewOnOffLight(testOptions("lamp"))
	if err != nil {
		t.Fatal(err)
	}
	detach, _, err := deviceseam.Attach(lamp, host)
	if err != nil {
		t.Fatal(err)
	}
	defer detach()
	aDone := make(chan struct{})
	go func() { lamp.SetReachable(ctx, false); close(aDone) }()
	<-host.entered
	bDone := make(chan struct{})
	go func() { lamp.SetReachable(ctx, true); close(bDone) }()
	<-aDone
	<-bDone
	host.mu.Lock()
	told := host.fakeHost.reachable
	host.mu.Unlock()
	if len(told) == 0 || told[len(told)-1] != lamp.Reachable() {
		t.Errorf("the host was told %v, the device is reachable %v: the last notification is not the state", told, lamp.Reachable())
	}
}

// reenteringHost calls back into the device from its first unreachable
// notification, as a node observer that reacts to the ReachableChanged
// event the notification emits (bridge EmitEvent → node OnEvent, on the
// notifying goroutine) and re-asserts the device's state.
type reenteringHost struct {
	*fakeHost
	once func()
}

func (h *reenteringHost) NotifyReachable(ctx context.Context, key endpoint.SourceKey, reachable bool) {
	h.fakeHost.NotifyReachable(ctx, key, reachable)
	if f := h.once; f != nil && !reachable {
		h.once = nil
		f()
	}
}

// TestSetReachableFromTheNotificationReturns: a host that sets the
// device's reachability again from within its notification returns, and
// the host hears both changes in the order they were made, the last one
// the device's state (review II, R2-P1-dr-rest-06). With the lock held
// across the notification, the inner call deadlocked.
func TestSetReachableFromTheNotificationReturns(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	host := &reenteringHost{fakeHost: &fakeHost{clk: clocktest.New(time.Unix(1_000_000, 0)), numbers: map[string]uint16{"test:lamp": 7}}}
	lamp, err := NewOnOffLight(testOptions("lamp"))
	if err != nil {
		t.Fatal(err)
	}
	host.once = func() { lamp.SetReachable(ctx, true) }
	detach, _, err := deviceseam.Attach(lamp, host)
	if err != nil {
		t.Fatal(err)
	}
	defer detach()
	done := make(chan struct{})
	go func() { lamp.SetReachable(ctx, false); close(done) }()
	// Ten seconds: no loaded machine takes that long for a call that
	// returns, so only a deadlock trips it (review II, R2-P1-rw-08).
	wait, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	select {
	case <-done:
	case <-wait.Done():
		t.Fatal("SetReachable never returned: the host's SetReachable from its notification deadlocked")
	}
	host.mu.Lock()
	told := slices.Clone(host.reachable)
	host.mu.Unlock()
	if !slices.Equal(told, []bool{false, true}) || !lamp.Reachable() {
		t.Errorf("the host was told %v, the device is reachable %v; want [false true], true", told, lamp.Reachable())
	}
}

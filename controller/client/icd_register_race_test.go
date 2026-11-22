// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package client_test

import (
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"thingfabric.dev/go-fabric/controller/client"
	"thingfabric.dev/go-fabric/fabrictest"
)

// TestICDRegisterJoinsARegistrationStartedDuringItsChecks (review II,
// R2-P6-fsm-2355): a Register whose node checks run while another call
// with the same options starts its registration joins that registration,
// as the doc says: both return nil and the peer gets one RegisterClient.
// Before, the second call started its operation without admitting again
// and was refused ("cannot start registering while registering is in
// progress").
func TestICDRegisterJoinsARegistrationStartedDuringItsChecks(t *testing.T) {
	t.Parallel()
	p := newICDPair(t, icdPairOptions{})
	dev := p.pair.Device.Addr()
	// The other registration's RegisterClient is held on the way to the
	// device, so it is still in flight when the first call admits again.
	var hold atomic.Bool
	p.site.Net().Filter(func(d fabrictest.Datagram) bool { return !hold.Load() || !sameUDP(d.To, dev) })
	var first, checked atomic.Bool
	var other atomic.Pointer[started]
	client.OnRegisterChecked(p.icd(), func() {
		if !first.CompareAndSwap(false, true) {
			return
		}
		hold.Store(true)
		o := start(func() error { return p.icd().Register(parityCtx(t), uncovered) })
		other.Store(o)
		for deadline := time.Now().Add(time.Minute); !client.ICDRegistering(p.icd()) && !o.done.Load(); runtime.Gosched() {
			if time.Now().After(deadline) {
				t.Error("the other registration did not start")
				break
			}
		}
		checked.Store(true)
	})
	this := start(func() error { return p.icd().Register(parityCtx(t), uncovered) })
	p.eventually(t, "the first call's checks", checked.Load)
	hold.Store(false)
	p.eventually(t, "both Register calls", func() bool { return this.done.Load() && other.Load().done.Load() })
	if err := this.error(); err != nil {
		t.Fatalf("the first Register: %v", err)
	}
	if err := other.Load().error(); err != nil {
		t.Fatalf("the other Register: %v", err)
	}
	if n := len(p.deviceServerRegistrations(t)); n != 1 {
		t.Fatalf("%d registrations on the device, want 1", n)
	}
}

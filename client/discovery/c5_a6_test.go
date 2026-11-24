// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package discovery

import (
	"context"
	"errors"
	"testing"

	"github.com/miekg/dns"
)

// TestOperational_ResolveKeepsAHostWhoseSRVTargetDiffersInCase: the cache
// keys names case-insensitively and keeps the first-seen spelling, so a
// host another service named first as "host.local" is the same *Name this
// service's SRV calls "HOST.local". A change to the service must not take
// the spelling difference for a target that left and stop the host's
// discovery (review II, R4-fsm-4551).
func TestOperational_ResolveKeepsAHostWhoseSRVTargetDiffersInCase(t *testing.T) {
	t.Parallel()
	n, conn, _ := testNames(t)
	// Another service's resolution named the host first, in lower case.
	onLoop(t, n, func() { n.get("host.local") })
	deliver(t, n, 0, []dns.RR{srvRR(opQ, "HOST.local", 5540, 120, false)})
	s := n.Service(opCFID, opNode)
	defer s.Close()
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { _, err := s.Resolve(ctx); done <- err }()
	waitFor(t, func() bool { return asked(t, conn, opQ, TypeSRV) })
	discovering := func() (ok bool) {
		onLoop(t, n, func() { _, ok = n.solicitor.discovering[n.get("host.local")] })
		return ok
	}
	if !discovering() {
		t.Fatal("the SRV target without addresses is not being resolved")
	}
	// Any change of the service re-reads its targets.
	deliver(t, n, 0, []dns.RR{txtRR(opQ, 4500, false, "SII=5000")})
	onLoop(t, n, func() {})
	if !discovering() {
		t.Fatal("the host discovery stopped after a change although the SRV still targets the host (spelling differs only in case)")
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
}

// TestStatus_ResolutionRestartsWhenNeededAgainBeforeItsCleanup: a
// resolution stopped because the service became reachable with no attempt
// outstanding clears its handle only in a cleanup posted to the loop. An
// attempt on a service unreachable again before that cleanup runs found
// the handle still set and started nothing; the cleanup now restarts the
// resolution when the status still asks for one (review II, R4-conc-4552).
func TestStatus_ResolutionRestartsWhenNeededAgainBeforeItsCleanup(t *testing.T) {
	t.Parallel()
	n, _, _ := testNames(t)
	svc := n.Service(opCFID, opNode)
	defer svc.Close()
	st := svc.Status()
	rejected := make(chan bool)
	st.Connecting(rejected)
	close(rejected)
	eventually(t, "the rejected attempt", func() bool { return !st.IsConnecting() })
	if !st.IsResolving() {
		t.Fatal("an unreachable service is not resolving")
	}
	var stopped chan struct{}
	onLoop(t, n, func() {
		stopped = svc.status.resolving
		svc.setReachable(true) // no attempt outstanding: stops the resolution
		svc.status.connecting[new(int)] = struct{}{}
		svc.setReachable(false) // the cleanup has not run yet
	})
	eventually(t, "the stopped resolution's cleanup", func() bool {
		var cur chan struct{}
		onLoop(t, n, func() { cur = svc.status.resolving })
		return cur != stopped
	})
	if !st.IsResolving() {
		t.Fatal("an unreachable service with an attempt outstanding is not resolving after the stopped resolution's cleanup")
	}
}

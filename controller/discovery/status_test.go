// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package discovery

import (
	"context"
	"testing"
	"time"

	"github.com/miekg/dns"
)

// eventually polls cond until it holds or five seconds pass.
func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

// TestStatus_ConnectingResolvesUntilAnAddressAppears: an attempt on an
// unreachable service starts the resolution (IpServiceStatus.ts:114-186);
// the new address it finds marks the service reachable (IpService.ts:324-325)
// and ends the resolution; the attempt's success settles it (:117-133).
func TestStatus_ConnectingResolvesUntilAnAddressAppears(t *testing.T) {
	t.Parallel()
	n, conn, clk := testNames(t)
	svc := n.Service(opCFID, opNode)
	st := svc.Status()
	st.SetReachable(false) // already false: nothing happens
	if st.IsResolving() || st.IsConnecting() || !st.ConnectionInitiatedAt().IsZero() {
		t.Fatal("idle status is busy")
	}
	result := make(chan bool, 1)
	st.Connecting(result)
	if !st.IsConnecting() || !st.IsResolving() || !st.ConnectionInitiatedAt().Equal(clk.Now()) {
		t.Fatalf("connecting %v resolving %v at %v", st.IsConnecting(), st.IsResolving(), st.ConnectionInitiatedAt())
	}
	eventually(t, "the SRV query", func() bool { return asked(t, conn, opQ, TypeSRV) })
	deliver(t, n, 0, []dns.RR{srvRR(opQ, "ctrl.local", 5540, 120, true), aaaaRR("ctrl.local", "fd00::7", 120, true)})
	eventually(t, "the resolution to end", func() bool { return !st.IsResolving() })
	if !st.Reachable() || !st.IsConnecting() {
		t.Fatalf("reachable %v connecting %v", st.Reachable(), st.IsConnecting())
	}
	result <- true
	eventually(t, "the attempt to settle", func() bool { return !st.IsConnecting() })
	if !st.Reachable() || !st.ConnectionInitiatedAt().IsZero() {
		t.Fatal("a successful attempt left the service unreachable or connecting")
	}
	at := clk.Now().Add(time.Second)
	st.SetLastReceiptAt(at)
	if !st.LastReceiptAt().Equal(at) {
		t.Fatal("LastReceiptAt")
	}
	svc.Close()
}

// TestStatus_FailedAttemptAndReachabilityStopResolving: a failed or
// rejected attempt marks the service unreachable (:129-143); clearing
// reachability while connecting resolves, setting it with no attempt left
// stops the resolution (:78-90, :188-194); Close waits for it (:36-41).
func TestStatus_FailedAttemptAndReachabilityStopResolving(t *testing.T) {
	t.Parallel()
	n, _, _ := testNames(t)
	svc := n.Service(opCFID, opNode)
	st := svc.Status()
	st.SetReachable(true)
	failed := make(chan bool, 1)
	st.Connecting(failed)
	if st.IsResolving() {
		t.Fatal("resolving a reachable service")
	}
	failed <- false
	eventually(t, "the failed attempt", func() bool { return !st.Reachable() && !st.IsConnecting() })
	rejected := make(chan bool)
	st.Connecting(rejected)
	if !st.IsResolving() {
		t.Fatal("an attempt on an unreachable service does not resolve")
	}
	close(rejected)
	eventually(t, "the rejected attempt", func() bool { return !st.IsConnecting() })
	if !st.IsResolving() || st.Reachable() {
		t.Fatal("the resolution stopped although the service is unreachable")
	}
	st.SetReachable(true)
	eventually(t, "the resolution to stop", func() bool { return !st.IsResolving() })
	pending := make(chan bool)
	st.SetReachable(false)
	st.Connecting(pending)
	svc.Close()
	if st.IsResolving() {
		t.Fatal("Close left the resolution running")
	}
}

// TestService_Changes: one coalesced signal per batch of address or TXT
// changes; the channel closes with its context (IpService.ts:122-124,
// :342-355).
func TestService_Changes(t *testing.T) {
	t.Parallel()
	n, _, _ := testNames(t)
	svc := n.Service(opCFID, opNode)
	ctx, cancel := context.WithCancel(context.Background())
	changes := svc.Changes(ctx)
	deliver(t, n, 0, []dns.RR{srvRR(opQ, "ctrl.local", 5540, 120, true), aaaaRR("ctrl.local", "fd00::7", 120, true)})
	<-changes
	deliver(t, n, 0, []dns.RR{txtRR(opQ, 120, true, "SII=800")})
	<-changes
	cancel()
	for range changes {
	}
	closed := svc.Changes(ctx)
	_ = n.Close()
	for range closed {
	}
	if svc.Changes(context.Background()) == nil {
		t.Fatal("nil channel on a closed cache")
	}
}

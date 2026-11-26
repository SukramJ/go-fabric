// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package discovery

import (
	"testing"

	"github.com/miekg/dns"

	"thingfabric.dev/go-fabric/mdns"
)

// TestStatus_CompletedResolutionStartsNoneW2B5 (issue #623): a resolution
// that ended because an address appeared starts no new one from its
// cleanup, even when the service is unreachable again by then with an
// attempt outstanding; only a resolution stopped by stopResolving is
// restarted there (R4-conc-4552). The next one starts from a connection
// attempt or a reachable-to-unreachable change.
func TestStatus_CompletedResolutionStartsNoneW2B5(t *testing.T) {
	t.Parallel()
	n, conn, _ := testNames(t)
	svc := n.Service(opCFID, opNode)
	defer svc.Close()
	st := svc.Status()
	pending := make(chan bool)
	st.Connecting(pending)
	if !st.IsResolving() {
		t.Fatal("an attempt on an unreachable service does not resolve")
	}
	eventually(t, "the SRV query", func() bool { return asked(t, conn, opQ, TypeSRV) })
	deliver(t, n, 0, []dns.RR{srvRR(opQ, "ctrl.local", 5540, 120, true)})
	m := new(dns.Msg)
	m.Response = true
	m.Answer = []dns.RR{aaaaRR("ctrl.local", "fd00::7", 120, true)}
	b, err := m.Pack()
	if err != nil {
		t.Fatal(err)
	}
	var ended chan struct{}
	onLoop(t, n, func() {
		ended = svc.status.resolving
		// The new address ends the resolution and marks the service
		// reachable, in the microtask the packet queues; the next one
		// makes it unreachable again, before the resolution's cleanup.
		n.handleMessage(mdns.QueryResponse{Msg: b})
		n.loop.microtask(func() { svc.setReachable(false) })
	})
	eventually(t, "the completed resolution's cleanup", func() bool {
		var cur chan struct{}
		onLoop(t, n, func() { cur = svc.status.resolving })
		return cur != ended
	})
	if st.Reachable() {
		t.Fatal("the service is reachable; the test needs it unreachable at the cleanup")
	}
	if st.IsResolving() {
		t.Fatal("the cleanup of a resolution that found an address started a new one")
	}
	again := make(chan bool)
	st.Connecting(again)
	if !st.IsResolving() {
		t.Fatal("a new attempt on the unreachable service does not resolve")
	}
}

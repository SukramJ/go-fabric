// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package camera

import (
	"slices"
	"testing"

	wrdef "thingfabric.dev/go-fabric/cluster/spec/webrtctransportrequestor"
)

// TestReportDropsListedRawSession (review II, R2-P6-store-2203, decision
// 207, fable-6b-w2 G1 module rule): a raw-route session the camera stops
// listing in CurrentSessions without an End leaves the requestor once an
// earlier report listed it — the rule the registry applies to its own
// sessions; a report that never listed it (one built before the offer)
// leaves it, a session the registry names is the registry's, and another
// endpoint's entry stays; a new session under a reused id starts
// unlisted. Before, the entry stayed for good, the sweep
// counted it as held and the requestor accepted signals for the dead id.
func TestReportDropsListedRawSession(t *testing.T) {
	t.Parallel()
	f := newPSFixture()
	f.svc.reported = f.svc.reconcile
	f.pastFirstBatch()
	if _, err := f.establish(t, psEndpoint, psOfferRequest(nil)); err != nil {
		t.Fatalf("establish: %v", err)
	}
	held := func(id uint16) bool {
		return slices.ContainsFunc(f.sessions(), func(s wrdef.WebRTCSessionStruct) bool { return s.Id == id })
	}
	// A report without the fresh session: it was never listed, so it stays.
	f.setCamera(psEndpoint, []sessionPeer{{3, psServer}})
	f.batch()
	if !held(9) {
		t.Fatal("a report that never listed the raw session removed it")
	}
	// Listed, then gone without an End (the camera restarted).
	_ = f.upsert(trackedSession(func(s *wrdef.WebRTCSessionStruct) { s.Id, s.PeerEndpointId = 4, psOtherEndpoint }))
	f.setCamera(psEndpoint, []sessionPeer{{9, psServer}})
	f.batch()
	f.svc.w.startedUp(psCamera)
	f.setCamera(psEndpoint, []sessionPeer{})
	f.batch()
	if held(9) {
		t.Errorf("requestor after the camera dropped the listed raw session = %+v, want it gone", f.sessions())
	}
	if _, ok := f.svc.ps.offerOf(f.svc.ps.key(target{psCamera, psEndpoint}, 9)); ok {
		t.Error("the dropped session's offer is still recorded")
	}
	if !held(4) {
		t.Error("another endpoint's entry was removed")
	}

	// The camera hands id 9 to a new session; the old one was listed. A
	// report built before the new offer lists nothing: the new session
	// starts unlisted, as a new registry life does, and stays.
	f.setCamera(psEndpoint, []sessionPeer{{9, psServer}})
	if _, err := f.establish(t, psEndpoint, psOfferRequest(nil)); err != nil {
		t.Fatalf("establish: %v", err)
	}
	f.batch() // lists 9: the old session's mark
	// The camera loses 9 without an End and answers a new offer with 9 …
	if _, err := f.establish(t, psEndpoint, psOfferRequest(nil)); err != nil {
		t.Fatalf("establish again: %v", err)
	}
	f.setCamera(psEndpoint, []sessionPeer{})
	f.batch() // … a report built before the new offer
	if !held(9) {
		t.Error("a report built before a new session under a reused id removed it")
	}
}

// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package camera

import "testing"

// TestRemovedNodeLeavesNoLeases (review II, R2-P1-cam-a-02): a node that
// leaves the fabric takes this controller's leases of its streams along,
// where its sessions and provider records already go. Another camera
// commissioned under the same node id that reports a stream of the same
// id — allocated by another controller — is not taken for this
// controller's own. Before, the lease survived the removal, and the
// report kept it as this controller's allocation.
func TestRemovedNodeLeavesNoLeases(t *testing.T) {
	t.Parallel()
	x := newReuseFixture(t, 42)
	x.start(t, "a")
	if !x.s.m.ownsStream(tTarget, StreamVideo, 9) {
		t.Fatal("the start's allocation of video stream 9 is not leased")
	}
	x.s.nodeRemoved(tNode)
	if n := x.s.m.leasedEndpoints(); n != 0 {
		t.Errorf("%d endpoints still leased after the node was removed", n)
	}
	// A new camera under the same node id reports a stream 9 it holds for
	// another controller.
	io := x.s.m.io.(routedIO).fakeIO
	io.mu.Lock()
	io.state = fixtureState().with(func(s *cameraState) { s.video = []allocatedVideoStream{liveStream(9, 1)} })
	io.mu.Unlock()
	c, err := x.s.m.capabilities(t.Context(), tTarget)
	if err != nil {
		t.Fatalf("capabilities: %v", err)
	}
	if len(c.Allocated.Video) != 1 || c.Allocated.Video[0].VideoStreamId != 9 {
		t.Fatalf("allocated video = %+v, want the new camera's stream 9", c.Allocated.Video)
	}
	if c.Allocated.Video[0].AllocatedByUs {
		t.Error("the new camera's stream 9 is reported as this controller's allocation")
	}
	if x.s.m.ownsStream(tTarget, StreamVideo, 9) {
		t.Error("ownsStream(9) is true for another controller's stream")
	}
}

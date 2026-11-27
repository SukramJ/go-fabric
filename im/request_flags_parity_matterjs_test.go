// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package im

import "testing"

// TestRequestFlagsForSendingParityMatterJS ports the
// "InteractionClientMessenger.requestFlagsForSending" group of matter.js
// packages/protocol/test/interaction/InteractionClientMessengerTest.ts:886-944
// at db91886c (#5069): the sender's rule for a false SuppressResponse and
// TimedRequest. matter.js returns the request without the omitted flags; the
// Go rule returns the encoding the encoders apply (RequestEncoding), and a
// true flag stays on the wire whatever it says
// (TestRequestEncodingOmitFalseFlags), which is "keeps a flag that is true".
func TestRequestFlagsForSendingParityMatterJS(t *testing.T) {
	t.Parallel()
	const rev = FalseRequestFlagsOmittedFromRevision
	for _, tc := range []struct {
		name        string
		omit, group bool
		peer, own   uint16
		wantOmitted bool
	}{
		{"sends both flags below the revision that leaves out false flags (group)", false, true, 12, rev - 1, false},
		{"sends both flags below the revision that leaves out false flags (unicast)", false, false, rev, rev - 1, false},
		{"sends both flags in a group request", false, true, rev, rev, false},
		{"leaves out false flags of a request to a peer at that revision", false, false, rev, rev, true},
		{"sends both flags to a peer below that revision", false, false, rev - 1, rev, false},
		{"leaves out false flags of a group request when forced", true, true, 12, rev, true},
		{"leaves out false flags when forced, whatever the revisions", true, false, rev - 1, rev - 1, true},
		{"keeps a flag that is true", false, false, rev, rev, true},
		{"sends both flags at the current revision of this node", false, false, rev, uint16(MatterInteractionModelRevision), false},
	} {
		if got := RequestFlagsForSending(tc.omit, tc.group, tc.peer, tc.own); got.OmitFalseFlags != tc.wantOmitted {
			t.Errorf("%s: OmitFalseFlags %v, want %v", tc.name, got.OmitFalseFlags, tc.wantOmitted)
		}
	}
	// The last row is matter.js's characterization (:940-943): it fails
	// once this module's revision reaches 14, as a reminder that peers then
	// get fewer flags.
	if MatterInteractionModelRevision >= FalseRequestFlagsOmittedFromRevision {
		t.Error("this module's revision leaves out false flags now: peers get fewer flags")
	}
}

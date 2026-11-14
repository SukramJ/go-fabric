// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package im

// A fabric-sensitive event record is disclosed only to the fabric that owns
// it, whatever the session's auth mode: the filter compares the record's
// FabricIndex with the accessing fabric (0 on a session without one), and a
// PASE session's implicit Administer grant lifts only the ACL gate. matter.js
// packages/protocol/src/action/server/EventReadResponse.ts:79 and :341-350
// at db021da0 (#readAllowedEvents); the cases port
// packages/node/test/node/EventReadResponseTest.ts:142-168 ("filters
// fabric-sensitive concrete event not owned by the accessing fabric") and
// :172-218 ("reads fabric-sensitive concrete event owned by the accessing
// fabric"). matter.js runs both for FabricFiltered true and false;
// AuthorizeEventReports takes no FabricFiltered input, so the drop is
// independent of it by construction. Review II, R2-P4-imread-01.

import (
	"context"
	"testing"
)

// fakeMessageCompleteEvent stands in for Messages.MessageCompleteEvent
// (access "S V"): the IM layer reads its FabricIndex field by name.
type fakeMessageCompleteEvent struct {
	MessageID                []byte
	FutureMessagesPreference uint8
	FabricIndex              uint8
}

const (
	tstMessagesCluster        uint32 = 0x0097
	tstMessageCompleteEventID uint32 = 0x0002
)

func messageCompleteReport(recordFabric uint8) EventReport {
	return EventReport{
		Path: ConcreteEventPath{
			Endpoint: 0, HasEndpoint: true,
			Cluster: tstMessagesCluster, HasCluster: true,
			Event: tstMessageCompleteEventID, HasEvent: true,
		},
		Number:   3,
		Priority: EventPriorityInfo,
		Data: AttributeValue{Value: fakeMessageCompleteEvent{
			MessageID:   []byte{0x00, 0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88, 0x99, 0x00, 0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff},
			FabricIndex: recordFabric,
		}},
	}
}

func TestEventReadParity_FabricSensitiveFilteredToAccessingFabric(t *testing.T) {
	t.Parallel()
	// The ACL grants nothing on fabric 5 itself: what a PASE session reads
	// rests on its implicit grant, what a CASE session reads on the ACL.
	checker := fakeEventACL{allowFabric: 5, maxPriv: 5}
	cases := []struct {
		name string
		ctx  context.Context
		auth EventReadAuthorizer
		want int
	}{
		// EventReadResponseTest.ts:142-168: the session is on NO_FABRIC with
		// Administer — here the PASE session before AddNOC.
		{"PASE without a fabric: the record of fabric 5 is not disclosed", context.Background(), EventReadAuthorizer{Checker: checker, PASE: true}, 0},
		{"PASE marked in ctx only, without a fabric", WithAuthModePASE(context.Background()), EventReadAuthorizer{Checker: checker}, 0},
		{"PASE adopted onto fabric 2: the record of fabric 5 is not disclosed", context.Background(), EventReadAuthorizer{Checker: checker, FabricIndex: 2, PASE: true}, 0},
		// EventReadResponseTest.ts:172-218: the accessing fabric owns the record.
		{"PASE adopted onto fabric 5: its own record", context.Background(), EventReadAuthorizer{Checker: checker, FabricIndex: 5, PASE: true}, 1},
		{"CASE on fabric 5: its own record", context.Background(), EventReadAuthorizer{Checker: checker, FabricIndex: 5}, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := AuthorizeEventReports(tc.ctx, tc.auth, []EventReport{messageCompleteReport(5)})
			if len(got) != tc.want {
				t.Fatalf("got %d events, want %d", len(got), tc.want)
			}
		})
	}
}

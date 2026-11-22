// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"errors"
	"fmt"
	"net"
	"slices"
	"testing"

	"thingfabric.dev/go-fabric/groups"
	"thingfabric.dev/go-fabric/im"
)

// TestGroupMessageOutcomes pins the reports matter.js derives from a group
// message (SessionManager.groupSessionFromPacket, InteractionServer
// .handleInvokeRequest on a group session) for the branches the end-to-end
// test does not reach.
func TestGroupMessageOutcomes(t *testing.T) {
	t.Parallel()
	src := net.ParseIP("fe80::2")
	path := im.ConcreteCommandPath{Cluster: 6, Command: 2, HasCluster: true, HasCommand: true}

	// Nothing reported for a request that ended before dispatch.
	if evs := invokeOutcomeEvents(&groups.Message{}, src, im.GroupInvokeReport{}); evs != nil {
		t.Errorf("unprocessed: %+v", evs)
	}
	// No endpoint ran it: with members AccessAllowed false,
	// without members no access evaluation and the field absent.
	evs := invokeOutcomeEvents(&groups.Message{FabricIndex: 1, GroupID: 5, HasValidMapping: true, Endpoints: []uint16{3}}, src,
		im.GroupInvokeReport{Processed: true, Requested: []im.ConcreteCommandPath{path}})
	if len(evs) != 1 || evs[0].Result != groups.TestResultSuccess || evs[0].AccessAllowed == nil || *evs[0].AccessAllowed || evs[0].HasEndpoint {
		t.Errorf("denied everywhere: %+v", evs)
	}
	evs = invokeOutcomeEvents(&groups.Message{FabricIndex: 1, GroupID: 5, HasValidMapping: true}, src,
		im.GroupInvokeReport{Processed: true, Requested: []im.ConcreteCommandPath{path}})
	if len(evs) != 1 || evs[0].AccessAllowed != nil {
		t.Errorf("no members: %+v", evs)
	}

	// Decode failures: a rejection names no fabric and carries the
	// candidate fabrics and the group with its owner (matter.js
	// SessionManager.ts:811-820 at db021da0).
	ev, ok := decodeFailureEvent(src, &groups.DecodeError{
		CandidateFabrics: []uint8{2}, GroupID: 0x0303, HasGroupID: true, GroupFabricIndex: 2,
	})
	if !ok || ev.FabricIndex != 0 || ev.HasGroupID || !slices.Equal(ev.CandidateFabrics, []uint8{2}) ||
		!ev.HasHeaderGroupID || ev.HeaderGroupID != 0x0303 || ev.HeaderGroupFabricIndex != 2 || !ev.SourceIP.Equal(src) ||
		ev.ResultFor(2) != groups.TestResultFailedAuth || ev.ResultFor(1) != groups.TestResultNoAvailableKey {
		t.Errorf("rejection: %+v", ev)
	}
	if ev, ok = decodeFailureEvent(src, fmt.Errorf("wrapped: %w", &groups.DecodeError{})); !ok || ev.HasHeaderGroupID || ev.ResultFor(1) != groups.TestResultNoAvailableKey {
		t.Errorf("rejection without candidates or group: %+v", ev)
	}
	for _, err := range []error{groups.ErrMalformed, groups.ErrNoKey, groups.ErrDecrypt, errGroupMessagingMissing, errors.New("other")} {
		if _, ok := decodeFailureEvent(src, err); ok {
			t.Errorf("%v was reported", err)
		}
	}
	noopGroupMessaging{}.ReportGroupMessage(groups.GroupMessageEvent{})
}

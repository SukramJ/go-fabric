// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"errors"
	"net"
	"testing"

	"github.com/SukramJ/go-fabric/groups"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/transport/message"
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
	// A key that is not the group's mapped one: FailedAuth, no group id.
	evs := invokeOutcomeEvents(&groups.Message{FabricIndex: 1, GroupID: 5}, src, im.GroupInvokeReport{Processed: true, Requested: []im.ConcreteCommandPath{path}})
	if len(evs) != 1 || evs[0].Result != groups.TestResultFailedAuth || evs[0].HasGroupID || evs[0].FabricIndex != 1 {
		t.Errorf("invalid mapping: %+v", evs)
	}
	// Valid mapping, no endpoint ran it: with members AccessAllowed false,
	// without members no access evaluation and the field absent.
	evs = invokeOutcomeEvents(&groups.Message{FabricIndex: 1, GroupID: 5, HasValidMapping: true, Endpoints: []uint16{3}}, src,
		im.GroupInvokeReport{Processed: true, Requested: []im.ConcreteCommandPath{path}})
	if len(evs) != 1 || evs[0].Result != groups.TestResultSuccess || evs[0].AccessAllowed == nil || *evs[0].AccessAllowed || evs[0].HasEndpoint {
		t.Errorf("denied everywhere: %+v", evs)
	}
	evs = invokeOutcomeEvents(&groups.Message{FabricIndex: 1, GroupID: 5, HasValidMapping: true}, src,
		im.GroupInvokeReport{Processed: true, Requested: []im.ConcreteCommandPath{path}})
	if len(evs) != 1 || evs[0].AccessAllowed != nil {
		t.Errorf("no members: %+v", evs)
	}

	// Decode failures.
	hdr := message.Header{
		SessionID: 9, MessageCounter: 1, HasSourceNodeID: true, SourceNodeID: 1,
		DestSize: message.DestGroup, DestGroupID: 0x0303, SessionType: message.SessionGroup,
	}
	plain := append(hdr.Marshal(), make([]byte, 20)...)
	ev, ok := decodeFailureEvent(plain, src, groups.ErrDecrypt)
	if !ok || ev.Result != groups.TestResultFailedAuth || !ev.HasHeaderGroupID || ev.HeaderGroupID != 0x0303 {
		t.Errorf("FailedAuth without privacy: %+v", ev)
	}
	ev, _ = decodeFailureEvent(plain, src, &groups.NoKeyError{Authenticated: true, FabricIndex: 2, GroupID: 0x0404})
	if ev.Result != groups.TestResultNoAvailableKey || ev.FabricIndex != 2 || ev.HeaderGroupID != 0x0303 {
		t.Errorf("NoKey with a readable header: %+v", ev)
	}
	hdr.Privacy = true
	private := append(hdr.Marshal(), make([]byte, 20)...)
	ev, _ = decodeFailureEvent(private, src, &groups.NoKeyError{Authenticated: true, FabricIndex: 2, GroupID: 0x0404})
	if !ev.HasHeaderGroupID || ev.HeaderGroupID != 0x0404 || ev.HasGroupID {
		t.Errorf("NoKey behind privacy: %+v, want the unmapped key's group as the header group", ev)
	}
	if ev, _ = decodeFailureEvent(private, src, groups.ErrNoKey); ev.Result != groups.TestResultNoAvailableKey || ev.HasHeaderGroupID {
		t.Errorf("bare ErrNoKey: %+v", ev)
	}
	if _, ok := decodeFailureEvent(plain, src, groups.ErrMalformed); ok {
		t.Error("a malformed message was reported")
	}
	if _, ok := decodeFailureEvent(plain, src, errors.New("other")); ok {
		t.Error("an unrelated failure was reported")
	}
	noopGroupMessaging{}.ReportGroupMessage(groups.GroupMessageEvent{})
}

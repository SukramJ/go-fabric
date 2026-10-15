// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core

import (
	"context"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	actionsdef "github.com/SukramJ/go-fabric/cluster/spec/actions"
	"github.com/SukramJ/go-fabric/cluster/spec/spectest"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
)

type actionsStub struct {
	actions []actionsdef.ActionStruct
	lists   []actionsdef.EndpointListStruct
	got     []ActionRequest
	err     error
}

func (s *actionsStub) ActionList() []actionsdef.ActionStruct          { return s.actions }
func (s *actionsStub) EndpointLists() []actionsdef.EndpointListStruct { return s.lists }

func (s *actionsStub) HandleAction(_ context.Context, req ActionRequest) error {
	s.got = append(s.got, req)
	return s.err
}

type eventLog struct{ events []uint32 }

func (l *eventLog) MatterEmitEvent(_ uint16, _, eventID uint32, _ any, _ contract.EventPriority) {
	l.events = append(l.events, eventID)
}

func newActionsStub() *actionsStub {
	return &actionsStub{
		actions: []actionsdef.ActionStruct{
			{ActionId: 0x1001, Name: "Scene", Type: actionsdef.ActionTypeScene, EndpointListId: 0xE001, SupportedCommands: actionsdef.CommandBitsInstantAction | actionsdef.CommandBitsInstantActionWithTransition, State: actionsdef.ActionStateInactive},
			{ActionId: 0x1002, Name: "Wake up", Type: actionsdef.ActionTypeSequence, EndpointListId: 0xE001, SupportedCommands: actionsdef.CommandBitsStartActionWithDuration | actionsdef.CommandBitsDisableActionWithDuration, State: actionsdef.ActionStateInactive},
		},
		lists: []actionsdef.EndpointListStruct{{EndpointListId: 0xE001, Name: "Room 1", Type: actionsdef.EndpointListTypeRoom, Endpoints: []uint16{3}}},
	}
}

func TestActionsListsAreTheHosts(t *testing.T) {
	t.Parallel()
	host := newActionsStub()
	for i := range 300 {
		host.lists = append(host.lists, actionsdef.EndpointListStruct{EndpointListId: uint16(i), Name: "x"})
	}
	a, err := NewActions(ActionsConfig{Host: host})
	if err != nil {
		t.Fatal(err)
	}
	v, _ := a.MatterRead(actionsdef.AttrActionList)
	if l, ok := v.(spec.List[actionsdef.ActionStruct]); !ok || len(l) != 2 {
		t.Errorf("ActionList = %#v", v)
	}
	v, _ = a.MatterRead(actionsdef.AttrEndpointLists)
	if l, ok := v.(spec.List[actionsdef.EndpointListStruct]); !ok || len(l) != actionsMaxListLength {
		t.Errorf("EndpointLists has %d entries, want the first 256", len(v.(spec.List[actionsdef.EndpointListStruct])))
	}
	if _, err := actionsdef.Definition.Attribute(actionsdef.AttrActionList).Decode(spectest.Decode(t, spectest.Encode(t, spec.List[actionsdef.ActionStruct](host.actions)))); err != nil {
		t.Errorf("ActionList does not decode: %v", err)
	}
	var changed []uint32
	a.OnMatterAttributesChanged(func(ids []uint32) { changed = append(changed, ids...) })
	before := a.MatterDataVersion()
	a.ActionListChanged()
	a.EndpointListsChanged()
	if a.MatterDataVersion() == before || len(changed) != 2 {
		t.Errorf("changes reported %v, data version %d -> %d", changed, before, a.MatterDataVersion())
	}
}

// TestActionsCommandStatuses walks DECODE_AND_VALIDATE (ActionsCluster.cpp
// :152-161): NOT_FOUND for an unknown action, INVALID_COMMAND for a
// command the action does not support or a payload that is not the
// command's, and the host's status otherwise.
func TestActionsCommandStatuses(t *testing.T) {
	t.Parallel()
	host := newActionsStub()
	a, err := NewActions(ActionsConfig{Host: host})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	invoke := 9 * uint32(1)
	cases := []struct {
		name   string
		cmd    uint32
		fields any
		want   im.StatusCode
	}{
		{"unknown action", actionsdef.CmdInstantAction, actionsdef.InstantActionRequest{ActionId: 0x7777}, im.StatusNotFound},
		{"unsupported command", actionsdef.CmdStopAction, actionsdef.StopActionRequest{ActionId: 0x1001}, im.StatusInvalidCommand},
		{"wrong payload", actionsdef.CmdInstantAction, actionsdef.StopActionRequest{ActionId: 0x1001}, im.StatusInvalidCommand},
		{"no payload", actionsdef.CmdInstantAction, nil, im.StatusInvalidCommand},
	}
	for _, tc := range cases {
		if _, err := a.MatterInvoke(ctx, tc.cmd, tc.fields); statusOf(err) != tc.want {
			t.Errorf("%s: %v, want status 0x%02X", tc.name, err, tc.want)
		}
	}
	if len(host.got) != 0 {
		t.Fatalf("the host saw a refused command: %+v", host.got)
	}

	if _, err := a.MatterInvoke(ctx, actionsdef.CmdInstantActionWithTransition, actionsdef.InstantActionWithTransitionRequest{ActionId: 0x1001, InvokeId: &invoke, TransitionTime: 30}); err != nil {
		t.Fatalf("supported command refused: %v", err)
	}
	if _, err := a.MatterInvoke(ctx, actionsdef.CmdStartActionWithDuration, actionsdef.StartActionWithDurationRequest{ActionId: 0x1002, Duration: 60}); err != nil {
		t.Fatalf("supported command refused: %v", err)
	}
	if len(host.got) != 2 || *host.got[0].TransitionTime != 30 || *host.got[0].InvokeID != 9 || *host.got[1].Duration != 60 {
		t.Errorf("host saw %+v", host.got)
	}

	host.err = spec.Errorf(im.StatusBusy, "busy")
	if _, err := a.MatterInvoke(ctx, actionsdef.CmdInstantAction, actionsdef.InstantActionRequest{ActionId: 0x1001}); statusOf(err) != im.StatusBusy {
		t.Errorf("host status lost: %v", err)
	}
}

// TestActionsRequestShapes maps every request payload to its command.
func TestActionsRequestShapes(t *testing.T) {
	t.Parallel()
	d := uint32(5)
	tt := uint16(2)
	payloads := map[uint32]any{
		actionsdef.CmdInstantAction:               actionsdef.InstantActionRequest{ActionId: 1},
		actionsdef.CmdInstantActionWithTransition: actionsdef.InstantActionWithTransitionRequest{ActionId: 1, TransitionTime: tt},
		actionsdef.CmdStartAction:                 actionsdef.StartActionRequest{ActionId: 1},
		actionsdef.CmdStartActionWithDuration:     actionsdef.StartActionWithDurationRequest{ActionId: 1, Duration: d},
		actionsdef.CmdStopAction:                  actionsdef.StopActionRequest{ActionId: 1},
		actionsdef.CmdPauseAction:                 actionsdef.PauseActionRequest{ActionId: 1},
		actionsdef.CmdPauseActionWithDuration:     actionsdef.PauseActionWithDurationRequest{ActionId: 1, Duration: d},
		actionsdef.CmdResumeAction:                actionsdef.ResumeActionRequest{ActionId: 1},
		actionsdef.CmdEnableAction:                actionsdef.EnableActionRequest{ActionId: 1},
		actionsdef.CmdEnableActionWithDuration:    actionsdef.EnableActionWithDurationRequest{ActionId: 1, Duration: d},
		actionsdef.CmdDisableAction:               actionsdef.DisableActionRequest{ActionId: 1},
		actionsdef.CmdDisableActionWithDuration:   actionsdef.DisableActionWithDurationRequest{ActionId: 1, Duration: d},
	}
	for cmd, p := range payloads {
		r, ok := actionRequest(cmd, p)
		if !ok || r.ActionID != 1 || r.Command != cmd {
			t.Errorf("command 0x%02X: %+v (%v)", cmd, r, ok)
		}
		if _, ok := actionRequest(cmd+1, p); ok {
			t.Errorf("command 0x%02X payload accepted for 0x%02X", cmd, cmd+1)
		}
	}
}

func TestActionsEvents(t *testing.T) {
	t.Parallel()
	a, err := NewActions(ActionsConfig{Host: newActionsStub()})
	if err != nil {
		t.Fatal(err)
	}
	log := &eventLog{}
	a.SetMatterEventEmitter(log)
	a.SetEndpoint(1)
	if err := a.EmitStateChanged(0x1001, 0, actionsdef.ActionStateActive); err != nil {
		t.Fatal(err)
	}
	if err := a.EmitActionFailed(0x1001, 0, actionsdef.ActionStateInactive, actionsdef.ActionErrorInterrupted); err != nil {
		t.Fatal(err)
	}
	if len(log.events) != 2 || log.events[0] != actionsdef.EventStateChanged || log.events[1] != actionsdef.EventActionFailed {
		t.Errorf("events = %v", log.events)
	}
}

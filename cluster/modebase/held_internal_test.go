// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package modebase

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec/rvcrunmode"
)

type heldChanger struct{}

func (heldChanger) ChangeToMode(context.Context, uint8) (Status, string, error) {
	return StatusSuccess, "", nil
}

// TestStoreReadsTheServersValues: the generated store beside the server
// (decision 154) reads SupportedModes and CurrentMode as the server
// serves them — the peers a request or response check resolves — and
// nothing else; a write changes nothing the server holds.
func TestStoreReadsTheServersValues(t *testing.T) {
	s, err := NewLaundryWasherMode(Config{Changer: heldChanger{}, SupportedModes: []ModeOption{
		{Label: "Normal", Mode: 0, Tags: []ModeTag{{Value: LaundryTagNormal}}},
		{Label: "Delicate", Mode: 1, Tags: []ModeTag{{Value: LaundryTagDelicate}}},
	}, CurrentMode: 1})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []uint32{AttrSupportedModes, AttrCurrentMode} {
		want, _ := s.MatterRead(id)
		got, ok := s.srv.MatterRead(id)
		if !ok || !reflect.DeepEqual(got, want) {
			t.Errorf("attribute 0x%04X: the store reads %v (%v), the server %v", id, got, ok, want)
		}
	}
	if _, ok := (modeSource{s}).MatterAttribute(0xFFF0); ok {
		t.Error("the store's source answers an attribute the server does not serve")
	}
	if changed, err := s.MatterWriteChanged(context.Background(), AttrCurrentMode, uint8(0)); changed || err == nil {
		t.Errorf("a write of CurrentMode: changed %v, err %v; want a refusal that changes nothing", changed, err)
	}
}

// TestDeprecatedHooksKeepTheirBehaviour: the Hooks surface kept for its
// deprecation window (decision 154) behaves as before — a handler
// registered with Handle runs through Invoke and through MatterInvoke, and
// CheckWrite runs the veto SetBeforeWrite installed.
func TestDeprecatedHooksKeepTheirBehaviour(t *testing.T) {
	s, err := NewRvcRunMode(Config{Changer: heldChanger{}, SupportedModes: []ModeOption{
		{Label: "Idle", Mode: 0, Tags: []ModeTag{{Value: RvcRunTagIdle}}},
		{Label: "Cleaning", Mode: 1, Tags: []ModeTag{{Value: RvcRunTagCleaning}}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	if err := s.Handle(CmdChangeToMode, func(context.Context, any) (any, error) { calls++; return nil, nil }); err != nil {
		t.Fatal(err)
	}
	req := rvcrunmode.ChangeToModeRequest{NewMode: 1}
	if _, handled, err := s.Invoke(context.Background(), CmdChangeToMode, req); !handled || err != nil {
		t.Errorf("Invoke: handled %v, err %v; want the host's handler", handled, err)
	}
	if _, err := s.MatterInvoke(context.Background(), CmdChangeToMode, req); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Errorf("the host's handler ran %d times, want 2", calls)
	}
	veto := errors.New("vetoed")
	s.SetBeforeWrite(func(context.Context, uint32, any) error { return veto })
	if err := s.CheckWrite(context.Background(), AttrCurrentMode, uint8(1)); !errors.Is(err, veto) {
		t.Errorf("CheckWrite = %v, want the veto", err)
	}
	s.Init(s.inst)
	// ModeBase defines no event: both emit paths refuse, endpoint and
	// emitter set or not.
	s.SetEndpoint(3)
	s.SetMatterEventEmitter(nil)
	if s.Emit(3, 0, nil) == nil || s.EmitEvent(0, nil) == nil {
		t.Error("an event ModeBase does not define was emitted")
	}
}

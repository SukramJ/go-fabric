// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package modebase

import (
	"context"
	"reflect"
	"testing"
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

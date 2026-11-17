// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package modeselect

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

// heldSource is a ModeSource with two modes.
type heldSource struct{ mode uint8 }

func (heldSource) ModeDescription() string                   { return "Mode" }
func (heldSource) ModeNamespace() (uint8, bool)              { return 0, false }
func (h heldSource) CurrentMode() uint8                      { return h.mode }
func (heldSource) ChangeToMode(context.Context, uint8) error { return nil }
func (heldSource) SupportedModes() []ModeOptionStruct {
	return []ModeOptionStruct{{Label: "A", Mode: 1}, {Label: "B", Mode: 2}}
}

// TestStoreReadsTheServersValues: the generated store beside the server
// (decision 154) reads the four served attributes as the server serves
// them — the peers a request or response check resolves — and nothing
// else.
func TestStoreReadsTheServersValues(t *testing.T) {
	s := NewServer(Config{Source: heldSource{mode: 2}})
	src := modeSource{s}
	for _, id := range []uint32{AttrDescription, AttrStandardNamespace, AttrSupportedModes, AttrCurrentMode} {
		want, _ := s.MatterRead(id)
		got, ok := src.MatterAttribute(id)
		if !ok || !reflect.DeepEqual(got, want) {
			t.Errorf("attribute 0x%04X: the store reads %v (%v), the server %v", id, got, ok, want)
		}
	}
	if _, ok := src.MatterAttribute(0xFFF0); ok {
		t.Error("the store's source answers an attribute the server does not serve")
	}
}

// TestMustPanicsOnARefusedSelection holds R2-P6-err-1468: a refused fixed
// selection is reported where it is made, not left as a nil instance.
func TestMustPanicsOnARefusedSelection(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("must returned on an error")
		}
	}()
	_ = must(0, errors.New("refused"))
}

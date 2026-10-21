// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package servicearea

import (
	"context"
	"errors"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	sa "github.com/SukramJ/go-fabric/cluster/spec/servicearea"
)

// definitionWith is the ServiceArea definition with edit applied to a
// copy of its attributes: the shipped definition has no writable
// attribute and a stored form that is always the declared one, so the
// two defensive branches behind those facts are reached on a definition
// that breaks them.
func definitionWith(tb testing.TB, edit func(attrs []spec.Attribute)) *Server {
	tb.Helper()
	def := *sa.Definition
	def.Attributes = append([]spec.Attribute(nil), sa.Definition.Attributes...)
	edit(def.Attributes)
	inst, err := spec.New(&def, spec.Options{})
	if err != nil {
		tb.Fatal(err)
	}
	s := &Server{Instance: inst}
	s.hooks.Init(inst)
	return s
}

func attrIn(attrs []spec.Attribute, id uint32) *spec.Attribute {
	for i := range attrs {
		if attrs[i].ID == id {
			return &attrs[i]
		}
	}
	return nil
}

// TestMatterWriteRunsTheVetoOnAWritableAttribute: a write that passed the
// definition's checks reaches the host's veto. The shipped definition has
// no writable attribute, so SelectedAreas is made one for the test.
func TestMatterWriteRunsTheVetoOnAWritableAttribute(t *testing.T) {
	t.Parallel()

	s := definitionWith(t, func(attrs []spec.Attribute) {
		a := attrIn(attrs, sa.AttrSelectedAreas)
		a.Access = spec.Access{RW: "RW", Read: spec.PrivilegeView, Write: spec.PrivilegeOperate}
	})
	veto := errors.New("vetoed")
	var seen uint32
	s.SetBeforeWrite(func(_ context.Context, attrID uint32, _ any) error {
		seen = attrID
		return veto
	})
	if err := s.MatterWrite(context.Background(), sa.AttrSelectedAreas, []uint32{7}); !errors.Is(err, veto) {
		t.Fatalf("MatterWrite = %v, want the veto", err)
	}
	if seen != sa.AttrSelectedAreas {
		t.Errorf("the veto saw attribute 0x%04X", seen)
	}
}

// TestSetAttributesRefusesAListOfAnotherType: a stored form that is not
// the list the attribute is held in is refused, naming the attribute.
func TestSetAttributesRefusesAListOfAnotherType(t *testing.T) {
	t.Parallel()

	s := definitionWith(t, func(attrs []spec.Attribute) {
		a := attrIn(attrs, sa.AttrSelectedAreas)
		a.Type = spec.Type{Name: "uint8", Kind: spec.KindUint, Bits: 8}
	})
	err := s.SetAttributes(map[uint32]any{sa.AttrSelectedAreas: uint8(3)})
	if !errors.Is(err, spec.ErrInvalidValue) {
		t.Errorf("SetAttributes = %v, want ErrInvalidValue", err)
	}
}

// idList is a list type of uint32, as a spec.List is of its entries.
type idList []uint32

// TestListValue: a nil is an empty list, the plain and the converted
// slice are the list, anything else is not.
func TestListValue(t *testing.T) {
	t.Parallel()

	if got, ok := listValue[uint32](nil); !ok || got != nil {
		t.Errorf("listValue(nil) = %v, %v", got, ok)
	}
	if got, ok := listValue[uint32]([]uint32{1}); !ok || len(got) != 1 {
		t.Errorf("listValue([]uint32) = %v, %v", got, ok)
	}
	if got, ok := listValue[uint32](idList{1, 2}); !ok || len(got) != 2 {
		t.Errorf("listValue(named slice) = %v, %v", got, ok)
	}
	if _, ok := listValue[uint32]("x"); ok {
		t.Error("listValue(string) was accepted")
	}
	if _, ok := listValue[uint32]([]string{"x"}); ok {
		t.Error("listValue([]string) was accepted")
	}
}

// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package core

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	fixedlabeldef "github.com/SukramJ/go-fabric/cluster/spec/fixedlabel"
	"github.com/SukramJ/go-fabric/cluster/spec/spectest"
	"github.com/SukramJ/go-fabric/im"
)

// TestParityMatterJS_FixedLabel holds the FixedLabel server to the
// conformance of its generated definition (spectest.CheckServer) and
// checks that LabelList reads as given, cannot be written, and survives
// the caller changing its slice.
func TestParityMatterJS_FixedLabel(t *testing.T) {
	t.Parallel()
	labels := []fixedlabeldef.LabelStruct{{Label: "room", Value: "kitchen"}, {Label: "orientation", Value: "north"}}
	srv, err := NewFixedLabel(labels)
	if err != nil {
		t.Fatal(err)
	}
	spectest.CheckServer(t, srv, fixedlabeldef.Definition, 0)
	want := slices.Clone(labels)
	labels[0].Value = "changed"
	got, ok := srv.MatterRead(fixedlabeldef.AttrLabelList)
	if l, isList := got.(spec.List[fixedlabeldef.LabelStruct]); !ok || !isList || !slices.Equal(l, want) {
		t.Errorf("LabelList %#v", got)
	}
	err = srv.MatterWrite(context.Background(), fixedlabeldef.AttrLabelList, spec.List[fixedlabeldef.LabelStruct]{})
	var sce im.StatusCodeError
	if !errors.As(err, &sce) || sce.MatterStatusCode() != im.StatusUnsupportedWrite {
		t.Errorf("write LabelList: %v", err)
	}
	if empty, err := NewFixedLabel(nil); err != nil || len(empty.MatterAttributes()) != 1 {
		t.Errorf("no labels: %v", err)
	}
	// LabelStruct's Label and Value are "max 16".
	for _, l := range []fixedlabeldef.LabelStruct{{Label: "seventeen-chars-x"}, {Value: "seventeen-chars-x"}} {
		if _, err := NewFixedLabel([]fixedlabeldef.LabelStruct{l}); !errors.Is(err, spec.ErrInvalidValue) {
			t.Errorf("label %+v: %v", l, err)
		}
	}
	if _, err := NewFixedLabel([]fixedlabeldef.LabelStruct{{Label: "sixteen-chars-xx", Value: "sixteen-chars-xx"}}); err != nil {
		t.Errorf("16 characters: %v", err)
	}
}

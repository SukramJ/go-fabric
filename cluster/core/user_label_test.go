// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package core

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/cluster/spec/spectest"
	userlabeldef "github.com/SukramJ/go-fabric/cluster/spec/userlabel"
	"github.com/SukramJ/go-fabric/im"
)

// statusOf is the IM status an error carries; 0xFF for none.
func statusOf(err error) im.StatusCode {
	var sce im.StatusCodeError
	if errors.As(err, &sce) {
		return sce.MatterStatusCode()
	}
	return 0xFF
}

// TestParityMatterJS_UserLabel holds UserLabel to its generated definition
// and to matter.js UserLabelServer.ts: a LabelList longer than maxLabels
// (default 255) is RESOURCE_EXHAUSTED, a label over "max 16" is
// CONSTRAINT_ERROR (chip UserLabelCluster.cpp:47-51), an accepted list is
// handed to the host and stored, a refused one is neither.
func TestParityMatterJS_UserLabel(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	var persisted [][]userlabeldef.LabelStruct
	initial := []userlabeldef.LabelStruct{{Label: "room", Value: "hall"}}
	srv, err := NewUserLabel(UserLabelConfig{
		Labels: initial,
		OnWrite: func(_ context.Context, l []userlabeldef.LabelStruct) error {
			persisted = append(persisted, l)
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	spectest.CheckServer(t, srv, userlabeldef.Definition, 0)
	if v, _ := srv.MatterRead(userlabeldef.AttrLabelList); !slices.Equal(v.(spec.List[userlabeldef.LabelStruct]), initial) {
		t.Errorf("initial LabelList %v", v)
	}

	labels := func(n int) spec.List[userlabeldef.LabelStruct] {
		l := make(spec.List[userlabeldef.LabelStruct], n)
		for i := range l {
			l[i] = userlabeldef.LabelStruct{Label: "k", Value: "v"}
		}
		return l
	}
	if err := srv.MatterWrite(ctx, userlabeldef.AttrLabelList, labels(DefaultUserLabelMaxLabels)); err != nil {
		t.Fatalf("255 labels: %v", err)
	}
	if err := srv.MatterWrite(ctx, userlabeldef.AttrLabelList, labels(DefaultUserLabelMaxLabels+1)); statusOf(err) != im.StatusResourceExhausted {
		t.Errorf("256 labels: %v, want RESOURCE_EXHAUSTED", err)
	}
	for _, l := range []userlabeldef.LabelStruct{{Label: "seventeen-chars-x"}, {Value: "seventeen-chars-x"}} {
		if err := srv.MatterWrite(ctx, userlabeldef.AttrLabelList, spec.List[userlabeldef.LabelStruct]{l}); statusOf(err) != im.StatusConstraintError {
			t.Errorf("label %+v: %v, want CONSTRAINT_ERROR", l, err)
		}
	}
	if err := srv.MatterWrite(ctx, userlabeldef.AttrLabelList, []userlabeldef.LabelStruct{}); statusOf(err) != im.StatusConstraintError {
		t.Errorf("plain slice: %v, want CONSTRAINT_ERROR", err)
	}
	if len(persisted) != 1 || len(persisted[0]) != DefaultUserLabelMaxLabels {
		t.Errorf("persisted %d writes", len(persisted))
	}
	if v, _ := srv.MatterRead(userlabeldef.AttrLabelList); len(v.(spec.List[userlabeldef.LabelStruct])) != DefaultUserLabelMaxLabels {
		t.Error("a refused write changed LabelList")
	}

	// A host-set limit, and a host that refuses the write.
	small, err := NewUserLabel(UserLabelConfig{MaxLabels: 4, OnWrite: func(context.Context, []userlabeldef.LabelStruct) error {
		return errors.New("disk full")
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := small.MatterWrite(ctx, userlabeldef.AttrLabelList, labels(5)); statusOf(err) != im.StatusResourceExhausted {
		t.Errorf("5 of 4: %v", err)
	}
	if err := small.MatterWrite(ctx, userlabeldef.AttrLabelList, labels(4)); err == nil {
		t.Error("the host's refusal was not answered")
	}
	if v, _ := small.MatterRead(userlabeldef.AttrLabelList); len(v.(spec.List[userlabeldef.LabelStruct])) != 0 {
		t.Error("a host-refused write was stored")
	}
	if _, err := NewUserLabel(UserLabelConfig{MaxLabels: 1, Labels: labels(2)}); !errors.Is(err, ErrUserLabelConfig) {
		t.Errorf("initial labels over the limit: %v", err)
	}
	if _, err := NewUserLabel(UserLabelConfig{Labels: []userlabeldef.LabelStruct{{Label: "seventeen-chars-x"}}}); !errors.Is(err, spec.ErrInvalidValue) {
		t.Errorf("initial label over max 16: %v", err)
	}
}

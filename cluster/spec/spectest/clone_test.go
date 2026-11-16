// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package spectest_test

import (
	"slices"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec/spectest"
)

// cloneLeaf and cloneProbe are a hand-written pair with a correct deep
// Clone: every kind fill gives a value, a nested list of structs, an
// optional pointer and a field of type any and a map left nil.
type cloneLeaf struct {
	Octets []byte
	On     bool
}

func (v cloneLeaf) Clone() cloneLeaf {
	c := v
	if v.Octets != nil {
		c.Octets = slices.Clone(v.Octets)
	}
	return c
}

type cloneProbe struct {
	Leaves  []cloneLeaf
	Label   *string
	Level   int16
	Ratio   float32
	Name    string
	Any     any
	Lookup  map[string]int
	private []byte
}

func (v cloneProbe) Clone() cloneProbe {
	c := v
	if v.Leaves != nil {
		c.Leaves = make([]cloneLeaf, len(v.Leaves))
		for i := range v.Leaves {
			c.Leaves[i] = v.Leaves[i].Clone()
		}
	}
	if v.Label != nil {
		l := *v.Label
		c.Label = &l
	}
	return c
}

// shallowProbe shares its list with the original; lossyProbe drops it.
type (
	shallowProbe struct{ List []uint32 }
	lossyProbe   struct{ List []uint32 }
)

func (v shallowProbe) Clone() shallowProbe { return v }
func (lossyProbe) Clone() lossyProbe       { return lossyProbe{} }

// TestCheckClone: a deep Clone passes; one that shares a list with the
// original, and one that loses it (an empty list cloned to nil), are
// reported.
func TestCheckClone(t *testing.T) {
	t.Parallel()
	_ = cloneProbe{}.private
	if errs := run(t, func(r *recorder) { spectest.CheckClone[cloneProbe](r) }); len(errs) != 0 {
		t.Errorf("a deep Clone was reported: %q", errs)
	}
	wantReport(t, "shallow", run(t, func(r *recorder) { spectest.CheckClone[shallowProbe](r) }), "overwriting the clone reached the original")
	wantReport(t, "lossy", run(t, func(r *recorder) { spectest.CheckClone[lossyProbe](r) }), "Clone =")
}

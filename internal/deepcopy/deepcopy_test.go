// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package deepcopy_test

import (
	"testing"

	"thingfabric.dev/go-fabric/internal/deepcopy"
)

type entry struct{ b []byte }

func (e entry) Clone() entry { return entry{b: append([]byte(nil), e.b...)} }

// TestEachCopiesEveryEntryDeep: an edit of the copy reaches no entry of the
// original; nil stays nil, empty stays empty.
func TestEachCopiesEveryEntryDeep(t *testing.T) {
	t.Parallel()
	in := []entry{{b: []byte{1}}, {b: []byte{2}}}
	out := deepcopy.Each(in)
	out[0].b[0], out[1].b[0] = 9, 9
	if in[0].b[0] != 1 || in[1].b[0] != 2 {
		t.Fatalf("the copy shares an entry's bytes: %v", in)
	}
	if deepcopy.Each[entry](nil) != nil {
		t.Error("nil did not stay nil")
	}
	if got := deepcopy.Each([]entry{}); got == nil || len(got) != 0 {
		t.Errorf("empty became %v", got)
	}
}

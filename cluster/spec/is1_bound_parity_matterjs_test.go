// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package spec_test

import (
	"bytes"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
)

// matter.js TlvString.ts:57-69 at 862ad75f bounds a string that carries an
// IS1 by its whole length before it slices at the IS1: a 40-byte NodeLabel
// ("max 32") whose sixth byte is 0x1F is a ValidationOutOfBoundsError —
// CONSTRAINT_ERROR — not the accepted "ABCDE". One that fits is accepted
// truncated.
func TestDecodeStringInBoundsTheLengthBeforeIS1(t *testing.T) {
	t.Parallel()
	decode := func(wire []byte) spec.Node {
		t.Helper()
		el, err := tlv.NewDecoder(wire).Next()
		if err != nil {
			t.Fatalf("Next: %v", err)
		}
		return spec.Node{El: el}
	}
	over := decode(append([]byte{0x0C, 0x28, 'A', 'B', 'C', 'D', 'E', 0x1F}, bytes.Repeat([]byte{'x'}, 34)...))
	wantStatus(t, "40 long before its IS1", second(spec.DecodeStringIn(0, 32)(over)), im.StatusConstraintError)

	fits := decode(append([]byte{0x0C, 0x20, 'A', 'B', 'C', 'D', 'E', 0x1F}, bytes.Repeat([]byte{'x'}, 26)...))
	if v, err := spec.DecodeStringIn(0, 32)(fits); err != nil || v != "ABCDE" {
		t.Fatalf("32 long with an IS1 = %q, %v, want \"ABCDE\"", v, err)
	}
}

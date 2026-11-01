// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"bytes"
	"context"
	"errors"
	"testing"

	mattercore "github.com/SukramJ/go-fabric/cluster/core"
	bidef "github.com/SukramJ/go-fabric/cluster/spec/basicinformation"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
)

// A NodeLabel write is bounded by the length it arrived with before the
// decoder cut it at its IS1: matter.js decodes the write with the
// attribute's TlvString ("max 32"), which checks `value.length >
// maxLength` before it slices (TlvString.ts:57-69 at 862ad75f), so a
// 40-byte label whose sixth byte is 0x1F is CONSTRAINT_ERROR, not the
// accepted "ABCDE". A label that fits is written truncated.
func TestNodeLabelWriteBoundsTheLengthBeforeIS1(t *testing.T) {
	t.Parallel()
	path := im.ConcreteAttributePath{Cluster: bidef.ClusterID, Attribute: bidef.AttrNodeLabel}
	write := func(wire []byte) error {
		t.Helper()
		dec := tlv.NewDecoder(wire)
		el, err := dec.Next()
		if err != nil {
			t.Fatalf("Next: %v", err)
		}
		v, err := attributeValueReader(path, el, dec)
		if err != nil {
			t.Fatalf("attributeValueReader: %v", err)
		}
		bi, err := mattercore.NewBasicInformation(mattercore.Config{VendorID: 0xFFF1, ProductID: 0x8001, NodeLabel: "n", VendorName: "v", ProductName: "p", SerialNumber: "s"})
		if err != nil {
			t.Fatalf("NewBasicInformation: %v", err)
		}
		return bi.MatterWrite(context.Background(), bidef.AttrNodeLabel, v.Value)
	}

	err := write(append([]byte{0x0C, 0x28, 'A', 'B', 'C', 'D', 'E', 0x1F}, bytes.Repeat([]byte{'x'}, 34)...))
	var se im.StatusCodeError
	if !errors.As(err, &se) || se.MatterStatusCode() != im.StatusConstraintError {
		t.Fatalf("40-byte NodeLabel with an IS1 at byte 6: %v, want CONSTRAINT_ERROR", err)
	}
	if err := write(append([]byte{0x0C, 0x20, 'A', 'B', 'C', 'D', 'E', 0x1F}, bytes.Repeat([]byte{'x'}, 26)...)); err != nil {
		t.Fatalf("32-byte NodeLabel with an IS1: %v, want accepted", err)
	}
}

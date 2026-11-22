// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"testing"

	gkmdef "thingfabric.dev/go-fabric/cluster/spec/groupkeymanagement"
	"thingfabric.dev/go-fabric/im"
	"thingfabric.dev/go-fabric/tlv"
)

// TestKeySetWriteKeepsANullStartTimeApartFromZero (R2-P6-par-1013, the
// wire half): a KeySetWrite whose EpochKey1 is null reaches the server
// with EpochStartTime1 null when the wire says null and as a set 0 when
// the wire says 0 — the server refuses the second (InvalidCommand), which
// it can only do when the decode keeps the two apart.
func TestKeySetWriteKeepsANullStartTimeApartFromZero(t *testing.T) {
	t.Parallel()
	key := make([]byte, 16)
	for _, nullStart := range []bool{true, false} {
		enc := tlv.NewEncoder()
		enc.StartStruct(tlv.AnonymousTag())
		enc.StartStruct(tlv.ContextTag(0))
		enc.PutUint(tlv.ContextTag(0), 7)
		enc.PutUint(tlv.ContextTag(1), 0)
		enc.PutOctets(tlv.ContextTag(2), key)
		enc.PutUint(tlv.ContextTag(3), 1)
		enc.PutNull(tlv.ContextTag(4))
		if nullStart {
			enc.PutNull(tlv.ContextTag(5))
		} else {
			enc.PutUint(tlv.ContextTag(5), 0)
		}
		enc.PutNull(tlv.ContextTag(6))
		enc.PutNull(tlv.ContextTag(7))
		_ = enc.EndContainer()
		_ = enc.EndContainer()
		b, err := enc.Bytes()
		if err != nil {
			t.Fatal(err)
		}
		dec := tlv.NewDecoder(b)
		open, err := dec.Next()
		if err != nil {
			t.Fatal(err)
		}
		f, err := commandFieldsReader(im.ConcreteCommandPath{Cluster: gkmdef.ClusterID, Command: gkmdef.CmdKeySetWrite, HasCluster: true, HasCommand: true}, dec, open)
		if err != nil {
			t.Fatalf("null start %v: %v", nullStart, err)
		}
		req, ok := f.(gkmdef.KeySetWriteRequest)
		if !ok {
			t.Fatalf("decoded %T, want the generated request", f)
		}
		if got := req.GroupKeySet.EpochStartTime1.Null; got != nullStart {
			t.Errorf("wire null=%v: EpochStartTime1.Null = %v", nullStart, got)
		}
	}
}

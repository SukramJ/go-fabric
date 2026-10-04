// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package bridge

// GroupKeyManagement (0x003F) over the wire: every command is sealed by a
// CASE controller, decoded by commandFieldsReader, served by the real
// cluster on a real store and its response encoded by
// defaultCommandFieldsWriter. Before the codec existed, each of these
// requests reached the server as the generic tag map and answered Failure.

import (
	"bytes"
	"testing"

	mattercore "github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/store"
	"github.com/SukramJ/go-fabric/tlv"
)

const (
	gkmCluster              uint32 = 0x003F
	gkmCmdKeySetWrite       uint32 = 0x00
	gkmCmdKeySetRead        uint32 = 0x01
	gkmCmdKeySetReadResp    uint32 = 0x02
	gkmCmdKeySetRemove      uint32 = 0x03
	gkmCmdKeySetReadAll     uint32 = 0x04
	gkmCmdKeySetReadAllResp uint32 = 0x05
)

// newGKMHarness starts a bridge whose root carries a real
// GroupKeyManagement server, with a CASE Administer ACL in place.
func newGKMHarness(t *testing.T) *secureHarness {
	t.Helper()
	h := newSecureHarness(t, nil, func(st *store.Store, _ uint8) []contract.ClusterServer {
		gkm, err := mattercore.NewGroupKeyManagement(st, mattercore.GroupKeyMgmtConfig{})
		if err != nil {
			t.Fatalf("NewGroupKeyManagement: %v", err)
		}
		return []contract.ClusterServer{gkm}
	})
	h.allowAll()
	return h
}

// putKeySet encodes a GroupKeySetStruct at tag 0 with one epoch key and an
// explicit GroupKeyMulticastPolicy.
func putKeySet(id uint16, key []byte, start uint64, multicastPolicy uint64) func(enc *tlv.Encoder) {
	return func(enc *tlv.Encoder) {
		enc.StartStruct(tlv.ContextTag(0))
		enc.PutUint(tlv.ContextTag(0), uint64(id))
		enc.PutUint(tlv.ContextTag(1), 0) // TrustFirst
		enc.PutOctets(tlv.ContextTag(2), key)
		enc.PutUint(tlv.ContextTag(3), start)
		enc.PutNull(tlv.ContextTag(4))
		enc.PutNull(tlv.ContextTag(5))
		enc.PutNull(tlv.ContextTag(6))
		enc.PutNull(tlv.ContextTag(7))
		enc.PutUint(tlv.ContextTag(8), multicastPolicy)
		_ = enc.EndContainer()
	}
}

func putUint16Field(v uint16) func(enc *tlv.Encoder) {
	return func(enc *tlv.Encoder) { enc.PutUint(tlv.ContextTag(0), uint64(v)) }
}

func TestGroupKeyManagementCommandsOverTheWire(t *testing.T) {
	t.Parallel()
	h := newGKMHarness(t)
	key := bytes.Repeat([]byte{0xA5}, 16)

	// KeySetWrite with GroupKeyMulticastPolicy=AllNodes: matter.js 452d6f5c
	// accepts any policy and ignores it.
	if _, _, status, isStatus := invokeResult(t, h.invoke(0, gkmCluster, gkmCmdKeySetWrite, putKeySet(0x01A1, key, 2220000, 1))); !isStatus || status != im.StatusSuccess {
		t.Fatalf("KeySetWrite status = %v (isStatus=%v), want Success", status, isStatus)
	}
	stored, err := h.store.GetGroupKeySet(t.Context(), h.fabric, 0x01A1)
	if err != nil {
		t.Fatalf("stored key set: %v", err)
	}
	if !bytes.Equal(stored.EpochKey0, key) || stored.EpochStart0 != 2220000 {
		t.Fatalf("stored key set = %+v, want the written key and start time", stored)
	}

	// KeySetRead: KeySetReadResponse with keys null and PerGroupID reported.
	cmd, fields, _, isStatus := invokeResult(t, h.invoke(0, gkmCluster, gkmCmdKeySetRead, putUint16Field(0x01A1)))
	if isStatus || cmd != gkmCmdKeySetReadResp {
		t.Fatalf("KeySetRead answered command 0x%02X (isStatus=%v), want KeySetReadResponse 0x02", cmd, isStatus)
	}
	gks := fields.mustChild(t, 0)
	if got := gks.mustChild(t, 0).El.Uint; got != 0x01A1 {
		t.Errorf("GroupKeySetID = 0x%X, want 0x01A1", got)
	}
	for _, tag := range []uint64{2, 4, 5, 6, 7} {
		if !gks.mustChild(t, tag).El.IsNull {
			t.Errorf("field %d is not null in KeySetReadResponse", tag)
		}
	}
	if got := gks.mustChild(t, 3).El.Uint; got != 2220000 {
		t.Errorf("EpochStartTime0 = %d, want 2220000", got)
	}
	if mp := gks.mustChild(t, 8); mp.El.IsNull || mp.El.Uint != 0 {
		t.Errorf("GroupKeyMulticastPolicy = %+v, want PerGroupID (0)", mp.El)
	}

	// KeySetReadAllIndices lists the IPK first.
	cmd, fields, _, isStatus = invokeResult(t, h.invoke(0, gkmCluster, gkmCmdKeySetReadAll, nil))
	if isStatus || cmd != gkmCmdKeySetReadAllResp {
		t.Fatalf("KeySetReadAllIndices answered command 0x%02X (isStatus=%v), want 0x05", cmd, isStatus)
	}
	var ids []uint64
	for _, c := range fields.mustChild(t, 0).Children {
		ids = append(ids, c.El.Uint)
	}
	if len(ids) != 2 || ids[0] != 0 || ids[1] != 0x01A1 {
		t.Fatalf("GroupKeySetIDs = %v, want [0 0x1A1]", ids)
	}

	// KeySetRemove, then KeySetRead answers NotFound.
	if _, _, status, _ := invokeResult(t, h.invoke(0, gkmCluster, gkmCmdKeySetRemove, putUint16Field(0x01A1))); status != im.StatusSuccess {
		t.Fatalf("KeySetRemove status = %v, want Success", status)
	}
	if _, _, status, _ := invokeResult(t, h.invoke(0, gkmCluster, gkmCmdKeySetRead, putUint16Field(0x01A1))); status != im.StatusNotFound {
		t.Fatalf("KeySetRead after remove status = %v, want NotFound", status)
	}
	// Key set 0 cannot be removed.
	if _, _, status, _ := invokeResult(t, h.invoke(0, gkmCluster, gkmCmdKeySetRemove, putUint16Field(0))); status != im.StatusInvalidCommand {
		t.Fatalf("KeySetRemove(0) status = %v, want InvalidCommand", status)
	}
}

// TestGroupKeyManagementMalformedPayloadsOverTheWire pins the schema-level
// rejects: a request that does not match the command's TLV schema answers
// InvalidCommand (matter.js #decodeWithSchema / requestTlv.validate), an
// epoch key of the wrong length ConstraintError (the field's "16"
// constraint, enforced by the server).
func TestGroupKeyManagementMalformedPayloadsOverTheWire(t *testing.T) {
	t.Parallel()
	h := newGKMHarness(t)

	cases := []struct {
		name   string
		cmd    uint32
		fields func(enc *tlv.Encoder)
		want   im.StatusCode
	}{
		{"KeySetWrite without GroupKeySet", gkmCmdKeySetWrite, nil, im.StatusInvalidCommand},
		{"KeySetWrite GroupKeySet not a struct", gkmCmdKeySetWrite, func(enc *tlv.Encoder) { enc.PutUint(tlv.ContextTag(0), 1) }, im.StatusInvalidCommand},
		{"KeySetWrite missing EpochStartTime0", gkmCmdKeySetWrite, func(enc *tlv.Encoder) {
			enc.StartStruct(tlv.ContextTag(0))
			enc.PutUint(tlv.ContextTag(0), 5)
			enc.PutUint(tlv.ContextTag(1), 0)
			enc.PutOctets(tlv.ContextTag(2), make([]byte, 16))
			for _, tag := range []uint8{4, 5, 6, 7} {
				enc.PutNull(tlv.ContextTag(tag))
			}
			_ = enc.EndContainer()
		}, im.StatusInvalidCommand},
		{"KeySetWrite short epoch key", gkmCmdKeySetWrite, putKeySet(5, make([]byte, 15), 10, 0), im.StatusConstraintError},
		{"KeySetRead without id", gkmCmdKeySetRead, nil, im.StatusInvalidCommand},
		{"KeySetRemove id is a string", gkmCmdKeySetRemove, func(enc *tlv.Encoder) { enc.PutUTF8(tlv.ContextTag(0), "x") }, im.StatusInvalidCommand},
	}
	for _, tc := range cases {
		_, _, status, isStatus := invokeResult(t, h.invoke(0, gkmCluster, tc.cmd, tc.fields))
		if !isStatus || status != tc.want {
			t.Errorf("%s: status = %v (isStatus=%v), want %v", tc.name, status, isStatus, tc.want)
		}
	}
}

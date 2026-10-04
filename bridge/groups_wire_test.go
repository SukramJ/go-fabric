// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package bridge

// The Groups cluster over the wire, on endpoints the assembler gave the
// stack's Groups server, with GroupKeyManagement.GroupTable read back
// through the IM read path.

import (
	"bytes"
	"slices"
	"testing"

	mattercore "github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
)

// readGroupTable reads GroupKeyManagement.GroupTable (fabric-filtered) and
// returns group id → endpoints.
func (h *secureHarness) readGroupTable(t *testing.T) map[uint64][]uint64 {
	t.Helper()
	enc := tlv.NewEncoder()
	enc.StartStruct(tlv.AnonymousTag())
	enc.StartArray(tlv.ContextTag(0)) // AttributeRequests
	enc.StartList(tlv.AnonymousTag())
	enc.PutUint(tlv.ContextTag(2), 0)
	enc.PutUint(tlv.ContextTag(3), uint64(gkmCluster))
	enc.PutUint(tlv.ContextTag(4), 0x0001)
	_ = enc.EndContainer()
	_ = enc.EndContainer()
	enc.PutBool(tlv.ContextTag(3), true) // FabricFiltered
	enc.PutUint(tlv.ContextTag(0xFF), 12)
	_ = enc.EndContainer()
	body, err := enc.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	op, payload, ok := h.exchange(im.OpcodeReadRequest, body)
	if !ok || op != im.OpcodeReportData {
		t.Fatalf("Read answered opcode 0x%02X (ok=%v)", op, ok)
	}
	report := decodeTLVTree(t, payload)
	reports := report.mustChild(t, 1).Children
	if len(reports) != 1 {
		t.Fatalf("ReportData carries %d attribute reports, want 1", len(reports))
	}
	data := reports[0].mustChild(t, 1).mustChild(t, 2)
	out := make(map[uint64][]uint64)
	for _, entry := range data.Children {
		var eps []uint64
		for _, ep := range entry.mustChild(t, 2).Children {
			eps = append(eps, ep.El.Uint)
		}
		out[entry.mustChild(t, 1).El.Uint] = eps
	}
	return out
}

func TestGroupsCommandsOverTheWire(t *testing.T) {
	t.Parallel()
	gh := newGroupsHarness(t, 2)
	lamps := gh.lampIDs()
	gh.provisionGroup(t, 0x01A1, bytes.Repeat([]byte{0xA1}, 16), 0x0101, "Kitchen", lamps...)

	table := gh.readGroupTable(t)
	if got := table[0x0101]; !slices.Equal(got, []uint64{uint64(lamps[0]), uint64(lamps[1])}) {
		t.Fatalf("GroupTable[0x0101] = %v, want both lamps", got)
	}

	// ViewGroup answers the name.
	cmd, fields, _, isStatus := invokeResult(t, gh.invoke(lamps[0], mattercore.GroupsClusterID, 0x01, putUint16Field(0x0101)))
	if isStatus || cmd != 0x01 || fields.mustChild(t, 0).El.Uint != 0 || fields.mustChild(t, 2).El.String != "Kitchen" {
		t.Fatalf("ViewGroup = cmd 0x%02X fields %+v", cmd, fields)
	}

	// GetGroupMembership with an empty list answers every group and the
	// remaining capacity.
	cmd, fields, _, _ = invokeResult(t, gh.invoke(lamps[0], mattercore.GroupsClusterID, 0x02, func(enc *tlv.Encoder) {
		enc.StartArray(tlv.ContextTag(0))
		_ = enc.EndContainer()
	}))
	if cmd != 0x02 || fields.mustChild(t, 0).El.Uint != 0xFE-1 || len(fields.mustChild(t, 1).Children) != 1 {
		t.Fatalf("GetGroupMembership = cmd 0x%02X fields %+v", cmd, fields)
	}

	// AddGroup for a group without a GroupKeyMap entry: UnsupportedAccess.
	_, fields, _, _ = invokeResult(t, gh.invoke(lamps[0], mattercore.GroupsClusterID, 0x00, func(enc *tlv.Encoder) {
		enc.PutUint(tlv.ContextTag(0), 0x0777)
		enc.PutUTF8(tlv.ContextTag(1), "")
	}))
	if got := im.StatusCode(fields.mustChild(t, 0).El.Uint); got != im.StatusUnsupportedAccess {
		t.Fatalf("AddGroup unmapped = %v, want UnsupportedAccess", got)
	}

	// RemoveGroup on one lamp leaves the other in the group.
	_, fields, _, _ = invokeResult(t, gh.invoke(lamps[0], mattercore.GroupsClusterID, 0x03, putUint16Field(0x0101)))
	if fields.mustChild(t, 0).El.Uint != 0 {
		t.Fatalf("RemoveGroup = %+v", fields)
	}
	if got := gh.readGroupTable(t)[0x0101]; !slices.Equal(got, []uint64{uint64(lamps[1])}) {
		t.Fatalf("GroupTable after RemoveGroup = %v", got)
	}

	// RemoveAllGroups on the other lamp empties the table.
	if _, _, status, isStatus := invokeResult(t, gh.invoke(lamps[1], mattercore.GroupsClusterID, 0x04, nil)); !isStatus || status != im.StatusSuccess {
		t.Fatalf("RemoveAllGroups = %v", status)
	}
	if got := gh.readGroupTable(t); len(got) != 0 {
		t.Fatalf("GroupTable after RemoveAllGroups = %v", got)
	}

	// AddGroupIfIdentifying while not identifying: success, nothing added.
	if _, _, status, _ := invokeResult(t, gh.invoke(lamps[0], mattercore.GroupsClusterID, 0x05, func(enc *tlv.Encoder) {
		enc.PutUint(tlv.ContextTag(0), 0x0101)
		enc.PutUTF8(tlv.ContextTag(1), "x")
	})); status != im.StatusSuccess {
		t.Fatalf("AddGroupIfIdentifying = %v", status)
	}
	if got := gh.readGroupTable(t); len(got) != 0 {
		t.Fatalf("GroupTable after AddGroupIfIdentifying while not identifying = %v", got)
	}
}

func TestGroupsMalformedPayloadsOverTheWire(t *testing.T) {
	t.Parallel()
	gh := newGroupsHarness(t, 1)
	ep := gh.lampIDs()[0]
	cases := []struct {
		name   string
		cmd    uint32
		fields func(enc *tlv.Encoder)
	}{
		{"AddGroup without a name", 0x00, putUint16Field(1)},
		{"AddGroup name is a number", 0x00, func(enc *tlv.Encoder) {
			enc.PutUint(tlv.ContextTag(0), 1)
			enc.PutUint(tlv.ContextTag(1), 1)
		}},
		{"ViewGroup without an id", 0x01, nil},
		{"GetGroupMembership without a list", 0x02, nil},
		{"GetGroupMembership list is a number", 0x02, putUint16Field(1)},
		{"GetGroupMembership entry is a string", 0x02, func(enc *tlv.Encoder) {
			enc.StartArray(tlv.ContextTag(0))
			enc.PutUTF8(tlv.AnonymousTag(), "x")
			_ = enc.EndContainer()
		}},
		{"RemoveGroup without an id", 0x03, nil},
		{"AddGroupIfIdentifying without an id", 0x05, func(enc *tlv.Encoder) { enc.PutUTF8(tlv.ContextTag(1), "x") }},
	}
	for _, tc := range cases {
		_, _, status, isStatus := invokeResult(t, gh.invoke(ep, mattercore.GroupsClusterID, tc.cmd, tc.fields))
		if !isStatus || status != im.StatusInvalidCommand {
			t.Errorf("%s: status %v (isStatus=%v), want InvalidCommand", tc.name, status, isStatus)
		}
	}
	// A group id beyond uint16 violates the field's type bound.
	_, _, status, _ := invokeResult(t, gh.invoke(ep, mattercore.GroupsClusterID, 0x01, func(enc *tlv.Encoder) {
		enc.PutUint(tlv.ContextTag(0), 0x1_0000)
	}))
	if status != im.StatusConstraintError {
		t.Errorf("ViewGroup id 0x10000: %v, want ConstraintError", status)
	}
}

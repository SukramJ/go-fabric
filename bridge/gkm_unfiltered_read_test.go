// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"slices"
	"testing"

	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/store"
	"github.com/SukramJ/go-fabric/tlv"
)

// readAttributeData reads one concrete attribute over the CASE session with
// the given FabricFiltered flag and returns the AttributeDataIB's Data node.
func (h *secureHarness) readAttributeData(t *testing.T, endpoint uint16, cluster, attribute uint32, fabricFiltered bool) tlvNode {
	t.Helper()
	enc := tlv.NewEncoder()
	enc.StartStruct(tlv.AnonymousTag())
	enc.StartArray(tlv.ContextTag(0)) // AttributeRequests
	enc.StartList(tlv.AnonymousTag())
	enc.PutUint(tlv.ContextTag(2), uint64(endpoint))
	enc.PutUint(tlv.ContextTag(3), uint64(cluster))
	enc.PutUint(tlv.ContextTag(4), uint64(attribute))
	_ = enc.EndContainer()
	_ = enc.EndContainer()
	enc.PutBool(tlv.ContextTag(3), fabricFiltered)
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
	reports := decodeTLVTree(t, payload).mustChild(t, 1).Children
	if len(reports) != 1 {
		t.Fatalf("ReportData carries %d attribute reports, want 1", len(reports))
	}
	data, ok := reports[0].child(1)
	if !ok {
		t.Fatalf("attribute 0x%04X/0x%04X answered a status, not data", cluster, attribute)
	}
	return data.mustChild(t, 2)
}

// fabricIndexesOf lists the FabricIndex (tag 0xFE) of every entry.
func fabricIndexesOf(t *testing.T, list tlvNode) []uint64 {
	t.Helper()
	out := make([]uint64, 0, len(list.Children))
	for _, e := range list.Children {
		out = append(out, e.mustChild(t, 0xFE).El.Uint)
	}
	return out
}

// TestGroupKeyManagementUnfilteredReadsCoverEveryFabric: GroupKeyMap and
// GroupTable are fabric-scoped lists without fabric-sensitive fields, so a
// read with isFabricFiltered=false returns every fabric's entries whole,
// and a fabric-filtered read only the accessing fabric's. Mirrors matter.js
// ListManager createProxy (filtering only for `session.fabricFiltered ||
// config.fabricSensitive`) — the same rule OperationalCredentials.Fabrics
// and NOCs follow here.
func TestGroupKeyManagementUnfilteredReadsCoverEveryFabric(t *testing.T) {
	t.Parallel()
	gh := newGroupsHarness(t, 2)
	ctx := context.Background()
	lamps := gh.lampIDs()
	gh.provisionGroup(t, 0x0011, make([]byte, 16), 0x0101, "Own", lamps[0])

	other := addHarnessFabric(t, gh.store, 0x0000_0000_0000_0BAD, [8]byte{9, 9, 9, 9, 9, 9, 9, 9})
	if err := gh.store.SetGroupKeyMapping(ctx, store.GroupKeyMapping{FabricIndex: other, GroupID: 0x0202, GroupKeySetID: 0x0022}); err != nil {
		t.Fatal(err)
	}
	if err := gh.groups.AddEndpointForGroup(ctx, other, 0x0202, lamps[1], "Other"); err != nil {
		t.Fatal(err)
	}

	for _, attr := range []uint32{0x0000, 0x0001} { // GroupKeyMap, GroupTable
		filtered := fabricIndexesOf(t, gh.readAttributeData(t, 0, gkmCluster, attr, true))
		if !slices.Equal(filtered, []uint64{uint64(gh.fabric)}) {
			t.Errorf("attribute 0x%04X fabric-filtered: fabrics %v, want only %d", attr, filtered, gh.fabric)
		}
		all := fabricIndexesOf(t, gh.readAttributeData(t, 0, gkmCluster, attr, false))
		if !slices.Equal(all, []uint64{uint64(gh.fabric), uint64(other)}) {
			t.Errorf("attribute 0x%04X unfiltered: fabrics %v, want [%d %d]", attr, all, gh.fabric, other)
		}
	}
	// The other fabric's entries go out whole: no field of either struct
	// is fabric-sensitive.
	table := gh.readAttributeData(t, 0, gkmCluster, 0x0001, false)
	foreign := table.Children[1]
	if foreign.mustChild(t, 1).El.Uint != 0x0202 || foreign.mustChild(t, 3).El.String != "Other" {
		t.Errorf("foreign GroupTable entry lost its fields: %+v", foreign)
	}
}

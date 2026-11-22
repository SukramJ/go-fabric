// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package cli

import (
	"testing"

	"thingfabric.dev/go-fabric/client/interaction"
	"thingfabric.dev/go-fabric/cluster/spec"
	groupsdef "thingfabric.dev/go-fabric/cluster/spec/groups"
	"thingfabric.dev/go-fabric/im"
	"thingfabric.dev/go-fabric/tlv"
)

// addGroupFields is an AddGroupResponse's fields with its members in the
// order given: {tag, value} pairs.
func addGroupFields(t *testing.T, members ...[2]uint64) spec.Node {
	t.Helper()
	enc := tlv.NewEncoder()
	enc.StartStruct(tlv.AnonymousTag())
	for _, m := range members {
		enc.PutUint(tlv.ContextTag(uint8(m[0])), m[1])
	}
	if err := enc.EndContainer(); err != nil {
		t.Fatal(err)
	}
	b, err := enc.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	n, err := spec.ReadElement(tlv.NewDecoder(b))
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// TestAddGroupOKJudgesByTag holds R2-P6-wire-2609: the AddGroupResponse
// is read by its members' tags — GroupId listed first does not pass for
// the Status — and an answer without the response command fails.
func TestAddGroupOKJudgesByTag(t *testing.T) {
	ok := func(fields spec.Node) interaction.Result {
		return interaction.Result{Command: groupsdef.CmdAddGroupResponse, HasFields: true, Fields: fields}
	}
	if err := addGroupOK(ok(addGroupFields(t, [2]uint64{0, 0}, [2]uint64{1, 257})), 257); err != nil {
		t.Errorf("a successful response: %v", err)
	}
	if err := addGroupOK(ok(addGroupFields(t, [2]uint64{1, 257}, [2]uint64{0, 0})), 257); err != nil {
		t.Errorf("the members in reverse order: %v", err)
	}
	// GroupId 0 first and a failing Status: position would read success.
	if err := addGroupOK(ok(addGroupFields(t, [2]uint64{1, 0}, [2]uint64{0, uint64(im.StatusResourceExhausted)})), 257); err == nil {
		t.Error("a failing Status behind the GroupId passed")
	}
	if err := addGroupOK(ok(addGroupFields(t, [2]uint64{0, 0}, [2]uint64{1, 258})), 257); err == nil {
		t.Error("a response for another group passed")
	}
	if err := addGroupOK(interaction.Result{Status: im.StatusSuccess}, 257); err == nil {
		t.Error("a status without the response command passed")
	}
}

// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"errors"
	"testing"

	"thingfabric.dev/go-fabric/cluster/spec"
	groupsdef "thingfabric.dev/go-fabric/cluster/spec/groups"
	"thingfabric.dev/go-fabric/im"
	"thingfabric.dev/go-fabric/tlv"
)

// TestHandReaderNeverReadsAnUncheckedRequest (gate round 4,
// fable-read-6a-gate re-read §4): with the hand readers' own checks gone,
// a request reaches a hand reader only after its definition checked it —
// a command the definition knows without a decoder is refused
// INVALID_COMMAND, and one the definition does not know is not handed to
// the reader at all.
func TestHandReaderNeverReadsAnUncheckedRequest(t *testing.T) {
	t.Parallel()
	def := &spec.Cluster{Name: "Fake", Commands: []spec.Command{{ID: 1, Name: "NoDecoder", Direction: spec.Request}}}
	called := false
	read := func(im.ConcreteCommandPath, *tlv.Decoder) (any, bool, error) {
		called = true
		return "read", true, nil
	}
	fields := func() (*tlv.Decoder, tlv.Element) {
		enc := tlv.NewEncoder()
		enc.StartStruct(tlv.AnonymousTag())
		enc.PutUint(tlv.ContextTag(0), 7)
		_ = enc.EndContainer()
		b, _ := enc.Bytes()
		dec := tlv.NewDecoder(b)
		el, _ := dec.Next()
		return dec, el
	}
	dec, el := fields()
	_, err := heldByDefinition(def, im.ConcreteCommandPath{Cluster: 0xFFF1FC09, Command: 1}, dec, el, read)
	if sce, ok := errors.AsType[im.StatusCodeError](err); !ok || sce.MatterStatusCode() != im.StatusInvalidCommand {
		t.Errorf("a command without a decoder: err %v, want INVALID_COMMAND", err)
	}
	dec, el = fields()
	if _, err := heldByDefinition(def, im.ConcreteCommandPath{Cluster: 0xFFF1FC09, Command: 2}, dec, el, read); err != nil {
		t.Errorf("a command the definition does not know: %v", err)
	}
	if called {
		t.Error("the hand reader read an unchecked request")
	}
}

// TestGroupListAsATLVListIsRefused (gate round 4, re-read §4): a TLV list
// (0x17) where GetGroupMembership's GroupList array belongs is refused
// INVALID_COMMAND, as matter.js's TlvArray refuses every type but an array
// (TlvArray.ts:73-74) — the hand reader's own type check is gone, the
// definition's holds it.
func TestGroupListAsATLVListIsRefused(t *testing.T) {
	t.Parallel()
	enc := tlv.NewEncoder()
	enc.StartStruct(tlv.AnonymousTag())
	enc.StartList(tlv.ContextTag(0))
	enc.PutUint(tlv.AnonymousTag(), 1)
	_ = enc.EndContainer()
	_ = enc.EndContainer()
	b, err := enc.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	dec := tlv.NewDecoder(b)
	el, _ := dec.Next()
	_, err = commandFieldsReader(im.ConcreteCommandPath{Cluster: groupsdef.ClusterID, Command: groupsdef.CmdGetGroupMembership, HasCluster: true, HasCommand: true}, dec, el)
	if sce, ok := errors.AsType[im.StatusCodeError](err); !ok || sce.MatterStatusCode() != im.StatusInvalidCommand {
		t.Errorf("GroupList as a TLV list: err %v, want INVALID_COMMAND", err)
	}
}

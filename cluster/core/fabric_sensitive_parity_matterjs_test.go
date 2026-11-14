// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core_test

import (
	"bytes"
	"testing"

	"github.com/SukramJ/go-fabric/bridge"
	"github.com/SukramJ/go-fabric/cluster/spec"
	messagesdef "github.com/SukramJ/go-fabric/cluster/spec/messages"
	"github.com/SukramJ/go-fabric/tlv"
)

// The tests of this file port matter.js
// packages/node/test/behavior/state/managed/values/ListManagerTest.ts:263
// ("masks another fabric's sensitive field in the entries a sort
// comparator receives") by meaning onto the servers whose fabric-scoped
// list holds fabric-sensitive fields: on a read that is not
// fabric-filtered, the list holds the entries of fabrics 2 and 1, the
// accessing fabric 1's entry is whole and fabric 2's carries no
// fabric-sensitive field — and for a session without a fabric no entry
// does (AccessControl.ts:347-349 at db021da0). The assertions are on the
// wire bytes the bridge writes.

// fabricIndexOnly is an entry as another fabric reads it when every field
// but FabricIndex is fabric-sensitive.
type fabricIndexOnly uint8

func (f fabricIndexOnly) EncodeTLV(enc *tlv.Encoder, tag tlv.Tag) {
	enc.StartStruct(tag)
	enc.PutUint(tlv.ContextTag(0xFE), uint64(f))
	_ = enc.EndContainer()
}

// wantWire holds v, as the bridge writes it, to the list of want.
func wantWire(t *testing.T, what string, v any, want ...spec.Encodable) {
	t.Helper()
	got, err := bridge.AttributeValueTLV(v)
	if err != nil {
		t.Fatalf("%s: %v", what, err)
	}
	enc := tlv.NewEncoder()
	spec.List[spec.Encodable](want).EncodeTLV(enc, tlv.AnonymousTag())
	exp, err := enc.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, exp) {
		t.Errorf("%s:\n got % X\nwant % X", what, got, exp)
	}
}

// TestParityMatterJS_MessagesEntryOfAnotherFabricWithoutSensitiveFields:
// every field of MessageStruct but FabricIndex is "S"
// (messages.element.ts:121-133), so another fabric's message reads as its
// FabricIndex alone.
func TestParityMatterJS_MessagesEntryOfAnotherFabricWithoutSensitiveFields(t *testing.T) {
	t.Parallel()
	m, _, _ := newMessages(t, 0)
	for _, f := range []uint8{2, 1} {
		if err := decodeAndInvoke(t, m, msgCtx(f, false), messagesdef.CmdPresentMessagesRequest, presentRequest(messageID(f), "of a fabric")); err != nil {
			t.Fatal(err)
		}
	}
	local, _ := m.MatterRead(messagesdef.AttrMessages)
	whole := local.(spec.List[messagesdef.MessageStruct])
	if len(whole) != 2 || whole[1].FabricIndex != 1 {
		t.Fatalf("local read %+v", whole)
	}
	v, _ := m.MatterReadFiltered(msgCtx(1, false), messagesdef.AttrMessages)
	wantWire(t, "unfiltered read by fabric 1", v, fabricIndexOnly(2), whole[1])
	v, _ = m.MatterReadFiltered(msgCtx(0, false), messagesdef.AttrMessages)
	wantWire(t, "unfiltered read without a fabric", v, fabricIndexOnly(2), fabricIndexOnly(1))
	v, _ = m.MatterReadFiltered(msgCtx(1, true), messagesdef.AttrMessages)
	wantWire(t, "filtered read by fabric 1", v, whole[1])
}

// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"testing"

	"thingfabric.dev/go-fabric/cluster/levelcontrol"
	lvldef "thingfabric.dev/go-fabric/cluster/spec/levelcontrol"
	"thingfabric.dev/go-fabric/contract"
	"thingfabric.dev/go-fabric/tlv"
)

// TestUndecodableWriteEndsTheInteractionWithFailure (owner decision 161,
// R2-P6-wire-0442): a WriteRequest whose second attribute data writes a
// structure to LevelControl.OnLevel is answered with a StatusResponse
// FAILURE in place of the WriteResponse; the first attribute data was
// written, the structure wrote nothing — OnLevel is not null.
func TestUndecodableWriteEndsTheInteractionWithFailure(t *testing.T) {
	t.Parallel()
	srv := levelcontrol.NewServer(levelcontrol.Config{Source: &notifyingLevelPort{}})
	h := newAppHarness(t, appDevice{deviceType: 0x0101, servers: []contract.ClusterServer{srv}})
	epID := h.endpoints[0x0101]

	enc := tlv.NewEncoder()
	enc.StartStruct(tlv.AnonymousTag())
	enc.PutBool(tlv.ContextTag(0), false)
	enc.PutBool(tlv.ContextTag(1), false)
	enc.StartArray(tlv.ContextTag(2))
	data := func(value func()) {
		enc.StartStruct(tlv.AnonymousTag())
		enc.StartList(tlv.ContextTag(1))
		enc.PutUint(tlv.ContextTag(2), uint64(epID))
		enc.PutUint(tlv.ContextTag(3), uint64(lvldef.ClusterID))
		enc.PutUint(tlv.ContextTag(4), uint64(lvldef.AttrOnLevel))
		_ = enc.EndContainer()
		value()
		_ = enc.EndContainer()
	}
	data(func() { enc.PutUint(tlv.ContextTag(2), 77) })
	data(func() {
		enc.StartStruct(tlv.ContextTag(2))
		enc.PutUint(tlv.ContextTag(0), 1)
		_ = enc.EndContainer()
	})
	_ = enc.EndContainer()
	enc.PutBool(tlv.ContextTag(3), false)
	enc.PutUint(tlv.ContextTag(0xFF), 12)
	_ = enc.EndContainer()
	body, err := enc.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	op, payload, ok := h.exchange(0x06, body)
	if !ok || op != 0x01 {
		t.Fatalf("answered opcode 0x%02X (ok=%v), want a StatusResponse", op, ok)
	}
	if st := decodeTLVTree(t, payload).mustChild(t, 0).El.Uint; st != 0x01 {
		t.Errorf("StatusResponse status 0x%02X, want FAILURE", st)
	}
	v, _ := srv.MatterRead(lvldef.AttrOnLevel)
	if got, isLevel := v.(uint8); !isLevel || got != 77 {
		t.Errorf("OnLevel is %#v, want 77 — the first data written, the structure not", v)
	}
}

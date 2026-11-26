// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Kill tests for the C4 survivors in the command-field decoders and the
// small lookups beside them: fields_reader_groupkeys.go, fields_reader_scenes.go,
// element_qualities.go and case_seam.go (phase 8 wave 2b, #602, decision
// 692).

package bridge

import (
	"testing"

	mattercore "thingfabric.dev/go-fabric/cluster/core"
	"thingfabric.dev/go-fabric/im"
	"thingfabric.dev/go-fabric/internal/bridgeseam"
	"thingfabric.dev/go-fabric/tlv"
)

// structDecoderW2B2b encodes fill into an anonymous structure and returns
// a decoder past its opener.
func structDecoderW2B2b(t *testing.T, fill func(enc *tlv.Encoder)) *tlv.Decoder {
	t.Helper()
	enc := tlv.NewEncoder()
	enc.StartStruct(tlv.AnonymousTag())
	fill(enc)
	if err := enc.EndContainer(); err != nil {
		t.Fatal(err)
	}
	buf, err := enc.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	dec := tlv.NewDecoder(buf)
	if _, err := dec.Next(); err != nil {
		t.Fatal(err)
	}
	return dec
}

// TestSingleUint16RequestReadsOnlyContextTagZeroW2B2b: a further field
// after GroupKeySetID, an unknown one a newer peer may send, does not
// replace it. Kills fields_reader_groupkeys.go:40 (&& → ||).
func TestSingleUint16RequestReadsOnlyContextTagZeroW2B2b(t *testing.T) {
	t.Parallel()
	dec := structDecoderW2B2b(t, func(enc *tlv.Encoder) {
		enc.PutUint16(tlv.ContextTag(0), 5)
		enc.PutUint16(tlv.ContextTag(1), 9)
	})
	v, err := decodeSingleUint16Request(dec, "KeySetRead")
	if err != nil {
		t.Fatal(err)
	}
	if v != 5 {
		t.Fatalf("decoded %d, want field 0's 5", v)
	}
}

// TestScenesRequestsKeepTheirFieldsW2B2b: a member container does not end
// the read of the fields after it, and a RecallScene TransitionTime is
// carried when it is a value. Kills fields_reader_scenes.go:41 (!= → ==)
// and :93 (drop !).
func TestScenesRequestsKeepTheirFieldsW2B2b(t *testing.T) {
	t.Parallel()
	dec := structDecoderW2B2b(t, func(enc *tlv.Encoder) {
		enc.StartList(tlv.ContextTag(0))
		enc.PutUint16(tlv.ContextTag(0), 1)
		if err := enc.EndContainer(); err != nil {
			t.Fatal(err)
		}
		enc.PutUint16(tlv.ContextTag(1), 7)
	})
	nodes, err := readScenesTree(dec)
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 2 {
		t.Fatalf("read %d fields, want 2: the member list ended the read", len(nodes))
	}
	if len(nodes[0].children) != 1 {
		t.Fatalf("the list holds %d members, want 1", len(nodes[0].children))
	}

	recall := structDecoderW2B2b(t, func(enc *tlv.Encoder) {
		enc.PutUint16(tlv.ContextTag(0), 1)    // GroupID
		enc.PutUint(tlv.ContextTag(1), 2)      // SceneID
		enc.PutUint32(tlv.ContextTag(2), 1500) // TransitionTime
	})
	path := im.ConcreteCommandPath{Endpoint: 1, Cluster: mattercore.ScenesManagementClusterID, Command: 0x05}
	fields, ok, err := scenesFieldsReader(path, recall)
	if err != nil || !ok {
		t.Fatalf("RecallScene: ok=%v err=%v", ok, err)
	}
	req, isRecall := fields.(mattercore.RecallSceneRequest)
	if !isRecall {
		t.Fatalf("decoded %T, want RecallSceneRequest", fields)
	}
	if req.TransitionTime == nil || *req.TransitionTime != 1500 {
		t.Fatalf("TransitionTime = %v, want 1500", req.TransitionTime)
	}
}

// TestAttributeFixedOfAnUnknownClusterW2B2b: a cluster with no definition
// has no fixed attribute. Kills element_qualities.go:41 (guard off).
func TestAttributeFixedOfAnUnknownClusterW2B2b(t *testing.T) {
	t.Parallel()
	if (specElementQualities{}).AttributeFixed(0xFFF1FC99, 0) {
		t.Fatal("an attribute of an unknown cluster is fixed")
	}
}

// TestCaseEstablishmentSeamRefusesANonAdapterW2B2b: the seam installs a
// completion only on a CASE adapter; anything else, a typed nil adapter
// included, is refused. Kills case_seam.go:16 (|| → &&).
func TestCaseEstablishmentSeamRefusesANonAdapterW2B2b(t *testing.T) {
	t.Parallel()
	fn := func(uint16, bool) bool { return true }
	if bridgeseam.SetCaseEstablishmentCompletion((*CaseAdapter)(nil), fn) {
		t.Error("the seam accepted a nil *CaseAdapter")
	}
	if bridgeseam.SetCaseEstablishmentCompletion("not an adapter", fn) {
		t.Error("the seam accepted a value that is no *CaseAdapter")
	}
}

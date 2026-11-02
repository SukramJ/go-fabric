// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of matter.js packages/node/test/node/client/ClientNodeSemanticTagTest.ts
// at 862ad75f: a semantic tag a peer reports without a label reaches the
// controller as carrying none — the optional nullable Label absent, not
// null. matter.js writes the tags on a device's Descriptor TagList; the Go
// device's Descriptor serves no TagList, so the port reports the tag list
// to the client image (Node.apply) as the device sends it, the label left
// out of the struct.
package client

import (
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/cluster/spec/descriptor"
	"github.com/SukramJ/go-fabric/tlv"
)

// tagList is the TagList value of tags, as a peer encodes it.
func tagList(t *testing.T, tags ...descriptor.Semtag) spec.Node {
	t.Helper()
	enc := tlv.NewEncoder()
	enc.StartArray(tlv.AnonymousTag())
	for _, tg := range tags {
		tg.EncodeTLV(enc, tlv.AnonymousTag())
	}
	enc.EndContainer()
	b, err := enc.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	n, err := decodeValue(b)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func TestClientNodeSemanticTagParity(t *testing.T) {
	t.Parallel()
	t.Run("reports a tag written without a label as carrying no label", func(t *testing.T) {
		t.Parallel()
		n, rec, _ := peerImage(t, &MemoryStore{})
		label := spec.ValueOf("initial")
		initial := descriptor.Semtag{MfgCode: spec.NullOf[uint16](), NamespaceId: 7, Tag: 1, Label: &label}
		unlabeled := descriptor.Semtag{MfgCode: spec.NullOf[uint16](), NamespaceId: 8, Tag: 2}
		report := func(version uint32, tags ...descriptor.Semtag) {
			b := &report{}
			b.attr(1, descriptor.ClusterID, spec.AttrFeatureMap, uintNode(uint64(descriptor.FeatureTagList)), version)
			b.attr(1, descriptor.ClusterID, spec.AttrAttributeList, listNode(0, 1, 2, 3, uint64(descriptor.AttrTagList), 65528, 65529, 65531, 65532, 65533), version)
			b.attr(1, descriptor.ClusterID, descriptor.AttrTagList, tagList(t, tags...), version)
			n.apply(wildcard, &b.r, true)
		}
		decoded := func() []descriptor.Semtag {
			v, err := n.Endpoint(1).Cluster(descriptor.ClusterID).Decoded(descriptor.AttrTagList)
			if err != nil {
				t.Fatal(err)
			}
			return v.(spec.List[descriptor.Semtag])
		}
		report(2, initial)
		if tags := decoded(); len(tags) != 1 {
			t.Fatalf("tags %+v, want one", tags)
		}
		rec.take()
		report(3, initial, unlabeled)
		if got := rec.take(); len(got) == 0 {
			t.Fatal("the tag list change was not announced")
		}
		tags := decoded()
		if len(tags) != 2 {
			t.Fatalf("tags %+v, want two", tags)
		}
		labeled, bare := tags[0], tags[1]
		if labeled.Label == nil || labeled.Label.Null || labeled.Label.Value != "initial" {
			t.Fatalf("labeled tag %+v", labeled)
		}
		if bare.NamespaceId != 8 || bare.Tag != 2 || bare.Label != nil {
			t.Fatalf("unlabeled tag %+v, want namespace 8, tag 2 and no label", bare)
		}
	})
}

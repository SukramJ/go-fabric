// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package groups

import (
	"context"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
)

// u8List is a list of uint8 values.
type u8List []uint8

func (l u8List) EncodeTLV(enc *tlv.Encoder, tag tlv.Tag) {
	enc.StartArray(tag)
	for _, v := range l {
		enc.PutUint(tlv.AnonymousTag(), uint64(v))
	}
	_ = enc.EndContainer()
}

// TestSenderWriteListChunked: a group write of a list value is the
// unicast write's list chunking in one message — REPLACE-ALL with the
// empty list, then one append per element (matter.js
// ClientGroupInteraction.write → ClientInteraction.write → Write(),
// Write.ts:149-171 at 0ceb202d); AccessControl's ACL is not chunked
// (:18-21, :151-156), nor a write with DisableListChunking.
func TestSenderWriteListChunked(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	m, st, tr := senderSetup(t)
	rx, _ := NewManager(st, nil)
	s, err := m.Sender(ctx, 1, 257)
	if err != nil {
		t.Fatal(err)
	}
	read := func(_ im.ConcreteAttributePath, el tlv.Element, dec *tlv.Decoder) (im.AttributeValue, error) {
		if el.IsContainer {
			n, err := spec.ReadContainer(dec, el)
			return im.AttributeValue{Value: n}, err
		}
		return im.AttributeValue{Value: el.Uint}, nil
	}
	write := func(path im.ConcreteAttributePath, opts RequestOptions) im.WriteRequest {
		t.Helper()
		if err := s.Write(ctx, path, u8List{3, 4}, opts); err != nil {
			t.Fatal(err)
		}
		payload, _ := decodeSent(t, rx, tr)
		w, err := im.UnmarshalWriteRequestTLV(tlv.NewDecoder(payload), read)
		if err != nil {
			t.Fatal(err)
		}
		return w
	}
	path := im.ConcreteAttributePath{Cluster: 0x0040, Attribute: 0x0000, HasCluster: true, HasAttribute: true}
	w := write(path, RequestOptions{})
	if len(w.Writes) != 3 {
		t.Fatalf("%d AttributeDataIBs, want 3 (replace-all + 2 appends)", len(w.Writes))
	}
	if n, ok := w.Writes[0].Value.Value.(spec.Node); !ok || len(n.Children) != 0 || w.Writes[0].Path.HasListIndex {
		t.Fatalf("first write %+v, want REPLACE-ALL with the empty list", w.Writes[0])
	}
	for i, want := range []uint64{3, 4} {
		a := w.Writes[i+1]
		if !a.Path.HasListIndex || !a.Path.ListAppend || a.Value.Value != want {
			t.Fatalf("append %d: %+v", i, a)
		}
	}
	acl := im.ConcreteAttributePath{Cluster: 0x001F, Attribute: 0x0000, HasCluster: true, HasAttribute: true}
	if w := write(acl, RequestOptions{}); len(w.Writes) != 1 {
		t.Fatalf("an ACL write is chunked: %d writes", len(w.Writes))
	}
	if w := write(path, RequestOptions{DisableListChunking: true}); len(w.Writes) != 1 {
		t.Fatalf("DisableListChunking: %d writes", len(w.Writes))
	}
}

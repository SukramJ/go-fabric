// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"errors"
	"testing"

	mattercore "github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/cluster/spec"
	userlabeldef "github.com/SukramJ/go-fabric/cluster/spec/userlabel"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
)

// TestAttributeValueReader_UserLabel pins the write-decode path for
// UserLabel.LabelList (0x0041/0x0000) through the generated decoder: a
// whole-list write and a list-append item decode into the list type the
// server stores, a label over "max 16" reaches the server as a value it
// answers CONSTRAINT_ERROR without failing the request, and the decoded
// list is accepted by the real server.
//
// Bite check: without the UserLabel cases the container falls through to
// primitiveAttributeValue, the value is nil and the server answers
// CONSTRAINT_ERROR for every write.
func TestAttributeValueReader_UserLabel(t *testing.T) {
	t.Parallel()
	path := im.ConcreteAttributePath{HasCluster: true, HasAttribute: true, Cluster: userlabeldef.ClusterID, Attribute: userlabeldef.AttrLabelList}
	encode := func(v spec.Encodable) *tlv.Decoder {
		enc := tlv.NewEncoder()
		v.EncodeTLV(enc, tlv.AnonymousTag())
		b, err := enc.Bytes()
		if err != nil {
			t.Fatal(err)
		}
		return tlv.NewDecoder(b)
	}
	srv, err := mattercore.NewUserLabel(mattercore.UserLabelConfig{})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	want := spec.List[userlabeldef.LabelStruct]{{Label: "room", Value: "hall"}, {Label: "floor", Value: "1"}}
	dec := encode(want)
	av, err := attributeValueReader(path, advanceToContent(t, dec), dec)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := av.Value.(spec.List[userlabeldef.LabelStruct])
	if !ok || len(got) != 2 || got[1] != want[1] {
		t.Fatalf("decoded %#v", av.Value)
	}
	if err := srv.MatterWrite(ctx, userlabeldef.AttrLabelList, av.Value); err != nil {
		t.Fatalf("server refused the decoded list: %v", err)
	}

	item := path
	item.ListAppend = true
	dec = encode(userlabeldef.LabelStruct{Label: "zone", Value: "a"})
	av, err = attributeValueReader(item, advanceToContent(t, dec), dec)
	if err != nil {
		t.Fatal(err)
	}
	if l, ok := av.Value.(spec.List[userlabeldef.LabelStruct]); !ok || len(l) != 1 || l[0].Label != "zone" {
		t.Fatalf("append item decoded %#v", av.Value)
	}

	for _, p := range []im.ConcreteAttributePath{path, item} {
		var v spec.Encodable = spec.List[userlabeldef.LabelStruct]{{Label: "seventeen-chars-x"}}
		if p.ListAppend {
			v = userlabeldef.LabelStruct{Label: "seventeen-chars-x"}
		}
		dec = encode(v)
		av, err = attributeValueReader(p, advanceToContent(t, dec), dec)
		if err != nil {
			t.Fatalf("append %v: an undecodable value failed the request: %v", p.ListAppend, err)
		}
		if p.ListAppend {
			continue // the dispatcher answers a non-list item CONSTRAINT_ERROR (appendListValue)
		}
		err = srv.MatterWrite(ctx, userlabeldef.AttrLabelList, av.Value)
		var sce im.StatusCodeError
		if !errors.As(err, &sce) || sce.MatterStatusCode() != im.StatusConstraintError {
			t.Errorf("label over max 16: %v, want CONSTRAINT_ERROR", err)
		}
	}
}

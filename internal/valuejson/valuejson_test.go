// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package valuejson_test

import (
	"math"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	basicinfo "github.com/SukramJ/go-fabric/cluster/spec/basicinformation"
	descdef "github.com/SukramJ/go-fabric/cluster/spec/descriptor"
	"github.com/SukramJ/go-fabric/internal/valuejson"
	"github.com/SukramJ/go-fabric/tlv"
)

func node(t *testing.T, put func(e *tlv.Encoder)) spec.Node {
	t.Helper()
	e := tlv.NewEncoder()
	put(e)
	b, err := e.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	n, err := spec.ReadElement(tlv.NewDecoder(b))
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func decode(t *testing.T, f *spec.Field, n spec.Node, byName bool) string {
	t.Helper()
	b, err := valuejson.Decode(f, n, byName)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// TestDecodeForms: a typed struct by name and by id, a list of them, the
// untyped fallback, the scalar kinds and null.
func TestDecodeForms(t *testing.T) {
	t.Parallel()
	label := spec.Nullable[string]{Value: "<L&R>"}
	tags := node(t, func(e *tlv.Encoder) {
		spec.List[descdef.Semtag]{{MfgCode: spec.Nullable[uint16]{Null: true}, NamespaceId: 7, Tag: 1, Label: &label}}.EncodeTLV(e, tlv.AnonymousTag())
	})
	f := valuejson.AttributeField(descdef.ClusterID, descdef.AttrTagList)
	if f == nil {
		t.Fatal("TagList has no type")
	}
	for _, c := range []struct {
		f      *spec.Field
		byName bool
		want   string
	}{
		{f, true, `[{"label":"<L&R>","mfgCode":null,"namespaceId":7,"tag":1}]`},
		{f, false, `[{"0":null,"1":7,"2":1,"3":"<L&R>"}]`},
		{nil, true, `[{"0":null,"1":7,"2":1,"3":"<L&R>"}]`},
	} {
		if got := decode(t, c.f, tags, c.byName); got != c.want {
			t.Errorf("got %s, want %s", got, c.want)
		}
	}
	scalars := node(t, func(e *tlv.Encoder) {
		e.StartArray(tlv.AnonymousTag())
		e.PutBool(tlv.AnonymousTag(), true)
		e.PutInt(tlv.AnonymousTag(), -3)
		e.PutUint64(tlv.AnonymousTag(), math.MaxUint64)
		e.PutFloat32(tlv.AnonymousTag(), 1.5)
		e.PutFloat64(tlv.AnonymousTag(), math.NaN())
		e.PutUTF8(tlv.AnonymousTag(), "x")
		e.PutOctets(tlv.AnonymousTag(), []byte{1, 2})
		e.PutNull(tlv.AnonymousTag())
		_ = e.EndContainer()
	})
	if got, want := decode(t, nil, scalars, true), `[true,-3,18446744073709551615,1.5,null,"x","AQI=",null]`; got != want {
		t.Errorf("scalars %s, want %s", got, want)
	}
	// A typed value of the wrong shape renders untyped.
	if got := decode(t, f, node(t, func(e *tlv.Encoder) { e.PutUTF8(tlv.AnonymousTag(), "x") }), true); got != `"x"` {
		t.Errorf("mismatched shape %s", got)
	}
	// A global attribute is typed for any cluster.
	if valuejson.AttributeField(0xFFF1FC00, 0xFFFC) == nil || valuejson.AttributeField(0xFFF1FC00, 0) != nil {
		t.Error("global or unknown attribute lookup")
	}
	if valuejson.EventField(basicinfo.ClusterID, basicinfo.EventStartUp) == nil || valuejson.EventField(0xFFF1FC00, 0) != nil {
		t.Error("event lookup")
	}
}

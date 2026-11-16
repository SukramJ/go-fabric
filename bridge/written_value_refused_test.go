// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"reflect"
	"testing"

	mattercore "github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/cluster/spec"
	lvldef "github.com/SukramJ/go-fabric/cluster/spec/levelcontrol"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
)

// writeValueOf decodes the attribute value build encodes, as the write
// path does.
func writeValueOf(t *testing.T, path im.ConcreteAttributePath, build func(enc *tlv.Encoder)) im.AttributeValue {
	t.Helper()
	enc := tlv.NewEncoder()
	build(enc)
	b, err := enc.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	dec := tlv.NewDecoder(b)
	el, err := dec.Next()
	if err != nil {
		t.Fatal(err)
	}
	av, err := attributeValueReader(path, el, dec)
	if err != nil {
		t.Fatalf("attributeValueReader: %v", err)
	}
	return av
}

func attrPath(cluster, attr uint32) im.ConcreteAttributePath {
	return im.ConcreteAttributePath{Endpoint: 1, Cluster: cluster, Attribute: attr, HasEndpoint: true, HasCluster: true, HasAttribute: true}
}

// TestContainerWrittenToAScalarIsRefused (R2-P6-wire-0442): a structure or
// list written to a scalar attribute is not a null: it decodes to a value
// the definition refuses, for a nullable attribute as for another.
func TestContainerWrittenToAScalarIsRefused(t *testing.T) {
	t.Parallel()
	for _, attr := range []uint32{lvldef.AttrOnLevel, lvldef.AttrOptions} {
		for name, build := range map[string]func(*tlv.Encoder){
			"struct": func(enc *tlv.Encoder) {
				enc.StartStruct(tlv.AnonymousTag())
				enc.PutUint(tlv.ContextTag(0), 1)
				_ = enc.EndContainer()
			},
			"array": func(enc *tlv.Encoder) {
				enc.StartArray(tlv.AnonymousTag())
				enc.PutUint(tlv.AnonymousTag(), 1)
				_ = enc.EndContainer()
			},
		} {
			av := writeValueOf(t, attrPath(lvldef.ClusterID, attr), build)
			if av.IsNull || av.Value == nil {
				t.Errorf("attr 0x%04X, %s: decoded to a null (%+v)", attr, name, av)
				continue
			}
			if _, undecoded := av.Value.(undecodedValue); !undecoded {
				t.Errorf("attr 0x%04X, %s: decoded to %T, want an undecoded value", attr, name, av.Value)
			}
			if err := spec.CheckWritten(lvldef.Definition, 0, attr, av.Value, nil); err == nil {
				t.Errorf("attr 0x%04X, %s: the definition accepts it", attr, name)
			}
		}
	}
	// A list-append item of a primitive list that is a container is
	// refused the same way, not appended as a nil element.
	p := attrPath(0xFFF1FC07, 0)
	p.ListAppend = true
	av := writeValueOf(t, p, func(enc *tlv.Encoder) {
		enc.StartStruct(tlv.AnonymousTag())
		_ = enc.EndContainer()
	})
	if _, undecoded := av.Value.(undecodedValue); !undecoded {
		t.Errorf("append item: decoded to %#v, want an undecoded value", av.Value)
	}
}

// TestAccessControlWritesDecodeThroughTheirDefinitions (R2-P6-acl-0441,
// R2-P6-wire-1703): AccessControl.ACL, .Extension and
// GroupKeyManagement.GroupKeyMap decode through their generated
// definitions: a well-formed list decodes to the cluster's types as
// before; an over-wide integer, a mistyped field and a list element that
// is not a struct are refused whole — never cut to width, read as 0 or
// dropped.
func TestAccessControlWritesDecodeThroughTheirDefinitions(t *testing.T) {
	t.Parallel()
	acl := attrPath(0x001F, 0x0000)
	entry := func(enc *tlv.Encoder, privilege uint64, subject func(*tlv.Encoder), target func(*tlv.Encoder)) {
		enc.StartStruct(tlv.AnonymousTag())
		enc.PutUint(tlv.ContextTag(1), privilege)
		enc.PutUint(tlv.ContextTag(2), 2)
		enc.StartArray(tlv.ContextTag(3))
		subject(enc)
		_ = enc.EndContainer()
		enc.StartArray(tlv.ContextTag(4))
		target(enc)
		_ = enc.EndContainer()
		enc.PutUint(tlv.ContextTag(254), 3)
		_ = enc.EndContainer()
	}
	okSubject := func(enc *tlv.Encoder) { enc.PutUint(tlv.AnonymousTag(), 0x1122) }
	okTarget := func(enc *tlv.Encoder) {
		enc.StartStruct(tlv.AnonymousTag())
		enc.PutUint(tlv.ContextTag(0), 6)
		enc.PutNull(tlv.ContextTag(1))
		enc.PutNull(tlv.ContextTag(2))
		_ = enc.EndContainer()
	}
	list := func(entries ...func(*tlv.Encoder)) func(*tlv.Encoder) {
		return func(enc *tlv.Encoder) {
			enc.StartArray(tlv.AnonymousTag())
			for _, e := range entries {
				e(enc)
			}
			_ = enc.EndContainer()
		}
	}

	good := writeValueOf(t, acl, list(func(enc *tlv.Encoder) { entry(enc, 5, okSubject, okTarget) }))
	cl := uint32(6)
	want := []mattercore.AccessControlEntryStruct{{
		Privilege: 5, AuthMode: 2, Subjects: []uint64{0x1122},
		Targets: []mattercore.ACLTargetStruct{{Cluster: &cl}}, FabricIndex: 1,
	}}
	if !reflect.DeepEqual(good.Value, want) {
		t.Errorf("a well-formed ACL decoded to %#v, want %#v", good.Value, want)
	}

	refused := map[string]func(*tlv.Encoder){
		"privilege 0x0105": list(func(enc *tlv.Encoder) { entry(enc, 0x0105, okSubject, okTarget) }),
		"a string subject": list(func(enc *tlv.Encoder) {
			entry(enc, 5, func(enc *tlv.Encoder) { enc.PutUTF8(tlv.AnonymousTag(), "x") }, okTarget)
		}),
		"a string target cluster": list(func(enc *tlv.Encoder) {
			entry(enc, 5, okSubject, func(enc *tlv.Encoder) {
				enc.StartStruct(tlv.AnonymousTag())
				enc.PutUTF8(tlv.ContextTag(0), "x")
				enc.PutNull(tlv.ContextTag(1))
				enc.PutNull(tlv.ContextTag(2))
				_ = enc.EndContainer()
			})
		}),
		"an element that is not a struct": list(
			func(enc *tlv.Encoder) { entry(enc, 5, okSubject, okTarget) },
			func(enc *tlv.Encoder) { enc.PutUint(tlv.AnonymousTag(), 7) },
		),
	}
	for name, build := range refused {
		av := writeValueOf(t, acl, build)
		if _, undecoded := av.Value.(undecodedValue); !undecoded {
			t.Errorf("ACL with %s: decoded to %#v, want it refused", name, av.Value)
		}
	}

	ext := writeValueOf(t, attrPath(0x001F, 0x0001), list(func(enc *tlv.Encoder) {
		enc.StartStruct(tlv.AnonymousTag())
		enc.PutOctets(tlv.ContextTag(1), []byte{1, 2})
		_ = enc.EndContainer()
	}, func(enc *tlv.Encoder) { enc.PutBool(tlv.AnonymousTag(), true) }))
	if _, undecoded := ext.Value.(undecodedValue); !undecoded {
		t.Errorf("Extension with a non-struct element: decoded to %#v", ext.Value)
	}
	gkm := writeValueOf(t, attrPath(0x003F, 0x0000), list(func(enc *tlv.Encoder) {
		enc.StartStruct(tlv.AnonymousTag())
		enc.PutUint(tlv.ContextTag(1), 0x10001)
		enc.PutUint(tlv.ContextTag(2), 1)
		_ = enc.EndContainer()
	}))
	if _, undecoded := gkm.Value.(undecodedValue); !undecoded {
		t.Errorf("GroupKeyMap with GroupId 0x10001: decoded to %#v", gkm.Value)
	}
	gkmOK := writeValueOf(t, attrPath(0x003F, 0x0000), list(func(enc *tlv.Encoder) {
		enc.StartStruct(tlv.AnonymousTag())
		enc.PutUint(tlv.ContextTag(1), 0x0101)
		enc.PutUint(tlv.ContextTag(2), 7)
		_ = enc.EndContainer()
	}))
	if want := []mattercore.GroupKeyMapStruct{{GroupID: 0x0101, GroupKeySetID: 7, FabricIndex: 1}}; !reflect.DeepEqual(gkmOK.Value, want) {
		t.Errorf("a well-formed GroupKeyMap decoded to %#v, want %#v", gkmOK.Value, want)
	}
}

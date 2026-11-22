// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"reflect"
	"strings"
	"testing"

	"thingfabric.dev/go-fabric/im"
	"thingfabric.dev/go-fabric/tlv"
)

// TestHandReadRequestIgnoresAnUnknownContainerField (R2-P6-wire-0440): a
// hand-read request with a context-tagged struct field its command does
// not define reads as the same request without it — the reader reads the
// checked tree's defined fields, never the unknown member's inner tags as
// the command's own.
func TestHandReadRequestIgnoresAnUnknownContainerField(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("x", 64)
	cases := []struct {
		name      string
		path      im.ConcreteCommandPath
		fields    func(enc *tlv.Encoder)
		unknownAt uint8
		inner     func(enc *tlv.Encoder)
	}{
		{
			"UpdateFabricLabel",
			im.ConcreteCommandPath{Cluster: 0x003E, Command: 0x09, HasCluster: true, HasCommand: true},
			func(enc *tlv.Encoder) { enc.PutUTF8(tlv.ContextTag(0), "ok") },
			5, func(enc *tlv.Encoder) { enc.PutUTF8(tlv.ContextTag(0), long) },
		},
		{
			"ArmFailSafe",
			im.ConcreteCommandPath{Cluster: 0x0030, Command: 0x00, HasCluster: true, HasCommand: true},
			func(enc *tlv.Encoder) {
				enc.PutUint(tlv.ContextTag(0), 60)
				enc.PutUint(tlv.ContextTag(1), 1)
			},
			7, func(enc *tlv.Encoder) {
				enc.PutUint(tlv.ContextTag(0), 900)
				enc.PutUint(tlv.ContextTag(1), 99)
			},
		},
		{
			"RemoveFabric",
			im.ConcreteCommandPath{Cluster: 0x003E, Command: 0x0A, HasCluster: true, HasCommand: true},
			func(enc *tlv.Encoder) { enc.PutUint(tlv.ContextTag(0), 1) },
			9, func(enc *tlv.Encoder) { enc.PutUint(tlv.ContextTag(0), 0) },
		},
	}
	read := func(t *testing.T, path im.ConcreteCommandPath, build func(*tlv.Encoder)) any {
		t.Helper()
		enc := tlv.NewEncoder()
		enc.StartStruct(tlv.AnonymousTag())
		build(enc)
		_ = enc.EndContainer()
		b, err := enc.Bytes()
		if err != nil {
			t.Fatal(err)
		}
		dec := tlv.NewDecoder(b)
		el, err := dec.Next()
		if err != nil {
			t.Fatal(err)
		}
		f, err := commandFieldsReader(path, dec, el)
		if err != nil {
			t.Fatalf("%v", err)
		}
		return f
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			plain := read(t, tc.path, tc.fields)
			withUnknown := read(t, tc.path, func(enc *tlv.Encoder) {
				tc.fields(enc)
				enc.StartStruct(tlv.ContextTag(tc.unknownAt))
				tc.inner(enc)
				_ = enc.EndContainer()
			})
			if !reflect.DeepEqual(plain, withUnknown) {
				t.Errorf("with an unknown struct field the request reads %+v, without it %+v", withUnknown, plain)
			}
		})
	}
}

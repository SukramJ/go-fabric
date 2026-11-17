// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package im

import (
	"testing"

	"github.com/SukramJ/go-fabric/tlv"
)

// TestPathListsRefuseANonContextMember is the standing guard of the path
// lists' half of R2-P6-wire-0504 (review II; Fable's read of sub round 1,
// H-5): AttributePathIB, EventPathIB, CommandPathIB and ClusterPathIB are
// tagged lists, and a member with a tag other than a context tag refuses
// the path, as it refuses a structure ([contextMember]); a plain path
// still decodes.
func TestPathListsRefuseANonContextMember(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		fields func(enc *tlv.Encoder)
		decode func(*tlv.Decoder) error
	}{
		{"AttributePathIB", func(enc *tlv.Encoder) {
			enc.PutUint(tlv.ContextTag(tagAttrPathEndpoint), 1)
			enc.PutUint(tlv.ContextTag(tagAttrPathCluster), 6)
			enc.PutUint(tlv.ContextTag(tagAttrPathAttribute), 0)
		}, func(d *tlv.Decoder) error { _, err := readAttributePathFields(d); return err }},
		{"EventPathIB", func(enc *tlv.Encoder) {
			enc.PutUint(tlv.ContextTag(tagEventPathEndpoint), 1)
			enc.PutUint(tlv.ContextTag(tagEventPathCluster), 6)
			enc.PutUint(tlv.ContextTag(tagEventPathEvent), 0)
		}, func(d *tlv.Decoder) error { _, err := readEventPathFields(d); return err }},
		{"CommandPathIB", func(enc *tlv.Encoder) {
			enc.PutUint(tlv.ContextTag(tagCmdPathEndpoint), 1)
			enc.PutUint(tlv.ContextTag(tagCmdPathCluster), 6)
			enc.PutUint(tlv.ContextTag(tagCmdPathCommand), 0)
		}, func(d *tlv.Decoder) error { _, err := readCommandPathFields(d); return err }},
		{"ClusterPathIB", func(enc *tlv.Encoder) {
			enc.PutUint(tlv.ContextTag(1), 1)
			enc.PutUint(tlv.ContextTag(2), 6)
		}, func(d *tlv.Decoder) error { _, _, _, _, err := readClusterPathFields(d); return err }},
	}
	foreign := []tlv.Tag{tlv.AnonymousTag(), tlv.CommonTag(1), tlv.FullyQualifiedTag(0xFFF1, 1, 1)}
	for _, tc := range cases {
		wire := func(member *tlv.Tag) *tlv.Decoder {
			enc := tlv.NewEncoder()
			enc.StartList(tlv.AnonymousTag())
			if member != nil {
				enc.PutUint(*member, 7)
			}
			tc.fields(enc)
			_ = enc.EndContainer()
			b, err := enc.Bytes()
			if err != nil {
				t.Fatal(err)
			}
			d := tlv.NewDecoder(b)
			if _, err := d.Next(); err != nil {
				t.Fatal(err)
			}
			return d
		}
		if err := tc.decode(wire(nil)); err != nil {
			t.Fatalf("%s: the plain path is refused: %v", tc.name, err)
		}
		for _, tag := range foreign {
			if err := tc.decode(wire(&tag)); err == nil {
				t.Errorf("%s: a member of tag kind %d was skipped, want the path refused", tc.name, tag.Kind)
			}
		}
	}
}

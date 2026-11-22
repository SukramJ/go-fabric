// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package spectest

import (
	"bytes"
	"fmt"
	"slices"
	"testing"

	"thingfabric.dev/go-fabric/cluster/spec"
	"thingfabric.dev/go-fabric/tlv"
)

// fabricIndexTag is the context tag of a fabric-scoped struct's
// FabricIndex field (Core §7.13.6, the generated field ID 0xFE).
const fabricIndexTag = 0xFE

// FabricSensitiveList is a list attribute of a cluster definition with
// fabric-sensitive data: a fabric-scoped list whose entry struct has
// fabric-sensitive fields (access "S"), or a list whose own access is
// fabric-sensitive.
type FabricSensitiveList struct {
	Cluster   *spec.Cluster
	Attribute *spec.Attribute
	// Sensitive are the entry struct's fabric-sensitive fields, in
	// definition order; none for a fabric-sensitive list whose entry
	// struct marks none.
	Sensitive []spec.Field
}

// String names the attribute as "<cluster>.<attribute> (0xCCCC/0xAAAA)".
func (l FabricSensitiveList) String() string {
	return fmt.Sprintf("%s.%s (0x%04X/0x%04X)", l.Cluster.Name, l.Attribute.Name, l.Cluster.ID, l.Attribute.ID)
}

// FabricSensitiveLists returns def's fabric-scoped list attributes ("F" or
// "S") whose entry struct carries a fabric-sensitive field, and its list
// attributes whose own access is fabric-sensitive ("S"), in definition
// order — the attributes whose entries matter.js serves another fabric
// without those fields, or not at all.
func FabricSensitiveLists(def *spec.Cluster) []FabricSensitiveList {
	var out []FabricSensitiveList
	for i := range def.Attributes {
		a := &def.Attributes[i]
		if a.Access.Fabric == "" || a.Type.Kind != spec.KindList || a.Type.Entry == nil {
			continue
		}
		st := a.Type.Entry.Type.Struct
		if a.Type.Entry.Type.Kind != spec.KindStruct || st == nil {
			continue
		}
		var sensitive []spec.Field
		for j := range st.Fields {
			if st.Fields[j].FabricSensitive {
				sensitive = append(sensitive, st.Fields[j])
			}
		}
		if len(sensitive) > 0 || a.Access.Fabric == "S" {
			out = append(out, FabricSensitiveList{Cluster: def, Attribute: a, Sensitive: sensitive})
		}
	}
	return out
}

// CheckWithheld holds remote — the wire bytes of an unfiltered read of l
// by a session of fabric reader, 0 for a session without a fabric — to
// own, the wire bytes of the fabric-filtered read of l by fabric owner
// (owner's entries whole), as matter.js serves them at db021da0:
//
//   - a fabric-sensitive list ("S") lists for a remote read only the
//     reader's entries, a session without a fabric none
//     (packages/node/src/behavior/state/managed/values/ListManager.ts:610-616
//     createProxy, `config.fabricSensitive && !session.command`);
//   - a fabric-scoped list ("F") lists every fabric's entries; an entry of
//     another fabric than the reader — every entry, for a session without
//     a fabric — carries its other fields and FabricIndex and none of its
//     fabric-sensitive fields: an entry's location carries its FabricIndex
//     as the owning fabric (StructManager.ts:110-113), a field getter
//     answers undefined where mayRead refuses (StructManager.ts:306 for a
//     primitive field, :376-378 for a managed one), mayRead of a
//     fabric-sensitive field refuses another owning fabric and every
//     session without a fabric
//     (packages/protocol/src/action/server/AccessControl.ts:314, :342-356),
//     and the encoder leaves an undefined field out of an unfiltered read
//     (packages/types/src/tlv/TlvObject.ts:128-134).
//
// owner must differ from reader. Every entry of remote that is not the
// reader's carries no fabric-sensitive field — and for an "S" list is not
// there at all, whichever fabric owns it — and owner's entries are
// own's, in order, without those fields ("F") or absent ("S"). It reports
// each difference with tb.Errorf and returns the number of owner's
// entries: the entries the read checked. A caller that finds it 0 has not
// exercised the attribute.
func CheckWithheld(tb testing.TB, l FabricSensitiveList, own, remote []byte, owner, reader uint8) (checked int) {
	tb.Helper()
	if owner == 0 || owner == reader {
		tb.Fatalf("%s: owner %d and reader %d: the owner is a fabric other than the reader's", l, owner, reader)
	}
	ownEntries, err := listEntries(own)
	if err != nil {
		tb.Errorf("%s: fabric-filtered read by fabric %d: %v", l, owner, err)
		return 0
	}
	remoteEntries, err := listEntries(remote)
	if err != nil {
		tb.Errorf("%s: unfiltered read by fabric %d: %v", l, reader, err)
		return 0
	}
	var want []spec.Node
	for _, e := range ownEntries {
		if f, ok := entryFabric(e); !ok || f != owner {
			tb.Errorf("%s: fabric-filtered read by fabric %d lists an entry of fabric %d (%v): % X", l, owner, f, ok, encodeNode(e))
			return 0
		}
		if l.Attribute.Access.Fabric != "S" {
			want = append(want, withoutFields(e, l.Sensitive))
		}
	}
	var got []spec.Node
	for i, e := range remoteEntries {
		f, ok := entryFabric(e)
		if !ok {
			tb.Errorf("%s: unfiltered read by fabric %d, entry %d carries no FabricIndex: % X", l, reader, i, encodeNode(e))
			continue
		}
		if reader != 0 && f == reader {
			continue
		}
		if l.Attribute.Access.Fabric == "S" && f != owner {
			// A fabric-sensitive list lists the reader's entries alone,
			// whether or not the entry carries a sensitive field (the
			// owner's are counted below).
			tb.Errorf("%s: unfiltered read by fabric %d, entry %d of fabric %d: a fabric-sensitive list lists only the reader's entries: % X", l, reader, i, f, encodeNode(e))
			continue
		}
		if leaked := sensitiveIn(e, l.Sensitive); len(leaked) > 0 {
			tb.Errorf("%s: unfiltered read by fabric %d, entry %d of fabric %d: fabric-sensitive fields served %q: % X", l, reader, i, f, leaked, encodeNode(e))
		}
		if f == owner {
			got = append(got, e)
		}
	}
	if len(got) != len(want) {
		tb.Errorf("%s: unfiltered read by fabric %d lists %d entries of fabric %d, matter.js %d\n got % X\n own % X", l, reader, len(got), owner, len(want), remote, own)
		return len(ownEntries)
	}
	for i := range want {
		if g, w := encodeNode(got[i]), encodeNode(want[i]); !bytes.Equal(g, w) {
			tb.Errorf("%s: unfiltered read by fabric %d, entry %d of fabric %d\n got % X\nwant % X", l, reader, i, owner, g, w)
		}
	}
	return len(ownEntries)
}

// sensitiveIn names the fabric-sensitive fields e carries.
func sensitiveIn(e spec.Node, fields []spec.Field) []string {
	var out []string
	for _, c := range e.Children {
		if c.El.Tag.Kind != tlv.TagKindContext {
			continue
		}
		if j := slices.IndexFunc(fields, func(f spec.Field) bool { return f.ID == c.El.Tag.Number }); j >= 0 {
			out = append(out, fields[j].Name)
		}
	}
	return out
}

// listEntries decodes a list attribute's value into its entries.
func listEntries(b []byte) ([]spec.Node, error) {
	n, err := spec.ReadElement(tlv.NewDecoder(b))
	if err != nil {
		return nil, fmt.Errorf("spectest: decode % X: %w", b, err)
	}
	if !n.El.IsContainer || n.El.Type != tlv.TypeArray {
		return nil, fmt.Errorf("spectest: not a list: % X", b)
	}
	return n.Children, nil
}

// entryFabric is an entry's FabricIndex.
func entryFabric(e spec.Node) (uint8, bool) {
	for _, c := range e.Children {
		if c.El.Tag.Kind == tlv.TagKindContext && c.El.Tag.Number == fabricIndexTag {
			return uint8(c.El.Uint), true //nolint:gosec // G115: a FabricIndex is a uint8 on the wire (fabric-idx)
		}
	}
	return 0, false
}

// withoutFields is e without the fields named.
func withoutFields(e spec.Node, fields []spec.Field) spec.Node {
	out := spec.Node{El: e.El}
	for _, c := range e.Children {
		if c.El.Tag.Kind == tlv.TagKindContext && slices.ContainsFunc(fields, func(f spec.Field) bool { return f.ID == c.El.Tag.Number }) {
			continue
		}
		out.Children = append(out.Children, c)
	}
	return out
}

// encodeNode is n's wire bytes under an anonymous tag.
func encodeNode(n spec.Node) []byte {
	enc := tlv.NewEncoder()
	n.EncodeTLV(enc, tlv.AnonymousTag())
	b, _ := enc.Bytes()
	return b
}

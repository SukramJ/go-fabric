// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package spectest_test

import (
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	icddef "github.com/SukramJ/go-fabric/cluster/spec/icdmanagement"
	"github.com/SukramJ/go-fabric/cluster/spec/spectest"
	"github.com/SukramJ/go-fabric/tlv"
)

// entry is a struct of context-tagged unsigned fields, in order.
type entry [][2]uint64

// list encodes entries as a list attribute value.
func list(t *testing.T, entries ...entry) []byte {
	t.Helper()
	enc := tlv.NewEncoder()
	enc.StartArray(tlv.AnonymousTag())
	for _, e := range entries {
		enc.StartStruct(tlv.AnonymousTag())
		for _, f := range e {
			enc.PutUint(tlv.ContextTag(uint8(f[0])), f[1]) //nolint:gosec // G115: test tags are below 256
		}
		_ = enc.EndContainer()
	}
	_ = enc.EndContainer()
	b, err := enc.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestFabricSensitiveListsFindsTheMarkedEntryFields(t *testing.T) {
	t.Parallel()
	got := spectest.FabricSensitiveLists(icddef.Definition)
	if len(got) != 1 || got[0].Attribute.ID != icddef.AttrRegisteredClients {
		t.Fatalf("IcdManagement: %v", got)
	}
	names := make([]string, 0, len(got[0].Sensitive))
	for _, f := range got[0].Sensitive {
		names = append(names, f.Name)
	}
	if len(names) != 3 || names[0] != "CheckInNodeId" || names[1] != "MonitoredSubject" || names[2] != "ClientType" {
		t.Errorf("sensitive fields %q", names)
	}
	if s := got[0].String(); s != "IcdManagement.RegisteredClients (0x0046/0x0003)" {
		t.Errorf("String() = %q", s)
	}
	// A struct attribute, a list of scalars and a list without "S" fields
	// are none.
	def := &spec.Cluster{Name: "X", Attributes: []spec.Attribute{
		{ID: 1, Access: spec.Access{Fabric: "F"}, Type: spec.Type{Kind: spec.KindStruct}},
		{ID: 2, Access: spec.Access{Fabric: "F"}, Type: spec.Type{Kind: spec.KindList, Entry: &spec.Field{Type: spec.Type{Kind: spec.KindUint}}}},
		{ID: 3, Access: spec.Access{Fabric: "F"}, Type: spec.Type{Kind: spec.KindList, Entry: &spec.Field{Type: spec.Type{Kind: spec.KindStruct, Struct: &spec.Struct{Fields: []spec.Field{{ID: 254}}}}}}},
		{ID: 4, Type: spec.Type{Kind: spec.KindList, Entry: &spec.Field{Type: spec.Type{Kind: spec.KindStruct, Struct: icddef.MonitoringRegistrationStructDef}}}},
	}}
	if got := spectest.FabricSensitiveLists(def); len(got) != 0 {
		t.Errorf("unmarked lists found: %v", got)
	}
}

func TestCheckWithheld(t *testing.T) {
	t.Parallel()
	icd := spectest.FabricSensitiveLists(icddef.Definition)[0]
	// No list of the model is fabric-sensitive ("S") with an entry struct
	// that has "S" fields; this one is RegisteredClients made so.
	sensitive := spectest.FabricSensitiveLists(&spec.Cluster{Name: "X", Attributes: []spec.Attribute{{
		ID: 9, Name: "Y", Access: spec.Access{RW: "R", Fabric: "S"},
		Type: spec.Type{Kind: spec.KindList, Entry: &spec.Field{Type: spec.Type{Kind: spec.KindStruct, Struct: icddef.MonitoringRegistrationStructDef}}},
	}}})
	if len(sensitive) != 1 {
		t.Fatalf("sensitive list: %v", sensitive)
	}
	own := list(t, entry{{1, 7}, {2, 8}, {4, 1}, {254, 1}})
	redacted := entry{{254, 1}}
	cases := []struct {
		name   string
		l      spectest.FabricSensitiveList
		own    []byte
		remote []byte
		reader uint8
		report string // "" for none
		n      int
	}{
		{"withheld for another fabric", icd, own, list(t, redacted, entry{{1, 9}, {2, 9}, {4, 0}, {254, 2}}), 2, "", 1},
		{"withheld for a session without a fabric", icd, own, list(t, redacted, entry{{254, 2}}), 0, "", 1},
		{"served", icd, own, list(t, entry{{1, 7}, {254, 1}}), 2, "fabric-sensitive fields served [\"CheckInNodeId\"]", 1},
		{"served to a session without a fabric", icd, own, list(t, redacted, entry{{2, 9}, {254, 2}}), 0, "fabric-sensitive fields served [\"MonitoredSubject\"]", 1},
		{"left out", icd, own, list(t), 2, "lists 0 entries of fabric 1, matter.js 1", 1},
		{"another field differs", icd, list(t, entry{{3, 5}, {254, 1}}), list(t, entry{{254, 1}}), 2, "entry 0 of fabric 1", 1},
		{"no entry", icd, list(t), list(t), 2, "", 0},
		{"sensitive list leaves the owner out", sensitive[0], list(t, entry{{1, 1}, {254, 1}}), list(t), 2, "", 1},
		{"sensitive list lists the owner", sensitive[0], list(t, entry{{1, 1}, {254, 1}}), list(t, entry{{254, 1}}), 2, "lists 1 entries of fabric 1, matter.js 0", 1},
		// A fabric-sensitive list lists the reader's entries alone: a third
		// fabric's is reported though it carries no sensitive field
		// (review II, R2-P6-test-1364).
		{"sensitive list lists a third fabric", sensitive[0], list(t, entry{{1, 1}, {254, 1}}), list(t, entry{{254, 3}}), 2, "entry 0 of fabric 3: a fabric-sensitive list", 1},
		{"sensitive list lists an entry to a session without a fabric", sensitive[0], list(t, entry{{1, 1}, {254, 1}}), list(t, entry{{254, 3}}), 0, "entry 0 of fabric 3: a fabric-sensitive list", 1},
		{"sensitive list lists the reader's own", sensitive[0], list(t, entry{{1, 1}, {254, 1}}), list(t, entry{{1, 4}, {254, 2}}), 2, "", 1},
		{"entry without FabricIndex", icd, own, list(t, entry{{4, 1}}), 2, "carries no FabricIndex", 1},
		{"own read of another fabric", icd, list(t, entry{{254, 2}}), list(t), 2, "lists an entry of fabric 2", 0},
		{"own read not a list", icd, []byte{0x04, 0x00}, list(t), 2, "not a list", 0},
		{"remote read not TLV", icd, own, []byte{0x16}, 2, "decode", 0},
		{"owner is the reader", icd, own, own, 1, "the owner is a fabric other than the reader's", 0},
	}
	for _, c := range cases {
		var n int
		errs := run(t, func(tb *recorder) { n = spectest.CheckWithheld(tb, c.l, c.own, c.remote, 1, c.reader) })
		if c.report == "" {
			if len(errs) != 0 {
				t.Errorf("%s: reported %q", c.name, errs)
			}
		} else {
			wantReport(t, c.name, errs, c.report)
		}
		if n != c.n {
			t.Errorf("%s: %d entries checked, want %d", c.name, n, c.n)
		}
	}
}

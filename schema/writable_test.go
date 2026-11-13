// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package schema

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"

	matterparity "github.com/SukramJ/go-fabric/parity"
)

// snapshotAttribute is one attribute entry in the matter.js HEAD schema
// snapshot: its id, the access string the cluster states for it (empty for
// an attribute a derived cluster inherits) and its effective access, which
// matter.js resolves for every attribute, inherited ones included.
type snapshotAttribute struct {
	ID        uint32 `json:"id"`
	Name      string `json:"name"`
	Access    string `json:"access"`
	Effective *struct {
		Access struct {
			RW        string `json:"rw"`
			WritePriv string `json:"writePriv"`
		} `json:"access"`
	} `json:"effective"`
}

type snapshotCluster struct {
	ID         uint32              `json:"id"`
	Name       string              `json:"name"`
	Attributes []snapshotAttribute `json:"attributes"`
}

type snapshotSchema struct {
	Clusters []snapshotCluster `json:"clusters"`
}

func loadWritableSnapshot(t *testing.T) *snapshotSchema {
	t.Helper()
	var s snapshotSchema
	if err := json.Unmarshal(matterparity.SchemaJSON(), &s); err != nil {
		t.Fatalf("unmarshal matter schema snapshot: %v", err)
	}
	if len(s.Clusters) == 0 {
		t.Fatal("matter schema snapshot has no clusters")
	}
	return &s
}

// effectiveWritable mirrors matter.js Access.writable
// (packages/model/src/aspects/Access.ts:44-46 at db021da0,
// `!!this.rw && this.rw !== Access.Rw.Read`) on an attribute's effective
// rw: "R" or none is read-only; "RW" and the optional-write "R[W]" are
// writable.
func effectiveWritable(a snapshotAttribute) bool {
	return a.Effective != nil && a.Effective.Access.RW != "" && a.Effective.Access.RW != "R"
}

// globalAttribute reports an id in the global attribute range
// (GeneratedCommandList 0xFFF8 … ClusterRevision 0xFFFD). The snapshot lists
// FeatureMap and ClusterRevision as attributes of a cluster; the write gate
// answers the range itself (endpoint/dispatcher.go isGlobalAttribute), so
// readOnlyAttributes leaves it out.
func globalAttribute(id uint32) bool { return id >= 0xFFF8 && id <= 0xFFFD }

// readOnlyDrift compares table, cluster by cluster, with the read-only
// attributes of the snapshot clusters — every attribute whose effective
// access is not writable, the global range excepted — and returns one line
// per difference: a table cluster the snapshot does not have, a read-only
// attribute the table lacks, a table entry the snapshot marks writable or
// does not list. The effective access is each attribute's own (review II,
// W3-clugen-05): an attribute a derived cluster inherits states no access
// string, and a comparison by that string skipped it, so a read-only
// inherited attribute could be missing from the table unnoticed.
func readOnlyDrift(clusters []snapshotCluster, table map[uint32]map[uint32]struct{}) []string {
	byID := make(map[uint32]snapshotCluster, len(clusters))
	for _, c := range clusters {
		byID[c.ID] = c
	}
	var drift []string
	for clusterID, tableSet := range table {
		sc, ok := byID[clusterID]
		if !ok {
			drift = append(drift, fmt.Sprintf("readOnlyAttributes has cluster 0x%04X but the snapshot does not — stale table entry", clusterID))
			continue
		}
		want := make(map[uint32]struct{})
		for _, a := range sc.Attributes {
			if a.Effective == nil {
				drift = append(drift, fmt.Sprintf("cluster 0x%04X (%s): attr 0x%04X has no effective layer in the snapshot", clusterID, sc.Name, a.ID))
				continue
			}
			if !globalAttribute(a.ID) && !effectiveWritable(a) {
				want[a.ID] = struct{}{}
			}
		}
		for id := range want {
			if _, present := tableSet[id]; !present {
				drift = append(drift, fmt.Sprintf("cluster 0x%04X (%s): attr 0x%04X is read-only in matter.js but missing from the table", clusterID, sc.Name, id))
			}
		}
		for id := range tableSet {
			if _, present := want[id]; !present {
				drift = append(drift, fmt.Sprintf("cluster 0x%04X (%s): attr 0x%04X is in the read-only table but matter.js marks it writable/unknown", clusterID, sc.Name, id))
			}
		}
	}
	slices.Sort(drift)
	return drift
}

// TestReadOnlyAttributeParity pins readOnlyAttributes (via AttributeWritable)
// against the committed matter.js HEAD schema snapshot. For every cluster in
// the table it derives the read-only attribute set straight from the
// snapshot's per-attribute effective access and asserts an EXACT match — no
// missing read-only attribute, no stray/writable attribute — so the
// hand-maintained table cannot drift from matter.js when the snapshot is
// regenerated.
func TestReadOnlyAttributeParity(t *testing.T) {
	t.Parallel()

	snap := loadWritableSnapshot(t)
	for _, d := range readOnlyDrift(snap.Clusters, readOnlyAttributes) {
		t.Error(d)
	}

	// Cross-check the public AttributeWritable verdict for every attribute
	// of the table's clusters.
	for _, sc := range snap.Clusters {
		if _, ok := readOnlyAttributes[sc.ID]; !ok {
			continue
		}
		for _, a := range sc.Attributes {
			if globalAttribute(a.ID) {
				continue
			}
			writable, known := AttributeWritable(sc.ID, a.ID)
			if effectiveWritable(a) {
				if !writable {
					t.Errorf("AttributeWritable(0x%04X, 0x%04X) writable=false, want true (access %q)",
						sc.ID, a.ID, a.Access)
				}
			} else if !known || writable {
				t.Errorf("AttributeWritable(0x%04X, 0x%04X) = (writable %v, known %v), want (false, true) (access %q)",
					sc.ID, a.ID, writable, known, a.Access)
			}
		}
	}
}

// TestReadOnlyDriftSeesAnInheritedAttribute holds the comparison itself: a
// read-only attribute the snapshot lists without an access string — as a
// derived cluster lists what it inherits — is one the table must carry, a
// global one is not, and a writable or unlisted table entry is reported.
func TestReadOnlyDriftSeesAnInheritedAttribute(t *testing.T) {
	t.Parallel()
	attr := func(id uint32, access, rw string) snapshotAttribute {
		a := snapshotAttribute{ID: id, Access: access}
		a.Effective = &struct {
			Access struct {
				RW        string `json:"rw"`
				WritePriv string `json:"writePriv"`
			} `json:"access"`
		}{}
		a.Effective.Access.RW = rw
		return a
	}
	clusters := []snapshotCluster{{ID: 0x1234, Name: "Derived", Attributes: []snapshotAttribute{
		attr(0x0000, "R V", "R"),
		attr(0x0001, "", "R"), // inherited, read-only
		attr(0x0002, "", "RW"),
		attr(0xFFFD, "", "R"), // ClusterRevision: the gate's own
	}}}
	set := func(ids ...uint32) map[uint32]map[uint32]struct{} {
		return map[uint32]map[uint32]struct{}{0x1234: newReadOnlySet(ids...)}
	}
	if d := readOnlyDrift(clusters, set(0x0000, 0x0001)); len(d) != 0 {
		t.Errorf("the exact table drifts: %q", d)
	}
	d := readOnlyDrift(clusters, set(0x0000))
	if len(d) != 1 || !strings.Contains(d[0], "attr 0x0001 is read-only in matter.js but missing") {
		t.Errorf("a table without the inherited read-only attribute: %q, want the one missing entry", d)
	}
	d = readOnlyDrift(clusters, set(0x0000, 0x0001, 0x0002, 0x0003))
	if len(d) != 2 || !strings.Contains(d[0], "attr 0x0002 is in the read-only table") || !strings.Contains(d[1], "attr 0x0003 is in the read-only table") {
		t.Errorf("a table with a writable and an unlisted attribute: %q, want both", d)
	}
}

// TestAttributeWritable covers representative concrete verdicts plus the
// unknown-cluster / unknown-attribute fall-through behaviour.
func TestAttributeWritable(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name         string
		cluster      uint32
		attr         uint32
		wantWritable bool
		wantKnown    bool
	}{
		// OnOff.OnOff (0x0000, "R V") — the regressing read-only attribute.
		{"OnOff/OnOff read-only", 0x0006, 0x0000, false, true},
		// OnOff.OnTime (0x4001, "RW VO") — writable, not in the table.
		{"OnOff/OnTime writable", 0x0006, 0x4001, true, false},
		// LevelControl.CurrentLevel (0x0000, "R V") — read-only.
		{"LevelControl/CurrentLevel read-only", 0x0008, 0x0000, false, true},
		// DoorLock.LockState (0x0000, "R V") — read-only.
		{"DoorLock/LockState read-only", 0x0101, 0x0000, false, true},
		// DoorLock.OperatingMode (0x0025, "R[W] VM") — optional-write ⇒ writable.
		{"DoorLock/OperatingMode writable", 0x0101, 0x0025, true, false},
		// FlowMeasurement.MeasuredValue (0x0000, "R V") — read-only.
		{"FlowMeasurement/MeasuredValue read-only", 0x0404, 0x0000, false, true},
		// SmokeCoAlarm.ExpressedState (0x0000, "R V") — read-only.
		{"SmokeCoAlarm/ExpressedState read-only", 0x005C, 0x0000, false, true},
		// SmokeCoAlarm.SmokeSensitivityLevel (0x000B, "RW VM") — writable.
		{"SmokeCoAlarm/SmokeSensitivityLevel writable", 0x005C, 0x000B, true, false},
		// FanControl.PercentCurrent (0x0003, "R V") — read-only.
		{"FanControl/PercentCurrent read-only", 0x0202, 0x0003, false, true},
		// FanControl.PercentSetting (0x0002, "RW VO") — writable.
		{"FanControl/PercentSetting writable", 0x0202, 0x0002, true, false},
		// PumpConfigurationAndControl.Capacity (0x0013, "R V") — read-only.
		{"PumpConfigurationAndControl/Capacity read-only", 0x0200, 0x0013, false, true},
		// PumpConfigurationAndControl.OperationMode (0x0020, "RW VM") — writable.
		{"PumpConfigurationAndControl/OperationMode writable", 0x0200, 0x0020, true, false},
		// OperationalState.OperationalState (0x0004, "R V") — read-only.
		{"OperationalState/OperationalState read-only", 0x0060, 0x0004, false, true},
		// RvcOperationalState.CountdownTime (0x0002, inherited "R V") — read-only.
		{"RvcOperationalState/CountdownTime read-only", 0x0061, 0x0002, false, true},
		// Cluster the bridge does not expose → unknown, treated writable.
		{"unknown cluster", 0xBEEF, 0x0000, true, false},
		// Global attribute (no access string) → not tracked.
		{"OnOff ClusterRevision global", 0x0006, 0xFFFD, true, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			writable, known := AttributeWritable(tc.cluster, tc.attr)
			if writable != tc.wantWritable || known != tc.wantKnown {
				t.Errorf("AttributeWritable(0x%04X, 0x%04X) = (%v, %v), want (%v, %v)",
					tc.cluster, tc.attr, writable, known, tc.wantWritable, tc.wantKnown)
			}
		})
	}
}

// TestAttributeWritePrivilege pins the write privilege generated from the
// matter.js access strings: AccessControl ACL "RW F A" needs Administer,
// BasicInformation NodeLabel "RW VM" Manage, OnOff OnTime "RW VO" and an
// attribute with no entry the Operate default.
func TestAttributeWritePrivilege(t *testing.T) {
	t.Parallel()
	cases := []struct {
		cluster, attr uint32
		want          uint8
	}{
		{0x001F, 0x0000, PrivilegeAdminister},
		{0x0028, 0x0005, PrivilegeManage},
		{0x0006, 0x4001, PrivilegeOperate},
		{0xFFF1FC00, 0x0000, PrivilegeOperate},
	}
	for _, c := range cases {
		if got := AttributeWritePrivilege(c.cluster, c.attr); got != c.want {
			t.Errorf("AttributeWritePrivilege(0x%04X, 0x%04X) = %d, want %d", c.cluster, c.attr, got, c.want)
		}
	}
}

// TestAttributeWritePrivilegeParity holds the generated
// attributeWritePrivileges to the snapshot's effective access, attribute by
// attribute and in both directions (review II, W3-clugen-04): a writable
// attribute (rw other than "R", matter.js Access.writable) whose effective
// write privilege is Manage or Administer has exactly that entry, and the
// table has no other. The effective layer is each attribute's own — an
// attribute a derived cluster inherits without an access string included —
// so a table built from another cluster's string for the same (id, name)
// fails here the moment the two disagree.
func TestAttributeWritePrivilegeParity(t *testing.T) {
	t.Parallel()
	level := map[string]uint8{"O": PrivilegeOperate, "M": PrivilegeManage, "A": PrivilegeAdminister}
	want := map[uint32]map[uint32]uint8{}
	inherited := 0
	for _, c := range loadWritableSnapshot(t).Clusters {
		for _, a := range c.Attributes {
			if a.Effective == nil {
				t.Fatalf("%s (0x%04X) attribute %s (0x%04X) has no effective layer in the snapshot", c.Name, c.ID, a.Name, a.ID)
			}
			acc := a.Effective.Access
			if acc.RW == "" || acc.RW == "R" {
				continue
			}
			p, ok := level[acc.WritePriv]
			if !ok {
				t.Fatalf("%s (0x%04X) attribute %s (0x%04X): writable (rw %q) with write privilege %q", c.Name, c.ID, a.Name, a.ID, acc.RW, acc.WritePriv)
			}
			if p <= PrivilegeOperate {
				continue
			}
			if want[c.ID] == nil {
				want[c.ID] = map[uint32]uint8{}
			}
			want[c.ID][a.ID] = p
			if a.Access == "" {
				inherited++
			}
		}
	}
	if inherited == 0 {
		t.Error("the snapshot has no inherited attribute above Operate: the case this test exists for is not exercised")
	}
	for cid, attrs := range want {
		for aid, p := range attrs {
			if got, ok := attributeWritePrivileges[cid][aid]; !ok || got != p {
				t.Errorf("attributeWritePrivileges[0x%04X][0x%04X] = %d (%v), the effective access says %d", cid, aid, got, ok, p)
			}
		}
	}
	for cid, attrs := range attributeWritePrivileges {
		for aid, p := range attrs {
			if _, ok := want[cid][aid]; !ok {
				t.Errorf("attributeWritePrivileges[0x%04X][0x%04X] = %d, and the effective access names no privilege above Operate", cid, aid, p)
			}
		}
	}
}

// TestDeviceTypeAllowsServerCluster pins the three answers of the
// conformance oracle: a listed cluster, an unlisted one, an unknown type.
func TestDeviceTypeAllowsServerCluster(t *testing.T) {
	t.Parallel()
	if allowed, known := DeviceTypeAllowsServerCluster(0x0100, 0x0006); !allowed || !known {
		t.Errorf("OnOffLight / OnOff: allowed=%v known=%v", allowed, known)
	}
	if allowed, known := DeviceTypeAllowsServerCluster(0x0100, 0x0201); allowed || !known {
		t.Errorf("OnOffLight / Thermostat: allowed=%v known=%v", allowed, known)
	}
	if _, known := DeviceTypeAllowsServerCluster(0xFFFF, 0x0006); known {
		t.Error("an unknown device type is reported known")
	}
}

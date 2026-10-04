// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package schema

import (
	"encoding/json"
	"strings"
	"testing"

	matterparity "github.com/SukramJ/go-fabric/parity"
)

// snapshotAttribute is one attribute entry in the matter.js HEAD schema
// snapshot; only id + access are needed to derive writability.
type snapshotAttribute struct {
	ID     uint32 `json:"id"`
	Access string `json:"access"`
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

// snapshotWritable mirrors matter.js Access.writable
// (../matter.js/packages/model/src/aspects/Access.ts:44-46): writability is
// decided by the leading read/write token of the access string. rw == "R"
// (or no rw token, e.g. a bare "A" privilege) is read-only; "W", "RW", and
// the optional-write "R[W]" are writable.
func snapshotWritable(access string) bool {
	fields := strings.Fields(access)
	if len(fields) == 0 {
		return false // no rw token → read-only
	}
	switch fields[0] {
	case "R":
		return false
	case "W", "RW", "R[W]":
		return true
	default:
		return false // e.g. "A" — privilege only, no read/write token
	}
}

// TestReadOnlyAttributeParity pins readOnlyAttributes (via AttributeWritable)
// against the committed matter.js HEAD schema snapshot. For every cluster in
// the table it derives the read-only attribute set straight from the
// snapshot's per-attribute access strings and asserts an EXACT match — no
// missing read-only attribute, no stray/writable attribute — so the
// hand-maintained table cannot drift from matter.js when the snapshot is
// regenerated.
func TestReadOnlyAttributeParity(t *testing.T) {
	t.Parallel()

	snap := loadWritableSnapshot(t)
	byID := make(map[uint32]snapshotCluster, len(snap.Clusters))
	for _, c := range snap.Clusters {
		byID[c.ID] = c
	}

	for clusterID, tableSet := range readOnlyAttributes {
		sc, ok := byID[clusterID]
		if !ok {
			t.Errorf("readOnlyAttributes has cluster 0x%04X but the snapshot does not — stale table entry", clusterID)
			continue
		}

		// Derive the snapshot's read-only set for this cluster. Attributes
		// with no access string (globals) carry no writability verdict and
		// are excluded on both sides.
		want := make(map[uint32]struct{})
		for _, a := range sc.Attributes {
			if a.Access == "" {
				continue
			}
			if !snapshotWritable(a.Access) {
				want[a.ID] = struct{}{}
			}
		}

		// Exact set comparison catches both a missing read-only attribute
		// and a stray entry that matter.js actually marks writable.
		if len(want) != len(tableSet) {
			t.Errorf("cluster 0x%04X (%s): table has %d read-only attrs, snapshot has %d",
				clusterID, sc.Name, len(tableSet), len(want))
		}
		for id := range want {
			if _, present := tableSet[id]; !present {
				t.Errorf("cluster 0x%04X (%s): attr 0x%04X is read-only in matter.js but missing from the table",
					clusterID, sc.Name, id)
			}
		}
		for id := range tableSet {
			if _, present := want[id]; !present {
				t.Errorf("cluster 0x%04X (%s): attr 0x%04X is in the read-only table but matter.js marks it writable/unknown",
					clusterID, sc.Name, id)
			}
		}

		// Cross-check the public AttributeWritable verdict for every
		// access-bearing attribute of the cluster.
		for _, a := range sc.Attributes {
			if a.Access == "" {
				continue
			}
			writable, known := AttributeWritable(clusterID, a.ID)
			if snapshotWritable(a.Access) {
				if !writable {
					t.Errorf("AttributeWritable(0x%04X, 0x%04X) writable=false, want true (access %q)",
						clusterID, a.ID, a.Access)
				}
			} else {
				if !known || writable {
					t.Errorf("AttributeWritable(0x%04X, 0x%04X) = (writable %v, known %v), want (false, true) (access %q)",
						clusterID, a.ID, writable, known, a.Access)
				}
			}
		}
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

// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package chipdm

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SukramJ/go-fabric/script/levelgen/leveltable"
)

// TestChipDataModelLevels compares every level file with CHIP's
// data_model/<level> at the harness pin, by presence, and holds the
// differences to LevelAcknowledged: it fails on a difference the table
// does not explain and on an entry that explains none. Without a
// checkout it skips (fails under GOFABRIC_CHIP_REQUIRED).
func TestChipDataModelLevels(t *testing.T) {
	files, err := leveltable.Load(filepath.Join(repoRoot, "parity", "levels"))
	if err != nil {
		t.Fatal(err)
	}
	pin := makeVar(t, "CHIP_TEST_IMAGE_COMMIT")
	root := chipRoot()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	var all []Difference
	compared := 0
	for _, f := range files {
		level := f.Matter.Revision
		if _, skip := LevelsWithoutDataModel[level]; skip {
			continue
		}
		version := DataModelVersion(level)
		if _, err := TreeID(root, pin, version); err != nil {
			msg := "no connectedhomeip checkout with data_model/" + version + " at the pin " + pin + " at " + root +
				" (set GOFABRIC_CHIP_ROOT, or run make chiptool-setup)"
			if os.Getenv("GOFABRIC_CHIP_REQUIRED") != "" {
				t.Fatalf("GOFABRIC_CHIP_REQUIRED is set, but %s", msg)
			}
			t.Skip(msg)
		}
		chip, err := ReadChipLevelModel(root, pin, version)
		if err != nil {
			t.Fatalf("level %s: %v", level, err)
		}
		all = append(all, CompareLevel(level, LevelIDsOfFile(f), LevelIDsOfModel(chip.Model))...)
		compared++
	}
	if compared == 0 {
		t.Fatal("no level compared")
	}
	rec := Reconcile(all, LevelAcknowledged)
	for _, d := range rec.Unexplained {
		t.Errorf("unexplained level difference: %s — classify it in levels_acknowledged.go (class, reason, source)", d)
	}
	for _, a := range rec.Stale {
		t.Errorf("stale level acknowledgement: %s (chip %s, ours %s) explains no difference any more — remove it", a.Path, a.Chip, a.Ours)
	}
	t.Logf("compared %d levels, %d differences, %d explained", compared, len(all), len(all)-len(rec.Unexplained))
}

// TestCompareLevelOnFixtures holds CompareLevel and the id readers to a
// level file and a model written for the test.
func TestCompareLevelOnFixtures(t *testing.T) {
	var f leveltable.File
	f.Matter.Revision = "1.4.2"
	f.DeviceTypes = []leveltable.DeviceType{{ID: 0x0100, Revision: 3}, {ID: 0x0230, Revision: 1}}
	f.Clusters = []leveltable.Cluster{
		{ID: 0x0006, Features: []leveltable.Feature{{Bit: 0, Code: "LT"}}, Attributes: []uint32{0, 0x4000, 0xFFFD}, Commands: []uint32{0, 1}},
		{ID: 0x0104},
	}
	m := &Model{
		DeviceTypes:  []*DeviceType{{ID: u32(0x0100), Name: "OnOffLight"}},
		BaseClusters: []*Cluster{{Name: "Base", Events: []*Element{{ID: u32(0), Name: "E"}}}},
		Clusters: []*Cluster{
			{
				ID: u32(0x0006), Name: "OnOff", Base: "Base",
				Features:   []*Element{{Name: "LT", Constraint: "0"}, {Name: "OFFONLY", Constraint: "2"}, {Name: "X"}},
				Attributes: []*Element{{ID: u32(0), Name: "OnOff"}, {ID: u32(0xFFFC), Name: "FeatureMap"}},
				Commands:   []*Element{{ID: u32(0), Name: "Off"}, {ID: u32(1), Name: "On"}},
			},
			{ID: u32(0x0201), Name: "Thermostat"},
			{Name: "NoID"},
		},
	}
	got := CompareLevel("1.4.2", LevelIDsOfFile(f), LevelIDsOfModel(m))
	lines := make([]string, 0, len(got))
	for _, d := range got {
		lines = append(lines, d.Path+" chip="+d.Chip+" ours="+d.Ours)
	}
	want := []string{
		"L1_4_2.cluster.0x0006.attribute.0x4000 chip=absent ours=present",
		"L1_4_2.cluster.0x0006.event.0x00 chip=present ours=absent",
		"L1_4_2.cluster.0x0006.feature.2 chip=present ours=absent",
		"L1_4_2.cluster.0x0104 chip=absent ours=present",
		"L1_4_2.cluster.0x0201 chip=present ours=absent",
		"L1_4_2.deviceType.0x0230 chip=absent ours=present",
	}
	if strings.Join(lines, "\n") != strings.Join(want, "\n") {
		t.Fatalf("differences:\n%s\nwant:\n%s", strings.Join(lines, "\n"), strings.Join(want, "\n"))
	}
	ack := Acknowledgement{Path: "*.cluster.0x0104", Property: "presence", Chip: "absent", Ours: "present"}
	if !ack.matches(got[3]) || ack.matches(got[4]) {
		t.Error("a level-wildcard acknowledgement")
	}
}

// TestLevelAcknowledgedEntries: every entry of the level table and every
// skipped level carries a reason, every entry a source and a class.
func TestLevelAcknowledgedEntries(t *testing.T) {
	for _, a := range LevelAcknowledged {
		if a.Reason == "" || a.Source == "" || a.Class == "" || a.Property != "presence" {
			t.Errorf("entry %s: class, reason, source and the property presence are required", a.Path)
		}
	}
	for l, why := range LevelsWithoutDataModel {
		if why == "" {
			t.Errorf("level %s skipped without a reason", l)
		}
	}
}

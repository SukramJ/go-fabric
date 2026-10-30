// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package schema_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/SukramJ/go-fabric/parity"
	"github.com/SukramJ/go-fabric/schema"
	"github.com/SukramJ/go-fabric/script/levelgen/leveltable"
)

// TestLevelsGeneratedFileIsCurrent regenerates schema/levels_gen.go in
// memory from parity/levels/*.json and requires the committed file to
// equal it, and the last level to know every element of the pin.
func TestLevelsGeneratedFileIsCurrent(t *testing.T) {
	t.Parallel()
	root := moduleRoot(t)
	files, err := leveltable.Load(filepath.Join(root, "parity", "levels"))
	if err != nil {
		t.Fatal(err)
	}
	pin, err := leveltable.LoadPin(filepath.Join(root, "parity", "schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := leveltable.CheckPin(files, pin); err != nil {
		t.Fatal(err)
	}
	want, err := leveltable.GoSource(files)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(root, "schema", "levels_gen.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Error("schema/levels_gen.go is stale: run `go run ./script/levelgen` and read the diff")
	}
}

// TestLevelsSnapshotHash holds LevelsSnapshotSHA256 to the level files.
func TestLevelsSnapshotHash(t *testing.T) {
	t.Parallel()
	files, err := leveltable.Load(filepath.Join(moduleRoot(t), "parity", "levels"))
	if err != nil {
		t.Fatal(err)
	}
	if got := leveltable.Hash(files); got != schema.LevelsSnapshotSHA256 {
		t.Errorf("level files SHA-256 %s, generated constant %s: regenerate with `go run ./script/levelgen`", got, schema.LevelsSnapshotSHA256)
	}
}

// TestLevelsEndAtThePin: the levels ascend and the last is the pin's
// revision (parity/schema.json matter.revision).
func TestLevelsEndAtThePin(t *testing.T) {
	t.Parallel()
	var snap struct {
		Matter struct {
			Revision string `json:"revision"`
		} `json:"matter"`
	}
	if err := json.Unmarshal(parity.SchemaJSON(), &snap); err != nil {
		t.Fatal(err)
	}
	ls := schema.Levels()
	if len(ls) == 0 || string(ls[len(ls)-1]) != snap.Matter.Revision {
		t.Fatalf("Levels() = %v, the pin's revision %q must be last", ls, snap.Matter.Revision)
	}
	for i := 1; i < len(ls); i++ {
		if leveltable.CompareLevels(string(ls[i-1]), string(ls[i])) >= 0 {
			t.Errorf("Levels() not ascending at %s, %s", ls[i-1], ls[i])
		}
	}
}

// TestLevelAccessors reads elements whose history the level files show
// (brief S3 part 1, measured): Thermostat (0x0201) SCH (bit 3) up to
// 1.4.2 and not after, TEVT (bit 9) from 1.6.0.
func TestLevelAccessors(t *testing.T) {
	t.Parallel()
	const thermostat = 0x0201
	sch := schema.ClusterFeatureLevels(thermostat, 3)
	for _, c := range []struct {
		l    schema.Level
		want bool
	}{{"1.4", true}, {"1.4.1", true}, {"1.4.2", true}, {"1.5.1", false}, {"1.6.0", false}, {"1.6.1", false}} {
		if got := sch.Has(c.l); got != c.want {
			t.Errorf("SCH at %s = %v, want %v", c.l, got, c.want)
		}
	}
	if got := schema.ClusterFeatureLevels(thermostat, 9).Levels(); len(got) != 2 || got[0] != "1.6.0" {
		t.Errorf("TEVT levels = %v, want [1.6.0 1.6.1]", got)
	}
	if schema.LevelSet(0xFF).Has("1.7") {
		t.Error("an unknown level is in no set")
	}
	all := schema.ClusterLevels(thermostat)
	if len(all.Levels()) != len(schema.Levels()) {
		t.Errorf("Thermostat levels = %v", all.Levels())
	}
	if !schema.ClusterAttributeLevels(thermostat, 0x0000).Has("1.4") ||
		!schema.ClusterCommandLevels(thermostat, 0x00).Has("1.4") ||
		!schema.ClusterEventLevels(thermostat, 0x00).Has("1.6.1") ||
		!schema.DeviceTypeLevels(0x0301).Has("1.4") {
		t.Error("an element every level knows is missing")
	}
	if schema.ClusterLevels(0xFFFF_FFFF) != 0 || schema.ClusterFeatureLevels(0xFFFF_FFFF, 0) != 0 ||
		schema.ClusterAttributeLevels(0xFFFF_FFFF, 0) != 0 || schema.ClusterCommandLevels(0xFFFF_FFFF, 0) != 0 ||
		schema.ClusterEventLevels(0xFFFF_FFFF, 0) != 0 {
		t.Error("an unknown cluster has no levels")
	}
	if r, ok := schema.ClusterRevisionAt(thermostat, "1.4"); !ok || r == 0 {
		t.Errorf("ClusterRevisionAt(Thermostat, 1.4) = %d, %v", r, ok)
	}
	if _, ok := schema.ClusterRevisionAt(thermostat, "1.7"); ok {
		t.Error("no revision at an unknown level")
	}
	// Groupcast (0x0065) is not in the 1.4 model (parity/levels, measured).
	if _, ok := schema.ClusterRevisionAt(0x0065, "1.4"); ok {
		t.Error("no revision where the cluster is absent")
	}
	if _, ok := schema.ClusterRevisionAt(0xFFFF_FFFF, "1.4"); ok {
		t.Error("no revision of an unknown cluster")
	}
	if got := schema.LevelSet(0xFF).Levels(); len(got) != len(schema.Levels()) {
		t.Errorf("bits beyond the levels: %v", got)
	}
}

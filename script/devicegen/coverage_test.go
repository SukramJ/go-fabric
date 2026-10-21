// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"
)

// TestOperandOfAMalformedParameter: an operand that is no expression reads
// as the empty one rather than failing the generation.
func TestOperandOfAMalformedParameter(t *testing.T) {
	t.Parallel()

	got := (&ast{Param: json.RawMessage(`"not an expression"`)}).operand()
	if got.Type != astEmpty {
		t.Errorf("operand = %+v, want the empty expression", got)
	}
}

// TestDocsRefuseADeviceTypeTheSnapshotLacks: a committed name without an
// id in the snapshot is an error of the package doc and of the reference
// page, not a silent gap.
func TestDocsRefuseADeviceTypeTheSnapshotLacks(t *testing.T) {
	t.Parallel()

	g := testGenerator(t)
	if _, err := g.generatePackageDoc([]string{"NoSuchDeviceType"}); err == nil {
		t.Error("generatePackageDoc accepted a device type the snapshot lacks")
	}
	if _, err := g.generateReference([]string{"NoSuchDeviceType"}); err == nil {
		t.Error("generateReference accepted a device type the snapshot lacks")
	}
}

// TestContractNamesRefuseAnIDBeyondUint16: the contract's table is keyed by
// a uint16.
func TestContractNamesRefuseAnIDBeyondUint16(t *testing.T) {
	t.Parallel()

	id := uint32(0x10000)
	g := &generator{snap: &snapshot{DeviceTypes: []*deviceType{{ID: &id, Name: "TooBig"}}}}
	if _, err := g.generateContractNames(); err == nil {
		t.Error("generateContractNames accepted an id beyond uint16")
	}
}

// TestElementOfAnUnknownKind: a requirement names an attribute, a command
// or an event; any other kind finds nothing.
func TestElementOfAnUnknownKind(t *testing.T) {
	t.Parallel()

	c := &cluster{Attributes: []*element{{Name: "X"}}}
	if c.element("attribute", "X") == nil {
		t.Error("the attribute was not found")
	}
	if c.element("feature", "X") != nil {
		t.Error("an element of an unknown kind was found")
	}
}

// TestNewGeneratorRefusesAnIncompleteRoot: a root without the snapshot, or
// with one but without the cluster definitions, is an error.
func TestNewGeneratorRefusesAnIncompleteRoot(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if _, err := newGenerator(root); err == nil {
		t.Error("newGenerator accepted a root without a snapshot")
	}
	realRoot, err := moduleRoot()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "parity"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(realRoot, snapshotPath), filepath.Join(root, snapshotPath)); err != nil {
		t.Skipf("no symlinks: %v", err)
	}
	if _, err := newGenerator(root); err == nil {
		t.Error("newGenerator accepted a root without cluster definitions")
	}
}

// TestRunWithHelp: -h is the flag package's help, not a failure to
// generate.
func TestRunWithHelp(t *testing.T) {
	t.Parallel()

	if err := run([]string{"-h"}, io.Discard); err == nil {
		t.Error("-h returned nil")
	}
}

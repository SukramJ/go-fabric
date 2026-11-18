// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"os/exec"
	"reflect"
	"strings"
	"testing"
)

// report is apidiff's module-mode output as it prints it.
const report = `Ignoring internal package github.com/SukramJ/go-fabric/internal/udpseam
Incompatible changes:
- ./cluster/spec.Bool: removed
- ./controller/client.(*Cluster).Decoded: changed from func(uint32) (any, error) to func(uint32) any
- ./examples/quickstart-controller.Thing: removed
- ./script/apidiff.change: removed
- ./node/old: removed
- ./cluster/media.(*stateful[State]).State: changed from func() A to func() B
`

var root = target{Dir: ".", Path: module, Name: rootName}

func TestParse(t *testing.T) {
	got := parse(report, root)
	want := []change{
		{Line: "./cluster/spec.Bool: removed", Package: "cluster/spec", Symbol: "spec.Bool", Bare: "Bool", Path: "cluster/spec"},
		{Line: "./controller/client.(*Cluster).Decoded: changed from func(uint32) (any, error) to func(uint32) any", Package: "controller/client", Symbol: "Cluster.Decoded"},
		{Line: "./node/old: removed", Package: "node/old", Symbol: "node/old"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("parse:\n got %+v\nwant %+v", got, want)
	}
}

// TestSplitRoot: apidiff writes a symbol of the module's root package
// without a prefix (objectString in golang.org/x/exp/apidiff).
func TestSplitRoot(t *testing.T) {
	if c := split("Version", module); c.Package != "." || c.Symbol != "Version" {
		t.Errorf("root symbol: %+v", c)
	}
	if c := qualify(split("Version", module), root); c.Symbol != "fabric.Version" || c.Path != "." {
		t.Errorf("root symbol qualified: %+v", c)
	}
	if c := qualify(split("(*Driver).Close", module), root); c.Package != "." || c.Symbol != "Driver.Close" {
		t.Errorf("root method: %+v", c)
	}
}

// TestNestedModule: a nested module's changes are named by its package
// names and, for a removed package, by the repository-relative path
// (R2-P6-sup-2803).
func TestNestedModule(t *testing.T) {
	esphome := target{Dir: "ble/esphome", Path: module + "/ble/esphome", Name: "esphome"}
	out := "- DiscoverOptions: removed\n- ./esphometest.Proxy: removed\n- package " + module + "/ble/esphome/esphometest: removed\n"
	got := parse(out, esphome)
	want := []string{"esphome.DiscoverOptions", "esphometest.Proxy", "ble/esphome/esphometest"}
	if len(got) != len(want) {
		t.Fatalf("parse: %+v", got)
	}
	for i, c := range got {
		if c.Symbol != want[i] {
			t.Errorf("change %d: symbol %q, want %q", i, c.Symbol, want[i])
		}
	}
}

const changelog = "# Changelog\n\n## [Unreleased]\n\n### Added\n\n- `spec.Bool`, new.\n\n" +
	"### Changed\n\n- `client.Cluster.Decoded` returns one value.\n\n### Removed\n\n- `node/old`: gone.\n\n" +
	"## [0.10.2] — 2026-10-23\n\n### Removed\n\n- `spec.Bool`.\n"

func TestSplitRemovedPackage(t *testing.T) {
	c := split("package "+module+"/controller/thread", module)
	if c.Package != "controller/thread" || c.Symbol != "controller/thread" {
		t.Fatalf("split(package …/controller/thread) = %+v", c)
	}
	if got := unannounced([]change{c}, "- `controller/thread`: moved to `thread`"); len(got) != 0 {
		t.Fatalf("announced removal reported: %+v", got)
	}
}

func TestUnannounced(t *testing.T) {
	text, err := unreleased(changelog, "v0.10.2")
	if err != nil {
		t.Fatal(err)
	}
	missing := unannounced(parse(report, root), text)
	if len(missing) != 1 || missing[0].Symbol != "spec.Bool" {
		t.Errorf("missing %+v, want only spec.Bool: Added does not announce a break, and an older release's line does not count", missing)
	}
	// A release pull request: [Unreleased] is empty, the cut version above
	// the base tag carries the lines, the base's own section does not count.
	cut := "# Changelog\n\n## [Unreleased]\n\n## [0.10.3] — 2026-10-24\n\n### Added\n\n- `spec.Bool`, new.\n\n" +
		"### Changed\n\n- `client.Cluster.Decoded` returns one value.\n\n### Removed\n\n- `node/old`: gone.\n\n" +
		"## [0.10.2] — 2026-10-23\n\n### Removed\n\n- `spec.Bool`.\n"
	text, err = unreleased(cut, "v0.10.2")
	if err != nil {
		t.Fatal(err)
	}
	missing = unannounced(parse(report, root), text)
	if len(missing) != 1 || missing[0].Symbol != "spec.Bool" {
		t.Errorf("release cut: missing %+v, want only spec.Bool", missing)
	}
}

// TestUnreleasedFailsClosed: a base whose heading the changelog does not
// carry — a nested module's tag taken for the root's, a mistyped -base —
// is an error, not a read to the end of the file that counts every older
// release's lines (R2-P6-sup-2801).
func TestUnreleasedFailsClosed(t *testing.T) {
	if _, err := unreleased("# no section\n", "v0.10.2"); err == nil {
		t.Error("a changelog without [Unreleased] reads as announcing")
	}
	if _, err := unreleased(changelog, "v0.9.9"); err == nil || !strings.Contains(err.Error(), "0.9.9") {
		t.Errorf("a base without its heading: err %v", err)
	}
	// A nested module's tag names the version of its heading.
	if _, err := unreleased(changelog, "ble/esphome/v0.10.2"); err != nil {
		t.Errorf("nested tag: %v", err)
	}
}

// TestUnannouncedNeedsThePackage: a line that names another package's
// symbol of the same name does not announce the break (R2-P6-sup-2802).
func TestUnannouncedNeedsThePackage(t *testing.T) {
	changes := parse("- ./cluster/onoff.Options: removed\n", root)
	if got := unannounced(changes, "- `node.Options` gains a field.\n"); len(got) != 1 {
		t.Errorf("node.Options announced onoff.Options: %+v", got)
	}
	if got := unannounced(changes, "- `node`: `Options` gains a field.\n- `cluster/onoff`: a new attribute.\n"); len(got) != 1 {
		t.Errorf("the package and the symbol on two lines announced onoff.Options: %+v", got)
	}
	for _, line := range []string{
		"- `onoff.Options` is gone.\n",
		"- `cluster/onoff.Options` is gone.\n",
		"- **Breaking:** `cluster/onoff`: `Options` is gone.\n",
	} {
		if got := unannounced(changes, line); len(got) != 0 {
			t.Errorf("%q does not announce onoff.Options: %+v", line, got)
		}
	}
}

// TestLastTagSkipsNestedTags: with a nested module's tag on the same
// commit as the root's release, the root's base is still the v* tag, and a
// nested module finds its own (R2-P6-sup-2801, R2-P6-sup-2803).
func TestLastTagSkipsNestedTags(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	for _, args := range [][]string{
		{"init", "-q"},
		{"-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "--allow-empty", "-m", "one"},
		{"tag", "v0.1.0"},
		{"-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "--allow-empty", "-m", "two"},
		{"tag", "v0.2.0"},
		{"tag", "ble/esphome/v0.2.1"},
	} {
		if out, err := exec.CommandContext(t.Context(), "git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	for dir, want := range map[string]string{".": "v0.2.0", "ble/esphome": "ble/esphome/v0.2.1", "wifi/x": ""} {
		got, err := lastTag(dir)
		if err != nil || got != want {
			t.Errorf("lastTag(%q) = %q, %v; want %q", dir, got, err, want)
		}
	}
}

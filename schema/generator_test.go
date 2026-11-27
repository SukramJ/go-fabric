// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package schema_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// The generator (script/generate_matter_schema.go, //go:build ignore) is
// built once and run over synthetic snapshots in a temporary tree (-root):
// the cases below hold forms parity/schema.json does not carry at the pin,
// which the snapshot tests beside this file therefore cannot reach.
var (
	generatorOnce sync.Once
	generatorBin  string
	generatorErr  error
)

func schemaGenerator(t *testing.T) string {
	t.Helper()
	generatorOnce.Do(func() {
		dir, err := os.MkdirTemp("", "schemagen-")
		if err != nil {
			generatorErr = err
			return
		}
		generatorBin = filepath.Join(dir, "generate_matter_schema")
		out, err := exec.Command("go", "build", "-o", generatorBin, "../script/generate_matter_schema.go").CombinedOutput() //nolint:gosec // G204: the go tool on the module's own generator
		if err != nil {
			generatorErr = err
			generatorBin = string(out)
		}
	})
	if generatorErr != nil {
		t.Fatalf("build the generator: %v\n%s", generatorErr, generatorBin)
	}
	return generatorBin
}

// synthSnapshot is a snapshot of one cluster (id 0x0006, "A") with the
// given attributes and commands, and one device type without requirements.
func synthSnapshot(attributes, commands string) string {
	return synthSnapshotFeatures(attributes, commands, ``)
}

// synthSnapshotFeatures is synthSnapshot with the cluster's features.
func synthSnapshotFeatures(attributes, commands, features string) string {
	return `{"matter":{"revision":"1.6.1","sourceCommit":"0123456789"},
"clusters":[{"id":6,"name":"A","revision":1,"featureMap":0,"classification":"application",
  "attributes":[` + attributes + `],"commands":[` + commands + `],"events":[],"features":[` + features + `],"datatypes":[],"attributeOrder":[65533]}],
"deviceTypes":[{"id":256,"name":"D","classification":"simple","revision":1,"requirements":[],
  "effective":{"composition":"","base":"","conditions":[],"requirements":[]}}],
"baseDeviceTypes":[],"globalDatatypes":[],"definitions":[],"globalAttributeOrder":[65533]}`
}

// runGenerator writes snap into a fresh tree and runs the generator over
// it; it returns the tree and the generator's combined output.
func runGenerator(t *testing.T, snap string) (root, out string, err error) {
	t.Helper()
	root = t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "parity"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "parity", "schema.json"), []byte(snap), 0o600); err != nil {
		t.Fatal(err)
	}
	b, err := exec.Command(schemaGenerator(t), "-root", root).CombinedOutput() //nolint:gosec // G204: the generator built above
	return root, string(b), err
}

func readGenerated(t *testing.T, root, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, "schema", name)) //nolint:gosec // G304: the test's own tree
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// tableRows returns the lines of the generated map var name.
func tableRows(src, name string) string {
	_, rest, ok := strings.Cut(src, "var "+name+" ")
	if !ok {
		return ""
	}
	head, _, _ := strings.Cut(rest, "\n")
	if strings.HasSuffix(head, "{}") {
		return "" // an empty map
	}
	body, _, _ := strings.Cut(rest, "\n}\n")
	return body
}

// TestGeneratorReadsTheEffectiveDirection: a derived cluster's override of
// a base command states no direction of its own (RvcOperationalState's
// Pause at the pin); its effective layer carries the inherited one. A
// timed or Large Message request command stated so enters the timed and
// Large Message tables as writeInvokeAccessFile's tables take it
// (R2-P6-sec-2701).
func TestGeneratorReadsTheEffectiveDirection(t *testing.T) {
	t.Parallel()
	root, out, err := runGenerator(t, synthSnapshot(``,
		`{"id":1,"name":"Pause","effective":{"direction":"request","access":{"timed":true,"writePriv":"O"},"quality":{"largeMessage":true}}}`))
	if err != nil {
		t.Fatalf("generator: %v\n%s", err, out)
	}
	if rows := tableRows(readGenerated(t, root, "timed_gen.go"), "timedInvokePaths"); !strings.Contains(rows, "0x01: {}, // Pause") {
		t.Errorf("timedInvokePaths lacks the override without a raw direction:\n%s", rows)
	}
	if rows := tableRows(readGenerated(t, root, "large_message_gen.go"), "largeMessageCommands"); !strings.Contains(rows, "0x01: {}, // Pause") {
		t.Errorf("largeMessageCommands lacks the override without a raw direction:\n%s", rows)
	}
}

// TestGeneratorRefusesARequestWithoutEffectiveLayer: a request command
// without an effective block has no access to generate the timed, Large
// Message or invoke tables from; the generation stops rather than leaving
// the command out (R2-P6-sec-2701).
func TestGeneratorRefusesARequestWithoutEffectiveLayer(t *testing.T) {
	t.Parallel()
	_, out, err := runGenerator(t, synthSnapshot(``, `{"id":1,"name":"Pause","direction":"request"}`))
	if err == nil || !strings.Contains(out, "without an effective block") {
		t.Fatalf("generator: %v, want a refusal of the request without an effective block\n%s", err, out)
	}
}

// TestGeneratorReadsTheEffectiveChangesOmitted: an attribute a derived
// cluster inherits carries no quality string of its own; its effective
// quality states changesOmitted, and the table reads that (R2-P6-par-2702).
func TestGeneratorReadsTheEffectiveChangesOmitted(t *testing.T) {
	t.Parallel()
	root, out, err := runGenerator(t, synthSnapshot(
		`{"id":2,"name":"Inherited","effective":{"access":{"rw":"R","readPriv":"V"},"quality":{"changesOmitted":true}}}`, ``))
	if err != nil {
		t.Fatalf("generator: %v\n%s", err, out)
	}
	if rows := tableRows(readGenerated(t, root, "attribute_access_gen.go"), "changesOmittedAttributes"); !strings.Contains(rows, "0x0002: {}, // Inherited") {
		t.Errorf("changesOmittedAttributes lacks the inherited attribute:\n%s", rows)
	}
}

// TestGeneratorRefusesAFeatureWithoutAUsableBit: a feature without a bit,
// or with one outside the 32-bit FeatureMap, is refused rather than left
// out of clusterFeatures (R2-P6-par-2714).
func TestGeneratorRefusesAFeatureWithoutAUsableBit(t *testing.T) {
	t.Parallel()
	for name, feature := range map[string]string{
		"no bit": `{"name":"F","effective":{"title":"Eff"}}`,
		"bit 32": `{"name":"F","bit":32,"effective":{"title":"Eff"}}`,
		"bit -1": `{"name":"F","bit":-1,"effective":{"title":"Eff"}}`,
	} {
		_, out, err := runGenerator(t, synthSnapshotFeatures(``, ``, feature))
		if err == nil || !strings.Contains(out, "A.F") {
			t.Errorf("%s: generator %v, want a refusal naming A.F\n%s", name, err, out)
		}
	}
	root, out, err := runGenerator(t, synthSnapshotFeatures(``, ``, `{"name":"F","bit":31,"effective":{"title":"Eff"}}`))
	if err != nil {
		t.Fatalf("bit 31: %v\n%s", err, out)
	}
	if rows := tableRows(readGenerated(t, root, "devicetype_requirements_gen.go"), "clusterFeatures"); !strings.Contains(rows, `{Name: "F", Bit: 31, Title: "Eff"}`) {
		t.Errorf("clusterFeatures lacks bit 31:\n%s", rows)
	}
}

// TestGeneratorWritesNothingWhenItFails: a generation that fails at a later
// table leaves every file as it was — none of the earlier tables is
// written (R2-P6-err-2709). The snapshot fails at the attribute order,
// after the cluster, access, timed and invoke tables.
func TestGeneratorWritesNothingWhenItFails(t *testing.T) {
	t.Parallel()
	snap := strings.Replace(synthSnapshot(``, ``), `"globalAttributeOrder":[65533]`, `"globalAttributeOrder":[]`, 1)
	root := t.TempDir()
	for dir, files := range map[string]map[string]string{
		"parity": {"schema.json": snap},
		"schema": {"clusters.go": "old clusters\n"},
	} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o750); err != nil {
			t.Fatal(err)
		}
		for name, content := range files {
			if err := os.WriteFile(filepath.Join(root, dir, name), []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
	out, err := exec.Command(schemaGenerator(t), "-root", root).CombinedOutput() //nolint:gosec // G204: the generator built above
	if err == nil || !strings.Contains(string(out), "globalAttributeOrder") {
		t.Fatalf("generator: %v, want the attribute order's refusal\n%s", err, out)
	}
	entries, err := os.ReadDir(filepath.Join(root, "schema"))
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if len(names) != 1 || names[0] != "clusters.go" {
		t.Errorf("schema/ holds %v after a failed run, want only the old clusters.go", names)
	}
	if got := readGenerated(t, root, "clusters.go"); got != "old clusters\n" {
		t.Errorf("clusters.go was rewritten by a failed run:\n%.200s", got)
	}
}

// TestGeneratorRefusesACommandWithoutADirection: a command whose raw and
// effective layers both state no direction is refused by every writer
// that selects request commands, not skipped — matter.js reads such a
// command as a request (CommandModel.ts:23-25 at db91886c, isRequest is
// "not a response"), so skipping it would leave a timed request out of
// timedInvokePaths (R2-P6-sec-2701, Fable fable-read-6b-gen R1).
func TestGeneratorRefusesACommandWithoutADirection(t *testing.T) {
	t.Parallel()
	_, out, err := runGenerator(t, synthSnapshot(``,
		`{"id":1,"name":"Pause","effective":{"access":{"timed":true,"writePriv":"O"}}}`))
	if err == nil || !strings.Contains(out, "A.Pause") || !strings.Contains(out, "direction") {
		t.Fatalf("generator: %v, want a refusal of A.Pause's missing direction\n%s", err, out)
	}
}

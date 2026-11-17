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
func runGenerator(t *testing.T, snap string) (root string, out string, err error) {
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

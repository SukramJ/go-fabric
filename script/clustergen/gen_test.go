// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

var update = flag.Bool("update", false, "rewrite the golden files")

var loadOnce = sync.OnceValues(func() (*snapshot, error) {
	root, err := moduleRoot()
	if err != nil {
		return nil, err
	}
	return loadSnapshot(filepath.Join(root, snapshotPath))
})

func testSnapshot(t *testing.T) *snapshot {
	t.Helper()
	s, err := loadOnce()
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// TestGolden pins the generator's output for representative clusters: a
// derived cluster with struct lists (RvcRunMode, from ModeBase), one with
// feature-gated enum values and nullable bounded attributes
// (PumpConfigurationAndControl), a derived one whose bounds name siblings
// and whose enum values hang on features
// (CarbonMonoxideConcentrationMeasurement, from ConcentrationMeasurement),
// and a derived one with a struct-list attribute and a fieldless command
// (HepaFilterMonitoring, from ResourceMonitoring). Run with -update after a
// deliberate generator change and read the diff.
func TestGolden(t *testing.T) {
	t.Parallel()
	snap := testSnapshot(t)
	for _, name := range []string{"RvcRunMode", "PumpConfigurationAndControl", "CarbonMonoxideConcentrationMeasurement", "HepaFilterMonitoring"} {
		o, err := generate(snap, name)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		for suffix, got := range map[string][]byte{".go.golden": o.Source, "_test.go.golden": o.Test} {
			path := filepath.Join("testdata", o.Package+suffix)
			if *update {
				if err := os.WriteFile(path, got, 0o644); err != nil { //nolint:gosec // a golden file
					t.Fatal(err)
				}
				continue
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("%s: %v (run go test ./script/clustergen -update)", path, err)
			}
			if !bytes.Equal(got, want) {
				t.Errorf("%s differs from the generator's output; run go test ./script/clustergen -update and read the diff", path)
			}
		}
	}
}

// TestCommittedPackagesAreCurrent fails when a committed generated package
// under cluster/spec/ is not what the generator makes of parity/schema.json
// now — a snapshot refreshed, or the generator changed, without
// regenerating (make generate-matter-schema, or go run ./script/clustergen).
func TestCommittedPackagesAreCurrent(t *testing.T) {
	t.Parallel()
	snap := testSnapshot(t)
	root, err := moduleRoot()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range committed {
		o, err := generate(snap, name)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		for file, want := range map[string][]byte{"definition_gen.go": o.Source, "definition_gen_test.go": o.Test} {
			path := filepath.Join(root, outDir, o.Package, file)
			got, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(got, want) {
				t.Errorf("%s is stale: run go run ./script/clustergen", path)
			}
		}
	}
}

func TestRun(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	var out bytes.Buffer
	if err := run([]string{"-out", dir}, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "rvcrunmode") {
		t.Errorf("output %q", out.String())
	}
	out.Reset()
	if err := run([]string{"-out", dir}, &out); err != nil || out.Len() != 0 {
		t.Errorf("a second run rewrote files: %q %v", out.String(), err)
	}
	if err := run([]string{"-nope"}, &out); err == nil {
		t.Error("an unknown flag was accepted")
	}
	blocker := filepath.Join(dir, "file")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"-out", blocker}, &out); err == nil {
		t.Error("an output directory below a file was accepted")
	}
}

func TestNames(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{
		"percent": "Percent", "epoch-s": "EpochS", "3Phase": "V3Phase", "": "X", "a b": "AB", "ModeTag": "ModeTag",
	} {
		if got := exportName(in); got != want {
			t.Errorf("exportName(%q) = %q, want %q", in, got, want)
		}
	}
	if packageName("Switch") != "switchcluster" || packageName("RvcRunMode") != "rvcrunmode" {
		t.Error("packageName")
	}
	for in, want := range map[string]string{"OperationModeEnum": "OperationMode", "PumpStatusBitmap": "PumpStatus", "Enum": "Enum", "ModeTag": "ModeTag"} {
		if got := valuePrefix(in); got != want {
			t.Errorf("valuePrefix(%q) = %q", in, got)
		}
	}
	if lowerFirst("") != "" || lowerFirst("Abc") != "abc" {
		t.Error("lowerFirst")
	}
	if short("85cf66472b") != "85cf664" || short("abc") != "abc" {
		t.Error("short")
	}
	if primitiveBits("enum8") != 8 || primitiveBits("map32") != 32 || primitiveBits("single") != 0 {
		t.Error("primitiveBits")
	}
	if goInt(24, false) != "uint32" || goInt(40, true) != "int64" || goInt(8, true) != "int8" || goInt(16, false) != "uint16" {
		t.Error("goInt")
	}
}

func raw(s string) json.RawMessage { return json.RawMessage(s) }

func TestLiterals(t *testing.T) {
	t.Parallel()
	if !isMandatory(&conformance{AST: raw(`{"type":"M"}`)}) || isMandatory(nil) ||
		!isMandatory(&conformance{AST: raw(`{"type":"otherwise","param":[{"type":"optionalIf","param":{"type":"name","param":"X"}},{"type":"M"}]}`)}) ||
		isMandatory(&conformance{AST: raw(`{"type":"otherwise","param":[{"type":"P"},{"type":"M"}]}`)}) ||
		isMandatory(&conformance{AST: raw(`{"type":"name","param":"F"}`)}) {
		t.Error("isMandatory")
	}
	if b, w, ok := bitsOf(&constraint{Min: raw("2"), Max: raw("4")}); !ok || b != 2 || w != 3 {
		t.Error("bitsOf range")
	}
	if _, _, ok := bitsOf(&constraint{Text: "desc"}); ok {
		t.Error("bitsOf without bits")
	}
	if _, _, ok := bitsOf(nil); ok {
		t.Error("bitsOf nil")
	}
	if _, ok := intOf(raw("1.5")); ok {
		t.Error("intOf fraction")
	}
	for in, want := range map[string]string{
		"null": "spec.NullDefault{}", "true": "true", "3": "float64(3)", `"x"`: `"x"`, `{"type":"x"}`: "", "": "", "[": "",
	} {
		if got := defaultLiteral(raw(in)); got != want {
			t.Errorf("defaultLiteral(%s) = %q, want %q", in, got, want)
		}
	}
	if got := boundLiteral(raw(`{"type":"-","lhs":1}`)); !strings.Contains(got, "Expr") {
		t.Errorf("boundLiteral expression %q", got)
	}
	if got := boundLiteral(raw(`{"type":"reference","name":"x"}`)); got != `&spec.Bound{Ref: "x"}` {
		t.Errorf("boundLiteral reference %q", got)
	}
	c := &constraint{
		Text: "0, 2 to 5", Desc: true, In: raw(`{"type":"reference","name":"list"}`), Entry: &constraint{Text: "max 3", Max: raw("3")},
		Parts: []*constraint{{Text: "0", Value: raw("0")}, {Text: "2 to 5", Min: raw("2"), Max: raw("5")}},
	}
	lit := constraintLiteral(c)
	for _, want := range []string{"Desc: true", `In: "list"`, "Entry: &spec.Constraint", "Parts: []spec.Constraint{{"} {
		if !strings.Contains(lit, want) {
			t.Errorf("constraintLiteral %q lacks %q", lit, want)
		}
	}
	if constraintLiteral(nil) != "" || accessLiteral(nil) != "" || accessLiteral(&access{}) != "" || accessText(nil) != "" {
		t.Error("empty literals")
	}
	if got := accessText(&access{RW: "RW", Fabric: "F", ReadPriv: "V", WritePriv: "M", Timed: true}); got != "RW F VM T" {
		t.Errorf("accessText %q", got)
	}
	if got := accessLiteral(&access{Fabric: "S", Timed: true}); got != `spec.Access{Fabric: "S", Timed: true}` {
		t.Errorf("accessLiteral %q", got)
	}
	if confLiteral(nil) != "spec.Conformance{}" || confText(nil) != "O" || confComment(nil) != "" || confField(nil) != "" {
		t.Error("empty conformance")
	}
	if got := confLiteral(&conformance{Text: "?", AST: raw(`{"type":"bogus"}`)}); !strings.Contains(got, "ConfDesc") {
		t.Errorf("an unknown node renders %q", got)
	}
	for _, ast := range []string{
		`[`, `{"type":"name","param":1}`, `{"type":"revision","param":"x"}`, `{"type":"choice","param":1}`,
		`{"type":"choice","param":{"name":"a","num":1,"expr":{"type":"bogus"}}}`, `{"type":"otherwise","param":1}`,
		`{"type":"otherwise","param":[{"type":"bogus"}]}`, `{"type":"!","param":{"type":"bogus"}}`, `{"type":"&","param":1}`,
		`{"type":"&","param":{"lhs":{"type":"bogus"},"rhs":{"type":"M"}}}`, `{"type":"&","param":{"lhs":{"type":"M"},"rhs":{"type":"bogus"}}}`,
	} {
		if _, err := astLiteral(raw(ast)); err == nil {
			t.Errorf("astLiteral(%s) accepted", ast)
		}
	}
	if got, err := astLiteral(raw(`{"type":"value","param":7}`)); err != nil || !strings.Contains(got, `Value: "7"`) {
		t.Errorf("value node %q %v", got, err)
	}
	if typeText(&value{}) != "untyped" || describe(&value{Type: "bool", Access: &access{RW: "R"}}) != "bool, O, R" {
		t.Error("typeText / describe")
	}
	if priorityLiteral("debug") != "spec.PriorityDebug" || priorityLiteral("critical") != "spec.PriorityCritical" || priorityLiteral("") != "spec.PriorityInfo" {
		t.Error("priorityLiteral")
	}
	if lo, hi, ok := lengthBounds(&constraint{Value: raw("16")}); !ok || lo != 16 || hi != 16 {
		t.Error("lengthBounds value")
	}
	if lo, hi, ok := numericBounds(&constraint{Value: raw("8")}, "uint8"); !ok || lo != 8 || hi != 8 {
		t.Error("numericBounds value")
	}
	if lo, hi, ok := numericBounds(nil, "percent"); !ok || lo != 0 || hi != 100 {
		t.Error("numericBounds percent")
	}
	if featureTitle(&feature{Name: "N"}) != "N" {
		t.Error("featureTitle")
	}
}

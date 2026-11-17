// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
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

// TestSnapshotRefusesAFeatureWithoutAUsableBit: a feature without a bit
// would decode as bit 0, and one at bit 32 or above shifts out of the
// uint32 mask; both are refused when the snapshot is read
// (R2-P6-par-2714).
func TestSnapshotRefusesAFeatureWithoutAUsableBit(t *testing.T) {
	t.Parallel()

	for name, feature := range map[string]string{
		"no bit": `{"name":"F","effective":{"title":"Feat"}}`,
		"bit 32": `{"name":"F","bit":32,"effective":{"title":"Feat"}}`,
	} {
		raw := `{"deviceTypes":[{"id":4096,"name":"T","classification":"simple","revision":1,"effective":{"composition":"tree","requirements":[]}}],
			"clusters":[{"id":1,"name":"C","classification":"application","features":[` + feature + `],"attributes":[],"commands":[],"events":[]}]}`
		if _, err := parseSnapshot([]byte(raw)); err == nil || !strings.Contains(err.Error(), "C.F") {
			t.Errorf("%s: parseSnapshot = %v, want a refusal naming C.F", name, err)
		}
	}
}

// TestSnapshotRefusesAnUnknownConformance: a conformance node the
// evaluators do not know, or one whose parameter does not decode, is
// refused when the snapshot is read — as clustergen refuses an unknown
// node — instead of reading as false or permit (R2-P6-par-2707,
// wizard-6b §2 gen Q1).
func TestSnapshotRefusesAnUnknownConformance(t *testing.T) {
	t.Parallel()

	for name, ast := range map[string]string{
		"unknown root":           `{"type":"bogus"}`,
		"unknown in otherwise":   `{"type":"otherwise","param":[{"type":"name","param":"A"},{"type":"bogus"}]}`,
		"otherwise not a list":   `{"type":"otherwise","param":{"type":"M"}}`,
		"revision not a number":  `{"type":"revision","param":"v2"}`,
		"name not a string":      `{"type":"name","param":7}`,
		"operator without rhs":   `{"type":"&","param":{"lhs":{"type":"name","param":"A"}}}`,
		"negation of nothing":    `{"type":"!","param":7}`,
		"unknown under a choice": `{"type":"choice","param":{"name":"a","num":1,"orMore":true,"expr":{"type":"bogus"}}}`,
	} {
		raw := `{"deviceTypes":[{"id":4096,"name":"T","classification":"simple","revision":1,"effective":{"composition":"tree","requirements":[
			{"element":"serverCluster","name":"C","id":1,"conformance":{"text":"?","ast":` + ast + `},"requirements":[]}]}}],
			"clusters":[{"id":1,"name":"C","classification":"application","features":[],"attributes":[],"commands":[],"events":[]}]}`
		if _, err := parseSnapshot([]byte(raw)); err == nil || !strings.Contains(err.Error(), "T") {
			t.Errorf("%s: parseSnapshot = %v, want a refusal naming T", name, err)
		}
	}
	// A cluster element's conformance is checked as well.
	raw := `{"deviceTypes":[{"id":4096,"name":"T","classification":"simple","revision":1,"effective":{"composition":"tree","requirements":[]}}],
		"clusters":[{"id":1,"name":"C","classification":"application","features":[],"attributes":[{"id":0,"name":"A","effective":{"conformance":{"text":"?","ast":{"type":"bogus"}}}}],"commands":[],"events":[]}]}`
	if _, err := parseSnapshot([]byte(raw)); err == nil || !strings.Contains(err.Error(), "C.A") {
		t.Errorf("an element's unknown conformance: parseSnapshot = %v, want a refusal naming C.A", err)
	}
}

// TestMandatedUnderRefusesWhatItDoesNotEvaluate: a Base requirement's
// condition is a name, "&", "|" or "!"; a node the judgement does not
// evaluate (a description, a choice, a value, a comparison) is an error,
// not false (R2-P6-par-2707).
func TestMandatedUnderRefusesWhatItDoesNotEvaluate(t *testing.T) {
	t.Parallel()

	for _, astJSON := range []string{
		`{"type":"desc"}`,
		`{"type":"empty"}`,
		`{"type":"D"}`,
		`{"type":"&","param":{"lhs":{"type":"name","param":"Simple"},"rhs":{"type":"desc"}}}`,
		`{"type":"otherwise","param":[{"type":"choice","param":{"name":"a","num":1,"expr":{"type":"O"}}},{"type":"M"}]}`,
		`{"type":"!","param":{"type":"==","param":{"lhs":{"type":"name","param":"A"},"rhs":{"type":"value","param":1}}}}`,
		// A tree checkAST would refuse reads its gaps as the empty
		// conformance, which no condition evaluates either.
		`{"type":"otherwise","param":[{"type":"&","param":{}}]}`,
		`{"type":"!","param":{"type":"&","param":{"lhs":{"type":"!","param":{"type":"M"}}}}}`,
	} {
		if got, err := mandatedUnder(conf(t, astJSON).root(), map[string]bool{"Simple": true}); err == nil {
			t.Errorf("mandatedUnder(%s) = %v, want an error", astJSON, got)
		}
	}
}

// TestRequirementConstraintForms holds ingestElement's reading of a
// requirement constraint to matter.js's ClusterRequirements.ingestElement
// (support/codegen/src/endpoints/ClusterRequirements.ts:169-187 at
// db021da0, read by Fable for R2-P6-par-2708): a number is taken, a
// non-numeric value states nothing and min / max are still tried; a form
// the generator does not render is refused (wizard-6b §2 gen Q1).
func TestRequirementConstraintForms(t *testing.T) {
	t.Parallel()

	g := &generator{}
	dt := &deviceType{Name: "T"}
	def := &cluster{Name: "C", Attributes: []*element{{ID: 7, Name: "A"}}}
	ingest := func(text string) (*clusterReq, error) {
		var c constraint
		if err := json.Unmarshal([]byte(text), &c); err != nil {
			t.Fatal(err)
		}
		cr := &clusterReq{def: def}
		return cr, g.ingestElement(dt, cr, &requirement{Element: "attribute", Name: "A", Constraint: &c})
	}
	cr, err := ingest(`{"text":"kWh | kVAh","value":{"type":"reference","name":"kWh"},"max":5}`)
	if err != nil || len(cr.elements) != 1 || cr.elements[0].hasMin || !cr.elements[0].hasMax || cr.elements[0].max != 5 {
		t.Errorf("a reference value beside a max: %+v %v, want max 5 alone", cr.elements, err)
	}
	cr, err = ingest(`{"text":"kWh","value":{"type":"reference","name":"kWh"}}`)
	if err != nil || len(cr.elements) != 0 {
		t.Errorf("a reference value alone: %+v %v, want nothing stated", cr.elements, err)
	}
	for _, c := range []string{
		`{"text":"26°C","value":{"type":"celsius","value":26}}`,
		`{"text":"min x","min":{"type":"bogus"}}`,
		`{"text":"max \"a\"","max":"a"}`,
	} {
		if _, err := ingest(c); err == nil {
			t.Errorf("%s: no error, want a refusal of the form", c)
		}
	}
}

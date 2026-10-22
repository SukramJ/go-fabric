// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/schema"
)

var update = flag.Bool("update", false, "rewrite the golden files")

var loadOnce = sync.OnceValues(func() (*generator, error) {
	root, err := moduleRoot()
	if err != nil {
		return nil, err
	}
	return newGenerator(root)
})

func testGenerator(t *testing.T) *generator {
	t.Helper()
	g, err := loadOnce()
	if err != nil {
		t.Fatal(err)
	}
	return g
}

// TestGolden pins the generator's output for three device types: a light
// that derives from another and specialises four clusters (DimmableLight,
// dimmable-light.ts), a simple type with a mandatory-with-extension
// cluster and a mandatory PowerSource component, which stays in the model
// (SmokeCoAlarm; requirement-coverage.ts:41-44), and a utility endpoint
// type with conditional optional servers (BridgedNode, bridged-node.ts).
// Run with -update after a deliberate generator change and read the diff.
func TestGolden(t *testing.T) {
	t.Parallel()
	g := testGenerator(t)
	for _, name := range []string{"DimmableLight", "SmokeCoAlarm", "BridgedNode"} {
		o, err := g.generate(name)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		path := filepath.Join("testdata", strings.TrimSuffix(o.File, ".go")+".go.golden")
		if *update {
			if err := os.WriteFile(path, o.Source, 0o644); err != nil { //nolint:gosec // a golden file
				t.Fatal(err)
			}
			continue
		}
		want, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("%s: %v (run go test ./script/devicegen -update)", path, err)
		}
		if !bytes.Equal(o.Source, want) {
			t.Errorf("%s differs from the generator's output; run go test ./script/devicegen -update and read the diff", path)
		}
	}
}

// TestCommittedFilesAreCurrent fails when a committed generated file under
// device/ is not what the generator makes of parity/schema.json now — a
// snapshot refreshed, or the generator changed, without regenerating (make
// generate-matter-schema, or go run ./script/devicegen) — and when device/
// holds a generated file the generator no longer writes.
func TestCommittedFilesAreCurrent(t *testing.T) {
	t.Parallel()
	g := testGenerator(t)
	root, err := moduleRoot()
	if err != nil {
		t.Fatal(err)
	}
	files, err := g.all()
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{}
	for _, o := range files {
		want[o.File] = true
		path := filepath.Join(root, outDir, o.File)
		got, err := os.ReadFile(path) //nolint:gosec // a committed file of this module
		if err != nil || !bytes.Equal(got, o.Source) {
			t.Errorf("%s is stale: run go run ./script/devicegen", path)
		}
	}
	entries, err := os.ReadDir(filepath.Join(root, outDir))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if (strings.HasSuffix(e.Name(), "_gen.go") || strings.HasSuffix(e.Name(), "_gen_test.go")) && !want[e.Name()] {
			t.Errorf("device/%s is generated but no committed device type writes it", e.Name())
		}
	}
}

// TestEveryDeviceTypeIsListed: every snapshot device type is committed or
// excluded with its Appendix F.3 group, never both; the lists name no
// device type the snapshot lacks; committed is sorted.
func TestEveryDeviceTypeIsListed(t *testing.T) {
	t.Parallel()
	g := testGenerator(t)
	if !slices.IsSorted(committed) || len(slices.Compact(slices.Clone(committed))) != len(committed) {
		t.Error("committed is not sorted and unique")
	}
	inSnapshot := map[string]bool{}
	for _, dt := range g.snap.DeviceTypes {
		inSnapshot[dt.Name] = true
		_, ex := excluded[dt.Name]
		in := slices.Contains(committed, dt.Name)
		switch {
		case in && ex:
			t.Errorf("%s is both committed and excluded", dt.Name)
		case !in && !ex:
			t.Errorf("%s is in neither committed nor excluded: decide its scope (concept Appendix F.3)", dt.Name)
		}
	}
	for _, name := range committed {
		if !inSnapshot[name] {
			t.Errorf("committed %s is not a snapshot device type", name)
		}
	}
	for name, group := range excluded {
		if !inSnapshot[name] {
			t.Errorf("excluded %s is not a snapshot device type", name)
		}
		if !strings.HasPrefix(group, "F.3 ") && !strings.Contains(group, "Appendix F.3") {
			t.Errorf("excluded %s names no Appendix F.3 group: %q", name, group)
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
	if !strings.Contains(out.String(), "dimmable_light_gen.go") {
		t.Errorf("output %q", out.String())
	}
	out.Reset()
	if err := run([]string{"-out", dir}, &out); err != nil || out.Len() != 0 {
		t.Errorf("a second run rewrote files: %q %v", out.String(), err)
	}
	cdir := filepath.Join(dir, "contract")
	if err := run([]string{"-out", dir, "-contract-out", cdir}, &out); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(cdir, contractNamesFile)); err != nil {
		t.Errorf("-contract-out: %v", err)
	}
	if err := run([]string{"-nope"}, &out); err == nil {
		t.Error("an unknown flag was accepted")
	}
	blocker := filepath.Join(dir, "file")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"-out", filepath.Join(blocker, "sub")}, &out); err == nil {
		t.Error("an output directory below a file was accepted")
	}
	if _, err := write(blocker, &output{File: "x.go"}); err == nil {
		t.Error("a write below a file was accepted")
	}
}

func TestNames(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{
		"DimmableLight": "dimmable_light", "SmokeCoAlarm": "smoke_co_alarm", "RootNode": "root_node",
		"OnOffPlugInUnit": "on_off_plug_in_unit", "EnergyEvse": "energy_evse", "OtaRequestor": "ota_requestor",
		"PM25Sensor": "pm25_sensor",
	} {
		if got := snakeName(in); got != want {
			t.Errorf("snakeName(%q) = %q, want %q", in, got, want)
		}
	}
	for in, want := range map[string]string{"a b": "AB", "3Phase": "V3Phase", "": "X", "OnOff": "OnOff"} {
		if got := exportName(in); got != want {
			t.Errorf("exportName(%q) = %q", in, got)
		}
	}
	if packageName("Switch") != "switchcluster" || packageName("OnOff") != "onoff" {
		t.Error("packageName")
	}
	if shortCommit("abc") != "abc" || shortCommit("0123456789") != "0123456" {
		t.Error("shortCommit")
	}
	for in, want := range map[string]string{"/devices/dimmable-light.ts": "DimmableLight", "/endpoints/root.ts": "RootNode", "/endpoints/aggregator.ts": "Aggregator", "/endpoints/power-source.ts": "PowerSource"} {
		g := testGenerator(t)
		if got := matterjsPath(g.snap.deviceType(want)); !strings.HasSuffix(got, in) {
			t.Errorf("matterjsPath(%s) = %s", want, got)
		}
	}
}

// conf builds a conformance from its JSON AST.
func conf(t *testing.T, astJSON string) *conformance {
	t.Helper()
	if astJSON == "" {
		return nil
	}
	var a ast
	if err := json.Unmarshal([]byte(astJSON), &a); err != nil {
		t.Fatal(err)
	}
	return &conformance{AST: &a}
}

// TestDisposition holds dispositionOf to requirement-disposition.ts
// (:98-145).
func TestDisposition(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		ast       string
		mandates  bool
		rev       int
		want      disposition
		mandatory bool
	}{
		{"", false, 1, dispUnstated, false},
		{`{"type":"X"}`, true, 1, dispDisallow, false},
		{`{"type":"Z"}`, true, 1, dispDisallow, false},
		{`{"type":"M"}`, false, 1, dispMandate, true},
		{`{"type":"P"}`, false, 1, dispProvisional, false},
		{`{"type":"O"}`, true, 1, dispRelax, false},
		{`{"type":"O"}`, false, 1, dispPermit, false},
		{`{"type":"name","param":"Foo"}`, true, 1, dispGated, false},
		{`{"type":"name","param":"Foo"}`, false, 1, dispPermit, false},
		{`{"type":"revision","param":2}`, false, 2, dispMandate, false},
		{`{"type":"revision","param":2}`, false, 1, dispPermit, false},
		{`{"type":"otherwise","param":[{"type":"revision","param":3},{"type":"O"}]}`, true, 2, dispRelax, false},
		{`{"type":"otherwise","param":[{"type":"revision","param":3},{"type":"M"}]}`, false, 2, dispMandate, true},
		{`{"type":"otherwise","param":[{"type":"revision","param":3},{"type":"P"}]}`, false, 2, dispProvisional, false},
		{`{"type":"otherwise","param":[{"type":"revision","param":3},{"type":"X"}]}`, false, 2, dispDisallow, false},
		{`{"type":"otherwise","param":[{"type":"revision","param":3},{"type":"O"}]}`, false, 2, dispPermit, false},
		{`{"type":"otherwise","param":[{"type":"revision","param":3},{"type":"name","param":"A"}]}`, true, 2, dispGated, false},
		{`{"type":"otherwise","param":[{"type":"P"},{"type":"M"}]}`, false, 1, dispProvisional, false},
		{`{"type":"otherwise","param":[{"type":"name","param":"A"},{"type":"M"}]}`, false, 1, dispMandate, true},
	} {
		c := conf(t, tc.ast)
		if got := dispositionOf(c, tc.mandates, tc.rev); got != tc.want {
			t.Errorf("%s (mandates %v, rev %d): %d, want %d", tc.ast, tc.mandates, tc.rev, got, tc.want)
		}
		if got := isMandatory(c); got != tc.mandatory {
			t.Errorf("isMandatory(%s) = %v", tc.ast, got)
		}
	}
	if (*conformance)(nil).text() != "" {
		t.Error("nil text")
	}
}

// TestMandatedUnder covers the Base-server judgement's expression forms.
func TestMandatedUnder(t *testing.T) {
	t.Parallel()
	conds := map[string]bool{"Simple": true, "Client": true}
	for astJSON, want := range map[string]bool{
		`{"type":"M"}`:                     true,
		`{"type":"O"}`:                     false,
		`{"type":"name","param":"Simple"}`: true,
		`{"type":"&","param":{"lhs":{"type":"name","param":"Simple"},"rhs":{"type":"name","param":"Client"}}}`: true,
		`{"type":"&","param":{"lhs":{"type":"name","param":"Simple"},"rhs":{"type":"name","param":"Node"}}}`:   false,
		`{"type":"|","param":{"lhs":{"type":"name","param":"Node"},"rhs":{"type":"name","param":"Client"}}}`:   true,
		`{"type":"!","param":{"type":"name","param":"Node"}}`:                                                  true,
		`{"type":"otherwise","param":[{"type":"name","param":"Node"},{"type":"M"}]}`:                           true,
		`{"type":"otherwise","param":[{"type":"name","param":"Simple"},{"type":"O"}]}`:                         true,
		`{"type":"otherwise","param":[{"type":"name","param":"Node"},{"type":"O"}]}`:                           false,
		`{"type":"otherwise","param":[{"type":"&","param":{}}]}`:                                               false,
		`{"type":"&","param":{"lhs":{"type":"|","param":{}},"rhs":{"type":"!","param":7}}}`:                    false,
		`{"type":"!","param":{"type":"&","param":{"lhs":{"type":"!","param":{"type":"M"}}}}}`:                  true,
	} {
		if got := mandatedUnder(conf(t, astJSON).root(), conds); got != want {
			t.Errorf("mandatedUnder(%s) = %v", astJSON, got)
		}
	}
}

// TestRequirementLost: a requirement the generator cannot carry fails the
// run, as matter.js's reportRequirementLost fails its build.
func TestRequirementLost(t *testing.T) {
	t.Parallel()
	base := func() *snapshot {
		raw := `{"deviceTypes":[{"id":4096,"name":"T","classification":"simple","revision":1,"effective":{"composition":"tree","requirements":[
			{"element":"serverCluster","name":"C","id":1,"conformance":{"text":"M","ast":{"type":"M"}},"referent":{"id":1,"name":"C"},"requirements":[]}]}}],
			"clusters":[{"id":1,"name":"C","classification":"application","features":[{"name":"F","bit":0,"effective":{"title":"Feat"}}],
			"attributes":[{"id":0,"name":"A","effective":{}}],"commands":[],"events":[]}]}`
		s, err := parseSnapshot([]byte(raw))
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	nested := func(s *snapshot, r *requirement) {
		s.DeviceTypes[0].Effective.Requirements[0].Requirements = []*requirement{r}
	}
	for name, tc := range map[string]struct {
		mutate func(*snapshot)
		want   string
	}{
		"unknown cluster": {func(s *snapshot) { s.Clusters = nil }, "unknown cluster C"},
		"unknown kind": {func(s *snapshot) {
			s.DeviceTypes[0].Effective.Requirements = append(s.DeviceTypes[0].Effective.Requirements, &requirement{Element: "nope", Name: "N"})
		}, "no generator claims"},
		"unknown feature": {func(s *snapshot) { nested(s, &requirement{Element: "feature", Name: "G"}) }, "unknown feature G"},
		"unknown element": {func(s *snapshot) { nested(s, &requirement{Element: "attribute", Name: "B"}) }, "unknown attribute B"},
		"nested kind":     {func(s *snapshot) { nested(s, &requirement{Element: "commandField", Name: "X"}) }, "commandField requirement X"},
		"default": {func(s *snapshot) {
			nested(s, &requirement{Element: "attribute", Name: "A", Default: json.RawMessage(`"x"`)})
		}, "is not numeric"},
	} {
		s := base()
		tc.mutate(s)
		g := &generator{snap: s, defs: map[string]bool{}}
		if _, err := g.generate("T"); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: %v, want %q", name, err, tc.want)
		}
	}
	s := base()
	g := &generator{snap: s, defs: map[string]bool{}}
	if _, err := g.generate("Nope"); err == nil {
		t.Error("an unknown device type generated")
	}
	s.DeviceTypes[0].ID = nil
	if _, err := g.generate("T"); err == nil {
		t.Error("a device type without id generated")
	}
	if _, err := g.generateRegistry([]string{"T"}); err == nil {
		t.Error("a registry entry without id generated")
	}
	if _, err := g.generateTest([]string{"T"}); err == nil {
		t.Error("a test for a device type without id generated")
	}
	if !errors.Is(func() error {
		s := base()
		s.Clusters = nil
		_, err := (&generator{snap: s}).generate("T")
		return err
	}(), errRequirementLost) {
		t.Error("an unknown cluster does not wrap errRequirementLost")
	}
	if _, err := parseSnapshot([]byte(`{}`)); err == nil {
		t.Error("an empty snapshot parsed")
	}
	if _, err := parseSnapshot([]byte(`{`)); err == nil {
		t.Error("a broken snapshot parsed")
	}
	if _, err := loadSnapshot(filepath.Join(t.TempDir(), "none.json")); err == nil {
		t.Error("a missing snapshot loaded")
	}
}

// TestSyntheticElements covers the element alterations no committed type
// states: a relaxed element, a stated default, a constraint with bounds
// only, and a client cluster's mandated feature.
func TestSyntheticElements(t *testing.T) {
	t.Parallel()
	raw := `{"deviceTypes":[{"id":4096,"name":"T","classification":"utility","revision":1,"effective":{"composition":"full-family","base":"B","conditions":["K"],"requirements":[
		{"element":"serverCluster","name":"C","id":1,"conformance":{"text":"O","ast":{"type":"O"}},"referent":{"id":1,"name":"C"},"requirements":[
			{"element":"attribute","name":"A","conformance":{"text":"O","ast":{"type":"O"}}},
			{"element":"attribute","name":"B","default":5},
			{"element":"command","name":"K","constraint":{"text":"2 to 9","min":2,"max":9}},
			{"element":"event","name":"E","conformance":{"text":"M","ast":{"type":"M"}}},
			{"element":"attribute","name":"N","constraint":{"text":"x","value":{"ref":"y"}}}]},
		{"element":"clientCluster","name":"C","id":1,"conformance":{"text":"M","ast":{"type":"M"}},"referent":{"id":1,"name":"C"},"requirements":[
			{"element":"feature","name":"F","conformance":{"text":"M","ast":{"type":"M"}}}]},
		{"element":"serverCluster","name":"Descriptor","id":29,"referent":{"id":29,"name":"Descriptor"},"requirements":[
			{"element":"feature","name":"TAGLIST","conformance":{"text":"M","ast":{"type":"M"}}}]}]}}],
		"clusters":[{"id":1,"name":"C","classification":"application","features":[{"name":"F","bit":2,"effective":{"title":"Feat"}}],
		"attributes":[{"id":0,"name":"A","effective":{"conformance":{"text":"M","ast":{"type":"M"}}}},{"id":1,"name":"B","effective":{}},{"id":2,"name":"N","effective":{}}],
		"commands":[{"id":3,"name":"K","effective":{}}],"events":[{"id":4,"name":"E","effective":{}}]},
		{"id":29,"name":"Descriptor","classification":"endpoint","features":[{"name":"TAGLIST","bit":0,"effective":{"title":"TagList"}}],"attributes":[],"commands":[],"events":[]}]}`
	s, err := parseSnapshot([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	g := &generator{snap: s, defs: map[string]bool{"c": true}}
	o, err := g.generate("T")
	if err != nil {
		t.Fatal(err)
	}
	src := string(o.Source)
	for _, want := range []string{
		`Name: "A", ID: 0x0000, Presence: PresenceOptional`,
		`Name: "B", ID: 0x0001, HasDefault: true, Default: 5}`,
		`Name: "K", ID: 0x0003, HasMin: true, Min: 2, HasMax: true, Max: 9}`,
		`Name: "E", ID: 0x0004, Presence: PresenceMandatory}`,
		`0x4, FeatureNames: []string{"F"}`,
		`Composition: schema.CompositionFullFamily`,
		`Conditions: []string{"K"}`,
		`ID: 0x001D, Name: "Descriptor"`,
		`Definition: cdef.Definition`,
		`func (d *T) C() *cdef.Server { return cdef.WrapOn(d.Server(0x0001), d.queue) }`,
	} {
		if !strings.Contains(src, want) {
			t.Errorf("generated source lacks %q:\n%s", want, src)
		}
	}
	if strings.Contains(src, `Name: "N"`) {
		t.Error("a non-numeric constraint value was carried")
	}
	if !strings.Contains(matterjsPath(s.DeviceTypes[0]), "/endpoints/t.ts") {
		t.Error("a utility type is not an endpoints/ file")
	}
	// An accessor that would shadow a Device method is refused.
	s.Clusters[0].Name = "Spec"
	g.defs = map[string]bool{"spec": true}
	if _, err := g.generate("T"); err == nil || !strings.Contains(err.Error(), "shadows") {
		t.Errorf("shadowing accessor: %v", err)
	}
}

// TestReferenceIsCurrent fails when docs/reference/device-types.md is not
// what the generator makes of parity/schema.json now.
func TestReferenceIsCurrent(t *testing.T) {
	t.Parallel()
	g := testGenerator(t)
	root, err := moduleRoot()
	if err != nil {
		t.Fatal(err)
	}
	o, err := g.generateReference(committed)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, referenceDir, o.File)
	got, err := os.ReadFile(path) //nolint:gosec // a committed file of this module
	if err != nil || !bytes.Equal(got, o.Source) {
		t.Errorf("%s is stale: run go run ./script/devicegen", path)
	}
}

// TestContractNamesAreCurrent fails when contract/devicetype_names_gen.go
// is not what the generator makes of parity/schema.json now.
func TestContractNamesAreCurrent(t *testing.T) {
	t.Parallel()
	g := testGenerator(t)
	root, err := moduleRoot()
	if err != nil {
		t.Fatal(err)
	}
	o, err := g.generateContractNames()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, contractDir, o.File)
	got, err := os.ReadFile(path) //nolint:gosec // a committed file of this module
	if err != nil || !bytes.Equal(got, o.Source) {
		t.Errorf("%s is stale: run go run ./script/devicegen", path)
	}
}

// TestLabelOverrides: every override names a snapshot device type and
// differs from its matter.js name — an override equal to the name, or for
// a type the snapshot dropped, is dead and goes.
func TestLabelOverrides(t *testing.T) {
	t.Parallel()
	g := testGenerator(t)
	names := map[uint32]string{}
	for _, dt := range g.snap.DeviceTypes {
		if dt.ID != nil {
			names[*dt.ID] = dt.Name
		}
	}
	for id, label := range labelOverrides {
		name, ok := names[id]
		switch {
		case !ok:
			t.Errorf("override 0x%04X (%q) names no snapshot device type", id, label)
		case name == label:
			t.Errorf("override 0x%04X equals the matter.js name %q", id, name)
		}
	}
}

// TestContractDeviceTypeNameCoversEverySchemaType: every device type of
// schema/devicetypes.go has a contract label — its matter.js name, or its
// override — never the hex fallback (issue #57).
func TestContractDeviceTypeNameCoversEverySchemaType(t *testing.T) {
	t.Parallel()
	if len(schema.DeviceTypeNames) == 0 {
		t.Fatal("schema.DeviceTypeNames is empty")
	}
	for id, name := range schema.DeviceTypeNames {
		want := name
		if o, ok := labelOverrides[uint32(id)]; ok {
			want = o
		}
		if got := contract.DeviceTypeName(uint16(id)); got != want { //nolint:gosec // generateContractNames refuses an id above uint16
			t.Errorf("contract.DeviceTypeName(0x%04X) = %q, want %q", id, got, want)
		}
	}
}

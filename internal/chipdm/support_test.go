// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package chipdm

import (
	"archive/tar"
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestLoadSnapshotShapes(t *testing.T) {
	t.Parallel()
	doc := `{
  "matter": {"revision": "1.6.1", "sourceCommit": "abc"},
  "clusters": [{
    "id": 6, "name": "OnOff", "revision": 6,
    "features": [{"name": "LT", "bit": 0, "effective": {"conformance": {"text": "O", "ast": {"type": "O"}}}}],
    "attributes": [{"id": 0, "name": "OnOff", "effective": {"type": "bool", "quality": {"nonvolatile": true, "scene": false},
      "default": null, "entry": {"type": "uint8"}}}],
    "commands": [
      {"id": 0, "name": "Off", "effective": {"response": "status"}},
      {"id": 1, "name": "OffResponse", "effective": {}},
      {"id": 2, "name": "Explicit", "direction": "response", "effective": {}}
    ],
    "events": [{"id": 0, "name": "Ev", "effective": {"priority": "info", "fields": [{"id": 0, "name": "F", "default": 3}]}}],
    "datatypes": [
      {"name": "E", "metatype": "enum", "fields": []},
      {"name": "B", "metatype": "bitmap"},
      {"name": "S", "metatype": "object"},
      {"name": "N", "metatype": "integer"}
    ]
  }],
  "deviceTypes": [{"id": 256, "name": "OnOffLight", "revision": 3, "classification": "simple", "requirements": [
    {"id": 6, "name": "OnOff", "element": "serverCluster", "conformance": "M"},
    {"id": 6, "name": "OnOff", "element": "clientCluster", "conformance": "O"},
    {"id": 17, "name": "PowerSource", "element": "deviceType", "conformance": "O"}
  ]}],
  "globalDatatypes": [{"name": "status", "metatype": "enum", "fields": [{"id": 0, "name": "Success"}]}]
}`
	m, info, err := LoadSnapshot([]byte(doc))
	if err != nil {
		t.Fatal(err)
	}
	if info.Revision != "1.6.1" || info.SourceCommit != "abc" {
		t.Errorf("info = %+v", info)
	}
	c := m.Clusters[0]
	if c.Features[0].Constraint != "0" || c.Features[0].Conformance.Key() != "o" {
		t.Errorf("feature = %+v", c.Features[0])
	}
	a := c.Attributes[0]
	if a.Default.Kind != "null" || strings.Join(a.Quality, ",") != "nonvolatile" || a.EntryType != "uint8" {
		t.Errorf("attribute = %+v", a)
	}
	if c.Commands[0].Direction != "request" || c.Commands[1].Direction != "response" || c.Commands[2].Direction != "response" {
		t.Errorf("directions = %s %s %s", c.Commands[0].Direction, c.Commands[1].Direction, c.Commands[2].Direction)
	}
	if f := c.Events[0].Fields[0]; f.Default.Text != "3" {
		t.Errorf("event field = %+v", f)
	}
	kinds := make([]string, 0, len(c.Datatypes))
	for _, d := range c.Datatypes {
		kinds = append(kinds, d.Kind)
	}
	if strings.Join(kinds, ",") != "enum,bitmap,struct," {
		t.Errorf("kinds = %v", kinds)
	}
	if r := m.DeviceTypes[0].Requirements; r[0].Side != "server" || r[1].Side != "client" || r[2].Side != "deviceType" {
		t.Errorf("requirements = %+v", r)
	}
	if len(m.Globals) != 1 || m.Globals[0].Kind != "enum" {
		t.Errorf("globals = %+v", m.Globals)
	}
}

func TestLoadSnapshotErrors(t *testing.T) {
	t.Parallel()
	cluster := func(body string) string {
		return `{"clusters":[{"id":1,"name":"C",` + body + `}]}`
	}
	for name, doc := range map[string]string{
		"json":                `{`,
		"feature conformance": cluster(`"features":[{"name":"F","effective":{"conformance":{"ast":{"type":"bogus"}}}}]`),
		"attribute":           cluster(`"attributes":[{"id":0,"name":"A"}]`),
		"attribute value":     cluster(`"attributes":[{"id":0,"name":"A","effective":{"conformance":{"ast":{"type":"bogus"}}}}]`),
		"command":             cluster(`"commands":[{"id":0,"name":"X"}]`),
		"event":               cluster(`"events":[{"id":0,"name":"X"}]`),
		"datatype":            cluster(`"datatypes":[{"name":"D","fields":[{"name":"F","conformance":{"ast":{"type":"bogus"}}}]}]`),
		"default":             cluster(`"attributes":[{"id":0,"name":"A","effective":{"default":{"type":"celsius"}}}]`),
		"entry":               cluster(`"attributes":[{"id":0,"name":"A","effective":{"entry":{"default":{"type":"percent","value":"x"}}}}]`),
		"requirement element": `{"deviceTypes":[{"id":1,"name":"D","requirements":[{"id":1,"name":"R","element":"condition"}]}]}`,
		"requirement conf":    `{"deviceTypes":[{"id":1,"name":"D","requirements":[{"id":1,"name":"R","element":"serverCluster","conformance":"[A"}]}]}`,
		"global":              `{"globalDatatypes":[{"name":"G","conformance":{"ast":{"type":"bogus"}}}]}`,
	} {
		if _, _, err := LoadSnapshot([]byte(doc)); err == nil {
			t.Errorf("%s: LoadSnapshot accepted %s", name, doc)
		}
	}
}

func TestSnapshotDefault(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]Value{
		`null`: {"null", "null"}, `true`: {"bool", "true"}, `7`: {"number", "7"}, `"18446744073709551615"`: {"number", "18446744073709551615"},
		`"XX"`: {"string", "XX"}, `[]`: {"list", "[]"}, `{"type":"reference","name":"Auto"}`: {"reference", "Auto"},
		`{"type":"celsius","value":7}`: {"celsius", "7"}, `{"type":"percent","value":0.5}`: {"percent", "0.5"},
		`{"type":"bytes","value":"00"}`: {"bytes", "00"}, `{"a":1}`: {"object", `{"a":1}`},
	} {
		got, err := snapshotDefault([]byte(in))
		if err != nil || *got != want {
			t.Errorf("snapshotDefault(%s) = %+v, %v; want %+v", in, got, err, want)
		}
	}
	for _, in := range []string{`{`, `{"type":"percent"}`} {
		if _, err := snapshotDefault([]byte(in)); err == nil {
			t.Errorf("snapshotDefault(%s) succeeded", in)
		}
	}
}

func TestDataModelVersion(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{"1.6.1": "1.6.1", "1.6.0": "1.6", "1.4": "1.4", "1.5.0.1": "1.5"} {
		if got := DataModelVersion(in); got != want {
			t.Errorf("DataModelVersion(%q) = %q, want %q", in, got, want)
		}
	}
}

func tarOf(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := tar.NewWriter(&buf)
	if err := w.WriteHeader(&tar.Header{Name: "data_model/1.6.1/", Typeflag: tar.TypeDir, Mode: 0o755}); err != nil {
		t.Fatal(err)
	}
	for name, body := range files {
		if err := w.WriteHeader(&tar.Header{Name: name, Typeflag: tar.TypeReg, Mode: 0o644, Size: int64(len(body))}); err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// TestReadChipModel stands in for git: rev-parse answers the tree, archive
// the fixture under data_model/1.6.1/.
func TestReadChipModel(t *testing.T) { //nolint:paralleltest // replaces the package's git runner
	files := map[string]string{
		"data_model/1.6.1/spec_tag": "1.6.1-test\n", "data_model/1.6.1/spec_sha": "abc",
		"data_model/1.6.1/scraper_version": "alchemy v1", "elsewhere/x.xml": "<x/>",
	}
	for k, v := range fixture {
		files["data_model/1.6.1/"+k] = v
	}
	archive := tarOf(t, files)
	saved := runGit
	t.Cleanup(func() { runGit = saved })

	var failArchive, failTree bool
	runGit = func(root string, args ...string) ([]byte, error) {
		switch args[0] {
		case "rev-parse":
			if failTree {
				return nil, errors.New("no such commit")
			}
			return []byte("tree123\n"), nil
		case "archive":
			if failArchive {
				return nil, errors.New("archive failed")
			}
			return archive, nil
		}
		return nil, errors.New("unexpected git call")
	}
	x, err := ReadChipModel("/chip", "c0ffee", "1.6.1")
	if err != nil {
		t.Fatal(err)
	}
	p := x.Provenance
	if p.Commit != "c0ffee" || p.Tree != "tree123" || p.SpecTag != "1.6.1-test" || p.SpecSHA != "abc" || p.Scraper != "alchemy v1" ||
		p.DataModel != "1.6.1" || p.Repository != Repository {
		t.Errorf("provenance = %+v", p)
	}
	failArchive = true
	if _, err := ReadChipModel("/chip", "c0ffee", "1.6.1"); err == nil {
		t.Error("a failing archive was accepted")
	}
	failArchive, failTree = false, true
	if _, err := ReadChipModel("/chip", "c0ffee", "1.6.1"); err == nil {
		t.Error("a missing commit was accepted")
	}
	failTree = false
	archive = []byte("not a tar archive, but long enough to be read as one header block ......" + strings.Repeat("x", 600))
	if _, err := ReadChipModel("/chip", "c0ffee", "1.6.1"); err == nil {
		t.Error("a corrupt archive was accepted")
	}
	archive = tarOf(t, map[string]string{"data_model/1.6.1/clusters/X.xml": "<cluster"})
	if _, err := ReadChipModel("/chip", "c0ffee", "1.6.1"); err == nil {
		t.Error("malformed XML was accepted")
	}
}

func TestRunGit(t *testing.T) {
	t.Parallel()
	if _, err := runGit(t.TempDir(), "rev-parse", "--verify", "--quiet", "HEAD"); err == nil {
		t.Error("git found a commit in an empty directory")
	}
	if _, err := TreeID(t.TempDir(), "HEAD", "1.6.1"); err == nil {
		t.Error("TreeID succeeded outside a checkout")
	}
}

func TestReconcile(t *testing.T) {
	t.Parallel()
	acks := []Acknowledgement{
		{Class: ClassMatterJS, Path: "*.Response.Field", Property: "conformance", Chip: "a", Ours: "b"},
		{Class: ClassCHIP, Path: "Cluster.*.Member", Property: "type", Chip: "x", Ours: "y"},
		{Class: ClassHarness, Path: "Exact.Path", Property: "default", Chip: "1", Ours: "2"},
		{Class: ClassMatterJS, Path: "Never.Matches", Property: "default", Chip: "1", Ours: "2"},
	}
	diffs := []Difference{
		{Path: "C1.Response.Field", Property: "conformance", Chip: "a", Ours: "b"},
		{Path: "Response.Field", Property: "conformance", Chip: "a", Ours: "b"},    // the wildcard needs an enclosing element
		{Path: "C1.Other.Field", Property: "conformance", Chip: "a", Ours: "b"},    // a segment differs
		{Path: "C1.Response.Field", Property: "conformance", Chip: "a", Ours: "c"}, // a value differs
		{Path: "cluster.Anything.member", Property: "type", Chip: "x", Ours: "y"},  // canonical, segment wildcard
		{Path: "Cluster.Member", Property: "type", Chip: "x", Ours: "y"},           // segment count differs
		{Path: "Exact.Path", Property: "default", Chip: "1", Ours: "2"},
		{Path: "Exact.Path", Property: "constraint", Chip: "1", Ours: "2"}, // the property differs
	}
	r := Reconcile(diffs, acks)
	if len(r.Explained[0]) != 1 || len(r.Explained[1]) != 1 || len(r.Explained[2]) != 1 {
		t.Errorf("explained = %v", r.Explained)
	}
	if len(r.Unexplained) != 5 {
		t.Errorf("unexplained = %v", r.Unexplained)
	}
	if len(r.Stale) != 1 || r.Stale[0].Path != "Never.Matches" {
		t.Errorf("stale = %v", r.Stale)
	}
	counts := r.CountByClass(acks)
	if counts[ClassMatterJS] != 1 || counts[ClassCHIP] != 1 || counts[ClassHarness] != 1 {
		t.Errorf("counts = %v", counts)
	}
}

func TestRender(t *testing.T) {
	t.Parallel()
	acks := []Acknowledgement{
		{ClassMatterJS, "A.B", "default", "1", "2", "a | reason", "src", ""},
		{ClassCHIP, "C.D", "type", "x", "y", "why", "where", "none"},
		{ClassHarness, "E.F", "command", "absent", "present", "harness", "spec_parsing.py", ""},
	}
	res := &Result{
		Compared:    map[string]int{"cluster": 2},
		Normalized:  map[string]int{"type-alias": 3},
		NotCompared: map[string]int{"semantic namespaces": 4},
		Provisional: []string{"TemperatureAlarm"},
	}
	rec := Reconcile([]Difference{
		{Path: "A.B", Property: "default", Chip: "1", Ours: "2"},
		{Path: "C.D", Property: "type", Chip: "x", Ours: "y"},
	}, acks)
	out := ReportInput{
		Snapshot: SnapshotInfo{Revision: "1.6.1", SourceCommit: "m"}, SnapshotSHA256: "s",
		Chip: Provenance{Commit: "c", DataModel: "1.6.1"}, HarnessCommit: "other",
		Result: res, Reconciliation: rec, Acknowledged: acks,
	}.Render()
	for _, want := range []string{
		"**a different commit** (`other`)", "| cluster | 2 |", "| **total** | **2** |",
		"| (iii) representation, normalized in code | 3 |", "| unexplained | 0 |",
		"| semantic namespaces | 4 |", "- TemperatureAlarm\n", `a \| reason`, "- **C.D** (type): CHIP `x`, snapshot `y` — 1 difference(s).",
		"| E.F | command | `absent` | `present` | 0 |",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("report lacks %q:\n%s", want, out)
		}
	}
}

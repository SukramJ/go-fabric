// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// fixtureClusters are the generated packages of the clusters
// bridge/testdata/application-wire-fixtures.json has matter.js encodings
// for.
var fixtureClusters = []string{
	"fancontrol", "smokecoalarm", "pumpconfigurationandcontrol", "switchcluster", "operationalstate",
	"rvcoperationalstate", "rvcrunmode", "laundrywashermode", "dishwashermode", "rvccleanmode",
}

// fixtureTest round-trips every matter.js wire fixture through the
// generated codecs: the fixture's bytes decode with the definition's
// decoder for the element, and the decoded value encodes back to the same
// bytes.
const fixtureTest = `// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package fixtures_test

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"os"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/tlv"
%s)

func TestMatterJSFixtures(t *testing.T) {
	raw, err := os.ReadFile(%q)
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Label    string ` + "`json:\"label\"`" + `
		Kind     string ` + "`json:\"kind\"`" + `
		Cluster  uint32 ` + "`json:\"cluster\"`" + `
		Element  string ` + "`json:\"element\"`" + `
		BytesHex string ` + "`json:\"bytesHex\"`" + `
	}
	if err := json.Unmarshal(raw, &fixtures); err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, f := range fixtures {
		def := spec.Lookup(f.Cluster)
		if def == nil {
			t.Errorf("%%s: cluster 0x%%04X has no generated definition", f.Label, f.Cluster)
			continue
		}
		var decode func(spec.Node) (any, error)
		switch f.Kind {
		case "commands":
			if c := def.CommandByName(f.Element); c != nil {
				decode = c.Decode
			}
		case "events":
			for i := range def.Events {
				if def.Events[i].Name == f.Element {
					decode = def.Events[i].Decode
				}
			}
		case "attributes":
			if a := def.AttributeByName(f.Element); a != nil {
				decode = a.Decode
			}
		}
		if decode == nil {
			continue // a scalar or scalar-list attribute: no generated type
		}
		want, err := hex.DecodeString(f.BytesHex)
		if err != nil {
			t.Fatal(err)
		}
		n, err := spec.ReadElement(tlv.NewDecoder(want))
		if err != nil {
			t.Errorf("%%s: %%v", f.Label, err)
			continue
		}
		v, err := decode(n)
		if err != nil {
			t.Errorf("%%s: decode: %%v", f.Label, err)
			continue
		}
		enc := tlv.NewEncoder()
		v.(spec.Encodable).EncodeTLV(enc, tlv.AnonymousTag())
		got, err := enc.Bytes()
		if err != nil || !bytes.Equal(got, want) {
			t.Errorf("%%s: %%T re-encodes as %%X, matter.js %%X (%%v)", f.Label, v, got, want, err)
		}
		checked++
	}
	if checked < 40 {
		t.Errorf("only %%d fixtures checked", checked)
	}
}
`

// TestEveryClusterGeneratesAndCompiles generates every cluster of the
// snapshot into a scratch module that replaces this one, vets it — which
// compiles every package and its generated test — and runs the generated
// tests of the clusters matter.js wire fixtures exist for, together with a
// test that round-trips each fixture through the generated codecs.
func TestEveryClusterGeneratesAndCompiles(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles every cluster; skipped in -short")
	}
	t.Parallel()
	root, err := moduleRoot()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := run([]string{"-all", "-out", dir}, &strings.Builder{}); err != nil {
		t.Fatal(err)
	}
	snap := testSnapshot(t)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != len(snap.Clusters) {
		t.Fatalf("%d packages for %d clusters", len(entries), len(snap.Clusters))
	}
	goMod := fmt.Sprintf("module gentest\n\ngo 1.27.2\n\nrequire github.com/SukramJ/go-fabric v0.0.0\n\nreplace github.com/SukramJ/go-fabric => %s\n", root)
	sum, err := os.ReadFile(filepath.Join(root, "go.sum"))
	if err != nil {
		t.Fatal(err)
	}
	var imports strings.Builder
	for _, p := range fixtureClusters {
		fmt.Fprintf(&imports, "\t_ \"gentest/%s\"\n", p)
	}
	files := map[string]string{
		"go.mod":                    goMod,
		"go.sum":                    string(sum),
		"fixtures/fixtures_test.go": fmt.Sprintf(fixtureTest, imports.String(), filepath.Join(root, "bridge", "testdata", "application-wire-fixtures.json")),
	}
	for name, content := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	gotool, err := exec.LookPath("go")
	if err != nil {
		t.Skip("no go tool on PATH")
	}
	env := append(os.Environ(), "GOFLAGS=-mod=mod", "GOPROXY=off", "GOWORK=off")
	test := make([]string, 0, 3+len(fixtureClusters))
	test = append(test, "test", "-count=1", "./fixtures")
	for _, p := range fixtureClusters {
		test = append(test, "./"+p)
	}
	for _, args := range [][]string{{"vet", "./..."}, test} {
		cmd := exec.CommandContext(t.Context(), gotool, args...) //nolint:gosec // the go tool of this toolchain, fixed arguments
		cmd.Dir = dir
		cmd.Env = env
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("go %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
}

// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package schema_test

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/SukramJ/go-fabric/parity"
	"github.com/SukramJ/go-fabric/schema"
)

// matterJSElementsDir returns the standard-elements directory of a sibling
// matter.js checkout (../matter.js relative to the module root) and whether
// it exists. The extractor that produced the pinned snapshot walks the same
// MatterDefinition tree these files build.
//
// The module root is found by walking up to go.mod: counting parent
// directories would silently resolve to an unrelated tree the day this file
// moves, and the failure would look like "no matter.js checkout" rather
// than a wrong path.
func matterJSElementsDir() (string, bool) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		return "", false
	}
	root := filepath.Dir(thisFile)
	for {
		if _, err := os.Stat(filepath.Join(root, "go.mod")); err == nil {
			break
		}
		parent := filepath.Dir(root)
		if parent == root {
			return "", false
		}
		root = parent
	}
	dir := filepath.Join(root, "..", "matter.js", "packages", "model", "src", "standard", "elements")
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		return "", false
	}
	return dir, true
}

var (
	// reClusterHeader captures the cluster name + id from the element header,
	// e.g. `= Cluster(\n    { name: "OnOff", id: 0x6, ...`.
	reClusterHeader = regexp.MustCompile(`Cluster\(\s*\{\s*name:\s*"([^"]+)",\s*id:\s*(0x[0-9a-fA-F]+)`)
	// reClusterRevision captures the ClusterRevision (0xFFFD) default, e.g.
	// `Attribute({ name: "ClusterRevision", id: 0xfffd, type: "ClusterRevision", default: 6 })`.
	reClusterRevision = regexp.MustCompile(`name:\s*"ClusterRevision",\s*id:\s*0xfffd[^}]*?default:\s*(\d+)`)
)

// parseLiveClusterRevisions reads every *.element.ts in dir and returns the
// live ClusterRevision default per cluster ID. Type-inheriting clusters that
// declare no inline ClusterRevision (a handful in the ConcentrationMeasurement /
// ResourceMonitoring families) are skipped: their revision cannot be read
// from the element source, so the diff covers the clusters that state one.
func parseLiveClusterRevisions(dir string) (map[uint32]uint16, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	live := make(map[uint32]uint16, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".element.ts") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		hdr := reClusterHeader.FindSubmatch(raw)
		if hdr == nil {
			continue // not a cluster element (datatype, device type, ...)
		}
		id64, err := strconv.ParseUint(string(hdr[2]), 0, 32)
		if err != nil {
			continue
		}
		rev := reClusterRevision.FindSubmatch(raw)
		if rev == nil {
			continue // revision inherited from a base type — not inline here
		}
		r64, err := strconv.ParseUint(string(rev[1]), 10, 16)
		if err != nil {
			continue
		}
		live[uint32(id64)] = uint16(r64)
	}
	return live, nil
}

// pinnedSnapshotProvenance reads the matter block of the pinned snapshot so
// the staleness report can name the pinned spec revision + matter.js source
// commit. It reads the embedded copy — the same bytes every parity test
// validates against — so the report cannot name a revision no test used.
func pinnedSnapshotProvenance() (revision, commit string) {
	revision, commit = "unknown", "unknown"
	raw := parity.SchemaJSON()
	var s struct {
		Matter struct {
			Revision     string `json:"revision"`
			SourceCommit string `json:"sourceCommit"`
		} `json:"matter"`
	}
	if err := json.Unmarshal(raw, &s); err != nil {
		return revision, commit
	}
	if s.Matter.Revision != "" {
		revision = s.Matter.Revision
	}
	if s.Matter.SourceCommit != "" {
		commit = s.Matter.SourceCommit
	}
	return revision, commit
}

// gitHead returns the HEAD commit of the checkout containing dir, or "unknown".
func gitHead(dir string) string {
	out, err := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(out))
}

func shortCommit(c string) string {
	if len(c) > 12 {
		return c[:12]
	}
	return c
}

// TestMatterJSSchemaStalenessAgainstLiveCheckout makes upstream schema
// staleness visible. It compares the pinned per-cluster ClusterRevision
// (schema.ClusterRevisions, generated from parity/schema.json) of every
// cluster of the snapshot — the module serves all of them — against a live
// ../matter.js checkout's element sources and reports every cluster whose
// revision has increased upstream: the drift the pin would otherwise hide.
//
// The snapshot bump is a MANUAL decision (CLAUDE.md, "Regenerate the Matter
// schema"): a revision increment can carry attribute, constraint, or command
// changes that need review, so this guard only surfaces the drift, it does
// not perform the bump. Behaviour:
//
//   - ../matter.js absent -> Skip (a local tooling check; CI has no live
//     checkout, only the pinned snapshot).
//   - No cluster drifted -> Pass (the checkout is at the pin, or upstream
//     moved no revision).
//   - Drift found -> an advisory Skip listing exactly which clusters
//     drifted. A checkout ahead of the pin is the ordinary state between
//     two pin lifts, so it is not a failure; the pin lift reads this list
//     (notes/parity/pin-lifts.md).
//
// Mirrors the extraction the parity snapshot performs in
// script/extract-from-matter-js.ts: the ClusterRevision (attribute
// 0xFFFD) default in
// ../matter.js/packages/model/src/standard/elements/*.element.ts.
func TestMatterJSSchemaStalenessAgainstLiveCheckout(t *testing.T) {
	t.Parallel()

	dir, ok := matterJSElementsDir()
	if !ok {
		t.Skip("../matter.js checkout not present — schema staleness is a local/scheduled tooling guard")
	}

	live, err := parseLiveClusterRevisions(dir)
	if err != nil {
		t.Fatalf("parse live matter.js element revisions: %v", err)
	}
	if len(live) == 0 {
		t.Skipf("no cluster revisions parsed from %s — matter.js element layout may have changed", dir)
	}

	pinnedRev, pinnedCommit := pinnedSnapshotProvenance()
	liveCommit := gitHead(dir)

	type drift struct {
		id            uint32
		name          string
		pinned, upsts uint16
	}
	var drifts []drift
	ids := make([]uint32, 0, len(schema.ClusterRevisions))
	for id := range schema.ClusterRevisions {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })

	for _, id := range ids {
		liveRev, present := live[id]
		if !present {
			// The cluster inherits its revision from a base type (not
			// inline): it cannot be compared from the element source.
			continue
		}
		if pinnedR := schema.ClusterRevisions[id]; liveRev > pinnedR {
			name, _ := schema.ClusterName(id)
			drifts = append(drifts, drift{id: id, name: name, pinned: pinnedR, upsts: liveRev})
		}
	}

	if len(drifts) == 0 {
		t.Logf("matter.js schema pin is current for all %d clusters (pinned matter %s, commit %s)",
			len(ids), pinnedRev, shortCommit(pinnedCommit))
		return
	}

	var b strings.Builder
	fmt.Fprintf(&b, "matter.js HEAD advanced %d cluster revision(s) past the pinned snapshot\n", len(drifts))
	fmt.Fprintf(&b, "  pinned snapshot: matter %s (commit %s)\n", pinnedRev, shortCommit(pinnedCommit))
	fmt.Fprintf(&b, "  live checkout:   %s (commit %s)\n", dir, shortCommit(liveCommit))
	for _, d := range drifts {
		fmt.Fprintf(&b, "  0x%04X %-40s pinned=%d  upstream=%d\n", d.id, d.name, d.pinned, d.upsts)
	}
	b.WriteString("Adopting the newer schema is a manual review (a pin lift): `make generate-matter-schema` " +
		"after deciding to bump, then reconcile the hand-stated revision constants and re-run the parity tests.")
	t.Log(b.String())
	t.Skipf("matter.js schema pin is behind the checkout: %d cluster(s) drifted upstream", len(drifts))
}

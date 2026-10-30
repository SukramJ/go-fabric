// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"os"
	"regexp"
	"testing"

	"github.com/SukramJ/go-fabric/surface"
)

// TestCommittedSnapshot: the committed surface parses, the digest the
// summary states is its effective surface's, the summary is what the
// report renders to, and the surface does not differ from itself. No
// ratchet constant: the snapshot is the ratchet (make surface-check).
func TestCommittedSnapshot(t *testing.T) {
	raw, err := os.ReadFile("surface.json")
	if err != nil {
		t.Fatal(err)
	}
	md, err := os.ReadFile("summary.md")
	if err != nil {
		t.Fatal(err)
	}
	var rep surface.Report
	if err := json.Unmarshal(raw, &rep); err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile("\\| " + regexp.QuoteMeta(digestLabel) + " \\| `([0-9a-f]{64})` \\|").FindSubmatch(md)
	if m == nil {
		t.Fatal("summary.md states no effective surface digest")
	}
	digest := rep.Effective.Digest()
	if got := hex.EncodeToString(digest[:]); got != string(m[1]) {
		t.Fatalf("summary digest %s, surface.json's %s: regenerate with make surface", m[1], got)
	}
	if !bytes.Equal(summary(rep), md) {
		t.Fatal("summary.md is not what surface.json renders to: regenerate with make surface")
	}
	if d := surface.Diff(rep.Effective, rep.Effective); len(d) != 0 {
		t.Fatalf("the surface differs from itself: %v", d)
	}
	if len(rep.Effective.Endpoints) == 0 || len(rep.Effective.Root.Clusters) == 0 {
		t.Fatal("the snapshot holds no surface")
	}
}

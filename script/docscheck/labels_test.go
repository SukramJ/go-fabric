// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package docscheck

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// v11Label is the "v1.1" the code once used for its first release line.
var v11Label = regexp.MustCompile(`\bv1\.1\b`)

// TestNoV11LabelInGo: no non-test Go file of the module says "v1.1". The
// label named the first release line of the code this module grew from;
// a comment or an error string that still carries it describes a release
// a reader cannot find — 17 such sites at the 5a review, 8 at the 6b one
// (review II, N5-fxdocs-1, R2-P6-doc-3014), one of them the host-visible
// text of wire.ErrSchedulesReadOnly. Say what the code does instead.
func TestNoV11LabelInGo(t *testing.T) {
	files := 0
	err := filepath.WalkDir(repoRoot, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if d.IsDir() {
			if p != repoRoot && (strings.HasPrefix(name, ".") || name == "testdata" || name == "node_modules") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			return nil
		}
		files++
		fh, err := os.Open(p)
		if err != nil {
			return err
		}
		defer fh.Close()
		rel, _ := filepath.Rel(repoRoot, p)
		sc := bufio.NewScanner(fh)
		sc.Buffer(make([]byte, 1<<20), 1<<20)
		for n := 1; sc.Scan(); n++ {
			if v11Label.MatchString(sc.Text()) {
				t.Errorf("%s:%d: %q names the v1.1 release line; say what the code does", filepath.ToSlash(rel), n, strings.TrimSpace(sc.Text()))
			}
		}
		return sc.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
	if files < 500 {
		t.Fatalf("only %d Go files read; the walk is broken", files)
	}
}

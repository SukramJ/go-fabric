// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package measurement_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// hostVocabulary names one host's hardware: words that place a host's
// device knowledge in the module, which stays on the far side of
// contract/ (CLAUDE.md, "Where the host's obligation begins").
var hostVocabulary = regexp.MustCompile(`\b(HM|HmIP|HM-[A-Z]+|CCU|LOWBAT|Homematic)\b`)

// TestNoHostVocabulary holds R2-P6-doc-1465's class for this package: a
// server here states its behaviour in the contract's and Matter's terms,
// never in one host's hardware (decision 141).
func TestNoHostVocabulary(t *testing.T) {
	t.Parallel()
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(b), "\n") {
			if m := hostVocabulary.FindString(line); m != "" {
				t.Errorf("%s:%d names a host's hardware (%q)", f, i+1, m)
			}
		}
	}
}

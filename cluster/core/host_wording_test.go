// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// hostWords names one host's devices: "rich model, dumb bridge" keeps that
// knowledge host-side (CLAUDE.md), so the package's comments and messages
// speak of "the host" instead.
var hostWords = regexp.MustCompile(`\b(CCU|HM|HmIP|Homematic)\b`)

// TestNoHostWording (review II, R2-P6-api-1215): no non-test file of the
// package names one host's devices.
func TestNoHostWording(t *testing.T) {
	t.Parallel()
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(src), "\n") {
			if hostWords.MatchString(line) {
				t.Errorf("%s:%d names a host: %s", f, i+1, strings.TrimSpace(line))
			}
		}
	}
}

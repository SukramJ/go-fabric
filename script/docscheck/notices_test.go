// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package docscheck

import (
	"bufio"
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// noticesLinkedHeading opens the table of THIRD-PARTY-NOTICES.md that a
// host builds its own notices from: the modules every build importing this
// module links.
const noticesLinkedHeading = "### Linked into a build that imports this module"

// noticesRow is one row of a notices table: | `module` | licence |.
var noticesRow = regexp.MustCompile("^\\| `([^`]+)` \\|")

// TestNoticesListEveryLinkedModule holds THIRD-PARTY-NOTICES.md's "Linked
// into a build" table to the module graph: its rows are exactly the
// modules `go list -deps` reaches from the packages a consumer imports —
// every package outside script/, internal/ and examples/, tests excluded.
// The table called modernc.org/sqlite and its eight dependencies test-only
// while package store linked the driver (review II, R2-P5-sup-06).
func TestNoticesListEveryLinkedModule(t *testing.T) {
	if testing.Short() {
		t.Skip("runs go list over the module")
	}
	pkgs := goList(t, "list", "./...")
	var public []string
	for _, p := range pkgs {
		if strings.Contains(p, "/script/") || strings.Contains(p, "/internal/") || strings.Contains(p, "/examples/") || strings.HasSuffix(p, "/examples") {
			continue
		}
		public = append(public, p)
	}
	if len(public) < 100 {
		t.Fatalf("go list ./... gave %d public packages; the walk is broken", len(public))
	}
	args := append([]string{"list", "-deps", "-f", "{{with .Module}}{{if not .Main}}{{.Path}}{{end}}{{end}}"}, public...)
	linked := map[string]bool{}
	for _, m := range goList(t, args...) {
		linked[m] = true
	}

	data, err := os.ReadFile(filepath.Join(repoRoot, "THIRD-PARTY-NOTICES.md"))
	if err != nil {
		t.Fatal(err)
	}
	listed := map[string]bool{}
	in := false
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		line := sc.Text()
		switch {
		case line == noticesLinkedHeading:
			in = true
		case in && strings.HasPrefix(line, "#"):
			in = false
		case in:
			if m := noticesRow.FindStringSubmatch(line); m != nil {
				listed[m[1]] = true
			}
		}
	}
	if len(listed) == 0 {
		t.Fatalf("THIRD-PARTY-NOTICES.md has no rows under %q", noticesLinkedHeading)
	}
	for _, m := range sortedKeys(linked) {
		if !listed[m] {
			t.Errorf("%s is linked by a package a consumer imports but is not in THIRD-PARTY-NOTICES.md's %q table", m, noticesLinkedHeading)
		}
	}
	for _, m := range sortedKeys(listed) {
		if !linked[m] {
			t.Errorf("THIRD-PARTY-NOTICES.md's %q table lists %s, which no package a consumer imports links", noticesLinkedHeading, m)
		}
	}
}

// goList runs the go command in the module root and returns its non-empty
// output lines.
func goList(t *testing.T, args ...string) []string {
	t.Helper()
	cmd := exec.Command("go", args...)
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(), "GOWORK=off")
	out, err := cmd.Output()
	if err != nil {
		var stderr []byte
		if ee := (*exec.ExitError)(nil); errors.As(err, &ee) {
			stderr = ee.Stderr
		}
		t.Fatalf("go %s: %v\n%s", args[0], err, stderr)
	}
	var lines []string
	for _, l := range strings.Split(string(out), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			lines = append(lines, l)
		}
	}
	return lines
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

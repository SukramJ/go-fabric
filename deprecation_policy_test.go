// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package fabric_test

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// README.md states a deprecation policy: a deprecated identifier is marked
// with a `Deprecated: ` paragraph, keeps its behaviour for the window, and is
// announced in CHANGELOG.md before it disappears.
//
// Prose enforces nothing. A policy no test can fail is a statement of intent,
// and this file is the difference. It was written while the module had no
// deprecations, when a policy is cheap to make enforceable; the v0.2.0 marks
// were the first held to it, and v0.3.0 removed them after their window. Every
// deprecation since is held to what the README promises rather than to what
// its author remembered of it.
//
// Two things are checked, both of them mechanical:
//
//   - The marker's shape. `Deprecated: ` must open its own paragraph — that is
//     what staticcheck's SA1019 and gopls key on, so a marker written as
//     "// deprecated, use X" or buried mid-sentence produces no warning at any
//     consumer and the window silently never starts.
//   - The announcement. Every deprecated identifier must be named in
//     CHANGELOG.md, because the window only means something to a consumer who
//     is told the clock is running.
//
// A marker inside a `const (` or `var (` group is indented; it is a marker
// all the same (SA1019 reads it), so the pattern allows leading space.
var deprecatedMarker = regexp.MustCompile(`(?m)^[ \t]*// Deprecated: `)

// looseDeprecation catches the near-misses that produce no tooling warning —
// a comment that OPENS with the word and so was plainly meant as a marker,
// but is spelled in a way SA1019 and gopls do not recognise. It deliberately
// does not match a mention of deprecation inside a sentence: prose about the
// policy is not a marker, and flagging it would make the guard fire on its
// own documentation.
// It stays at the top level: indented, the word opens prose about wire
// values the spec retired (FanMode, the WindowCovering percent fields).
var looseDeprecation = regexp.MustCompile(`(?mi)^//\s*deprecated\b[^\n]*$`)

func TestDeprecationMarkersMatchThePolicy(t *testing.T) {
	t.Parallel()

	root := moduleRoot(t)
	var (
		proper []string // file:identifier-bearing line, correctly marked
		loose  []string // mentions deprecation without the exact marker
	)

	walkGoFiles(t, root, func(path string, src []byte) {
		rel, _ := filepath.Rel(root, path)
		text := string(src)

		properHere := deprecatedMarker.FindAllStringIndex(text, -1)
		for range properHere {
			proper = append(proper, rel)
		}
		// Every loose mention that is not one of the proper ones is a
		// candidate for a marker that will never warn anyone.
		for _, m := range looseDeprecation.FindAllString(text, -1) {
			if strings.HasPrefix(strings.TrimLeft(m, " \t"), "// Deprecated: ") {
				continue // the exact form the policy asks for
			}
			loose = append(loose, rel+": "+strings.TrimSpace(m))
		}
	})

	for _, l := range loose {
		t.Errorf("this reads like a deprecation marker but does not match `// Deprecated: ` at the "+
			"start of a paragraph, so staticcheck SA1019 and gopls will not warn a consumer and the "+
			"window never starts: %s", l)
	}

	t.Logf("deprecation markers found: %d", len(proper))
	if len(proper) == 0 {
		// Not a failure: the module has deprecated nothing. Said out loud so
		// a reader knows this run checked something and found none, rather
		// than assuming the test is asleep.
		t.Log("no deprecated identifiers in the module — the announcement check below has nothing to verify")
	}
}

func TestDeprecatedIdentifiersAreAnnounced(t *testing.T) {
	t.Parallel()

	root := moduleRoot(t)
	changelog, err := os.ReadFile(filepath.Join(root, "CHANGELOG.md"))
	if err != nil {
		t.Fatalf("read CHANGELOG.md: %v", err)
	}

	var missing []string
	walkGoFiles(t, root, func(path string, src []byte) {
		rel, _ := filepath.Rel(root, path)
		for _, ident := range deprecatedIdentifiers(string(src)) {
			if !strings.Contains(string(changelog), ident) {
				missing = append(missing, rel+": "+ident)
			}
		}
	})

	for _, m := range missing {
		t.Errorf("%s is marked deprecated but is named nowhere in CHANGELOG.md. The window only "+
			"means something to a consumer who is told the clock is running — README.md's API "+
			"stability section says the deprecation is announced when it is marked, not when it "+
			"is removed", m)
	}
}

// declAfterMarker finds the declaration a `Deprecated: ` marker belongs to:
// the one on the next non-comment line. Parsing Go for that is overkill
// here: the declaration line is enough to name it. Group 1 is set when the
// marker documents a whole `const (` / `var (` group, whose members
// groupMembers then reads; group 2 is the identifier of a single
// declaration.
var declAfterMarker = regexp.MustCompile(`(?m)^[ \t]*// Deprecated: [^\n]*\n(?:[ \t]*//[^\n]*\n)*[ \t]*(?:(?:const|var)[ \t]*(\()|(?:func|type|var|const|)[ \t]*(?:\([^)]*\)[ \t]*)?([A-Z]\w*))`)

// groupMember is the exported names one line of a declaration group
// declares (`Name`, `Name Type = v`, `A, B = 1, 2`).
var groupMember = regexp.MustCompile(`^\s*([A-Z]\w*(?:\s*,\s*[A-Z]\w*)*)\b`)

// deprecatedIdentifiers returns every identifier a `Deprecated: ` marker in
// src covers: the declaration after it, every member of a `const (` /
// `var (` group whose doc comment carries it (the policy deprecates the
// group's names, not the keyword), and a group member with its own marker.
func deprecatedIdentifiers(src string) []string {
	var out []string
	for _, m := range declAfterMarker.FindAllStringSubmatchIndex(src, -1) {
		switch {
		case m[2] >= 0:
			out = append(out, groupMembers(src[m[3]:])...)
		case m[4] >= 0:
			out = append(out, src[m[4]:m[5]])
		}
	}
	return out
}

// groupMembers reads the exported names of a declaration group, from the
// text after its opening paren to the line that closes it. Comment and
// blank lines name nothing.
func groupMembers(body string) []string {
	var out []string
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, ")") {
			break
		}
		if trimmed == "" || strings.HasPrefix(trimmed, "//") {
			continue
		}
		if g := groupMember.FindStringSubmatch(line); g != nil {
			for _, name := range strings.Split(g[1], ",") {
				out = append(out, strings.TrimSpace(name))
			}
		}
	}
	return out
}

// TestDeprecatedConstGroupIsChecked holds the announcement check to a
// deprecation written on a `const (` group: the marker documents the group,
// so each of its names must reach CHANGELOG.md. The first such groups in
// the module were ModeSelect's field tags (report L, question 5), removed
// by brief BZ; the synthetic source keeps the reader under test.
func TestDeprecatedConstGroupIsChecked(t *testing.T) {
	t.Parallel()

	synthetic := "// Deprecated: gone.\nconst (\n\t// A is one.\n\tA uint8 = 0\n\n\tB, C = 1, 2\n\td = 3\n)\n\nconst E = 4\n" +
		"const (\n\tF = 5\n\t// Deprecated: also gone.\n\tG = 6\n)\n// Deprecated: one.\nfunc H() {}\n" +
		"// Deprecated: a method.\nfunc (r *R) M() {}\n"
	if got, want := deprecatedIdentifiers(synthetic), []string{"A", "B", "C", "G", "H", "M"}; !slices.Equal(got, want) {
		t.Errorf("deprecatedIdentifiers(synthetic) = %v, want %v", got, want)
	}
}

// walkGoFiles visits every .go file in the module, skipping the directories
// that carry no module source.
func walkGoFiles(t *testing.T, root string, fn func(path string, src []byte)) {
	t.Helper()
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "testdata", "node_modules":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		src, readErr := os.ReadFile(path) //nolint:gosec // paths come from walking the module root
		if readErr != nil {
			return readErr
		}
		fn(path, src)
		return nil
	})
	if err != nil {
		t.Fatalf("walk module: %v", err)
	}
}

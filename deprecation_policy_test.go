// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package fabric_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// README.md states a deprecation policy: a deprecated identifier is marked
// with a `Deprecated: ` paragraph, keeps its behaviour for the window, and is
// announced in CHANGELOG.md before it disappears.
//
// Prose enforces nothing. A policy no test can fail is a statement of intent,
// and this file is the difference — it is empty of findings today, because the
// module has no deprecations yet, and that is exactly when a policy is cheap
// to make enforceable. The first deprecation someone writes will be held to
// what the README promises rather than to what they remembered of it.
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
var deprecatedMarker = regexp.MustCompile(`(?m)^// Deprecated: `)

// looseDeprecation catches the near-misses that produce no tooling warning —
// a comment that OPENS with the word and so was plainly meant as a marker,
// but is spelled in a way SA1019 and gopls do not recognise. It deliberately
// does not match a mention of deprecation inside a sentence: prose about the
// policy is not a marker, and flagging it would make the guard fire on its
// own documentation.
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
			if strings.HasPrefix(m, "// Deprecated: ") {
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

	// The identifier a `Deprecated: ` marker belongs to is the one declared
	// on the next non-comment line. Parsing Go for that is overkill here:
	// the declaration line is enough to name it.
	declAfterMarker := regexp.MustCompile(`(?m)^// Deprecated: [^\n]*\n(?://[^\n]*\n)*(?:func|type|var|const|)\s*(?:\([^)]*\)\s*)?([A-Z]\w*)`)

	var missing []string
	walkGoFiles(t, root, func(path string, src []byte) {
		rel, _ := filepath.Rel(root, path)
		for _, m := range declAfterMarker.FindAllStringSubmatch(string(src), -1) {
			ident := m[1]
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

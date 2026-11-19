// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package requiredchecks

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestReleaseNotesShortFormAboveTheLimit: a CHANGELOG section longer than
// RELEASE_NOTES_MAX becomes the short form — the intro with its relative
// links pointing at the tag's tree, the link to the section at the tag,
// and each subsection with the number of its entries — and a section
// within the limit is the section itself (owner decision 267; v0.23.0's
// release job failed with HTTP 422 "body is too long").
func TestReleaseNotesShortFormAboveTheLimit(t *testing.T) {
	bash := lookBash(t)
	root, err := filepath.Abs(repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(root, "script", "release-notes.sh")

	dir := t.TempDir()
	changelog := "# Changelog\n\n## [Unreleased]\n\n## [1.2.0] — 2026-01-02\n\n" +
		"The intro, see [the guide](docs/guide.md) and [the site](https://example.invalid/x).\n\n" +
		"### Added\n\n- one\n- two\n\n### Removed\n\n- **Breaking:** three\n\n" +
		"## [1.1.0] — 2026-01-01\n\n### Fixed\n\n- four\n"
	if err := os.WriteFile(filepath.Join(dir, "CHANGELOG.md"), []byte(changelog), 0o600); err != nil {
		t.Fatal(err)
	}
	run := func(tag, maxChars string) string {
		t.Helper()
		out := filepath.Join(dir, "notes-"+tag+".md")
		cmd := exec.CommandContext(t.Context(), bash, script, tag, out)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "RELEASE_NOTES_MAX="+maxChars, "GITHUB_SERVER_URL=https://github.com", "GITHUB_REPOSITORY=owner/repo")
		if b, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("release-notes.sh %s: %v\n%s", tag, err, b)
		}
		b, err := os.ReadFile(out)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}

	short := run("v1.2.0", "50")
	for _, want := range []string{
		"The intro, see [the guide](https://github.com/owner/repo/blob/v1.2.0/docs/guide.md) and [the site](https://example.invalid/x).",
		"[CHANGELOG.md § 1.2.0](https://github.com/owner/repo/blob/v1.2.0/CHANGELOG.md#120--2026-01-02)",
		"| Added | 2 |",
		"| Removed | 1 |",
		"1 entries are marked **Breaking**.",
	} {
		if !strings.Contains(short, want) {
			t.Errorf("short form lacks %q:\n%s", want, short)
		}
	}
	if strings.Contains(short, "- one") || strings.Contains(short, "four") {
		t.Errorf("short form carries entries or another section:\n%s", short)
	}

	full := run("v1.2.0", "100000")
	if !strings.HasPrefix(full, "The intro, see [the guide](docs/guide.md)") || !strings.Contains(full, "- **Breaking:** three") || strings.Contains(full, "four") {
		t.Errorf("a section within the limit is the section itself:\n%s", full)
	}
}

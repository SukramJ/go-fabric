// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package main is the matter.js test-porting register: which of matter.js's
// test files at the schema pin this module has ported to Go tests, which it
// has in part, which are still to do, and which it leaves out on purpose.
//
// Three artefacts, one of them generated from the other two:
//
//   - matterjs_tests.json — the SNAPSHOT. Every *Test.ts under
//     packages/node/test and packages/protocol/test of matter.js at the commit
//     parity/schema.json names in matter.sourceCommit, with its line count,
//     the SHA-1 of its blob and its top-level describe names. Written by
//     `go run ./script/testparity -snapshot` (make testparity-snapshot) from a
//     matter.js clone (MATTERJS_DIR, default ../matter.js) through git
//     ls-tree and git cat-file only — never the clone's working tree. A pin
//     lift regenerates it; its diff shows the test files upstream added,
//     removed and changed.
//   - register.json — the REGISTER, maintained by hand, one entry per
//     snapshot path.
//   - notes/parity/matterjs_tests.md — the PAGE, rendered from both by
//     `go run ./script/testparity -render` (make testparity). Never edited
//     by hand.
//
// # register.json
//
//	{
//	  "entries": [
//	    {
//	      "path":        "packages/protocol/test/…/FooTest.ts", // a snapshot path
//	      "status":      "ported" | "partial" | "planned" | "excluded",
//	      "go":          ["<module-relative package dir>:<TestFunc>", …],
//	                     // ported and partial only, at least one
//	      "missing":     ["<it name>", …], // partial only, at least one
//	      "class":       "controller-stage" | "js-runtime-only" | "joint-fabric" |
//	                     "device-ble-thread" | "test-infrastructure",
//	                     // excluded only, required there
//	      "until":       "<roadmap stage or milestone>",
//	                     // controller-stage (required) and device-ble-thread (optional)
//	      "reason":      "<one sentence>", // every entry
//	      "snapshotSha": "<40 hex>"       // the blob the mapping was checked against
//	    }
//	  ]
//	}
//
// Entries are sorted by path. ported means the Go tests cover the file's it
// cases semantically; partial names the it cases they do not cover. The
// classes: controller-stage — controller behaviour this module does not have
// yet; js-runtime-only — tests of JavaScript runtime concerns (observables,
// environment, plugins, storage caches, the Reactor); joint-fabric — Joint
// Fabric, deferred; device-ble-thread — device-side BLE or Thread, out of
// scope until milestone 7; test-infrastructure — tests of matter.js's own test
// helpers.
//
// The guard is register_test.go (make testparity-check): every snapshot path
// has an entry and every entry a path, every go reference resolves to a Test
// function in the named package's _test.go files, a ported or partial entry
// whose snapshotSha differs from the snapshot fails ("re-check the mapping"),
// an excluded one too ("re-check the exclusion": its reason may no longer
// hold),
// the committed page is what rendering produces, and the number of planned
// entries sits exactly on testparityPlannedRatchet. It needs no matter.js
// clone: the snapshot is committed.
package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// The committed artefacts, relative to the module root.
const (
	snapshotFile = "script/testparity/matterjs_tests.json"
	registerFile = "script/testparity/register.json"
	pageFile     = "notes/parity/matterjs_tests.md"
	schemaFile   = "parity/schema.json"
)

// snapshotRoots are the matter.js test trees the register covers.
var snapshotRoots = []string{"packages/node/test", "packages/protocol/test"}

func main() {
	snapshot := flag.Bool("snapshot", false, "regenerate "+snapshotFile+" from the matter.js clone in $MATTERJS_DIR (default ../matter.js) at the commit "+schemaFile+" names")
	render := flag.Bool("render", false, "render "+pageFile+" from the snapshot and the register")
	root := flag.String("root", ".", "module root")
	flag.Parse()
	if !*snapshot && !*render {
		fmt.Fprintln(os.Stderr, "testparity: nothing to do; pass -snapshot and/or -render")
		os.Exit(1)
	}
	if *snapshot {
		dir := os.Getenv("MATTERJS_DIR")
		if dir == "" {
			dir = "../matter.js"
		}
		if err := writeSnapshot(*root, dir); err != nil {
			fmt.Fprintln(os.Stderr, "testparity:", err)
			var unavailable errUnavailable
			if errors.As(err, &unavailable) {
				os.Exit(2)
			}
			os.Exit(1)
		}
	}
	if *render {
		if err := writePage(*root); err != nil {
			fmt.Fprintln(os.Stderr, "testparity:", err)
			os.Exit(1)
		}
	}
}

// errUnavailable marks a missing matter.js clone or a clone without the pinned
// commit; main exits 2 on it, so a caller can tell "no clone" from a failure.
type errUnavailable struct{ msg string }

func (e errUnavailable) Error() string { return e.msg }

// Snapshot is matterjs_tests.json.
type Snapshot struct {
	Commit string         `json:"commit"`
	Roots  []string       `json:"roots"`
	Files  []SnapshotFile `json:"files"`
}

// SnapshotFile is one matter.js test file at the pin.
type SnapshotFile struct {
	Path      string   `json:"path"`
	Lines     int      `json:"lines"`
	SHA       string   `json:"sha"`
	Describes []string `json:"describes"`
}

func writeSnapshot(root, matterjs string) error {
	commit, err := pinnedCommit(path.Join(root, schemaFile))
	if err != nil {
		return err
	}
	if fi, statErr := os.Stat(matterjs); statErr != nil || !fi.IsDir() { //nolint:gosec // G703: the clone the operator names in MATTERJS_DIR
		return errUnavailable{fmt.Sprintf("no matter.js clone at %s (set MATTERJS_DIR)", matterjs)}
	}
	if out, gitErr := exec.CommandContext(context.Background(), "git", "-C", matterjs, "cat-file", "-t", commit).Output(); gitErr != nil || strings.TrimSpace(string(out)) != "commit" { //nolint:gosec // G204: git with the operator's clone and the pinned commit
		return errUnavailable{fmt.Sprintf("matter.js clone at %s has no commit %s (fetch it)", matterjs, commit)}
	}
	snap, err := buildSnapshot(matterjs, commit)
	if err != nil {
		return err
	}
	return writeJSON(path.Join(root, snapshotFile), snap)
}

// pinnedCommit reads matter.sourceCommit from parity/schema.json.
func pinnedCommit(schemaPath string) (string, error) {
	raw, err := os.ReadFile(schemaPath) //nolint:gosec // G304: the module's own parity/schema.json
	if err != nil {
		return "", err
	}
	var doc struct {
		Matter struct {
			SourceCommit string `json:"sourceCommit"`
		} `json:"matter"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return "", fmt.Errorf("%s: %w", schemaPath, err)
	}
	if !shaPattern.MatchString(doc.Matter.SourceCommit) {
		return "", fmt.Errorf("%s: matter.sourceCommit %q is not a commit id", schemaPath, doc.Matter.SourceCommit)
	}
	return doc.Matter.SourceCommit, nil
}

func buildSnapshot(matterjs, commit string) (*Snapshot, error) {
	args := append([]string{"-C", matterjs, "ls-tree", "-r", commit, "--"}, snapshotRoots...)
	out, err := exec.CommandContext(context.Background(), "git", args...).Output() //nolint:gosec // G204: git ls-tree with the operator's clone and the pinned commit
	if err != nil {
		return nil, fmt.Errorf("git ls-tree: %w", err)
	}
	files, err := parseLsTree(out)
	if err != nil {
		return nil, err
	}
	contents, err := catBlobs(matterjs, files)
	if err != nil {
		return nil, err
	}
	for i := range files {
		files[i].Lines = countLines(contents[i])
		files[i].Describes = topLevelDescribes(contents[i])
	}
	return &Snapshot{Commit: commit, Roots: append([]string(nil), snapshotRoots...), Files: files}, nil
}

// parseLsTree keeps the *Test.ts blobs of `git ls-tree -r` output, sorted by
// path.
func parseLsTree(out []byte) ([]SnapshotFile, error) {
	var files []SnapshotFile
	for _, line := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
		if line == "" {
			continue
		}
		meta, p, ok := strings.Cut(line, "\t")
		fields := strings.Fields(meta)
		if !ok || len(fields) != 3 {
			return nil, fmt.Errorf("git ls-tree: unexpected line %q", line)
		}
		if fields[1] != "blob" || !strings.HasSuffix(p, "Test.ts") {
			continue
		}
		files = append(files, SnapshotFile{Path: p, SHA: fields[2], Describes: []string{}})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files, nil
}

// catBlobs reads every blob through one `git cat-file --batch`.
func catBlobs(matterjs string, files []SnapshotFile) ([][]byte, error) {
	var in bytes.Buffer
	for _, f := range files {
		in.WriteString(f.SHA + "\n")
	}
	cmd := exec.CommandContext(context.Background(), "git", "-C", matterjs, "cat-file", "--batch") //nolint:gosec // G204: git with the operator's clone
	cmd.Stdin = &in
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git cat-file --batch: %w", err)
	}
	r := bufio.NewReader(bytes.NewReader(out))
	contents := make([][]byte, len(files))
	for i, f := range files {
		header, err := r.ReadString('\n')
		if err != nil {
			return nil, fmt.Errorf("git cat-file --batch: %s: %w", f.Path, err)
		}
		fields := strings.Fields(header)
		if len(fields) != 3 || fields[0] != f.SHA || fields[1] != "blob" {
			return nil, fmt.Errorf("git cat-file --batch: %s: unexpected header %q", f.Path, header)
		}
		size, err := strconv.Atoi(fields[2])
		if err != nil {
			return nil, fmt.Errorf("git cat-file --batch: %s: %w", f.Path, err)
		}
		body := make([]byte, size+1) // content plus the batch's trailing newline
		if _, err := io.ReadFull(r, body); err != nil {
			return nil, fmt.Errorf("git cat-file --batch: %s: %w", f.Path, err)
		}
		contents[i] = body[:size]
	}
	return contents, nil
}

// countLines counts lines the way an editor numbers them: a final line
// without a trailing newline still counts.
func countLines(b []byte) int {
	if len(b) == 0 {
		return 0
	}
	n := bytes.Count(b, []byte("\n"))
	if b[len(b)-1] != '\n' {
		n++
	}
	return n
}

// describePattern matches a describe call at column 0 — the file's top level.
// The first argument is a string literal (quoted, single-quoted or a template
// without substitutions) or, failing that, an expression up to the first
// comma, kept verbatim.
var describePattern = regexp.MustCompile("^describe(?:\\.(?:only|skip))?\\(\\s*(?:\"((?:[^\"\\\\]|\\\\.)*)\"|'((?:[^'\\\\]|\\\\.)*)'|`([^`$]*)`|([^,)]+))")

var openOnly = regexp.MustCompile(`^describe(?:\.(?:only|skip))?\(\s*$`)

func topLevelDescribes(b []byte) []string {
	names := []string{}
	lines := strings.Split(string(b), "\n")
	for i, line := range lines {
		line = strings.TrimRight(line, "\r")
		if openOnly.MatchString(line) && i+1 < len(lines) {
			line += strings.TrimSpace(lines[i+1]) // a formatter broke a long name onto the next line
		}
		m := describePattern.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		for _, g := range m[1:] {
			if g != "" {
				names = append(names, strings.TrimSpace(g))
				break
			}
		}
	}
	return names
}

func writeJSON(p string, v any) error {
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, append(raw, '\n'), 0o644) //nolint:gosec // a committed artefact, world-readable like every file in the tree
}

func writePage(root string) error {
	snap, err := loadSnapshot(path.Join(root, snapshotFile))
	if err != nil {
		return err
	}
	reg, err := loadRegister(path.Join(root, registerFile))
	if err != nil {
		return err
	}
	if err := reg.check(snap); err != nil {
		return err
	}
	page := renderPage(snap, reg)
	return os.WriteFile(path.Join(root, pageFile), page, 0o644) //nolint:gosec // a committed artefact
}

func loadSnapshot(p string) (*Snapshot, error) {
	raw, err := os.ReadFile(p) //nolint:gosec // G304: the committed snapshot under the module root
	if err != nil {
		return nil, err
	}
	var snap Snapshot
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&snap); err != nil {
		return nil, fmt.Errorf("%s: %w", p, err)
	}
	return &snap, nil
}

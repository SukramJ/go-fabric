// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package main is the matter.js test-porting register: which of matter.js's
// test files at the schema pin this module has ported to Go tests, which it
// has in part, which are still to do, and which it leaves out on purpose.
//
// Three artefacts, one of them generated from the other two:
//
//   - matterjs_tests.json — the SNAPSHOT. Every *Test.ts under
//     packages/general/test, packages/model/test, packages/node/test,
//     packages/nodejs/test, packages/protocol/test,
//     packages/thread-br-client/test and packages/types/test
//     of matter.js at the commit
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
//	      "left":        [{"it": "<it name>", "class": "<class>", "reason": "<one sentence>"}, …],
//	                     // ported and partial only: cases that will not be ported
//	      "class":       "controller-stage" | "js-runtime-only" | "joint-fabric" |
//	                     "device-ble-thread" | "test-infrastructure" | "host-server",
//	                     // excluded only, required there
//	      "until":       "<roadmap stage or milestone>",
//	                     // controller-stage (required) and device-ble-thread (optional)
//	      "reason":      "<one sentence>", // every entry
//	      "snapshotSha": "<40 hex>"       // the blob the mapping was checked against
//	    }
//	  ],
//	  "skips": [
//	    {
//	      "test":   "<module-relative package dir>:<TestFunc>",
//	      "source": "<the matter.js or chip case it would mirror>",
//	      "reason": "<why it does not run>"
//	    }
//	  ]
//	}
//
// # The second register: matterjs-server
//
// matterjs-server (github.com/matter-js/matterjs-server), matter.js's own
// server on top of it, is the behaviour source of what this module ports
// from it ahead of matter.js (notes/parity/by_design.md, the Ahead of
// matter.js register). Its register has the same three artefacts —
// matterjsserver_tests.json, matterjsserver_register.json and
// notes/parity/matterjsserver_tests.md — and the same entry rules. Its
// snapshot is not of whole trees: it lists the test files of the parts the
// module ports (serverSnapshotRoots), at matterjsServerCommit, written by
// `go run ./script/testparity -server-snapshot` from a matterjs-server clone
// (MATTERJS_SERVER_DIR, default ../matterjs-server). A test of
// matterjs-server's WebSocket server rather than of the behaviour the
// module takes is excluded with class host-server: go-fabric-server's.
//
// Entries are sorted by path, skips by test. A skip is a Go test whose body
// is an unconditional t.Skip("FixMe: …"): it names a gap and tests nothing,
// so it is listed here, rendered on the page and reviewed with the register
// instead of being read later as a fact. ported means the Go tests cover the file's it
// cases semantically; partial names the it cases they do not cover. The
// classes: controller-stage — controller behaviour this module does not have
// yet; js-runtime-only — tests of JavaScript runtime concerns (observables,
// environment, plugins, storage caches, the Reactor) and of matter.js's own
// platform, util, storage, time and codec runtime (its Node.js bindings, its
// in-memory model classes, byte readers and writers, hand-written codecs the
// Go standard library replaces); joint-fabric — Joint
// Fabric, deferred; device-ble-thread — device-side BLE or Thread, out of
// scope until milestone 7; test-infrastructure — tests of matter.js's own test
// helpers.
//
// left is the per-case counterpart of an exclusion: an it case of a ported or
// partial file that leaves missing without a port, with one of the five
// classes above or by-design (the Go behaviour deliberately differs; the
// reason names the notes/parity/by_design.md entry) or skip (an existing
// t.Skip subtest of the Go port names the case and its reason). A case is
// missing or left, never both. The snapshot holds a file's top-level
// describe names only, so a left row written "<describe> > <it>" is held to
// them and an it-level row is free text, as a missing case is. -render
// writes the rows as the note's "Left:" sentence.
//
// The guard is register_test.go (make testparity-check): every snapshot path
// has an entry and every entry a path, every go reference resolves to a Test
// function in the named package's _test.go files, a ported or partial entry
// whose snapshotSha differs from the snapshot fails ("re-check the mapping"),
// an excluded one too ("re-check the exclusion": its reason may no longer
// hold),
// every FixMe-skipped Go test in the module is a skips row and every row
// such a test, the committed page is what rendering produces, and four
// exact ratchets hold: the planned entries (testparityPlannedRatchet), the
// ported entries (testparityPortedRatchet), the missing it cases summed
// over the partial entries (testparityMissingRatchet) and the left it cases
// summed over the ported and partial entries (testparityLeftRatchet). It
// needs no matter.js clone: the snapshot is committed.
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

// The committed artefacts of the matterjs-server register.
const (
	serverSnapshotFile = "script/testparity/matterjsserver_tests.json"
	serverRegisterFile = "script/testparity/matterjsserver_register.json"
	serverPageFile     = "notes/parity/matterjsserver_tests.md"
)

// matterjsServerCommit is the matterjs-server commit the module's ports of
// it follow; its citations name the same commit (script/citecheck).
const matterjsServerCommit = "0bd06f222a3d51415980022c5f207705de57b8aa"

// snapshotRoots are the matter.js test trees the register covers.
var snapshotRoots = []string{
	"packages/general/test",
	"packages/model/test",
	"packages/node/test",
	"packages/nodejs/test",
	"packages/protocol/test",
	"packages/thread-br-client/test",
	"packages/types/test",
}

// serverSnapshotRoots are the matterjs-server test files the module ports
// or classifies: its camera stream management's (phase 37, issue 410). A
// root is a file here, not a tree.
var serverSnapshotRoots = []string{
	"packages/ws-controller/test/CameraCommandsTest.ts",
	"packages/ws-controller/test/CameraErrorsTest.ts",
	"packages/ws-controller/test/CameraStreamManagerTest.ts",
	"packages/ws-controller/test/CameraVideoAllocateTest.ts",
	"packages/ws-controller/test/CameraWireContractTest.ts",
	"packages/ws-controller/test/MatterCameraDeviceIoTest.ts",
	"packages/ws-controller/test/MatterControllerCameraStreamsTest.ts",
	"packages/ws-controller/test/OverlayPolicyTest.ts",
	"packages/ws-controller/test/SdpConstraintsTest.ts",
	"packages/ws-controller/test/SessionRegistryClientKeyTest.ts",
	"packages/ws-controller/test/SnapshotPolicyTest.ts",
	"packages/ws-controller/test/StartingSessionSignallingTest.ts",
	"packages/ws-controller/test/StreamPolicyTest.ts",
	"packages/ws-controller/test/VideoWindowSearchTest.ts",
	"packages/ws-controller/test/WebRtcCallbackBridgeTest.ts",
	"packages/ws-controller/test/WebRtcProviderArgumentsTest.ts",
	"packages/ws-controller/test/WebRtcProviderDeviceTest.ts",
	"packages/ws-controller/test/WebRtcProviderSessionWatchTest.ts",
	"packages/ws-controller/test/WebRtcProviderSessionsTest.ts",
	"packages/ws-controller/test/WebRtcSessionStreamsTest.ts",
}

// source is one register: its artefacts, the roots its snapshot covers, the
// clone it is taken from and how the pin is read.
type source struct {
	// name is the source as the page and the errors name it.
	name                                 string
	snapshotFile, registerFile, pageFile string
	roots                                []string
	// cloneEnv names the clone's environment variable, cloneDefault its
	// path without one.
	cloneEnv, cloneDefault string
	// pin is the commit the snapshot is taken at.
	pin func(root string) (string, error)
	// intro is the page's paragraph before the counts.
	intro func(snap *Snapshot) string
	// skips: the register holds the module's FixMe skips.
	skips bool
}

var (
	matterjsSource = &source{
		name: "matter.js", snapshotFile: snapshotFile, registerFile: registerFile, pageFile: pageFile,
		roots: snapshotRoots, cloneEnv: "MATTERJS_DIR", cloneDefault: "../matter.js",
		pin:   func(root string) (string, error) { return pinnedCommit(path.Join(root, schemaFile)) },
		intro: matterjsIntro, skips: true,
	}
	serverSource = &source{
		name: "matterjs-server", snapshotFile: serverSnapshotFile, registerFile: serverRegisterFile, pageFile: serverPageFile,
		roots: serverSnapshotRoots, cloneEnv: "MATTERJS_SERVER_DIR", cloneDefault: "../matterjs-server",
		pin:   func(string) (string, error) { return matterjsServerCommit, nil },
		intro: serverIntro,
	}
	sources = []*source{matterjsSource, serverSource}
)

func main() {
	snapshot := flag.Bool("snapshot", false, "regenerate "+snapshotFile+" from the matter.js clone in $MATTERJS_DIR (default ../matter.js) at the commit "+schemaFile+" names")
	serverSnapshot := flag.Bool("server-snapshot", false, "regenerate "+serverSnapshotFile+" from the matterjs-server clone in $MATTERJS_SERVER_DIR (default ../matterjs-server) at "+matterjsServerCommit)
	render := flag.Bool("render", false, "render "+pageFile+" and "+serverPageFile+" from the snapshots and the registers")
	root := flag.String("root", ".", "module root")
	flag.Parse()
	if !*snapshot && !*serverSnapshot && !*render {
		fmt.Fprintln(os.Stderr, "testparity: nothing to do; pass -snapshot, -server-snapshot and/or -render")
		os.Exit(1)
	}
	for _, s := range []struct {
		on  bool
		src *source
	}{{*snapshot, matterjsSource}, {*serverSnapshot, serverSource}} {
		if !s.on {
			continue
		}
		dir := os.Getenv(s.src.cloneEnv)
		if dir == "" {
			dir = s.src.cloneDefault
		}
		if err := writeSnapshot(*root, s.src, dir); err != nil {
			fmt.Fprintln(os.Stderr, "testparity:", err)
			var unavailable errUnavailable
			if errors.As(err, &unavailable) {
				os.Exit(2)
			}
			os.Exit(1)
		}
	}
	if *render {
		for _, src := range sources {
			if err := writePage(*root, src); err != nil {
				fmt.Fprintln(os.Stderr, "testparity:", err)
				os.Exit(1)
			}
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

func writeSnapshot(root string, src *source, clone string) error {
	commit, err := src.pin(root)
	if err != nil {
		return err
	}
	if fi, statErr := os.Stat(clone); statErr != nil || !fi.IsDir() { //nolint:gosec // G703: the clone the operator names in the source's variable
		return errUnavailable{fmt.Sprintf("no %s clone at %s (set %s)", src.name, clone, src.cloneEnv)}
	}
	if out, gitErr := exec.CommandContext(context.Background(), "git", "-C", clone, "cat-file", "-t", commit).Output(); gitErr != nil || strings.TrimSpace(string(out)) != "commit" { //nolint:gosec // G204: git with the operator's clone and the pinned commit
		return errUnavailable{fmt.Sprintf("%s clone at %s has no commit %s (fetch it)", src.name, clone, commit)}
	}
	snap, err := buildSnapshot(clone, commit, src.roots)
	if err != nil {
		return err
	}
	return writeJSON(path.Join(root, src.snapshotFile), snap)
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

func buildSnapshot(matterjs, commit string, roots []string) (*Snapshot, error) {
	args := append([]string{"-C", matterjs, "ls-tree", "-r", commit, "--"}, roots...)
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
	return &Snapshot{Commit: commit, Roots: append([]string(nil), roots...), Files: files}, nil
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

func writePage(root string, src *source) error {
	snap, err := loadSnapshot(path.Join(root, src.snapshotFile))
	if err != nil {
		return err
	}
	reg, err := loadRegister(path.Join(root, src.registerFile))
	if err != nil {
		return err
	}
	if err := reg.check(snap); err != nil {
		return err
	}
	page := renderPage(src, snap, reg)
	return os.WriteFile(path.Join(root, src.pageFile), page, 0o644) //nolint:gosec // a committed artefact
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

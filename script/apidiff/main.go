// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Command apidiff is the API-break guard (ADR 0025 principle 9, concept
// v6 A.4): it compares the public packages of the module with the last
// release tag and prints the incompatible changes; with -check it fails
// when one of them is not named under [Unreleased] in CHANGELOG.md — in
// its Changed, Removed or Deprecated section — the README's rule "a break
// that is not in the changelog does not ship".
//
// The comparison is golang.org/x/exp/cmd/apidiff in module mode, the
// module's tool dependency (go.mod's tool directive), built once and run
// over the tag's tree (git archive into a temporary directory) and over
// the working tree. internal/ is left out by apidiff itself; examples/,
// script/ and conformance/ are not public API (README "What public API
// means") and are left out here. On the tag itself there is nothing to
// compare. Nothing is committed: the export data lives in the temporary
// directory.
//
// The base is the last release tag of the module: `git describe --tags
// --abbrev=0 --match 'v[0-9]*'`, never a nested module's tag
// (ble/esphome/v0.22.2), which can name another commit and another
// version (R2-P6-sup-2801). The nested modules a tracked go.mod outside
// the excluded trees declares (ble/bluez, ble/esphome, wifi/wpasupplicant)
// are released on their own and compared on their own, each with its last
// "<dir>/v*" tag (R2-P6-sup-2803); one CHANGELOG.md announces the breaks
// of all of them. Each module is exported with GOOS=linux, the platform
// CI runs on, so a local run on darwin compares what CI compares.
//
// Usage: go run ./script/apidiff [-check] [-base TAG]
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strconv"
	"strings"
)

const module = "thingfabric.dev/go-fabric"

func main() {
	check := flag.Bool("check", false, "fail when an incompatible change has no CHANGELOG line under [Unreleased]")
	base := flag.String("base", "", "the root module's release to compare with; empty is `git describe --tags --abbrev=0 --match 'v[0-9]*'`")
	flag.Parse()
	if err := run(*check, *base); err != nil {
		fmt.Fprintln(os.Stderr, "apidiff:", err)
		os.Exit(1)
	}
}

// target is one module the guard compares.
type target struct {
	// Dir is the module's directory, relative to the repository root.
	Dir string
	// Path is its module path.
	Path string
	// Name qualifies a symbol of the module's root package in the
	// CHANGELOG ("fabric.Version", "esphome.Central").
	Name string
}

// rootName is the root package's name (doc.go).
const rootName = "fabric"

func run(check bool, base string) error {
	targets, err := modules()
	if err != nil {
		return err
	}
	head, err := output("git", "rev-parse", "HEAD")
	if err != nil {
		return err
	}
	var changelog string
	if check {
		b, err := os.ReadFile("CHANGELOG.md")
		if err != nil {
			return err
		}
		changelog = string(b)
	}
	tmp, err := os.MkdirTemp("", "apidiff-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	tool := filepath.Join(tmp, "apidiff")
	if _, err := output("go", "build", "-o", tool, "golang.org/x/exp/cmd/apidiff"); err != nil {
		return fmt.Errorf("build the tool: %w", err)
	}
	var failed []string
	for i, t := range targets {
		b := base
		if t.Dir != "." || b == "" {
			if b, err = lastTag(t.Dir); err != nil {
				return err
			}
		}
		if b == "" { // a nested module never released: nothing to compare
			fmt.Printf("apidiff: %s has no release tag %sv*; nothing to compare\n", t.Path, tagPrefix(t.Dir))
			continue
		}
		ok, err := compare(t, b, head, filepath.Join(tmp, strconv.Itoa(i)), tool, check, changelog)
		if err != nil {
			return fmt.Errorf("%s: %w", t.Path, err)
		}
		if !ok {
			failed = append(failed, t.Path)
		}
	}
	if len(failed) > 0 {
		return fmt.Errorf("an incompatible change is not announced (%s)", strings.Join(failed, ", "))
	}
	return nil
}

// modules lists the root module and every nested module a tracked go.mod
// declares outside the trees that are not public API.
func modules() ([]target, error) {
	targets := []target{{Dir: ".", Path: module, Name: rootName}}
	out, err := output("git", "ls-files", "--", "*/go.mod")
	if err != nil {
		return nil, err
	}
	for _, f := range strings.Fields(out) {
		dir := filepath.ToSlash(filepath.Dir(f))
		if isExcluded(dir) {
			continue
		}
		targets = append(targets, target{Dir: dir, Path: module + "/" + dir, Name: path.Base(dir)})
	}
	return targets, nil
}

// tagPrefix is the prefix of a module's release tags: none for the root,
// "<dir>/" for a nested module (the Go modules convention).
func tagPrefix(dir string) string {
	if dir == "." {
		return ""
	}
	return dir + "/"
}

// lastTag is the module's last release tag reachable from HEAD, "" for a
// nested module without one. The root module must have one.
func lastTag(dir string) (string, error) {
	tag, err := output("git", "describe", "--tags", "--abbrev=0", "--match", tagPrefix(dir)+"v[0-9]*")
	if err != nil {
		if dir != "." && strings.Contains(err.Error(), "No names found") {
			return "", nil
		}
		return "", fmt.Errorf("the last tag of %s: %w", dir, err)
	}
	return tag, nil
}

// compare runs apidiff for one module against its tag and, with check,
// holds its incompatible changes to the CHANGELOG. It reports false when a
// change is not announced.
func compare(t target, base, head, tmp, tool string, check bool, changelog string) (bool, error) {
	tagCommit, err := output("git", "rev-list", "-n", "1", base)
	if err != nil {
		return false, fmt.Errorf("tag %s: %w", base, err)
	}
	if head == tagCommit {
		fmt.Printf("apidiff: %s: HEAD is %s; nothing to compare\n", t.Path, base)
		return true, nil
	}
	old := filepath.Join(tmp, "old")
	if err := extract(base, old); err != nil {
		return false, err
	}
	oldExport, newExport := filepath.Join(tmp, "old.export"), filepath.Join(tmp, "new.export")
	// The tag is exported under the module path its own go.mod declares,
	// which differs from t.Path across a move (v0.24.1 is
	// github.com/SukramJ/go-fabric, v0.25.0 thingfabric.dev/go-fabric).
	// apidiff's ModuleChanges pairs packages by their path relative to the
	// module (golang.org/x/exp apidiff.go, relativePath), so a moved
	// module is compared package by package, not reported as removed.
	oldPath, err := modulePath(filepath.Join(old, t.Dir))
	if err != nil {
		return false, fmt.Errorf("tag %s: %w", base, err)
	}
	if err := runIn(filepath.Join(old, t.Dir), tool, "-m", "-w", oldExport, oldPath); err != nil {
		return false, fmt.Errorf("export %s: %w", base, err)
	}
	if err := runIn(t.Dir, tool, "-m", "-w", newExport, t.Path); err != nil {
		return false, fmt.Errorf("export the working tree: %w", err)
	}
	out, err := output(tool, "-m", "-incompatible", oldExport, newExport)
	if err != nil {
		return false, fmt.Errorf("compare: %w", err)
	}
	oldRoot, err := modulePath(old)
	if err != nil {
		return false, fmt.Errorf("tag %s: %w", base, err)
	}
	changes, moves := splitMoves(parse(out, t), oldRoot, module)
	if len(moves) > 0 {
		fmt.Printf("apidiff: %s: %d change(s) against %s only move the module path %s → %s\n", t.Path, len(moves), base, oldRoot, module)
		if check {
			text, err := unreleased(changelog, base)
			if err != nil {
				return false, err
			}
			if !word(module).MatchString(text) {
				fmt.Printf("apidiff: %s: the move to %s is not named under [Unreleased] or a release newer than %s (### Changed, ### Removed or ### Deprecated) in CHANGELOG.md\n", t.Path, module, base)
				return false, nil
			}
		}
	}
	if len(changes) == 0 {
		fmt.Printf("apidiff: %s: no incompatible change against %s\n", t.Path, base)
		return true, nil
	}
	fmt.Printf("apidiff: %s: %d incompatible change(s) against %s:\n", t.Path, len(changes), base)
	for _, c := range changes {
		fmt.Println("  " + c.Line)
	}
	if !check {
		return true, nil
	}
	text, err := unreleased(changelog, base)
	if err != nil {
		return false, err
	}
	missing := unannounced(changes, text)
	if len(missing) == 0 {
		fmt.Printf("apidiff: %s: every incompatible change is named under [Unreleased] or a release newer than %s\n", t.Path, base)
		return true, nil
	}
	fmt.Printf("apidiff: %s: not named under [Unreleased] or a release newer than %s (### Changed, ### Removed or ### Deprecated) in CHANGELOG.md:\n", t.Path, base)
	for _, c := range missing {
		fmt.Printf("  %s (name %q)\n", c.Line, c.Symbol)
	}
	return false, nil
}

// extract writes the tree of rev into dir: git archive, unpacked by tar.
func extract(rev, dir string) error {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	archive := exec.CommandContext(context.Background(), "git", "archive", "--format=tar", rev) //nolint:gosec // rev is the tag the guard resolved itself
	untar := exec.CommandContext(context.Background(), "tar", "-x", "-C", dir)                  //nolint:gosec // dir is the guard's own temporary directory
	pipe, err := archive.StdoutPipe()
	if err != nil {
		return err
	}
	untar.Stdin = pipe
	var stderr bytes.Buffer
	archive.Stderr, untar.Stderr = &stderr, &stderr
	if err := untar.Start(); err != nil {
		return err
	}
	if err := archive.Run(); err != nil {
		return fmt.Errorf("git archive %s: %w: %s", rev, err, stderr.String())
	}
	if err := untar.Wait(); err != nil {
		return fmt.Errorf("tar: %w: %s", err, stderr.String())
	}
	return nil
}

func output(name string, args ...string) (string, error) {
	cmd := exec.CommandContext(context.Background(), name, args...) //nolint:gosec // the guard runs git, go and the apidiff it built, with arguments it builds
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(string(out)), nil
}

// modulePath is the module path the go.mod in dir declares.
func modulePath(dir string) (string, error) {
	cmd := exec.CommandContext(context.Background(), "go", "mod", "edit", "-json") //nolint:gosec // as output
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOWORK=off")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("go mod edit -json in %s: %w", dir, err)
	}
	var mod struct{ Module struct{ Path string } }
	if err := json.Unmarshal(out, &mod); err != nil {
		return "", fmt.Errorf("go mod edit -json in %s: %w", dir, err)
	}
	if mod.Module.Path == "" {
		return "", fmt.Errorf("go mod edit -json in %s: no module path", dir)
	}
	return mod.Module.Path, nil
}

func runIn(dir, name string, args ...string) error {
	cmd := exec.CommandContext(context.Background(), name, args...) //nolint:gosec // as output
	cmd.Dir = dir
	// The export is the API CI sees: ble/bluez and wifi/wpasupplicant
	// declare theirs in _linux files and a stub elsewhere.
	cmd.Env = append(os.Environ(), "GOOS=linux")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

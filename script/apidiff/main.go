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
// Usage: go run ./script/apidiff [-check] [-base TAG]
package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const module = "github.com/SukramJ/go-fabric"

func main() {
	check := flag.Bool("check", false, "fail when an incompatible change has no CHANGELOG line under [Unreleased]")
	base := flag.String("base", "", "the release to compare with; empty is `git describe --tags --abbrev=0`")
	flag.Parse()
	if err := run(*check, *base); err != nil {
		fmt.Fprintln(os.Stderr, "apidiff:", err)
		os.Exit(1)
	}
}

func run(check bool, base string) error {
	if base == "" {
		tag, err := output("git", "describe", "--tags", "--abbrev=0")
		if err != nil {
			return fmt.Errorf("the last tag: %w", err)
		}
		base = tag
	}
	head, err := output("git", "rev-parse", "HEAD")
	if err != nil {
		return err
	}
	tagCommit, err := output("git", "rev-list", "-n", "1", base)
	if err != nil {
		return fmt.Errorf("tag %s: %w", base, err)
	}
	if head == tagCommit {
		fmt.Printf("apidiff: HEAD is %s; nothing to compare\n", base)
		return nil
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
	old := filepath.Join(tmp, "old")
	if err := extract(base, old); err != nil {
		return err
	}
	oldExport, newExport := filepath.Join(tmp, "old.export"), filepath.Join(tmp, "new.export")
	if err := runIn(old, tool, "-m", "-w", oldExport, module); err != nil {
		return fmt.Errorf("export %s: %w", base, err)
	}
	if err := runIn(".", tool, "-m", "-w", newExport, module); err != nil {
		return fmt.Errorf("export the working tree: %w", err)
	}
	out, err := output(tool, "-m", "-incompatible", oldExport, newExport)
	if err != nil {
		return fmt.Errorf("compare: %w", err)
	}
	changes := parse(out)
	if len(changes) == 0 {
		fmt.Printf("apidiff: no incompatible change against %s\n", base)
		return nil
	}
	fmt.Printf("apidiff: %d incompatible change(s) against %s:\n", len(changes), base)
	for _, c := range changes {
		fmt.Println("  " + c.Line)
	}
	if !check {
		return nil
	}
	changelog, err := os.ReadFile("CHANGELOG.md")
	if err != nil {
		return err
	}
	missing := unannounced(changes, unreleased(string(changelog)))
	if len(missing) == 0 {
		fmt.Println("apidiff: every incompatible change is named under [Unreleased]")
		return nil
	}
	fmt.Println("apidiff: not named under [Unreleased] (### Changed, ### Removed or ### Deprecated) in CHANGELOG.md:")
	for _, c := range missing {
		fmt.Printf("  %s (name %q)\n", c.Line, c.Symbol)
	}
	return errors.New("an incompatible change is not announced")
}

// extract writes the tree of rev into dir: git archive, unpacked by tar.
func extract(rev, dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	archive := exec.Command("git", "archive", "--format=tar", rev)
	untar := exec.Command("tar", "-x", "-C", dir)
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
	cmd := exec.Command(name, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(string(out)), nil
}

func runIn(dir, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

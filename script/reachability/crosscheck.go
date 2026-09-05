// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

//go:build ignore

// crosscheck reads the inventory produced by script/reachability and re-checks
// every unreached FUNCTION with a plain text search, to separate genuine
// findings from RTA artefacts.
//
// The direction is inverted relative to the reference daemon's crosscheck, and
// the inversion is the whole point. There, the root set is production, so a caller
// found OUTSIDE test files refutes the finding. Here the root set is the tests,
// so the question this tool asks is
//
//	does any file — test file INCLUDED — name this identifier, other than the
//	one that declares it?
//
// A hit means RTA lost the call (a function value stored in a map, a method
// reached only through an interface the analysis could not resolve) and the
// entry is a false positive. No hit anywhere means nothing in the module so
// much as spells the name, which is as strong as a text-level check gets.
//
// Text search cannot distinguish a call from a mention in a comment or a
// t.Error string, so it over-reports callers and therefore under-reports dead
// code. That bias is deliberate: this tool exists to shrink the candidate list
// to entries worth reading, never to be the verdict.
//
// Run from the module root:
//
//	go run ./script/reachability/crosscheck.go
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// entry mirrors one unreachable row of the inventory.
type entry struct {
	Package    string `json:"package"`
	Identifier string `json:"identifier"`
	File       string `json:"file"`
	Line       int    `json:"line"`
	Kind       string `json:"kind"`
}

// inventory mirrors the fields of inventory.json this tool reads.
type inventory struct {
	Head        string  `json:"head"`
	Unreachable []entry `json:"unreachable"`
}

// result is the document written to crosscheck.json.
type result struct {
	Head            string  `json:"head"`
	TotalCandidates int     `json:"total_candidates"`
	FalsePositives  int     `json:"false_positives"`
	Genuine         int     `json:"genuine"`
	GenuineEntries  []entry `json:"genuine_entries"`
	Refuted         []entry `json:"refuted_entries"`
}

func main() {
	root, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "getwd: %v\n", err)
		os.Exit(1)
	}

	raw, err := os.ReadFile(filepath.Join(root, "script", "reachability", "inventory.json"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "load inventory (run `make reachability` first): %v\n", err)
		os.Exit(1)
	}
	var inv inventory
	if err := json.Unmarshal(raw, &inv); err != nil {
		fmt.Fprintf(os.Stderr, "parse inventory: %v\n", err)
		os.Exit(1)
	}

	// Only functions. A type or a var is judged by whether reachable code
	// names it, which is already a reference check; re-running a text search
	// over those would just restate the same evidence in a weaker form.
	var candidates []entry
	for _, e := range inv.Unreachable {
		if e.Kind == "func" {
			candidates = append(candidates, e)
		}
	}

	out := result{Head: inv.Head, TotalCandidates: len(candidates)}
	for _, c := range candidates {
		if hasAnyMention(root, c) {
			out.FalsePositives++
			out.Refuted = append(out.Refuted, c)
			continue
		}
		out.GenuineEntries = append(out.GenuineEntries, c)
	}
	out.Genuine = len(out.GenuineEntries)

	enc, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "encode result: %v\n", err)
		os.Exit(1)
	}
	outPath := filepath.Join(root, "script", "reachability", "crosscheck.json")
	if err := os.WriteFile(outPath, append(enc, '\n'), 0o600); err != nil {
		fmt.Fprintf(os.Stderr, "write output: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Cross-check complete:\n")
	fmt.Printf("  candidates (funcs): %d\n", out.TotalCandidates)
	fmt.Printf("  refuted by a text mention elsewhere: %d\n", out.FalsePositives)
	fmt.Printf("  no mention anywhere in the module:   %d\n", out.Genuine)
	for _, e := range out.GenuineEntries {
		fmt.Printf("    %s.%s (%s:%d)\n", e.Package, e.Identifier, e.File, e.Line)
	}
	fmt.Printf("Output: %s\n", outPath)
}

// hasAnyMention reports whether any Go file in the module other than the one
// declaring e names the identifier. Test files count — see the file comment.
// script/ is excluded: this analyzer's own source quotes identifier names in
// its documentation, and a tool refuting its own findings is worthless.
func hasAnyMention(root string, e entry) bool {
	cmd := exec.Command("grep", "-rln", "--include=*.go", "-w", e.Identifier, ".") //nolint:gosec // G204: identifier comes from the tool's own inventory
	cmd.Dir = root
	out, _ := cmd.Output()

	declFile := filepath.Clean(e.File)
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		clean := filepath.Clean(strings.TrimPrefix(line, "./"))
		if clean == declFile || strings.HasPrefix(clean, "script"+string(filepath.Separator)) {
			continue
		}
		return true
	}
	return false
}

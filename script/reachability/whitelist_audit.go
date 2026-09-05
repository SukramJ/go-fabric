// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

//go:build ignore

// whitelist_audit inspects every fabric:reachable:reason="..." annotation in
// the module and classifies each one, so an annotation cannot quietly become a
// permanent exemption for code nothing uses.
//
// Three outcomes, and the third is this port's addition:
//
//   - PRODUCTIVE — the annotated identifier has at least one call site outside
//     its own declaration. The annotation is redundant but harmless.
//   - MASKED — zero call sites anywhere. The annotation is the only reason the
//     identifier is not in the unreached list, so it is load-bearing, and its
//     reason text is the entire justification. Read every one of these.
//   - INERT — the annotation is placed where the analyzer does not look, so it
//     has no effect at all and the identifier is judged as if it were absent.
//
// The analyzer recognises an annotation ONLY on the line directly above the
// declaration (see findWhitelistComment in main.go). This tool deliberately
// scans a wider window — up to four lines above — precisely so it can report
// the difference as INERT. An annotation buried mid-doc-comment is silent in
// every other channel: the analyzer ignores it, the identifier is reported
// unreached, and the author sees a finding they believe they already answered.
//
// Output: script/reachability/whitelist-audit.md
//
// Run from the module root:
//
//	go run ./script/reachability/whitelist_audit.go
package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// marker must match whitelistMarker in main.go.
const marker = "fabric:reachable:reason="

// strictOffset must match the placement rule findWhitelistComment enforces:
// the annotation is only honoured on the line directly above the declaration.
const strictOffset = 1

// scanWindow is how many lines above a declaration this tool looks. It is wider
// than strictOffset on purpose, so a misplaced annotation is found and reported
// as INERT rather than being invisible.
const scanWindow = 4

// annotatedItem is one annotation found in the tree.
type annotatedItem struct {
	File       string
	Line       int
	Identifier string
	Reason     string
	Offset     int // declaration line minus annotation line
}

// classifiedItem is an annotatedItem with its verdict.
type classifiedItem struct {
	annotatedItem
	Callers []string
	Status  string
}

func main() {
	root, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "getwd: %v\n", err)
		os.Exit(1)
	}

	items, err := collectAnnotations(root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "collect annotations: %v\n", err)
		os.Exit(1)
	}

	results := make([]classifiedItem, 0, len(items))
	for _, item := range items {
		c := classifiedItem{annotatedItem: item}
		switch {
		case item.Offset != strictOffset:
			c.Status = "INERT"
		default:
			c.Callers = findCallers(root, item)
			c.Status = "PRODUCTIVE"
			if len(c.Callers) == 0 {
				c.Status = "MASKED"
			}
		}
		results = append(results, c)
	}

	// INERT first, then MASKED, then PRODUCTIVE — worst first.
	rank := map[string]int{"INERT": 0, "MASKED": 1, "PRODUCTIVE": 2}
	sort.Slice(results, func(i, j int) bool {
		if rank[results[i].Status] != rank[results[j].Status] {
			return rank[results[i].Status] < rank[results[j].Status]
		}
		if results[i].File != results[j].File {
			return results[i].File < results[j].File
		}
		return results[i].Line < results[j].Line
	})

	outPath := filepath.Join(root, "script", "reachability", "whitelist-audit.md")
	counts := map[string]int{}
	for _, r := range results {
		counts[r.Status]++
	}
	if err := writeReport(outPath, results, counts); err != nil {
		fmt.Fprintf(os.Stderr, "write report: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Whitelist audit complete:\n")
	fmt.Printf("  total annotated: %d\n", len(results))
	fmt.Printf("  INERT (misplaced, analyzer never sees them): %d\n", counts["INERT"])
	fmt.Printf("  MASKED (no caller — the annotation is the only thing hiding them): %d\n", counts["MASKED"])
	fmt.Printf("  PRODUCTIVE: %d\n", counts["PRODUCTIVE"])
	fmt.Printf("Output: %s\n", outPath)
}

// collectAnnotations walks the module and pairs each annotation with the
// declaration below it.
func collectAnnotations(root string) ([]annotatedItem, error) {
	var items []annotatedItem

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil //nolint:nilerr // an unreadable entry is skipped, not fatal
		}
		if d.IsDir() {
			if name := d.Name(); name == ".git" || name == "vendor" || name == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		fset := token.NewFileSet()
		f, perr := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if perr != nil {
			return nil
		}
		rel := strings.TrimPrefix(path, root+string(filepath.Separator))
		for _, decl := range f.Decls {
			reason, offset, ok := findAnnotation(f, fset, decl)
			if !ok {
				continue
			}
			ident := declIdentifier(decl)
			if ident == "" {
				continue
			}
			items = append(items, annotatedItem{
				File:       rel,
				Line:       fset.Position(decl.Pos()).Line,
				Identifier: ident,
				Reason:     reason,
				Offset:     offset,
			})
		}
		return nil
	})
	return items, err
}

// declIdentifier returns the first exported identifier a declaration declares.
func declIdentifier(decl ast.Decl) string {
	switch d := decl.(type) {
	case *ast.FuncDecl:
		if d.Name != nil && ast.IsExported(d.Name.Name) {
			return d.Name.Name
		}
	case *ast.GenDecl:
		for _, spec := range d.Specs {
			switch s := spec.(type) {
			case *ast.TypeSpec:
				if ast.IsExported(s.Name.Name) {
					return s.Name.Name
				}
			case *ast.ValueSpec:
				for _, n := range s.Names {
					if ast.IsExported(n.Name) {
						return n.Name
					}
				}
			}
		}
	}
	return ""
}

// findAnnotation looks for the marker within scanWindow lines above decl and
// returns the reason plus how far above the declaration it sat.
func findAnnotation(f *ast.File, fset *token.FileSet, decl ast.Decl) (reason string, offset int, found bool) {
	declLine := fset.Position(decl.Pos()).Line
	best := -1
	for _, cg := range f.Comments {
		for _, c := range cg.List {
			line := fset.Position(c.Pos()).Line
			if line >= declLine || line < declLine-scanWindow {
				continue
			}
			text := strings.TrimSpace(strings.TrimPrefix(c.Text, "//"))
			after, ok := strings.CutPrefix(text, marker)
			if !ok {
				continue
			}
			// Nearest annotation wins, so one declaration cannot claim an
			// annotation that belongs to the declaration above it.
			if best == -1 || line > best {
				best = line
				reason = strings.Trim(after, `"`)
			}
		}
	}
	if best == -1 {
		return "", 0, false
	}
	return reason, declLine - best, true
}

// findCallers text-searches the module for mentions of the identifier outside
// its declaring file. Test files count: the analyzer's root set is the tests,
// so a test mention is exactly the kind of reach an annotation would be
// redundant against.
func findCallers(root string, item annotatedItem) []string {
	cmd := exec.Command("grep", "-rln", "--include=*.go", "-w", item.Identifier, ".") //nolint:gosec // G204: identifier comes from the module's own source
	cmd.Dir = root
	out, _ := cmd.Output()

	declFile := filepath.Clean(item.File)
	var callers []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		clean := filepath.Clean(strings.TrimPrefix(line, "./"))
		if clean == declFile || strings.HasPrefix(clean, "script"+string(filepath.Separator)) {
			continue
		}
		callers = append(callers, clean)
	}
	return callers
}

// writeReport renders the audit as markdown.
func writeReport(path string, results []classifiedItem, counts map[string]int) error {
	f, err := os.Create(path) //nolint:gosec // G304: fixed path under the module root
	if err != nil {
		return fmt.Errorf("create: %w", err)
	}
	defer func() { _ = f.Close() }()

	fmt.Fprintf(f, "# `%s` annotation audit\n\n", strings.TrimSuffix(marker, "="))
	fmt.Fprintf(f, "Total: %d — INERT: %d — MASKED: %d — PRODUCTIVE: %d\n\n",
		len(results), counts["INERT"], counts["MASKED"], counts["PRODUCTIVE"])

	if len(results) == 0 {
		fmt.Fprintf(f, "_No annotations in the module. Every exported identifier in the\n")
		fmt.Fprintf(f, "inventory is classified by measurement alone, with nothing exempted\n")
		fmt.Fprintf(f, "by assertion._\n")
		return nil
	}

	section(f, results, "INERT",
		"The annotation is not on the line directly above the declaration, so the\n"+
			"analyzer never sees it and the identifier is judged as if it were absent.\n"+
			"Move it to the last line of the doc comment.")
	section(f, results, "MASKED",
		"Nothing in the module names these identifiers. The annotation is the only\n"+
			"thing keeping them out of the unreached list, so its reason text carries the\n"+
			"whole justification — usually \"a host consumer calls this\". Verify that claim\n"+
			"or delete the identifier; a reason that is not true is worse than a finding.")
	section(f, results, "PRODUCTIVE",
		"These identifiers are named elsewhere in the module, so the annotation is\n"+
			"redundant and can be removed.")
	return nil
}

// section renders one status group.
func section(f *os.File, results []classifiedItem, status, blurb string) {
	var group []classifiedItem
	for _, r := range results {
		if r.Status == status {
			group = append(group, r)
		}
	}
	fmt.Fprintf(f, "## %s (%d)\n\n%s\n\n", status, len(group), blurb)
	if len(group) == 0 {
		fmt.Fprintf(f, "_None._\n\n")
		return
	}
	fmt.Fprintf(f, "| Identifier | File | Line | Reason |\n|---|---|---|---|\n")
	for _, r := range group {
		fmt.Fprintf(f, "| `%s` | `%s` | %d | %s |\n", r.Identifier, r.File, r.Line, r.Reason)
	}
	fmt.Fprintf(f, "\n")
}

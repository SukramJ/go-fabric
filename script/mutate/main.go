// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package main is a mutation sampler: it measures whether the tests notice
// a small, deliberate change to a production file.
//
// # What it is for
//
// Coverage says a line ran under a test; it does not say a test would fail
// if the line were wrong. This program answers the second question for a
// sample of the conditions a package decides on. It is the tool of the
// review's mutation step (review II, C4, decisions 403, 512, 664, 685) and
// runs on GitHub CI by manual dispatch (.github/workflows/mutate.yml), which
// shards a package's files over a runner matrix. It is not a guard: nothing
// fails on a survivor; a survivor is a finding a person reads.
//
// # What it mutates
//
// For every file named on the command line (paths relative to -root):
//
//   - a comparison or logical operator: == ↔ !=, < → <=, <= → <, > → >=,
//     >= → >, && ↔ ||;
//   - a logical not, dropped;
//   - a guard — an if without else whose body ends in return, break,
//     continue, goto or panic — switched off by rewriting its condition to
//     `false && (cond)`; one whose condition is exactly `err != nil` is
//     reported as "guard off (err)".
//
// # How it runs
//
// One mutant at a time per worker (-p), applied through `go test -overlay`:
// the mutated file is written under -tmp and the overlay replaces the
// original in the build, so the tree is never written. The mutant runs the
// tests of its own file's package; when they pass, it runs the packages of
// -wide as well. A test run is bounded by -timeout.
//
// # Output
//
// One TSV line per mutant, sorted by file and line, written to -o:
//
//	verdict  wide-verdict  note  file:line  kind  source line
//
// The verdict is KILLED (the tests failed), SURVIVED (they passed),
// INVALID (the mutant did not build) or NOTRUN (not started within
// -budget). The wide verdict is empty unless the own-package tests let the
// mutant survive and -wide is set. The note is "timeout" (killed by the
// test deadline) or "nospace" (the volume ran out of space; the line is
// INVALID and says nothing about the tests).
//
// # Disk
//
// Every mutant compiles its package afresh, so the build cache grows with
// each one. With -minfree set, the free space of the volume holding the
// build cache ($GOCACHE, else `go env GOCACHE`) is checked before each
// mutant is handed out; below the bound, the builds in flight finish and
// `go clean -cache` runs before the next one starts.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stderr))
}

// mutant is one change to one file.
type mutant struct {
	file       string // relative to root
	line       int
	kind       string
	start, end int // byte offsets replaced
	repl       string
	orig       string // the source line, trimmed
}

// swap maps an operator to the one that replaces it.
var swap = map[token.Token]string{
	token.EQL: "!=", token.NEQ: "==",
	token.LSS: "<=", token.LEQ: "<", token.GTR: ">=", token.GEQ: ">",
	token.LAND: "||", token.LOR: "&&",
}

// collect parses root/rel and returns its mutants and its source.
func collect(root, rel string) ([]mutant, []byte, error) {
	abs := filepath.Join(root, rel)
	src, err := os.ReadFile(abs) //nolint:gosec // a file the caller names, read only
	if err != nil {
		return nil, nil, err
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, abs, src, 0)
	if err != nil {
		return nil, nil, err
	}
	off := func(p token.Pos) int { return fset.Position(p).Offset }
	var out []mutant
	ast.Inspect(f, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.BinaryExpr:
			if r, ok := swap[x.Op]; ok {
				s := off(x.OpPos)
				out = append(out, mutant{rel, fset.Position(x.OpPos).Line, "op " + x.Op.String() + "→" + r, s, s + len(x.Op.String()), r, ""})
			}
		case *ast.UnaryExpr:
			if x.Op == token.NOT {
				s := off(x.OpPos)
				out = append(out, mutant{rel, fset.Position(x.OpPos).Line, "drop !", s, s + 1, "", ""})
			}
		case *ast.IfStmt:
			if !isGuard(x) {
				return true
			}
			s, e := off(x.Cond.Pos()), off(x.Cond.End())
			kind := "guard off"
			if string(src[s:e]) == "err != nil" {
				kind = "guard off (err)"
			}
			out = append(out, mutant{rel, fset.Position(x.Cond.Pos()).Line, kind, s, e, "false && (" + string(src[s:e]) + ")", ""})
		}
		return true
	})
	lines := bytes.Split(src, []byte("\n"))
	for i := range out {
		if l := out[i].line - 1; l < len(lines) {
			out[i].orig = strings.TrimSpace(string(lines[l]))
		}
	}
	return out, src, nil
}

// isGuard reports whether x is an if without else whose body ends in
// return, a branch statement or a call of panic.
func isGuard(x *ast.IfStmt) bool {
	if x.Else != nil || len(x.Body.List) == 0 {
		return false
	}
	switch last := x.Body.List[len(x.Body.List)-1].(type) {
	case *ast.ReturnStmt, *ast.BranchStmt:
		return true
	case *ast.ExprStmt:
		if c, ok := last.X.(*ast.CallExpr); ok {
			if id, ok := c.Fun.(*ast.Ident); ok && id.Name == "panic" {
				return true
			}
		}
	}
	return false
}

// goTest runs `go test` on pkgs with the overlay and returns the verdict
// and the note.
func goTest(root, overlay string, pkgs []string, timeout time.Duration) (verdict, note string) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout+60*time.Second)
	defer cancel()
	args := append([]string{"test", "-count=1", "-overlay", overlay, "-timeout", timeout.String()}, pkgs...)
	cmd := exec.CommandContext(ctx, "go", args...) //nolint:gosec // the go tool, with the caller's packages
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	b, err := cmd.CombinedOutput()
	s := string(b)
	switch {
	case err == nil:
		return "SURVIVED", ""
	case strings.Contains(s, "no space left on device"):
		return "INVALID", "nospace"
	case strings.Contains(s, "[build failed]") || strings.Contains(s, "[setup failed]"):
		return "INVALID", ""
	case strings.Contains(s, "panic: test timed out") || ctx.Err() != nil:
		return "KILLED", "timeout"
	default:
		return "KILLED", ""
	}
}

// goCache is the build cache directory: $GOCACHE, else `go env GOCACHE`;
// empty when neither answers.
func goCache(ctx context.Context) string {
	if d := os.Getenv("GOCACHE"); d != "" {
		return d
	}
	b, err := exec.CommandContext(ctx, "go", "env", "GOCACHE").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// logf writes one diagnostic line; a failed write to stderr has nowhere
// to be reported.
func logf(w io.Writer, format string, a ...any) {
	_, _ = fmt.Fprintf(w, format+"\n", a...)
}

// result is one mutant's outcome.
type result struct {
	m               mutant
	own, wide, note string
}

// config is the parsed command line.
type config struct {
	root, wide, out, tmp string
	workers, minFree     int
	timeout, budget      time.Duration
	files                []string
}

// parseFlags reads the command line; false after a usage error, which it
// has reported.
func parseFlags(args []string, stderr io.Writer) (config, bool) {
	var c config
	fs := flag.NewFlagSet("mutate", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.StringVar(&c.root, "root", ".", "module root")
	fs.IntVar(&c.workers, "p", 4, "parallel mutants")
	fs.StringVar(&c.wide, "wide", "", "comma-separated packages run for a survivor of the own-package tests")
	fs.DurationVar(&c.timeout, "timeout", 120*time.Second, "per test run")
	fs.StringVar(&c.out, "o", "mutants.tsv", "output TSV")
	fs.StringVar(&c.tmp, "tmp", "", "scratch dir for the mutated files and overlays (default: a fresh temporary directory, removed at the end)")
	fs.IntVar(&c.minFree, "minfree", 0, "GiB: when the volume of the build cache has less free, wait for the builds in flight and run `go clean -cache` (0: never)")
	fs.DurationVar(&c.budget, "budget", 0, "stop handing out mutants after this long; the rest are written as NOTRUN (0: no limit)")
	if err := fs.Parse(args); err != nil {
		return c, false
	}
	if c.workers < 1 {
		logf(stderr, "mutate: -p must be at least 1")
		return c, false
	}
	c.files = fs.Args()
	return c, true
}

// run is the program: it parses args, runs the mutants and writes the TSV.
// It returns the exit code: 0 done, 2 a usage or input error, 1 the output
// could not be written.
func run(args []string, stderr io.Writer) int {
	c, ok := parseFlags(args, stderr)
	if !ok {
		return 2
	}
	if c.tmp == "" {
		d, err := os.MkdirTemp("", "mutate-")
		if err != nil {
			logf(stderr, "mutate: %v", err)
			return 2
		}
		defer func() { _ = os.RemoveAll(d) }()
		c.tmp = d
	}
	// The overlay names files by the path the go command resolves, which
	// follows symbolic links (macOS's /var is /private/var): resolve them
	// here too, or the overlay matches nothing and every mutant survives.
	root, err := filepath.Abs(c.root)
	if err == nil {
		root, err = filepath.EvalSymlinks(root)
	}
	if err != nil {
		logf(stderr, "mutate: %v", err)
		return 2
	}
	c.root = root
	var all []mutant
	srcs := map[string][]byte{}
	for _, rel := range c.files {
		ms, src, err := collect(c.root, rel)
		if err != nil {
			logf(stderr, "mutate: %v", err)
			return 2
		}
		srcs[rel] = src
		all = append(all, ms...)
	}
	logf(stderr, "%d mutants in %d files", len(all), len(srcs))
	results := mutateAll(c, all, srcs, stderr)
	if err := os.WriteFile(c.out, tsv(results), 0o600); err != nil {
		logf(stderr, "mutate: %v", err)
		return 1
	}
	return 0
}

// mutateAll runs every mutant on c.workers workers and returns the
// results in the order of all; a mutant not started within c.budget is
// NOTRUN.
func mutateAll(c config, all []mutant, srcs map[string][]byte, stderr io.Writer) []result {
	ctx := context.Background()
	var wides []string
	if c.wide != "" {
		wides = strings.Split(c.wide, ",")
	}
	cache := ""
	if c.minFree > 0 {
		cache = goCache(ctx)
	}
	results := make([]result, len(all))
	for i := range all {
		results[i] = result{m: all[i], own: "NOTRUN"}
	}
	var wg sync.WaitGroup
	ch := make(chan int)
	// gate: a worker holds it shared while its build runs; the cache clean
	// takes it exclusively, so it never pulls files from under a build.
	var gate sync.RWMutex
	for w := range c.workers {
		wg.Go(func() {
			mf := filepath.Join(c.tmp, fmt.Sprintf("w%d.go", w))
			of := filepath.Join(c.tmp, fmt.Sprintf("w%d.json", w))
			for i := range ch {
				gate.RLock()
				results[i] = mutateOne(c, &all[i], srcs[all[i].file], wides, mf, of)
				gate.RUnlock()
			}
		})
	}
	start := time.Now()
	for i := range all {
		if c.budget > 0 && time.Since(start) > c.budget {
			logf(stderr, "budget %s spent: %d of %d mutants not run", c.budget, len(all)-i, len(all))
			break
		}
		if c.minFree > 0 && freeGiB(cache) < c.minFree {
			gate.Lock()
			if b, err := exec.CommandContext(ctx, "go", "clean", "-cache").CombinedOutput(); err != nil {
				logf(stderr, "go clean -cache: %v: %s", err, b)
			}
			gate.Unlock()
		}
		ch <- i
	}
	close(ch)
	wg.Wait()
	return results
}

// mutateOne writes the mutated file and its overlay to mf and of and runs
// the tests against them.
func mutateOne(c config, m *mutant, src []byte, wides []string, mf, of string) result {
	mut := append(append(append([]byte{}, src[:m.start]...), m.repl...), src[m.end:]...)
	j, err := json.Marshal(map[string]map[string]string{"Replace": {filepath.Join(c.root, m.file): mf}})
	if err == nil {
		err = errors.Join(os.WriteFile(mf, mut, 0o600), os.WriteFile(of, j, 0o600))
	}
	if err != nil {
		note := ""
		if strings.Contains(err.Error(), "no space left on device") {
			note = "nospace"
		}
		return result{m: *m, own: "INVALID", note: note}
	}
	own, note := goTest(c.root, of, []string{"./" + filepath.Dir(m.file)}, c.timeout)
	wide := ""
	if own == "SURVIVED" && len(wides) > 0 {
		wide, note = goTest(c.root, of, wides, c.timeout)
	}
	return result{m: *m, own: own, wide: wide, note: note}
}

// tsv renders the results, sorted by file and then line.
func tsv(results []result) []byte {
	sort.SliceStable(results, func(a, b int) bool {
		if results[a].m.file != results[b].m.file {
			return results[a].m.file < results[b].m.file
		}
		return results[a].m.line < results[b].m.line
	})
	var buf bytes.Buffer
	for i := range results {
		r := &results[i]
		fmt.Fprintf(&buf, "%s\t%s\t%s\t%s:%d\t%s\t%s\n", r.own, r.wide, r.note, r.m.file, r.m.line, r.m.kind, r.m.orig)
	}
	return buf.Bytes()
}

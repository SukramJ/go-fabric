// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fixture is a module of one package with two mutants: Eq's == → != fails
// TestEq (killed); Pos's > → >= is invisible to TestPos, which never asks
// about zero (survived).
var fixture = map[string]string{
	"go.mod": "module example.test/fixture\n\ngo 1.22\n",
	"p/p.go": `package p

// Eq reports whether a equals b.
func Eq(a, b int) bool { return a == b }

// Pos reports whether x is positive.
func Pos(x int) bool { return x > 0 }
`,
	"p/p_test.go": `package p

import "testing"

func TestEq(t *testing.T) {
	if !Eq(1, 1) || Eq(1, 2) {
		t.Fatal("Eq")
	}
}

func TestPos(t *testing.T) {
	if !Pos(1) || Pos(-1) {
		t.Fatal("Pos")
	}
}
`,
}

func writeFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for name, src := range fixture {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// TestMutateKilledAndSurvived runs the program on the fixture: one TSV line
// per mutant, sorted by line, the survivor run again under -wide, and the
// tree unchanged afterwards.
func TestMutateKilledAndSurvived(t *testing.T) {
	t.Setenv("GOWORK", "off")
	t.Setenv("GOFLAGS", "")
	root := writeFixture(t)
	out := filepath.Join(t.TempDir(), "mutants.tsv")
	var stderr bytes.Buffer
	code := run([]string{"-root", root, "-p", "2", "-wide", "./p", "-timeout", "60s", "-minfree", "1", "-o", out, "p/p.go"}, &stderr)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "2 mutants in 1 files") {
		t.Errorf("stderr %q: want the mutant count", stderr.String())
	}
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	want := "KILLED\t\t\tp/p.go:4\top ==→!=\tfunc Eq(a, b int) bool { return a == b }\n" +
		"SURVIVED\tSURVIVED\t\tp/p.go:7\top >→>=\tfunc Pos(x int) bool { return x > 0 }\n"
	if string(got) != want {
		t.Errorf("TSV:\n%s\nwant:\n%s", got, want)
	}
	src, err := os.ReadFile(filepath.Join(root, "p", "p.go"))
	if err != nil {
		t.Fatal(err)
	}
	if string(src) != fixture["p/p.go"] {
		t.Error("the tree was written: p/p.go changed")
	}
}

// TestMutateBudgetSpent: a budget spent before the first mutant leaves
// every mutant NOTRUN and runs no test.
func TestMutateBudgetSpent(t *testing.T) {
	root := writeFixture(t)
	ms, src, err := collect(root, "p/p.go")
	if err != nil {
		t.Fatal(err)
	}
	c := config{root: root, tmp: t.TempDir(), workers: 1, timeout: time.Minute, budget: time.Second, start: time.Now().Add(-time.Hour)}
	var stderr bytes.Buffer
	got := string(tsv(mutateAll(c, ms, map[string][]byte{"p/p.go": src}, &stderr)))
	if n := strings.Count(got, "NOTRUN\t"); n != 2 {
		t.Errorf("TSV:\n%s\nwant two NOTRUN lines", got)
	}
	if !strings.Contains(stderr.String(), "2 of 2 mutants not run") {
		t.Errorf("stderr %q: want the budget line", stderr.String())
	}
}

// TestCollectKinds: every mutation kind the package doc names, in source
// order (a guard before its condition's operators); the if with an else
// and the one whose body does not leave are not guards.
func TestCollectKinds(t *testing.T) {
	root := t.TempDir()
	src := `package q

func f(a, b int, err error) int {
	if err != nil {
		return 0
	}
	if a < b && !(a <= b) {
		panic("x")
	}
	for a >= b {
		if a != 0 {
			break
		}
	}
	if a == 1 || b > 2 {
		a++
	}
	if a == 2 {
		return 1
	} else {
		return 2
	}
}
`
	if err := os.WriteFile(filepath.Join(root, "q.go"), []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	ms, _, err := collect(root, "q.go")
	if err != nil {
		t.Fatal(err)
	}
	kinds := make([]string, 0, len(ms))
	for _, m := range ms {
		kinds = append(kinds, m.kind)
	}
	want := []string{
		"guard off (err)", "op !=→==",
		"guard off", "op &&→||", "op <→<=", "drop !", "op <=→<",
		"op >=→>",
		"guard off", "op !=→==",
		"op ||→&&", "op ==→!=", "op >→>=",
		"op ==→!=",
	}
	if strings.Join(kinds, "|") != strings.Join(want, "|") {
		t.Errorf("kinds\n%q\nwant\n%q", kinds, want)
	}
}

// TestRunRefusesBadInput: a missing file, a bad -p and a -wide entry that
// is empty or a go test flag (P10-sup-6902) are usage errors.
func TestRunRefusesBadInput(t *testing.T) {
	var stderr bytes.Buffer
	if code := run([]string{"-root", t.TempDir(), "-o", filepath.Join(t.TempDir(), "x.tsv"), "missing.go"}, &stderr); code != 2 {
		t.Errorf("missing file: exit %d, want 2", code)
	}
	if code := run([]string{"-p", "0"}, &stderr); code != 2 {
		t.Errorf("-p 0: exit %d, want 2", code)
	}
	if code := run([]string{"-nosuchflag"}, &stderr); code != 2 {
		t.Errorf("unknown flag: exit %d, want 2", code)
	}
	for _, wide := range []string{"-exec,./x", "./p,", ",./p", "./p,-run=X"} {
		if code := run([]string{"-wide", wide, "-o", filepath.Join(t.TempDir(), "x.tsv")}, &stderr); code != 2 {
			t.Errorf("-wide %q: exit %d, want 2", wide, code)
		}
	}
}

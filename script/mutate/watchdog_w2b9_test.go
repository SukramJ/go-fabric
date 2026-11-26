// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

//go:build linux || darwin

package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
)

// memFixture is a module whose one guard bounds an allocation loop: More
// stops the test at three blocks of 16 MiB. Switched off, the loop runs to
// its own bound of 64 blocks (1 GiB, touched) and then holds them, spinning,
// until the test deadline — the runaway the watchdog is for (decision 693),
// held so that no poll can miss it; `>=` → `>` takes a fourth block and
// stays far below the limit. The test appends its pid to $MUTATE_PIDS_W2B9.
var memFixture = map[string]string{
	"go.mod": "module example.test/memfixture\n\ngo 1.22\n",
	"m/m.go": `package m

// More reports whether the i-th block is still wanted.
func More(i int) bool {
	if i >= 3 {
		return false
	}
	return true
}
`,
	"m/m_test.go": `package m

import (
	"os"
	"runtime"
	"strconv"
	"testing"
)

var keep [][]byte

func TestMore(t *testing.T) {
	f, err := os.OpenFile(os.Getenv("MUTATE_PIDS_W2B9"), os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.WriteString(strconv.Itoa(os.Getpid()) + "\n")
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 64 && More(i); i++ {
		b := make([]byte, 16<<20)
		for j := 0; j < len(b); j += 4096 {
			b[j] = 1
		}
		keep = append(keep, b)
	}
	for len(keep) > 4 {
		runtime.Gosched()
	}
}
`,
}

// TestMutateMaxRSSKillsTheGroupW2B9 runs the program on memFixture under
// -maxrss 384MiB: the guard-off mutant is KILLED with the note memory, the
// other one — same build, same tool, 64 MiB — survives, so the build alone
// does not trip the limit; and no test binary is left running afterwards.
func TestMutateMaxRSSKillsTheGroupW2B9(t *testing.T) {
	t.Setenv("GOWORK", "off")
	t.Setenv("GOFLAGS", "")
	root := t.TempDir()
	for name, src := range memFixture {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// A cold build cache would compile the standard library under the
	// limit; build once first, so the runs below measure the mutants.
	warm := exec.CommandContext(t.Context(), "go", "test", "-count=1", "-run", "^$", "./m")
	warm.Dir = root
	warm.Env = append(os.Environ(), "CGO_ENABLED=0")
	if b, err := warm.CombinedOutput(); err != nil {
		t.Fatalf("warm-up build: %v\n%s", err, b)
	}
	pids := filepath.Join(t.TempDir(), "pids")
	t.Setenv("MUTATE_PIDS_W2B9", pids)
	out := filepath.Join(t.TempDir(), "mutants.tsv")
	var stderr bytes.Buffer
	code := run([]string{"-root", root, "-p", "1", "-maxrss", "384MiB", "-timeout", "20s", "-o", out, "m/m.go"}, &stderr)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	want := "KILLED\t\tmemory\tm/m.go:5\tguard off\tif i >= 3 {\n" +
		"SURVIVED\t\t\tm/m.go:5\top >=→>\tif i >= 3 {\n"
	if string(got) != want {
		t.Errorf("TSV:\n%s\nwant:\n%s", got, want)
	}
	b, err := os.ReadFile(pids)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Fields(string(b))
	if len(lines) != 2 {
		t.Fatalf("pids %q: want one per mutant run", lines)
	}
	for _, l := range lines {
		pid, err := strconv.Atoi(l)
		if err != nil {
			t.Fatal(err)
		}
		if err := syscall.Kill(pid, 0); !errors.Is(err, syscall.ESRCH) {
			t.Errorf("test binary %d still exists after the run (kill 0: %v)", pid, err)
		}
	}
}

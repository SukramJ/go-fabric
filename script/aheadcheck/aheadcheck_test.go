// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// The fixture tests build a matter.js stand-in as a git repository in a
// temporary directory (the marker text present, then removed) and run the
// check over testdata/register.md. The register tests read the committed
// notes/parity/by_design.md and todo-marker.txt; the one that reads the
// markers at the pin needs the matter.js clone and skips without it.
package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SukramJ/go-fabric/parity"
)

// fixtureRepo is a git repository with three commits: the markers present
// (first), the absence marker's class gaining a member (second), the
// presence marker's file removed and the class marker's class gaining a
// member (third).
type fixtureRepo struct {
	dir                  string
	first, second, third string
}

func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.CommandContext(context.Background(), "git", append([]string{"-C", dir, "-c", "commit.gpgsign=false"}, args...)...)
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_NAME=fixture", "GIT_AUTHOR_EMAIL=fixture@example.invalid",
		"GIT_COMMITTER_NAME=fixture", "GIT_COMMITTER_EMAIL=fixture@example.invalid")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, stderr.String())
	}
	return strings.TrimSpace(string(out))
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func newFixtureRepo(t *testing.T) fixtureRepo {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	dir := t.TempDir()
	gitIn(t, dir, "init", "-q")
	commit := func(msg string) string {
		gitIn(t, dir, "add", "-A")
		gitIn(t, dir, "commit", "-q", "-m", msg)
		return gitIn(t, dir, "rev-parse", "HEAD")
	}
	server := filepath.Join(dir, "src", "FixtureServer.ts")
	writeFile(t, server, "export class FixtureServer extends FixtureBehavior {}\n")
	writeFile(t, filepath.Join(dir, "src", "Other.ts"), "/** The work is left to the device.\n */\n")
	multi := filepath.Join(dir, "src", "Multi.ts")
	writeFile(t, multi, "export class Base {\n    b() {}\n}\n\nexport class Multi extends Base {\n    a() {}\n}\n\nexport class After {}\n")
	r := fixtureRepo{dir: dir}
	r.first = commit("the markers present")
	writeFile(t, server, "export class FixtureServer extends FixtureBehavior {\n    override initialize() {}\n}\n")
	r.second = commit("the absence marker's class gains a member")
	if err := os.Remove(filepath.Join(dir, "src", "Other.ts")); err != nil {
		t.Fatal(err)
	}
	writeFile(t, multi, "export class Base {\n    b() {}\n}\n\nexport class Multi extends Base {\n    a() {}\n    c() {}\n}\n\nexport class After {}\n")
	r.third = commit("the presence marker's file removed, the class marker's class gains a member")
	return r
}

// fixtureRoot lays testdata/register.md and an allowance out as a module
// root.
func fixtureRoot(t *testing.T, allowance string) string {
	t.Helper()
	reg, err := os.ReadFile(filepath.Join("testdata", "register.md"))
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	writeFile(t, filepath.Join(root, registerPath), string(reg))
	if allowance != "" {
		writeFile(t, filepath.Join(root, allowancePath), allowance)
	}
	return root
}

func runFixture(t *testing.T, root, dir, pin, ref string) (code int, lines []string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code = run(&out, &errOut, root, dir, pin, ref)
	if errOut.Len() > 0 {
		t.Logf("stderr: %s", errOut.String())
	}
	for l := range strings.SplitSeq(strings.TrimSpace(out.String()), "\n") {
		if l != "" {
			lines = append(lines, l)
		}
	}
	return code, lines
}

func TestAheadcheckQuietWhileTheMarkersHold(t *testing.T) {
	repo := newFixtureRepo(t)
	root := fixtureRoot(t, "# fixture\nBD-Fixture-Todo\n")
	code, lines := runFixture(t, root, repo.dir, repo.first, repo.first)
	if code != 0 || len(lines) != 1 || !strings.Contains(lines[0], "every marker holds") {
		t.Fatalf("at the first commit = %d %q, want 0 and the all-clear", code, lines)
	}
	if !strings.Contains(lines[0], "4 rows, 3 with a marker, 1 in todo-marker.txt") {
		t.Errorf("summary %q does not count the section's rows only", lines[0])
	}
}

func TestAheadcheckReportsTheRowAtTheRef(t *testing.T) {
	repo := newFixtureRepo(t)
	root := fixtureRoot(t, "BD-Fixture-Todo\n")
	code, lines := runFixture(t, root, repo.dir, repo.first, repo.second)
	if code != 1 || len(lines) != 1 {
		t.Fatalf("at the second commit = %d %q, want 1 and one finding", code, lines)
	}
	for _, want := range []string{"BD-Fixture-Absence", "caught up?", "src/FixtureServer.ts", repo.second[:8]} {
		if !strings.Contains(lines[0], want) {
			t.Errorf("finding %q does not name %q", lines[0], want)
		}
	}
	code, lines = runFixture(t, root, repo.dir, repo.first, repo.third)
	if code != 1 || len(lines) != 3 {
		t.Fatalf("at the third commit = %d %q, want 1 and three findings", code, lines)
	}
	if !strings.Contains(lines[2], "BD-Fixture-Class") || !strings.Contains(lines[2], "the body of class Multi in src/Multi.ts differs") {
		t.Errorf("third finding %q does not name the class row", lines[2])
	}
	if !strings.Contains(lines[1], "BD-Fixture-TwoFiles") || !strings.Contains(lines[1], "src/Other.ts does not exist") {
		t.Errorf("second finding %q does not name the row and the gone file", lines[1])
	}
}

func TestAheadcheckRowWrongAtThePin(t *testing.T) {
	repo := newFixtureRepo(t)
	root := fixtureRoot(t, "BD-Fixture-Todo\n")
	code, lines := runFixture(t, root, repo.dir, repo.second, repo.second)
	if code != 1 || len(lines) != 1 || !strings.Contains(lines[0], "BD-Fixture-Absence") || !strings.Contains(lines[0], "the row is wrong") {
		t.Fatalf("pin at the second commit = %d %q, want the absence row wrong at the pin", code, lines)
	}
}

func TestAheadcheckAllowance(t *testing.T) {
	repo := newFixtureRepo(t)
	code, lines := runFixture(t, fixtureRoot(t, ""), repo.dir, repo.first, repo.first)
	if code != 1 || len(lines) != 1 || !strings.Contains(lines[0], "BD-Fixture-Todo") || !strings.Contains(lines[0], "no marker") {
		t.Errorf("without the allowance = %d %q, want the unmarked row refused", code, lines)
	}
	code, lines = runFixture(t, fixtureRoot(t, "BD-Fixture-Todo\nBD-Fixture-Absence\nBD-Fixture-Gone # closed\n"), repo.dir, repo.first, repo.first)
	if code != 1 || len(lines) != 2 {
		t.Fatalf("with a stale allowance = %d %q, want two findings", code, lines)
	}
	if !strings.Contains(lines[0], "BD-Fixture-Absence") || !strings.Contains(lines[0], "still in todo-marker.txt") {
		t.Errorf("first finding %q: want the marked row still allowed", lines[0])
	}
	if !strings.Contains(lines[1], "BD-Fixture-Gone") || !strings.Contains(lines[1], "no row of the register") {
		t.Errorf("second finding %q: want the allowance naming no row", lines[1])
	}
}

// fixtureRootCaught lays the fixture register out with caught-up.txt.
func fixtureRootCaught(t *testing.T, caught string) string {
	t.Helper()
	root := fixtureRoot(t, "BD-Fixture-Todo\n")
	writeFile(t, filepath.Join(root, caughtUpPath), caught)
	return root
}

// TestAheadcheckCaughtUp: a row in caught-up.txt passes while its marker is
// gone at the pin the line names, and is a finding when the marker still
// holds, when the line names another pin, or when it names no row.
func TestAheadcheckCaughtUp(t *testing.T) {
	repo := newFixtureRepo(t)

	// Listed, the marker gone at the pin: the run passes and says so.
	root := fixtureRootCaught(t, "# fixture\nBD-Fixture-Absence "+repo.second[:8]+" P9-Z # closes later\n")
	code, lines := runFixture(t, root, repo.dir, repo.second, repo.second)
	if code != 0 || len(lines) != 2 {
		t.Fatalf("listed, marker gone = %d %q, want 0, the note and the summary", code, lines)
	}
	for _, want := range []string{"BD-Fixture-Absence", "caught up at " + repo.second[:8], "reconciled by P9-Z"} {
		if !strings.Contains(lines[0], want) {
			t.Errorf("note %q does not name %q", lines[0], want)
		}
	}
	if !strings.Contains(lines[1], "1 in caught-up.txt") || !strings.Contains(lines[1], "every other row's marker holds") {
		t.Errorf("summary %q does not count the caught-up row", lines[1])
	}
	// The ref is not read for a listed row: at the third commit only the
	// two other rows report "caught up?".
	code, lines = runFixture(t, root, repo.dir, repo.second, repo.third)
	if code != 1 || len(lines) != 3 || !strings.Contains(lines[0], "reconciled by P9-Z") ||
		!strings.Contains(lines[1], "BD-Fixture-TwoFiles") || !strings.Contains(lines[2], "BD-Fixture-Class") {
		t.Errorf("listed, ref at the third commit = %d %q, want the note and the two other rows' findings", code, lines)
	}

	// Listed, the marker still holds at the pin.
	root = fixtureRootCaught(t, "BD-Fixture-Absence "+repo.first+" P9-Z\n")
	code, lines = runFixture(t, root, repo.dir, repo.first, repo.first)
	if code != 1 || len(lines) != 1 || !strings.Contains(lines[0], "BD-Fixture-Absence") ||
		!strings.Contains(lines[0], "the marker still holds at the pin ("+repo.first[:8]+") — remove its line") {
		t.Errorf("listed, marker present = %d %q, want the still-holds finding", code, lines)
	}

	// A line naming no row.
	root = fixtureRootCaught(t, "BD-Fixture-Gone "+repo.first[:8]+" P9-Z\n")
	code, lines = runFixture(t, root, repo.dir, repo.first, repo.first)
	if code != 1 || len(lines) != 1 || !strings.Contains(lines[0], "caught-up.txt names BD-Fixture-Gone, which is no row") {
		t.Errorf("stale line = %d %q, want the stale-line finding", code, lines)
	}

	// The line names another pin than the current one, the marker gone at
	// the current pin: the row rode across a lift.
	root = fixtureRootCaught(t, "BD-Fixture-Absence "+repo.first[:8]+" P9-Z\n")
	code, lines = runFixture(t, root, repo.dir, repo.second, repo.second)
	if code != 1 || len(lines) != 1 || !strings.Contains(lines[0], "BD-Fixture-Absence") ||
		!strings.Contains(lines[0], "caught up at "+repo.first[:8]+", but the pin is "+repo.second[:8]) {
		t.Errorf("sha off the pin = %d %q, want the pin finding", code, lines)
	}

	// A listed row without a marker.
	root = fixtureRootCaught(t, "BD-Fixture-Todo "+repo.first[:8]+" P9-Z\n")
	code, lines = runFixture(t, root, repo.dir, repo.first, repo.first)
	if code != 1 || len(lines) != 1 || !strings.Contains(lines[0], "BD-Fixture-Todo") || !strings.Contains(lines[0], "has no marker but is in caught-up.txt") {
		t.Errorf("unmarked row listed = %d %q", code, lines)
	}

	// A malformed line, or a row listed twice: the check cannot run.
	for name, content := range map[string]string{
		"two fields": "BD-Fixture-Absence P9-Z\n",
		"short sha":  "BD-Fixture-Absence abc P9-Z\n",
		"upper sha":  "BD-Fixture-Absence ABCDEF12 P9-Z\n",
		"twice":      "BD-Fixture-Absence " + repo.first[:8] + " P9-Z\nBD-Fixture-Absence " + repo.first[:8] + " P9-Y\n",
	} {
		if code, _ := runFixture(t, fixtureRootCaught(t, content), repo.dir, repo.first, repo.first); code != 2 {
			t.Errorf("%s: exit %d, want 2", name, code)
		}
	}
}

func TestAheadcheckCannotRun(t *testing.T) {
	repo := newFixtureRepo(t)
	root := fixtureRoot(t, "BD-Fixture-Todo\n")
	for name, c := range map[string]struct{ root, dir, pin, ref string }{
		"no clone":        {root, filepath.Join(t.TempDir(), "none"), repo.first, repo.first},
		"pin unknown":     {root, repo.dir, strings.Repeat("0", 40), repo.first},
		"ref unknown":     {root, repo.dir, repo.first, "origin/none"},
		"no register":     {t.TempDir(), repo.dir, repo.first, repo.first},
		"allowance a dir": {fixtureRootWithDirAllowance(t), repo.dir, repo.first, repo.first},
	} {
		if code, _ := runFixture(t, c.root, c.dir, c.pin, c.ref); code != 2 {
			t.Errorf("%s: exit %d, want 2", name, code)
		}
	}
}

func fixtureRootWithDirAllowance(t *testing.T) string {
	t.Helper()
	root := fixtureRoot(t, "")
	if err := os.MkdirAll(filepath.Join(root, allowancePath), 0o750); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestParseRegisterRefuses(t *testing.T) {
	head := registerHeading + "\n\n"
	table := "| Entry | marker file | marker text |\n| --- | --- | --- |\n"
	for name, c := range map[string]struct {
		doc     string
		problem string
	}{
		"no heading": {doc: table + "| A | `a.ts` | `\"x\"` |\n"},
		"no columns": {doc: head + "| Entry | marker |\n| --- | --- |\n| A | x |\n"},
		"no table":   {doc: head + "prose\n\n## next\n"},
		"no rows":    {doc: head + table + "\nprose\n"},
		"short row":  {doc: head + table + "| A | `a.ts` |\n"},
		"count":      {doc: head + table + "| A | `a.ts`<br>`b.ts` | `\"x\"` |\n", problem: "2 marker files but 1 marker texts"},
		"unquoted":   {doc: head + table + "| A | `a.ts` | `x` |\n", problem: "neither"},
		"half":       {doc: head + table + "| A | `a.ts` | not recorded |\n", problem: "neither"},
		"bad escape": {doc: head + table + "| A | `a.ts` | `\"\\q\"` |\n", problem: "no Go-quoted string"},
		"empty":      {doc: head + table + "| A | `a.ts` | `\" \"` |\n", problem: "empty marker text"},
	} {
		rows, err := parseRegister(c.doc)
		if c.problem == "" {
			if err == nil {
				t.Errorf("%s: parsed %v, want an error", name, rows)
			}
			continue
		}
		if err != nil || len(rows) != 1 || !strings.Contains(rows[0].problem, c.problem) {
			t.Errorf("%s: rows %+v, err %v; want one row with the problem %q", name, rows, err, c.problem)
			continue
		}
		if f, _ := check(rows, nil, nil, nil, revision{}, revision{}); len(f) != 1 || !strings.Contains(f[0], c.problem) {
			t.Errorf("%s: findings %q, want the problem reported", name, f)
		}
	}
}

func TestReadPin(t *testing.T) {
	if _, err := readPin([]byte("{")); err == nil {
		t.Error("readPin of broken JSON succeeded")
	}
	if _, err := readPin([]byte(`{"matter":{}}`)); err == nil {
		t.Error("readPin without sourceCommit succeeded")
	}
	if pin, err := readPin(parity.SchemaJSON()); err != nil || len(pin) != 40 {
		t.Errorf("readPin of the extract = %q, %v; want a full commit id", pin, err)
	}
	if firstNonEmpty("", "b", "c") != "b" || firstNonEmpty("", "") != "" || short("abc") != "abc" {
		t.Error("helpers")
	}
}

// TestAheadcheckRegisterParses holds the committed register to the column
// contract without the clone: every row parses, every row without a marker
// is allowed, and the allowance names only such rows.
func TestAheadcheckRegisterParses(t *testing.T) {
	root := filepath.Join("..", "..")
	doc, err := os.ReadFile(filepath.Join(root, registerPath))
	if err != nil {
		t.Fatal(err)
	}
	rows, err := parseRegister(string(doc))
	if err != nil {
		t.Fatalf("the register does not parse: %v", err)
	}
	allow, err := readAllowance(filepath.Join(root, allowancePath))
	if err != nil {
		t.Fatal(err)
	}
	// The unmarked rows go through check without a show: an allowed row
	// reads nothing. The marked ones are read at the pin by the next test.
	marked := rows[:0:0]
	for _, r := range rows {
		if !r.noMarker {
			marked = append(marked, r)
		}
	}
	unmarked := rows[:0:0]
	for _, r := range rows {
		if r.noMarker {
			unmarked = append(unmarked, r)
		}
	}
	if f, _ := check(unmarked, allow, nil, nil, revision{}, revision{}); len(f) > 0 {
		t.Errorf("the register against todo-marker.txt:\n%s", strings.Join(f, "\n"))
	}
	for _, r := range marked {
		if r.problem != "" {
			t.Errorf("%s (by_design.md:%d): %s", r.entry, r.line, r.problem)
		}
		if allow[r.entry] {
			t.Errorf("%s has a marker but is still in todo-marker.txt", r.entry)
		}
	}
	if len(marked) == 0 {
		t.Error("no register row has a marker")
	}
	// caught-up.txt names marked rows only, each at the current pin; that
	// their markers are gone there is read by the next test.
	caught, err := readCaughtUp(filepath.Join(root, caughtUpPath))
	if err != nil {
		t.Fatal(err)
	}
	pin, err := readPin(parity.SchemaJSON())
	if err != nil {
		t.Fatal(err)
	}
	byEntry := map[string]row{}
	for _, r := range rows {
		byEntry[r.entry] = r
	}
	for name, c := range caught {
		r, ok := byEntry[name]
		switch {
		case !ok:
			t.Errorf("caught-up.txt names %s, which is no row of the register", name)
		case r.noMarker:
			t.Errorf("caught-up.txt names %s, which has no marker", name)
		case !strings.HasPrefix(pin, c.pin):
			t.Errorf("caught-up.txt lists %s at %s, the pin is %s", name, c.pin, pin)
		}
	}
}

// TestAheadcheckRegisterHoldsAtThePin reads every marker of the committed
// register at the pin. It needs the matter.js clone named by MATTERJS_DIR
// and skips without it, as make cite-check needs it (`make ahead-check`
// and the weekly job run the same check against a ref).
func TestAheadcheckRegisterHoldsAtThePin(t *testing.T) {
	dir := os.Getenv("MATTERJS_DIR")
	if dir == "" {
		t.Skip("MATTERJS_DIR is not set: the markers are read at the pin by make ahead-check and the weekly matterjs-pin job")
	}
	pin, err := readPin(parity.SchemaJSON())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := (gitRepo{dir: dir}).commit(pin); err != nil {
		t.Skipf("MATTERJS_DIR=%s has no pin %s: %v", dir, pin, err)
	}
	var out, errOut bytes.Buffer
	if code := run(&out, &errOut, filepath.Join("..", ".."), dir, pin, pin); code != 0 {
		t.Errorf("exit %d at the pin:\n%s%s", code, out.String(), errOut.String())
	}
}

func TestClassBody(t *testing.T) {
	src := "export class Base {\n    b() {}\n}\nclass Multi{\n    a() {}\n}\nexport class Empty extends Base {}\nexport class Open {\n    x() {}\n"
	for name, want := range map[string]string{
		"Base":  "export class Base {\n    b() {}\n}\n",
		"Multi": "class Multi{\n    a() {}\n}\n",
		"Empty": "export class Empty extends Base {}\n",
	} {
		if got, ok := classBody(src, name); !ok || got != want {
			t.Errorf("classBody(%s) = %q, %v; want %q", name, got, ok, want)
		}
	}
	for _, name := range []string{"Open", "None", "Bas"} {
		if got, ok := classBody(src, name); ok {
			t.Errorf("classBody(%s) = %q, want none", name, got)
		}
	}
}

func TestClassMarkerWrongAtThePin(t *testing.T) {
	rows, err := parseRegister(registerHeading + "\n\n| Entry | marker file | marker text |\n| --- | --- | --- |\n" +
		"| A | `a.ts` | `class None` |\n| B | `gone.ts` | `class B` |\n| C | `a.ts` | `class Some extra` |\n")
	if err != nil {
		t.Fatal(err)
	}
	show := func(_, path string) (string, error) {
		if path == "a.ts" {
			return "export class Some {}\n", nil
		}
		return "", errNoFile
	}
	pin := revision{name: "the pin", sha: "p"}
	f, _ := check(rows, nil, nil, show, pin, revision{name: "ref", sha: "r"})
	if len(f) != 3 || !strings.Contains(f[0], "no class None in a.ts") || !strings.Contains(f[1], "gone.ts does not exist") ||
		!strings.Contains(f[2], "neither a Go-quoted string") {
		t.Errorf("findings %q", f)
	}
}

// TestReadFailureIsExitTwo: a read that fails for a reason other than a
// path absent at the revision — a lazy blob fetch that failed — makes the
// run exit 2, not a finding (R2-P6-err-2812).
func TestReadFailureIsExitTwo(t *testing.T) {
	rows, err := parseRegister(registerHeading + "\n\n| Entry | marker file | marker text |\n| --- | --- | --- |\n" +
		"| A | `a.ts` | `\"x\"` |\n")
	if err != nil {
		t.Fatal(err)
	}
	show := func(_, _ string) (string, error) { return "", errors.New("fatal: unable to fetch blob") }
	var out, errOut strings.Builder
	if code := report(&out, &errOut, rows, nil, nil, show, revision{name: "the pin", sha: "p"}, revision{name: "ref", sha: "r"}); code != 2 {
		t.Errorf("exit %d, want 2; out %q", code, out.String())
	}
	if !strings.Contains(errOut.String(), "unable to fetch blob") || out.Len() != 0 {
		t.Errorf("out %q, errOut %q", out.String(), errOut.String())
	}
}

// TestGitShowTellsAbsentFromUnreadable: a path the tree does not list is
// errNoFile; a listed one is read.
func TestGitShowTellsAbsentFromUnreadable(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.ts"), []byte("x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	repo := gitRepo{dir: dir}
	for _, args := range [][]string{{"init", "-q"}, {"add", "a.ts"}, {"-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "-m", "a"}} {
		if _, err := repo.git(args...); err != nil {
			t.Fatal(err)
		}
	}
	sha, err := repo.commit("HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if got, err := repo.show(sha, "a.ts"); err != nil || got != "x\n" {
		t.Errorf("show a.ts = %q, %v", got, err)
	}
	if _, err := repo.show(sha, "b.ts"); !errors.Is(err, errNoFile) {
		t.Errorf("show b.ts: %v, want errNoFile", err)
	}
	if _, err := (gitRepo{dir: t.TempDir()}).show(sha, "a.ts"); err == nil || errors.Is(err, errNoFile) {
		t.Errorf("show outside a repository: %v, want an error that is not errNoFile", err)
	}
}

// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package main implements the Ahead-of-matter.js marker check (issue #274,
// brief CN, `make ahead-check`).
//
// The *Ahead of matter.js* register at the head of notes/parity/by_design.md
// lists the behaviour this module implements where matter.js implements none
// (CLAUDE.md, workflow step 5). Each row names a marker: the matter.js file
// and a quoted text that holds while the gap exists. When matter.js closes
// the gap the text goes, and the two implementations are reconciled at the
// next pin lift (notes/parity/pin-lifts.md, the verdict "caught up"). This
// program is the trigger for that reconciliation; the manual re-read at the
// lift is its confirmation.
//
// # What it checks
//
//  1. Parse. The first table after the heading "## Ahead of matter.js —
//     register" whose header names the columns "Entry", "marker file" and
//     "marker text". A marker file cell holds one or more backticked paths
//     (several separated by <br>), a marker text cell one backticked
//     marker per path, in the same order: a Go-quoted string, or
//     `class <Name>`, which stands for that class's whole body as it reads
//     at the pin, from its declaration through the lone '}' closing it (the
//     column contract above the table in by_design.md).
//  2. Allowance. A row whose two marker cells read "not recorded" is
//     refused unless it is listed in todo-marker.txt next to this file (one
//     entry per line, '#' starts a comment; a missing file is an empty
//     list). The list is the owner's to shrink: an entry whose row has a
//     marker, or that names no row, is refused too.
//  3. At the pin. parity.SchemaJSON()'s matter.sourceCommit, as cite-check
//     reads it. A marker text absent from its file there means the row is
//     wrong: an error.
//  4. At the ref (-ref, default $MATTERJS_REF, else HEAD). A marker text
//     absent there, or a marker file gone, is the finding "caught up?"
//     naming the row, the file and the ref.
//
// All git access is `git rev-parse` and `git show <rev>:<path>` on the
// matter.js clone given by -matterjs (default $MATTERJS_DIR, else
// ../matter.js); the working tree is never read. On a blobless clone `git
// show` fetches exactly the blob it names, which is what the weekly job in
// .github/workflows/matterjs-pin.yml relies on.
//
// # Exit status
//
// 0 when every row holds; 1 on any finding, one line each on stdout; 2 when
// the check could not run (no clone, the pin or the ref unresolvable, the
// register unreadable). A check that did not run is not green.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/SukramJ/go-fabric/parity"
)

// registerPath and allowancePath are relative to the module root.
const (
	registerPath  = "notes/parity/by_design.md"
	allowancePath = "script/aheadcheck/todo-marker.txt"
)

func main() {
	var (
		root   = flag.String("root", ".", "module root")
		matter = flag.String("matterjs", "", "matter.js clone (default $MATTERJS_DIR, else ../matter.js)")
		ref    = flag.String("ref", "", "matter.js revision to check against (default $MATTERJS_REF, else HEAD)")
	)
	flag.Parse()
	dir := firstNonEmpty(*matter, os.Getenv("MATTERJS_DIR"), filepath.Join(*root, "..", "matter.js"))
	rev := firstNonEmpty(*ref, os.Getenv("MATTERJS_REF"), "HEAD")
	pin, err := readPin(parity.SchemaJSON())
	if err != nil {
		fmt.Fprintln(os.Stderr, "aheadcheck:", err)
		os.Exit(2)
	}
	os.Exit(run(os.Stdout, os.Stderr, *root, dir, pin, rev))
}

func firstNonEmpty(s ...string) string {
	for _, v := range s {
		if v != "" {
			return v
		}
	}
	return ""
}

// readPin returns matter.sourceCommit of the schema extract.
func readPin(schema []byte) (string, error) {
	var doc struct {
		Matter struct {
			SourceCommit string `json:"sourceCommit"`
		} `json:"matter"`
	}
	if err := json.Unmarshal(schema, &doc); err != nil {
		return "", fmt.Errorf("parity/schema.json: %w", err)
	}
	if doc.Matter.SourceCommit == "" {
		return "", errors.New("parity/schema.json carries no matter.sourceCommit")
	}
	return doc.Matter.SourceCommit, nil
}

// run checks the register under root against the clone at pin and ref and
// returns the exit status. Findings go to out, the reason a check could not
// run to errOut.
func run(out, errOut io.Writer, root, matterDir, pin, ref string) int {
	reg, err := os.ReadFile(filepath.Join(root, registerPath)) //nolint:gosec // G304: fixed path under the module root
	if err != nil {
		_, _ = fmt.Fprintln(errOut, "aheadcheck:", err)
		return 2
	}
	allow, err := readAllowance(filepath.Join(root, allowancePath))
	if err != nil {
		_, _ = fmt.Fprintln(errOut, "aheadcheck:", err)
		return 2
	}
	rows, err := parseRegister(string(reg))
	if err != nil {
		_, _ = fmt.Fprintln(errOut, "aheadcheck:", registerPath+":", err)
		return 2
	}
	repo := gitRepo{dir: matterDir}
	pinSha, err := repo.commit(pin)
	if err != nil {
		_, _ = fmt.Fprintf(errOut, "aheadcheck: the pin %s in %s: %v\n", pin, matterDir, err)
		return 2
	}
	refSha, err := repo.commit(ref)
	if err != nil {
		_, _ = fmt.Fprintf(errOut, "aheadcheck: the ref %s in %s: %v\n", ref, matterDir, err)
		return 2
	}
	findings := check(rows, allow, repo.show, revision{name: "the pin", sha: pinSha}, revision{name: ref, sha: refSha})
	for _, f := range findings {
		_, _ = fmt.Fprintln(out, f)
	}
	if len(findings) > 0 {
		return 1
	}
	marked := 0
	for _, r := range rows {
		if !r.noMarker {
			marked++
		}
	}
	_, _ = fmt.Fprintf(out, "aheadcheck: %d rows, %d with a marker, %d in todo-marker.txt; every marker holds at the pin %s and at %s (%s)\n",
		len(rows), marked, len(rows)-marked, short(pinSha), ref, short(refSha))
	return 0
}

// readAllowance reads the TODO-marker allowance: one row name per line,
// blank lines and '#' comments ignored. A missing file is an empty list.
func readAllowance(path string) (map[string]bool, error) {
	data, err := os.ReadFile(path) //nolint:gosec // G304: fixed path under the module root
	if errors.Is(err, os.ErrNotExist) {
		return map[string]bool{}, nil
	}
	if err != nil {
		return nil, err
	}
	allow := map[string]bool{}
	for line := range strings.SplitSeq(string(data), "\n") {
		if i := strings.IndexByte(line, '#'); i >= 0 {
			line = line[:i]
		}
		if line = strings.TrimSpace(line); line != "" {
			allow[line] = true
		}
	}
	return allow, nil
}

// errNoFile marks a path that does not exist at a revision.
var errNoFile = errors.New("no such file at the revision")

// gitRepo reads a matter.js clone through git plumbing only.
type gitRepo struct{ dir string }

func (r gitRepo) git(args ...string) ([]byte, error) {
	cmd := exec.CommandContext(context.Background(), "git", append([]string{"-C", r.dir}, args...)...) //nolint:gosec // G204: git plumbing with the tool's own arguments
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}

// commit resolves rev to a full commit id, or fails.
func (r gitRepo) commit(rev string) (string, error) {
	out, err := r.git("rev-parse", "--verify", "--quiet", rev+"^{commit}")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// show returns a file's content at a commit; errNoFile when the path does
// not exist there.
func (r gitRepo) show(sha, path string) (string, error) {
	if _, err := r.git("cat-file", "-e", sha+":"+path); err != nil {
		return "", fmt.Errorf("%w: %s", errNoFile, path)
	}
	out, err := r.git("show", sha+":"+path)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func short(sha string) string {
	if len(sha) > 8 {
		return sha[:8]
	}
	return sha
}

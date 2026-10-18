// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"path"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
)

// driftWindow is how far, in lines either side of the cited range, a name
// the citing sentence gives may stand before the citation counts as
// drifted (concept v4 §A.2 step 3).
const driftWindow = 15

// treeRoots are the matter.js source roots a citation may name: the
// packages, and the support (codegen, chip-testing) and examples trees
// the Go code cites as well. The rest of the clone (docs, CI) is cited by
// nothing and only widens the basename space.
var treeRoots = []string{"examples", "packages", "support"}

// Resolution rules (concept v4 §A.2 step 2).
const (
	ruleExact    = "exact"    // a path from a tree root (packages/…), as written
	ruleSuffix   = "suffix"   // a partial path, a unique suffix of the tree
	ruleBasename = "basename" // a bare file name, a unique basename
)

// tree is the matter.js tree at one commit, read through git plumbing.
type tree interface {
	// files lists the paths under the tree roots at the commit.
	files() []string
	// content returns a file's lines at the commit.
	content(path string) ([]string, error)
}

// gitRepo reads a matter.js clone through git plumbing only: rev-parse,
// ls-tree and cat-file. It never looks at the working tree.
type gitRepo struct {
	dir   string
	trees map[string]*gitTree
	batch *catFile
}

func (r *gitRepo) git(args ...string) ([]byte, error) {
	cmd := exec.CommandContext(context.Background(), "git", append([]string{"-C", r.dir}, args...)...) //nolint:gosec // G204: git plumbing with the tool's own arguments
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}

// commit resolves sha to a full commit id, or fails.
func (r *gitRepo) commit(sha string) (string, error) {
	out, err := r.git("rev-parse", "--verify", "--quiet", sha+"^{commit}")
	if err != nil {
		return "", fmt.Errorf("commit %s not in %s", sha, r.dir)
	}
	return strings.TrimSpace(string(out)), nil
}

// at returns the tree at sha (cached).
func (r *gitRepo) at(sha string) (*gitTree, error) {
	full, err := r.commit(sha)
	if err != nil {
		return nil, err
	}
	if t, ok := r.trees[full]; ok {
		return t, nil
	}
	out, err := r.git(append([]string{"ls-tree", "-r", "--name-only", full, "--"}, treeRoots...)...)
	if err != nil {
		return nil, err
	}
	t := &gitTree{repo: r, sha: full, cache: map[string][]string{}}
	for _, p := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if p != "" {
			t.paths = append(t.paths, p)
		}
	}
	if r.trees == nil {
		r.trees = map[string]*gitTree{}
	}
	r.trees[full] = t
	return t, nil
}

func (r *gitRepo) close() {
	if r.batch != nil {
		r.batch.close()
	}
}

type gitTree struct {
	repo  *gitRepo
	sha   string
	paths []string
	cache map[string][]string
}

func (t *gitTree) files() []string { return t.paths }

func (t *gitTree) content(p string) ([]string, error) {
	if lines, ok := t.cache[p]; ok {
		return lines, nil
	}
	if t.repo.batch == nil {
		b, err := newCatFile(t.repo.dir)
		if err != nil {
			return nil, err
		}
		t.repo.batch = b
	}
	data, err := t.repo.batch.show(t.sha + ":" + p)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(string(data), "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1] // a final newline ends the last line, it opens none
	}
	t.cache[p] = lines
	return lines, nil
}

// catFile is one `git cat-file --batch` process: `git show <sha>:<path>`
// for every cited file without a process each.
type catFile struct {
	cmd *exec.Cmd
	in  io.WriteCloser
	out *bufio.Reader
}

func newCatFile(dir string) (*catFile, error) {
	cmd := exec.CommandContext(context.Background(), "git", "-C", dir, "cat-file", "--batch") //nolint:gosec // G204: the clone the caller names
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &catFile{cmd: cmd, in: in, out: bufio.NewReader(out)}, nil
}

func (c *catFile) show(obj string) ([]byte, error) {
	if _, err := fmt.Fprintln(c.in, obj); err != nil {
		return nil, err
	}
	header, err := c.out.ReadString('\n')
	if err != nil {
		return nil, err
	}
	fields := strings.Fields(header)
	if len(fields) == 2 && fields[1] == "missing" {
		return nil, fmt.Errorf("%s: missing", obj)
	}
	if len(fields) != 3 || fields[1] != "blob" {
		return nil, fmt.Errorf("%s: unexpected cat-file header %q", obj, strings.TrimSpace(header))
	}
	size, err := strconv.Atoi(fields[2])
	if err != nil {
		return nil, err
	}
	data := make([]byte, size+1) // the object and its trailing newline
	if _, err := io.ReadFull(c.out, data); err != nil {
		return nil, err
	}
	return data[:size], nil
}

func (c *catFile) close() {
	_ = c.in.Close()
	_ = c.cmd.Wait()
}

// normalise strips what stands before a matter.js path: anything up to
// the first "packages/", or a leading "./", "../" and "matter.js/".
func normalise(p string) string {
	if i := strings.Index(p, "packages/"); i >= 0 && (i == 0 || p[i-1] == '/') {
		return p[i:]
	}
	p = strings.TrimPrefix(p, "matter.js/")
	for {
		switch {
		case strings.HasPrefix(p, "./"):
			p = p[2:]
		case strings.HasPrefix(p, "../"):
			p = p[3:]
		case strings.HasPrefix(p, "matter.js/"):
			p = p[len("matter.js/"):]
		default:
			return p
		}
	}
}

// fromRoot reports whether p starts with a tree root ("packages/…").
func fromRoot(p string) bool {
	for _, r := range treeRoots {
		if strings.HasPrefix(p, r+"/") {
			return true
		}
	}
	return false
}

// resolve finds the cited path in the tree: exactly for a path from a
// tree root, as a unique suffix (on a path-element boundary) for a partial
// path, as a unique basename for a bare file name. It returns the rule
// that resolved it, or the candidates when there are none or several.
func resolve(t tree, cited string) (resolved, rule string, candidates []string) {
	p := normalise(cited)
	switch {
	case fromRoot(p):
		for _, f := range t.files() {
			if f == p {
				return f, ruleExact, nil
			}
		}
		return "", ruleExact, nil
	case strings.Contains(p, "/"):
		for _, f := range t.files() {
			if strings.HasSuffix(f, "/"+p) {
				candidates = append(candidates, f)
			}
		}
		rule = ruleSuffix
	default:
		for _, f := range t.files() {
			if path.Base(f) == p {
				candidates = append(candidates, f)
			}
		}
		rule = ruleBasename
	}
	if len(candidates) == 1 {
		return candidates[0], rule, nil
	}
	sort.Strings(candidates)
	return "", rule, candidates
}

// occurrences returns the 1-based lines of lines that name ident as a
// whole word ("#name" with its '#'). loose also matches a name that starts
// upper-case as the tail of a longer one — `ReceiveAccept` in
// `BdxReceiveAccept`, as the Go side drops matter.js's type prefixes: the
// drift test reads loosely (a name near the cited line in any form keeps
// the citation), a correction strictly (it moves only to the name itself).
func occurrences(lines []string, ident string, loose bool) []int {
	re := identRE(ident, loose)
	var out []int
	for i, l := range lines {
		if re.MatchString(l) {
			out = append(out, i+1)
		}
	}
	return out
}

var identREs = map[string]*regexp.Regexp{}

func identRE(ident string, loose bool) *regexp.Regexp {
	key := fmt.Sprint(loose, " ", ident)
	if re, ok := identREs[key]; ok {
		return re
	}
	head := `(^|[^A-Za-z0-9_$#])`
	if loose && ident[0] >= 'A' && ident[0] <= 'Z' {
		head = `(^|[^_$#])`
	}
	re := regexp.MustCompile(head + regexp.QuoteMeta(ident) + `($|[^A-Za-z0-9_$])`)
	identREs[key] = re
	return re
}

// Hard error kinds.
const (
	errNotFound  = "file not found"
	errAmbiguous = "ambiguous"
	errLine      = "line beyond end of file"
)

// result is the outcome of checking one citation.
type result struct {
	cite     citation
	rule     string
	resolved string
	// hard is the hard error kind; empty when the citation resolved and
	// its lines exist.
	hard       string
	candidates []string
	length     int
	// checked is false when the sentence names no identifier that occurs
	// in the cited file: only the hard checks applied.
	checked bool
	drift   *driftEntry
	// ident and occ are the name the correction would follow and its
	// lines in the cited file (for hard errors and drift); attached is
	// whether the sentence ties that name to the citation.
	ident    string
	occ      []int
	attached bool
}

// check checks one citation against t (concept v4 §A.2 step 3).
func check(t tree, c citation) result {
	r := result{cite: c}
	resolved, rule, cands := resolve(t, c.Path)
	r.rule = rule
	if resolved == "" {
		r.candidates = cands
		if len(cands) > 1 {
			r.hard = errAmbiguous
		} else {
			r.hard = errNotFound
		}
		return r
	}
	r.resolved = resolved
	lines, err := t.content(resolved)
	if err != nil {
		r.hard = errNotFound
		return r
	}
	r.length = len(lines)
	stem := strings.TrimSuffix(path.Base(resolved), ".ts")

	// The names of the citation the cited file contains, nearest first.
	var found []string
	occ := map[string][]int{}
	for _, id := range c.Idents {
		if id == stem {
			continue // the file's own class, declared once at its head
		}
		if o := occurrences(lines, id, true); len(o) > 0 {
			found = append(found, id)
			occ[id] = o
		}
	}
	if len(found) > 0 {
		r.ident, r.occ = found[0], occurrences(lines, found[0], false)
		r.attached = slices.Contains(c.Attached, found[0])
	}
	if c.Lo < 1 || c.Hi < c.Lo || c.Hi > len(lines) {
		r.hard = errLine
		return r
	}
	if len(found) == 0 {
		return r
	}
	r.checked = true
	for _, id := range found {
		for _, l := range occ[id] {
			if l >= c.Lo-driftWindow && l <= c.Hi+driftWindow {
				return r
			}
		}
	}
	r.drift = &driftEntry{
		File:        c.File,
		Line:        c.Line,
		Citation:    c.Text,
		Resolved:    resolved,
		At:          c.Sha,
		Identifier:  r.ident,
		Nearest:     nearest(occ[r.ident], c.Lo),
		Occurrences: len(r.occ),
	}
	return r
}

func nearest(lines []int, to int) int {
	best := 0
	for _, l := range lines {
		if best == 0 || abs(l-to) < abs(best-to) {
			best = l
		}
	}
	return best
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

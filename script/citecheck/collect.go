// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
)

// A citation is one `<path>.ts:<line>[-<line>]` found in a Go file, with
// the names of its sentence that belong to it.
type citation struct {
	// File is the Go file, slash-separated and relative to the module
	// root; Line the Go line on which the citation's line number stands
	// (the file name stands there too: a path breaks across comment lines
	// after a '/' only).
	File string
	Line int
	// Text is the citation as written ("DescriptorServer.ts:227-254").
	Text string
	// Path is the cited path as written, without the line part.
	Path string
	// Lo and Hi are the cited lines (Hi == Lo for a single line).
	Lo, Hi int
	// Sha is the commit the citation is checked at, set by run; empty for
	// the pin. Shas are every `at <sha>` of the sentence, nearest to the
	// citation first: run keeps the first that is a matter.js commit,
	// since a sentence may name a chip commit too.
	Sha  string
	Shas []string
	// Idents are the names of the sentence nearer to this citation than
	// to any other reference of the sentence, nearest first; the stems of
	// the files the sentence cites are not among them. Attached are those
	// of them the sentence ties to the citation by its parentheses: in the
	// citation's own group, just before it ("Foo.bar (Foo.ts:<n>)"), or
	// just after it ("Foo.ts:<n> (`bar`)").
	Idents   []string
	Attached []string
}

// citeRE matches a cited TypeScript path and its line or line range.
var citeRE = regexp.MustCompile(`((?:[A-Za-z0-9_.@-]+/)*[A-Za-z0-9_.@-]*[A-Za-z0-9_@-]\.ts):(\d+)(?:[-–](\d+))?`)

// The other references a sentence may carry: a bare line part (":108-112",
// whose file the reader takes from the context, so it is not checked) and
// a citation of a file in another language ("foo.go:12"). Neither is
// checked, but each takes the names nearest to it, so a name given for
// one of them is not read as the matter.js citation's.
var (
	bareLineRE  = regexp.MustCompile(`(?:^|[\s(,;])(:\d+(?:[-–]\d+)?)\b`)
	otherCiteRE = regexp.MustCompile(`[A-Za-z0-9_-]\.(?:go|cpp|h|py|yaml|json|md|js|mts):\d+(?:[-–]\d+)?`)
)

// shaRE matches an `at <sha>` or an `at` followed by a backticked sha.
var shaRE = regexp.MustCompile("\\bat\\s+`?([0-9a-f]{7,40})`?(?:[^0-9A-Za-z]|$)")

// Identifier forms the drift check reads in a sentence: a `#name`, a
// backticked name, and a CamelCase word (upper with two humps or more, or
// lower with one or more).
var (
	hashIdentRE  = regexp.MustCompile(`#[A-Za-z_$][A-Za-z0-9_$]*`)
	tickRE       = regexp.MustCompile("`([^`\n]+)`")
	tickIdentRE  = regexp.MustCompile(`^#?[A-Za-z_$][A-Za-z0-9_$]*(?:\.#?[A-Za-z_$][A-Za-z0-9_$]*)*(?:\(\))?$`)
	camelIdentRE = regexp.MustCompile(`\b(?:[A-Z][a-z0-9]+(?:[A-Z][A-Za-z0-9]*)+|[a-z][a-z0-9]*[A-Z][A-Za-z0-9]*)\b`)
)

// segment is a run of text a sentence cannot cross: one paragraph of
// consecutive full-line comments, or one line of anything else. lines[i]
// is the Go line of text byte i.
type segment struct {
	text  string
	lines []int
	code  bool // a line of code: a citation's sentence is its string literal
}

func (s *segment) add(text string, line int, sep string) {
	s.text += sep + text
	for range len(sep) + len(text) { // bytes, not runes
		s.lines = append(s.lines, line)
	}
}

// lineStartRE is a comment line opening with a citation's line part.
var lineStartRE = regexp.MustCompile(`^:\d`)

// segments cuts a Go file into [segment]s. A full-line `//` comment
// continues the paragraph of the comment line before it; an empty comment
// line or any other line ends it. Every other line is a segment of its
// own, so string literals and trailing comments are read too. A path the
// comment broke after a '/', or between ".ts" and its ":<line>", is
// rejoined without a space.
func segments(src string) []segment {
	var out []segment
	var cur *segment
	flush := func() {
		if cur != nil && strings.TrimSpace(cur.text) != "" {
			out = append(out, *cur)
		}
		cur = nil
	}
	for i, raw := range strings.Split(src, "\n") {
		line := i + 1
		trimmed := strings.TrimSpace(raw)
		if !strings.HasPrefix(trimmed, "//") {
			flush()
			out = append(out, segment{code: true})
			out[len(out)-1].add(raw, line, "")
			continue
		}
		body := strings.TrimSpace(strings.TrimPrefix(trimmed, "//"))
		if body == "" {
			flush()
			continue
		}
		if cur == nil {
			cur = &segment{}
			cur.add(body, line, "")
			continue
		}
		sep := " "
		if strings.HasSuffix(cur.text, "/") || strings.HasSuffix(cur.text, ".ts") && lineStartRE.MatchString(body) {
			sep = ""
		}
		cur.add(body, line, sep)
	}
	flush()
	return out
}

// sentenceBounds returns the sentence of text around [lo, hi): it ends at
// a '.', '!' or '?' followed by white space (not after "e.g", "i.e", "cf",
// "vs", "etc") or at the end of the segment.
func sentenceBounds(text string, lo, hi int) (start, end int) {
	isEnd := func(i int) bool { // text[i] is a terminator followed by space
		if i+1 >= len(text) || !strings.ContainsRune(".!?", rune(text[i])) {
			return false
		}
		if text[i+1] != ' ' && text[i+1] != '\t' {
			return false
		}
		head := text[:i]
		for _, abbr := range []string{"e.g", "i.e", "cf", "vs", "etc"} {
			if strings.HasSuffix(head, abbr) && (len(head) == len(abbr) || !isWordByte(head[len(head)-len(abbr)-1])) {
				return false
			}
		}
		return true
	}
	start = 0
	for i := lo - 1; i >= 0; i-- {
		if isEnd(i) {
			start = i + 1
			break
		}
	}
	end = len(text)
	for i := hi; i < len(text); i++ {
		if isEnd(i) {
			end = i + 1
			break
		}
	}
	return start, end
}

func isWordByte(b byte) bool {
	return b == '_' || b == '$' || b >= '0' && b <= '9' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z'
}

// nameHit is a name the sentence gives, at [pos, end).
type nameHit struct {
	name     string
	pos, end int
}

// names returns the names text gives — `#name`s, backticked names (a
// dotted one by its last segment), CamelCase words — leaving out the
// spans in blank (the citations, so a file name is not read as a name).
func names(text string, blank [][2]int) []nameHit {
	b := []byte(text)
	for _, sp := range blank {
		for i := sp[0]; i < sp[1]; i++ {
			b[i] = ' '
		}
	}
	text = string(b)
	var hits []nameHit
	for _, m := range hashIdentRE.FindAllStringIndex(text, -1) {
		hits = append(hits, nameHit{text[m[0]:m[1]], m[0], m[1]})
	}
	for _, m := range tickRE.FindAllStringSubmatchIndex(text, -1) {
		inner := strings.TrimSpace(text[m[2]:m[3]])
		if !tickIdentRE.MatchString(inner) {
			continue
		}
		inner = strings.TrimSuffix(inner, "()")
		if dot := strings.LastIndexByte(inner, '.'); dot >= 0 {
			inner = inner[dot+1:]
		}
		if len(strings.TrimPrefix(inner, "#")) >= 3 {
			hits = append(hits, nameHit{inner, m[2], m[3]})
		}
	}
	for _, m := range camelIdentRE.FindAllStringIndex(text, -1) {
		if m[0] > 0 && text[m[0]-1] == '#' {
			continue // read as a #name above
		}
		hits = append(hits, nameHit{text[m[0]:m[1]], m[0], m[1]})
	}
	return hits
}

// gap is the distance between two spans; 0 when they overlap.
func gap(a0, a1, b0, b1 int) int {
	switch {
	case a1 <= b0:
		return b0 - a1
	case b1 <= a0:
		return a0 - b1
	}
	return 0
}

// span is one reference of a sentence before its names are assigned; a
// sink (a bare line part, another language's citation) is not emitted.
type span struct {
	start, end int // in the sentence
	sink       bool
	c          citation
}

// collectFile returns the citations in one Go file's source.
func collectFile(file, src string) []citation {
	var out []citation
	for _, seg := range segments(src) {
		matches := citeRE.FindAllStringSubmatchIndex(seg.text, -1)
		for i := 0; i < len(matches); {
			s, e := sentenceBounds(seg.text, matches[i][0], matches[i][1])
			if seg.code {
				if ls, le, ok := literalBounds(seg.text, matches[i][0], matches[i][1]); ok {
					s, e = ls, le
				}
			}
			j := i
			for j < len(matches) && matches[j][0] < e {
				j++
			}
			out = append(out, collectSentence(file, seg, s, e, matches[i:j])...)
			i = j
		}
	}
	return out
}

// literalBounds returns the inside of the Go string literal of the code
// line text that holds [lo, hi), if one does: on a line of code the
// sentence of a citation in a message is that message, not the code
// around it.
func literalBounds(text string, lo, hi int) (start, end int, ok bool) {
	for i := 0; i < len(text); i++ {
		q := text[i]
		if q != '"' && q != '`' {
			continue
		}
		j := i + 1
		for j < len(text) && text[j] != q {
			if q == '"' && text[j] == '\\' {
				j++
			}
			j++
		}
		if j >= len(text) {
			return 0, 0, false
		}
		if i < lo && hi <= j {
			return i + 1, j, true
		}
		i = j
	}
	return 0, 0, false
}

// parenGroups returns, for every byte of text, the index of the '(' of the
// innermost parenthesis group around it (-1 outside any), and for each
// group the '(' of the group around it (-1 for none).
func parenGroups(text string) (inner []int, parent map[int]int) {
	inner = make([]int, len(text))
	parent = map[int]int{}
	stack := []int{}
	for i := range len(text) {
		top := -1
		if len(stack) > 0 {
			top = stack[len(stack)-1]
		}
		switch text[i] {
		case '(':
			inner[i] = top
			parent[i] = top
			stack = append(stack, i)
		case ')':
			inner[i] = top
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		default:
			inner[i] = top
		}
	}
	return inner, parent
}

// attached reports whether the name at [n0, n1) is tied to the citation
// at [c0, c1) by the parentheses of text (see [citation.Attached]).
func attached(text string, inner []int, parent map[int]int, n0, n1, c0, c1 int) bool {
	gc, gn := inner[c0], inner[n0]
	switch {
	case gc >= 0 && gc == gn:
		return true
	case gc >= 0 && n1 <= gc && gn == parent[gc] && strings.Trim(text[n1:gc], " `") == "":
		return true
	case gn >= 0 && c1 <= gn && gc == parent[gn] && strings.Trim(text[c1:gn], " `:,") == "":
		return true
	}
	return false
}

// collectSentence builds the citations of the sentence text[s:e], whose
// citations are matches: each with the names nearer to it than to any
// other reference of the sentence.
func collectSentence(file string, seg segment, s, e int, matches [][]int) []citation {
	sentence := seg.text[s:e]
	spans := make([]span, 0, len(matches))
	stems := map[string]bool{}
	for _, m := range matches {
		lo, _ := strconv.Atoi(seg.text[m[4]:m[5]])
		hi := lo
		if m[6] >= 0 {
			hi, _ = strconv.Atoi(seg.text[m[6]:m[7]])
		}
		p := seg.text[m[2]:m[3]]
		stems[strings.TrimSuffix(path.Base(p), ".ts")] = true
		spans = append(spans, span{start: m[0] - s, end: m[1] - s, c: citation{
			File: file, Line: seg.lines[m[1]-1], Text: seg.text[m[0]:m[1]], Path: p, Lo: lo, Hi: hi,
		}})
	}
	for _, m := range bareLineRE.FindAllStringSubmatchIndex(sentence, -1) {
		spans = append(spans, span{start: m[2], end: m[3], sink: true})
	}
	for _, m := range otherCiteRE.FindAllStringIndex(sentence, -1) {
		spans = append(spans, span{start: m[0], end: m[1], sink: true})
	}
	blank := make([][2]int, 0, len(spans))
	for i := range spans {
		blank = append(blank, [2]int{spans[i].start, spans[i].end})
	}
	type owned struct {
		name     string
		dist     int
		attached bool
	}
	inner, parent := parenGroups(sentence)
	own := make([][]owned, len(spans))
	for _, h := range names(sentence, blank) {
		if stems[h.name] {
			continue // a file the sentence cites, named as such
		}
		best, bestGap := -1, 0
		for k := range spans {
			if g := gap(h.pos, h.end, spans[k].start, spans[k].end); best < 0 || g < bestGap {
				best, bestGap = k, g
			}
		}
		sp := &spans[best]
		own[best] = append(own[best], owned{h.name, bestGap, attached(sentence, inner, parent, h.pos, h.end, sp.start, sp.end)})
	}
	shas := shaRE.FindAllStringSubmatchIndex(sentence, -1)
	out := make([]citation, 0, len(spans))
	for k := range spans {
		sp := &spans[k]
		if sp.sink {
			continue
		}
		c := sp.c
		sort.SliceStable(own[k], func(a, b int) bool { return own[k][a].dist < own[k][b].dist })
		seen := map[string]bool{}
		for _, o := range own[k] {
			if !seen[o.name] {
				seen[o.name] = true
				c.Idents = append(c.Idents, o.name)
			}
			if o.attached && !slices.Contains(c.Attached, o.name) {
				c.Attached = append(c.Attached, o.name)
			}
		}
		ordered := append([][]int(nil), shas...)
		sort.SliceStable(ordered, func(a, b int) bool {
			return gap(ordered[a][2], ordered[a][3], sp.start, sp.end) < gap(ordered[b][2], ordered[b][3], sp.start, sp.end)
		})
		for _, sm := range ordered {
			c.Shas = append(c.Shas, sentence[sm[2]:sm[3]])
		}
		out = append(out, c)
	}
	sort.SliceStable(out, func(a, b int) bool { return out[a].Line < out[b].Line })
	return out
}

// collect walks root for *.go files — skipping every directory whose name
// starts with "." (.git, .claude and its worktrees) and vendor — and
// returns their citations in file order.
func collect(root string) ([]citation, error) {
	var out []citation
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			if p != root && (strings.HasPrefix(name, ".") || name == "vendor") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") {
			return nil
		}
		src, err := os.ReadFile(p) //nolint:gosec // G304: a file of the walked module
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		out = append(out, collectFile(filepath.ToSlash(rel), string(src))...)
		return nil
	})
	return out, err
}

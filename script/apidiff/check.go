// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"errors"
	"fmt"
	"path"
	"regexp"
	"strings"
	"unicode"
)

// change is one incompatible change apidiff reports.
type change struct {
	// Line is apidiff's line, without its "- ".
	Line string
	// Package is the module-relative package path ("cluster/spec", "."
	// for the module's root package).
	Package string
	// Symbol is the name the CHANGELOG must carry: the symbol qualified by
	// its package's name ("spec.Bool"), "Report.Values" for a method, the
	// repository-relative path for a removed package.
	Symbol string
	// Bare is a package-level symbol without its package ("Bool"); empty
	// for a method and a removed package.
	Bare string
	// Path is the repository-relative path of the package of a
	// package-level symbol ("cluster/spec", "wifi/wpasupplicant"), which
	// a CHANGELOG line names in backticks.
	Path string
}

// excluded are the module-relative trees that are not public API.
var excluded = []string{"examples", "script", "conformance", "internal"}

// parse reads apidiff's module-mode report of the module t: the
// "- ./pkg.Symbol: message" lines, the ones of an excluded tree left out.
func parse(out string, t target) []change {
	var changes []change
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "- ") {
			continue
		}
		line = strings.TrimPrefix(line, "- ")
		subject, _, _ := strings.Cut(line, ": ")
		c := split(subject, t.Path)
		c.Line = line
		if isExcluded(c.Package) || (c.Symbol != c.Package && unexported(c.Symbol)) {
			continue
		}
		changes = append(changes, qualify(c, t))
	}
	return changes
}

// split splits "./cluster/spec.(*Report).Values" into the package
// ("cluster/spec") and the symbol ("Report.Values"). apidiff writes a
// symbol of the module's root package without a prefix ("Driver",
// "(*Driver).Close"), one of a child package with "./<path>.", and one
// outside the module with its full path (golang.org/x/exp/apidiff,
// objectString).
func split(subject, modulePath string) change {
	// A removed package is reported as "package <import path>: removed";
	// the CHANGELOG names it by its module-relative path.
	if rest, ok := strings.CutPrefix(subject, "package "); ok {
		rel := strings.TrimPrefix(strings.TrimPrefix(rest, modulePath), "/")
		if rel == "" {
			rel = "."
		}
		return change{Package: rel, Symbol: rel}
	}
	rel, child := strings.CutPrefix(subject, "./")
	if !child {
		rel, child = strings.CutPrefix(subject, modulePath+"/")
	}
	if !child { // the module's root package
		return change{Package: ".", Symbol: tidy(subject)}
	}
	slash := strings.LastIndex(rel, "/")
	dot := strings.Index(rel[slash+1:], ".")
	if dot < 0 {
		return change{Package: rel, Symbol: rel}
	}
	dot += slash + 1
	return change{Package: rel[:dot], Symbol: tidy(rel[dot+1:])}
}

// qualify writes the name the CHANGELOG must carry for a change of the
// module t: a package-level symbol qualified by its package's name, the
// last element of its path (the root package's name for "."), so that a
// line naming node.Options does not announce a break of onoff.Options
// (R2-P6-sup-2802); a method as Type.Method, which names its type
// already; a removed package by its repository-relative path.
func qualify(c change, t target) change {
	repoPath := c.Package
	if t.Dir != "." {
		repoPath = t.Dir + "/" + c.Package
		if c.Package == "." {
			repoPath = t.Dir
		}
	}
	switch {
	case c.Symbol == c.Package: // a removed package
		c.Symbol = repoPath
	case strings.Contains(c.Symbol, "."): // Type.Method
	default:
		name := path.Base(c.Package)
		if c.Package == "." {
			name = t.Name
		}
		c.Bare, c.Path = c.Symbol, repoPath
		c.Symbol = name + "." + c.Symbol
	}
	return c
}

// tidy writes a method as Type.Method.
func tidy(symbol string) string {
	return strings.NewReplacer("(*", "", "(", "", ")", "").Replace(symbol)
}

// unexported reports whether the symbol's first name is unexported: a
// method of an unexported type, reached only as a promoted method of an
// exported one, whose own change apidiff reports.
func unexported(symbol string) bool {
	r := []rune(symbol)
	return len(r) > 0 && unicode.IsLower(r[0])
}

func isExcluded(pkg string) bool {
	for _, e := range excluded {
		if pkg == e || strings.HasPrefix(pkg, e+"/") {
			return true
		}
	}
	return false
}

// unreleased returns the text of the Changed, Removed and Deprecated
// sections of every release newer than base in a Keep-a-Changelog file:
// [Unreleased] and the sections above base's own. A release pull request
// has already renamed [Unreleased] to the version it cuts while the tag
// is still the previous one, and its lines announce the break just the
// same; base is the tag's name (v0.10.3, ble/esphome/v0.10.3), the
// heading its version. A changelog without [Unreleased] or without base's
// heading is an error: reading on to the end of the file would count every
// older release's lines as an announcement (R2-P6-sup-2801).
func unreleased(changelog, base string) (string, error) {
	_, rest, ok := strings.Cut(changelog, "## [Unreleased]")
	if !ok {
		return "", errors.New("CHANGELOG.md has no ## [Unreleased] heading")
	}
	version := strings.TrimPrefix(path.Base(base), "v")
	i := strings.Index(rest, "\n## ["+version+"]")
	if i < 0 {
		return "", fmt.Errorf("CHANGELOG.md has no ## [%s] heading below [Unreleased] for the base %s", version, base)
	}
	rest = rest[:i]
	var b strings.Builder
	keep := false
	for _, line := range strings.Split(rest, "\n") {
		if strings.HasPrefix(line, "## ") {
			keep = false // a newer release's heading: its sections decide again
			continue
		}
		if strings.HasPrefix(line, "### ") {
			h := strings.TrimSpace(strings.TrimPrefix(line, "### "))
			keep = h == "Changed" || h == "Removed" || h == "Deprecated"
			continue
		}
		if keep {
			b.WriteString(line)
			b.WriteByte('\n')
		}
	}
	return b.String(), nil
}

// unannounced returns the changes the text does not announce. A change
// is announced by a line that names its Symbol as a word of its own
// ("spec.Bool", "cluster/spec.Bool", "Report.Values"), or, for a
// package-level symbol, by a line that names its package in backticks, the
// CHANGELOG's form ("- `cluster/lock`: `StateSource` is …"), and the bare
// symbol as a word.
func unannounced(changes []change, text string) []change {
	lines := strings.Split(text, "\n")
	var missing []change
	for _, c := range changes {
		named := word(c.Symbol)
		bare := word(c.Bare)
		found := false
		for _, l := range lines {
			if named.MatchString(l) ||
				(c.Bare != "" && strings.Contains(l, "`"+c.Path+"`") && bare.MatchString(l)) {
				found = true
				break
			}
		}
		if !found {
			missing = append(missing, c)
		}
	}
	return missing
}

// splitMoves separates the changes that only move the module path from
// the rest. Across a move (github.com/SukramJ/go-fabric → thingfabric.dev/
// go-fabric, v0.25.0) every signature that names a type of the module
// changes its type identity; apidiff reports each as "changed from A to
// B". Such a change is a move when B is A with every oldRoot replaced by
// newRoot: no other part of the signature moved. One CHANGELOG line that
// names the new module path announces all of them; every other change is
// checked symbol by symbol as before.
func splitMoves(changes []change, oldRoot, newRoot string) (rest, moves []change) {
	for _, c := range changes {
		if moved(c.Line, oldRoot, newRoot) {
			moves = append(moves, c)
		} else {
			rest = append(rest, c)
		}
	}
	return rest, moves
}

// moved reports whether line ("subject: changed from A to B") changes
// only oldRoot to newRoot, or prints A and B alike (a type of the
// symbol's own package, unqualified). A and B are split at each " to " in turn, since
// a signature may itself contain one.
func moved(line, oldRoot, newRoot string) bool {
	if oldRoot == newRoot {
		return false
	}
	_, msg, ok := strings.Cut(line, ": changed from ")
	if !ok {
		return false
	}
	for i := 0; ; {
		j := strings.Index(msg[i:], " to ")
		if j < 0 {
			return false
		}
		from, to := msg[:i+j], msg[i+j+len(" to "):]
		// A type of the changed symbol's own package prints unqualified,
		// so its move reads "changed from *T to *T": the same text, only
		// the package path behind it differs.
		if from == to || strings.Contains(from, oldRoot) && strings.ReplaceAll(from, oldRoot, newRoot) == to {
			return true
		}
		i += j + 1
	}
}

// word matches name as a word of its own.
func word(name string) *regexp.Regexp {
	return regexp.MustCompile(`(^|[^\w])` + regexp.QuoteMeta(name) + `($|[^\w])`)
}

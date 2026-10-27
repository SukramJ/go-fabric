// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"regexp"
	"strings"
	"unicode"
)

// change is one incompatible change apidiff reports.
type change struct {
	// Line is apidiff's line, without its "- ".
	Line string
	// Package is the module-relative package path ("cluster/spec").
	Package string
	// Symbol is the name the CHANGELOG must carry: "Bool", "Report.Values"
	// for a method, the package path for a removed package.
	Symbol string
}

// excluded are the module-relative trees that are not public API.
var excluded = []string{"examples", "script", "conformance", "internal"}

// parse reads apidiff's module-mode report: the "- ./pkg.Symbol: message"
// lines, the ones of an excluded tree left out.
func parse(out string) []change {
	var changes []change
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "- ") {
			continue
		}
		line = strings.TrimPrefix(line, "- ")
		subject, _, _ := strings.Cut(line, ": ")
		c := split(subject)
		c.Line = line
		if isExcluded(c.Package) || (c.Symbol != c.Package && unexported(c.Symbol)) {
			continue
		}
		changes = append(changes, c)
	}
	return changes
}

// split splits "./cluster/spec.(*Report).Values" into the package
// ("cluster/spec") and the symbol ("Report.Values").
func split(subject string) change {
	// A removed package is reported as "package <import path>: removed";
	// the CHANGELOG names it by its module-relative path.
	if rest, ok := strings.CutPrefix(subject, "package "); ok {
		rel := strings.TrimPrefix(strings.TrimPrefix(rest, module), "/")
		if rel == "" {
			rel = "."
		}
		return change{Package: rel, Symbol: rel}
	}
	subject = strings.TrimPrefix(subject, "./")
	if subject == "." {
		return change{Package: ".", Symbol: module}
	}
	if strings.HasPrefix(subject, ".") { // the module root package
		return change{Package: ".", Symbol: tidy(strings.TrimLeft(subject, "."))}
	}
	slash := strings.LastIndex(subject, "/")
	dot := strings.Index(subject[slash+1:], ".")
	if dot < 0 {
		return change{Package: subject, Symbol: subject}
	}
	dot += slash + 1
	return change{Package: subject[:dot], Symbol: tidy(subject[dot+1:])}
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
// same; base is the tag's name (v0.10.3), the heading its version.
func unreleased(changelog, base string) string {
	_, rest, ok := strings.Cut(changelog, "## [Unreleased]")
	if !ok {
		return ""
	}
	if i := strings.Index(rest, "\n## ["+strings.TrimPrefix(base, "v")+"]"); i >= 0 {
		rest = rest[:i]
	}
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
	return b.String()
}

// unannounced returns the changes whose symbol the text does not name as
// a word of its own.
func unannounced(changes []change, text string) []change {
	var missing []change
	for _, c := range changes {
		re := regexp.MustCompile(`(^|[^\w])` + regexp.QuoteMeta(c.Symbol) + `($|[^\w])`)
		if !re.MatchString(text) {
			missing = append(missing, c)
		}
	}
	return missing
}

// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package main

import (
	"go/token"
	"strings"
	"unicode"
)

// exportName turns a matter.js name into an exported Go identifier: the
// first letter upper-cased, every character that is not a letter or a
// digit dropped (the word after it capitalised), and a leading digit
// prefixed with "V".
func exportName(name string) string {
	var b strings.Builder
	upper := true
	for _, r := range name {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			upper = true
			continue
		}
		if upper {
			r = unicode.ToUpper(r)
			upper = false
		}
		b.WriteRune(r)
	}
	out := b.String()
	if out == "" {
		return "X"
	}
	if unicode.IsDigit(rune(out[0])) {
		out = "V" + out
	}
	return out
}

// packageName is the Go package name of a cluster: its matter.js name in
// lower case, with "cluster" appended where that is a Go keyword
// (Switch is package switchcluster).
func packageName(cluster string) string {
	name := strings.ToLower(exportName(cluster))
	if token.IsKeyword(name) {
		name += "cluster"
	}
	return name
}

// valuePrefix is the prefix of an enum's or a bitmap's constants: the
// type name without its "Enum" / "Bitmap" suffix (OperationModeEnum's
// values are OperationModeNormal, …).
func valuePrefix(typeName string) string {
	for _, suffix := range []string{"Enum", "Bitmap"} {
		if p, ok := strings.CutSuffix(typeName, suffix); ok && p != "" {
			return p
		}
	}
	return typeName
}

// lowerFirst lower-cases the first letter.
func lowerFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToLower(s[:1]) + s[1:]
}

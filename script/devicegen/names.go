// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"go/token"
	"strings"
	"unicode"
)

// exportName turns a matter.js name into an exported Go identifier, as
// script/clustergen does: the first letter upper-cased, every character
// that is not a letter or a digit dropped (the word after it
// capitalised), and a leading digit prefixed with "V".
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

// packageName is the Go package of a cluster's generated definition under
// cluster/spec, as script/clustergen names it: the matter.js name in lower
// case, with "cluster" appended where that is a Go keyword.
func packageName(cluster string) string {
	name := strings.ToLower(exportName(cluster))
	if token.IsKeyword(name) {
		name += "cluster"
	}
	return name
}

// snakeName is a device type's file name stem: matter.js's decamelize
// (the generator's devices/<decamelized>.ts, EndpointFile.ts:35-49) with
// "_" for "-" — "DimmableLight" is dimmable_light, "SmokeCoAlarm"
// smoke_co_alarm.
func snakeName(name string) string {
	rs := []rune(name)
	var b strings.Builder
	for i, r := range rs {
		if unicode.IsUpper(r) && i > 0 {
			prev := rs[i-1]
			nextLower := i+1 < len(rs) && unicode.IsLower(rs[i+1])
			if unicode.IsLower(prev) || unicode.IsDigit(prev) || (unicode.IsUpper(prev) && nextLower) {
				b.WriteByte('_')
			}
		}
		b.WriteRune(unicode.ToLower(r))
	}
	return b.String()
}

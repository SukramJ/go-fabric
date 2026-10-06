// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package spec

import (
	"strings"

	"github.com/SukramJ/go-fabric/schema"
)

// ConformanceFromSchema converts a conformance expression of the schema's
// generated device-type tables (schema.Conformance, matter.js's
// Conformance.Ast kept as a plain value) into the tree this package
// evaluates. Both carry matter.js's node types verbatim, so the conversion
// is structural.
func ConformanceFromSchema(c schema.Conformance) Conformance {
	out := Conformance{Text: c.Text, Op: ConformanceOp(c.Op), Name: c.Name, Value: c.Value, Rev: c.Rev}
	if c.Choice != nil {
		out.Choice = &Choice{Name: c.Choice.Name, Num: c.Choice.Num, OrMore: c.Choice.OrMore, OrLess: c.Choice.OrLess}
	}
	if len(c.Args) > 0 {
		out.Args = make([]Conformance, len(c.Args))
		for i := range c.Args {
			out.Args[i] = ConformanceFromSchema(c.Args[i])
		}
	}
	return out
}

// Names returns the names a conformance expression references — conditions
// and features, a qualified name ("RootNode.GroupcastListenerCond") joined
// by its dots — in the order they appear, each once.
//
// Mirrors matter.js collectNames (packages/model/src/logic/device-types/
// ModelLookups.ts) over Conformance.Ast.
func (c Conformance) Names() []string {
	var out []string
	seen := map[string]bool{}
	c.collectNames(func(name string) {
		if !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
	})
	return out
}

func (c Conformance) collectNames(add func(string)) {
	switch c.Op {
	case ConfName:
		add(c.Name)
	case ConfDot:
		if segments, ok := c.dotSegments(); ok {
			add(strings.Join(segments, "."))
			return
		}
		for _, a := range c.Args {
			a.collectNames(add)
		}
	case ConfAnd, ConfOr, ConfXor, ConfEq, ConfNe, ConfGt, ConfLt, ConfGte, ConfLte,
		ConfNot, ConfOptionalIf, ConfChoice, ConfOtherwise:
		for _, a := range c.Args {
			a.collectNames(add)
		}
	default:
	}
}

// dotSegments is matter.js collectDotSegments: the names of a chain of "."
// operators over plain names, or false when an operand is anything else.
func (c Conformance) dotSegments() ([]string, bool) {
	switch c.Op {
	case ConfName:
		return []string{c.Name}, true
	case ConfDot:
		l, ok := lhsOf(c).dotSegments()
		if !ok {
			return nil, false
		}
		r, ok := rhsOf(c).dotSegments()
		if !ok {
			return nil, false
		}
		return append(l, r...), true
	default:
		return nil, false
	}
}

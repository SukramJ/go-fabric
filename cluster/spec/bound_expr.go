// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package spec

import (
	"encoding/json"
	"math"
	"sync"
)

// boundExpr is a parsed constraint expression: a number, a (qualified)
// reference to a sibling, an operator over two operands or a function over
// its arguments. Any other node — a unit value the generator left
// unscaled, an unknown type — is opaque and never resolves.
type boundExpr struct {
	op       string // "num", "ref", "+", "-", "*", "/", "^", "maxOf", "minOf"; "" is opaque
	num      float64
	name     string
	lhs, rhs *boundExpr
	args     []*boundExpr
}

// boundExprs caches the parsed tree per expression text: a generated
// definition states each once and every write evaluates it.
var boundExprs sync.Map // string → *boundExpr

// parseBoundExpr is the tree of an expression the generator emits as
// JSON; text that is no JSON is opaque.
func parseBoundExpr(text string) *boundExpr {
	if e, ok := boundExprs.Load(text); ok {
		return e.(*boundExpr) //nolint:forcetypeassert // the map holds *boundExpr only
	}
	e := parseBoundNode(json.RawMessage(text))
	boundExprs.Store(text, e)
	return e
}

func parseBoundNode(raw json.RawMessage) *boundExpr {
	var n float64
	if json.Unmarshal(raw, &n) == nil {
		return &boundExpr{op: "num", num: n}
	}
	var node struct {
		Type string            `json:"type"`
		Name string            `json:"name"`
		LHS  json.RawMessage   `json:"lhs"`
		RHS  json.RawMessage   `json:"rhs"`
		Args []json.RawMessage `json:"args"`
	}
	if json.Unmarshal(raw, &node) != nil {
		return &boundExpr{}
	}
	switch node.Type {
	case "reference":
		return &boundExpr{op: "ref", name: node.Name}
	case ".":
		// The rhs names a member of the lhs (Constraint.ts:1065-1085).
		if name := dottedReference(string(raw)); name != "" {
			return &boundExpr{op: "ref", name: name}
		}
	case "+", "-", "*", "/", "^":
		if len(node.LHS) > 0 && len(node.RHS) > 0 {
			return &boundExpr{op: node.Type, lhs: parseBoundNode(node.LHS), rhs: parseBoundNode(node.RHS)}
		}
	case "maxOf", "minOf":
		e := &boundExpr{op: node.Type}
		for _, a := range node.Args {
			e.args = append(e.args, parseBoundNode(a))
		}
		return e
	}
	return &boundExpr{}
}

// evalBound computes an expression against the sibling attributes; ok is
// false when it has no number — an operand that does not resolve, a
// function without a numeric argument, an opaque node. Mirrors matter.js
// packages/model/src/aspects/Constraint.ts:969-1096 at 862ad75f
// (evaluate): "+", "-" and "*" of two numbers, "/" of two numbers, "^" as
// -(|a|^b) for a negative base, maxOf / minOf over the numeric arguments
// only (Constraint.ts:14-36, Functions.extreme), a reference resolved by
// name (NameResolver) and anything else undefined. matter.js computes a
// power beyond 2^53 as a BigInt (Constraint.ts:1049-1058); the bounds here
// are float64 like every other bound of [Instance.satisfies], exact for the
// ±2^62 the snapshot states, while 2^62 - 1 rounds to 2^62 — a value within
// float64's spacing there (1024) of the bound compares as the bound.
func (i *Instance) evalBound(e *boundExpr, peers Peers) (float64, bool) {
	switch e.op {
	case "num":
		return e.num, true
	case "ref":
		v, ok := i.resolveName(e.name, peers)
		if !ok {
			return 0, false
		}
		return asFloat(v)
	case "+", "-", "*", "/", "^":
		l, okL := i.evalBound(e.lhs, peers)
		r, okR := i.evalBound(e.rhs, peers)
		if !okL || !okR {
			return 0, false
		}
		switch e.op {
		case "+":
			return l + r, true
		case "-":
			return l - r, true
		case "*":
			return l * r, true
		case "/":
			return l / r, true
		}
		// Constraint.ts:1038-1061: the parser encodes a unary minus in the
		// base, so -a^b is -(|a|^b).
		p := math.Pow(math.Abs(l), r)
		if l < 0 {
			p = -p
		}
		return p, true
	case "maxOf", "minOf":
		var (
			out   float64
			found bool
		)
		for _, a := range e.args {
			v, ok := i.evalBound(a, peers)
			if !ok {
				continue
			}
			if !found || (e.op == "maxOf" && v > out) || (e.op == "minOf" && v < out) {
				out, found = v, true
			}
		}
		return out, found
	}
	return 0, false
}

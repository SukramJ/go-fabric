// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package spec

import (
	"encoding/json"
	"math"
	"math/big"
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
		if b, isExpr := e.(*boundExpr); isExpr {
			return b
		}
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
// packages/model/src/aspects/Constraint.ts:969-1096 at db021da0
// (evaluate): "+", "-" and "*" of two numbers, "/" of two numbers, "^" as
// -(|a|^b) for a negative base, maxOf / minOf over the numeric arguments
// only (Constraint.ts:14-36, Functions.extreme), a reference resolved by
// name (NameResolver) and anything else undefined. matter.js computes a
// power above Number.MAX_SAFE_INTEGER (2^53 - 1) as a BigInt (Constraint.ts:1049-1058); the bounds here
// are float64, for a float, a length and a count ([Instance.satisfies]); an
// integer value is held to [Instance.evalBoundExact] instead, where
// 2^62 - 1 stays 2^62 - 1.
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

// maxExactExponent bounds the exponent evalBoundExact raises exactly; a
// larger one, a negative or a fractional one is computed as float64 (the
// snapshot states none beyond 64).
const maxExactExponent = 256

// evalBoundExact is [Instance.evalBound] computed in exact rationals, for
// an integer value ([Instance.satisfiesInt]): "+", "-", "*" and "/" exactly,
// "^" exactly for an integer base and a whole exponent up to
// maxExactExponent — matter.js computes a power above Number.MAX_SAFE_INTEGER (2^53 - 1) as a BigInt
// (Constraint.ts:1049-1058) — and maxOf / minOf over the numeric
// arguments only. What evalBound leaves without a number has none here.
func (i *Instance) evalBoundExact(e *boundExpr, peers Peers) (*big.Rat, bool) {
	switch e.op {
	case "num":
		if math.IsNaN(e.num) || math.IsInf(e.num, 0) {
			return nil, false
		}
		return new(big.Rat).SetFloat64(e.num), true
	case "ref":
		v, ok := i.resolveName(e.name, peers)
		if !ok {
			return nil, false
		}
		return ratOf(v)
	case "+", "-", "*", "/", "^":
		l, okL := i.evalBoundExact(e.lhs, peers)
		r, okR := i.evalBoundExact(e.rhs, peers)
		if !okL || !okR {
			return nil, false
		}
		if v, ok, exact := exactOp(e.op, l, r); exact {
			return v, ok
		}
		f, ok := i.evalBound(e, peers)
		if !ok || math.IsNaN(f) || math.IsInf(f, 0) {
			return nil, false
		}
		return new(big.Rat).SetFloat64(f), true
	case "maxOf", "minOf":
		var out *big.Rat
		for _, a := range e.args {
			v, ok := i.evalBoundExact(a, peers)
			if !ok {
				continue
			}
			if out == nil || (e.op == "maxOf" && v.Cmp(out) > 0) || (e.op == "minOf" && v.Cmp(out) < 0) {
				out = v
			}
		}
		return out, out != nil
	}
	return nil, false
}

// exactOp computes l op r exactly; exact is false for a power it leaves to
// float64 (a fractional base or exponent, a negative exponent, one beyond
// maxExactExponent), ok false for a division by zero.
func exactOp(op string, l, r *big.Rat) (v *big.Rat, ok, exact bool) {
	switch op {
	case "+":
		return new(big.Rat).Add(l, r), true, true
	case "-":
		return new(big.Rat).Sub(l, r), true, true
	case "*":
		return new(big.Rat).Mul(l, r), true, true
	case "/":
		if r.Sign() == 0 {
			return nil, false, true
		}
		return new(big.Rat).Quo(l, r), true, true
	}
	if !l.IsInt() || !r.IsInt() || r.Sign() < 0 || !r.Num().IsInt64() || r.Num().Int64() > maxExactExponent {
		return nil, false, false
	}
	// Constraint.ts:1038-1061: -a^b is -(|a|^b).
	p := new(big.Int).Exp(new(big.Int).Abs(l.Num()), r.Num(), nil)
	if l.Sign() < 0 {
		p.Neg(p)
	}
	return new(big.Rat).SetInt(p), true, true
}

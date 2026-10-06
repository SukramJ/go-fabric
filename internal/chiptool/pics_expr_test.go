// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package chiptool

import (
	"fmt"
	"strings"
	"testing"
)

// The PICS expression a case of the test descriptor carries ("pics"), and
// its evaluation against a PICS file. A case whose expression is false for
// the DUT is not applicable to it and is not run — what the CHIP test
// harness does when it selects cases from a PICS, and what matter.js does
// when it filters the descriptor (packages/testing/src/test-descriptor.ts
// filter → PicsExpression.evaluate). Ported from matter.js
// packages/testing/src/chip/pics/expression.ts: names, "!", "&", "|" and
// parentheses, "&" and "|" right-associative at one precedence, and a
// trailing "(" tolerated as the hint some descriptors append.

type picsAST struct {
	op          string // "name", "!", "&", "|"
	name        string
	left, right *picsAST
}

type picsToken struct{ kind, name string }

func tokenizePICS(def string) []picsToken {
	var out []picsToken
	for i := 0; i < len(def); i++ {
		ch := def[i]
		switch {
		case strings.IndexByte("&|!()", ch) >= 0:
			out = append(out, picsToken{kind: string(ch)})
		case ch == ' ' || ch == '\r' || ch == '\n' || ch == '\t':
		default:
			j := i
			for j+1 < len(def) && strings.IndexByte("&|!() \r\n\t", def[j+1]) < 0 {
				j++
			}
			out = append(out, picsToken{kind: "name", name: def[i : j+1]})
			i = j
		}
	}
	return out
}

// parsePICSExpr parses a descriptor PICS expression.
func parsePICSExpr(def string) (*picsAST, error) {
	toks := tokenizePICS(def)
	pos := 0
	peek := func() *picsToken {
		if pos < len(toks) {
			return &toks[pos]
		}
		return nil
	}
	bad := func() error { return fmt.Errorf("invalid PICS expression %q", def) }
	var parseExpr func() (*picsAST, error)
	var parsePrefix func() (*picsAST, error)
	parsePrefix = func() (*picsAST, error) {
		t := peek()
		if t == nil {
			return nil, bad()
		}
		pos++
		switch t.kind {
		case "name":
			return &picsAST{op: "name", name: t.name}, nil
		case "(":
			e, err := parseExpr()
			if err != nil {
				return nil, err
			}
			if n := peek(); n == nil || n.kind != ")" {
				return nil, bad()
			}
			pos++
			return e, nil
		case "!":
			operand, err := parsePrefix()
			if err != nil {
				return nil, err
			}
			return &picsAST{op: "!", left: operand}, nil
		}
		return nil, bad()
	}
	parseExpr = func() (*picsAST, error) {
		lhs, err := parsePrefix()
		if err != nil {
			return nil, err
		}
		if n := peek(); n != nil && (n.kind == "&" || n.kind == "|") {
			pos++
			rhs, err := parseExpr()
			if err != nil {
				return nil, err
			}
			return &picsAST{op: n.kind, left: lhs, right: rhs}, nil
		}
		return lhs, nil
	}
	ast, err := parseExpr()
	if err != nil {
		return nil, err
	}
	if n := peek(); n != nil && n.kind != "(" {
		return nil, bad()
	}
	return ast, nil
}

// evaluate reports whether the expression holds for values (a code is true
// when its value is "1").
func (a *picsAST) evaluate(values map[string]string) bool {
	switch a.op {
	case "!":
		return !a.left.evaluate(values)
	case "&":
		return a.left.evaluate(values) && a.right.evaluate(values)
	case "|":
		return a.left.evaluate(values) || a.right.evaluate(values)
	default:
		return values[a.name] == "1"
	}
}

func TestPICSExpression(t *testing.T) {
	t.Parallel()
	values := map[string]string{"A": "1", "B": "0", "C": "1"}
	for _, tc := range []struct {
		expr string
		want bool
	}{
		{"A", true},
		{"B", false},
		{"D", false},
		{"!B", true},
		{"A & B", false},
		{"A | B", true},
		{"!(A & B)", true},
		{"B | !C", false},
		{"(A | B) & C", true},
		{"A & B | C", true}, // right-associative, as matter.js parses it: A & (B | C)
		{"A (a hint", true},
	} {
		ast, err := parsePICSExpr(tc.expr)
		if err != nil {
			t.Fatalf("%q: %v", tc.expr, err)
		}
		if got := ast.evaluate(values); got != tc.want {
			t.Errorf("%q = %v, want %v", tc.expr, got, tc.want)
		}
	}
	for _, bad := range []string{"", "A &", "(A", "A )"} {
		if _, err := parsePICSExpr(bad); err == nil {
			t.Errorf("%q parsed; want an error", bad)
		}
	}
}

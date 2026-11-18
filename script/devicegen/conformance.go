// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"encoding/json"
	"errors"
	"fmt"
)

// Conformance AST node types: matter.js Conformance.Flag and
// Conformance.Special (packages/model/src/aspects/Conformance.ts:275-295).
const (
	astMandatory   = "M"
	astOptional    = "O"
	astProvisional = "P"
	astDisallowed  = "X"
	astObsolete    = "Z"
	astEmpty       = "empty"
	astName        = "name"
	astOtherwise   = "otherwise"
	astOptionalIf  = "optionalIf"
	astRevision    = "revision"
	astAnd         = "&"
	astOr          = "|"
	astNot         = "!"
	astDeprecated  = "D"
	astDesc        = "desc"
	astValue       = "value"
	astChoice      = "choice"
)

// astNodes are the conformance node types the generator reads, by the
// shape of their parameter — the set script/clustergen renders (its
// opLiteral). checkAST refuses any other node, and a parameter that does
// not decode, before the evaluators below read the tree: they would read
// one as false or as permit (R2-P6-par-2707, wizard-6b §2 gen Q1).
var astNodes = map[string]string{
	astMandatory: "flag", astOptional: "flag", astProvisional: "flag", astDeprecated: "flag",
	astDisallowed: "flag", astObsolete: "flag", astEmpty: "flag", astDesc: "flag",
	astName: "name", astValue: "value", astRevision: "revision", astChoice: "choice",
	astOtherwise: "list", astOptionalIf: "operand", astNot: "operand",
	"==": "binary", "!=": "binary", astOr: "binary", "^": "binary", astAnd: "binary",
	".": "binary", ">": "binary", "<": "binary", ">=": "binary", "<=": "binary",
}

// checkAST refuses a node outside astNodes, or one whose parameter does
// not decode as its type's, anywhere in the tree.
func checkAST(a *ast) error {
	shape, ok := astNodes[a.Type]
	if !ok {
		return fmt.Errorf("unknown conformance node %q", a.Type)
	}
	bad := func(err error) error {
		return fmt.Errorf("conformance node %q: parameter %s: %w", a.Type, a.Param, err)
	}
	switch shape {
	case "name":
		var s string
		if err := json.Unmarshal(a.Param, &s); err != nil {
			return bad(err)
		}
	case "revision":
		var n int
		if err := json.Unmarshal(a.Param, &n); err != nil {
			return bad(err)
		}
	case "list":
		var terms []*ast
		if err := json.Unmarshal(a.Param, &terms); err != nil {
			return bad(err)
		}
		for _, t := range terms {
			if t == nil {
				return bad(errors.New("an empty term"))
			}
			if err := checkAST(t); err != nil {
				return err
			}
		}
	case "operand":
		var o ast
		if err := json.Unmarshal(a.Param, &o); err != nil {
			return bad(err)
		}
		return checkAST(&o)
	case "binary":
		var p struct {
			LHS *ast `json:"lhs"`
			RHS *ast `json:"rhs"`
		}
		if err := json.Unmarshal(a.Param, &p); err != nil {
			return bad(err)
		}
		if p.LHS == nil || p.RHS == nil {
			return bad(errors.New("lhs and rhs required"))
		}
		if err := checkAST(p.LHS); err != nil {
			return err
		}
		return checkAST(p.RHS)
	case "choice":
		var c struct {
			Expr *ast `json:"expr"`
		}
		if err := json.Unmarshal(a.Param, &c); err != nil {
			return bad(err)
		}
		if c.Expr == nil {
			return bad(errors.New("no expression"))
		}
		return checkAST(c.Expr)
	default: // "flag", "value": no parameter read
	}
	return nil
}

// root returns a conformance's AST; an absent conformance is matter.js's
// empty conformance.
func (c *conformance) root() *ast {
	if c == nil || c.AST == nil {
		return &ast{Type: astEmpty}
	}
	return c.AST
}

// text is the conformance as matter.js prints it; empty when none.
func (c *conformance) text() string {
	if c == nil {
		return ""
	}
	return c.Text
}

// list decodes an otherwise list's terms.
func (a *ast) list() []*ast {
	var out []*ast
	_ = json.Unmarshal(a.Param, &out)
	return out
}

// operand decodes the single operand of "!" or an optionalIf.
func (a *ast) operand() *ast {
	var out ast
	if json.Unmarshal(a.Param, &out) != nil {
		return &ast{Type: astEmpty}
	}
	return &out
}

// operands decodes a binary operator's lhs and rhs.
func (a *ast) operands() (lhs, rhs *ast) {
	var p struct {
		LHS *ast `json:"lhs"`
		RHS *ast `json:"rhs"`
	}
	_ = json.Unmarshal(a.Param, &p)
	if p.LHS == nil {
		p.LHS = &ast{Type: astEmpty}
	}
	if p.RHS == nil {
		p.RHS = &ast{Type: astEmpty}
	}
	return p.LHS, p.RHS
}

// name decodes a name node's name.
func (a *ast) name() string {
	var s string
	_ = json.Unmarshal(a.Param, &s)
	return s
}

// revision decodes a revision node's revision.
func (a *ast) revision() int {
	var n int
	_ = json.Unmarshal(a.Param, &n)
	return n
}

// isMandatory mirrors matter.js Conformance.isMandatory
// (packages/model/src/aspects/Conformance.ts:84-100): "M", or an otherwise
// list with "M" ahead of any "P".
func isMandatory(c *conformance) bool {
	a := c.root()
	if a.Type == astMandatory {
		return true
	}
	if a.Type == astOtherwise {
		for _, t := range a.list() {
			if t.Type == astProvisional {
				return false
			}
			if t.Type == astMandatory {
				return true
			}
		}
	}
	return false
}

// isProvisional mirrors Conformance.isProvisional (Conformance.ts:110-120).
func isProvisional(c *conformance) bool {
	a := c.root()
	if a.Type == astProvisional {
		return true
	}
	if a.Type == astOtherwise {
		for _, t := range a.list() {
			if t.Type == astProvisional {
				return true
			}
		}
	}
	return false
}

// isDisallowed and isObsolete mirror Conformance.isDisallowed /
// isObsolete (Conformance.ts:125-139).
func isDisallowed(c *conformance) bool { return c.root().Type == astDisallowed }

func isObsolete(c *conformance) bool { return c.root().Type == astObsolete }

// disposition is matter.js's RequirementDisposition
// (support/codegen/src/endpoints/requirement-disposition.ts:17-38).
type disposition int

const (
	dispMandate disposition = iota
	dispRelax
	dispGated
	dispProvisional
	dispPermit
	dispDisallow
	dispUnstated
)

// dispositionOf mirrors requirement-disposition.ts dispositionOf
// (:98-145) with satisfiesRevision (:54-64) and dispositionOfTerms
// (:72-93): a "Rev >= vN" term is resolved against the device type's
// revision.
func dispositionOf(c *conformance, clusterMandates bool, deviceRevision int) disposition {
	a := c.root()
	if a.Type == astEmpty {
		return dispUnstated
	}
	if a.Type == astDisallowed || a.Type == astObsolete {
		return dispDisallow
	}
	terms := []*ast{a}
	if a.Type == astOtherwise {
		terms = a.list()
	}
	applicable := 0
	for applicable < len(terms) {
		t := terms[applicable]
		if t.Type != astRevision {
			break
		}
		if deviceRevision >= t.revision() {
			return dispMandate
		}
		applicable++
	}
	if applicable > 0 {
		return dispositionOfTerms(terms[applicable:], clusterMandates)
	}
	if isMandatory(c) {
		return dispMandate
	}
	if isProvisional(c) {
		return dispProvisional
	}
	if a.Type == astOptional {
		if clusterMandates {
			return dispRelax
		}
		return dispPermit
	}
	if clusterMandates {
		return dispGated
	}
	return dispPermit
}

func dispositionOfTerms(terms []*ast, clusterMandates bool) disposition {
	if len(terms) > 0 {
		switch terms[0].Type {
		case astMandatory:
			return dispMandate
		case astProvisional:
			return dispProvisional
		case astDisallowed, astObsolete:
			return dispDisallow
		case astOptional:
			if clusterMandates {
				return dispRelax
			}
			return dispPermit
		default:
		}
	}
	if clusterMandates {
		return dispGated
	}
	return dispPermit
}

// mandatedUnder reports whether a Base requirement's conformance makes it
// mandatory where exactly the conditions in conds hold — the question
// matter.js's DeviceTypeConformance.missingBaseServersOf answers with a
// requirement judged "missing" (packages/model/src/logic/device-types/
// DeviceTypeConformance.ts:104-144). A name is a condition; an otherwise
// list applies its first term that applies; an optional or provisional
// term mandates nothing. Any other node — a description, a choice, a
// value, a comparison, a revision, an empty or deprecated conformance —
// is an error rather than false: the judgement does not evaluate it
// (R2-P6-par-2707).
func mandatedUnder(a *ast, conds map[string]bool) (bool, error) {
	switch a.Type {
	case astMandatory:
		return true, nil
	case astOptional, astProvisional, astDisallowed, astObsolete, astOptionalIf:
		return false, nil
	case astName, astAnd, astOr, astNot:
		return holds(a, conds)
	case astOtherwise:
		for _, t := range a.list() {
			switch t.Type {
			case astMandatory:
				return true, nil
			case astOptional, astProvisional, astDisallowed, astObsolete, astOptionalIf:
				return false, nil
			default:
				h, err := holds(t, conds)
				if err != nil || h {
					return h, err
				}
			}
		}
		return false, nil
	default:
		return false, fmt.Errorf("a Base requirement's conformance node %q: not evaluated", a.Type)
	}
}

// holds evaluates a boolean conformance expression over conds: a name,
// "&", "|" and "!". Any other node is an error (R2-P6-par-2707).
func holds(a *ast, conds map[string]bool) (bool, error) {
	switch a.Type {
	case astName:
		return conds[a.name()], nil
	case astAnd, astOr:
		l, r := a.operands()
		lv, err := holds(l, conds)
		if err != nil {
			return false, err
		}
		rv, err := holds(r, conds)
		if err != nil {
			return false, err
		}
		if a.Type == astAnd {
			return lv && rv, nil
		}
		return lv || rv, nil
	case astNot:
		v, err := holds(a.operand(), conds)
		return !v, err
	default:
		return false, fmt.Errorf("conformance node %q in a condition: not evaluated", a.Type)
	}
}

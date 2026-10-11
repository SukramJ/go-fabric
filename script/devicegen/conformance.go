// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package main

import (
	"encoding/json"
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
)

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
// term mandates nothing.
func mandatedUnder(a *ast, conds map[string]bool) bool {
	switch a.Type {
	case astMandatory:
		return true
	case astName:
		return conds[a.name()]
	case astAnd:
		l, r := a.operands()
		return holds(l, conds) && holds(r, conds)
	case astOr:
		l, r := a.operands()
		return holds(l, conds) || holds(r, conds)
	case astNot:
		return !holds(a.operand(), conds)
	case astOtherwise:
		for _, t := range a.list() {
			switch t.Type {
			case astMandatory:
				return true
			case astOptional, astProvisional, astDisallowed, astObsolete, astOptionalIf:
				return false
			default:
				if holds(t, conds) {
					return true
				}
			}
		}
		return false
	default:
		return false
	}
}

// holds evaluates a boolean conformance expression over conds.
func holds(a *ast, conds map[string]bool) bool {
	switch a.Type {
	case astName:
		return conds[a.name()]
	case astAnd:
		l, r := a.operands()
		return holds(l, conds) && holds(r, conds)
	case astOr:
		l, r := a.operands()
		return holds(l, conds) || holds(r, conds)
	case astNot:
		return !holds(a.operand(), conds)
	default:
		return false
	}
}

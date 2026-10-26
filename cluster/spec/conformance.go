// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package spec

// ConformanceOp is the type of a conformance AST node, matter.js's
// Conformance.Flag, Conformance.Special and Conformance.Operator values
// verbatim (packages/model/src/aspects/Conformance.ts).
type ConformanceOp string

// Conformance node types.
const (
	ConfMandatory   ConformanceOp = "M"
	ConfOptional    ConformanceOp = "O"
	ConfProvisional ConformanceOp = "P"
	ConfDeprecated  ConformanceOp = "D"
	ConfDisallowed  ConformanceOp = "X"
	ConfObsolete    ConformanceOp = "Z"
	ConfEmpty       ConformanceOp = "empty"
	ConfDesc        ConformanceOp = "desc"
	ConfName        ConformanceOp = "name"
	ConfValue       ConformanceOp = "value"
	ConfChoice      ConformanceOp = "choice"
	ConfOtherwise   ConformanceOp = "otherwise"
	ConfOptionalIf  ConformanceOp = "optionalIf"
	ConfRevision    ConformanceOp = "revision"
	ConfNot         ConformanceOp = "!"
	ConfEq          ConformanceOp = "=="
	ConfNe          ConformanceOp = "!="
	ConfOr          ConformanceOp = "|"
	ConfXor         ConformanceOp = "^"
	ConfAnd         ConformanceOp = "&"
	ConfDot         ConformanceOp = "."
	ConfGt          ConformanceOp = ">"
	ConfLt          ConformanceOp = "<"
	ConfGte         ConformanceOp = ">="
	ConfLte         ConformanceOp = "<="
)

// Choice is the parameter of a choice node ("O.a+"): the choice group,
// how many of its members to pick, and whether more or fewer are allowed.
type Choice struct {
	Name   string
	Num    int
	OrMore bool
	OrLess bool
}

// Conformance is a conformance AST node, the structure matter.js parses a
// conformance expression into. The zero value is the empty conformance,
// which matter.js treats as optional.
type Conformance struct {
	// Text is the expression as matter.js prints it; set on the root only.
	Text string
	Op   ConformanceOp
	// Name is the referenced name of a ConfName node (a feature, or
	// another element or field).
	Name string
	// Value is the JSON text of a ConfValue node's value.
	Value string
	// Rev is the revision of a ConfRevision node ("Rev >= v3").
	Rev int
	// Choice is the parameter of a ConfChoice node.
	Choice *Choice
	// Args are the operands: the terms of an otherwise list, [lhs, rhs] of
	// a binary operator, the single operand of "!", an optionalIf or a
	// choice.
	Args []Conformance
}

// Applicability is matter.js's Conformance.Applicability.
type Applicability uint8

// Applicability values.
const (
	// ApplicabilityNone: the element may not appear.
	ApplicabilityNone Applicability = iota
	// Optional: the implementation decides.
	ApplicabilityOptional
	// ApplicabilityConditional: features alone do not decide — another element, a
	// field value or the revision does.
	ApplicabilityConditional
	// ApplicabilityMandatory: the element must appear.
	ApplicabilityMandatory
)

// FeatureContext answers which names are features of the cluster
// (defined) and which of those the server supports.
type FeatureContext interface {
	Defined(name string) bool
	Supported(name string) bool
}

// RevisionContext is a [FeatureContext] that also knows the revision of
// the cluster it judges ([Context] returns one). A revision condition
// ("Rev >= vN") is decided against it: present at or above revision N,
// absent below — matter.js's runtime conformance compiler resolves the
// node "at compile time since the cluster revision is static"
// (packages/node/src/behavior/state/validation/conformance-compiler.ts:252-256,
// `revision >= ast.param ? ConformantNode : NonconformantNode`, the
// schema's revision defaulting to 1). A context without a revision leaves
// the condition conditional, as matter.js's model-level
// computeApplicability does (packages/model/src/aspects/Conformance.ts:995-997,
// 1004-1006).
type RevisionContext interface {
	FeatureContext
	Revision() uint16
}

// revisionApplicability decides a ConfRevision node: Mandatory where the
// context's revision reaches c.Rev, None below it, Conditional without a
// revision.
func revisionApplicability(c Conformance, ctx FeatureContext) Applicability {
	rc, ok := ctx.(RevisionContext)
	if !ok {
		return ApplicabilityConditional
	}
	rev := int(rc.Revision())
	if rev == 0 {
		rev = 1 // conformance-compiler.ts:254, `?? 1`
	}
	if rev >= c.Rev {
		return ApplicabilityMandatory
	}
	return ApplicabilityNone
}

// IsMandatory mirrors Conformance.isMandatory: "M", or an otherwise list
// that reaches "M" before any "P". It decides whether a payload field is
// mandatory (ValueModel.mandatory, which TlvOfModel encodes as TlvField).
func (c Conformance) IsMandatory() bool {
	switch c.Op {
	case ConfMandatory:
		return true
	case ConfOtherwise:
		for _, t := range c.Args {
			switch t.Op {
			case ConfProvisional:
				return false
			case ConfMandatory:
				return true
			default:
			}
		}
	default:
	}
	return false
}

// IsDisallowed reports a bare "X".
func (c Conformance) IsDisallowed() bool { return c.Op == ConfDisallowed }

// Applicability mirrors Conformance.applicabilityFor (computeApplicability
// in packages/model/src/aspects/Conformance.ts) for a server: deprecated
// and obsolete terms are disallowed.
func (c Conformance) Applicability(ctx FeatureContext) Applicability {
	return c.applicability(ctx, false)
}

// ApplicabilityDeprecatedOptional is [Conformance.Applicability] with the
// terms that deprecate or retire an element optional — applicabilityFor's
// deprecatedIsOptional (packages/model/src/aspects/Conformance.ts:182,
// :985-987 at 862ad75f), which a client judges an element the peer
// implements by.
func (c Conformance) ApplicabilityDeprecatedOptional(ctx FeatureContext) Applicability {
	return c.applicability(ctx, true)
}

func (c Conformance) applicability(ctx FeatureContext, deprecatedIsOptional bool) Applicability {
	if c.Op == ConfOtherwise {
		fallback := ApplicabilityNone
		provisional := false
		for i, node := range c.Args {
			if node.Op == ConfProvisional {
				provisional = true
				continue
			}
			// A trailing "D" announces a future deprecation; the terms
			// before it decide.
			if node.Op == ConfDeprecated && i == len(c.Args)-1 {
				continue
			}
			switch assessOuter(node, ctx, deprecatedIsOptional) {
			case ApplicabilityConditional:
				fallback = ApplicabilityConditional
			case ApplicabilityOptional:
				if fallback != ApplicabilityConditional {
					fallback = ApplicabilityOptional
				}
			case ApplicabilityMandatory:
				if provisional {
					return ApplicabilityOptional
				}
				return ApplicabilityMandatory
			default:
			}
		}
		return fallback
	}
	return assessOuter(c, ctx, deprecatedIsOptional)
}

func assessOuter(c Conformance, ctx FeatureContext, deprecatedIsOptional bool) Applicability {
	switch c.Op {
	case ConfChoice:
		// Choice conformance is ignored for these purposes.
		return assessOuter(arg(c), ctx, deprecatedIsOptional)
	case ConfOptional, ConfEmpty, "":
		return ApplicabilityOptional
	case ConfOptionalIf:
		if a := assessInner(arg(c), ctx); a != ApplicabilityMandatory {
			return a
		}
		return ApplicabilityOptional
	case ConfDisallowed:
		return ApplicabilityNone
	case ConfDeprecated, ConfObsolete:
		if deprecatedIsOptional {
			return ApplicabilityOptional
		}
		return ApplicabilityNone
	case ConfProvisional:
		return ApplicabilityOptional
	case ConfMandatory:
		return ApplicabilityMandatory
	case ConfDesc:
		return ApplicabilityConditional
	case ConfRevision:
		return revisionApplicability(c, ctx)
	default:
		return assessInner(c, ctx)
	}
}

func assessInner(c Conformance, ctx FeatureContext) Applicability {
	switch c.Op {
	case ConfName:
		if ctx.Defined(c.Name) {
			if ctx.Supported(c.Name) {
				return ApplicabilityMandatory
			}
			return ApplicabilityNone
		}
		// A field or element name: indeterminate here.
		return ApplicabilityConditional
	case ConfNot:
		switch assessInner(arg(c), ctx) {
		case ApplicabilityNone:
			return ApplicabilityMandatory
		case ApplicabilityMandatory:
			return ApplicabilityNone
		default:
			return ApplicabilityConditional
		}
	case ConfAnd:
		lhs, rhs := assessInner(lhsOf(c), ctx), assessInner(rhsOf(c), ctx)
		switch {
		case lhs == ApplicabilityNone || rhs == ApplicabilityNone:
			return ApplicabilityNone
		case lhs == ApplicabilityConditional || rhs == ApplicabilityConditional:
			return ApplicabilityConditional
		}
		return ApplicabilityMandatory
	case ConfRevision:
		return revisionApplicability(c, ctx)
	case ConfOr:
		lhs, rhs := assessInner(lhsOf(c), ctx), assessInner(rhsOf(c), ctx)
		switch {
		case lhs == ApplicabilityNone && rhs == ApplicabilityNone:
			return ApplicabilityNone
		case lhs == ApplicabilityMandatory || rhs == ApplicabilityMandatory:
			return ApplicabilityMandatory
		}
		return ApplicabilityConditional
	default:
		// Member access, comparisons, XOR and values: decidable only once
		// the record is known.
		return ApplicabilityConditional
	}
}

func arg(c Conformance) Conformance {
	if len(c.Args) == 0 {
		return Conformance{Op: ConfEmpty}
	}
	return c.Args[0]
}

func lhsOf(c Conformance) Conformance { return arg(c) }

func rhsOf(c Conformance) Conformance {
	if len(c.Args) < 2 {
		return Conformance{Op: ConfEmpty}
	}
	return c.Args[1]
}

// choiceOf returns the choice a conformance states at its top level or in
// one of its otherwise terms ("O.a+", "[LEV].b"), with the expression it
// qualifies.
func (c Conformance) choiceOf() (*Choice, Conformance, bool) {
	if c.Op == ConfChoice && c.Choice != nil {
		return c.Choice, arg(c), true
	}
	if c.Op == ConfOtherwise {
		for _, t := range c.Args {
			if ch, expr, ok := t.choiceOf(); ok {
				return ch, expr, true
			}
		}
	}
	return nil, Conformance{}, false
}

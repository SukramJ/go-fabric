// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package chipdm

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// Conformance operators. The flags and the special forms carry matter.js's
// own names (packages/model/src/aspects/Conformance.ts, Conformance.Flag,
// Conformance.Special, Conformance.Operator), so an AST read from the
// snapshot's "effective.conformance.ast" needs no translation.
const (
	opMandatory   = "M"
	opOptional    = "O"
	opProvisional = "P"
	opDeprecated  = "D"
	opDisallowed  = "X"
	opObsolete    = "Z"
	opDesc        = "desc"
	opName        = "name"
	opValue       = "value"
	opOptionalIf  = "optionalIf"
	opOtherwise   = "otherwise"
	opChoice      = "choice"
	opRevision    = "revision"
	opNot         = "!"
	opAnd         = "&"
	opOr          = "|"
	opXor         = "^"
	opDot         = "."
	opEQ          = "=="
	opNE          = "!="
	opGT          = ">"
	opLT          = "<"
	opGTE         = ">="
	opLTE         = "<="
)

// Conf is a conformance expression of either side, as one tree.
//
// The CHIP side is built from the data model's <mandatoryConform> /
// <optionalConform> / <otherwiseConform> / … element tree the way matter.js
// translates it (support/codegen/src/chipdm/translate-conformance.ts); the
// snapshot side is matter.js's own AST (Conformance.ast) or, where the
// snapshot carries only the text (device-type requirements), that text
// parsed with matter.js's grammar (Conformance.ts, ParsedAst).
type Conf struct {
	Op string `json:"op"`

	// Atom is the referenced name (opName) or the literal (opValue).
	Atom string `json:"atom,omitempty"`

	// Rev is the cluster revision of opRevision ("Rev >= vN").
	Rev int `json:"rev,omitempty"`

	// Args are the operands: one for "!", opOptionalIf and opChoice, two
	// for a binary operator, any number for opOtherwise.
	Args []*Conf `json:"args,omitempty"`

	// Choice, ChoiceNum, OrMore and OrLess describe opChoice.
	Choice    string `json:"choice,omitempty"`
	ChoiceNum int    `json:"choiceNum,omitempty"`
	OrMore    bool   `json:"orMore,omitempty"`
	OrLess    bool   `json:"orLess,omitempty"`
}

func isFlag(op string) bool {
	switch op {
	case opMandatory, opOptional, opProvisional, opDeprecated, opDisallowed, opObsolete:
		return true
	}
	return false
}

func isBinary(op string) bool {
	switch op {
	case opAnd, opOr, opXor, opEQ, opNE, opGT, opLT, opGTE, opLTE, opDot:
		return true
	}
	return false
}

// Key is the canonical form two conformances are compared by. It is the
// expression, not its text: names are compared case- and punctuation-
// insensitively (matter.js values.ts canonicalizeName; "Wi-Fi" and "WiFi"
// name one condition), literals by value (0x0A and 10 are one number), and
// the operands of an AND, OR or XOR chain as a set — "A & B" and "B & A" are
// one condition however the spec's table or CHIP's tree happens to nest it.
// The rendering follows matter.js's serialization (Conformance.serialize)
// without white space, so a key reads like matter.js's own normalized
// report text ("statuscode==success,o").
func (c *Conf) Key() string {
	if c == nil {
		return ""
	}
	return c.render(true)
}

// String is the readable form, matter.js's serialization of the tree.
func (c *Conf) String() string {
	if c == nil {
		return ""
	}
	return c.render(false)
}

func precedence(op string) int {
	switch op {
	case opGT, opLT, opGTE, opLTE:
		return 0
	case opEQ, opNE:
		return 1
	case opAnd:
		return 2
	case opOr, opXor:
		return 3
	}
	return -1
}

func (c *Conf) render(canonical bool) string {
	space := " "
	sep := ", "
	if canonical {
		space, sep = "", ","
	}
	switch {
	case isFlag(c.Op), c.Op == opDesc:
		if canonical {
			return strings.ToLower(c.Op)
		}
		return c.Op
	case c.Op == opName, c.Op == opValue:
		if canonical {
			return canonicalAtom(c.Atom)
		}
		return c.Atom
	case c.Op == opRevision:
		if canonical {
			return "rev>=v" + strconv.Itoa(c.Rev)
		}
		return "Rev >= v" + strconv.Itoa(c.Rev)
	case c.Op == opOptionalIf:
		return "[" + c.arg(0).render(canonical) + "]"
	case c.Op == opOtherwise:
		parts := make([]string, len(c.Args))
		for i, a := range c.Args {
			parts[i] = a.render(canonical)
		}
		return strings.Join(parts, sep)
	case c.Op == opNot:
		inner := c.arg(0)
		if isBinary(inner.Op) && inner.Op != opDot {
			return "!(" + inner.render(canonical) + ")"
		}
		return "!" + inner.render(canonical)
	case c.Op == opChoice:
		out := atomic(c.arg(0), "", canonical) + "." + c.Choice
		if c.ChoiceNum > 1 {
			out += strconv.Itoa(c.ChoiceNum)
		}
		switch {
		case c.OrMore && !c.OrLess:
			out += "+"
		case c.OrLess && !c.OrMore:
			out += "-"
		}
		return out
	case c.Op == opDot:
		return atomic(c.arg(0), c.Op, canonical) + "." + atomic(c.arg(1), c.Op, canonical)
	case isBinary(c.Op):
		if canonical && (c.Op == opAnd || c.Op == opOr || c.Op == opXor) {
			operands := c.flatten(c.Op)
			parts := make([]string, len(operands))
			for i, o := range operands {
				parts[i] = atomic(o, c.Op, canonical)
			}
			slices.Sort(parts)
			return strings.Join(parts, c.Op)
		}
		return atomic(c.arg(0), c.Op, canonical) + space + c.Op + space + atomic(c.arg(1), c.Op, canonical)
	}
	return "?" + c.Op
}

// atomic renders an operand of operator op, parenthesized when the operand
// binds less tightly (matter.js Conformance.ts serializeAtomic).
func atomic(c *Conf, op string, canonical bool) string {
	text := c.render(canonical)
	if op == "" {
		if isBinary(c.Op) && c.Op != opDot {
			return "(" + text + ")"
		}
		return text
	}
	p, q := precedence(op), precedence(c.Op)
	if p >= 0 && q >= 0 && p < q {
		return "(" + text + ")"
	}
	if canonical && p >= 0 && q >= 0 && p == q && c.Op != op {
		// "A | B ^ C" mixes two operators of one precedence; keep the
		// grouping explicit so a flattened chain cannot absorb it.
		return "(" + text + ")"
	}
	return text
}

func (c *Conf) arg(i int) *Conf {
	if i < len(c.Args) && c.Args[i] != nil {
		return c.Args[i]
	}
	return &Conf{Op: "?"}
}

// flatten collects the operands of a chain of one associative operator.
func (c *Conf) flatten(op string) []*Conf {
	if c.Op != op {
		return []*Conf{c}
	}
	var out []*Conf
	for _, a := range c.Args {
		out = append(out, a.flatten(op)...)
	}
	return out
}

// canonicalAtom reduces a name or literal to its comparable form: a number
// to its decimal value, anything else to lower-case letters and digits
// (matter.js values.ts canonicalizeName / canonicalizeValue).
func canonicalAtom(s string) string {
	if n, ok := parseNumber(s); ok {
		return n
	}
	return canonicalName(s)
}

// canonicalName is matter.js values.ts canonicalizeName: lower case, letters
// and digits only.
func canonicalName(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// parseNumber recognizes the numeric literals matter.js values.ts
// translateValue accepts (decimal, 0x hex, 0b binary, an optional sign and
// fraction) and renders them in decimal.
func parseNumber(s string) (string, bool) {
	t := strings.TrimSpace(s)
	if t == "" {
		return "", false
	}
	neg := false
	switch t[0] {
	case '-':
		neg, t = true, t[1:]
	case '+':
		t = t[1:]
	}
	lower := strings.ToLower(t)
	var digits string
	switch {
	case strings.HasPrefix(lower, "0x"):
		v, err := strconv.ParseUint(lower[2:], 16, 64)
		if err != nil {
			return "", false
		}
		digits = strconv.FormatUint(v, 10)
	case strings.HasPrefix(lower, "0b"):
		v, err := strconv.ParseUint(lower[2:], 2, 64)
		if err != nil {
			return "", false
		}
		digits = strconv.FormatUint(v, 10)
	default:
		if !isDecimal(lower) {
			return "", false
		}
		f, err := strconv.ParseFloat(lower, 64)
		if err != nil {
			return "", false
		}
		if strings.Contains(lower, ".") {
			digits = strconv.FormatFloat(f, 'f', -1, 64)
		} else {
			v, err := strconv.ParseUint(lower, 10, 64)
			if err != nil {
				return "", false
			}
			digits = strconv.FormatUint(v, 10)
		}
	}
	if neg && digits != "0" {
		digits = "-" + digits
	}
	return digits, true
}

func isDecimal(s string) bool {
	dot := false
	if s == "" || s[0] == '.' || s[len(s)-1] == '.' {
		return false
	}
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9':
		case r == '.' && !dot:
			dot = true
		default:
			return false
		}
	}
	return true
}

// --- matter.js's AST, as the snapshot carries it ---------------------------

// confFromAST decodes matter.js's Conformance.Ast as the extractor emits it
// (script/extract-from-matter-js.ts conformanceOut): {"type", "param"}.
func confFromAST(raw json.RawMessage) (*Conf, error) {
	var node struct {
		Type  string          `json:"type"`
		Param json.RawMessage `json:"param"`
	}
	if err := json.Unmarshal(raw, &node); err != nil {
		return nil, fmt.Errorf("conformance ast: %w", err)
	}
	switch {
	case isFlag(node.Type), node.Type == opDesc:
		return &Conf{Op: node.Type}, nil
	case node.Type == "empty":
		return nil, nil
	case node.Type == opName, node.Type == opValue:
		atom, err := atomFromJSON(node.Param)
		if err != nil {
			return nil, err
		}
		return &Conf{Op: node.Type, Atom: atom}, nil
	case node.Type == opRevision:
		var rev int
		if err := json.Unmarshal(node.Param, &rev); err != nil {
			return nil, fmt.Errorf("revision conformance: %w", err)
		}
		return &Conf{Op: opRevision, Rev: rev}, nil
	case node.Type == opNot, node.Type == opOptionalIf:
		inner, err := confFromAST(node.Param)
		if err != nil {
			return nil, err
		}
		return &Conf{Op: node.Type, Args: []*Conf{inner}}, nil
	case node.Type == opOtherwise:
		var list []json.RawMessage
		if err := json.Unmarshal(node.Param, &list); err != nil {
			return nil, fmt.Errorf("otherwise conformance: %w", err)
		}
		out := &Conf{Op: opOtherwise}
		for _, item := range list {
			c, err := confFromAST(item)
			if err != nil {
				return nil, err
			}
			out.Args = append(out.Args, c)
		}
		return out, nil
	case node.Type == opChoice:
		var p struct {
			Name   string          `json:"name"`
			Num    int             `json:"num"`
			OrMore bool            `json:"orMore"`
			OrLess bool            `json:"orLess"`
			Expr   json.RawMessage `json:"expr"`
		}
		if err := json.Unmarshal(node.Param, &p); err != nil {
			return nil, fmt.Errorf("choice conformance: %w", err)
		}
		expr, err := confFromAST(p.Expr)
		if err != nil {
			return nil, err
		}
		return &Conf{Op: opChoice, Args: []*Conf{expr}, Choice: p.Name, ChoiceNum: p.Num, OrMore: p.OrMore, OrLess: p.OrLess}, nil
	case isBinary(node.Type):
		var p struct {
			LHS json.RawMessage `json:"lhs"`
			RHS json.RawMessage `json:"rhs"`
		}
		if err := json.Unmarshal(node.Param, &p); err != nil {
			return nil, fmt.Errorf("%s conformance: %w", node.Type, err)
		}
		lhs, err := confFromAST(p.LHS)
		if err != nil {
			return nil, err
		}
		rhs, err := confFromAST(p.RHS)
		if err != nil {
			return nil, err
		}
		return &Conf{Op: node.Type, Args: []*Conf{lhs, rhs}}, nil
	}
	return nil, fmt.Errorf("conformance ast: unknown node type %q", node.Type)
}

// atomFromJSON renders a FieldValue param: a string name, a number, a
// boolean, null, or matter.js's {"type":"reference","name":…}.
func atomFromJSON(raw json.RawMessage) (string, error) {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return "", fmt.Errorf("conformance atom: %w", err)
	}
	switch x := v.(type) {
	case string:
		return x, nil
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64), nil
	case bool:
		return strconv.FormatBool(x), nil
	case nil:
		return "null", nil
	case map[string]any:
		if name, ok := x["name"].(string); ok {
			return name, nil
		}
	}
	return "", fmt.Errorf("conformance atom: unsupported value %s", raw)
}

// --- matter.js's text grammar ----------------------------------------------

// ParseConformance parses conformance text in matter.js's grammar
// (packages/model/src/aspects/Conformance.ts, ParsedAst): top-level terms
// separated by commas form an otherwise list, "[…]" is optional-if, a
// trailing ".a", ".a2+" or ".a-" is a choice, "Rev >= vN" is revision
// conformance, and the binary operators bind by matter.js's precedence
// (comparisons, then equality, then "&", then "|" and "^").
func ParseConformance(text string) (*Conf, error) {
	p := &confParser{}
	if err := p.lex(strings.Replace(text, " or ", " | ", 1)); err != nil {
		return nil, err
	}
	var terms []*Conf
	for !p.done() {
		var term *Conf
		if p.at("[") {
			p.next()
			inner, err := p.expression()
			if err != nil {
				return nil, err
			}
			if !p.at("]") {
				return nil, fmt.Errorf("conformance %q: unterminated optional group", text)
			}
			p.next()
			term, err = p.choice(&Conf{Op: opOptionalIf, Args: []*Conf{inner}})
			if err != nil {
				return nil, err
			}
		} else {
			var err error
			term, err = p.expression()
			if err != nil {
				return nil, err
			}
		}
		terms = append(terms, term)
		// A comma or white space separates the terms ("top-to-bottom").
		if p.at(",") {
			p.next()
		}
	}
	switch len(terms) {
	case 0:
		return nil, nil
	case 1:
		return terms[0], nil
	}
	return &Conf{Op: opOtherwise, Args: terms}, nil
}

type confToken struct {
	kind string // "word", "value" or the operator itself
	text string
}

type confParser struct {
	toks []confToken
	pos  int
}

func (p *confParser) lex(s string) error {
	for i := 0; i < len(s); {
		r := s[i]
		switch {
		case r == ' ' || r == '\t' || r == '\n' || r == '\r':
			i++
		case isWordStart(r):
			j := i
			for j < len(s) && isWordPart(s[j]) {
				j++
			}
			p.toks = append(p.toks, confToken{"word", s[i:j]})
			i = j
		case r >= '0' && r <= '9':
			j := i
			for j < len(s) && (isWordPart(s[j]) || s[j] == '.') {
				j++
			}
			p.toks = append(p.toks, confToken{"value", s[i:j]})
			i = j
		default:
			two := ""
			if i+1 < len(s) {
				two = s[i : i+2]
			}
			switch two {
			case "==", "!=", ">=", "<=":
				p.toks = append(p.toks, confToken{two, two})
				i += 2
				continue
			}
			if !strings.ContainsRune("!&|^.<>[](),+-", rune(r)) {
				return fmt.Errorf("conformance %q: unexpected character %q", s, r)
			}
			p.toks = append(p.toks, confToken{string(r), string(r)})
			i++
		}
	}
	return nil
}

func isWordStart(r byte) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || r == '_'
}

func isWordPart(r byte) bool {
	return isWordStart(r) || (r >= '0' && r <= '9')
}

func (p *confParser) done() bool { return p.pos >= len(p.toks) }

func (p *confParser) tok() confToken {
	if p.done() {
		return confToken{}
	}
	return p.toks[p.pos]
}

func (p *confParser) peek() confToken {
	if p.pos+1 >= len(p.toks) {
		return confToken{}
	}
	return p.toks[p.pos+1]
}

func (p *confParser) at(kind string) bool { return !p.done() && p.toks[p.pos].kind == kind }

func (p *confParser) next() { p.pos++ }

func (p *confParser) expression() (*Conf, error) {
	var operands []*Conf
	var operators []string
	first, err := p.atomicChoice()
	if err != nil {
		return nil, err
	}
	operands = append(operands, first)
	for !p.done() && precedence(p.tok().kind) >= 0 {
		operators = append(operators, p.tok().kind)
		p.next()
		next, err := p.atomicChoice()
		if err != nil {
			return nil, err
		}
		operands = append(operands, next)
	}
	// Fold by precedence, left to right within a level (matter.js
	// Conformance.ts parseExpression).
	for level := range 4 {
		for i := 0; i < len(operators); {
			if precedence(operators[i]) != level {
				i++
				continue
			}
			node := &Conf{Op: operators[i], Args: []*Conf{operands[i], operands[i+1]}}
			operands = append(operands[:i], append([]*Conf{node}, operands[i+2:]...)...)
			operators = append(operators[:i], operators[i+1:]...)
		}
	}
	return operands[0], nil
}

func (p *confParser) atomicChoice() (*Conf, error) {
	expr, err := p.atomic()
	if err != nil {
		return nil, err
	}
	return p.choice(expr)
}

func isChoiceIndicator(s string) bool {
	if s == "" || len(s) > 2 || s[0] < 'a' || s[0] > 'z' {
		return false
	}
	return len(s) == 1 || (s[1] >= '0' && s[1] <= '9')
}

func (p *confParser) choice(expr *Conf) (*Conf, error) {
	if !p.at(".") {
		return expr, nil
	}
	p.next()
	t := p.tok()
	if t.kind != "word" {
		return nil, fmt.Errorf("conformance: choice indicator expected after %q", expr.String())
	}
	if !isChoiceIndicator(t.text) {
		// A qualified name (SolicitOffer.VideoStreamID), not a choice.
		dot := &Conf{Op: opDot, Args: []*Conf{expr, {Op: opName, Atom: t.text}}}
		p.next()
		for p.at(".") && p.peek().kind == "word" && !isChoiceIndicator(p.peek().text) {
			p.next()
			dot = &Conf{Op: opDot, Args: []*Conf{dot, {Op: opName, Atom: p.tok().text}}}
			p.next()
		}
		return p.choice(dot)
	}
	p.next()
	c := &Conf{Op: opChoice, Args: []*Conf{expr}, Choice: t.text[:1], ChoiceNum: 1}
	if len(t.text) == 2 {
		c.ChoiceNum = int(t.text[1] - '0')
	}
	switch {
	case p.at("+"):
		c.OrMore = true
		p.next()
	case p.at("-"):
		c.OrLess = true
		p.next()
	}
	return c, nil
}

func (p *confParser) atomic() (*Conf, error) {
	if p.done() {
		return nil, errors.New("conformance: expression expected")
	}
	t := p.tok()
	switch t.kind {
	case "word":
		p.next()
		if isFlag(t.text) {
			return &Conf{Op: t.text}, nil
		}
		switch strings.ToLower(t.text) {
		case "desc":
			return &Conf{Op: opDesc}, nil
		case "null", "true", "false":
			return &Conf{Op: opValue, Atom: strings.ToLower(t.text)}, nil
		}
		if t.text == "Rev" && p.at(">=") {
			p.next()
			v := p.tok()
			if v.kind == "word" && len(v.text) > 1 && v.text[0] == 'v' {
				if n, err := strconv.Atoi(v.text[1:]); err == nil {
					p.next()
					return &Conf{Op: opRevision, Rev: n}, nil
				}
			}
			return nil, errors.New("conformance: malformed revision conformance")
		}
		return &Conf{Op: opName, Atom: t.text}, nil
	case "value":
		p.next()
		return &Conf{Op: opValue, Atom: t.text}, nil
	case "!":
		p.next()
		inner, err := p.atomicChoice()
		if err != nil {
			return nil, err
		}
		return &Conf{Op: opNot, Args: []*Conf{inner}}, nil
	case "(":
		p.next()
		inner, err := p.expression()
		if err != nil {
			return nil, err
		}
		if !p.at(")") {
			return nil, errors.New("conformance: unterminated group")
		}
		p.next()
		return inner, nil
	}
	return nil, fmt.Errorf("conformance: unexpected %q", t.text)
}

// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package spec

import (
	"errors"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/SukramJ/go-fabric/im"
)

// The refusals of a struct or command field's conformance. Each is
// answered CONSTRAINT_ERROR: matter.js's ConformanceError,
// EnumValueConformanceError and UnknownEnumValueError all carry
// Status.ConstraintError (packages/protocol/src/action/errors.ts:99-123 at
// 862ad75f).
var (
	// ErrConformance: a field the conformance requires is absent, one it
	// does not allow is present, or a choice group holds too few or too
	// many fields (matter.js ConformanceError).
	ErrConformance = errors.New("spec: field conformance violated")
	// ErrEnumValueConformance: an enum value whose own conformance the
	// features or the sibling fields do not allow (matter.js
	// EnumValueConformanceError).
	ErrEnumValueConformance = errors.New("spec: enum value conformance violated")
	// ErrUnknownEnumValue: a value the enum does not define (matter.js
	// UnknownEnumValueError).
	ErrUnknownEnumValue = errors.New("spec: enum value not defined")
)

// The node codes of matter.js's conformance compiler
// (packages/node/src/behavior/state/validation/conformance-util.ts:21-39
// at 862ad75f).
type confCode uint8

const (
	codeNonconformant confCode = iota
	codeConformant
	codeOptional
	codeDisallowed
	codeValue
	codeEvaluate
)

// confValue is a value a conformance expression reads: undefined (absent)
// when !defined, the null when defined and v is nil.
type confValue struct {
	v       any
	defined bool
}

// confNode is matter.js's DynamicNode: a static node, or an Evaluate node
// whose eval yields a static one for a record.
type confNode struct {
	code  confCode
	value confValue
	eval  func(own confValue, loc *confLocation) confNode
}

var (
	conformantNode    = confNode{code: codeConformant}
	nonconformantNode = confNode{code: codeNonconformant}
)

func (n confNode) static() bool { return n.code != codeEvaluate }

// evaluateNode is conformance-util.ts:62-67.
func evaluateNode(n confNode, own confValue, loc *confLocation) confNode {
	if n.code == codeEvaluate {
		return n.eval(own, loc)
	}
	return n
}

// asConformance is conformance-util.ts:76-84: a value node is conformant
// when the value is defined.
func asConformance(n confNode) confNode {
	if n.code == codeValue {
		if n.value.defined {
			return conformantNode
		}
		return nonconformantNode
	}
	return n
}

// asBoolean is conformance-util.ts:90-103.
func asBoolean(n confNode) bool {
	switch asConformance(n).code {
	case codeConformant, codeOptional:
		return true
	default:
		return false
	}
}

// asNonvalue is conformance-compiler.ts:752-761.
func asNonvalue(n confNode) confNode {
	if n.code == codeValue {
		if asBoolean(n) {
			return conformantNode
		}
		return nonconformantNode
	}
	return n
}

// confChoice counts the fields of one choice group a struct holds
// (ValidationLocation.Choice).
type confChoice struct {
	count, target  int
	orMore, orLess bool
}

// confLocation is what a struct's validation hands each field's
// validator: the struct the field sits in (siblings), the choice counts,
// and the resolvers of a name the struct does not hold
// (ValueValidator.ts:411-418 sublocation).
type confLocation struct {
	siblings reflect.Value
	fields   *Struct
	choices  map[string]*confChoice
	scope    nameScope
}

// nameScope resolves a name that is not a sibling: the cluster's
// attributes (matter.js resolves it through the managed value's parent,
// NameResolver.ts:88-105), then outer — matter.js's outerResolve, the
// request of a command whose response is judged (ProtocolService.ts:681-684).
//
// conformance switches the field conformance on, enums the enum-value
// part of it: [Instance.CheckConformance] judges both, a [Server] calls it
// for a controller's write, and a command response is judged without the
// enum values ([Instance.checkResponse]). The value checks of the host's
// own values ([Instance.CheckSet], [Instance.CheckValue]) and
// [Instance.ValidateWrite] leave it off.
type nameScope struct {
	peers       Peers
	outer       func(name string) (any, bool)
	conformance bool
	enums       bool
}

// sameName reports whether two specification names name one element, as
// matter.js compares them camelized (conformance-compiler.ts:103 camelize).
func sameName(a, b string) bool { return strings.EqualFold(goFieldName(a), goFieldName(b)) }

// fieldValueOf reads a Go struct field as the value conformance sees: a
// nil pointer or interface is absent, a null Nullable or a pointer to a
// nil interface is the null.
func fieldValueOf(fv reflect.Value) confValue {
	if !fv.IsValid() {
		return confValue{}
	}
	switch fv.Kind() {
	case reflect.Pointer, reflect.Interface:
		if fv.IsNil() {
			return confValue{}
		}
		fv = fv.Elem()
		if fv.Kind() == reflect.Interface {
			if fv.IsNil() {
				return confValue{defined: true}
			}
			fv = fv.Elem()
		}
	default:
	}
	v := fv.Interface()
	if n, ok := v.(nullable); ok {
		x, isNull := n.nullValue()
		if isNull {
			return confValue{defined: true}
		}
		v = x
	}
	return confValue{v: v, defined: true}
}

// fieldOfStruct returns the Go field of v the definition field name maps to.
func fieldOfStruct(v reflect.Value, name string) reflect.Value {
	for v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface {
		if v.IsNil() {
			return reflect.Value{}
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return reflect.Value{}
	}
	t := v.Type()
	for n := range t.NumField() {
		if sf := t.Field(n); sf.IsExported() && sameName(sf.Name, name) {
			return v.Field(n)
		}
	}
	return reflect.Value{}
}

// confCompiler compiles the conformance of one field of a struct: matter.js
// astToFunction (conformance-compiler.ts:87-220) for a member whose parent
// is fields.
type confCompiler struct {
	inst   *Instance
	fields *Struct
	// nameRef overrides the name reference while a comparison's
	// right-hand side names a member of an enum sibling
	// (conformance-compiler.ts:671-710).
	nameRef func(name string) confNode
}

func (c *confCompiler) feature(name string) (*Feature, bool) {
	for i := range c.inst.def.Features {
		if f := &c.inst.def.Features[i]; f.Name == name {
			return f, true
		}
	}
	return nil, false
}

// createNameReference is conformance-compiler.ts:102-124: a sibling read
// from the record, else a cluster attribute, else the outer resolver.
func (c *confCompiler) createNameReference(name string) confNode {
	if c.nameRef != nil {
		return c.nameRef(name)
	}
	if c.fields != nil {
		for fi := range c.fields.Fields {
			if f := &c.fields.Fields[fi]; sameName(f.Name, name) {
				return confNode{code: codeEvaluate, eval: func(_ confValue, loc *confLocation) confNode {
					return confNode{code: codeValue, value: fieldValueOf(fieldOfStruct(loc.siblings, f.Name))}
				}}
			}
		}
	}
	for ai := range c.inst.def.Attributes {
		if a := &c.inst.def.Attributes[ai]; sameName(a.Name, name) {
			id := a.ID
			return confNode{code: codeEvaluate, eval: func(_ confValue, loc *confLocation) confNode {
				if loc.scope.peers == nil {
					return confNode{code: codeValue}
				}
				v, ok := loc.scope.peers(id)
				return confNode{code: codeValue, value: confValue{v: v, defined: ok}}
			}}
		}
	}
	return confNode{code: codeEvaluate, eval: func(_ confValue, loc *confLocation) confNode {
		if loc.scope.outer == nil {
			return confNode{code: codeValue}
		}
		v, ok := loc.scope.outer(name)
		return confNode{code: codeValue, value: confValue{v: v, defined: ok}}
	}}
}

// compile is conformance-compiler.ts:233-310.
func (c *confCompiler) compile(ast Conformance) confNode {
	switch ast.Op {
	case "", ConfEmpty, ConfDesc, ConfProvisional, ConfDeprecated, ConfOptional:
		return confNode{code: codeOptional}
	case ConfChoice:
		return c.createChoice(ast)
	case ConfOtherwise:
		return c.createGroup(ast.Args)
	case ConfName:
		return c.createName(ast.Name)
	case ConfRevision:
		rev := int(c.inst.def.Revision)
		if rev == 0 {
			rev = 1
		}
		if rev >= ast.Rev {
			return conformantNode
		}
		return nonconformantNode
	case ConfOptionalIf:
		return c.createOptionalIf(arg(ast))
	case ConfValue:
		return confNode{code: codeValue, value: literalOf(ast.Value)}
	case ConfDisallowed, ConfObsolete:
		return confNode{code: codeDisallowed}
	case ConfMandatory:
		return conformantNode
	case ConfDot:
		return c.createDotAccess(lhsOf(ast), rhsOf(ast))
	case ConfAnd, ConfOr, ConfXor:
		return c.createLogicalBinaryOp(ast.Op, lhsOf(ast), rhsOf(ast))
	case ConfEq, ConfNe, ConfGt, ConfLt, ConfGte, ConfLte:
		return c.createComparisonOp(ast.Op, lhsOf(ast), rhsOf(ast))
	case ConfNot:
		return createLogicalInversion(c.compile(arg(ast)))
	default:
		return confNode{code: codeOptional}
	}
}

// createChoice is conformance-compiler.ts:312-362.
func (c *confCompiler) createChoice(ast Conformance) confNode {
	compiled := c.compile(arg(ast))
	if compiled.static() {
		if code := asConformance(compiled).code; code == codeNonconformant || code == codeDisallowed {
			return compiled
		}
	}
	ch := ast.Choice
	if ch == nil {
		return compiled
	}
	return confNode{code: codeEvaluate, eval: func(own confValue, loc *confLocation) confNode {
		result := evaluateNode(compiled, own, loc)
		if code := asConformance(result).code; code == codeNonconformant || code == codeDisallowed {
			return result
		}
		if loc.choices != nil {
			cc := loc.choices[ch.Name]
			if cc == nil {
				cc = &confChoice{target: ch.Num, orMore: ch.OrMore, orLess: ch.OrLess}
				loc.choices[ch.Name] = cc
			}
			if own.defined {
				cc.count++
			}
		}
		return result
	}}
}

// createGroup is conformance-compiler.ts:364-421: the first member that is
// not nonconformant decides; a trailing "D" is dropped.
func (c *confCompiler) createGroup(param []Conformance) confNode {
	if n := len(param); n > 0 && param[n-1].Op == ConfDeprecated {
		param = param[:n-1]
	}
	var reduced []confNode
	for _, p := range param {
		m := c.compile(p)
		if m.code == codeNonconformant {
			continue
		}
		if m.code == codeOptional || m.code == codeConformant {
			if len(reduced) > 0 {
				reduced = append(reduced, m)
				break
			}
			return m
		}
		reduced = append(reduced, m)
	}
	if len(reduced) == 1 && reduced[0].static() {
		return reduced[0]
	}
	return confNode{code: codeEvaluate, eval: func(own confValue, loc *confLocation) confNode {
		for _, m := range reduced {
			if n := evaluateNode(m, own, loc); n.code != codeNonconformant {
				return n
			}
		}
		return nonconformantNode
	}}
}

// createName is conformance-compiler.ts:433-459: a feature, a command or
// event of the cluster (conformant: element conformance is structural),
// else a value read at run time.
func (c *confCompiler) createName(name string) confNode {
	if f, ok := c.feature(name); ok {
		if c.inst.features&f.Mask() != 0 {
			return conformantNode
		}
		return nonconformantNode
	}
	for ci := range c.inst.def.Commands {
		if c.inst.def.Commands[ci].Name == name {
			return conformantNode
		}
	}
	for ei := range c.inst.def.Events {
		if c.inst.def.Events[ei].Name == name {
			return conformantNode
		}
	}
	return c.createNameReference(name)
}

// createDotAccess is conformance-compiler.ts:461-512.
func (c *confCompiler) createDotAccess(lhs, rhs Conformance) confNode {
	left := c.compile(lhs)
	if rhs.Op != ConfName {
		return confNode{code: codeValue}
	}
	property := rhs.Name
	if left.code == codeConformant && lhs.Op == ConfName {
		left = c.createNameReference(lhs.Name)
	}
	access := func(v confValue) confNode {
		if !v.defined || v.v == nil {
			return confNode{code: codeValue}
		}
		return confNode{code: codeValue, value: fieldValueOf(fieldOfStruct(reflect.ValueOf(v.v), property))}
	}
	switch left.code {
	case codeValue:
		return access(left.value)
	case codeEvaluate:
		return confNode{code: codeEvaluate, eval: func(own confValue, loc *confLocation) confNode {
			l := left.eval(own, loc)
			if l.code != codeValue {
				return confNode{code: codeValue}
			}
			return access(l.value)
		}}
	default:
		return confNode{code: codeValue}
	}
}

// createOptionalIf is conformance-compiler.ts:514-545.
func (c *confCompiler) createOptionalIf(param Conformance) confNode {
	node := asConformance(c.compile(param))
	switch node.code {
	case codeConformant:
		return confNode{code: codeOptional}
	case codeEvaluate:
		return confNode{code: codeEvaluate, eval: func(own confValue, loc *confLocation) confNode {
			n := asConformance(node.eval(own, loc))
			if n.code == codeConformant {
				return confNode{code: codeOptional}
			}
			return n
		}}
	default:
		return node
	}
}

// createBooleanTest is conformance-util.ts:243-258.
func createBooleanTest(n confNode) confNode {
	if n.static() {
		return n
	}
	return confNode{code: codeEvaluate, eval: func(own confValue, loc *confLocation) confNode {
		return asConformance(n.eval(own, loc))
	}}
}

// createLogicalInversion is conformance-util.ts:125-151.
func createLogicalInversion(n confNode) confNode {
	operand := asConformance(n)
	switch operand.code {
	case codeNonconformant:
		return conformantNode
	case codeEvaluate:
		return confNode{code: codeEvaluate, eval: func(own confValue, loc *confLocation) confNode {
			if asConformance(operand.eval(own, loc)).code == codeNonconformant {
				return conformantNode
			}
			return nonconformantNode
		}}
	default:
		return nonconformantNode
	}
}

// createLogicalBinaryOp is conformance-compiler.ts:583-669.
func (c *confCompiler) createLogicalBinaryOp(op ConformanceOp, lhs, rhs Conformance) confNode {
	l := createBooleanTest(c.compile(lhs))
	r := createBooleanTest(c.compile(rhs))
	dynamic := func(fn func(a, b bool) bool) confNode {
		return confNode{code: codeEvaluate, eval: func(own confValue, loc *confLocation) confNode {
			if fn(asBoolean(evaluateNode(l, own, loc)), asBoolean(evaluateNode(r, own, loc))) {
				return conformantNode
			}
			return nonconformantNode
		}}
	}
	switch op {
	case ConfAnd:
		switch {
		case l.static():
			if asBoolean(l) {
				return asNonvalue(r)
			}
			return nonconformantNode
		case r.static():
			if asBoolean(r) {
				return asNonvalue(l)
			}
			return nonconformantNode
		}
		return dynamic(func(a, b bool) bool { return a && b })
	case ConfOr:
		switch {
		case l.static():
			if asBoolean(l) {
				return conformantNode
			}
			return asNonvalue(r)
		case r.static():
			if asBoolean(r) {
				return conformantNode
			}
			return asNonvalue(l)
		}
		return dynamic(func(a, b bool) bool { return a || b })
	default: // ConfXor
		switch {
		case l.static():
			if asBoolean(l) {
				return createLogicalInversion(r)
			}
			return asNonvalue(r)
		case r.static():
			if asBoolean(r) {
				return createLogicalInversion(l)
			}
			return asNonvalue(l)
		}
		return dynamic(func(a, b bool) bool { return a != b })
	}
}

// createComparisonOp is conformance-compiler.ts:671-710: where the
// left-hand side names an enum sibling, a name on the right-hand side is
// a member of that enum.
func (c *confCompiler) createComparisonOp(op ConformanceOp, lhs, rhs Conformance) confNode {
	saved := c.nameRef
	defer func() { c.nameRef = saved }()
	if lhs.Op == ConfName && c.fields != nil {
		for fi := range c.fields.Fields {
			f := &c.fields.Fields[fi]
			if !sameName(f.Name, lhs.Name) || f.Type.Kind != KindEnum || f.Type.Enum == nil {
				continue
			}
			e := f.Type.Enum
			c.nameRef = func(name string) confNode {
				for vi := range e.Values {
					if e.Values[vi].Name == name {
						return confNode{code: codeValue, value: confValue{v: e.Values[vi].Value, defined: true}}
					}
				}
				outer := *c
				outer.nameRef = saved
				return outer.createNameReference(name)
			}
		}
	}
	l, r := c.compile(lhs), c.compile(rhs)
	if l.static() && r.static() {
		return performComparison(op, l, r)
	}
	return confNode{code: codeEvaluate, eval: func(own confValue, loc *confLocation) confNode {
		return performComparison(op, evaluateNode(l, own, loc), evaluateNode(r, own, loc))
	}}
}

// performComparison is conformance-util.ts:183-209: conformant when both
// sides are defined, not null, and the operator holds.
func performComparison(op ConformanceOp, l, r confNode) confNode {
	if l.code != codeValue || r.code != codeValue {
		return nonconformantNode
	}
	a, b := l.value, r.value
	if !a.defined || a.v == nil || !b.defined || b.v == nil {
		return nonconformantNode
	}
	if compareValues(op, a.v, b.v) {
		return conformantNode
	}
	return nonconformantNode
}

// compareValues applies op to two values: numbers by value whatever their
// Go width, other values by equality.
func compareValues(op ConformanceOp, a, b any) bool {
	x, xok := numberOf(a)
	y, yok := numberOf(b)
	if xok && yok {
		switch op {
		case ConfEq:
			return x == y
		case ConfNe:
			return x != y
		case ConfGt:
			return x > y
		case ConfLt:
			return x < y
		case ConfGte:
			return x >= y
		case ConfLte:
			return x <= y
		default:
			return false
		}
	}
	switch op {
	case ConfEq:
		return reflect.DeepEqual(a, b)
	case ConfNe:
		return !reflect.DeepEqual(a, b)
	default:
		return false
	}
}

// numberOf is v as a float64 when it is a number of any Go type.
func numberOf(v any) (float64, bool) {
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return float64(rv.Int()), true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return float64(rv.Uint()), true
	case reflect.Float32, reflect.Float64:
		return rv.Float(), true
	default:
		return 0, false
	}
}

// literalOf is the value of a ConfValue node: a number, a bool or the
// null; any other text is kept as a string.
func literalOf(text string) confValue {
	switch strings.ToLower(text) {
	case "null":
		return confValue{defined: true}
	case "true":
		return confValue{v: true, defined: true}
	case "false":
		return confValue{v: false, defined: true}
	}
	if n, err := strconv.ParseInt(text, 0, 64); err == nil {
		return confValue{v: n, defined: true}
	}
	if f, err := strconv.ParseFloat(text, 64); err == nil {
		return confValue{v: f, defined: true}
	}
	return confValue{v: strings.Trim(text, `"`), defined: true}
}

// fieldValidator judges one field's presence and, for an enum, its value
// against the conformance (astToFunction, conformance-compiler.ts:126-220,
// with addEnumMemberValidation :763-868).
type fieldValidator func(own confValue, loc *confLocation) error

func (i *Instance) fieldValidator(where string, s *Struct, f *Field, enums bool) fieldValidator {
	c := &confCompiler{inst: i, fields: s}
	text := f.Conformance.Text
	nullable := f.Quality.Nullable
	require := func(own confValue) error {
		if !own.defined {
			return Errorf(im.StatusConstraintError, "%s.%s: Conformance %q: Matter requires you to set this attribute: %w", where, f.Name, text, ErrConformance)
		}
		return nil
	}
	disallow := func(own confValue) error {
		if own.defined {
			return Errorf(im.StatusConstraintError, "%s.%s: Conformance %q: Matter does not allow you to set this attribute: %w", where, f.Name, text, ErrConformance)
		}
		return nil
	}
	var main fieldValidator
	disallows := false
	node := c.compile(f.Conformance)
	switch node.code {
	case codeConformant:
		if !nullable {
			main = func(own confValue, _ *confLocation) error { return require(own) }
		}
	case codeNonconformant, codeDisallowed:
		main = func(own confValue, _ *confLocation) error { return disallow(own) }
		disallows = true
	case codeValue:
		switch {
		case !node.value.defined:
			main = func(own confValue, _ *confLocation) error { return disallow(own) }
			disallows = true
		case !nullable:
			main = func(own confValue, _ *confLocation) error { return require(own) }
		}
	case codeOptional:
	case codeEvaluate:
		main = func(own confValue, loc *confLocation) error {
			n := node.eval(own, loc)
			switch n.code {
			case codeConformant:
				if !nullable {
					return require(own)
				}
			case codeNonconformant, codeDisallowed:
				return disallow(own)
			case codeValue:
				if !n.value.defined {
					return disallow(own)
				}
				if !nullable {
					return require(own)
				}
			default:
			}
			return nil
		}
	}
	if disallows || !enums || f.Type.Kind != KindEnum || f.Type.Enum == nil {
		return main
	}
	return i.withEnumMembers(where, f, c, main)
}

// withEnumMembers is addEnumMemberValidation (conformance-compiler.ts:763-868
// at 862ad75f): a value must be a member's id or lie in a range member,
// and that member's conformance must allow it.
func (i *Instance) withEnumMembers(where string, f *Field, c *confCompiler, main fieldValidator) fieldValidator {
	e := f.Type.Enum
	if len(e.Values) == 0 && len(e.Ranges) == 0 {
		return main
	}
	type member struct {
		name, what string
		text       string
		node       confNode
	}
	compileMember := func(conf Conformance) confNode {
		if conf.Op == ConfChoice {
			conf = arg(conf)
		}
		return c.compile(conf)
	}
	ids := map[uint64]member{}
	for vi := range e.Values {
		v := &e.Values[vi]
		ids[v.Value] = member{name: v.Name, what: fmt.Sprintf("ID %d", v.Value), text: v.Conformance.Text, node: compileMember(v.Conformance)}
	}
	type rangeMember struct {
		r EnumRange
		m member
	}
	var ranges []rangeMember
	for ri := range e.Ranges {
		r := e.Ranges[ri]
		ranges = append(ranges, rangeMember{r, member{name: r.Name, what: fmt.Sprintf("range %d to %d", r.Min, r.Max), text: r.Conformance.Text, node: compileMember(r.Conformance)}})
	}
	return func(own confValue, loc *confLocation) error {
		if main != nil {
			if err := main(own, loc); err != nil {
				return err
			}
		}
		if !own.defined || own.v == nil {
			return nil
		}
		n, ok := numberOf(own.v)
		if !ok || n < 0 {
			return nil
		}
		v := uint64(n)
		m, found := ids[v]
		if !found {
			for ri := range ranges {
				if ranges[ri].r.Contains(v) {
					m, found = ranges[ri].m, true
					break
				}
			}
		}
		if !found {
			return Errorf(im.StatusConstraintError, "%s.%s: Matter does not define the enum value %d for %s: %w", where, f.Name, v, f.Name, ErrUnknownEnumValue)
		}
		code := m.node.code
		if code == codeEvaluate {
			// A value node counts by whether it is defined; matter.js
			// leaves that case unsupported (conformance-compiler.ts:829-830).
			code = asConformance(m.node.eval(confValue{v: true, defined: true}, loc)).code
		}
		if code == codeNonconformant || code == codeDisallowed {
			return Errorf(im.StatusConstraintError, "%s.%s: Conformance %q: Matter does not allow enum value %s (%s) here: %w", where, f.Name, m.text, m.name, m.what, ErrEnumValueConformance)
		}
		return nil
	}
}

// checkFieldConformance judges every field of the struct value rv against
// its conformance with the struct as the record, then the choice groups'
// counts (createStructValidator, ValueValidator.ts:392-467 at 862ad75f).
func (i *Instance) checkFieldConformance(where string, s *Struct, rv reflect.Value, scope nameScope) error {
	loc := &confLocation{siblings: rv, fields: s, choices: map[string]*confChoice{}, scope: scope}
	for fi := range s.Fields {
		f := &s.Fields[fi]
		if f.Conformance.Op == ConfDeprecated && f.Type.Kind == KindAny && f.Type.Name == "" {
			continue
		}
		validate := i.fieldValidator(where, s, f, scope.enums)
		if validate == nil {
			continue
		}
		if err := validate(fieldValueOf(fieldOfStruct(rv, f.Name)), loc); err != nil {
			return err
		}
	}
	for _, name := range slices.Sorted(maps.Keys(loc.choices)) {
		ch := loc.choices[name]
		if ch.count < ch.target && !ch.orLess {
			return Errorf(im.StatusConstraintError, "%s: Conformance choice %q: Too few fields present (%d of min %d): %w", where, name, ch.count, ch.target, ErrConformance)
		}
		if ch.count > ch.target && !ch.orMore {
			return Errorf(im.StatusConstraintError, "%s: Conformance choice %q: Too many fields present (%d of max %d): %w", where, name, ch.count, ch.target, ErrConformance)
		}
	}
	return nil
}

// CheckConformance holds the value of attrID to its type and constraint
// and every struct field it carries — in the attribute's own struct, a
// list's entries, a nested struct — to the field's conformance, with the
// struct as the record a name resolves in (a sibling field, else an
// attribute of the cluster through peers), the selected features and the
// cluster's revision: a field the conformance requires and the value
// leaves out, one it does not allow and the value carries, an enum value
// whose conformance does not allow it or that the enum does not define,
// and a choice group with too few or too many fields are refused with
// CONSTRAINT_ERROR ([ErrConformance], [ErrEnumValueConformance],
// [ErrUnknownEnumValue]). A [Server] calls it for a controller's write;
// a self-held server may call it where it takes a struct.
//
// Mirrors matter.js
// packages/node/src/behavior/state/validation/ValueValidator.ts:392-467
// (createStructValidator) over conformance.ts:19-60
// (createConformanceValidator) and conformance-compiler.ts:87-220
// (astToFunction), :763-868 (addEnumMemberValidation) at 862ad75f.
func (i *Instance) CheckConformance(attrID uint32, value any, peers Peers) error {
	a := i.def.Attribute(attrID)
	if a == nil || value == nil {
		return nil
	}
	_, err := i.checkValueIn(a.Name, a.Type, a.Quality.Nullable, a.Constraint, underlying(value), peers, nameScope{peers: peers, conformance: true, enums: true})
	return err
}

// checkResponse judges the presence of the response's fields a handler
// returned by their conformance — with the request resolving the name of
// the request command ("TestRequest.Enable"), the cluster's attributes a
// name the response does not hold — and answers a failure FAILURE: a field
// mandatory by the request left out, one the conformance does not allow
// present. Mirrors matter.js
// packages/node/src/node/integration/ProtocolService.ts:601-626
// (validateCommandPayload), :681-684 (outerResolve: the request by the
// request command's name) and :733-740 at 862ad75f (a response validation
// error is a FailureError).
//
// The fields' types, constraints and enum values, and the structs nested
// in the response, are not judged here: the module's servers answer what
// matter.js's behaviors answer, and some of it the response schema
// refuses — DoorLock echoes an index out of range with its constraint
// switched off (GetWeekDayScheduleResponse, DoorLockServer.ts:704-707) and answers a cluster status code in the
// global status enum (SetCredentialResponse DUPLICATE, :441). An absent
// payload, or one whose Go type does not carry every field the definition
// states (a hand-written decoder's), is not judged (:609-611).
func (i *Instance) checkResponse(cmdID uint32, request, response any, peers Peers) error {
	req := i.def.Command(cmdID, Request)
	if req == nil || response == nil {
		return nil
	}
	resp := i.def.responseOf(req)
	if resp == nil || len(resp.Fields) == 0 {
		return nil
	}
	rv := reflect.ValueOf(response)
	for rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return nil
		}
		rv = rv.Elem()
	}
	if rv.Kind() != reflect.Struct {
		return nil
	}
	for fi := range resp.Fields {
		if !rv.FieldByName(goFieldName(resp.Fields[fi].Name)).IsValid() {
			return nil
		}
	}
	scope := nameScope{peers: peers, conformance: true, outer: func(name string) (any, bool) {
		if request != nil && sameName(name, req.Name) {
			return request, true
		}
		return nil, false
	}}
	if err := i.checkFieldConformance(i.def.Name+": "+resp.Name, &Struct{Name: resp.Name, Fields: resp.Fields}, rv, scope); err != nil {
		return Errorf(im.StatusFailure, "%s: response validation failed for %s: %w", i.def.Name, req.Name, err)
	}
	return nil
}

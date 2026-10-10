// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package spec

import (
	"fmt"
	"math"
	"reflect"
	"strings"
	"unicode"
	"unicode/utf16"

	"github.com/SukramJ/go-fabric/im"
)

// StatusError carries an exact IM status to the dispatcher.
type StatusError struct {
	Status im.StatusCode
	Msg    string
}

// Error implements error.
func (e *StatusError) Error() string { return e.Msg }

// MatterStatusCode implements [im.StatusCodeError].
func (e *StatusError) MatterStatusCode() im.StatusCode { return e.Status }

var _ im.StatusCodeError = (*StatusError)(nil)

// Errorf returns a [StatusError] with status and a formatted message.
func Errorf(status im.StatusCode, format string, args ...any) error {
	return &StatusError{Status: status, Msg: fmt.Sprintf(format, args...)}
}

// Peers resolves the current value of a sibling attribute a constraint
// names ("minMeasuredValue to maxMeasuredValue"); nil resolves none, and an
// unresolved bound admits every value, as matter.js's does.
type Peers func(attrID uint32) (any, bool)

// ValidateWrite checks a write the way matter.js answers one
// (packages/protocol/src/action/server/AttributeWriteResponse.ts, then the
// attribute's ValueValidator), and returns the value normalised: nil for
// null, uint64 for an unsigned integer, enum or bitmap, int64 for a signed
// one, float64, bool, string or []byte. Struct and list values are
// returned as given, after their checks: a list's length against the
// constraint ("max 5") and each entry against the entry type and entry
// constraint; a struct's fields against their types and constraints.
//
// The statuses: an attribute the server does not serve is
// UNSUPPORTED_ATTRIBUTE; one that is not writable (access "R", or quality
// "F") UNSUPPORTED_WRITE; a value outside its type, its nullable range, its
// constraint, its enum's conformant values or its bitmap's defined bits
// CONSTRAINT_ERROR. A value of the wrong Go type is CONSTRAINT_ERROR as
// well — the convention every server in this module follows for a value
// the bridge decoded into a type the attribute does not hold.
func (i *Instance) ValidateWrite(attrID uint32, value any, peers Peers) (any, error) {
	a := i.def.Attribute(attrID)
	if a == nil || !i.Serves(attrID) {
		return nil, Errorf(im.StatusUnsupportedAttribute, "%s: attribute 0x%04X is not served", i.def.Name, attrID)
	}
	if !a.Writable() {
		return nil, Errorf(im.StatusUnsupportedWrite, "%s: attribute %s is read-only", i.def.Name, a.Name)
	}
	v, err := i.checkValue(a.Name, a.Type, a.Quality.Nullable, a.Constraint, value, peers)
	if err != nil {
		return nil, err
	}
	return v, nil
}

// CheckValue holds a value of the attribute attrID to its type, its
// nullability and its constraint — the checks [Instance.ValidateWrite]
// makes after access — whether the attribute is served or not, and
// returns it normalised as ValidateWrite does. matter.js validates a
// behavior's state against the model on every commit, not only on a
// controller's write (packages/node/src/behavior/state/validation/
// ValueValidator.ts, constraint.ts); a server holds the values its host
// sets to the same checks. A refusal is a [StatusError] with
// CONSTRAINT_ERROR, UNSUPPORTED_ATTRIBUTE for an attribute the cluster
// does not define.
func (i *Instance) CheckValue(attrID uint32, value any, peers Peers) (any, error) {
	a := i.def.Attribute(attrID)
	if a == nil {
		return nil, Errorf(im.StatusUnsupportedAttribute, "%s: attribute 0x%04X is not defined", i.def.Name, attrID)
	}
	return i.checkValue(a.Name, a.Type, a.Quality.Nullable, a.Constraint, underlying(value), peers)
}

func (i *Instance) checkValue(name string, t Type, nullable bool, c Constraint, value any, peers Peers) (any, error) {
	if value == nil {
		if nullable {
			return nil, nil
		}
		return nil, Errorf(im.StatusConstraintError, "%s: %s is not nullable", i.def.Name, name)
	}
	var (
		v   any
		why string
	)
	switch t.Kind {
	case KindUint, KindEnum, KindBitmap:
		v, why = i.checkUnsigned(t, nullable, c, value, peers)
	case KindInt:
		v, why = i.checkSigned(t, nullable, c, value, peers)
	case KindFloat32, KindFloat64:
		v, why = i.checkFloat(t, c, value, peers)
	case KindBool:
		if _, ok := value.(bool); !ok {
			why = fmt.Sprintf("%v is not a bool", value)
		}
		v = value
	case KindString, KindBytes:
		v, why = i.checkLength(t, c, value, peers)
	case KindList:
		if err := i.checkList(name, t, c, value, peers); err != nil {
			return nil, err
		}
		return value, nil
	case KindStruct:
		if err := i.checkStruct(name, t.Struct, value); err != nil {
			return nil, err
		}
		return value, nil
	default:
		return value, nil
	}
	if why != "" {
		return nil, Errorf(im.StatusConstraintError, "%s: %s %s", i.def.Name, name, why)
	}
	return v, nil
}

// checkUnsigned checks an unsigned integer, enum or bitmap: its width,
// its enum's conformant values or its bitmap's defined bits, its
// constraint. It returns the value as a uint64, or why it is refused.
func (i *Instance) checkUnsigned(t Type, nullable bool, c Constraint, value any, peers Peers) (v any, why string) {
	limit := UintMax(t.Bits, nullable)
	n, ok := asUint(value)
	switch {
	case !ok || n > limit:
		return nil, fmt.Sprintf("%v outside 0..%d", value, limit)
	case t.Kind == KindEnum && t.Enum != nil && !i.EnumSupported(t.Enum, n):
		return nil, fmt.Sprintf("%d is not a supported %s value", n, t.Enum.Name)
	case t.Kind == KindBitmap && t.Bitmap != nil && n&^t.Bitmap.Defined() != 0:
		return nil, fmt.Sprintf("0x%X sets reserved %s bits", n, t.Bitmap.Name)
	case !i.satisfies(c, float64(n), peers):
		return nil, fmt.Sprintf("%d violates %q", n, c.Text)
	}
	return n, ""
}

// checkSigned checks a signed integer against its width and constraint.
func (i *Instance) checkSigned(t Type, nullable bool, c Constraint, value any, peers Peers) (v any, why string) {
	lo, hi := IntRange(t.Bits, nullable)
	n, ok := asInt(value)
	switch {
	case !ok || n < lo || n > hi:
		return nil, fmt.Sprintf("%v outside %d..%d", value, lo, hi)
	case !i.satisfies(c, float64(n), peers):
		return nil, fmt.Sprintf("%d violates %q", n, c.Text)
	}
	return n, ""
}

// checkFloat checks a float against its precision and constraint.
func (i *Instance) checkFloat(t Type, c Constraint, value any, peers Peers) (v any, why string) {
	f, ok := asFloat(value)
	switch {
	case !ok || math.IsNaN(f) || (t.Kind == KindFloat32 && math.Abs(f) > math.MaxFloat32 && !math.IsInf(f, 0)):
		return nil, fmt.Sprintf("%v is not a %s", value, t.Name)
	case !i.satisfies(c, f, peers):
		return nil, fmt.Sprintf("%v violates %q", f, c.Text)
	}
	return f, ""
}

// checkLength checks a string or an octet string and its length
// constraint.
func (i *Instance) checkLength(t Type, c Constraint, value any, peers Peers) (v any, why string) {
	var n int
	switch x := value.(type) {
	case string:
		n = StringLength(x)
	case []byte:
		n = len(x)
	}
	if (t.Kind == KindString) != isString(value) || (t.Kind == KindBytes) != isBytes(value) {
		return nil, fmt.Sprintf("%v is not a %s", value, t.Name)
	}
	if !i.satisfies(c, float64(n), peers) {
		return nil, fmt.Sprintf("length %d violates %q", n, c.Text)
	}
	return value, ""
}

// checkList checks a list value: a Go slice or array whose length meets
// the constraint, and whose entries meet the entry type and the entry
// constraint ("max 16[2]", "all[min 1]") — the list checks of matter.js's
// packages/node/src/behavior/state/validation/ValueValidator.ts and
// constraint.ts.
func (i *Instance) checkList(name string, t Type, c Constraint, value any, peers Peers) error {
	rv := reflect.ValueOf(value)
	if rv.Kind() != reflect.Slice && rv.Kind() != reflect.Array {
		return Errorf(im.StatusConstraintError, "%s: %s %v is not a list", i.def.Name, name, value)
	}
	if !i.satisfies(c, float64(rv.Len()), peers) {
		return Errorf(im.StatusConstraintError, "%s: %s length %d violates %q", i.def.Name, name, rv.Len(), c.Text)
	}
	if t.Entry == nil {
		return nil
	}
	entry := t.Entry.Constraint
	if c.Entry != nil {
		entry = *c.Entry
	}
	for n := range rv.Len() {
		if _, err := i.checkValue(fmt.Sprintf("%s[%d]", name, n), t.Entry.Type, t.Entry.Quality.Nullable, entry, entryValue(rv.Index(n).Interface()), nil); err != nil {
			return err
		}
	}
	return nil
}

// checkStruct checks a struct value, a generated struct: each field the
// definition states against its type, nullability and constraint. The Go
// field of a definition field is named as script/clustergen names it
// (exportName in script/clustergen/names.go, assigned to fieldInfo.goName
// in script/clustergen/gen.go); an optional field is a pointer, nil when
// absent (gen.go writePayload); a nullable one a [Nullable]
// (fieldInfo.goType). A field constraint naming a sibling is not resolved.
func (i *Instance) checkStruct(name string, s *Struct, value any) error {
	rv := reflect.ValueOf(value)
	if rv.Kind() == reflect.Pointer && !rv.IsNil() {
		rv = rv.Elem()
	}
	if rv.Kind() != reflect.Struct {
		return Errorf(im.StatusConstraintError, "%s: %s %v is not a struct", i.def.Name, name, value)
	}
	if s == nil {
		return nil
	}
	for fi := range s.Fields {
		f := &s.Fields[fi]
		fv := rv.FieldByName(goFieldName(f.Name))
		if !fv.IsValid() {
			return Errorf(im.StatusConstraintError, "%s: %s has no field %s", i.def.Name, name, f.Name)
		}
		if fv.Kind() == reflect.Pointer {
			if fv.IsNil() {
				continue
			}
			fv = fv.Elem()
		}
		if _, err := i.checkValue(name+"."+f.Name, f.Type, f.Quality.Nullable, f.Constraint, entryValue(fv.Interface()), nil); err != nil {
			return err
		}
	}
	return nil
}

// entryValue is a list entry or a struct field in the form the checks
// read: a [Nullable] unwrapped (nil for null), a named scalar type turned
// into its underlying type.
func entryValue(v any) any {
	if n, ok := v.(nullable); ok {
		x, isNull := n.nullValue()
		if isNull {
			return nil
		}
		v = x
	}
	return underlying(v)
}

// goFieldName is the Go name script/clustergen gives a field named name,
// its exportName (script/clustergen/names.go): the first letter
// upper-cased, every character that is not a letter or a digit dropped and
// the next one upper-cased, a leading digit prefixed with "V".
func goFieldName(name string) string {
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

func isString(v any) bool { _, ok := v.(string); return ok }

func isBytes(v any) bool { _, ok := v.([]byte); return ok }

// satisfies evaluates a constraint against a number — a value, or the
// length of a string, octet string or list. A constraint with parts holds
// when one part does; a bound the runtime cannot evaluate holds.
func (i *Instance) satisfies(c Constraint, v float64, peers Peers) bool {
	if len(c.Parts) > 0 {
		for _, p := range c.Parts {
			if i.satisfies(p, v, peers) {
				return true
			}
		}
		return false
	}
	if b, ok := i.bound(c.Value, peers); ok && v != b {
		return false
	}
	if b, ok := i.bound(c.Min, peers); ok && v < b {
		return false
	}
	if b, ok := i.bound(c.Max, peers); ok && v > b {
		return false
	}
	return true
}

func (i *Instance) bound(b *Bound, peers Peers) (float64, bool) {
	switch {
	case b == nil || b.Expr != "":
		return 0, false
	case b.Ref == "":
		return float64(b.Int), true
	case peers == nil:
		return 0, false
	}
	a := i.def.AttributeByName(b.Ref)
	if a == nil {
		return 0, false
	}
	v, ok := peers(a.ID)
	if !ok || v == nil {
		return 0, false
	}
	return asFloat(v)
}

// UintMax is the largest value an unsigned integer of bits holds; one less
// when nullable, the all-ones value being null on the wire.
func UintMax(bits int, nullable bool) uint64 {
	if bits <= 0 || bits > 64 {
		bits = 64
	}
	limit := uint64(math.MaxUint64) >> (64 - bits)
	if nullable {
		limit--
	}
	return limit
}

// IntRange is the range a signed integer of bits holds; the minimum is one
// higher when nullable, the most negative value being null on the wire.
func IntRange(bits int, nullable bool) (lo, hi int64) {
	if bits <= 0 || bits > 64 {
		bits = 64
	}
	hi = int64(math.MaxInt64) >> (64 - bits)
	lo = -hi - 1
	if nullable {
		lo++
	}
	return lo, hi
}

// StringLength is the length matter.js's TlvString bounds a string by:
// JavaScript's String.length, the number of UTF-16 code units.
func StringLength(s string) int {
	n := 0
	for _, r := range s {
		n += utf16.RuneLen(r)
	}
	return n
}

func asUint(v any) (uint64, bool) {
	switch x := v.(type) {
	case uint8:
		return uint64(x), true
	case uint16:
		return uint64(x), true
	case uint32:
		return uint64(x), true
	case uint64:
		return x, true
	case uint:
		return uint64(x), true
	case int, int8, int16, int32, int64:
		n, _ := asInt(x)
		if n < 0 {
			return 0, false
		}
		return uint64(n), true
	}
	return 0, false
}

func asInt(v any) (int64, bool) {
	switch x := v.(type) {
	case int8:
		return int64(x), true
	case int16:
		return int64(x), true
	case int32:
		return int64(x), true
	case int64:
		return x, true
	case int:
		return int64(x), true
	case uint8, uint16, uint32, uint64, uint:
		n, _ := asUint(x)
		if n > math.MaxInt64 {
			return 0, false
		}
		return int64(n), true
	}
	return 0, false
}

func asFloat(v any) (float64, bool) {
	switch x := v.(type) {
	case float32:
		return float64(x), true
	case float64:
		return x, true
	}
	if n, ok := asInt(v); ok {
		return float64(n), true
	}
	if n, ok := asUint(v); ok {
		return float64(n), true
	}
	return 0, false
}

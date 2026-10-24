// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package spec

import (
	"fmt"
	"reflect"

	"github.com/SukramJ/go-fabric/im"
)

// EmptyListDefault is the default of an element whose stated default is
// the empty list ("default": []). The attribute state ignores it — a list
// attribute starts absent, as before — and [Instance.PatchDefaults] gives
// a struct field that states it an empty list of its own.
// fabric:reachable:reason="the Default of a generated element whose stated default is the empty list; it is only ever a package-level initializer value, which RTA does not follow"
type EmptyListDefault struct{}

// PatchDefaults returns value — what the host hands attrID as its initial
// state or a Set — with the defaults the feature selection makes storage
// hold: in every struct the value carries (the attribute's own, a list's
// entries, a struct field's struct), a field the host left out (a nil
// pointer; a field the generator types as a value is never left out)
// whose conformance is mandatory under the selected features receives the
// default the definition states for it, in its Go type — a bitmap as its
// generated bitmap type, a temperature or percentage in the encoding
// units the generator restated it in, an empty list as a list of its own
// — or the null when it is nullable and states none. A field whose
// conformance the features leave optional or disallow stays absent. An
// exported Go field the definition does not declare is refused
// ([ErrInvalidValue], "<field> is not a property of <struct>"). The value
// the host handed over is not changed: a struct is copied before a field
// is set. A scalar value is returned as it is.
//
// [Server] applies it where Initial and the host's Set enter the server,
// before the value is checked; a controller's write is not patched — the
// wire states every field it carries.
//
// Mirrors matter.js
// packages/node/src/behavior/state/managed/values/ValuePatcher.ts at
// 862ad75f: StructPatcher (:70-163; a key the schema does not declare is
// refused, :124-128) and ListPatcher (:168-233; a new entry patched over
// its defaults, :212-216) over getDefaults (:42-65), which takes each
// member's StoredDefaultValue under the supervisor's features
// (packages/model/src/logic/SelectDefaultValue.ts:39-58: no default
// without operational support, i.e. mandatory under the active features,
// Scope.ts:202-217; the stated default, else null for a mandatory
// nullable member). The defaults are computed per call against the
// instance's features, so two feature selections of one definition never
// share them (ValuePatcher.ts:33-35, defaultsCache per supervisor).
func (i *Instance) PatchDefaults(attrID uint32, value any) (any, error) {
	a := i.def.Attribute(attrID)
	if a == nil || value == nil {
		return value, nil
	}
	ctx := featuresOnly{Context(i.def, i.features)}
	out, changed, err := i.patchValue(ctx, a.Name, a.Type, reflect.ValueOf(value))
	if err != nil || !changed {
		return value, err
	}
	return out.Interface(), nil
}

// patchValue patches v of type t, returning the patched copy and whether
// anything changed.
func (i *Instance) patchValue(ctx FeatureContext, name string, t Type, v reflect.Value) (reflect.Value, bool, error) {
	if v.Kind() == reflect.Interface && !v.IsNil() {
		v = v.Elem()
	}
	if v.IsValid() && v.Kind() == reflect.Struct && v.Type().Implements(nullableType) {
		// A Nullable: its value is patched, the null is left as it is.
		if v.FieldByName("Null").Bool() {
			return v, false, nil
		}
		inner, changed, err := i.patchValue(ctx, name, t, v.FieldByName("Value"))
		if err != nil || !changed {
			return v, false, err
		}
		out := reflect.New(v.Type()).Elem()
		out.Set(v)
		out.FieldByName("Value").Set(inner)
		return out, true, nil
	}
	switch t.Kind {
	case KindStruct:
		return i.patchStruct(ctx, name, t.Struct, v)
	case KindList:
		if t.Entry == nil || (v.Kind() != reflect.Slice && v.Kind() != reflect.Array) {
			return v, false, nil
		}
		var out reflect.Value
		for n := range v.Len() {
			e, changed, err := i.patchValue(ctx, fmt.Sprintf("%s[%d]", name, n), t.Entry.Type, v.Index(n))
			if err != nil {
				return v, false, err
			}
			if !changed {
				continue
			}
			if !out.IsValid() {
				out = reflect.MakeSlice(reflect.SliceOf(v.Type().Elem()), v.Len(), v.Len())
				reflect.Copy(out, v)
				if v.Kind() == reflect.Slice {
					out = out.Convert(v.Type())
				}
			}
			out.Index(n).Set(e)
		}
		if !out.IsValid() {
			return v, false, nil
		}
		return out, true, nil
	default:
		return v, false, nil
	}
}

// patchStruct patches a struct value, or a pointer to one, field by field.
func (i *Instance) patchStruct(ctx FeatureContext, name string, s *Struct, v reflect.Value) (reflect.Value, bool, error) {
	if s == nil {
		return v, false, nil
	}
	ptr := v.Kind() == reflect.Pointer
	if ptr {
		if v.IsNil() {
			return v, false, nil
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return v, false, nil
	}
	rt := v.Type()
	declared := make(map[string]*Field, len(s.Fields))
	for fi := range s.Fields {
		f := &s.Fields[fi]
		declared[goFieldName(f.Name)] = f
		declared[goFieldName(f.Name)+"Field"] = f
	}
	for n := range rt.NumField() {
		if sf := rt.Field(n); sf.IsExported() && declared[sf.Name] == nil {
			return v, false, Errorf(im.StatusConstraintError, "%s: %s is not a property of %s", i.def.Name, sf.Name, s.Name)
		}
	}
	out := reflect.New(rt).Elem()
	out.Set(v)
	changed := false
	for fi := range s.Fields {
		f := &s.Fields[fi]
		fv := out.FieldByName(goFieldName(f.Name))
		if !fv.IsValid() || !fv.CanSet() {
			continue
		}
		fname := name + "." + f.Name
		if fv.Kind() == reflect.Pointer && fv.IsNil() {
			d, ok := i.storedDefault(ctx, f, fv.Type().Elem())
			if ok {
				p := reflect.New(fv.Type().Elem())
				p.Elem().Set(d)
				fv.Set(p)
				changed = true
			}
			continue
		}
		if fv.Kind() == reflect.Slice && fv.IsNil() {
			if _, empty := f.Default.(EmptyListDefault); empty && f.Conformance.Applicability(ctx) == ApplicabilityMandatory {
				fv.Set(reflect.MakeSlice(fv.Type(), 0, 0))
				changed = true
			}
			continue
		}
		// A pointer field is replaced by a pointer to the patched copy, so
		// the value the host handed over keeps its own.
		target, isPtr := fv, fv.Kind() == reflect.Pointer
		if isPtr {
			target = fv.Elem()
		}
		p, ok, err := i.patchValue(ctx, fname, f.Type, target)
		if err != nil {
			return v, false, err
		}
		if ok {
			if isPtr {
				np := reflect.New(target.Type())
				np.Elem().Set(p)
				fv.Set(np)
			} else {
				fv.Set(p)
			}
			changed = true
		}
	}
	if !changed {
		return v, false, nil
	}
	if ptr {
		p := reflect.New(rt)
		p.Elem().Set(out)
		return p, true, nil
	}
	return out, true, nil
}

// storedDefault is the value storage holds for f absent one, as a value
// of the Go type t: matter.js StoredDefaultValue (SelectDefaultValue.ts:39-58
// at 862ad75f) — nothing unless f is mandatory under the features, then
// the stated default, else the null of a nullable f.
func (i *Instance) storedDefault(ctx FeatureContext, f *Field, t reflect.Type) (reflect.Value, bool) {
	if f.Conformance.Applicability(ctx) != ApplicabilityMandatory {
		return reflect.Value{}, false
	}
	switch d := f.Default.(type) {
	case EmptyListDefault:
		if t.Kind() == reflect.Slice {
			return reflect.MakeSlice(t, 0, 0), true
		}
		return reflect.Value{}, false
	case nil:
		if !f.Quality.Nullable {
			return reflect.Value{}, false
		}
	case NullDefault:
		if !f.Quality.Nullable {
			return reflect.Value{}, false
		}
	default:
		v, ok := defaultOf(f.Type, d)
		if !ok {
			return reflect.Value{}, false
		}
		return convert(v, t)
	}
	// The null of a nullable field.
	if t.Implements(nullableType) || t.Kind() == reflect.Pointer || t.Kind() == reflect.Slice {
		return convert(nil, t)
	}
	return reflect.Value{}, false
}

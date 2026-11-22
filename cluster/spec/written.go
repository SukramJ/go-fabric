// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package spec

import (
	"errors"
	"fmt"
	"reflect"
	"sort"
	"sync"

	"thingfabric.dev/go-fabric/im"
)

// The check of a controller's written value against the definition, which
// the dispatcher makes before any server runs (owner decision 64): what
// matter.js validates when the written value enters the behavior's state
// (packages/node/src/behavior/state/managed/values/StructManager.ts:279-298
// at db021da0, the model's ValueValidator), for every server whose cluster has
// a generated definition, the hand-written ones included.

// FieldMap names, for a hand-written Go type that carries a definition
// struct, the Go field that holds each definition field: definition field
// name (matter.js's) → Go field name.
type FieldMap map[string]string

// heldForm is a [MapFields] declaration.
type heldForm struct {
	def    *Struct
	fields FieldMap
}

// fieldMaps holds the declarations: reflect.Type → heldForm.
var fieldMaps sync.Map

// MapFields declares that the hand-written Go type T carries the
// definition struct s, with the Go field of each of s's fields named by m:
// the definition's checks then read a value of T through m. A server
// written against its own types states the mapping explicitly, never by
// matching names; [VerifyFieldMaps] fails when a definition field has no
// Go field.
func MapFields[T any](s *Struct, m FieldMap) {
	fieldMaps.Store(reflect.TypeFor[T](), heldForm{def: s, fields: m})
}

// VerifyFieldMaps reports every declaration of [MapFields] whose mapping
// leaves a field of its definition struct without an exported Go field of
// the type, or names a Go field the type lacks.
func VerifyFieldMaps() error {
	var errs []error
	fieldMaps.Range(func(k, v any) bool {
		t, okT := k.(reflect.Type)
		hf, okF := v.(heldForm)
		if !okT || !okF {
			return true
		}
		for fi := range hf.def.Fields {
			f := &hf.def.Fields[fi]
			name, ok := hf.fields[f.Name]
			if !ok {
				errs = append(errs, fmt.Errorf("spec: %v maps no Go field to %s.%s", t, hf.def.Name, f.Name))
				continue
			}
			if sf, ok := t.FieldByName(name); !ok || !sf.IsExported() {
				errs = append(errs, fmt.Errorf("spec: %v has no exported field %s for %s.%s", t, name, hf.def.Name, f.Name))
			}
		}
		return true
	})
	sort.Slice(errs, func(a, b int) bool { return errs[a].Error() < errs[b].Error() })
	return errors.Join(errs...)
}

// DefinitionFormer is implemented by a hand-written Go type that carries a
// definition struct but cannot say, field by field, whether an optional
// field is present — a zero value stands for an absent one. The
// definition's checks read DefinitionForm, a value whose Go fields say so
// (the generated struct, a nil pointer for an absent field), in place of
// the type's own fields; without it a field left out would be judged as
// present with the value 0.
type DefinitionFormer interface {
	DefinitionForm() any
}

// mappedField returns the Go field of the struct value rv that holds the
// definition field name: through the type's [MapFields] declaration when
// it has one, else by the generated name; ok is false when rv's type
// declares a mapping that leaves name out.
func mappedField(rv reflect.Value, name string) (reflect.Value, bool) {
	if m, ok := mappedType(rv.Type()); ok {
		goName, ok := m[name]
		if !ok {
			return reflect.Value{}, false
		}
		return rv.FieldByName(goName), true
	}
	return rv.FieldByName(goFieldName(name)), true
}

// mappedType reports whether t carries a [MapFields] declaration.
func mappedType(t reflect.Type) (FieldMap, bool) {
	v, ok := fieldMaps.Load(t)
	if !ok {
		return nil, false
	}
	hf, ok := v.(heldForm)
	return hf.fields, ok
}

// CheckWritten holds a controller's written value of attrID to the
// definition def under the feature selection features, before any server
// runs: its type, nullability and constraint (peers resolve the attributes
// a constraint names), an enum's supported values, a bitmap's defined bits,
// an "in" constraint and the conformance of a struct's fields — the checks
// [Instance.ValidateWrite] and [Server.MatterWrite] make, with the
// attribute's relaxation ([Attribute.Relaxed]). The FabricIndex field of a
// written struct is the accessing fabric's, which the server assigns
// (matter.js packages/protocol/src/action/server/AttributeWriteResponse.ts:466-472
// at db021da0 injects it before validation), and is not judged. Whether the
// attribute is served and writable is the dispatcher's to answer first; an
// attribute def does not define, or a value the definition did not decode
// (a raw [Node]), is not judged. A refusal is a [StatusError] with
// CONSTRAINT_ERROR.
func CheckWritten(def *Cluster, features, attrID uint32, value any, peers Peers) error {
	a := def.Attribute(attrID)
	if a == nil {
		return nil
	}
	if _, raw := value.(Node); raw {
		return nil
	}
	i := &Instance{def: def, features: features}
	t, c := held(a.Type, a.Constraint, a.Relaxed)
	v := underlying(value)
	if _, err := i.checkValueIn(a.Name, t, a.Quality.Nullable, c, v, peers, nameScope{peers: peers, written: true}); err != nil {
		return err
	}
	if c.In != "" && v != nil && !i.inPeer(c.In, v, peers) {
		return Errorf(im.StatusConstraintError, "%s: %s %v is not one of the values allowed by %q", def.Name, a.Name, v, a.Constraint.Text)
	}
	if v == nil {
		return nil
	}
	_, err := i.checkValueIn(a.Name, t, a.Quality.Nullable, c, v, peers, nameScope{peers: peers, conformance: true, enums: true, written: true})
	return err
}

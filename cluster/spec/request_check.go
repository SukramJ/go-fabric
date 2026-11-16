// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package spec

import (
	"context"
	"fmt"
	"log/slog"
	"reflect"
	"slices"

	"github.com/SukramJ/go-fabric/im"
)

// CheckRequest holds a decoded request payload of the request command
// cmdID to the definition's Fields: each field's type, constraint — peers
// resolve the attributes a constraint names, and a constraint that lists
// an enum's members ("add, modify") admits those members only — and enum
// value (defined, and its member's conformance allowing it), and every
// field's conformance (one the conformance requires left out, one it does
// not allow present, a choice group's count). A refused request is
// CONSTRAINT_ERROR ([ErrConformance], [ErrEnumValueConformance],
// [ErrUnknownEnumValue] where they apply).
//
// A nil payload, a command the definition does not state, and a payload
// whose Go type does not carry every field the definition states (a
// hand-written decoder's own form) are not judged. It is the request's
// twin of the response check [Server.MatterInvoke] applies.
func (i *Instance) CheckRequest(cmdID uint32, fields any, peers Peers) error {
	cmd, rv, ok := i.requestOf(cmdID, fields)
	if !ok {
		return nil
	}
	scope := nameScope{peers: peers, conformance: true, enums: true}
	s := &Struct{Name: cmd.Name, Fields: cmd.Fields}
	if err := i.checkFieldConformance(i.def.Name+": "+cmd.Name, s, rv, scope); err != nil {
		return err
	}
	return i.checkRequestFields(cmd, rv, peers, scope)
}

// requestOf is the request command cmdID and its payload as a struct
// value carrying every field the command states; ok is false for a
// payload [Instance.CheckRequest] does not judge.
func (i *Instance) requestOf(cmdID uint32, fields any) (*Command, reflect.Value, bool) {
	cmd := i.def.Command(cmdID, Request)
	if cmd == nil || fields == nil || len(cmd.Fields) == 0 {
		return nil, reflect.Value{}, false
	}
	if d, ok := fields.(DefinitionFormer); ok {
		fields = d.DefinitionForm()
	}
	rv := reflect.ValueOf(fields)
	for rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return nil, reflect.Value{}, false
		}
		rv = rv.Elem()
	}
	if rv.Kind() != reflect.Struct {
		return nil, reflect.Value{}, false
	}
	for fi := range cmd.Fields {
		if fv, mapped := mappedField(rv, cmd.Fields[fi].Name); mapped && !fv.IsValid() {
			return nil, reflect.Value{}, false
		}
	}
	return cmd, rv, true
}

// checkRequestFields holds each field of the request rv to its type and
// constraint under scope; peers resolve a constraint naming an attribute.
func (i *Instance) checkRequestFields(cmd *Command, rv reflect.Value, peers Peers, scope nameScope) error {
	name := cmd.Name
	for fi := range cmd.Fields {
		f := &cmd.Fields[fi]
		fv, mapped := mappedField(rv, f.Name)
		if !mapped {
			continue
		}
		if fv.Kind() == reflect.Pointer {
			if fv.IsNil() {
				continue
			}
			fv = fv.Elem()
		}
		t, c := held(f.Type, f.Constraint, f.Relaxed)
		v := entryValue(fv.Interface())
		if _, err := i.checkValueIn(name+"."+f.Name, t, f.Quality.Nullable, c, v, peers, scope); err != nil {
			return err
		}
		if v == nil {
			continue
		}
		if n, ok := asUint(v); ok && !enumListAdmits(t, c, n) {
			return Errorf(im.StatusConstraintError, "%s: %s.%s %d is not one of %q", i.def.Name, name, f.Name, n, c.Text)
		}
	}
	return nil
}

// enumListAdmits reports whether a constraint that lists members of the
// enum t by name ("add, modify") admits v; a constraint that is no such
// list admits every value (the constraint check judged it already).
func enumListAdmits(t Type, c Constraint, v uint64) bool {
	if t.Kind != KindEnum || t.Enum == nil {
		return true
	}
	parts := c.Parts
	if len(parts) == 0 {
		parts = []Constraint{c}
	}
	var members []uint64
	for _, p := range parts {
		if p.Value == nil || p.Value.Ref == "" || p.Min != nil || p.Max != nil {
			return true
		}
		found := false
		for mi := range t.Enum.Values {
			if m := &t.Enum.Values[mi]; sameName(m.Name, p.Value.Ref) {
				members, found = append(members, m.Value), true
				break
			}
		}
		if !found {
			return true
		}
	}
	return slices.Contains(members, v)
}

// admitRequest is the request gate of [Server.MatterInvoke] and
// [InvokeHeld], run before any handler — the server's own or a host's. A
// command whose [RequestCheck] is Unchecked is not judged. A request
// [Instance.CheckRequest] refuses is refused with the command's stated
// [RequestCheck.Refusal]; for a command that states none the failure is
// logged and the command runs. The request's types and constraints are
// its decoder's to refuse, before the server runs: a generated
// definition's decoder holds them (decision 64), a hand-written one holds
// what its author's decoder holds, and a hand-written definition states a
// Refusal for a command whose requests the gate is to refuse.
func (i *Instance) admitRequest(ctx context.Context, cmdID uint32, fields any, peers Peers) error {
	cmd := i.def.Command(cmdID, Request)
	if cmd == nil || cmd.Check.Unchecked {
		return nil
	}
	err := i.CheckRequest(cmdID, fields, peers)
	if err == nil {
		return nil
	}
	if cmd.Check.Refusal != 0 {
		return Errorf(cmd.Check.Refusal, "%s: %s: %w", i.def.Name, cmd.Name, err)
	}
	logger().WarnContext(ctx, "spec: the request does not fit its definition; the command runs",
		"cluster", i.def.Name, "command", cmd.Name, "error", fmt.Sprint(err))
	return nil
}

// logger is the package's logger: the process default. A definition has
// no configuration of its own to carry one.
func logger() *slog.Logger { return slog.Default() }

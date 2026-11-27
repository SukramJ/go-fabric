// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package spec

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"slices"

	"thingfabric.dev/go-fabric/im"
)

// CheckRequest holds a decoded request payload of the request command
// cmdID to the definition's Fields: each field's conformance (one the
// conformance requires left out, one it does not allow present, a choice
// group's count) and its member of an enum the conformance does not
// allow, then its type, constraint — peers resolve the attributes a
// constraint names, and a constraint that lists an enum's members ("add,
// modify") admits those members only — and an enum value the enum does
// not define, field by field. A refused request is CONSTRAINT_ERROR
// ([ErrConformance], [ErrEnumValueConformance], [ErrUnknownEnumValue]
// where they apply); the first error is returned.
//
// A nil payload is judged as a request without fields: a mandatory field
// is missing (matter.js validates `data ?? {}`,
// packages/node/src/node/integration/ProtocolService.ts:624-625 at
// db91886c). A command the definition does not state, a command without
// fields, and a payload whose Go type does not carry every field the
// definition states (a hand-written decoder's own form) are not judged.
// It is the request's twin of the response check [Server.MatterInvoke]
// applies.
func (i *Instance) CheckRequest(cmdID uint32, fields any, peers Peers) error {
	if errs := i.requestErrors(cmdID, fields, peers, false); len(errs) > 0 {
		return errs[0]
	}
	return nil
}

// requestErrors are the errors [Instance.CheckRequest] finds in the
// request: with all, every one of every field, in the order matter.js
// records them (matter.js
// packages/node/src/behavior/state/validation/ValueValidator.ts:375-509 at
// db91886c, under a ValidationLocation.errors collector); else the first
// alone.
func (i *Instance) requestErrors(cmdID uint32, fields any, peers Peers, all bool) []error {
	cmd, rv, ok := i.requestOf(cmdID, fields)
	if !ok {
		return nil
	}
	var errs []error
	scope := nameScope{peers: peers, conformance: true, enums: true}
	if all {
		scope.errs = &errs
	}
	s := &Struct{Name: cmd.Name, Fields: cmd.Fields}
	value := func(f *Field) error { return i.checkRequestField(cmd.Name, f, rv, peers, scope) }
	if err := i.checkFields(i.def.Name+": "+cmd.Name, s, rv, scope, value); err != nil {
		errs = append(errs, err)
	}
	return errs
}

// requestOf is the request command cmdID and its payload as a struct
// value carrying every field the command states — an empty struct for a
// nil payload; ok is false for a payload [Instance.CheckRequest] does not
// judge: a nil pointer, and a payload in neither the definition's
// generated form nor a declared one ([payloadValue]; a hand-read request
// reaches the gate in its generated form, [HandForm]).
func (i *Instance) requestOf(cmdID uint32, fields any) (*Command, reflect.Value, bool) {
	cmd := i.def.Command(cmdID, Request)
	if cmd == nil || len(cmd.Fields) == 0 {
		return nil, reflect.Value{}, false
	}
	rv, ok := payloadValue(cmd, fields, false)
	if !ok {
		return nil, reflect.Value{}, false
	}
	return cmd, rv, true
}

// checkRequestField holds the field f of the request rv of the command
// name to its type and constraint under scope; peers resolve a constraint
// naming an attribute. A field the payload does not carry — every field
// of a nil payload's empty struct — is not judged.
func (i *Instance) checkRequestField(name string, f *Field, rv reflect.Value, peers Peers, scope nameScope) error {
	fv, mapped := mappedField(rv, f.Name)
	if !mapped || !fv.IsValid() {
		return nil
	}
	if fv.Kind() == reflect.Pointer {
		if fv.IsNil() {
			return nil
		}
		fv = fv.Elem()
	}
	t, c := held(f.Type, f.Constraint, f.Relaxed)
	v := entryValue(fv.Interface())
	if _, err := i.checkValueIn(name+"."+f.Name, t, f.Quality.Nullable, c, v, peers, scope); err != nil {
		return err
	}
	if v == nil {
		return nil
	}
	if n, ok := asUint(v); ok && !enumListAdmits(t, c, n) {
		return Errorf(im.StatusConstraintError, "%s: %s.%s %d is not one of %q", i.def.Name, name, f.Name, n, c.Text)
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

// AdmitRequest holds a controller's request fields of the request command
// cmdID to the definition def under the feature selection features,
// before any server runs — the request's twin of [CheckWritten], and the
// one request gate of the module: the endpoint dispatcher runs it on every
// command that reaches a server over the wire, with the FeatureMap and
// the attributes (peers) read from that server. A direct call of a
// server's MatterInvoke, [InvokeHeld] or [Hooks.Invoke] is a local call
// and is not validated, as matter.js validates only the commands its
// interaction layer invokes. The decision is [Instance]'s gate: a command
// that states a [RequestCheck.Refusal] answers the first error with it;
// else a value an enum does not define anywhere in the request is
// CONSTRAINT_ERROR ([ErrUnknownEnumValue]); else the errors are logged
// and nil is returned. A nil payload is judged as a request without
// fields; a payload not in the definition's generated form (a hand-written
// reader's) is not judged. nil for a nil def.
//
// Mirrors matter.js packages/node/src/node/integration/ProtocolService.ts:347-350
// at b25ed960 (the gate bound to each command of the per-endpoint cluster
// state, reached only from
// packages/protocol/src/action/server/CommandInvokeResponse.ts:316, :358) and
// :671-705 (invokeCommand's gate).
func AdmitRequest(ctx context.Context, def *Cluster, features, cmdID uint32, fields any, peers Peers) error {
	if def == nil {
		return nil
	}
	i := &Instance{def: def, features: features}
	return i.admitRequest(ctx, cmdID, fields, peers)
}

// admitRequest is the request gate [AdmitRequest] runs on every error
// [Instance.CheckRequest] finds in the request, not only
// the first: a command that states a [RequestCheck.Refusal] answers the
// first error with it; else a value an enum does not define anywhere in
// the request is refused with CONSTRAINT_ERROR ([ErrUnknownEnumValue]);
// else the errors are logged and the command runs. A command whose
// [RequestCheck] is Unchecked (deprecated) is not judged. The request's
// types and constraints are its decoder's to refuse, before the server
// runs: a generated definition's decoder holds them (decision 64), a
// hand-written one holds what its author's decoder holds, and a
// hand-written definition states a Refusal for a command whose requests
// the gate is to refuse.
//
// Mirrors matter.js packages/node/src/node/integration/ProtocolService.ts:671-705
// at b25ed960 (the gate in invokeCommand, :641-797: the errors collected :675-689, then
// onValidationError(errors[0]) :693-694, a thrown UnknownEnumValueError
// :692, :695-696, else a warning :697-702).
func (i *Instance) admitRequest(ctx context.Context, cmdID uint32, fields any, peers Peers) error {
	cmd := i.def.Command(cmdID, Request)
	if cmd == nil || cmd.Check.Unchecked {
		return nil
	}
	errs := i.requestErrors(cmdID, fields, peers, true)
	if len(errs) == 0 {
		return nil
	}
	if cmd.Check.Refusal != 0 {
		return Errorf(cmd.Check.Refusal, "%s: %s: %w", i.def.Name, cmd.Name, errs[0])
	}
	for _, err := range errs {
		if errors.Is(err, ErrUnknownEnumValue) {
			return err
		}
	}
	logger().WarnContext(ctx, "spec: request validation error; the command runs",
		"cluster", i.def.Name, "command", cmd.Name, "error", fmt.Sprint(errors.Join(errs...)))
	return nil
}

// logger is the package's logger: the process default. A definition has
// no configuration of its own to carry one.
func logger() *slog.Logger { return slog.Default() }

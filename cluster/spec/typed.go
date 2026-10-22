// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package spec

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
)

// The typed layer: script/clustergen generates, beside each cluster
// definition, a typed server (<cluster>def.Server, server_gen.go) over any
// contract.ClusterServer of the cluster — the generated [Server] or a
// self-held one — with a typed State, a Patch, typed command handlers and
// typed events. The helpers below are what that generated code calls; they
// add no semantics: a value still passes the server's own SetAttributes,
// MatterWrite, MatterInvoke and Emit as it does untyped.
//
// A generated State fills itself from the stored values
// ([StateReader]) and a generated Patch lists the values it sets
// ([PatchValuer]), each written per field by the generator: nothing here
// walks a State or a Patch by reflection.

// The optional interfaces a typed server reaches the server's own methods
// through. [*Server] implements all four.
type (
	// AttributeSetter is a server the host sets attributes of, all or
	// nothing ([Server.SetAttributes]).
	AttributeSetter interface {
		SetAttributes(values map[uint32]any) error
	}
	// CommandRegistrar is a server that takes command handlers: the
	// host's handler replaces the server's own handling of the command,
	// nil hands it back, and a command the server does not accept is
	// refused ([ErrNotAccepted]) — [Server.Handle], [HostHandlers].
	CommandRegistrar interface {
		Handle(cmdID uint32, fn CommandHandler) error
	}
	// EventEmitting is a server that emits its cluster's events
	// ([Server.Emit]).
	EventEmitting interface {
		Emit(endpoint uint16, eventID uint32, data any) error
	}
	// BeforeWriteSetter is a server that takes a write veto
	// ([Server.SetBeforeWrite]).
	BeforeWriteSetter interface {
		SetBeforeWrite(fn func(ctx context.Context, attrID uint32, value any) error)
	}
)

// The typed server's refusals for a server without the optional interface
// an operation needs.
var (
	// ErrNotSettable: the server is no [AttributeSetter].
	ErrNotSettable = errors.New("spec: the server does not set attributes (no SetAttributes)")
	// ErrNoHandlers: the server is no [CommandRegistrar].
	ErrNoHandlers = errors.New("spec: the server takes no command handlers (no Handle)")
	// ErrNoEvents: the server is no [EventEmitting].
	ErrNoEvents = errors.New("spec: the server emits no events (no Emit)")
	// ErrNoWriteVeto: the server is no [BeforeWriteSetter].
	ErrNoWriteVeto = errors.New("spec: the server takes no write veto (no SetBeforeWrite)")
)

// PatchValuer is a generated Patch: AttributeValues returns the
// attributes it sets in their stored form, generated per cluster
// (script/clustergen), so a list of a structure is its [List].
type PatchValuer interface {
	AttributeValues() map[uint32]any
}

// StateReader is a generated State: FromAttributeValues fills each field
// from the value read answers for its attribute, converted to the field's
// type ([Assign]), generated per cluster (script/clustergen) as
// [PatchValuer] is.
// fabric:reachable:reason="a constraint only: StateOf, OnChangedOf, OnChangedAsyncOf and OnWriteOf take it, and RTA records their instantiations, not the constraint interface"
type StateReader interface {
	FromAttributeValues(read func(attrID uint32) (any, bool))
}

// Assign converts the stored value v to T ([Convert]) and stores it in
// dst; false, with dst untouched, when v does not fit T. A value of type
// T itself is stored without conversion. A generated
// State.FromAttributeValues calls it per field.
// fabric:reachable:reason="generic: every generated server_gen.go calls an instantiation of it, and RTA records the instantiation, not the generic declaration"
func Assign[T any](dst *T, v any) bool {
	if t, ok := v.(T); ok {
		*dst = t
		return true
	}
	c, ok := convert(v, reflect.TypeFor[T]())
	if !ok {
		return false
	}
	t, ok := c.Interface().(T)
	if ok {
		*dst = t
	}
	return ok
}

// SetPatch sets the attributes a generated Patch sets
// ([PatchValuer.AttributeValues]) on srv, all or nothing as its
// SetAttributes sets them; [ErrNotSettable] for a server that is no
// [AttributeSetter].
func SetPatch(srv contract.ClusterServer, patch PatchValuer) error {
	setter, ok := srv.(AttributeSetter)
	if !ok {
		return fmt.Errorf("%w: %T", ErrNotSettable, srv)
	}
	return setter.SetAttributes(patch.AttributeValues())
}

// StateOf returns the generated State S of srv: each field the value srv
// reads for its attribute (MatterRead), converted to the field's type
// ([StateReader]). An attribute srv does not answer, or answers with a
// value the field's type cannot hold, leaves the field at its zero value.
// fabric:reachable:reason="generic: every generated server_gen.go and the typed tests call an instantiation of it, and RTA records the instantiation, not the generic declaration"
func StateOf[S any, P interface {
	*S
	StateReader
}](srv contract.ClusterServer) S {
	var st S
	P(&st).FromAttributeValues(srv.MatterRead)
	return st
}

// stateWith reads S from srv with value in place of attrID's.
func stateWith[S any, P interface {
	*S
	StateReader
}](srv contract.ClusterServer, attrID uint32, value any) S {
	var st S
	P(&st).FromAttributeValues(func(id uint32) (any, bool) {
		if id == attrID {
			return value, true
		}
		return srv.MatterRead(id)
	})
	return st
}

// OnChangedOf calls fn with the State S of srv and the change after every
// change srv reports, on the goroutine that made it and before the
// operation that made it returns: through its
// [contract.AttributeChangeNotifier] as it reports it, or through its
// [contract.ChangeNotifier] with every attribute it reports and
// [contract.OriginUnknown] — the switch of device.Device.OnChanged. It returns the unsubscribe function; a server
// that notifies nothing never calls fn.
// fabric:reachable:reason="generic: every generated server_gen.go and the typed tests call an instantiation of it, and RTA records the instantiation, not the generic declaration"
func OnChangedOf[S any, P interface {
	*S
	StateReader
}](srv contract.ClusterServer, fn func(state S, change contract.Change)) (unsubscribe func()) {
	if fn == nil {
		return func() {}
	}
	return ChangesOf(srv, func(c contract.Change) { fn(StateOf[S, P](srv), c) })
}

// ChangesOf calls fn with every change srv reports, inline: through its
// [contract.AttributeChangeNotifier] as it reports it, or through its
// [contract.ChangeNotifier] with every attribute it reports and
// [contract.OriginUnknown]. It returns the unsubscribe
// function; a server that notifies nothing never calls fn.
func ChangesOf(srv contract.ClusterServer, fn func(contract.Change)) (unsubscribe func()) {
	switch n := srv.(type) {
	case contract.AttributeChangeNotifier:
		return n.OnAttributeChange(fn)
	case contract.ChangeNotifier:
		return n.OnMatterValueChanged(func() {
			fn(contract.Change{Attributes: srv.MatterReportable(), Origin: contract.OriginUnknown})
		})
	default:
		return func() {}
	}
}

// OnChangedAsyncOf is [OnChangedOf] with fn queued on w's queue: the state
// is read and the change taken when the server reports it, and fn runs
// later, one call after the other, on the queue's goroutine. A wrapper
// without a queue ([NewWrapper]) delivers nothing — as a device that is
// not added delivers nothing (by_design
// BD-Matter-AsyncCallbacks-OnlyWhileAdded). It returns the unsubscribe
// function.
// fabric:reachable:reason="generic: every generated server_gen.go and the typed tests call an instantiation of it, and RTA records the instantiation, not the generic declaration"
func OnChangedAsyncOf[S any, P interface {
	*S
	StateReader
}](w Wrapper, fn func(state S, change contract.Change)) (unsubscribe func()) {
	if w.queue == nil || fn == nil {
		return func() {}
	}
	q := w.queue
	return OnChangedOf[S, P](w.srv, func(state S, change contract.Change) {
		q.Enqueue(func() { fn(state, change) })
	})
}

// OnWriteOf installs a typed write veto on srv ([ServerConfig.BeforeWrite]):
// fn receives the State S of srv with the written value in place of the
// attribute's, and the attribute id; nil removes the veto.
// [ErrNoWriteVeto] for a server that is no [BeforeWriteSetter].
// fabric:reachable:reason="generic: every generated server_gen.go and the typed tests call an instantiation of it, and RTA records the instantiation, not the generic declaration"
func OnWriteOf[S any, P interface {
	*S
	StateReader
}](srv contract.ClusterServer, fn func(ctx context.Context, next S, attrID uint32) error) error {
	setter, ok := srv.(BeforeWriteSetter)
	if !ok {
		return fmt.Errorf("%w: %T", ErrNoWriteVeto, srv)
	}
	if fn == nil {
		setter.SetBeforeWrite(nil)
		return nil
	}
	setter.SetBeforeWrite(func(ctx context.Context, attrID uint32, value any) error {
		return fn(ctx, stateWith[S, P](srv, attrID, value), attrID)
	})
	return nil
}

// HandleOn registers a typed handler for the request command cmdID on srv
// ([HandlerOf]), replacing the server's own handling of it; nil hands the
// command back to the server. [ErrNoHandlers] for a server that is no
// [CommandRegistrar], the server's refusal ([ErrNotAccepted]) for a
// command it does not accept.
// fabric:reachable:reason="generic: every generated server_gen.go and the typed tests call an instantiation of it, and RTA records the instantiation, not the generic declaration"
func HandleOn[Req, Resp any](srv contract.ClusterServer, cmdID uint32, fn func(ctx context.Context, req Req) (Resp, error)) error {
	reg, ok := srv.(CommandRegistrar)
	if !ok {
		return fmt.Errorf("%w: %T", ErrNoHandlers, srv)
	}
	return reg.Handle(cmdID, HandlerOf(fn))
}

// EmitOn emits the event eventID with data on endpoint through srv's own
// Emit; [ErrNoEvents] for a server that is no [EventEmitting].
func EmitOn(srv contract.ClusterServer, endpoint uint16, eventID uint32, data any) error {
	em, ok := srv.(EventEmitting)
	if !ok {
		return fmt.Errorf("%w: %T", ErrNoEvents, srv)
	}
	return em.Emit(endpoint, eventID, data)
}

// HandlerOf adapts a typed command handler to [CommandHandler]: the
// decoded request payload is asserted to Req (a payload of another type is
// INVALID_COMMAND), and a response of type struct{} — a status-only
// command — answers no payload. nil adapts to nil.
// fabric:reachable:reason="generic: every generated server_gen.go and the typed tests call an instantiation of it, and RTA records the instantiation, not the generic declaration"
func HandlerOf[Req, Resp any](fn func(ctx context.Context, req Req) (Resp, error)) CommandHandler {
	if fn == nil {
		return nil
	}
	return func(ctx context.Context, fields any) (any, error) {
		req, ok := fields.(Req)
		if !ok {
			if p, isPtr := fields.(*Req); isPtr && p != nil {
				req, ok = *p, true
			} else if fields == nil && emptyStruct(reflect.TypeFor[Req]()) {
				ok = true
			}
		}
		if !ok {
			return nil, Errorf(im.StatusInvalidCommand, "spec: request payload %T, want %T", fields, req)
		}
		resp, err := fn(ctx, req)
		if err != nil {
			return nil, err
		}
		if _, status := any(resp).(struct{}); status {
			return nil, nil
		}
		return resp, nil
	}
}

func emptyStruct(t reflect.Type) bool { return t.Kind() == reflect.Struct && t.NumField() == 0 }

// nullableType is the interface every [Nullable] implements.
var nullableType = reflect.TypeFor[nullable]()

// scalarClass groups the kinds convert converts between: bool, the
// numbers, string; 0 for every other kind.
func scalarClass(k reflect.Kind) int {
	switch k {
	case reflect.Bool:
		return 1
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return 2
	case reflect.String:
		return 3
	default:
		return 0
	}
}

// Convert is the conversion the typed form reads every field through
// ([StateOf]): v as a value of type t ([convert]). The device kit fills a
// server's configuration from a declaration's Initial with it.
func Convert(v any, t reflect.Type) (reflect.Value, bool) { return convert(v, t) }

// convert turns a stored value into a value of the generated type t: a
// width integer into its enum or bitmap type, a list entry by entry, nil
// into a nil pointer or slice or a null [Nullable], a value into a
// pointer to it, and a struct of another type — a self-held server's
// cluster/wire struct — field by field by name ([convertStruct]). false
// when v does not fit t.
func convert(v any, t reflect.Type) (reflect.Value, bool) {
	if v == nil {
		switch {
		case t.Kind() == reflect.Pointer, t.Kind() == reflect.Slice, t.Kind() == reflect.Interface:
			return reflect.Zero(t), true
		case t.Implements(nullableType):
			out := reflect.New(t).Elem()
			out.FieldByName("Null").SetBool(true)
			return out, true
		default:
			return reflect.Value{}, false
		}
	}
	src := reflect.ValueOf(v)
	if src.Type().AssignableTo(t) {
		if t.Kind() != reflect.Interface {
			src = src.Convert(t) // a spec.List as the []T it is assigned to
		}
		return src, true
	}
	if src.Kind() == reflect.Pointer {
		if src.IsNil() {
			return convert(nil, t)
		}
		return convert(src.Elem().Interface(), t)
	}
	if t.Kind() == reflect.Pointer {
		e, ok := convert(v, t.Elem())
		if !ok {
			return reflect.Value{}, false
		}
		p := reflect.New(t.Elem())
		p.Elem().Set(e)
		return p, true
	}
	if t.Implements(nullableType) {
		if n, ok := v.(nullable); ok {
			x, isNull := n.nullValue()
			if isNull {
				return convert(nil, t)
			}
			v = x
		}
		e, ok := convert(v, t.Field(0).Type)
		if !ok {
			return reflect.Value{}, false
		}
		out := reflect.New(t).Elem()
		out.Field(0).Set(e)
		return out, true
	}
	switch {
	case t.Kind() == reflect.Slice && (src.Kind() == reflect.Slice || src.Kind() == reflect.Array):
		out := reflect.MakeSlice(t, src.Len(), src.Len())
		for i := range src.Len() {
			e, ok := convert(src.Index(i).Interface(), t.Elem())
			if !ok {
				return reflect.Value{}, false
			}
			out.Index(i).Set(e)
		}
		return out, true
	case scalarClass(t.Kind()) != 0 && scalarClass(t.Kind()) == scalarClass(src.Kind()):
		return src.Convert(t), true
	case t.Kind() == reflect.Struct && src.Type().ConvertibleTo(t):
		return src.Convert(t), true
	case t.Kind() == reflect.Struct && src.Kind() == reflect.Struct:
		return convertStruct(src, t)
	default:
		return reflect.Value{}, false
	}
}

// convertStruct fills a t from the struct src field by field: each
// exported field of t takes the field of src with its name, converted —
// the name compared without case, as the generated types spell an id
// "Id" where cluster/wire spells it "ID". A
// field src does not have stays zero; an optional field (a pointer in t)
// whose value in src is the zero value of a plain type stays nil, as a
// wire struct leaves out an empty optional field. false when no field
// matches or a matching one does not fit.
func convertStruct(src reflect.Value, t reflect.Type) (reflect.Value, bool) {
	out := reflect.New(t).Elem()
	matched := false
	for i := range t.NumField() {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		sf, ok := src.Type().FieldByNameFunc(func(n string) bool { return strings.EqualFold(n, f.Name) })
		if !ok || !sf.IsExported() {
			continue
		}
		sv := src.FieldByIndex(sf.Index)
		matched = true
		if f.Type.Kind() == reflect.Pointer && sv.Kind() != reflect.Pointer && sv.IsZero() {
			continue
		}
		c, ok := convert(sv.Interface(), f.Type)
		if !ok {
			return reflect.Value{}, false
		}
		out.Field(i).Set(c)
	}
	return out, matched
}

// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package spec

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"sync"

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
// A generated State or Patch names the attribute of each field in its
// `matter` tag (the attribute id, "0x0000"); a Patch states the nullable
// attributes it sets to null in a []uint32 field named Null.

// The optional interfaces a typed server reaches the server's own methods
// through. [*Server] implements all four.
type (
	// AttributeSetter is a server the host sets attributes of, all or
	// nothing ([Server.SetAttributes]).
	AttributeSetter interface {
		SetAttributes(values map[uint32]any) error
	}
	// CommandRegistrar is a server that takes command handlers
	// ([Server.Handle]).
	CommandRegistrar interface {
		Handle(cmdID uint32, fn CommandHandler)
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

// typedField is one attribute field of a generated State or Patch.
type typedField struct {
	index int
	attr  uint32
}

// typedFields caches the attribute fields of each State and Patch type.
var typedFields sync.Map // reflect.Type → []typedField

// fieldsOf returns the attribute fields of the struct type t, by its
// `matter` tags. A malformed tag is a generator defect and panics.
func fieldsOf(t reflect.Type) []typedField {
	if v, ok := typedFields.Load(t); ok {
		fs, _ := v.([]typedField)
		return fs
	}
	var out []typedField
	for i := range t.NumField() {
		tag, ok := t.Field(i).Tag.Lookup("matter")
		if !ok {
			continue
		}
		id, err := strconv.ParseUint(tag, 0, 32)
		if err != nil {
			panic(fmt.Sprintf("spec: %s.%s: matter tag %q: %v", t, t.Field(i).Name, tag, err))
		}
		out = append(out, typedField{index: i, attr: uint32(id)})
	}
	typedFields.Store(t, out)
	return out
}

// PatchValues returns the attribute values a generated Patch sets, for
// [Server.SetAttributes]: each non-nil field's value, and nil for every id
// its Null field lists. The patch is the struct or a pointer to it.
func PatchValues(patch any) map[uint32]any {
	rv := reflect.Indirect(reflect.ValueOf(patch))
	out := map[uint32]any{}
	if rv.Kind() != reflect.Struct {
		return out
	}
	for _, f := range fieldsOf(rv.Type()) {
		if fv := rv.Field(f.index); !fv.IsNil() {
			out[f.attr] = fv.Elem().Interface()
		}
	}
	if null := rv.FieldByName("Null"); null.IsValid() {
		ids, _ := null.Interface().([]uint32)
		for _, id := range ids {
			out[id] = nil
		}
	}
	return out
}

// SetPatch sets the attributes a generated Patch sets ([PatchValues]) on
// srv, all or nothing as its SetAttributes sets them; [ErrNotSettable] for
// a server that is no [AttributeSetter].
func SetPatch(srv contract.ClusterServer, patch any) error {
	setter, ok := srv.(AttributeSetter)
	if !ok {
		return fmt.Errorf("%w: %T", ErrNotSettable, srv)
	}
	return setter.SetAttributes(PatchValues(patch))
}

// StateOf returns the generated State S of srv: each field the value srv
// reads for its attribute (MatterRead), converted to the field's type. An
// attribute srv does not answer, or answers with a value the field's type
// cannot hold, leaves the field at its zero value.
// fabric:reachable:reason="generic: every generated server_gen.go and the typed tests call an instantiation of it, and RTA records the instantiation, not the generic declaration"
func StateOf[S any](srv contract.ClusterServer) S {
	return stateOf[S](srv, 0, nil, false)
}

// stateOf reads S from srv, with value in place of attrID's when with.
func stateOf[S any](srv contract.ClusterServer, attrID uint32, value any, with bool) S {
	var st S
	rv := reflect.ValueOf(&st).Elem()
	if rv.Kind() != reflect.Struct {
		return st
	}
	for _, f := range fieldsOf(rv.Type()) {
		v, ok := value, with && f.attr == attrID
		if !ok {
			v, ok = srv.MatterRead(f.attr)
		}
		if !ok {
			continue
		}
		if c, ok := convert(v, rv.Field(f.index).Type()); ok {
			rv.Field(f.index).Set(c)
		}
	}
	return st
}

// OnChangedOf calls fn with the State S of srv and the changed attribute
// ids after every change srv reports: through its
// [contract.AttributeChangeNotifier], or through its
// [contract.ChangeNotifier] with every attribute it reports — the switch
// of device.Device.OnChanged. It returns the unsubscribe function; a
// server that notifies nothing never calls fn.
// fabric:reachable:reason="generic: every generated server_gen.go and the typed tests call an instantiation of it, and RTA records the instantiation, not the generic declaration"
func OnChangedOf[S any](srv contract.ClusterServer, fn func(state S, changed []uint32)) (unsubscribe func()) {
	if fn == nil {
		return func() {}
	}
	switch n := srv.(type) {
	case contract.AttributeChangeNotifier:
		return n.OnMatterAttributesChanged(func(ids []uint32) { fn(StateOf[S](srv), ids) })
	case contract.ChangeNotifier:
		return n.OnMatterValueChanged(func() { fn(StateOf[S](srv), srv.MatterReportable()) })
	default:
		return func() {}
	}
}

// OnWriteOf installs a typed write veto on srv ([ServerConfig.BeforeWrite]):
// fn receives the State S of srv with the written value in place of the
// attribute's, and the attribute id; nil removes the veto.
// [ErrNoWriteVeto] for a server that is no [BeforeWriteSetter].
// fabric:reachable:reason="generic: every generated server_gen.go and the typed tests call an instantiation of it, and RTA records the instantiation, not the generic declaration"
func OnWriteOf[S any](srv contract.ClusterServer, fn func(ctx context.Context, next S, attrID uint32) error) error {
	setter, ok := srv.(BeforeWriteSetter)
	if !ok {
		return fmt.Errorf("%w: %T", ErrNoWriteVeto, srv)
	}
	if fn == nil {
		setter.SetBeforeWrite(nil)
		return nil
	}
	setter.SetBeforeWrite(func(ctx context.Context, attrID uint32, value any) error {
		return fn(ctx, stateOf[S](srv, attrID, value, true), attrID)
	})
	return nil
}

// HandleOn registers a typed handler for the request command cmdID on srv
// ([HandlerOf]); nil removes the handler. [ErrNoHandlers] for a server
// that is no [CommandRegistrar].
// fabric:reachable:reason="generic: every generated server_gen.go and the typed tests call an instantiation of it, and RTA records the instantiation, not the generic declaration"
func HandleOn[Req, Resp any](srv contract.ClusterServer, cmdID uint32, fn func(ctx context.Context, req Req) (Resp, error)) error {
	reg, ok := srv.(CommandRegistrar)
	if !ok {
		return fmt.Errorf("%w: %T", ErrNoHandlers, srv)
	}
	reg.Handle(cmdID, HandlerOf(fn))
	return nil
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

// vetoError is a [ServerConfig.BeforeWrite] refusal that carries no IM
// status of its own: it answers FAILURE and unwraps to the refusal.
type vetoError struct{ err error }

func (e *vetoError) Error() string                   { return e.err.Error() }
func (e *vetoError) Unwrap() error                   { return e.err }
func (e *vetoError) MatterStatusCode() im.StatusCode { return im.StatusFailure }

// vetoed returns a write veto's error as the write's answer: as given
// when it carries an IM status, FAILURE otherwise — the dispatcher reads
// a plain error's text for a status (endpoint/dispatcher.go
// writeErrorStatus), and a host's message is not a status.
func vetoed(err error) error {
	if _, ok := errors.AsType[im.StatusCodeError](err); ok {
		return err
	}
	return &vetoError{err: err}
}

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

// convert turns a stored value into a value of the generated type t: a
// width integer into its enum or bitmap type, a list entry by entry, nil
// into a nil pointer or slice or a null [Nullable], a value into a
// pointer to it. false when v does not fit t.
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
	default:
		return reflect.Value{}, false
	}
}

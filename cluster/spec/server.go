// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package spec

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"sync"

	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
)

// Server errors; each error [NewServer], [Server.Set] or [Server.Emit]
// returns wraps one of these (or one of [New]'s).
var (
	// ErrFabricScoped: the definition has a fabric-scoped attribute
	// (access "F"). Its per-fabric reads are contract.FabricScopedReader's,
	// which the generated server does not implement.
	ErrFabricScoped = errors.New("spec: fabric-scoped attributes are not supported by the generated server")
	// ErrNotServed: a value for an attribute the instance does not serve.
	ErrNotServed = errors.New("spec: attribute not served")
	// ErrInvalidValue: a value outside the attribute's type or constraint.
	ErrInvalidValue = errors.New("spec: value outside the attribute's type or constraint")
	// ErrNotEmitted: an event the instance's EventList does not hold.
	ErrNotEmitted = errors.New("spec: event not in EventList")
	// ErrMissingHandler: a command the conformance makes mandatory for
	// the feature selection has no handler in [ServerConfig.Handlers].
	ErrMissingHandler = errors.New("spec: mandatory command has no handler")
)

// Source is the optional read port for attributes the device owns: a
// value it answers takes precedence over the server's stored one.
type Source interface {
	MatterAttribute(attrID uint32) (any, bool)
}

// Sink is the optional write port: told about every controller write that
// passed the definition's checks, before the server stores it; an error
// refuses the write and leaves the stored value as it was.
//
// value is the stored form described on [Server] — the Go type of the
// attribute's width (a uint32 for an epoch-s, a uint8 for an enum8), nil
// for null, a struct or list as decoded — not the uint64 / int64 /
// float64 [Instance.ValidateWrite] normalises to.
type Sink interface {
	MatterWriteAttribute(ctx context.Context, attrID uint32, value any) error
}

// CommandHandler carries out one accepted command. fields is the decoded
// request payload; the result is the response payload, or nil for a
// status-only command.
type CommandHandler func(ctx context.Context, fields any) (any, error)

// ServerConfig carries what a [Server] needs beyond the definition and
// the feature selection.
type ServerConfig struct {
	// DataVersion is an optional host-owned tracker; the server keeps its
	// own otherwise.
	DataVersion *cluster.DataVersionTracker
	// Source optionally answers reads ahead of the stored values.
	Source Source
	// Sink is optionally told about controller writes.
	Sink Sink
	// Initial holds the values the server starts with, checked as
	// [Server.Set] checks a value.
	Initial map[uint32]any
	// Handlers holds the command handlers, by request command id. Every
	// command the conformance makes mandatory for the feature selection
	// must have one, or construction fails with [ErrMissingHandler];
	// an optional command's handler may also be registered later with
	// [Server.Handle].
	Handlers map[uint32]CommandHandler
	// Unimplemented marks every accepted command Handlers leaves out as
	// the generated behavior's unimplemented one (matter.js
	// Behavior.unimplemented): construction does not fail on a mandatory
	// one, and the command stays out of AcceptedCommandList (and its
	// response out of GeneratedCommandList) until [Server.Handle]
	// registers it — matter.js's non-strict validation
	// (ValidatedElements.ts:106-108, :341-343, :361-368 at 0ceb202d). The
	// device layer's generated default server is such a stub.
	Unimplemented bool
}

// Server is a complete [contract.ClusterServer] for a cluster whose
// server matter.js derives from the model alone — the case of
// packages/node/src/behaviors/fixed-label/FixedLabelServer.ts
// (`export class FixedLabelServer extends FixedLabelBehavior {}`): the
// element lists come from the definition and the feature selection
// (packages/node/src/behavior/cluster/ValidatedElements.ts, the embedded
// [Instance]), a value — written by a controller or set by the host — is
// checked against the model, and the state is the attribute values,
// starting at the model's defaults. It stores them, dispatches commands
// to registered handlers, emits events with the definition's priority and
// reports changes with a data-version bump and an attribute-change
// notification.
//
// Values are kept in the Go type of the attribute's width: uint8 to
// uint64 for an unsigned integer, enum or bitmap, int8 to int64 for a
// signed one, float32 or float64, bool, string, []byte; nil for null.
// Struct and list values are kept as given and must not be changed after
// they are handed over.
type Server struct {
	*Instance
	cluster.AttributeChanges

	embedded cluster.DataVersionTracker
	ext      *cluster.DataVersionTracker
	source   Source
	sink     Sink

	mu       sync.Mutex
	values   map[uint32]any
	handlers map[uint32]CommandHandler
	// stubs are the accepted commands construction found unimplemented
	// ([ServerConfig.Unimplemented]); one is listed once it has a handler.
	stubs   []uint32
	emitter contract.EventEmitter
}

// Compile-time assertions.
var (
	_ contract.ClusterServer                  = (*Server)(nil)
	_ contract.ClusterDataVersion             = (*Server)(nil)
	_ contract.ClusterAttributeLister         = (*Server)(nil)
	_ contract.ClusterCommandLister           = (*Server)(nil)
	_ contract.ClusterEventLister             = (*Server)(nil)
	_ contract.ClusterAttributeReadPrivilege  = (*Server)(nil)
	_ contract.ClusterAttributeWritePrivilege = (*Server)(nil)
	_ contract.ClusterCommandInvokePrivilege  = (*Server)(nil)
	_ contract.TimedCommander                 = (*Server)(nil)
	_ contract.AttributeChangeNotifier        = (*Server)(nil)
	_ contract.EventReceiver                  = (*Server)(nil)
)

// NewServer binds def to opts as [New] does and returns a server holding
// cfg.Initial; a served attribute cfg.Initial leaves out starts at the
// definition's default, and is absent when the definition states none.
// A selection that serves a fabric-scoped attribute is refused
// ([ErrFabricScoped]); an unserved one does not matter.
func NewServer(def *Cluster, opts Options, cfg ServerConfig) (*Server, error) {
	inst, err := New(def, opts)
	if err != nil {
		return nil, err
	}
	for _, id := range inst.attrs {
		if a := def.Attribute(id); a.Access.Fabric == "F" {
			return nil, fmt.Errorf("%w: %s attribute %s", ErrFabricScoped, def.Name, a.Name)
		}
	}
	s := &Server{
		Instance: inst,
		ext:      cfg.DataVersion,
		source:   cfg.Source,
		sink:     cfg.Sink,
		values:   map[uint32]any{},
		handlers: map[uint32]CommandHandler{},
	}
	// A command the conformance requires is fatal at construction when it
	// has no implementation. Mirrors matter.js
	// packages/node/src/behavior/cluster/ValidatedElements.ts:331-333,357-370
	// at 0ceb202d (requireCommand, #validateRequiredCommands: "Implementation
	// missing", fatal) — rather than advertising it in AcceptedCommandList
	// and answering UNSUPPORTED_COMMAND at invoke.
	for id, fn := range cfg.Handlers {
		if fn != nil {
			s.handlers[id] = fn
		}
	}
	for _, id := range inst.accepted {
		switch {
		case s.handlers[id] != nil:
		case cfg.Unimplemented:
			s.stubs = append(s.stubs, id)
		case slices.Contains(inst.required, id):
			return nil, fmt.Errorf("%w: %s command %s (0x%02X)", ErrMissingHandler, def.Name, def.Command(id, Request).Name, id)
		}
	}
	for _, id := range inst.attrs {
		if v, ok := defaultValue(def.Attribute(id)); ok {
			s.values[id] = v
		}
	}
	for _, id := range slices.Sorted(maps.Keys(cfg.Initial)) {
		v, err := s.check(id, cfg.Initial[id])
		if err != nil {
			return nil, err
		}
		s.values[id] = v
	}
	// An "in" constraint names a sibling, so it is judged once every
	// initial value is in place, not in id order — matter.js validates the
	// whole state after the initializer has run. Mirrors matter.js
	// packages/node/src/behavior/internal/ServerBehaviorBacking.ts:60-63
	// (invokeInitializer: finalizeState → datasource.validate).
	for _, id := range inst.attrs {
		if err := s.checkIn(id, s.values[id], s.peers); err != nil {
			return nil, fmt.Errorf("%w: %w", ErrInvalidValue, err)
		}
	}
	return s, nil
}

// checkIn holds v to attrID's "in" constraint, if it states one; a null or
// absent value is not judged.
func (s *Server) checkIn(attrID uint32, v any, peers Peers) error {
	a := s.def.Attribute(attrID)
	if a == nil || a.Constraint.In == "" || v == nil {
		return nil
	}
	if !s.inPeer(a.Constraint.In, v, peers) {
		return Errorf(im.StatusConstraintError, "%s: %s %v is not one of the values allowed by %q", s.def.Name, a.Name, v, a.Constraint.Text)
	}
	return nil
}

func (s *Server) tracker() *cluster.DataVersionTracker {
	if s.ext != nil {
		return s.ext
	}
	return &s.embedded
}

// MatterDataVersion implements [contract.ClusterDataVersion].
func (s *Server) MatterDataVersion() uint32 { return s.tracker().Current() }

// MatterRead implements [contract.ClusterServer]: FeatureMap and
// ClusterRevision, then — for a served attribute — the Source's value,
// then the stored one. An attribute neither answers is absent.
func (s *Server) MatterRead(attrID uint32) (any, bool) {
	if v, ok := s.ReadGlobal(attrID); ok {
		return v, true
	}
	if !s.Serves(attrID) {
		return nil, false
	}
	if s.source != nil {
		if v, ok := s.source.MatterAttribute(attrID); ok {
			return v, true
		}
	}
	return s.Value(attrID)
}

// Value returns the stored value of attrID; false when none is stored.
func (s *Server) Value(attrID uint32) (any, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.values[attrID]
	return v, ok
}

// peers resolves a sibling attribute a constraint names.
func (s *Server) peers(attrID uint32) (any, bool) { return s.MatterRead(attrID) }

// MatterWrite implements [contract.ClusterServer]: [Instance.ValidateWrite]
// answers the status of a write the definition refuses; the Sink, when
// set, may refuse it as well and is handed the value in its stored form;
// then the value is stored, and a change bumps the data version and is
// notified.
func (s *Server) MatterWrite(ctx context.Context, attrID uint32, value any) error {
	v, err := s.ValidateWrite(attrID, value, s.peers)
	if err != nil {
		return err
	}
	v = stored(s.def.Attribute(attrID).Type, v)
	if s.sink != nil {
		if err := s.sink.MatterWriteAttribute(ctx, attrID, v); err != nil {
			return err
		}
	}
	s.store(map[uint32]any{attrID: v})
	return nil
}

// Set records a change the device made to attrID. The value is checked
// against the attribute's type and constraint — not its access: the
// device may change what a controller may not write. A change bumps the
// data version and is notified; an equal value changes nothing.
func (s *Server) Set(attrID uint32, value any) error {
	return s.SetAttributes(map[uint32]any{attrID: value})
}

// SetAttributes is [Server.Set] for several attributes at once: every
// value is checked before any is stored, and the changes are reported
// with one data-version bump and one notification, in id order.
func (s *Server) SetAttributes(values map[uint32]any) error {
	checked := make(map[uint32]any, len(values))
	for _, id := range slices.Sorted(maps.Keys(values)) {
		v, err := s.check(id, values[id])
		if err != nil {
			return err
		}
		checked[id] = v
	}
	// The siblings an "in" constraint reads are the state with this
	// change applied, as matter.js validates an assigned property against
	// the struct it is assigned in. Mirrors matter.js
	// packages/node/src/behavior/state/managed/values/StructManager.ts:273-287.
	pending := func(attrID uint32) (any, bool) {
		if v, ok := checked[attrID]; ok {
			return v, true
		}
		return s.peers(attrID)
	}
	for _, id := range slices.Sorted(maps.Keys(checked)) {
		if err := s.checkIn(id, checked[id], pending); err != nil {
			return fmt.Errorf("%w: %w", ErrInvalidValue, err)
		}
	}
	s.store(checked)
	return nil
}

// check holds a device-side value to the served attribute's type and
// constraint — the checks of [Instance.CheckValue], list length and list
// entries and struct fields included — and returns it in its stored form.
func (s *Server) check(attrID uint32, value any) (any, error) {
	if !s.Serves(attrID) {
		return nil, fmt.Errorf("%w: %s attribute 0x%04X", ErrNotServed, s.def.Name, attrID)
	}
	v, err := s.CheckValue(attrID, value, s.peers)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidValue, err)
	}
	return stored(s.def.Attribute(attrID).Type, v), nil
}

// defaultValue is the attribute's model default in its stored form: what
// matter.js starts a behavior's state with (packages/model/src/logic/
// DefaultValue.ts, SelectDefaultValue.ts). The generator renders a
// default as a float64, bool or string, or [NullDefault] for null
// (script/clustergen/gen.go defaultLiteral); one it does not render (a
// list, an object) or one that does not fit the attribute's kind leaves
// the attribute without a value.
func defaultValue(a *Attribute) (any, bool) {
	t := a.Type
	switch d := a.Default.(type) {
	case NullDefault:
		return nil, true
	case float64:
		switch t.Kind {
		case KindUint, KindEnum, KindBitmap:
			if d >= 0 {
				return stored(t, uint64(d)), true
			}
		case KindInt:
			return stored(t, int64(d)), true
		case KindFloat32, KindFloat64:
			return stored(t, d), true
		default:
		}
	case bool:
		if t.Kind == KindBool {
			return d, true
		}
	case string:
		if t.Kind == KindString {
			return d, true
		}
	}
	return nil, false
}

// store keeps values and reports the ones that changed.
func (s *Server) store(values map[uint32]any) {
	var changed []uint32
	s.mu.Lock()
	for id, v := range values {
		if old, ok := s.values[id]; ok && reflect.DeepEqual(old, v) {
			continue
		}
		s.values[id] = v
		changed = append(changed, id)
	}
	s.mu.Unlock()
	if len(changed) == 0 {
		return
	}
	slices.Sort(changed)
	s.tracker().Bump()
	s.Notify(changed...)
}

// Handle registers fn for the request command cmdID. A command the
// instance does not accept is never dispatched, registered or not. A
// mandatory command's handler belongs in [ServerConfig.Handlers]:
// construction refuses a server without it ([ErrMissingHandler]).
func (s *Server) Handle(cmdID uint32, fn CommandHandler) {
	s.mu.Lock()
	s.handlers[cmdID] = fn
	s.mu.Unlock()
}

// Handles reports whether an accepted command has a handler.
func (s *Server) Handles(cmdID uint32) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.Instance.Accepts(cmdID) && s.handlers[cmdID] != nil
}

// unimplementedLocked reports whether cmdID is a stub still without a
// handler.
func (s *Server) unimplementedLocked(cmdID uint32) bool {
	return slices.Contains(s.stubs, cmdID) && s.handlers[cmdID] == nil
}

// Accepts reports whether the server accepts the request command: the
// instance accepts it and it is not an unimplemented stub.
func (s *Server) Accepts(cmdID uint32) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.Instance.Accepts(cmdID) && !s.unimplementedLocked(cmdID)
}

// MatterAcceptedCommands implements [contract.ClusterCommandLister]: the
// instance's accepted commands without the unimplemented stubs
// (ServerBehaviorBacking.ts:127-147 at 0ceb202d lists the implemented
// commands only).
func (s *Server) MatterAcceptedCommands() []uint32 {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.Instance.MatterAcceptedCommands()
	if len(s.stubs) == 0 {
		return out
	}
	return slices.DeleteFunc(out, s.unimplementedLocked)
}

// MatterGeneratedCommands implements [contract.ClusterCommandLister]: the
// responses of the listed accepted commands (ServerBehaviorBacking.ts:137-148).
func (s *Server) MatterGeneratedCommands() []uint32 {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.stubs) == 0 {
		return s.Instance.MatterGeneratedCommands()
	}
	out := []uint32{}
	for _, id := range s.Instance.MatterAcceptedCommands() {
		if s.unimplementedLocked(id) {
			continue
		}
		if r := s.def.responseOf(s.def.Command(id, Request)); r != nil && !slices.Contains(out, r.ID) {
			out = append(out, r.ID)
		}
	}
	return out
}

// MatterInvoke implements [contract.ClusterServer]: an accepted command
// goes to its handler; one not accepted, or accepted without a handler,
// is UNSUPPORTED_COMMAND.
func (s *Server) MatterInvoke(ctx context.Context, cmdID uint32, fields any) (any, error) {
	s.mu.Lock()
	fn := s.handlers[cmdID]
	s.mu.Unlock()
	if !s.Instance.Accepts(cmdID) || fn == nil {
		return nil, im.UnsupportedCommandf("%s: command 0x%02X is not supported", s.def.Name, cmdID)
	}
	return fn(ctx, fields)
}

// SetMatterEventEmitter implements [contract.EventReceiver].
func (s *Server) SetMatterEventEmitter(emitter contract.EventEmitter) {
	s.mu.Lock()
	s.emitter = emitter
	s.mu.Unlock()
}

// Emit emits eventID on endpoint with the priority the definition gives
// it. An event outside EventList is an error ([ErrNotEmitted]); without
// an emitter the event is dropped.
func (s *Server) Emit(endpoint uint16, eventID uint32, data any) error {
	if !s.Emits(eventID) {
		return fmt.Errorf("%w: %s event 0x%02X", ErrNotEmitted, s.def.Name, eventID)
	}
	s.mu.Lock()
	emitter := s.emitter
	s.mu.Unlock()
	if emitter != nil {
		emitter.MatterEmitEvent(endpoint, s.def.ID, eventID, data, s.EventPriority(eventID))
	}
	return nil
}

// underlying turns a value of a named scalar type (a generated enum, a
// bitmap) into its underlying type, the form the checks read; a value of
// any other type is returned as given.
func underlying(v any) any {
	if v == nil {
		return nil
	}
	r := reflect.ValueOf(v)
	switch r.Kind() {
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return r.Uint()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return r.Int()
	case reflect.Float32, reflect.Float64:
		return r.Float()
	case reflect.Bool:
		return r.Bool()
	case reflect.String:
		return r.String()
	default:
		return v
	}
}

// stored turns a checked value (uint64, int64, float64) into the Go type
// of the attribute's width.
func stored(t Type, v any) any {
	switch x := v.(type) {
	case uint64:
		switch {
		case t.Bits > 0 && t.Bits <= 8:
			return uint8(x) //nolint:gosec // within the width per checkUnsigned
		case t.Bits > 8 && t.Bits <= 16:
			return uint16(x) //nolint:gosec // within the width per checkUnsigned
		case t.Bits > 16 && t.Bits <= 32:
			return uint32(x) //nolint:gosec // within the width per checkUnsigned
		}
	case int64:
		switch {
		case t.Bits > 0 && t.Bits <= 8:
			return int8(x) //nolint:gosec // within the width per checkSigned
		case t.Bits > 8 && t.Bits <= 16:
			return int16(x) //nolint:gosec // within the width per checkSigned
		case t.Bits > 16 && t.Bits <= 32:
			return int32(x) //nolint:gosec // within the width per checkSigned
		}
	case float64:
		if t.Kind == KindFloat32 {
			return float32(x)
		}
	}
	return v
}

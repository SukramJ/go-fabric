// SPDX-License-Identifier: MIT
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
)

// Source is the optional read port for attributes the device owns: a
// value it answers takes precedence over the server's stored one.
type Source interface {
	MatterAttribute(attrID uint32) (any, bool)
}

// Sink is the optional write port: told about every controller write that
// passed the definition's checks, before the server stores it; an error
// refuses the write and leaves the stored value as it was.
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
}

// Server is a complete [contract.ClusterServer] for a cluster whose
// server matter.js derives from the model alone — the case of
// packages/node/src/behaviors/fixed-label/FixedLabelServer.ts
// (`export class FixedLabelServer extends FixedLabelBehavior {}`): the
// element lists come from the definition and the feature selection
// (packages/node/src/behavior/cluster/ValidatedElements.ts, the embedded
// [Instance]), a controller write is
// checked against the model, and the state is the attribute values. It
// stores them, dispatches commands to registered handlers, emits events
// with the definition's priority and reports changes with a data-version
// bump and an attribute-change notification.
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
	emitter  contract.EventEmitter
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
	_ contract.AttributeChangeNotifier        = (*Server)(nil)
	_ contract.EventReceiver                  = (*Server)(nil)
)

// NewServer binds def to opts as [New] does and returns a server holding
// cfg.Initial. A definition with a fabric-scoped attribute is refused
// ([ErrFabricScoped]).
func NewServer(def *Cluster, opts Options, cfg ServerConfig) (*Server, error) {
	if def != nil {
		for ai := range def.Attributes {
			if a := &def.Attributes[ai]; a.Access.Fabric == "F" {
				return nil, fmt.Errorf("%w: %s attribute %s", ErrFabricScoped, def.Name, a.Name)
			}
		}
	}
	inst, err := New(def, opts)
	if err != nil {
		return nil, err
	}
	s := &Server{
		Instance: inst,
		ext:      cfg.DataVersion,
		source:   cfg.Source,
		sink:     cfg.Sink,
		values:   map[uint32]any{},
		handlers: map[uint32]CommandHandler{},
	}
	for _, id := range slices.Sorted(maps.Keys(cfg.Initial)) {
		v, err := s.check(id, cfg.Initial[id])
		if err != nil {
			return nil, err
		}
		s.values[id] = v
	}
	return s, nil
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
	s.store(checked)
	return nil
}

// check holds a device-side value to the served attribute's type and
// constraint and returns it in its stored form.
func (s *Server) check(attrID uint32, value any) (any, error) {
	a := s.def.Attribute(attrID)
	if a == nil || !s.Serves(attrID) {
		return nil, fmt.Errorf("%w: %s attribute 0x%04X", ErrNotServed, s.def.Name, attrID)
	}
	v, err := s.checkValue(a.Name, a.Type, a.Quality.Nullable, a.Constraint, underlying(value), s.peers)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidValue, err)
	}
	return stored(a.Type, v), nil
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
// instance does not accept is never dispatched, registered or not.
func (s *Server) Handle(cmdID uint32, fn CommandHandler) {
	s.mu.Lock()
	s.handlers[cmdID] = fn
	s.mu.Unlock()
}

// MatterInvoke implements [contract.ClusterServer]: an accepted command
// goes to its handler; one not accepted, or accepted without a handler,
// is UNSUPPORTED_COMMAND.
func (s *Server) MatterInvoke(ctx context.Context, cmdID uint32, fields any) (any, error) {
	s.mu.Lock()
	fn := s.handlers[cmdID]
	s.mu.Unlock()
	if !s.Accepts(cmdID) || fn == nil {
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

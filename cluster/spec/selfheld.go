// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package spec

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/SukramJ/go-fabric/contract"
)

// The parts a self-held server — a hand-written cluster server with its
// own command logic over a definition — implements the typed server's
// optional interfaces with ([AttributeSetter], [CommandRegistrar],
// [EventEmitting], [BeforeWriteSetter]), so the typed server has one shape
// over every server, generated or self-held (ADR 0025 principle 2).

// Errors of the self-held parts.
var (
	// ErrNotAccepted: a command handler for a command the server does not
	// accept ([CommandRegistrar]).
	ErrNotAccepted = errors.New("spec: the server does not accept the command")
	// ErrNotHeld: a value for an attribute the server derives or reads
	// from a host port rather than holding it ([AttributeSetter]).
	ErrNotHeld = errors.New("spec: the attribute is not held by the server")
)

// HostHandlers holds the command handlers a host registers over a
// self-held server's own command logic. A registered handler replaces the
// server's handling of its command — the request's checks included — as a
// matter.js host overrides the behavior's command method
// (packages/node/src/behaviors/level-control/LevelControlServer.ts:256-261
// names the override point of its commands); nil hands the command back
// to the server.
type HostHandlers struct {
	mu sync.Mutex
	m  map[uint32]CommandHandler
}

// Register sets fn as the host's handler of cmdID; nil removes it. A
// command inst does not accept is refused with [ErrNotAccepted].
func (h *HostHandlers) Register(inst *Instance, cmdID uint32, fn CommandHandler) error {
	if !inst.Accepts(cmdID) {
		return fmt.Errorf("%w: %s command 0x%02X", ErrNotAccepted, inst.def.Name, cmdID)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if fn == nil {
		delete(h.m, cmdID)
		return nil
	}
	if h.m == nil {
		h.m = map[uint32]CommandHandler{}
	}
	h.m[cmdID] = fn
	return nil
}

// Invoke runs the host's handler of cmdID; handled is false when the host
// registered none, and the server's own logic runs.
func (h *HostHandlers) Invoke(ctx context.Context, cmdID uint32, fields any) (resp any, handled bool, err error) {
	h.mu.Lock()
	fn := h.m[cmdID]
	h.mu.Unlock()
	if fn == nil {
		return nil, false, nil
	}
	resp, err = fn(ctx, fields)
	return resp, true, err
}

// Events is a self-held server's event path: the emitter the endpoint
// hands it ([contract.EventReceiver]) and the emission of its cluster's
// events with the definition's priority.
type Events struct {
	mu      sync.Mutex
	emitter contract.EventEmitter
}

// SetEmitter stores the endpoint's emitter.
func (e *Events) SetEmitter(emitter contract.EventEmitter) {
	e.mu.Lock()
	e.emitter = emitter
	e.mu.Unlock()
}

// Emit emits eventID with data on endpoint at the priority inst's
// definition gives it. An event outside inst's EventList is an error
// ([ErrNotEmitted]); without an emitter the event is dropped, as
// [Server.Emit] drops it.
func (e *Events) Emit(inst *Instance, endpoint uint16, eventID uint32, data any) error {
	if !inst.Emits(eventID) {
		return fmt.Errorf("%w: %s event 0x%02X", ErrNotEmitted, inst.def.Name, eventID)
	}
	e.mu.Lock()
	emitter := e.emitter
	e.mu.Unlock()
	if emitter != nil {
		emitter.MatterEmitEvent(endpoint, inst.def.ID, eventID, data, inst.EventPriority(eventID))
	}
	return nil
}

// WriteVeto is a self-held server's write veto ([BeforeWriteSetter]):
// the hook [ServerConfig.BeforeWrite] is on the generated server.
type WriteVeto struct {
	mu sync.Mutex
	fn func(ctx context.Context, attrID uint32, value any) error
}

// Set installs fn, replacing the one before; nil removes it.
func (w *WriteVeto) Set(fn func(ctx context.Context, attrID uint32, value any) error) {
	w.mu.Lock()
	w.fn = fn
	w.mu.Unlock()
}

// Check runs the veto on a controller's write that passed the
// definition's checks, with the value in its stored form; nil without a
// veto. The error is returned as given: its IM status when it carries
// one, FAILURE at the dispatcher otherwise.
func (w *WriteVeto) Check(ctx context.Context, attrID uint32, value any) error {
	w.mu.Lock()
	fn := w.fn
	w.mu.Unlock()
	if fn == nil {
		return nil
	}
	return fn(ctx, attrID, value)
}

// SelfHeld gives a self-held server whose state is a [Server] it keeps
// (the server's own logic around the generated store) the four optional
// interfaces: SetAttributes sets the store, Handle registers a host
// handler over the server's own logic ([HostHandlers]), Emit emits
// through the store's emitter, and SetBeforeWrite vetoes the writes the
// store takes. The server embeds it, routes MatterInvoke through
// [InvokeHeld], forwards its emitter with [SelfHeld.SetMatterEventEmitter]
// unless it keeps its own, and overrides SetAttributes where its own state
// sits beside the store. Every method it promotes is one of those the
// server answers anyway.
type SelfHeld struct {
	srv  *Server
	host HostHandlers
}

// NewSelfHeld returns the four interfaces over srv.
func NewSelfHeld(srv *Server) SelfHeld { return SelfHeld{srv: srv} }

// SetAttributes implements [AttributeSetter]: the host's own change,
// checked against the attribute's type and constraint, stored and
// reported, all or nothing ([Server.SetAttributes]).
func (h *SelfHeld) SetAttributes(values map[uint32]any) error { return h.srv.SetAttributes(values) }

// Handle implements [CommandRegistrar] ([HostHandlers]).
func (h *SelfHeld) Handle(cmdID uint32, fn CommandHandler) error {
	return h.host.Register(h.srv.Instance, cmdID, fn)
}

// Emit implements [EventEmitting] ([Server.Emit]).
func (h *SelfHeld) Emit(endpoint uint16, eventID uint32, data any) error {
	return h.srv.Emit(endpoint, eventID, data)
}

// SetBeforeWrite implements [BeforeWriteSetter] ([Server.SetBeforeWrite]).
func (h *SelfHeld) SetBeforeWrite(fn func(ctx context.Context, attrID uint32, value any) error) {
	h.srv.SetBeforeWrite(fn)
}

// SetMatterEventEmitter implements [contract.EventReceiver] by handing the
// emitter to the store.
func (h *SelfHeld) SetMatterEventEmitter(emitter contract.EventEmitter) {
	h.srv.SetMatterEventEmitter(emitter)
}

// InvokeHeld is a self-held server's MatterInvoke: the host's handler of
// cmdID when one is registered ([SelfHeld.Handle]), the store's — the
// server's own logic — otherwise. It is a function rather than a method so
// that embedding [SelfHeld] promotes no second invoke onto the server.
func InvokeHeld(ctx context.Context, h *SelfHeld, cmdID uint32, fields any) (any, error) {
	if resp, handled, err := h.host.Invoke(ctx, cmdID, fields); handled {
		return resp, err
	}
	return h.srv.MatterInvoke(ctx, cmdID, fields)
}

// RequestOf returns a decoded request payload as the generated request
// type T: the value as it is, or the value a non-nil *T points to — the
// two forms a self-held server takes its cluster's generated requests in.
// fabric:reachable:reason="generic: the self-held servers call instantiations of it, and RTA records the instantiation, not the generic declaration"
func RequestOf[T any](fields any) (T, bool) {
	switch v := fields.(type) {
	case *T:
		if v != nil {
			return *v, true
		}
	case T:
		return v, true
	}
	var zero T
	return zero, false
}

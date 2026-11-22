// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package spec

import (
	"context"

	"thingfabric.dev/go-fabric/contract"
)

// Hooks gives a self-held server that keeps its own state — no generated
// store, so no [SelfHeld] — three of the typed server's four optional
// interfaces over its definition's instance: Handle ([HostHandlers]),
// Emit ([Events]) and SetBeforeWrite ([WriteVeto]); the server writes
// SetAttributes over its own setters. The server embeds it, calls
// [Hooks.Init] with its instance when it is built, runs [Hooks.Invoke]
// first in its MatterInvoke (the host's handler; the request gate is the
// endpoint dispatcher's, [AdmitRequest]) and [Hooks.CheckWrite] after its own checks
// of a controller's write and its comparison with the held value, and hands the endpoint's emitter on with
// [Hooks.SetMatterEventEmitter] — promoted, or called from its own when
// it keeps the emitter itself too.
//
// Deprecated: a self-held server builds a [Server] over its own state —
// [ServerConfig] Source answering its reads, Handlers its own commands — and routes
// MatterInvoke through [InvokeHeld] over [NewSelfHeld]; its events and its
// veto are an [Events] and a [WriteVeto] (review II, decision 154).
// Removable in v0.25.0.
type Hooks struct {
	inst     *Instance
	handlers HostHandlers
	veto     WriteVeto
	events   Events
}

// Init binds the hooks to the server's instance.
func (h *Hooks) Init(inst *Instance) { h.inst = inst }

// Handle implements [CommandRegistrar]: the host's handler of cmdID
// replaces the server's own handling of it; nil hands it back
// ([HostHandlers.Register]).
func (h *Hooks) Handle(cmdID uint32, fn CommandHandler) error {
	return h.handlers.Register(h.inst, cmdID, fn)
}

// Invoke runs the host's handler of cmdID ([HostHandlers.Invoke]);
// handled is false when the host registered none, and the server's own
// logic runs. The request's and the response's model checks are the
// endpoint dispatcher's ([AdmitRequest], [CheckResponse]), run around the
// server; a direct call is a local one and is not validated.
func (h *Hooks) Invoke(ctx context.Context, cmdID uint32, fields any) (resp any, handled bool, err error) {
	return h.handlers.Invoke(ctx, cmdID, fields)
}

// Emit implements [EventEmitting]: eventID with data on endpoint at the
// priority the definition gives it ([Events.Emit]).
func (h *Hooks) Emit(endpoint uint16, eventID uint32, data any) error {
	return h.events.Emit(h.inst, endpoint, eventID, data)
}

// EmitEvent emits eventID with data on the endpoint the server is mounted
// on ([Events.EmitEvent]): the call the typed Record<Event> makes.
func (h *Hooks) EmitEvent(eventID uint32, data any) error {
	return h.events.EmitEvent(h.inst, eventID, data)
}

// SetEndpoint implements [contract.EndpointReceiver]: the endpoint
// [Hooks.EmitEvent] emits on. A server with a SetEndpoint of its own calls
// it from there.
func (h *Hooks) SetEndpoint(endpoint uint16) { h.events.SetEndpoint(endpoint) }

// SetMatterEventEmitter implements [contract.EventReceiver]: the emitter
// [Hooks.Emit] emits through.
func (h *Hooks) SetMatterEventEmitter(emitter contract.EventEmitter) { h.events.SetEmitter(emitter) }

// SetBeforeWrite implements [BeforeWriteSetter] ([WriteVeto]).
func (h *Hooks) SetBeforeWrite(fn func(ctx context.Context, attrID uint32, value any) error) {
	h.veto.Set(fn)
}

// CheckWrite runs the veto on a controller's write that passed the
// server's checks and changes the value it holds ([WriteVeto.Check]):
// called after the server's own comparison, never for a write of the held
// value.
func (h *Hooks) CheckWrite(ctx context.Context, attrID uint32, value any) error {
	return h.veto.Check(ctx, attrID, value)
}

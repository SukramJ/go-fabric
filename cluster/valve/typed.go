// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package valve

import (
	"context"
	"fmt"

	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/contract"
)

// The typed server's optional interfaces: Handle ([spec.SelfHeld]), the
// events ([spec.Events]) and SetBeforeWrite ([spec.WriteVeto]). The state is
// the host's StateSource: the host moves the valve there and tells the
// bridge through the source's contract.ChangeNotifier, so SetAttributes
// refuses every value.
var (
	_ spec.AttributeSetter   = (*Server)(nil)
	_ spec.CommandRegistrar  = (*Server)(nil)
	_ spec.EventEmitting     = (*Server)(nil)
	_ spec.BeforeWriteSetter = (*Server)(nil)
)

// SetAttributes implements [spec.AttributeSetter]: every attribute is the
// StateSource's ([spec.ErrNotHeld]).
func (*Server) SetAttributes(values map[uint32]any) error {
	for id := range values {
		return fmt.Errorf("%w: valve: attribute 0x%04X is the StateSource's", spec.ErrNotHeld, id)
	}
	return nil
}

// Handle implements [spec.CommandRegistrar] ([spec.SelfHeld.Handle]).
func (s *Server) Handle(cmdID uint32, fn spec.CommandHandler) error { return s.held.Handle(cmdID, fn) }

// Emit implements [spec.EventEmitting]: the host's ValveStateChanged or
// ValveFault ([spec.Events.Emit]).
func (s *Server) Emit(endpoint uint16, eventID uint32, data any) error {
	return s.events.Emit(s.inst, endpoint, eventID, data)
}

// EmitEvent emits eventID on the endpoint the server is mounted on
// ([spec.Events.EmitEvent]) — the call the typed Record<Event> makes.
func (s *Server) EmitEvent(eventID uint32, data any) error {
	return s.events.EmitEvent(s.inst, eventID, data)
}

// SetEndpoint implements contract.EndpointReceiver: the endpoint
// [Server.EmitEvent] emits on.
func (s *Server) SetEndpoint(endpoint uint16) { s.events.SetEndpoint(endpoint) }

// SetMatterEventEmitter implements contract.EventReceiver: the emitter
// [Server.Emit] emits through.
func (s *Server) SetMatterEventEmitter(emitter contract.EventEmitter) {
	s.events.SetEmitter(emitter)
}

// SetBeforeWrite implements [spec.BeforeWriteSetter]: the veto of a
// controller's DefaultOpenDuration write, before the StateSource takes it.
func (s *Server) SetBeforeWrite(fn func(ctx context.Context, attrID uint32, value any) error) {
	s.veto.Set(fn)
}

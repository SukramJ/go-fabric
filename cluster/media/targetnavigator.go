// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package media

import (
	"context"
	"fmt"
	"slices"

	"github.com/SukramJ/go-fabric/cluster/spec"
	targetdef "github.com/SukramJ/go-fabric/cluster/spec/targetnavigator"
)

// TargetState is the TargetNavigator attribute state
// (target-navigator.element.ts:22-29), the cluster's generated State.
// TargetList is mandatory and has no default: nil refuses construction
// ([ErrMissingState]). CurrentTarget (optional, served when the
// configuration lists it) is 0xFF — the element's default (:28) — while no
// target is current.
type TargetState = targetdef.State

func targetValues(st TargetState) map[uint32]any {
	return map[uint32]any{
		targetdef.AttrTargetList:    listOr(st.TargetList),
		targetdef.AttrCurrentTarget: st.CurrentTarget,
	}
}

func cloneTarget(st TargetState) TargetState {
	st.TargetList = slices.Clone(st.TargetList)
	return st
}

// Targets is the TargetNavigator host port (the generated
// [targetdef.Targets], the declaration's Targets): NavigateTarget
// (:41-48), answered by NavigateTargetResponse (:50-54); the port makes
// the target current in st.
type Targets = targetdef.Targets

// TargetsConfig carries the construction parameters of a TargetNavigator
// server.
type TargetsConfig struct {
	// Port carries out NavigateTarget; required.
	Port Targets
	// Attributes lists the optional attributes served: AttrCurrentTarget.
	Attributes []uint32
	// Events lists the optional events emitted: EventTargetUpdated.
	Events []uint32
	// Initial is the state the server starts with.
	Initial TargetState
	// DataVersion is an optional host-owned tracker.
	DataVersion dataVersion
}

// TargetsServer is a TargetNavigator (0x0505) server.
type TargetsServer struct {
	*spec.Server
	stateful[TargetState]
	events
}

// NewTargets builds a TargetNavigator server over cfg.Port.
func NewTargets(cfg TargetsConfig) (*TargetsServer, error) {
	if cfg.Port == nil {
		return nil, fmt.Errorf("%w: TargetNavigator", ErrNilPort)
	}
	s := &TargetsServer{}
	h := handlers{}
	port := cfg.Port
	handleState(h, &s.stateful, targetdef.CmdNavigateTarget, func(ctx context.Context, st *TargetState, req targetdef.NavigateTargetRequest) (any, error) {
		return port.NavigateTarget(ctx, st, req)
	})
	srv, err := newServer(targetdef.Definition, spec.Options{Attributes: cfg.Attributes, Events: cfg.Events},
		spec.ServerConfig{Handlers: h, DataVersion: cfg.DataVersion})
	if err != nil {
		return nil, fmt.Errorf("media: TargetNavigator: %w", err)
	}
	s.Server = srv
	if err := s.init(srv, cfg.Initial, targetValues, cloneTarget, storeState[TargetState]); err != nil {
		return nil, fmt.Errorf("media: TargetNavigator: %w", err)
	}
	if err := checkState(srv); err != nil {
		return nil, err
	}
	return s, nil
}

// SetAttributes implements [spec.AttributeSetter] ([stateful.setAttributes]).
func (s *TargetsServer) SetAttributes(values map[uint32]any) error { return s.setAttributes(values) }

// EmitTargetUpdated emits TargetUpdated (event 0x00, info; :31-39), which
// the server emits when cfg.Events lists it.
func (s *TargetsServer) EmitTargetUpdated(ev targetdef.TargetUpdatedEvent) error {
	return s.Emit(s.endpoint(), targetdef.EventTargetUpdated, ev)
}

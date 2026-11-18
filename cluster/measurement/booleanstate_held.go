// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package measurement

import (
	"fmt"
	"sync"

	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/cluster/spec"
	bsdef "github.com/SukramJ/go-fabric/cluster/spec/booleanstate"
	"github.com/SukramJ/go-fabric/contract"
)

// HeldBooleanStateConfig carries the construction parameters of a
// [HeldBooleanState].
type HeldBooleanStateConfig struct {
	// Features is the FeatureMap; ChangeEvent (CHGEVENT) emits StateChange.
	Features bsdef.Feature
	// DataVersion is an optional host-owned tracker.
	//
	// Deprecated: a host-owned tracker adds nothing — the wire carries the
	// endpoint's DataVersion for the cluster, or the server's own where it
	// serves one (decision 165, F-db021da0-46); leave it nil. Removable in
	// v0.25.0.
	DataVersion *cluster.DataVersionTracker
}

// HeldBooleanState is the BooleanState server that holds StateValue
// itself — matter.js's BooleanStateServer: the host sets StateValue (the
// typed Set), and with the ChangeEvent feature every change emits
// StateChange carrying the new value
// (packages/node/src/behaviors/boolean-state/BooleanStateServer.ts:20-28,
// reactTo stateValue$Changed). The store's four typed interfaces are
// promoted; the server learns its endpoint at reassembly (SetEndpoint).
//
// The host's sets — [HeldBooleanState.Set], [HeldBooleanState.SetAttributes]
// and [HeldBooleanState.SetAttributesFrom] — are serialised with the event
// each emits, so StateChange carries the value its own set stored, also
// when two host goroutines set StateValue at once (R2-P6-conc-1462).
type HeldBooleanState struct {
	*spec.Server
	emits bool
	// setMu serialises a set with the StateChange it emits.
	setMu sync.Mutex
}

// Compile-time assertions.
var (
	_ spec.AttributeSetter   = (*HeldBooleanState)(nil)
	_ spec.CommandRegistrar  = (*HeldBooleanState)(nil)
	_ spec.EventEmitting     = (*HeldBooleanState)(nil)
	_ spec.BeforeWriteSetter = (*HeldBooleanState)(nil)
	_ contract.EventReceiver = (*HeldBooleanState)(nil)
)

// NewHeldBooleanState builds the server, StateValue false.
func NewHeldBooleanState(cfg HeldBooleanStateConfig) (*HeldBooleanState, error) {
	srv, err := spec.NewServer(bsdef.Definition, spec.Options{Features: uint32(cfg.Features)}, spec.ServerConfig{DataVersion: cfg.DataVersion}) //nolint:staticcheck // the deprecated tracker is passed on for its window (decision 165)
	if err != nil {
		return nil, fmt.Errorf("measurement: BooleanState: %w", err)
	}
	return &HeldBooleanState{Server: srv, emits: srv.Emits(bsdef.EventStateChange)}, nil
}

// Set is [spec.Server.Set], serialised with the StateChange it emits.
func (s *HeldBooleanState) Set(attrID uint32, value any) error {
	return s.SetAttributesFrom(contract.OriginHostSet, map[uint32]any{attrID: value})
}

// SetAttributes implements [spec.AttributeSetter]
// ([spec.Server.SetAttributes]), serialised with the StateChange it emits.
func (s *HeldBooleanState) SetAttributes(values map[uint32]any) error {
	return s.SetAttributesFrom(contract.OriginHostSet, values)
}

// SetAttributesFrom is [spec.Server.SetAttributesFrom]; with ChangeEvent a
// change of StateValue then emits StateChange with the value this set
// stored.
func (s *HeldBooleanState) SetAttributesFrom(origin contract.Origin, values map[uint32]any) error {
	s.setMu.Lock()
	defer s.setMu.Unlock()
	before, _ := s.Value(bsdef.AttrStateValue)
	if err := s.Server.SetAttributesFrom(origin, values); err != nil {
		return err
	}
	if !s.emits {
		return nil
	}
	after, _ := s.Value(bsdef.AttrStateValue)
	if after == before {
		return nil
	}
	on, _ := after.(bool)
	_ = s.EmitEvent(bsdef.EventStateChange, bsdef.StateChangeEvent{StateValue: on})
	return nil
}

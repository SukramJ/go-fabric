// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package measurement

import (
	"fmt"
	"slices"
	"sync/atomic"

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
	DataVersion *cluster.DataVersionTracker
}

// HeldBooleanState is the BooleanState server that holds StateValue
// itself — matter.js's BooleanStateServer: the host sets StateValue (the
// typed Set), and with the ChangeEvent feature every change emits
// StateChange carrying the new value
// (packages/node/src/behaviors/boolean-state/BooleanStateServer.ts:20-28,
// reactTo stateValue$Changed). The store's four typed interfaces are
// promoted; the server learns its endpoint at reassembly (SetEndpoint).
type HeldBooleanState struct {
	*spec.Server
	endpoint atomic.Uint32
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
	srv, err := spec.NewServer(bsdef.Definition, spec.Options{Features: uint32(cfg.Features)}, spec.ServerConfig{DataVersion: cfg.DataVersion})
	if err != nil {
		return nil, fmt.Errorf("measurement: BooleanState: %w", err)
	}
	s := &HeldBooleanState{Server: srv}
	if srv.Emits(bsdef.EventStateChange) {
		srv.OnAttributeChange(func(c contract.Change) {
			if !slices.Contains(c.Attributes, bsdef.AttrStateValue) {
				return
			}
			v, _ := srv.Value(bsdef.AttrStateValue)
			on, _ := v.(bool)
			_ = srv.Emit(uint16(s.endpoint.Load()), bsdef.EventStateChange, bsdef.StateChangeEvent{StateValue: on}) //nolint:gosec // an endpoint number
		})
	}
	return s, nil
}

// SetEndpoint stamps the endpoint StateChange is addressed to; the bridge
// calls it at reassembly.
func (s *HeldBooleanState) SetEndpoint(endpoint uint16) { s.endpoint.Store(uint32(endpoint)) }

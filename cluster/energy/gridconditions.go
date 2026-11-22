// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package energy

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"thingfabric.dev/go-fabric/cluster"
	"thingfabric.dev/go-fabric/cluster/spec"
	egc "thingfabric.dev/go-fabric/cluster/spec/electricalgridconditions"
	"thingfabric.dev/go-fabric/contract"
	"thingfabric.dev/go-fabric/im"
)

// ElectricalGridConditions (0x00A0). Its chip rules are in
// src/app/clusters/electrical-grid-conditions-server/
// electrical-grid-conditions-server.cpp, cited below by line.

// ClusterIDElectricalGridConditions is the ElectricalGridConditions
// cluster id.
const ClusterIDElectricalGridConditions = egc.ClusterID

// The ElectricalGridConditions datatypes, the generated ones.
type (
	// GridConditionsFeature is an ElectricalGridConditions FeatureMap bit.
	GridConditionsFeature = egc.Feature
	// GridConditions is the ElectricalGridConditionsStruct.
	GridConditions = egc.ElectricalGridConditionsStruct
	// ThreeLevel is the ThreeLevelEnum.
	ThreeLevel = egc.ThreeLevelEnum
)

// GridConditionsFeatureForecasting is the FORE feature.
const GridConditionsFeatureForecasting = egc.FeatureForecasting

// GridConditionsConfig carries the construction parameters.
type GridConditionsConfig struct {
	Features GridConditionsFeature
	// DataVersion is an optional host-owned tracker.
	//
	// Deprecated: a host-owned tracker adds nothing — the wire carries the
	// endpoint's DataVersion for the cluster, or the server's own where it
	// serves one (decision 165, F-db021da0-46); leave it nil. Removable in
	// v0.25.0.
	DataVersion *cluster.DataVersionTracker
}

// GridConditionsServer implements [contract.ClusterServer] for
// ElectricalGridConditions. LocalGenerationAvailable is writable with
// View privilege, as the generated definition states (RW VO); the
// CurrentConditions checks and the CurrentConditionsChanged event are
// chip's.
type GridConditionsServer struct {
	*spec.Instance
	// SelfHeld is the typed server's command handlers, event path and
	// write veto over srv (typed.go).
	spec.SelfHeld

	srv *spec.Server

	// mu guards endpoint.
	mu       sync.Mutex
	endpoint uint16
}

// Compile-time assertions.
var (
	_ contract.ClusterServer                  = (*GridConditionsServer)(nil)
	_ contract.ClusterDataVersion             = (*GridConditionsServer)(nil)
	_ contract.ClusterAttributeLister         = (*GridConditionsServer)(nil)
	_ contract.ClusterEventLister             = (*GridConditionsServer)(nil)
	_ contract.ClusterAttributeWritePrivilege = (*GridConditionsServer)(nil)
	_ contract.AttributeChangeNotifier        = (*GridConditionsServer)(nil)
	_ contract.EventReceiver                  = (*GridConditionsServer)(nil)
)

// NewElectricalGridConditions builds an ElectricalGridConditions server.
// LocalGenerationAvailable and CurrentConditions start null and
// ForecastConditions empty, as chip's instance starts
// (electrical-grid-conditions-server.h:90-92). CurrentConditionsChanged
// is emitted: chip generates it on every CurrentConditions change
// (electrical-grid-conditions-server.cpp:142-143).
func NewElectricalGridConditions(cfg GridConditionsConfig) (*GridConditionsServer, error) {
	initial := map[uint32]any{
		egc.AttrLocalGenerationAvailable: nil,
		egc.AttrCurrentConditions:        nil,
	}
	if cfg.Features&egc.FeatureForecasting != 0 {
		initial[egc.AttrForecastConditions] = []GridConditions{}
	}
	srv, err := spec.NewServer(egc.Definition,
		spec.Options{Features: uint32(cfg.Features), Events: []uint32{egc.EventCurrentConditionsChanged}},
		spec.ServerConfig{DataVersion: cfg.DataVersion, Initial: initial}) //nolint:staticcheck // the deprecated tracker is passed on for its window (decision 165)
	if err != nil {
		return nil, fmt.Errorf("energy: %w", err)
	}
	return &GridConditionsServer{Instance: srv.Instance, SelfHeld: spec.NewSelfHeld(srv), srv: srv}, nil
}

// SetLocalGenerationAvailable records LocalGenerationAvailable; nil is
// null (electrical-grid-conditions-server.cpp:93-115).
func (s *GridConditionsServer) SetLocalGenerationAvailable(available *bool) error {
	if available == nil {
		return s.srv.Set(egc.AttrLocalGenerationAvailable, nil)
	}
	return s.srv.Set(egc.AttrLocalGenerationAvailable, *available)
}

// validGridConditions holds a conditions entry to chip's checks: both
// carbon levels in the enum, PeriodStart not after a non-null PeriodEnd
// (electrical-grid-conditions-server.cpp:119-136).
func validGridConditions(c GridConditions) error {
	if c.GridCarbonLevel > egc.ThreeLevelHigh || c.LocalCarbonLevel > egc.ThreeLevelHigh {
		return statusError{im.StatusConstraintError, "energy: ElectricalGridConditions: carbon level outside the enum"}
	}
	if !c.PeriodEnd.Null && c.PeriodStart > c.PeriodEnd.Value {
		return statusError{im.StatusConstraintError, "energy: ElectricalGridConditions: PeriodEnd before PeriodStart"}
	}
	return nil
}

// SetCurrentConditions records CurrentConditions — nil is null — and
// emits CurrentConditionsChanged carrying them
// (electrical-grid-conditions-server.cpp:117-163). Conditions chip's
// checks refuse leave the attribute as it was.
func (s *GridConditionsServer) SetCurrentConditions(c *GridConditions) error {
	event := egc.CurrentConditionsChangedEvent{CurrentConditions: spec.NullOf[GridConditions]()}
	var stored any
	if c != nil {
		if err := validGridConditions(*c); err != nil {
			return err
		}
		stored = *c
		event.CurrentConditions = spec.ValueOf(*c)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.srv.Set(egc.AttrCurrentConditions, stored); err != nil {
		return err
	}
	// A server not in the topology (spec.ErrNotMounted) has no event log
	// to record in: the state change stands, the event is dropped.
	if err := s.srv.EmitEvent(egc.EventCurrentConditionsChanged, event); !errors.Is(err, spec.ErrNotMounted) {
		return err
	}
	return nil
}

// SetForecastConditions records ForecastConditions, FORE only
// (electrical-grid-conditions-server.cpp:165-174; the read refuses the
// attribute without the feature, :68-73). chip stores the list as given;
// the generated server holds it to the definition's constraints — the
// carbon levels' enum and at most 56 entries — but not to
// SetCurrentConditions' period rule: an entry whose PeriodEnd is not after
// its PeriodStart, and entries out of order, are stored (review II,
// R2-P6-wire-1613, kept as chip's).
func (s *GridConditionsServer) SetForecastConditions(forecast []GridConditions) error {
	if !s.HasFeature("FORE") {
		return errors.New("energy: ElectricalGridConditions: ForecastConditions needs the Forecasting feature")
	}
	return s.srv.Set(egc.AttrForecastConditions, append([]GridConditions{}, forecast...))
}

// --- plumbing -------------------------------------------------------------

// SetEndpoint stamps the endpoint the events are addressed to; the bridge
// calls it at reassembly.
func (s *GridConditionsServer) SetEndpoint(endpoint uint16) {
	s.mu.Lock()
	s.endpoint = endpoint
	s.mu.Unlock()
	s.srv.SetEndpoint(endpoint) // the store emits on it (EmitEvent, the typed Record<Event>)
}

// SetMatterEventEmitter implements [contract.EventReceiver].
func (s *GridConditionsServer) SetMatterEventEmitter(emitter contract.EventEmitter) {
	s.srv.SetMatterEventEmitter(emitter)
}

// MatterDataVersion implements [contract.ClusterDataVersion].
func (s *GridConditionsServer) MatterDataVersion() uint32 { return s.srv.MatterDataVersion() }

// OnAttributeChange implements [contract.AttributeChangeNotifier].
func (s *GridConditionsServer) OnAttributeChange(cb func(change contract.Change)) (unsubscribe func()) {
	return s.srv.OnAttributeChange(cb)
}

// MatterRead resolves an attribute; a list as its generated list
// (commodityWireValue).
func (s *GridConditionsServer) MatterRead(attrID uint32) (any, bool) {
	return commodityRead(s.srv.MatterRead(attrID))
}

// MatterWrite stores a LocalGenerationAvailable write the definition
// accepts.
func (s *GridConditionsServer) MatterWrite(ctx context.Context, attrID uint32, value any) error {
	return s.srv.MatterWrite(ctx, attrID, value)
}

// MatterWriteChanged implements [contract.ChangeReportingWriter]: the
// generated server's write and its verdict.
func (s *GridConditionsServer) MatterWriteChanged(ctx context.Context, attrID uint32, value any) (bool, error) {
	return s.srv.MatterWriteChanged(ctx, attrID, value)
}

// MatterInvoke answers every command: the cluster has none.
func (s *GridConditionsServer) MatterInvoke(ctx context.Context, cmdID uint32, fields any) (any, error) {
	return spec.InvokeHeld(ctx, &s.SelfHeld, cmdID, fields) // a host's handler first ([spec.SelfHeld.Handle])
}

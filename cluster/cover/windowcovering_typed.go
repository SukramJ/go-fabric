// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package cover

import (
	"context"
	"fmt"
	"maps"
	"slices"

	"github.com/SukramJ/go-fabric/cluster/spec"
	wcdef "github.com/SukramJ/go-fabric/cluster/spec/windowcovering"
	"github.com/SukramJ/go-fabric/contract"
)

// The typed server's optional interfaces ([spec.AttributeSetter],
// [spec.CommandRegistrar], [spec.EventEmitting], [spec.BeforeWriteSetter]).
var (
	_ spec.AttributeSetter   = (*WindowCoveringServer)(nil)
	_ spec.CommandRegistrar  = (*WindowCoveringServer)(nil)
	_ spec.EventEmitting     = (*WindowCoveringServer)(nil)
	_ spec.BeforeWriteSetter = (*WindowCoveringServer)(nil)
	_ contract.EventReceiver = (*WindowCoveringServer)(nil)
)

// SetAttributes implements [spec.AttributeSetter]: the host's own change,
// all or nothing, each value checked against its attribute's type and
// constraint, stored without the [Mover] and reported as
// [WindowCoveringServer.SetCurrentPosition],
// [WindowCoveringServer.SetTargetPosition] and
// [WindowCoveringServer.SetOperationalStatus] report — no write veto. The
// held attributes are the served axes' CurrentPosition…Percent100ths and
// TargetPosition…Percent100ths (null is refused: the server keeps a
// position, [spec.ErrInvalidValue]), OperationalStatus and Mode (stored as
// given; ConfigStatus follows it). A position set while a simulated
// travel runs does not stop it: its next step overwrites the value, as
// matter.js's next step overwrites a state write
// (packages/node/src/behavior/Transitions.ts:455-534, :537-574).
//
// ConfigStatus and the deprecated CurrentPosition…Percentage are derived,
// and Type, EndProductType and SafetyStatus are the configuration's: a
// value other than the one they read is refused with [spec.ErrNotHeld].
func (s *WindowCoveringServer) SetAttributes(values map[uint32]any) error {
	checked := make(map[uint32]any, len(values))
	for _, id := range slices.Sorted(maps.Keys(values)) {
		v, err := s.inst.CheckSet(id, values[id], nil)
		if err != nil {
			return err
		}
		switch id {
		case wcdef.AttrConfigStatus, wcdef.AttrCurrentPositionLiftPercentage, wcdef.AttrCurrentPositionTiltPercentage:
			return fmt.Errorf("%w: windowcovering: attribute 0x%04X is derived", spec.ErrNotHeld, id)
		case wcdef.AttrType, wcdef.AttrEndProductType, wcdef.AttrSafetyStatus:
			if cur, _ := s.MatterRead(id); v != cur {
				return fmt.Errorf("%w: windowcovering: attribute 0x%04X is the configuration's (%v)", spec.ErrNotHeld, id, cur)
			}
			continue
		case wcdef.AttrCurrentPositionLiftPercent100ths, wcdef.AttrCurrentPositionTiltPercent100ths,
			wcdef.AttrTargetPositionLiftPercent100ths, wcdef.AttrTargetPositionTiltPercent100ths:
			if v == nil {
				return fmt.Errorf("%w: windowcovering: attribute 0x%04X: the server keeps a position, not null", spec.ErrInvalidValue, id)
			}
		}
		checked[id] = v
	}
	if len(checked) == 0 {
		return nil
	}
	var report []uint32
	s.mu.Lock()
	for _, a := range []*axis{s.lift, s.tilt} {
		if a == nil {
			continue
		}
		if v, ok := checked[a.attrCurrent].(uint16); ok && v != a.current {
			a.current = v
			report = append(report, a.attrCurrent, a.attrPercentage)
		}
		if v, ok := checked[a.attrTarget].(uint16); ok && v != a.target {
			a.target = v
			report = append(report, a.attrTarget)
		}
	}
	if v, ok := checked[wcdef.AttrOperationalStatus].(uint8); ok && v != s.operationalStatus {
		s.operationalStatus = v
		report = append(report, wcdef.AttrOperationalStatus)
	}
	if v, ok := checked[wcdef.AttrMode].(uint8); ok && v != s.wcMode {
		before := s.configStatusLocked()
		s.wcMode = v
		report = append(report, wcdef.AttrMode)
		if s.configStatusLocked() != before {
			report = append(report, wcdef.AttrConfigStatus)
		}
	}
	s.mu.Unlock()
	if len(report) > 0 {
		slices.Sort(report)
		s.changes.Notify(report...)
	}
	return nil
}

// Handle implements [spec.CommandRegistrar]: fn replaces the server's own
// handling of cmdID — the request's checks, the MaintenanceMode refusal,
// the [Mover] and the simulated travel — as a matter.js host overrides the
// WindowCoveringServer command method; fn runs outside the server's
// command queue. nil hands the command back. A command the feature
// selection does not accept is refused with [spec.ErrNotAccepted].
func (s *WindowCoveringServer) Handle(cmdID uint32, fn spec.CommandHandler) error {
	return s.host.Register(s.inst, cmdID, fn)
}

// Emit implements [spec.EventEmitting]. WindowCovering defines no event,
// so every event is refused with [spec.ErrNotEmitted].
func (s *WindowCoveringServer) Emit(endpoint uint16, eventID uint32, data any) error {
	return s.events.Emit(s.inst, endpoint, eventID, data)
}

// SetMatterEventEmitter implements [contract.EventReceiver].
func (s *WindowCoveringServer) SetMatterEventEmitter(emitter contract.EventEmitter) {
	s.events.SetEmitter(emitter)
}

// SetBeforeWrite implements [spec.BeforeWriteSetter]: fn vetoes a
// controller's write of Mode after the definition's and the server's
// checks and before it is stored, with the value written as a uint8.
func (s *WindowCoveringServer) SetBeforeWrite(fn func(ctx context.Context, attrID uint32, value any) error) {
	s.veto.Set(fn)
}

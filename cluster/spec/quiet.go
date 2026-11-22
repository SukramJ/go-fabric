// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package spec

import (
	"fmt"

	"thingfabric.dev/go-fabric/clock"
	"thingfabric.dev/go-fabric/cluster"
	"thingfabric.dev/go-fabric/contract"
)

// quietAttr is the report throttle of one quieter ("Q") attribute a
// [Server] serves.
type quietAttr struct {
	q *cluster.Quieter
	// origin is the origin of the change the throttle reports next, null
	// whether the last value it saw was null, and computed whether that
	// change is a Source-computed one ([Server.MarkQuietChanged]), whose
	// version advance waits for the emit; all under Server.mu.
	origin   contract.Origin
	null     bool
	computed bool
}

// initQuiet gives every served quieter attribute its throttle, over clk
// and the definition's [Attribute.QuietInterval].
func (s *Server) initQuiet(clk clock.Clock) {
	for _, id := range s.attrs {
		a := s.def.Attribute(id)
		if !a.Quality.Quieter {
			continue
		}
		if s.quiet == nil {
			s.quiet = map[uint32]*quietAttr{}
		}
		qa := &quietAttr{}
		qa.q = &cluster.Quieter{Clock: clk, Interval: a.QuietInterval, Report: func() { s.quietReport(id, qa) }}
		s.quiet[id] = qa
	}
}

// quietReport is a throttle's emit: the report listeners — the wire's —
// learn of the change, and for a Source-computed change
// ([Server.MarkQuietChanged]) the data version advances now; a stored
// change advanced it when it was stored ([Server.store]). Mirrors matter.js
// packages/node/src/behavior/internal/ServerBehaviorBacking.ts:230-262 at
// db021da0: a quieter property's change is not broadcast at the commit
// (:163-168, :242) but on the quiet event's emit (:258-261), where
// advanceVersionFor (:259) advances the version for a dynamic property
// only (behavior/state/managed/Datasource.ts:679-688, :705-713); a stored
// property's change advanced it at the commit, whatever its quality
// (Datasource.ts:1446-1457).
func (s *Server) quietReport(attrID uint32, qa *quietAttr) {
	s.mu.Lock()
	origin, computed := qa.origin, qa.computed
	qa.computed = false
	s.mu.Unlock()
	if computed {
		s.tracker().Bump()
	}
	s.AttributeChanges.ReportFrom(origin, attrID)
}

// MarkQuietChanged reports a change of a quieter ("Q") attribute the host
// computes on read — served from the [Source], so no Set sees it change —
// as [Server.MarkChanged] reports any other: the change listeners learn of
// it at once, the report listeners and the data version when the
// attribute's throttle emits ([Attribute.QuietInterval]), as the host's
// ([contract.OriginHostSet]) — matter.js advances a dynamic (computed)
// property's version on the quiet emit (Datasource.ts:679-688). An attribute the server does not serve is
// refused ([ErrNotServed]), one without the Q quality too
// ([ErrNotReportable]: [Server.MarkChanged] reports it).
func (s *Server) MarkQuietChanged(attrID uint32) error {
	if !s.Serves(attrID) {
		return fmt.Errorf("%w: %s cannot report 0x%04X, which is not an attribute it supports", ErrNotServed, s.def.Name, attrID)
	}
	qa := s.quiet[attrID]
	if qa == nil {
		return fmt.Errorf("%w: %s attribute 0x%04X is not quieter; report it with MarkChanged", ErrNotReportable, s.def.Name, attrID)
	}
	v, _ := s.MatterRead(attrID)
	s.mu.Lock()
	wasNull := qa.null
	qa.null, qa.origin, qa.computed = v == nil, contract.OriginHostSet, true
	notify := func() {
		s.AttributeChanges.ChangedFrom(contract.OriginHostSet, attrID)
		qa.q.Changed(wasNull, v == nil)
	}
	// Inside an open commit the change listeners learn of it once the
	// write left its window, as of a Set there ([Server.MarkChanged]); its
	// version still advances when the throttle emits.
	if open := s.commit; open != nil {
		open.notifies = append(open.notifies, notify)
		s.mu.Unlock()
		return nil
	}
	s.mu.Unlock()
	notify()
	return nil
}

// MatterQuiesce implements [contract.ClusterQuiescer]: every quieter
// attribute's deferred report is disarmed and dropped, unreported. The
// server stays usable; the next change reports as before.
func (s *Server) MatterQuiesce() {
	for _, qa := range s.quiet {
		qa.q.Stop()
	}
}

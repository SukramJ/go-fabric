// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package spec

import (
	"fmt"

	"github.com/SukramJ/go-fabric/clock"
	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/contract"
)

// quietAttr is the report throttle of one quieter ("Q") attribute a
// [Server] serves.
type quietAttr struct {
	q *cluster.Quieter
	// origin is the origin of the change the throttle reports next, and
	// null whether the last value it saw was null; both under Server.mu.
	origin contract.Origin
	null   bool
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

// quietReport is a throttle's emit: the data version advances and the
// report listeners — the wire's — learn of the change. Mirrors matter.js
// packages/node/src/behavior/internal/ServerBehaviorBacking.ts:230-262 at
// db021da0: a quieter property's change is not broadcast at the commit;
// on the quiet event's emit the datasource's version advances (:259) and
// the property is broadcast.
func (s *Server) quietReport(attrID uint32, qa *quietAttr) {
	s.mu.Lock()
	origin := qa.origin
	s.mu.Unlock()
	s.tracker().Bump()
	s.AttributeChanges.ReportFrom(origin, attrID)
}

// MarkQuietChanged reports a change of a quieter ("Q") attribute the host
// computes on read — served from the [Source], so no Set sees it change —
// as [Server.MarkChanged] reports any other: the change listeners learn of
// it at once, the report listeners and the data version when the
// attribute's throttle emits ([Attribute.QuietInterval]), as the host's
// ([contract.OriginHostSet]). An attribute the server does not serve is
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
	qa.null, qa.origin = v == nil, contract.OriginHostSet
	s.mu.Unlock()
	s.AttributeChanges.ChangedFrom(contract.OriginHostSet, attrID)
	qa.q.Changed(wasNull, v == nil)
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

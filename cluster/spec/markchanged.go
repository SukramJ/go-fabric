// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package spec

import (
	"errors"
	"fmt"
	"slices"

	"github.com/SukramJ/go-fabric/contract"
)

// The refusals of [Server.MarkChanged] and [MarkChangedOf].
var (
	// ErrNotReportable: MarkChanged named no attribute, or an attribute
	// whose quality keeps it from reporting directly — changes omitted
	// ("C") or quieter ("Q").
	ErrNotReportable = errors.New("spec: the change cannot be reported with MarkChanged")
	// ErrNoMarkChanged: the server is no [ChangeMarker] — it does not own
	// the data version a report advances.
	ErrNoMarkChanged = errors.New("spec: only a server that owns its data version reports changes (no MarkChanged)")
)

// ChangeMarker is a server that reports a change of attributes it
// computes on read ([Server.MarkChanged]).
type ChangeMarker interface {
	MarkChanged(attrIDs ...uint32) error
}

// MarkChanged reports attributes the host computes on read — served from
// the [Source] rather than a stored value, so no Set ever sees them
// change: it advances the data version once and notifies the change
// listeners and the report listeners, as the host's
// ([contract.OriginHostSet]). The ids are reported ascending, each once.
//
// It refuses, and changes nothing, when attrIDs is empty, when an id is
// not an attribute the server serves ([ErrNotServed]) — an attribute the
// definition does not define is not one either — and when an attribute's
// quality is changes omitted ("C"), whose changes the specification does
// not report, or quieter ("Q"), whose report goes through its throttle
// ([cluster.Quieter]) rather than directly ([ErrNotReportable]).
//
// Mirrors matter.js
// packages/node/src/behavior/internal/ServerBehaviorBacking.ts:186-221 at
// 862ad75f (markChanged: the empty list :187-189, an attribute not among
// the supported ones :196-198, the changesOmitted and quieter refusals
// :201-212, then datasource.advanceVersion :216 and broadcastChanges
// :218; Datasource.ts:151 advanceVersion). matter.js also refuses a call
// before the behavior is initialized (:191-193); a Server exists only
// constructed, so there is no such state.
func (s *Server) MarkChanged(attrIDs ...uint32) error {
	if len(attrIDs) == 0 {
		return fmt.Errorf("%w: %s cannot report a change without naming an attribute", ErrNotReportable, s.def.Name)
	}
	for _, id := range attrIDs {
		if !s.Serves(id) {
			return fmt.Errorf("%w: %s cannot report 0x%04X, which is not an attribute it supports", ErrNotServed, s.def.Name, id)
		}
		a := s.def.Attribute(id)
		switch {
		case a.Quality.ChangesOmitted:
			return fmt.Errorf("%w: %s cannot report %s; the specification omits change reporting for it", ErrNotReportable, s.def.Name, a.Name)
		case a.Quality.Quieter:
			return fmt.Errorf("%w: %s cannot report %s directly; report its change through its quieter so its throttle applies", ErrNotReportable, s.def.Name, a.Name)
		}
	}
	ids := slices.Compact(slices.Sorted(slices.Values(attrIDs)))
	s.tracker().Bump()
	s.NotifyFrom(contract.OriginHostSet, ids...)
	return nil
}

// MarkChangedOf reports attrIDs through srv's [ChangeMarker]
// ([Server.MarkChanged]); [ErrNoMarkChanged] for a server that is none —
// a self-held server, or a peer's state, whose data version is not this
// server's to advance. It is the route of the generated typed server's
// MarkChanged.
//
// Mirrors matter.js
// packages/node/src/behavior/internal/BehaviorBacking.ts:440-444 at
// 862ad75f: the base backing's markChanged refuses ("only a server
// behavior reports changes"), so a backing that does not own its version
// never advances it.
func MarkChangedOf(srv contract.ClusterServer, attrIDs ...uint32) error {
	m, ok := srv.(ChangeMarker)
	if !ok {
		return fmt.Errorf("%w: %T", ErrNoMarkChanged, srv)
	}
	return m.MarkChanged(attrIDs...)
}

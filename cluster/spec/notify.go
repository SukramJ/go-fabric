// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package spec

import "github.com/SukramJ/go-fabric/contract"

// The four notifications below shadow the ones [Server] would promote from
// its embedded cluster.AttributeChanges. Each keeps that behaviour exactly:
// it tells the listeners, and nothing else happens.

// Notify tells every listener that attrIDs changed, from
// [contract.OriginUnknown].
//
// It bypasses every check [Server.MarkChanged] makes — the ids are not
// held to the attributes the server serves, a changes-omitted ("C") or
// quieter ("Q") attribute is reported directly, past its throttle — and it
// advances no data version, so a DataVersionFilter keeps matching a
// version whose data moved.
//
// Deprecated: use [Server.Set] for a value the server holds and
// [Server.MarkChanged] (or [Server.MarkQuietChanged]) for one the host
// computes on read. Removable in v0.25.0.
func (s *Server) Notify(attrIDs ...uint32) { s.AttributeChanges.Notify(attrIDs...) }

// NotifyFrom tells every listener — change and report listeners alike —
// that attrIDs changed, from origin. It bypasses the checks and the data
// version advance [Server.Notify] names.
//
// Deprecated: use [Server.Set] or [Server.MarkChanged] (see
// [Server.Notify]). Removable in v0.25.0.
func (s *Server) NotifyFrom(origin contract.Origin, attrIDs ...uint32) {
	s.AttributeChanges.NotifyFrom(origin, attrIDs...)
}

// ChangedFrom tells the change listeners alone that attrIDs changed, from
// origin; no report reaches the wire. It bypasses the checks and the data
// version advance [Server.Notify] names.
//
// Deprecated: use [Server.Set] or [Server.MarkQuietChanged], which tell the
// change listeners and report through the attribute's throttle. Removable in
// v0.25.0.
func (s *Server) ChangedFrom(origin contract.Origin, attrIDs ...uint32) {
	s.AttributeChanges.ChangedFrom(origin, attrIDs...)
}

// ReportFrom tells the report listeners alone — the wire's — that attrIDs
// changed, from origin. It bypasses the checks and the data version
// advance [Server.Notify] names, and a quieter attribute's throttle.
//
// Deprecated: use [Server.Set], [Server.MarkChanged] or
// [Server.MarkQuietChanged]. Removable in v0.25.0.
func (s *Server) ReportFrom(origin contract.Origin, attrIDs ...uint32) {
	s.AttributeChanges.ReportFrom(origin, attrIDs...)
}

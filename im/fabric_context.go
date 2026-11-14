// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package im

import "context"

// fabricFilterContext carries the FabricFiltered flag + the operational
// FabricIndex through the IM dispatcher to fabric-scoped cluster
// servers. Mirrors matter.js's OnlineContext.{forFabricFilteredRead,
// fabricIndex} (packages/protocol/src/interaction/InteractionServer.ts:
// startReadInteraction). go-fabric threads via context.Context to
// avoid breaking the Dispatcher.Read signature.
type fabricFilterContext struct {
	filtered    bool
	fabricIndex uint8
}

type fabricFilterCtxKey struct{}

// WithFabricFilter returns a derived context carrying the FabricFiltered
// flag from the request and the operational FabricIndex resolved from
// the inbound session. Cluster servers that expose fabric-scoped
// attributes (OperationalCredentials.Fabrics, AccessControl.ACL) read
// this via [FabricFilterFromContext] inside their MatterReadFiltered and
// project the underlying list down to the requesting fabric when
// filtered=true. fabricIndex==0 means a session with no fabric yet — a
// PASE session before AddNOC. matter.js gives such a session no
// fabric of its own: a fabric-filtered read of a fabric-scoped list
// shows it no entry, and an unfiltered one withholds every entry's
// fabric-sensitive fields (packages/node/src/behavior/state/managed/
// values/ListManager.ts FabricFilteredListProxyHandler,
// packages/protocol/src/action/server/AccessControl.ts mayRead, which
// refuses a fabric-sensitive field to a session without
// `session.fabric`). [LookupFabricFilter] tells such a request apart
// from a read with no request behind it.
func WithFabricFilter(ctx context.Context, filtered bool, fabricIndex uint8) context.Context {
	return context.WithValue(ctx, fabricFilterCtxKey{}, fabricFilterContext{filtered: filtered, fabricIndex: fabricIndex})
}

// FabricFilterFromContext extracts the FabricFiltered flag + FabricIndex
// stamped by [WithFabricFilter]. Returns (false, 0) when no filter
// context is present — the safe default that yields the unfiltered
// list (Matter §7.5.2).
func FabricFilterFromContext(ctx context.Context) (filtered bool, fabricIndex uint8) {
	v, ok := ctx.Value(fabricFilterCtxKey{}).(fabricFilterContext)
	if !ok {
		return false, 0
	}
	return v.filtered, v.fabricIndex
}

// LookupFabricFilter is [FabricFilterFromContext] that also reports
// whether a filter was stamped at all. ok=false means no Interaction
// Model request is behind the read — a local caller, which matter.js
// models as a local actor that sees every entry whole (AccessControl.ts
// hasLocalActor). ok=true with fabricIndex 0 is a remote session that
// has no fabric yet; the two must not stand for one another, because the
// first may see what the second may not.
func LookupFabricFilter(ctx context.Context) (filtered bool, fabricIndex uint8, ok bool) {
	v, ok := ctx.Value(fabricFilterCtxKey{}).(fabricFilterContext)
	if !ok {
		return false, 0, false
	}
	return v.filtered, v.fabricIndex, true
}

// subjectContext carries the requesting peer's identity for ACL
// subject-matching (Matter §9.10.5.6). For CASE sessions: NodeID is
// the operational subject from the peer's NOC, CATs is the set of
// CASE Authenticated Tags lifted from the same NOC subject. A request
// with no subject context (NodeID==0) is treated as anonymous —
// matches only ACL entries whose Subjects list is empty (the
// fabric-wide wildcard).
type subjectContext struct {
	nodeID uint64
	cats   []uint32
}

type subjectCtxKey struct{}

// WithSubject stamps the operational peer subject — NodeID lifted out
// of the peer NOC plus the set of CASE Authenticated Tags from the
// same NOC — into ctx so [TopologyDispatcher.CheckACL] can enforce
// per-subject ACEs (Matter §9.10.5.6). PASE sessions stamp NodeID=0
// and nil CATs; the ACL gate bypasses subject matching on PASE via
// the fabricIndex==0 check.
//
// Mirrors connectedhomeip/src/access/AccessControl.cpp:441 — the
// `subjectDescriptor` the access check uses carries (fabricIndex,
// authMode, subject, cats). go-fabric carries the same triple via
// ctx so the IM dispatcher's signature stays minimal.
func WithSubject(ctx context.Context, nodeID uint64, cats []uint32) context.Context {
	var copyCATs []uint32
	if len(cats) > 0 {
		copyCATs = append(copyCATs, cats...)
	}
	return context.WithValue(ctx, subjectCtxKey{}, subjectContext{nodeID: nodeID, cats: copyCATs})
}

// SubjectFromContext extracts the peer subject (NodeID + CATs)
// stamped by [WithSubject]. Returns (0, nil) when no subject is
// present — the safe default that limits the request to entries
// whose Subjects list is empty (i.e. "any subject on this fabric").
func SubjectFromContext(ctx context.Context) (nodeID uint64, cats []uint32) {
	v, ok := ctx.Value(subjectCtxKey{}).(subjectContext)
	if !ok {
		return 0, nil
	}
	return v.nodeID, v.cats
}

// authModePASEContext marks a request as arriving over a PASE
// (commissioning) session. It is carried separately from the
// FabricIndex because the two stop agreeing the moment AddNOC
// succeeds: OperationalCredentials associates the freshly built fabric
// with the very PASE session that issued the command
// (matter.js packages/node/src/behaviors/operational-credentials/
// OperationalCredentialsServer.ts:279-283 `session.fabric = fabric`),
// so from then on the session reports a non-zero FabricIndex while
// still being a PASE session.
type authModePASEContext struct{}

type authModePASECtxKey struct{}

// WithAuthModePASE stamps "this request arrived over a PASE session"
// into ctx. The IM ACL gates read it via [IsPASEFromContext] and grant
// the implicit Administer privilege the commissioning channel carries,
// independent of the session's FabricIndex.
//
// Mirrors matter.js packages/protocol/src/interaction/
// FabricAccessControl.ts:189-191 — `subjectDesc.authMode ===
// AccessControl.AccessControlEntryAuthMode.Pase &&
// subjectDesc.isCommissioning` grants AccessLevel.Administer before any
// ACL entry is consulted. Keying that grant on `fabricIndex == 0`
// instead loses it at AddNOC, and the default `[CaseAdminSubject]` ACE
// never covers subject node-id 0 — so every follow-up ACL write,
// GroupKeySetWrite or other Administer command a commissioner sends
// over the same PASE channel would answer UnsupportedAccess.
func WithAuthModePASE(ctx context.Context) context.Context {
	return context.WithValue(ctx, authModePASECtxKey{}, authModePASEContext{})
}

// IsPASEFromContext reports whether the request arrived over a PASE
// session, as stamped by [WithAuthModePASE]. Returns false when no
// auth mode is present — the safe default, which leaves the request to
// the ordinary ACL evaluation rather than handing it Administer.
func IsPASEFromContext(ctx context.Context) bool {
	_, ok := ctx.Value(authModePASECtxKey{}).(authModePASEContext)
	return ok
}

type localNodeIDCtxKey struct{}

// WithLocalNodeID stamps the node's operational node id on the accessing
// session's fabric into ctx — 0 for a session without a fabric. A read or
// subscribe path that names a node is then answered for this node only
// ([AttributeExpansion]): matter.js filters a path's nodeId against that
// id (packages/protocol/src/action/server/DataResponse.ts:57-62 at
// db021da0, UNSPECIFIED_NODE_ID without a fabric).
func WithLocalNodeID(ctx context.Context, nodeID uint64) context.Context {
	return context.WithValue(ctx, localNodeIDCtxKey{}, nodeID)
}

// LocalNodeIDFromContext returns the id [WithLocalNodeID] stamped; ok is
// false when none was, and a path's node is then not looked at.
func LocalNodeIDFromContext(ctx context.Context) (nodeID uint64, ok bool) {
	nodeID, ok = ctx.Value(localNodeIDCtxKey{}).(uint64)
	return nodeID, ok
}

// checkSubjectACL is the IM ACL gate for one location once the gate is
// active (an [ACLChecker] is wired and the session is not PASE). A
// request without an accessing fabric that is not PASE has no subject
// the ACL could grant anything to and is refused with UnsupportedAccess
// — it fails closed instead of skipping the check. Mirrors chip
// src/transport/UnauthenticatedSessionTable.h:82-85 (an unauthenticated
// session's SubjectDescriptor is empty: fabric 0, auth mode none) and
// src/access/AccessControl.cpp:355-372 (Check finds no entry for that
// subject and denies). matter.js never reaches the gate there:
// InteractionServer.ts:161 `requiresSecureSession = true`.
//
// An index the caller could not resolve (endpoint.FabricIndexUnresolvable)
// is no accessing fabric either and is refused here, before the
// access-control source is asked: the gate does not rest on every
// [ACLChecker] knowing that index ([HasAccessingFabric]).
func checkSubjectACL(ctx context.Context, c ACLChecker, fabricIndex uint8, subjectNodeID uint64, subjectCATs []uint32, endpoint uint16, clusterID uint32, privilege uint8) StatusCode {
	if !HasAccessingFabric(fabricIndex) {
		return StatusUnsupportedAccess
	}
	return c.CheckACL(ctx, fabricIndex, subjectNodeID, subjectCATs, endpoint, clusterID, privilege)
}

// HasAccessingFabric reports whether fabricIndex names a fabric — the one
// reading of "no accessing fabric" every gate of this package uses: the
// subject's access check, the fabric-scoped write and invoke gates, the
// group authorizer. Matter constrains a fabric index to "1 to 254" (matter.js
// packages/model/src/standard/elements/fabric-index.element.ts:12-14 at
// db021da0); 0 is a session without a fabric — PASE before AddNOC,
// matter.js FabricIndex.NO_FABRIC (packages/types/src/datatype/FabricIndex.ts:28
// at db021da0) — and anything above 254 an index the caller could not
// resolve (endpoint.FabricIndexUnresolvable). Neither is an accessing
// fabric. No reference models an index it cannot resolve; reading it as no
// fabric is this module's fail-closed rule.
//
// Exported for the gate that runs outside this package on the same
// question: the bridge's filter of a subscription's attribute reports
// (bridge.readAuthorizedResults).
func HasAccessingFabric(fabricIndex uint8) bool {
	return fabricIndex >= 1 && fabricIndex <= 254
}

// timedCtxKey carries whether an Invoke or a Write runs inside a valid timed
// interaction.
type timedCtxKey struct{}

// WithTimedInteraction stamps whether the request runs inside a valid
// timed interaction (a TimedRequest preceded it on the exchange and its
// window has not expired). The transport stamps it once the timed gate has
// passed; [HandleInvokeRequest] then answers a timed-required command
// outside one with NeedsTimedInteraction for that path — matter.js
// CommandInvokeResponse.ts:291 `limits.timed && !this.session.timed`;
// the dispatcher's write path answers an attribute with the timed quality
// the same way (AttributeWriteResponse.ts:243-247).
func WithTimedInteraction(ctx context.Context, timed bool) context.Context {
	return context.WithValue(ctx, timedCtxKey{}, timed)
}

// TimedInteractionFromContext reports the stamp of [WithTimedInteraction];
// known is false when the caller stamped nothing, in which case the
// per-command timed check is the caller's.
func TimedInteractionFromContext(ctx context.Context) (timed, known bool) {
	timed, known = ctx.Value(timedCtxKey{}).(bool)
	return timed, known
}

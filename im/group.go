// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package im

import (
	"context"
	"slices"

	"github.com/SukramJ/go-fabric/schema"
)

// GroupSubject is the Incoming Subject Descriptor of a group message: the
// destination group, whether the key that authenticated the message is the
// one GroupKeyMap maps the group to, and the local endpoints that are
// members of the group. Mirrors matter.js Subject.Group
// (packages/protocol/src/action/server/Subject.ts).
type GroupSubject struct {
	GroupID         uint16
	HasValidMapping bool
	Endpoints       []uint16
}

type groupSubjectCtxKey struct{}

// WithGroupSubject stamps the Group subject of a group message into ctx.
// A request carrying one is evaluated under the Group auth mode: only
// Group access control entries apply, and their subject is the group id
// (Matter §9.10.5.6; matter.js FabricAccessControl #getIsdFromMessage).
func WithGroupSubject(ctx context.Context, s GroupSubject) context.Context {
	s.Endpoints = slices.Clone(s.Endpoints)
	return context.WithValue(ctx, groupSubjectCtxKey{}, s)
}

// GroupSubjectFromContext returns the Group subject stamped by
// [WithGroupSubject], or false for a unicast request.
func GroupSubjectFromContext(ctx context.Context) (GroupSubject, bool) {
	s, ok := ctx.Value(groupSubjectCtxKey{}).(GroupSubject)
	return s, ok
}

// CommandAuthorizer decides whether a command may run at a resolved
// (endpoint, cluster, command), returning [StatusSuccess] to proceed.
type CommandAuthorizer func(endpoint uint16, clusterID, commandID uint32) StatusCode

// AuthorizingInvoker is an optional interface a [Dispatcher] implements so
// a wildcard-endpoint command — the only shape a group command takes — can
// be dispatched to every endpoint that hosts the cluster and accepts the
// command, each location authorized before the command runs.
type AuthorizingInvoker interface {
	// InvokeAuthorized dispatches path, whose endpoint is a wildcard, to
	// every endpoint hosting path.Cluster that accepts path.Command, in
	// ascending endpoint order, skipping a location authorize denies.
	InvokeAuthorized(ctx context.Context, path ConcreteCommandPath, fields any, authorize CommandAuthorizer) []InvokeResult
}

// Group interactions answer nothing: a group message gets neither a
// response nor a StatusResponse (matter.js InteractionMessenger
// handleRequest sends no status for a group session). The handlers below
// return the status the interaction ended with only so the caller can log
// it.

// GroupInvokeReport is what a group InvokeRequest did, for the report
// Groupcast testing makes of every group message (matter.js
// InteractionServer.handleInvokeRequest emits its GroupMessageEventInfo
// from the same facts).
type GroupInvokeReport struct {
	// Status is the status the interaction ended with; nothing is sent.
	Status StatusCode
	// Processed reports that the request passed the checks that end a
	// group interaction before any command is considered (a TimedRequest
	// flag, a concrete or incomplete path, a wildcard sharing the
	// message) — in matter.js those throw before the invoke results are
	// iterated, so no outcome is reported for them.
	Processed bool
	// Requested are the command paths the request named.
	Requested []ConcreteCommandPath
	// Dispatched are the command's results on every endpoint it ran on;
	// an endpoint access control denied is not among them.
	Dispatched []InvokeResult
}

// HandleGroupInvokeRequest runs an InvokeRequest that arrived as a group
// message and returns the status it ended with; see [HandleGroupInvoke].
func HandleGroupInvokeRequest(ctx context.Context, d Dispatcher, req InvokeRequest, timedRequired func(clusterID, commandID uint32) bool) StatusCode {
	return HandleGroupInvoke(ctx, d, req, timedRequired).Status
}

// HandleGroupInvoke runs an InvokeRequest that arrived as a group
// message. ctx carries the fabric ([WithFabricFilter]) and the Group
// subject ([WithGroupSubject]). timedRequired reports a command whose
// model access requires a timed interaction; may be nil.
//
// Mirrors matter.js InteractionServer.handleInvokeRequest for a group
// session and CommandInvokeResponse.process / #processWildcard /
// #wildcardTargetOf: a TimedRequest flag on a group message mismatches
// the (absent) timed interaction; every command path must leave the
// endpoint out (a concrete path is InvalidAction) and name its cluster and
// command; a wildcard path may not share the message with another
// command; the command then runs on each member endpoint of the group
// that hosts the cluster and accepts the command, where the Group subject
// holds the command's privilege and the command needs no timed
// interaction. A denied endpoint is skipped silently, as every wildcard
// expansion skips it.
func HandleGroupInvoke(ctx context.Context, d Dispatcher, req InvokeRequest, timedRequired func(clusterID, commandID uint32) bool) GroupInvokeReport {
	if req.TimedRequest {
		return GroupInvokeReport{Status: StatusTimedRequestMismatch}
	}
	for _, inv := range req.Invokes {
		if inv.Path.HasEndpoint {
			return GroupInvokeReport{Status: StatusInvalidAction} // "Group commands cannot be concrete paths"
		}
		if !inv.Path.HasCluster || !inv.Path.HasCommand {
			return GroupInvokeReport{Status: StatusInvalidAction}
		}
	}
	if len(req.Invokes) > 1 {
		return GroupInvokeReport{Status: StatusInvalidAction} // "Wildcard path must not be used with multiple invokes"
	}
	subject, ok := GroupSubjectFromContext(ctx)
	invoker, canInvoke := d.(AuthorizingInvoker)
	if !ok || !canInvoke || len(req.Invokes) == 0 {
		return GroupInvokeReport{Status: StatusSuccess}
	}
	inv := req.Invokes[0]
	report := GroupInvokeReport{Status: StatusSuccess, Processed: true, Requested: []ConcreteCommandPath{inv.Path}}
	if len(subject.Endpoints) == 0 {
		return report // "No endpoints mapped to group, skipping wildcard invoke"
	}
	authorize := groupAuthorizer(ctx, d, subject)
	check := func(endpoint uint16, clusterID, commandID uint32) StatusCode {
		if timedRequired != nil && timedRequired(clusterID, commandID) {
			return StatusNeedsTimedInteraction
		}
		if status := authorize(endpoint, clusterID, invokePrivilegeOf(d, endpoint, clusterID, commandID)); !status.IsSuccess() {
			return status
		}
		// A group session carries no large messages: a Large Message
		// command is left out after the authority check, as a wildcard
		// path leaves it out — matter.js runs a group invoke through the
		// wildcard expansion (CommandInvokeResponse.ts:136-160 at db021da0)
		// and #wildcardTargetOf (:384-386).
		if schema.CommandLargeMessage(clusterID, commandID) {
			return StatusInvalidTransportType
		}
		return StatusSuccess
	}
	if !inv.DecodeStatus.IsSuccess() {
		// Fields that do not decode fail the command on every endpoint
		// access control lets it reach — after the access check, not
		// instead of it. matter.js CommandInvokeResponse authorises each
		// member endpoint (#processEndpointForWildcard) before
		// #invokeCommand decodes and validates the fields, so the decode
		// failure is a per-endpoint command status, and
		// InteractionServer.handleInvokeRequest reports each as a
		// GroupcastTesting outcome with AccessAllowed true (3e4c88b8,
		// #4526; Core §11.27.7.6.3). The command itself never runs: the
		// authoriser answers the decode status for an allowed endpoint,
		// which the dispatcher treats as "do not invoke".
		var allowed []uint16
		invoker.InvokeAuthorized(ctx, inv.Path, nil, func(endpoint uint16, clusterID, commandID uint32) StatusCode {
			if st := check(endpoint, clusterID, commandID); !st.IsSuccess() {
				return st
			}
			allowed = append(allowed, endpoint)
			return inv.DecodeStatus
		})
		report.Status = inv.DecodeStatus
		for _, ep := range allowed {
			p := inv.Path
			p.Endpoint, p.HasEndpoint = ep, true
			report.Dispatched = append(report.Dispatched, InvokeResult{Path: p, Status: inv.DecodeStatus})
		}
		return report
	}
	report.Dispatched = invoker.InvokeAuthorized(ctx, inv.Path, inv.Fields, check)
	return report
}

// HandleGroupWriteRequest runs a WriteRequest that arrived as a group
// message. ctx carries the fabric and the Group subject.
//
// Mirrors matter.js InteractionServer.handleWriteRequest for a group
// session and AttributeWriteResponse.process / #processWildcard /
// #writeAttributeForWildcard: chunking together with SuppressResponse is
// InvalidAction, a TimedRequest flag mismatches, a group write without
// SuppressResponse is InvalidAction; the writes then run in order, each
// on every member endpoint that hosts the attribute, where the attribute
// is writable, the Group subject holds its write privilege and the
// attribute has no timed quality (a group session is never timed); a path
// with WildcardPathFlags or a concrete path ends the interaction with
// InvalidAction ("Group writes can not be concrete paths") — after the
// writes before it have run, as in matter.js.
//
// A path with a null ListIndex (ADD) is applied on every member endpoint
// as an append, as connectedhomeip applies a group write's list operation
// per mapped endpoint with no rule about the attribute before it
// (src/app/WriteHandler.cpp:434 ProcessGroupAttributeDataIBs, :559
// WriteClusterData at 6170af84; its Busy rule for an ADD that does not
// follow its attribute is in the unicast loop only, :364-371). matter.js
// does not write it: its ADD rule accepts an ADD only after a concrete
// path to the same attribute
// (packages/node/src/node/server/InteractionServer.ts:511-531 at
// db021da0, the marker set for concrete paths only, :536-537), a group
// path is never concrete, so a group ADD is answered Busy, which a group
// session never sends (:550-553), and nothing is written. This follows
// connectedhomeip (owner decision 63): a list a controller chunks into a
// REPLACE and ADDs, as matter.js's own client does by default, would
// otherwise be left empty.
func HandleGroupWriteRequest(ctx context.Context, d Dispatcher, req WriteRequest) StatusCode {
	return HandleGroupWrite(ctx, d, req).Status
}

// GroupWriteOutcome is what [HandleGroupWrite] returns: the status of the
// group interaction and the dispatcher's results behind it.
type GroupWriteOutcome struct {
	// Status is what [HandleGroupWriteRequest] returns.
	Status StatusCode
	// Results are the results the dispatcher returned for the writes it
	// applied, every member endpoint's, in the order it returned them,
	// each with the write's verdict ([WriteResult.Commit]).
	Results []WriteResult
}

// HandleGroupWrite is [HandleGroupWriteRequest] that returns the
// dispatcher's results with the status: a group write's changes are
// reported to subscribers as a unicast write's are ([HandleWrite]) —
// matter.js writes a group path through the same commit as any other
// (AttributeWriteResponse.ts:75-78, :125-149, :278-296, :326-361 →
// :433-435 at db021da0).
func HandleGroupWrite(ctx context.Context, d Dispatcher, req WriteRequest) GroupWriteOutcome {
	var out GroupWriteOutcome
	out.Status = handleGroupWrite(ctx, d, req, func(rs []WriteResult) { out.Results = append(out.Results, rs...) })
	return out
}

// handleGroupWrite runs the group write, handing every dispatcher result
// to record.
func handleGroupWrite(ctx context.Context, d Dispatcher, req WriteRequest, record func([]WriteResult)) StatusCode { //nolint:gocyclo // the group write's per-path gates in matter.js order
	if req.MoreChunkedMessages && req.SuppressResponse {
		return StatusInvalidAction
	}
	if req.TimedRequest {
		return StatusTimedRequestMismatch
	}
	if !req.SuppressResponse {
		return StatusInvalidAction
	}
	subject, ok := GroupSubjectFromContext(ctx)
	writer, canWrite := d.(AuthorizingWriter)
	if !ok || !canWrite {
		return StatusSuccess
	}
	// A group session is never timed: the dispatcher skips an attribute
	// with the timed quality, as matter.js
	// packages/protocol/src/action/server/AttributeWriteResponse.ts:343-345
	// at db021da0 does on the wildcard path a group write takes.
	ctx = WithTimedInteraction(ctx, false)
	authorize := groupAuthorizer(ctx, d, subject)
	privProvider, hasPrivProvider := d.(AttributeWritePrivilegeProvider)
	tx := writeTransactionFromContext(ctx)
	_, fabricIndex := FabricFilterFromContext(ctx)
	localNodeID, hasLocalNodeID := LocalNodeIDFromContext(ctx)
	for _, w := range req.Writes {
		// WildcardPathFlags belong to Read and Subscribe: the element
		// ends the interaction with InvalidAction, after the writes
		// before it (AttributeWriteResponse.ts:59-66, the check every
		// element of process() runs first).
		if w.Path.WildcardPathFlags != 0 {
			return StatusInvalidAction
		}
		// A wildcard path naming another node is dropped without a status
		// (matter.js
		// packages/protocol/src/action/server/AttributeWriteResponse.ts:121-123
		// at db021da0, #processWildcard); a concrete one ends the group
		// interaction below whatever node it names (:75-78).
		concrete := w.Path.HasEndpoint && w.Path.HasCluster && w.Path.HasAttribute
		if !concrete && hasLocalNodeID && w.Path.HasNode && w.Path.Node != localNodeID {
			continue
		}
		if w.Path.HasEndpoint {
			return StatusInvalidAction
		}
		if !w.Path.HasCluster || !w.Path.HasAttribute {
			return StatusInvalidAction
		}
		if len(subject.Endpoints) == 0 {
			continue // "No endpoints mapped to group, skipping"
		}
		groupAuthorize := func(endpoint uint16, clusterID, attrID uint32) StatusCode {
			// A group write skips an attribute a chunked write holds
			// (matter.js AttributeWriteResponse.ts:355-358).
			if tx.heldByOther(fabricIndex, endpoint, clusterID, attrID) {
				return StatusBusy
			}
			priv := uint8(3) // Operate, the default write privilege
			if hasPrivProvider {
				priv = privProvider.MinWritePrivilege(endpoint, clusterID, attrID)
			}
			return authorize(endpoint, clusterID, priv)
		}
		// A numeric ListIndex, and a value its attribute's type cannot
		// decode, ends the interaction where the value would be written
		// ([valueGate]).
		if gate := valueGateFor(w); gate != nil {
			record(writer.WriteAuthorized(ctx, w.Path, w.Value, gate.wrap(groupAuthorize)))
			if gate.reached {
				return gate.status
			}
			continue
		}
		record(writer.WriteAuthorized(ctx, w.Path, w.Value, groupAuthorize))
	}
	return StatusSuccess
}

// groupAuthorizer returns the access check of a group message: the
// endpoint must be a member of the group, and the dispatcher's ACL must
// grant the Group subject the privilege there. Without an ACL checker it
// denies — a group message is never answered on the strength of no check.
func groupAuthorizer(ctx context.Context, d Dispatcher, subject GroupSubject) func(endpoint uint16, clusterID uint32, privilege uint8) StatusCode {
	aclChecker, hasACL := d.(ACLChecker)
	_, fabricIndex := FabricFilterFromContext(ctx)
	return func(endpoint uint16, clusterID uint32, privilege uint8) StatusCode {
		if !slices.Contains(subject.Endpoints, endpoint) {
			return StatusUnsupportedEndpoint
		}
		if !hasACL || !HasAccessingFabric(fabricIndex) {
			return StatusUnsupportedAccess
		}
		return aclChecker.CheckACL(ctx, fabricIndex, 0, nil, endpoint, clusterID, privilege)
	}
}

// invokePrivilegeOf returns the privilege a command needs, Operate unless
// the dispatcher says more.
func invokePrivilegeOf(d Dispatcher, endpoint uint16, clusterID, commandID uint32) uint8 {
	if p, ok := d.(CommandInvokePrivilegeProvider); ok {
		return p.MinInvokePrivilege(endpoint, clusterID, commandID)
	}
	return 3
}

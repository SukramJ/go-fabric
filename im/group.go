// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package im

import (
	"context"
	"slices"
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
		return authorize(endpoint, clusterID, invokePrivilegeOf(d, endpoint, clusterID, commandID))
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
// is writable and the Group subject holds its write privilege; a concrete
// path ends the interaction with InvalidAction ("Group writes can not be
// concrete paths") — after the writes before it have run, as in matter.js.
func HandleGroupWriteRequest(ctx context.Context, d Dispatcher, req WriteRequest) StatusCode {
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
	authorize := groupAuthorizer(ctx, d, subject)
	privProvider, hasPrivProvider := d.(AttributeWritePrivilegeProvider)
	for _, w := range req.Writes {
		if w.Path.HasEndpoint {
			return StatusInvalidAction
		}
		if !w.Path.HasCluster || !w.Path.HasAttribute {
			return StatusInvalidAction
		}
		if len(subject.Endpoints) == 0 {
			continue // "No endpoints mapped to group, skipping"
		}
		writer.WriteAuthorized(ctx, w.Path, w.Value, func(endpoint uint16, clusterID, attrID uint32) StatusCode {
			priv := uint8(3) // Operate, the default write privilege
			if hasPrivProvider {
				priv = privProvider.MinWritePrivilege(endpoint, clusterID, attrID)
			}
			return authorize(endpoint, clusterID, priv)
		})
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
		if !hasACL || fabricIndex == 0 {
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

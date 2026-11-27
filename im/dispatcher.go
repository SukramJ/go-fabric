// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package im

import "context"

// AttributeValue carries the result of a Read or the input of a Write.
// The bridge speaks cluster-native Go types ([bool], [uint8], [int16],
// …) and the IM layer is responsible only for routing — the TLV
// encoder bound to the cluster-server side does the wire-shape work.
type AttributeValue struct {
	// Value is the cluster-native Go value. Nil for unobserved /
	// nullable attributes.
	Value any
	// IsNull is true when the attribute value is explicitly null on
	// the wire (Matter NULL TLV element). Mutually exclusive with a
	// non-nil Value.
	IsNull bool
}

// DataVersionReader is an optional interface a [Dispatcher] implementation
// may provide to support DataVersion-conditional writes. When the dispatcher
// implements this interface, [HandleWriteRequest] calls CurrentDataVersion
// before applying each write that carries a DataVersion field.
//
// Mirrors matter.js packages/protocol/src/action/server/AttributeWriteResponse.ts:254-257
// at db021da0 (a write whose DataVersion differs from the cluster's answers
// DataVersionMismatch before the write is dispatched), and chip
// src/app/WriteHandler.cpp AttributeAccessInterface DataVersion verification.
type DataVersionReader interface {
	// CurrentDataVersion returns the current DataVersion for the cluster at
	// (endpoint, clusterID). Returns (0, false) when it has no version
	// for the cluster; 0 with true is an ordinary version.
	CurrentDataVersion(ctx context.Context, endpoint uint16, clusterID uint32) (uint32, bool)
}

// ACLChecker is the interface a [Dispatcher] implementation provides to
// gate Read, Write and Invoke calls against the AccessControl cluster's
// ACL table. A Dispatcher without one denies every request of a session
// that is not PASE (UNSUPPORTED_ACCESS), as the group path and
// [AuthorityAt] deny: a privilege nobody could verify is not held, and
// matter.js judges every peer's request by an access control that grants
// nothing without an entry (packages/node/src/behavior/context/server/
// RemoteActorContext.ts:86-87, packages/protocol/src/interaction/
// FabricAccessControl.ts:186-205 at db91886c). A node that means to run
// without access control says so with a checker that grants —
// endpoint.UnenforcedACL. Review II, R2-P6-sec-0560, owner decision 136;
// it was permissive.
//
// Mirrors connectedhomeip src/access/AccessControl.cpp:359-554 at 6170af84
// (Check, CheckACL) and the
// access passes of matter.js at db021da0 — a write's
// packages/protocol/src/action/server/AttributeWriteResponse.ts:215-218 and
// :236-241, an invoke's CommandInvokeResponse.ts:257-260 and :273-279. chip
// src/app/WriteHandler.cpp:770 at 6170af84 says "Execute the ACL Access
// Granting Algorithm before existence checks" — the IM gate calls this
// BEFORE attribute dispatch.
type ACLChecker interface {
	// CheckACL returns [StatusUnsupportedAccess] when the requesting fabric
	// does not hold the required privilege for (endpoint, clusterID) on
	// behalf of the requesting subject. Returns [StatusSuccess] when access
	// is granted.
	//
	// fabricIndex is the requesting fabric index extracted from the session
	// context (via [FabricFilterFromContext]); it names a fabric (1 to
	// 254). The gates ask only for a request that is not on a PASE
	// session — a PASE session keeps its implicit grant by its auth mode
	// ([IsPASEFromContext]) — and refuse a request without an accessing
	// fabric before the checker is asked ([HasAccessingFabric]; review II,
	// R2-P6-doc-0561).
	//
	// subjectNodeID + subjectCATs identify the requesting peer for per-
	// subject ACE matching (Matter §9.10.5.6). For CASE sessions both are
	// lifted from the peer NOC; for PASE both are zero / nil and the
	// PASE auth mode ([WithAuthModePASE]) grants. An ACL entry with an empty Subjects
	// list matches any subject on the fabric.
	//
	// requiredPrivilege is the minimum Matter privilege level required:
	//   1=View, 3=Operate, 4=Manage, 5=Administer.
	CheckACL(ctx context.Context, fabricIndex uint8, subjectNodeID uint64, subjectCATs []uint32, endpoint uint16, clusterID uint32, requiredPrivilege uint8) StatusCode
}

// AttributeReadPrivilegeProvider is an optional interface a [Dispatcher]
// may implement to signal that a specific (endpoint, cluster, attribute)
// path requires a higher read privilege than the default View (1).
// [HandleReadRequest] calls this before dispatching each concrete ACL
// check so elevated-privilege attributes (e.g. AccessControl.ACL and
// AccessControl.Extension which require Administer per Matter §9.10.5.3)
// are protected even on read paths. Mirrors chip
// src/app/clusters/access-control-server/access-control-server.cpp
// AttributeReadAclRequired and matter.js
// packages/model/src/standard/elements/access-control.element.ts
// access: "administer".
type AttributeReadPrivilegeProvider interface {
	// MinReadPrivilege returns the minimum Matter privilege level
	// required to read attrID on (endpoint, clusterID). Return 1
	// (View) for the common case; return 5 (Administer) for
	// fabric-security-sensitive attributes like ACL/Extension.
	MinReadPrivilege(endpoint uint16, clusterID, attrID uint32) uint8
}

// AttributeWritePrivilegeProvider is an optional interface a [Dispatcher]
// may implement to signal that a specific (endpoint, cluster, attribute)
// path requires a higher write privilege than the default Operate (3).
// [HandleWriteRequest] calls this before the per-write ACL check so
// elevated-privilege attributes (e.g. AccessControl.ACL / Extension which
// require Administer, or BasicInformation.NodeLabel which requires Manage)
// cannot be written by a merely-Operate subject. Mirrors the writeAccess
// bits in matter.js packages/model/src/standard/elements/*.element.ts and
// chip's per-attribute write-ACL guards.
type AttributeWritePrivilegeProvider interface {
	// MinWritePrivilege returns the minimum Matter privilege level
	// required to write attrID on (endpoint, clusterID). Return 3
	// (Operate) for the common case; return 4 (Manage) / 5 (Administer)
	// for elevated attributes.
	MinWritePrivilege(endpoint uint16, clusterID, attrID uint32) uint8
}

// WriteAuthorizer decides whether the requesting subject may write the
// RESOLVED (endpoint, cluster, attribute). It returns [StatusSuccess]
// when access is granted and a denial status (typically
// [StatusUnsupportedAccess]) otherwise.
//
// A wildcard-endpoint write carries no endpoint on the requested path,
// so the only place its privilege can be evaluated is where the path
// resolves. Authorizing the un-expanded request instead would check a
// zero-value endpoint and then apply the value to every endpoint the
// wildcard reaches.
type WriteAuthorizer func(endpoint uint16, clusterID, attrID uint32) StatusCode

// AuthorizingWriter is an optional interface a [Dispatcher] may implement
// so [HandleWriteRequest] can gate every location a wildcard-endpoint
// write expands to BEFORE the value is applied. Post-hoc filtering of the
// returned results is not sufficient: a write mutates cluster state (and
// drives device commands) inside the expansion loop.
//
// Mirrors matter.js
// packages/protocol/src/action/server/AttributeWriteResponse.ts:324-343
// (#writeAttributeForWildcard authorizes the resolved attribute at its own
// location and returns without emitting a status on denial).
type AuthorizingWriter interface {
	// WriteAuthorized behaves like [Dispatcher.Write] but consults
	// authorize for every resolved (endpoint, cluster, attribute) before
	// the value is applied. A denied location on a wildcard-endpoint path
	// is skipped silently (Matter §8.4.3.2 — a wildcard interaction
	// discloses only authorized paths); a denied concrete path yields the
	// denial status. A nil authorize dispatches exactly like Write.
	WriteAuthorized(ctx context.Context, path ConcreteAttributePath, value AttributeValue, authorize WriteAuthorizer) []WriteResult
}

// CommandInvokePrivilegeProvider is an optional interface a [Dispatcher]
// may implement to signal that a specific (endpoint, cluster, command)
// requires a higher invoke privilege than the default Operate (3).
// [HandleInvokeRequest] calls this before the per-invoke ACL check so
// administrative commands (e.g. OperationalCredentials.RemoveFabric,
// AdministratorCommissioning.OpenCommissioningWindow) cannot be invoked
// by a merely-Operate subject. Mirrors the invokeAccess bits in matter.js
// packages/model/src/standard/elements/*.element.ts and chip's
// per-command invoke-ACL guards.
type CommandInvokePrivilegeProvider interface {
	// MinInvokePrivilege returns the minimum Matter privilege level
	// required to invoke cmdID on (endpoint, clusterID). Return 3
	// (Operate) for the common case; return 4 (Manage) / 5 (Administer)
	// for elevated commands.
	MinInvokePrivilege(endpoint uint16, clusterID, cmdID uint32) uint8
}

// TimedCommandProvider is the optional [Dispatcher] capability
// [HandleInvokeRequest] consults for whether a concrete command needs a
// timed interaction: the dispatcher asks the cluster server it resolves
// (contract.TimedCommander, answered from a generated definition's model)
// and falls back to schema.IsTimedInvoke for a hand-written server or a
// path it does not resolve. A dispatcher without it is answered by
// schema.IsTimedInvoke alone. Mirrors matter.js
// packages/protocol/src/action/server/CommandInvokeResponse.ts:292-295,
// which reads `limits.timed` from the command's model.
type TimedCommandProvider interface {
	IsTimedCommand(endpoint uint16, clusterID, cmdID uint32) bool
}

// CommandPathChecker is the optional [Dispatcher] capability
// [HandleInvokeRequest] consults for a concrete command path's existence
// before the payload check: UNSUPPORTED_ENDPOINT,
// UNSUPPORTED_CLUSTER or UNSUPPORTED_COMMAND for a path the node does not
// serve, Success otherwise. A command whose payload failed to decode on a
// cluster the endpoint does not serve owes the existence status, not the
// decode one — matter.js CommandInvokeResponse.ts #processConcrete checks
// endpoint, cluster and command before it validates the request
// (TC-IDM-1.2).
type CommandPathChecker interface {
	CommandPathStatus(path ConcreteCommandPath) StatusCode
}

// Dispatcher is the cluster-server-side surface the IM layer routes
// Read / Write / Invoke requests through. Package endpoint's
// TopologyDispatcher (endpoint.TopologyDispatcher), which the bridge
// builds for its topology, is the one in this module (review II,
// R2-P6-doc-0563).
//
// Implementations:
//
//   - MUST return [StatusUnsupportedEndpoint] for endpoints the bridge
//     does not advertise.
//   - MUST return [StatusUnsupportedCluster] when the endpoint exists
//     but the requested cluster is absent.
//   - MUST return [StatusUnsupportedAttribute] / [StatusUnsupportedCommand]
//     when the cluster exists but the requested ID is missing.
//   - SHOULD return [StatusBusy] when a transient backend condition
//     prevents servicing the request.
//
// Wildcards: when a path field has its Has* flag false, the
// implementation expands across every concrete value matching. The
// returned Reads slice carries one (path, value, status) per concrete
// match.
type Dispatcher interface {
	// Read resolves attribute reads. The path may carry wildcards;
	// the implementation expands them to one ReadResult per match.
	Read(ctx context.Context, path ConcreteAttributePath) []ReadResult

	// Write resolves a single attribute write. Unlike Read, write
	// paths are typically concrete; a wildcard write fans out to
	// multiple WriteResults (one per matching attribute).
	Write(ctx context.Context, path ConcreteAttributePath, value AttributeValue) []WriteResult

	// Invoke dispatches a single command. fields is the decoded
	// cluster-native struct for the command's request payload (or
	// nil for parameterless commands). The response value is the
	// cluster-native struct for the command's response (or nil for
	// status-only commands).
	Invoke(ctx context.Context, path ConcreteCommandPath, fields any) InvokeResult
}

// ReadResult is one (path, value, status) tuple produced by a Read.
// Wildcard reads fan out into multiple ReadResults — one per
// (endpoint, cluster, attribute) match.
type ReadResult struct {
	Path   ConcreteAttributePath
	Value  AttributeValue
	Status StatusCode
	// DataVersion carries the per-cluster DataVersion that the cluster
	// reported at read time when HasDataVersion is set; 0 is then an
	// ordinary version, as every value is in matter.js
	// (packages/node/src/behavior/state/managed/Datasource.ts:597,
	// :695-697). endpoint.TopologyDispatcher reports one for every
	// cluster instance.
	DataVersion uint32
	// HasDataVersion reports that DataVersion is the cluster's version.
	// Without it the result has no version: it is never left out for a
	// DataVersionFilter, and its AttributeDataIB carries 0.
	HasDataVersion bool
	// FabricScope, when set, is the scope of a request's read of a
	// fabric-scoped list attribute: Value is what the server returned,
	// every fabric's entries whole, and the scope is applied to it when it
	// is encoded ([MaskFabricScoped]; [AttributeReport.FabricScope]).
	// endpoint.TopologyDispatcher sets it for every server on a request; a
	// read with no request behind it has none and reads the value whole.
	FabricScope *FabricScope
}

// WriteResult is one (path, status) pair produced by a Write.
type WriteResult struct {
	Path   ConcreteAttributePath
	Status StatusCode
	// ClusterStatus carries the cluster-specific status byte from
	// Matter §10.6.2.2 StatusIB. Surfaced when a cluster server
	// returns an error that implements [MatterClusterStatusError]
	// (e.g. AdministratorCommissioning §11.19.7.3 PAKEParameterError
	// = 0x02). Zero + HasClusterStatus=false means "no cluster-specific
	// status" — wire encoding omits the ClusterStatus tag.
	ClusterStatus    uint8
	HasClusterStatus bool
	// Commit is the verdict of a successful write: the attributes of the
	// cluster it changed ([WriteCommit]). nil means the dispatcher gave
	// no verdict, and the written attribute is taken to have changed; a
	// Commit whose Changed is empty is a write that changed nothing. A
	// refused write carries none. endpoint.TopologyDispatcher gives one
	// for every write it applies.
	Commit *WriteCommit
}

// WriteCommit is the verdict of one applied write: the ids of the
// written cluster's attributes the write changed, on the endpoint and
// cluster of [WriteResult.Path]. The written attribute is among them only
// when its value moved; another attribute the write moved with it is
// named when the server names it (contract.ChangeNamingWriter) —
// spec.Server names a sibling its Sink set inside the write. An empty Changed is a write that changed nothing: no
// DataVersion advance and no report. It is matter.js's per-commit change
// list (packages/node/src/behavior/state/managed/Datasource.ts:1476-1484
// at db021da0, a name noted only when its value differs; :1319, the list
// handed on), carried in the result instead of an observer.
type WriteCommit struct {
	Changed []uint32
}

// InvokeResult is the outcome of a single command dispatch.
type InvokeResult struct {
	Path     ConcreteCommandPath
	Response any // cluster-native response struct or nil for status-only
	Status   StatusCode
	// ClusterStatus carries the cluster-specific status byte per
	// Matter §10.6.2.2 — see [WriteResult.ClusterStatus] for the
	// envelope semantics.
	ClusterStatus    uint8
	HasClusterStatus bool
}

// maxPayloadCtxKey carries the largest payload one message of the
// request's exchange carries.
type maxPayloadCtxKey struct{}

// WithMaxPayloadSize stamps the largest payload one message of the
// request's exchange carries: its channel's largest message less the
// message overhead — matter.js MessageExchange.maxPayloadSize
// (packages/protocol/src/protocol/MessageExchange.ts:465-469 at db021da0,
// MATTER_MESSAGE_OVERHEAD :182), 1232 - 54 over UDP and BLE, the
// connection's largest message less 54 over TCP. A server that sizes a
// response by its exchange reads it with [MaxPayloadSizeFromContext], as
// matter.js's GeneralDiagnostics payloadTestRequest reads
// `exchange.maxPayloadSize`.
func WithMaxPayloadSize(ctx context.Context, n int) context.Context {
	return context.WithValue(ctx, maxPayloadCtxKey{}, n)
}

// MaxPayloadSizeFromContext reports the stamp of [WithMaxPayloadSize];
// ok is false when the caller stamped nothing.
func MaxPayloadSizeFromContext(ctx context.Context) (n int, ok bool) {
	n, ok = ctx.Value(maxPayloadCtxKey{}).(int)
	return n, ok
}

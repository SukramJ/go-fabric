// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package im

import (
	"context"
	"errors"
	"fmt"

	"github.com/SukramJ/go-fabric/schema"

	"github.com/SukramJ/go-fabric/tlv"
)

// InvokeRequestMessage tag numbers (Matter Core Spec §10.6.7).
const (
	tagInvokeReqSuppressResponse uint8 = 0
	tagInvokeReqTimedRequest     uint8 = 1
	tagInvokeReqInvokeRequests   uint8 = 2
	// tagInvokeReqDelayReportData is Matter 1.7's DelayReportData
	// (matter.js packages/types/src/protocol/messages/TlvInvokeRequest.ts,
	// 67be3a83 #4569): decoded and validated, not acted on — see
	// [InvokeRequest.DelayReportData].
	tagInvokeReqDelayReportData uint8 = 3
)

// InvokeResponseMessage tag numbers.
const (
	tagInvokeRespSuppressResponse uint8 = 0
	tagInvokeRespResponses        uint8 = 1
	tagInvokeRespMoreChunked      uint8 = 2
)

// DefaultMaxPathsPerInvoke is the ceiling on CommandDataIBs a single
// InvokeRequest batch may carry. Mirrors matter.js
// packages/types/src/protocol/definitions/interaction.ts
// DEFAULT_MAX_PATHS_PER_INVOKE — the same value the bridge advertises
// via BasicInformation.MaxPathsPerInvoke (0x0016,
// cluster/core/basic_information.go) and the CASE SessionParameters
// negotiation. Defined here (the protocol layer) rather than in
// cluster/core so [ValidateInvokeBatch] can enforce the exact number
// the bridge advertises without an import cycle.
const DefaultMaxPathsPerInvoke = 10

// CommandDataIB / CommandStatusIB tag numbers.
const (
	tagCmdDataPath   uint8 = 0
	tagCmdDataFields uint8 = 1
	tagCmdDataRef    uint8 = 2

	tagCmdStatusPath   uint8 = 0
	tagCmdStatusStatus uint8 = 1
	tagCmdStatusRef    uint8 = 2

	// InvokeResponseIB choice tags.
	tagInvokeRespCommandData   uint8 = 0
	tagInvokeRespCommandStatus uint8 = 1
)

// Errors.
var (
	// ErrInvalidInvokeRequest is returned for malformed invokes.
	ErrInvalidInvokeRequest = errors.New("im: invalid InvokeRequest")
)

// InvokeRequest is the in-memory form of an InvokeRequestMessage.
type InvokeRequest struct {
	SuppressResponse bool
	TimedRequest     bool
	Invokes          []CommandInvocation
	// DelayReportData is the request's DelayReportData field, nil when
	// absent. matter.js always decodes it and refuses a malformed one; the
	// bridge defers the subscription reports it asks for
	// (BD-Matter-DelayReportData).
	DelayReportData *DelayReportData
}

// DelayReportData is the DelayReportData structure of an InvokeRequest
// (matter.js TlvDelayReportData): both fields uint16 milliseconds,
// default 0.
type DelayReportData struct {
	DelayMinMs          uint16
	DelayJitterWindowMs uint16
}

// invokeDecodeStatusError is a malformed InvokeRequest field with the
// status matter.js answers it with: a wrong type is a
// ValidationDatatypeMismatchError (InvalidAction), a value outside the
// field's range a ValidationError (ConstraintError).
type invokeDecodeStatusError struct {
	msg    string
	status StatusCode
}

func (e invokeDecodeStatusError) Error() string                { return e.msg }
func (e invokeDecodeStatusError) MatterStatusCode() StatusCode { return e.status }
func (invokeDecodeStatusError) Is(target error) bool           { return target == ErrInvalidInvokeRequest }

// readDelayReportData decodes the DelayReportData structure whose opening
// element el is.
func readDelayReportData(dec *tlv.Decoder, el tlv.Element) (*DelayReportData, error) {
	if !el.IsContainer || el.Type != tlv.TypeStructure {
		if el.IsContainer {
			_ = skipContainer(dec)
		}
		return nil, invokeDecodeStatusError{"im: invalid InvokeRequest: DelayReportData not a structure", StatusInvalidAction}
	}
	out := &DelayReportData{}
	for {
		f, err := dec.Next()
		if err != nil {
			return nil, fmt.Errorf("%w: DelayReportData: %w", ErrInvalidInvokeRequest, err)
		}
		if f.IsEndContainer {
			return out, nil
		}
		if f.IsContainer {
			if err := skipContainer(dec); err != nil {
				return nil, err
			}
		}
		if f.Tag.Kind != tlv.TagKindContext || f.Tag.Number > 1 {
			continue
		}
		if f.Type < tlv.TypeUnsignedInt1 || f.Type > tlv.TypeUnsignedInt8 {
			return nil, invokeDecodeStatusError{"im: invalid InvokeRequest: DelayReportData field is not an unsigned integer", StatusInvalidAction}
		}
		if f.Uint > 0xFFFF {
			return nil, invokeDecodeStatusError{"im: invalid InvokeRequest: DelayReportData field exceeds uint16", StatusConstraintError}
		}
		if f.Tag.Number == 0 {
			out.DelayMinMs = uint16(f.Uint)
		} else {
			out.DelayJitterWindowMs = uint16(f.Uint)
		}
	}
}

// CommandInvocation is one entry in InvokeRequests.
type CommandInvocation struct {
	Path          ConcreteCommandPath
	Fields        any // cluster-native struct produced by [CommandFieldsReader]
	CommandRef    uint16
	HasCommandRef bool
	// DecodeStatus is non-Success when the [CommandFieldsReader] rejected
	// the command's fields with a typed [StatusCodeError] (a value outside
	// its schema type, for instance). Fields is nil then, and
	// [HandleInvokeRequest] answers the command with that status instead
	// of invoking it — the per-command outcome matter.js produces when
	// requestTlv.validate throws (packages/protocol/src/action/server/
	// CommandInvokeResponse.ts:446-448, :472-496).
	DecodeStatus StatusCode
}

// CommandFieldsReader extracts the cluster-native fields struct from
// the TLV stream. The IM layer does not own cluster-native decoding;
// the implementation switches on `path.Cluster` / `path.Command` and
// pulls elements from `dec` until the matching EndContainer arrives.
//
// `el` is the FIELDS container's opening element (TLV TypeStructure);
// `dec` is positioned just inside that container — calling
// `dec.Next()` returns the first field. Path is supplied so a single
// reader can dispatch across all cluster commands without an
// out-of-band cluster registry.
//
// Implementations that don't recognise the (cluster, command)
// pair MUST drain the container via the decoder's skip helper and
// return (nil, nil); the IM layer treats that as "fields skipped"
// and propagates path-only Invoke to the cluster server.
//
// A typed reject (an error carrying a [StatusCodeError]) normally leaves
// `dec` inside the fields container and the IM layer drains the rest. A
// reject the reader can only reach at the container's EndContainer — a
// mandatory field found missing — has consumed it already; such an error
// also implements [FieldsContainerConsumed] so the container is not
// drained twice.
type CommandFieldsReader func(path ConcreteCommandPath, dec *tlv.Decoder, el tlv.Element) (any, error)

// FieldsContainerConsumed is implemented by a [CommandFieldsReader] reject
// returned after the reader consumed the fields container's EndContainer.
type FieldsContainerConsumed interface {
	error
	FieldsContainerConsumed() bool
}

// fieldsContainerConsumed reports whether err says the reader already
// consumed the fields container.
func fieldsContainerConsumed(err error) bool {
	c, ok := errors.AsType[FieldsContainerConsumed](err)
	return ok && c.FieldsContainerConsumed()
}

// UnmarshalInvokeRequestTLV decodes an InvokeRequestMessage.
func UnmarshalInvokeRequestTLV(dec *tlv.Decoder, fieldsReader CommandFieldsReader) (InvokeRequest, error) {
	open, err := dec.Next()
	if err != nil {
		return InvokeRequest{}, err
	}
	if !open.IsContainer || open.Type != tlv.TypeStructure {
		return InvokeRequest{}, fmt.Errorf("%w: expected struct, got 0x%02X", ErrInvalidInvokeRequest, open.Type)
	}
	var req InvokeRequest
	for {
		el, err := dec.Next()
		if err != nil {
			return InvokeRequest{}, fmt.Errorf("%w: %w", ErrInvalidInvokeRequest, err)
		}
		if el.IsEndContainer {
			return req, nil
		}
		if el.Tag.Kind != tlv.TagKindContext {
			continue
		}
		switch uint8(el.Tag.Number & 0xFF) {
		case tagInvokeReqSuppressResponse:
			req.SuppressResponse = el.Bool
		case tagInvokeReqTimedRequest:
			req.TimedRequest = el.Bool
		case tagInvokeReqInvokeRequests:
			if !el.IsContainer || el.Type != tlv.TypeArray {
				return InvokeRequest{}, fmt.Errorf("%w: InvokeRequests not array", ErrInvalidInvokeRequest)
			}
			invokes, err := readCommandData(dec, fieldsReader)
			if err != nil {
				return InvokeRequest{}, err
			}
			req.Invokes = invokes
		case tagInvokeReqDelayReportData:
			drd, err := readDelayReportData(dec, el)
			if err != nil {
				return InvokeRequest{}, err
			}
			req.DelayReportData = drd
		default:
			if el.IsContainer {
				if err := skipContainer(dec); err != nil {
					return InvokeRequest{}, err
				}
			}
		}
	}
}

func readCommandData(dec *tlv.Decoder, fieldsReader CommandFieldsReader) ([]CommandInvocation, error) {
	var out []CommandInvocation
	for {
		el, err := dec.Next()
		if err != nil {
			return nil, err
		}
		if el.IsEndContainer {
			return out, nil
		}
		if !el.IsContainer || el.Type != tlv.TypeStructure {
			return nil, fmt.Errorf("%w: CommandDataIB not struct", ErrInvalidInvokeRequest)
		}
		inv, err := readCommandInvocation(dec, fieldsReader)
		if err != nil {
			return nil, err
		}
		out = append(out, inv)
	}
}

func readCommandInvocation(dec *tlv.Decoder, fieldsReader CommandFieldsReader) (CommandInvocation, error) {
	var inv CommandInvocation
	for {
		el, err := dec.Next()
		if err != nil {
			return CommandInvocation{}, err
		}
		if el.IsEndContainer {
			return inv, nil
		}
		if el.Tag.Kind != tlv.TagKindContext {
			continue
		}
		switch uint8(el.Tag.Number & 0xFF) {
		case tagCmdDataPath:
			if !el.IsContainer || el.Type != tlv.TypeList {
				return CommandInvocation{}, fmt.Errorf("%w: CommandPathIB not list", ErrInvalidInvokeRequest)
			}
			p, err := readCommandPathFields(dec)
			if err != nil {
				return CommandInvocation{}, err
			}
			inv.Path = p
		case tagCmdDataFields:
			if err := readCommandFields(dec, el, fieldsReader, &inv); err != nil {
				return CommandInvocation{}, err
			}
		case tagCmdDataRef:
			inv.CommandRef = uint16(el.Uint & 0xFFFF)
			inv.HasCommandRef = true
		}
	}
}

// readCommandFields reads the CommandFields element el of inv: the
// cluster-native fields, or the per-command status a refused payload
// carries.
func readCommandFields(dec *tlv.Decoder, el tlv.Element, fieldsReader CommandFieldsReader, inv *CommandInvocation) error {
	if fieldsReader == nil {
		// No cluster-native reader wired — skip the container.
		// The dispatcher fan-out can still route on path alone
		// (ArmFailSafe / disarm flows, where the path uniquely
		// identifies the command and fields carry only optional
		// arguments). Without this branch the nil-reader call
		// below panics.
		if el.IsContainer {
			if err := skipContainer(dec); err != nil {
				return fmt.Errorf("%w: skip fields: %w", ErrInvalidInvokeRequest, err)
			}
		}
		return nil
	}
	// CommandFields is a structure (Matter §10.6.12); a payload of
	// another type cannot decode against any command's schema and
	// is the command's INVALID_COMMAND, the rest of the batch
	// unaffected. Mirrors matter.js
	// packages/protocol/src/action/server/CommandInvokeResponse.ts:551-562
	// at 862ad75f (#decodeWithSchema: InvalidCommandError); the
	// reader read past it and the request failed as a whole.
	if notStruct, err := skipNonStructFields(dec, el); notStruct || err != nil {
		if err != nil {
			return err
		}
		inv.Fields, inv.DecodeStatus = nil, StatusInvalidCommand
		return nil
	}
	f, err := fieldsReader(inv.Path, dec, el)
	if err != nil {
		// A typed reject is a per-command outcome, not a malformed
		// request: drain the rest of the fields container so the
		// remaining commands of the batch still decode, and carry
		// the status to the dispatcher.
		var sce StatusCodeError
		if !errors.As(err, &sce) || sce.MatterStatusCode().IsSuccess() {
			return fmt.Errorf("%w: fields: %w", ErrInvalidInvokeRequest, err)
		}
		if el.IsContainer && !fieldsContainerConsumed(err) {
			if err := skipContainer(dec); err != nil {
				return fmt.Errorf("%w: fields: %w", ErrInvalidInvokeRequest, err)
			}
		}
		inv.Fields = nil
		inv.DecodeStatus = sce.MatterStatusCode()
		return nil
	}
	inv.Fields = f
	return nil
}

// skipNonStructFields reports whether the CommandFields element el is
// something other than a structure, consuming it when it is a container.
func skipNonStructFields(dec *tlv.Decoder, el tlv.Element) (bool, error) {
	if el.IsContainer && el.Type == tlv.TypeStructure {
		return false, nil
	}
	if el.IsContainer {
		if err := skipContainer(dec); err != nil {
			return true, fmt.Errorf("%w: fields: %w", ErrInvalidInvokeRequest, err)
		}
	}
	return true, nil
}

func readCommandPathFields(dec *tlv.Decoder) (ConcreteCommandPath, error) {
	var p ConcreteCommandPath
	for {
		el, err := dec.Next()
		if err != nil {
			return ConcreteCommandPath{}, err
		}
		if el.IsEndContainer {
			if !p.HasCluster || !p.HasCommand {
				return ConcreteCommandPath{}, fmt.Errorf("%w: missing cluster/command", ErrInvalidPath)
			}
			return p, nil
		}
		if el.Tag.Kind != tlv.TagKindContext {
			continue
		}
		switch uint8(el.Tag.Number & 0xFF) {
		case tagCmdPathEndpoint:
			p.Endpoint = uint16(el.Uint & 0xFFFF)
			p.HasEndpoint = true
		case tagCmdPathCluster:
			p.Cluster = uint32(el.Uint & 0xFFFFFFFF)
			p.HasCluster = true
		case tagCmdPathCommand:
			p.Command = uint32(el.Uint & 0xFFFFFFFF)
			p.HasCommand = true
		}
	}
}

// InvokeResponse is the in-memory form of an InvokeResponseMessage.
type InvokeResponse struct {
	SuppressResponse bool
	Responses        []InvokeResponseEntry
	// MoreChunkedMessages marks every chunk of a chunked InvokeResponse
	// but the last (Matter §10.7.2; matter.js TlvInvokeResponse tag 2).
	MoreChunkedMessages bool
}

// InvokeResponseEntry is one element in InvokeResponses — either a
// CommandData (with response payload) or a CommandStatus.
type InvokeResponseEntry struct {
	Path          ConcreteCommandPath
	Response      any
	HasResponse   bool
	Status        StatusIB
	IsStatus      bool
	CommandRef    uint16
	HasCommandRef bool
}

// HasCommandData reports whether any entry is a CommandDataIB (a command
// that produced a response payload) rather than a CommandStatusIB. Per
// Matter §8.8.3.2.1 a SuppressResponse invoke still ships a response when a
// CommandDataIB is generated; when the response carries only status entries
// the server sends nothing. Mirrors matter.js
// packages/node/src/node/server/InteractionServer.ts:1043-1074
// (`suppressedBuffer` is flushed on the first `cmd-response`; otherwise the
// invoke returns without sending).
func (ir InvokeResponse) HasCommandData() bool {
	for i := range ir.Responses {
		if !ir.Responses[i].IsStatus {
			return true
		}
	}
	return false
}

// ValidateInvokeBatch enforces the batch-invoke path rules a controller may
// violate when it packs more than one CommandDataIB into a single
// InvokeRequest. Returns StatusInvalidAction on the first offending command,
// else StatusSuccess. A single-command invoke always passes. Mirrors matter.js
// packages/protocol/src/action/server/CommandInvokeResponse.ts:64-92,171-185
// (CommandInvokeResponse.process / #processConcrete):
//   - the batch must not exceed [DefaultMaxPathsPerInvoke] paths, checked
//     before any per-command validation or dispatch (matter.js
//     packages/node/src/node/server/InteractionServer.ts:950-955
//     `if (invokeRequests.length > this.#maxPathsPerInvoke) throw
//     new StatusResponseError(..., Status.InvalidAction)`);
//   - a wildcard-endpoint path is illegal in a batch (matter.js "Wildcard path
//     must not be used with multiple invokes");
//   - every concrete path in a batch MUST carry a CommandRef ("The CommandRef
//     field must be specified for all commands in a batch invoke");
//   - duplicate concrete (endpoint, cluster, command) paths are rejected
//     ("Duplicate concrete command path on batch invoke");
//   - duplicate CommandRefs are rejected ("Duplicate commandRef on batch
//     invoke").
//
// The wire decoder guarantees every command path carries a cluster and a
// command, so a "wildcard" here is a path without a concrete endpoint.
func ValidateInvokeBatch(req InvokeRequest) StatusCode {
	if len(req.Invokes) > DefaultMaxPathsPerInvoke {
		return StatusInvalidAction
	}
	multiple := len(req.Invokes) > 1
	seenPaths := make(map[[3]uint32]struct{}, len(req.Invokes))
	seenRefs := make(map[uint16]struct{}, len(req.Invokes))
	for i := range req.Invokes {
		inv := req.Invokes[i]
		if !inv.Path.HasEndpoint {
			// Wildcard-endpoint invoke: legal only as the sole command.
			if multiple {
				return StatusInvalidAction
			}
			continue
		}
		if multiple && !inv.HasCommandRef {
			return StatusInvalidAction
		}
		key := [3]uint32{uint32(inv.Path.Endpoint), inv.Path.Cluster, inv.Path.Command}
		if _, dup := seenPaths[key]; dup {
			return StatusInvalidAction
		}
		seenPaths[key] = struct{}{}
		if inv.HasCommandRef {
			if _, dup := seenRefs[inv.CommandRef]; dup {
				return StatusInvalidAction
			}
			seenRefs[inv.CommandRef] = struct{}{}
		}
	}
	return StatusSuccess
}

// CommandFieldsWriter is the encoder counterpart of [CommandFieldsReader].
type CommandFieldsWriter func(enc *tlv.Encoder, tag tlv.Tag, fields any)

// MarshalTLV encodes ir. fieldsWriter is invoked for each non-status
// response that carries a payload.
func (ir InvokeResponse) MarshalTLV(enc *tlv.Encoder, fieldsWriter CommandFieldsWriter) {
	enc.StartStruct(tlv.AnonymousTag())
	enc.PutBool(tlv.ContextTag(tagInvokeRespSuppressResponse), ir.SuppressResponse)
	enc.StartArray(tlv.ContextTag(tagInvokeRespResponses))
	for _, ent := range ir.Responses {
		ent.marshal(enc, fieldsWriter)
	}
	_ = enc.EndContainer()
	if ir.MoreChunkedMessages {
		enc.PutBool(tlv.ContextTag(tagInvokeRespMoreChunked), true)
	}
	enc.PutUint(tlv.ContextTag(tagInteractionModelRevision), uint64(MatterInteractionModelRevision))
	_ = enc.EndContainer()
}

func (ent InvokeResponseEntry) marshal(enc *tlv.Encoder, fieldsWriter CommandFieldsWriter) {
	enc.StartStruct(tlv.AnonymousTag())
	if ent.IsStatus {
		enc.StartStruct(tlv.ContextTag(tagInvokeRespCommandStatus))
		ent.Path.MarshalTLV(enc, tlv.ContextTag(tagCmdStatusPath))
		ent.Status.MarshalTLV(enc, tlv.ContextTag(tagCmdStatusStatus))
		if ent.HasCommandRef {
			enc.PutUint(tlv.ContextTag(tagCmdStatusRef), uint64(ent.CommandRef))
		}
		_ = enc.EndContainer()
	} else {
		enc.StartStruct(tlv.ContextTag(tagInvokeRespCommandData))
		ent.Path.MarshalTLV(enc, tlv.ContextTag(tagCmdDataPath))
		if ent.HasResponse {
			fieldsWriter(enc, tlv.ContextTag(tagCmdDataFields), ent.Response)
		}
		if ent.HasCommandRef {
			enc.PutUint(tlv.ContextTag(tagCmdDataRef), uint64(ent.CommandRef))
		}
		_ = enc.EndContainer()
	}
	_ = enc.EndContainer()
}

// pathStatusEntry is the CommandStatusIB answering inv with status.
func pathStatusEntry(inv CommandInvocation, status StatusCode) InvokeResponseEntry {
	return InvokeResponseEntry{
		Path:          ConcreteCommandPath{Endpoint: inv.Path.Endpoint, Cluster: inv.Path.Cluster, Command: inv.Path.Command, HasEndpoint: true, HasCluster: true, HasCommand: true},
		CommandRef:    inv.CommandRef,
		HasCommandRef: inv.HasCommandRef,
		IsStatus:      true,
		Status:        StatusIB{Status: status},
	}
}

// HandleInvokeRequest dispatches a parsed InvokeRequest through d and
// returns the assembled InvokeResponse.
//
// ACL gate: mirrors matter.js
// packages/node/src/node/server/OnlineServerInteraction.ts
// FabricAccessControl.forRequest and chip src/app/CommandHandler.cpp
// "Execute the ACL Access Granting Algorithm before existence checks".
// When d implements [ACLChecker], the requesting fabric's privilege is
// verified before each command path is dispatched. The required
// privilege for a plain Invoke is Operate (3) per Matter §9.10.4.4;
// individual cluster servers may enforce additional Manage/Admin gates
// internally for sensitive commands such as RemoveFabric. fabricIndex
// is extracted via [FabricFilterFromContext]; a PASE session bypasses
// the ACL check so commissioning invokes (ArmFailSafe,
// CommissioningComplete) arrive before the fabric's ACL entry exists —
// fabricIndex==0 before AddNOC, [IsPASEFromContext] after it adopted the
// session onto the new fabric.
func HandleInvokeRequest(ctx context.Context, d Dispatcher, req InvokeRequest) InvokeResponse {
	g := newInvokeGate(ctx, d)
	// The access check a cluster server may consult for a privilege
	// beyond the command's own ([AuthorityAt]): the same gate the command
	// itself passed — PASE holds the implicit Administer, a CASE subject
	// what the ACL grants, and nothing is granted without an ACL source.
	ctx = WithAuthority(ctx, func(endpoint uint16, clusterID uint32, privilege uint8) StatusCode {
		if g.pase || g.fabricIndex == 0 {
			return StatusSuccess
		}
		if !g.hasACL {
			return StatusUnsupportedAccess
		}
		return g.aclChecker.CheckACL(g.ctx, g.fabricIndex, g.subjectNodeID, g.subjectCATs, endpoint, clusterID, privilege)
	})

	var ir InvokeResponse
	// The InvokeResponseMessage.SuppressResponse field is deprecated and matter.js
	// always emits it as false (packages/node/src/node/server/InteractionServer.ts:987-988
	// `emptyInvokeResponse.suppressResponse: false`). The controller's suppress-response
	// intent governs whether the bridge sends the InvokeResponse at all (handled at the
	// dispatch layer via [InvokeResponse.HasCommandData] per Matter §8.8.3.2.1), not the
	// value of this wire field — so it stays at its zero value here.
	for _, inv := range req.Invokes {
		// A wildcard-endpoint command runs on every endpoint that serves
		// the cluster, accepts the command and on which the session may
		// invoke it; an endpoint it may not is skipped, not reported.
		// Mirrors matter.js
		// packages/protocol/src/action/server/CommandInvokeResponse.ts:131-170
		// (#processWildcard) and :337-412 (#processEndpointForWildcard,
		// #wildcardTargetOf: the command's privilege, its fabric scope,
		// its timed quality) at 862ad75f. It answered UNSUPPORTED_ENDPOINT.
		if wild, ok := d.(AuthorizingInvoker); ok && !inv.Path.HasEndpoint && inv.DecodeStatus.IsSuccess() {
			for _, res := range wild.InvokeAuthorized(ctx, inv.Path, inv.Fields, g.wildcardTarget) {
				ir.Responses = append(ir.Responses, resultEntry(inv, res))
			}
			continue
		}
		if status := g.concrete(inv); !status.IsSuccess() {
			ir.Responses = append(ir.Responses, pathStatusEntry(inv, status))
			continue
		}
		// Fields the reader rejected never reach the cluster server: the
		// command owes the status the reader attached (ConstraintError for
		// an out-of-range value), the way matter.js answers a failed
		// requestTlv.validate (CommandInvokeResponse.ts:472-496).
		if !inv.DecodeStatus.IsSuccess() {
			ir.Responses = append(ir.Responses, pathStatusEntry(inv, inv.DecodeStatus))
			continue
		}
		ir.Responses = append(ir.Responses, resultEntry(inv, d.Invoke(ctx, inv.Path, inv.Fields)))
	}
	return ir
}

// DispatchEndpoints returns the endpoints req would dispatch a command to
// in ctx: each concrete path that passes the checks [HandleInvokeRequest]
// makes before dispatching (access, fabric scope, timed quality,
// existence), and every endpoint a wildcard path expands to. A path that
// fails a check contributes nothing, and nothing runs. It is matter.js's
// dispatch-endpoint set, collected in CommandInvokeResponse's validation
// pass and handed to its dispatch hook before any command runs
// (packages/protocol/src/action/server/CommandInvokeResponse.ts:159-165
// and :297 at 862ad75f), the endpoints a DelayReportData defers.
func DispatchEndpoints(ctx context.Context, d Dispatcher, req InvokeRequest) map[uint16]bool {
	g := newInvokeGate(ctx, d)
	out := map[uint16]bool{}
	for _, inv := range req.Invokes {
		if !inv.Path.HasEndpoint {
			if t, ok := d.(CommandTargeter); ok {
				for _, ep := range t.CommandTargets(inv.Path, g.wildcardTarget) {
					out[ep] = true
				}
			}
			continue
		}
		if g.concrete(inv).IsSuccess() {
			out[inv.Path.Endpoint] = true
		}
	}
	return out
}

// CommandTargeter is an optional interface a [Dispatcher] implements to
// name the endpoints a wildcard-endpoint command would run on, without
// running it: those [AuthorizingInvoker.InvokeAuthorized] dispatches to.
type CommandTargeter interface {
	CommandTargets(path ConcreteCommandPath, authorize CommandAuthorizer) []uint16
}

// invokeGate holds what an invoke is checked against before a command is
// dispatched: the session's fabric, subject and auth mode, the timed
// interaction, and the dispatcher's access, privilege, timed and path
// sources.
type invokeGate struct {
	ctx             context.Context
	fabricIndex     uint8
	subjectNodeID   uint64
	subjectCATs     []uint32
	pase            bool
	aclChecker      ACLChecker
	hasACL          bool
	privProvider    CommandInvokePrivilegeProvider
	hasPrivProvider bool
	pathChecker     CommandPathChecker
	hasPathChecker  bool
	timedProvider   TimedCommandProvider
	hasTimed        bool
	timed           bool
	timedKnown      bool
}

func newInvokeGate(ctx context.Context, d Dispatcher) *invokeGate {
	g := &invokeGate{ctx: ctx}
	_, g.fabricIndex = FabricFilterFromContext(ctx)
	g.subjectNodeID, g.subjectCATs = SubjectFromContext(ctx)
	g.pase = IsPASEFromContext(ctx)
	g.aclChecker, g.hasACL = d.(ACLChecker)
	g.privProvider, g.hasPrivProvider = d.(CommandInvokePrivilegeProvider)
	g.pathChecker, g.hasPathChecker = d.(CommandPathChecker)
	g.timedProvider, g.hasTimed = d.(TimedCommandProvider)
	g.timed, g.timedKnown = TimedInteractionFromContext(ctx)
	return g
}

// isTimed reports whether the command needs a timed interaction: the
// resolved server answers from its model first ([TimedCommandProvider]),
// the generated schema table otherwise.
func (g *invokeGate) isTimed(p ConcreteCommandPath) bool {
	if g.hasTimed && p.HasEndpoint {
		return g.timedProvider.IsTimedCommand(p.Endpoint, p.Cluster, p.Command)
	}
	return schema.IsTimedInvoke(p.Cluster, p.Command)
}

// invokePrivilege returns the minimum privilege needed to invoke the
// given command. Falls back to Operate (3) — the Matter §9.10.4.4
// default invoke privilege — when no CommandInvokePrivilegeProvider is
// wired or the command has no elevated requirement.
func (g *invokeGate) invokePrivilege(p ConcreteCommandPath) uint8 {
	const privilegeOperate uint8 = 3
	if g.hasPrivProvider {
		return g.privProvider.MinInvokePrivilege(p.Endpoint, p.Cluster, p.Command)
	}
	return privilegeOperate
}

// concrete returns Success when the command of a concrete path may be
// dispatched, otherwise the status its path answers.
func (g *invokeGate) concrete(inv CommandInvocation) StatusCode {
	// ACL gate. PASE skips ACL: commissioning invokes arrive before
	// the fabric's ACL entry exists (fabricIndex==0) and continue
	// over the same PASE channel after AddNOC adopted it onto the
	// new fabric ([IsPASEFromContext]) — matter.js
	// packages/protocol/src/interaction/FabricAccessControl.ts:189-191
	// keys the implicit Administer grant on the auth mode. The
	// required privilege is per-command (RemoveFabric,
	// OpenCommissioningWindow, … → Administer) rather than a flat
	// Operate, so an Operate-only subject cannot invoke an
	// administrative command.
	if g.hasACL && !g.pase && g.fabricIndex != 0 {
		if status := g.aclChecker.CheckACL(g.ctx, g.fabricIndex, g.subjectNodeID, g.subjectCATs, inv.Path.Endpoint, inv.Path.Cluster, g.invokePrivilege(inv.Path)); !status.IsSuccess() {
			return status
		}
	}
	// A fabric-scoped command needs an accessing fabric: invoked on a
	// session without one — PASE before AddNOC — it answers
	// UnsupportedAccess. Mirrors matter.js CommandInvokeResponse.ts:287
	// (`limits.fabricScoped && !this.session.fabric`). Found by the CHIP
	// Python harness: TC-IDM-1.2 step 5 sends KeySetRead over PASE.
	if g.fabricIndex == 0 && schema.IsFabricScopedInvoke(inv.Path.Cluster, inv.Path.Command) {
		return StatusUnsupportedAccess
	}
	// A timed-required command ("T") outside a timed interaction answers
	// NeedsTimedInteraction for its own path, after the access and
	// fabric checks — matter.js CommandInvokeResponse.ts:292-295.
	if g.timedKnown && !g.timed && g.isTimed(inv.Path) {
		return StatusNeedsTimedInteraction
	}
	// Existence before the payload check: matter.js
	// CommandInvokeResponse.ts #processConcrete answers
	// UnsupportedEndpoint / UnsupportedCluster / UnsupportedCommand
	// before it validates the request, so a payload a generated decoder
	// refused on a cluster the endpoint does not serve still answers
	// UnsupportedCluster (TC-IDM-1.2). matter.js checks existence ahead
	// of the fabric-scope and timed gates above too; those keep their
	// place here, pinned by the bridge's timed conformance tests.
	if g.hasPathChecker && inv.Path.HasEndpoint {
		if status := g.pathChecker.CommandPathStatus(inv.Path); !status.IsSuccess() {
			return status
		}
	}
	return StatusSuccess
}

// wildcardTarget is the [CommandAuthorizer] of a wildcard path's
// endpoints: the command's privilege, its fabric scope and its timed
// quality at the endpoint (#wildcardTargetOf,
// CommandInvokeResponse.ts:367-412 at 862ad75f).
func (g *invokeGate) wildcardTarget(endpoint uint16, clusterID, commandID uint32) StatusCode {
	at := ConcreteCommandPath{Endpoint: endpoint, Cluster: clusterID, Command: commandID, HasEndpoint: true, HasCluster: true, HasCommand: true}
	if g.hasACL && !g.pase && g.fabricIndex != 0 {
		if status := g.aclChecker.CheckACL(g.ctx, g.fabricIndex, g.subjectNodeID, g.subjectCATs, endpoint, clusterID, g.invokePrivilege(at)); !status.IsSuccess() {
			return status
		}
	}
	if g.fabricIndex == 0 && schema.IsFabricScopedInvoke(clusterID, commandID) {
		return StatusUnsupportedAccess
	}
	if g.timedKnown && !g.timed && g.isTimed(at) {
		return StatusNeedsTimedInteraction
	}
	return StatusSuccess
}

// resultEntry is the InvokeResponse entry of a dispatched command's
// result: its response, or its status. A cluster-specific status goes out
// under an outer FAILURE whatever the handler named — Matter §7.10.7,
// matter.js packages/protocol/src/action/server/CommandInvokeResponse.ts:448-451
// at 862ad75f (#addStatus); it went out under the handler's status.
func resultEntry(inv CommandInvocation, res InvokeResult) InvokeResponseEntry {
	ent := InvokeResponseEntry{
		Path:          res.Path,
		CommandRef:    inv.CommandRef,
		HasCommandRef: inv.HasCommandRef,
	}
	switch {
	case res.Status != StatusSuccess:
		status := res.Status
		if res.HasClusterStatus {
			status = StatusFailure
		}
		ent.IsStatus = true
		ent.Status = StatusIB{Status: status, ClusterStatus: res.ClusterStatus, HasClusterStatus: res.HasClusterStatus}
	case res.Response != nil:
		ent.Response = res.Response
		ent.HasResponse = true
	default:
		// Status-only success path: explicit StatusIB(Success).
		ent.IsStatus = true
		ent.Status = StatusIB{Status: StatusSuccess}
	}
	return ent
}

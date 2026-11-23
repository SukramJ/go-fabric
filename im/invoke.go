// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package im

import (
	"context"
	"errors"
	"fmt"

	"thingfabric.dev/go-fabric/schema"

	"thingfabric.dev/go-fabric/tlv"
)

// InvokeRequestMessage tag numbers (Matter Core Spec §10.6.7).
const (
	tagInvokeReqSuppressResponse uint8 = 0
	tagInvokeReqTimedRequest     uint8 = 1
	tagInvokeReqInvokeRequests   uint8 = 2
	// tagInvokeReqDelayReportData is Matter 1.7's DelayReportData
	// (matter.js packages/types/src/protocol/messages/TlvInvokeRequest.ts,
	// 67be3a83 #4569): decoded and validated by the server, written by the
	// client — see [InvokeRequest.DelayReportData].
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
	// InteractionModelRevision is the request's InteractionModelRevision
	// (tag 0xFF, TlvInvokeRequest.ts:30 at b25ed960):
	// [MarshalInvokeRequestTLV] writes it, 0 writing
	// [MatterInteractionModelRevision]; [UnmarshalInvokeRequestTLV] fills
	// it, 0 when the field is absent. The server acts on neither.
	InteractionModelRevision uint8
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
	var later laterErrors
	for {
		f, err := dec.Next()
		if err != nil {
			return nil, fmt.Errorf("%w: DelayReportData: %w", ErrInvalidInvokeRequest, err)
		}
		if f.IsEndContainer {
			if later.first != nil {
				return nil, later.first
			}
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
			_ = later.keep(invokeDecodeStatusError{"im: invalid InvokeRequest: DelayReportData field exceeds uint16", StatusConstraintError})
			continue
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
	// CommandInvokeResponse.ts:484-487 at db021da0, the failure the
	// command's status :509-546).
	DecodeStatus StatusCode
	// fieldsNotStructure records that CommandFields was no structure —
	// DecodeStatus is INVALID_COMMAND then, the reader not called: a
	// client decoding a response refuses it ([readInvokeResponseIB]).
	fieldsNotStructure bool
	// fieldsReject is the reader's typed reject of CommandFields the
	// CommandDataIB carries (DecodeStatus is its status): a client decoding
	// a response refuses it ([readInvokeResponseIB]). Nil for absent
	// CommandFields read as {}.
	fieldsReject error
	// absentFieldsRejected records that the CommandDataIB carried no
	// CommandFields and the reader refused them read as {} (DecodeStatus
	// is its status): a client decoding a response discards the entry
	// ([readInvokeResponseIB], decision 535).
	absentFieldsRejected bool
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
	var hasSuppress, hasTimed, hasInvokes bool
	var later laterErrors
	for {
		el, err := dec.Next()
		if err != nil {
			return InvokeRequest{}, fmt.Errorf("%w: %w", ErrInvalidInvokeRequest, err)
		}
		if el.IsEndContainer {
			if later.first != nil {
				return InvokeRequest{}, later.first
			}
			// SuppressResponse, TimedRequest and InvokeRequests are
			// mandatory: matter.js
			// packages/types/src/protocol/messages/TlvInvokeRequest.ts:19,
			// :22, :25 at db021da0 (TlvField).
			switch {
			case !hasSuppress:
				return InvokeRequest{}, fmt.Errorf("%w: %w", ErrInvalidInvokeRequest, missingField(tagInvokeReqSuppressResponse, "SuppressResponse"))
			case !hasTimed:
				return InvokeRequest{}, fmt.Errorf("%w: %w", ErrInvalidInvokeRequest, missingField(tagInvokeReqTimedRequest, "TimedRequest"))
			case !hasInvokes:
				return InvokeRequest{}, fmt.Errorf("%w: %w", ErrInvalidInvokeRequest, missingField(tagInvokeReqInvokeRequests, "InvokeRequests"))
			}
			return req, nil
		}
		if err := contextMember(el); err != nil {
			return InvokeRequest{}, fmt.Errorf("%w: %w", ErrInvalidInvokeRequest, err)
		}
		switch uint8(el.Tag.Number & 0xFF) {
		case tagInvokeReqSuppressResponse:
			// TlvField(0, TlvBoolean): a SuppressResponse of another type
			// fails the decode, and the request is answered
			// (packages/types/src/protocol/messages/TlvInvokeRequest.ts:19;
			// SuppressResponseTest.ts:337-343).
			if req.SuppressResponse, err = requestBoolField(el, "SuppressResponse"); err != nil {
				return InvokeRequest{}, fmt.Errorf("%w: %w", ErrInvalidInvokeRequest, err)
			}
			hasSuppress = true
		case tagInvokeReqTimedRequest:
			// TlvField(1, TlvBoolean), TlvInvokeRequest.ts:22: a TimedRequest
			// of another type fails the decode (FAILURE), never reads as false.
			if req.TimedRequest, err = requestBoolField(el, "TimedRequest"); err != nil {
				return InvokeRequest{}, fmt.Errorf("%w: %w", ErrInvalidInvokeRequest, err)
			}
			hasTimed = true
		case tagInvokeReqInvokeRequests:
			if !el.IsContainer || el.Type != tlv.TypeArray {
				return InvokeRequest{}, fmt.Errorf("%w: InvokeRequests not array", ErrInvalidInvokeRequest)
			}
			invokes, err := readCommandData(dec, fieldsReader)
			if err := later.keep(err); err != nil {
				return InvokeRequest{}, err
			}
			req.Invokes = invokes
			hasInvokes = true
		case tagInvokeReqDelayReportData:
			drd, err := readDelayReportData(dec, el)
			if err := later.keep(err); err != nil {
				return InvokeRequest{}, err
			}
			req.DelayReportData = drd
		case tagInteractionModelRevision:
			if el.Type >= tlv.TypeUnsignedInt1 && el.Type <= tlv.TypeUnsignedInt8 && el.Uint <= 0xFF {
				req.InteractionModelRevision = uint8(el.Uint)
			}
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
	var later laterErrors
	for {
		el, err := dec.Next()
		if err != nil {
			return nil, err
		}
		if el.IsEndContainer {
			if later.first != nil {
				return nil, later.first
			}
			return out, nil
		}
		if taggedArrayElement(el) || !el.IsContainer || el.Type != tlv.TypeStructure {
			return nil, fmt.Errorf("%w: CommandDataIB not struct", ErrInvalidInvokeRequest)
		}
		inv, err := readCommandInvocation(dec, fieldsReader)
		if err := later.keep(err); err != nil {
			return nil, err
		}
		out = append(out, inv)
	}
}

func readCommandInvocation(dec *tlv.Decoder, fieldsReader CommandFieldsReader) (CommandInvocation, error) {
	var inv CommandInvocation
	hasPath, hasFields := false, false
	var later laterErrors
	for {
		el, err := dec.Next()
		if err != nil {
			return CommandInvocation{}, err
		}
		if el.IsEndContainer {
			if later.first != nil {
				return CommandInvocation{}, later.first
			}
			// CommandPath is mandatory: matter.js
			// packages/types/src/protocol/types/TlvCommandData.ts:17 at
			// b25ed960 (TlvField).
			if !hasPath {
				return CommandInvocation{}, fmt.Errorf("%w: %w", ErrInvalidInvokeRequest, missingField(tagCmdDataPath, "CommandDataIB.CommandPath"))
			}
			if !hasFields && fieldsReader != nil {
				if err := readAbsentFields(fieldsReader, &inv); err != nil {
					return CommandInvocation{}, err
				}
				// Absent fields are not a payload that failed to decode
				// (R2-C2-wire-0551 is about the latter); the reject stays
				// the DecodeStatus a server answers with, and a client
				// discards the response (decision 535).
				inv.absentFieldsRejected = inv.fieldsReject != nil
				inv.fieldsReject = nil
			}
			return inv, nil
		}
		if err := contextMember(el); err != nil {
			return CommandInvocation{}, fmt.Errorf("%w: %w", ErrInvalidInvokeRequest, err)
		}
		switch uint8(el.Tag.Number & 0xFF) {
		case tagCmdDataPath:
			if !el.IsContainer || el.Type != tlv.TypeList {
				return CommandInvocation{}, fmt.Errorf("%w: CommandPathIB not list", ErrInvalidInvokeRequest)
			}
			p, err := readCommandPathFields(dec)
			if err := later.keep(err); err != nil {
				return CommandInvocation{}, err
			}
			inv.Path = p
			hasPath = true
		case tagCmdDataFields:
			hasFields = true
			if err := readCommandFields(dec, el, fieldsReader, &inv); err != nil {
				return CommandInvocation{}, err
			}
		case tagCmdDataRef:
			ref, err := uint16Field(el)
			if err != nil {
				if err := later.keep(fmt.Errorf("%w: CommandRef: %w", ErrInvalidInvokeRequest, err)); err != nil {
					return CommandInvocation{}, err
				}
			}
			inv.CommandRef = ref
			inv.HasCommandRef = true
		default:
			if err := skipField(dec, el); err != nil {
				return CommandInvocation{}, fmt.Errorf("%w: %w", ErrInvalidInvokeRequest, err)
			}
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
	// packages/protocol/src/action/server/CommandInvokeResponse.ts:555-563
	// at db021da0 (#decodeWithSchema: InvalidCommandError); the
	// reader read past it and the request failed as a whole.
	if notStruct, err := skipNonStructFields(dec, el); notStruct || err != nil {
		if err != nil {
			return err
		}
		inv.Fields, inv.DecodeStatus, inv.fieldsNotStructure = nil, StatusInvalidCommand, true
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
		inv.fieldsReject = err
		return nil
	}
	inv.Fields = f
	return nil
}

// emptyFields is the TLV of an empty anonymous structure: CommandFields
// read for a CommandDataIB that carries none.
var emptyFields = []byte{0x15, 0x18}

// readAbsentFields reads the CommandFields of inv, whose CommandDataIB
// carries none, as an empty structure: the fields reader decodes it as it
// decodes an explicit {} — a command with a mandatory field answers the
// reader's status (INVALID_COMMAND for a missing field), a command without
// fields hands its reader's value on (nil for one the reader decodes by
// definition). Absent and empty CommandFields thus take one decode path
// (notes/parity/by_design.md, decision 334). matter.js reads an absent
// request as {} in its model check (packages/node/src/node/integration/ProtocolService.ts:624-625
// at b25ed960) and answers an explicit {} that lacks a mandatory field
// INVALID_COMMAND (packages/types/src/tlv/TlvObject.ts:253-261,
// packages/types/src/common/ValidationError.ts:45-47,
// packages/protocol/src/action/server/CommandInvokeResponse.ts:529-535); its
// TLV check of an absent payload of a command it does not model-validate
// fails with an uncaught TypeError, FAILURE (:549-551, TlvObject.ts:256),
// which this module does not mirror.
func readAbsentFields(fieldsReader CommandFieldsReader, inv *CommandInvocation) error {
	dec := tlv.NewDecoder(emptyFields)
	el, err := dec.Next()
	if err != nil {
		return fmt.Errorf("%w: empty fields: %w", ErrInvalidInvokeRequest, err)
	}
	return readCommandFields(dec, el, fieldsReader, inv)
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
	var later laterErrors
	for {
		el, err := dec.Next()
		if err != nil {
			return ConcreteCommandPath{}, err
		}
		if el.IsEndContainer {
			if later.first != nil {
				return ConcreteCommandPath{}, later.first
			}
			if !p.HasCluster || !p.HasCommand {
				return ConcreteCommandPath{}, fmt.Errorf("%w: missing cluster/command", ErrInvalidPath)
			}
			return p, nil
		}
		// A non-context member refuses the path, as for every path list
		// ([readAttributePathField]; TlvCommandPath.ts:14 is a TlvTaggedList).
		if err := contextMember(el); err != nil {
			return ConcreteCommandPath{}, fmt.Errorf("%w: %w", ErrInvalidPath, err)
		}
		// Type- and range-checked, never masked (review 2026-11
		// W2-wire-03): matter.js
		// packages/types/src/protocol/types/TlvCommandPath.ts:16-18 (bounded
		// TlvEndpointNumber / TlvClusterId / TlvCommandId); chip
		// src/app/MessageDef/CommandPathIB.cpp:119-132 GetConcreteCommandPath.
		switch uint8(el.Tag.Number & 0xFF) {
		case tagCmdPathEndpoint:
			p.Endpoint, err = uint16Field(el)
			p.HasEndpoint = true
		case tagCmdPathCluster:
			p.Cluster, err = uint32Field(el)
			p.HasCluster = true
		case tagCmdPathCommand:
			p.Command, err = uint32Field(el)
			p.HasCommand = true
		default:
			err = skipField(dec, el)
		}
		if err != nil {
			if err := later.keep(fmt.Errorf("%w: %w", ErrInvalidPath, err)); err != nil {
				return ConcreteCommandPath{}, err
			}
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
// that produced a response payload) rather than a CommandStatusIB. It no
// longer decides whether a SuppressResponse invoke is answered: since
// matter.js 8addfb158 (#5011) such an invoke is never answered
// (packages/node/src/node/server/InteractionServer.ts:1104-1109 at
// b25ed960), and the bridge's dispatch layer follows that without
// consulting the response's contents.
//
// Deprecated: nothing in the module calls it, and the rule it once encoded
// (a SuppressResponse invoke is answered when it produced command data) is
// no longer matter.js's. There is no replacement; range over
// [InvokeResponse.Responses] and test IsStatus. Removable in v0.26.0.
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
// packages/protocol/src/action/server/CommandInvokeResponse.ts:68-99 at
// db021da0 (CommandInvokeResponse.process) and :196-212 (the duplicate
// checks of #processConcrete):
//   - the batch must not exceed [DefaultMaxPathsPerInvoke] paths, checked
//     before any per-command validation or dispatch (matter.js
//     packages/node/src/node/server/InteractionServer.ts:1017-1022
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
	return ValidateInvokeBatchWithin(req, DefaultMaxPathsPerInvoke)
}

// ValidateInvokeBatchWithin is [ValidateInvokeBatch] with the node's own
// MaxPathsPerInvoke in place of [DefaultMaxPathsPerInvoke]; maxPaths 0 is
// the default. Mirrors matter.js
// packages/node/src/node/server/InteractionServer.ts:198
// (`#maxPathsPerInvoke = node.state.basicInformation.maxPathsPerInvoke ??
// DEFAULT_MAX_PATHS_PER_INVOKE`) and :1017, the limit the node advertises.
func ValidateInvokeBatchWithin(req InvokeRequest, maxPaths int) StatusCode {
	if maxPaths <= 0 {
		maxPaths = DefaultMaxPathsPerInvoke
	}
	if len(req.Invokes) > maxPaths {
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
// ACL gate: mirrors matter.js's access passes of an invoke,
// packages/protocol/src/action/server/CommandInvokeResponse.ts:257-260 (the
// Operate pass before existence) and :273-279 (the command's privilege) at
// db021da0, and connectedhomeip's order,
// src/app/InteractionModelEngine.cpp:1810-1851 at 6170af84
// (ValidateCommandCanBeDispatched).
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
	// what the ACL grants, and nothing is granted without an ACL source:
	// fabric 0 without PASE is no grant either, the implicit Administer is
	// keyed on the auth mode alone (matter.js
	// packages/protocol/src/interaction/FabricAccessControl.ts:189-191 at
	// b25ed960, `authMode === Pase && isCommissioning`).
	ctx = WithAuthority(ctx, func(endpoint uint16, clusterID uint32, privilege uint8) StatusCode {
		if g.pase {
			return StatusSuccess
		}
		return checkSubjectACL(g.ctx, g.aclChecker, g.fabricIndex, g.subjectNodeID, g.subjectCATs, endpoint, clusterID, privilege)
	})

	var ir InvokeResponse
	// The InvokeResponseMessage.SuppressResponse field is deprecated and matter.js
	// always emits it as false (packages/node/src/node/server/InteractionServer.ts:1114 at b25ed960
	// `emptyInvokeResponse.suppressResponse: false`). The controller's suppress-response
	// intent governs whether the bridge sends the InvokeResponse at all (handled at the
	// dispatch layer: a suppressed invoke is never answered, matter.js
	// InteractionServer.ts:1104-1109 at b25ed960), not the
	// value of this wire field — so it stays at its zero value here.
	for _, inv := range req.Invokes {
		// A wildcard-endpoint command runs on every endpoint that serves
		// the cluster, accepts the command and on which the session may
		// invoke it; an endpoint it may not is skipped, not reported.
		// Mirrors matter.js
		// packages/protocol/src/action/server/CommandInvokeResponse.ts:131-173
		// (#processWildcard), :330-361 (#processEndpointForWildcard) and
		// :367-398 (#wildcardTargetOf: the command's privilege :380, its
		// Large Message quality :384-386, its fabric scope :388-390, its
		// timed quality :392-394) at db021da0. It answered
		// UNSUPPORTED_ENDPOINT.
		if wild, ok := d.(AuthorizingInvoker); ok && !inv.Path.HasEndpoint {
			if !inv.DecodeStatus.IsSuccess() {
				ir.Responses = append(ir.Responses, undecodedWildcardEntries(ctx, wild, g, inv)...)
				continue
			}
			for _, res := range wild.InvokeAuthorized(ctx, inv.Path, inv.Fields, g.wildcardTarget) {
				if res.Status == StatusSuccess && isNoResponse(res.Response) {
					// Answered with nothing at this endpoint ([NoResponse]),
					// as a concrete command below (Fable's read of sub
					// round 1, C-1).
					continue
				}
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
		// requestTlv.validate (CommandInvokeResponse.ts:484-487, :509-546).
		if !inv.DecodeStatus.IsSuccess() {
			ir.Responses = append(ir.Responses, pathStatusEntry(inv, inv.DecodeStatus))
			continue
		}
		res := d.Invoke(ctx, inv.Path, inv.Fields)
		if res.Status == StatusSuccess && isNoResponse(res.Response) {
			// The handler answers the command with nothing ([NoResponse]).
			continue
		}
		ir.Responses = append(ir.Responses, resultEntry(inv, res))
	}
	return ir
}

// undecodedWildcardEntries answers a wildcard-endpoint command whose fields
// the reader refused: the decode status once for every endpoint the
// command targets, the command run nowhere. The endpoints are the ones its
// expansion admits ([invokeGate.wildcardTarget]); the authorizer answers
// the decode status for an admitted endpoint, which the dispatcher treats as
// "do not invoke", as the group path does ([HandleGroupInvoke]). Mirrors
// matter.js packages/protocol/src/action/server/CommandInvokeResponse.ts:330-361
// at db021da0 (#processEndpointForWildcard selects the endpoints first) and
// :484-487, :509-546 (#invokeCommand validates per endpoint and adds the
// failure as that endpoint's status); no target, no status.
func undecodedWildcardEntries(ctx context.Context, wild AuthorizingInvoker, g *invokeGate, inv CommandInvocation) []InvokeResponseEntry {
	var out []InvokeResponseEntry
	wild.InvokeAuthorized(ctx, inv.Path, nil, func(endpoint uint16, clusterID, commandID uint32) StatusCode {
		if status := g.wildcardTarget(endpoint, clusterID, commandID); !status.IsSuccess() {
			return status
		}
		at := inv
		at.Path.Endpoint, at.Path.HasEndpoint = endpoint, true
		out = append(out, pathStatusEntry(at, inv.DecodeStatus))
		return inv.DecodeStatus
	})
	return out
}

// DispatchEndpoints returns the endpoints req would dispatch a command to
// in ctx: each concrete path that passes the checks [HandleInvokeRequest]
// makes before dispatching (access, existence, Large Message transport,
// fabric scope, timed quality), and every endpoint a wildcard path expands to. A path that
// fails a check contributes nothing, and nothing runs. It is matter.js's
// dispatch-endpoint set, collected in CommandInvokeResponse's validation
// pass (packages/protocol/src/action/server/CommandInvokeResponse.ts:159-165
// and :298 at db021da0) and handed to its dispatch hook before any command
// runs (:101-103), the endpoints a DelayReportData defers.
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
	// largeMessage / largeKnown: [LargeMessageSessionFromContext].
	largeMessage bool
	largeKnown   bool
}

func newInvokeGate(ctx context.Context, d Dispatcher) *invokeGate {
	g := &invokeGate{ctx: ctx}
	_, g.fabricIndex = FabricFilterFromContext(ctx)
	g.subjectNodeID, g.subjectCATs = SubjectFromContext(ctx)
	g.pase = IsPASEFromContext(ctx)
	g.aclChecker, g.hasACL = aclCheckerOf(d), true
	g.privProvider, g.hasPrivProvider = d.(CommandInvokePrivilegeProvider)
	g.pathChecker, g.hasPathChecker = d.(CommandPathChecker)
	g.timedProvider, g.hasTimed = d.(TimedCommandProvider)
	g.timed, g.timedKnown = TimedInteractionFromContext(ctx)
	g.largeMessage, g.largeKnown = LargeMessageSessionFromContext(ctx)
	return g
}

// refusesLargeMessage reports whether the command carries the Large
// Message quality and the session stated it does not carry such messages
// ([WithLargeMessageSession]); an unstated session refuses nothing.
func (g *invokeGate) refusesLargeMessage(clusterID, commandID uint32) bool {
	return g.largeKnown && !g.largeMessage && schema.CommandLargeMessage(clusterID, commandID)
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
	// No fabric and not PASE fails closed (checkSubjectACL).
	if g.hasACL && !g.pase {
		if status := checkSubjectACL(g.ctx, g.aclChecker, g.fabricIndex, g.subjectNodeID, g.subjectCATs, inv.Path.Endpoint, inv.Path.Cluster, g.invokePrivilege(inv.Path)); !status.IsSuccess() {
			return status
		}
	}
	// Existence after the access check and before the command's flag
	// gates and the payload check: matter.js answers UnsupportedEndpoint /
	// UnsupportedCluster / UnsupportedCommand right after the Operate pass
	// and before the command privilege, the fabric scope and the timed
	// quality (packages/protocol/src/action/server/CommandInvokeResponse.ts:263-271
	// at db021da0; connectedhomeip the same,
	// src/app/InteractionModelEngine.cpp:1846-1851 at 6170af84,
	// ValidateCommandCanBeDispatched returns the existence status before
	// CheckCommandFlags). The access check above uses the
	// Operate floor for a path that does not resolve
	// (CommandInvokePrivilegeProvider), so it discloses no more than
	// matter.js's Operate pass. A payload a generated decoder refused on a
	// cluster the endpoint does not serve still answers UnsupportedCluster
	// (TC-IDM-1.2).
	if g.hasPathChecker && inv.Path.HasEndpoint {
		if status := g.pathChecker.CommandPathStatus(inv.Path); !status.IsSuccess() {
			return status
		}
	}
	// A Large Message command ("L") on a session that carries no large
	// messages answers InvalidTransportType for its path, after the
	// existence checks and the command's privilege and before the fabric
	// scope and the timed quality — matter.js
	// packages/protocol/src/action/server/CommandInvokeResponse.ts:281-285
	// at db021da0 (existence :263-271, privilege :273-279, fabric scope
	// :287-290, timed :292); connectedhomeip CheckCommandFlags
	// (src/app/InteractionModelEngine.cpp:1875-1884 at 6170af84).
	if g.refusesLargeMessage(inv.Path.Cluster, inv.Path.Command) {
		return StatusInvalidTransportType
	}
	// A fabric-scoped command needs an accessing fabric: invoked on a
	// session without one — PASE before AddNOC, or a session whose fabric
	// the caller could not resolve ([HasAccessingFabric]) — it answers
	// UnsupportedAccess. Mirrors matter.js
	// packages/protocol/src/action/server/CommandInvokeResponse.ts:287-290
	// at db021da0 (`limits.fabricScoped && !this.session.fabric`). Found by
	// the CHIP Python harness: TC-IDM-1.2 step 5 sends KeySetRead over PASE.
	if !HasAccessingFabric(g.fabricIndex) && schema.IsFabricScopedInvoke(inv.Path.Cluster, inv.Path.Command) {
		return StatusUnsupportedAccess
	}
	// A timed-required command ("T") outside a timed interaction answers
	// NeedsTimedInteraction for its own path, after the access and
	// fabric checks — matter.js CommandInvokeResponse.ts:292-295.
	if g.timedKnown && !g.timed && g.isTimed(inv.Path) {
		return StatusNeedsTimedInteraction
	}
	return StatusSuccess
}

// wildcardTarget is the [CommandAuthorizer] of a wildcard path's
// endpoints: the command's privilege, its Large Message quality against the
// session, its fabric scope and its timed quality at the endpoint (#wildcardTargetOf,
// CommandInvokeResponse.ts:367-398 at db021da0).
func (g *invokeGate) wildcardTarget(endpoint uint16, clusterID, commandID uint32) StatusCode {
	at := ConcreteCommandPath{Endpoint: endpoint, Cluster: clusterID, Command: commandID, HasEndpoint: true, HasCluster: true, HasCommand: true}
	if g.hasACL && !g.pase {
		if status := checkSubjectACL(g.ctx, g.aclChecker, g.fabricIndex, g.subjectNodeID, g.subjectCATs, endpoint, clusterID, g.invokePrivilege(at)); !status.IsSuccess() {
			return status
		}
	}
	// A Large Message command on a session without large-message support
	// is left out of the targets, after the authority check —
	// CommandInvokeResponse.ts:378-386 at db021da0.
	if g.refusesLargeMessage(clusterID, commandID) {
		return StatusInvalidTransportType
	}
	// CommandInvokeResponse.ts:388 at db021da0, the same check at a
	// wildcard path's endpoint.
	if !HasAccessingFabric(g.fabricIndex) && schema.IsFabricScopedInvoke(clusterID, commandID) {
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
// at db021da0 (#addStatus); it went out under the handler's status.
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

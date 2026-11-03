// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package im

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/SukramJ/go-fabric/schema"
	"github.com/SukramJ/go-fabric/tlv"
)

// WriteRequestMessage tag numbers (Matter Core Spec §10.6.5).
const (
	tagWriteReqSuppressResponse uint8 = 0
	tagWriteReqTimedRequest     uint8 = 1
	tagWriteReqWriteRequests    uint8 = 2
	tagWriteReqMoreChunked      uint8 = 3
)

// WriteResponseMessage tag numbers.
const (
	tagWriteRespResponses uint8 = 0
)

// Errors.
var (
	// ErrInvalidWriteRequest is returned for malformed write requests.
	ErrInvalidWriteRequest = errors.New("im: invalid WriteRequest")
)

// WriteRequest is the in-memory form of a WriteRequestMessage.
type WriteRequest struct {
	SuppressResponse bool
	TimedRequest     bool
	Writes           []AttributeWrite
	// MoreChunkedMessages signals that further WriteRequest chunks
	// follow on the same exchange (Matter §10.6.5 tag 3). Each chunk
	// is answered with its own WriteResponse — matter.js
	// InteractionServer.ts:521-532 sends a per-chunk WriteResponse
	// and then reads the next chunk. The flag is invalid in
	// combination with SuppressResponse or a timed interaction
	// (InteractionServer.ts:397/:408 → InvalidAction).
	MoreChunkedMessages bool
}

// AttributeWrite is one entry in WriteRequests — a (path, value)
// pair the Dispatcher applies.
type AttributeWrite struct {
	Path  ConcreteAttributePath
	Value AttributeValue
	// DataVersion is optional per spec; when non-zero the device
	// MUST surface DataVersionMismatch if its current version
	// differs.
	DataVersion    uint32
	HasDataVersion bool
}

// AttributeValueReader extracts an [AttributeValue] from the TLV
// stream at the decoder's current position. Cluster-native type
// decoding lives outside the IM layer; the reader is supplied by the
// caller — typically the endpoint assembler.
//
// The path argument carries the AttributeDataIB's path (decoded just
// before this call) so the reader can dispatch on (cluster, attribute)
// to a cluster-native decoder. dec is positioned at the value's first
// element when el.IsContainer is true — the reader is responsible for
// draining the container before returning.
type AttributeValueReader func(path ConcreteAttributePath, el tlv.Element, dec *tlv.Decoder) (AttributeValue, error)

// UnmarshalWriteRequestTLV decodes a WriteRequestMessage. valueReader
// is invoked for the Data field of each AttributeDataIB; pass a
// [DefaultValueReader]-style helper that captures the Element tree
// when cluster-aware decoding is not yet available.
func UnmarshalWriteRequestTLV(dec *tlv.Decoder, valueReader AttributeValueReader) (WriteRequest, error) {
	open, err := dec.Next()
	if err != nil {
		return WriteRequest{}, err
	}
	if !open.IsContainer || open.Type != tlv.TypeStructure {
		return WriteRequest{}, fmt.Errorf("%w: expected struct, got 0x%02X", ErrInvalidWriteRequest, open.Type)
	}
	var req WriteRequest
	for {
		el, err := dec.Next()
		if err != nil {
			return WriteRequest{}, fmt.Errorf("%w: %w", ErrInvalidWriteRequest, err)
		}
		if el.IsEndContainer {
			return req, nil
		}
		if el.Tag.Kind != tlv.TagKindContext {
			continue
		}
		switch uint8(el.Tag.Number & 0xFF) {
		case tagWriteReqSuppressResponse:
			// TlvOptionalField(0, TlvBoolean): a SuppressResponse of another
			// type fails the decode, and the request is answered
			// (packages/types/src/protocol/messages/TlvWriteRequest.ts:18;
			// SuppressResponseTest.ts:399-403).
			if el.Type != tlv.TypeBoolTrue && el.Type != tlv.TypeBoolFalse {
				return WriteRequest{}, fmt.Errorf("%w: SuppressResponse not a boolean", ErrInvalidWriteRequest)
			}
			req.SuppressResponse = el.Bool
		case tagWriteReqTimedRequest:
			req.TimedRequest = el.Bool
		case tagWriteReqMoreChunked:
			req.MoreChunkedMessages = el.Bool
		case tagWriteReqWriteRequests:
			if !el.IsContainer || el.Type != tlv.TypeArray {
				return WriteRequest{}, fmt.Errorf("%w: WriteRequests not array", ErrInvalidWriteRequest)
			}
			ws, err := readAttributeWrites(dec, valueReader)
			if err != nil {
				return WriteRequest{}, err
			}
			req.Writes = ws
		default:
			if el.IsContainer {
				if err := skipContainer(dec); err != nil {
					return WriteRequest{}, err
				}
			}
		}
	}
}

func readAttributeWrites(dec *tlv.Decoder, valueReader AttributeValueReader) ([]AttributeWrite, error) {
	var out []AttributeWrite
	for {
		el, err := dec.Next()
		if err != nil {
			return nil, err
		}
		if el.IsEndContainer {
			return out, nil
		}
		if !el.IsContainer || el.Type != tlv.TypeStructure {
			return nil, fmt.Errorf("%w: AttributeDataIB not struct", ErrInvalidWriteRequest)
		}
		w, err := readAttributeData(dec, valueReader)
		if err != nil {
			return nil, err
		}
		out = append(out, w)
	}
}

func readAttributeData(dec *tlv.Decoder, valueReader AttributeValueReader) (AttributeWrite, error) {
	var w AttributeWrite
	for {
		el, err := dec.Next()
		if err != nil {
			return AttributeWrite{}, err
		}
		if el.IsEndContainer {
			return w, nil
		}
		if el.Tag.Kind != tlv.TagKindContext {
			continue
		}
		switch uint8(el.Tag.Number & 0xFF) {
		case tagAttributeDataDataVersion:
			// TlvUInt32, never masked (review 2026-11 W2-wire-03).
			v, err := uint32Field(el)
			if err != nil {
				return AttributeWrite{}, fmt.Errorf("%w: DataVersion: %w", ErrInvalidWriteRequest, err)
			}
			w.DataVersion = v
			w.HasDataVersion = true
		case tagAttributeDataPath:
			if !el.IsContainer || el.Type != tlv.TypeList {
				return AttributeWrite{}, fmt.Errorf("%w: path not list", ErrInvalidWriteRequest)
			}
			p, err := readAttributePathFields(dec)
			if err != nil {
				return AttributeWrite{}, err
			}
			w.Path = p
		case tagAttributeDataValue:
			if valueReader == nil {
				// Without a cluster-aware reader we cannot turn the
				// element tree into a Go value. Drain the container so
				// the surrounding decoder loop continues — the
				// downstream Dispatcher.Write will see a nil Value and
				// answer UnsupportedWrite.
				if el.IsContainer {
					if err := skipContainer(dec); err != nil {
						return AttributeWrite{}, fmt.Errorf("%w: drain value: %w", ErrInvalidWriteRequest, err)
					}
				}
				continue
			}
			v, err := valueReader(w.Path, el, dec)
			if err != nil {
				return AttributeWrite{}, fmt.Errorf("%w: value: %w", ErrInvalidWriteRequest, err)
			}
			w.Value = v
		}
	}
}

// WriteResponse is the in-memory form of a WriteResponseMessage.
type WriteResponse struct {
	Responses []AttributeStatus
}

// AttributeStatus is one entry in WriteResponses.
type AttributeStatus struct {
	Path   ConcreteAttributePath
	Status StatusIB
}

// MarshalTLV encodes wr.
func (wr WriteResponse) MarshalTLV(enc *tlv.Encoder) {
	enc.StartStruct(tlv.AnonymousTag())
	enc.StartArray(tlv.ContextTag(tagWriteRespResponses))
	for _, st := range wr.Responses {
		enc.StartStruct(tlv.AnonymousTag())
		st.Path.MarshalTLV(enc, tlv.ContextTag(tagAttributeStatusPath))
		st.Status.MarshalTLV(enc, tlv.ContextTag(tagAttributeStatusStatus))
		_ = enc.EndContainer()
	}
	_ = enc.EndContainer()
	enc.PutUint(tlv.ContextTag(tagInteractionModelRevision), uint64(MatterInteractionModelRevision))
	_ = enc.EndContainer()
}

// HandleWriteRequest dispatches a parsed WriteRequest through d and
// returns the assembled WriteResponse.
//
// DataVersion mismatch check: mirrors matter.js
// packages/node/src/node/server/InteractionServer.ts DataVersion check
// before write dispatch, and chip src/app/WriteHandler.cpp
// AttributeAccessInterface DataVersion verification. When the caller
// supplies a non-zero DataVersion in an AttributeDataIB and d implements
// [DataVersionReader], the cluster's current version is queried; a mismatch
// returns StatusDataVersionMismatch (0x92) instead of dispatching the write.
//
// ACL gate: mirrors matter.js
// packages/node/src/node/server/OnlineServerInteraction.ts
// FabricAccessControl.forRequest and chip src/app/WriteHandler.cpp:780
// "Execute the ACL Access Granting Algorithm before existence checks". When
// d implements [ACLChecker], the requesting fabric's privilege is verified
// before each write path is dispatched, at the location the path resolves
// to. The required privilege for a plain Write is Operate (3) per Matter
// §9.10.4.4, raised per attribute by [AttributeWritePrivilegeProvider].
// fabricIndex is extracted via [FabricFilterFromContext]; a PASE session
// bypasses the ACL check — fabricIndex==0 before AddNOC, and
// [IsPASEFromContext] after it, mirroring matter.js
// packages/protocol/src/interaction/FabricAccessControl.ts:189-191, which
// keys the implicit Administer grant on the session's auth mode.
func HandleWriteRequest(ctx context.Context, d Dispatcher, req WriteRequest) WriteResponse { //nolint:gocyclo // the write interaction's per-path gates in matter.js order
	_, fabricIndex := FabricFilterFromContext(ctx)
	subjectNodeID, subjectCATs := SubjectFromContext(ctx)
	pase := IsPASEFromContext(ctx)
	aclChecker, hasACL := d.(ACLChecker)
	privProvider, hasPrivProvider := d.(AttributeWritePrivilegeProvider)
	authWriter, hasAuthWriter := d.(AuthorizingWriter)
	dvReader, hasDV := d.(DataVersionReader)
	// PASE skips ACL: commissioning writes arrive before the fabric's ACL
	// entry exists, and the commissioner keeps writing (AccessControl.ACL
	// above all) over the same PASE channel after AddNOC adopted it onto
	// the new fabric — hence the auth-mode term. Without a fabric and
	// without PASE the gate stays active and fails closed
	// (checkSubjectACL).
	aclActive := hasACL && !pase

	// writePrivilege returns the minimum privilege needed to write the
	// given resolved (endpoint, cluster, attribute). Falls back to
	// Operate (3) — the Matter §9.10.4.4 default write privilege — when no
	// AttributeWritePrivilegeProvider is wired or the attribute has no
	// elevated requirement.
	writePrivilege := func(endpoint uint16, clusterID, attrID uint32) uint8 {
		const privilegeOperate uint8 = 3
		if hasPrivProvider {
			return privProvider.MinWritePrivilege(endpoint, clusterID, attrID)
		}
		return privilegeOperate
	}

	// lastWritten is the concrete attribute the previous data element of
	// this request was written to successfully. A data element for the
	// same attribute right after it is not re-authorized: chip
	// WriteHandler::CheckWriteAccess validates the ACL "only if path has
	// changed" from mLastSuccessfullyWrittenPath, so a chunked list write —
	// a REPLACE-ALL followed by ListIndex=null appends — is authorized once,
	// against the ACL in force when it began. Without it an administrator
	// who rewrites the ACL in chunks loses the write to its own empty
	// replace (TC-ACL-2.6, 2.8). matter.js checks every element
	// (BD-Matter-ChunkedWriteAuthorizedOnce).
	tx := writeTransactionFromContext(ctx)
	// authorizeWildcard gates each location a wildcard-endpoint write
	// resolves to: an attribute another interaction holds is skipped
	// (AttributeWriteResponse.ts:355-358), then the ACL decides.
	authorizeWildcard := func(endpoint uint16, clusterID, attrID uint32) StatusCode {
		if tx.heldByOther(endpoint, clusterID, attrID) {
			return StatusBusy
		}
		if !aclActive {
			return StatusSuccess
		}
		return checkSubjectACL(ctx, aclChecker, fabricIndex, subjectNodeID, subjectCATs, endpoint, clusterID, writePrivilege(endpoint, clusterID, attrID))
	}
	lastWritten := tx.lastWritten
	defer func() { tx.lastWritten = lastWritten }()
	var wr WriteResponse
	for _, w := range req.Writes {
		// A write path MUST name a concrete cluster and attribute. Matter
		// reserves wildcards for Read/Subscribe; a wildcard-attribute write
		// has no per-attribute privilege to gate on, so it would apply one
		// value — under one flat Operate check — to every attribute the
		// cluster exposes, AccessControl.ACL included. matter.js refuses
		// such a path with a StatusResponse of INVALID_ACTION for the whole
		// write (packages/protocol/src/action/server/AttributeWriteResponse.ts:278-285
		// "Wildcard path write must specify a clusterId and attributeId",
		// :305-312 "Wildcard path write must specify an attributeId"); this
		// side answers the path alone and goes on — an open finding
		// (notes/parity/matter_behaviour_findings.md, brief BI).
		if !w.Path.HasCluster || !w.Path.HasAttribute {
			wr.Responses = append(wr.Responses, AttributeStatus{
				Path:   w.Path,
				Status: StatusIB{Status: StatusInvalidAction},
			})
			continue
		}
		// An attribute another interaction's chunked write holds is
		// answered BUSY; a concrete path this interaction accepts is held
		// before it is written when the write is chunked or holds already.
		// Mirrors matter.js packages/node/src/node/server/
		// InteractionServer.ts:505-508 (#isHeldByOther → Status.Busy) and
		// :536-543 (the hold, taken before any await).
		if w.Path.HasEndpoint && !tx.holdUnlessBusy(w.Path, req.MoreChunkedMessages) {
			wr.Responses = append(wr.Responses, AttributeStatus{
				Path:   w.Path,
				Status: StatusIB{Status: StatusBusy},
			})
			continue
		}
		// ACL gate for a concrete path. The required privilege is
		// per-attribute (AccessControl.ACL → Administer,
		// BasicInformation.NodeLabel → Manage) rather than a flat Operate,
		// so an Operate-only subject cannot escalate via a privileged
		// write. A wildcard-endpoint path is authorized further down, at
		// every endpoint it resolves to.
		sameAsLast := lastWritten != nil && w.Path.HasEndpoint &&
			lastWritten.Endpoint == w.Path.Endpoint && lastWritten.Cluster == w.Path.Cluster && lastWritten.Attribute == w.Path.Attribute
		if !sameAsLast {
			lastWritten = nil
		}
		if aclActive && w.Path.HasEndpoint && !sameAsLast {
			priv := writePrivilege(w.Path.Endpoint, w.Path.Cluster, w.Path.Attribute)
			if status := checkSubjectACL(ctx, aclChecker, fabricIndex, subjectNodeID, subjectCATs, w.Path.Endpoint, w.Path.Cluster, priv); !status.IsSuccess() {
				wr.Responses = append(wr.Responses, AttributeStatus{
					Path:   w.Path,
					Status: StatusIB{Status: status},
				})
				continue
			}
		}
		// A DataVersion on a wildcard-endpoint path is ignored: Matter
		// §8.9.2.8.1 forbids a client to send one (the controller refuses
		// to, client/interaction/write.go formulateWrite), and the
		// server stays lenient — matter.js writes a wildcard path without
		// looking at the version (packages/protocol/src/action/server/AttributeWriteResponse.ts:67-71
		// at 862ad75f, "dataVersion silently ignored for Wildcard";
		// AttributeWriteResponseTest.ts "writes version mismatch wildcard
		// attribute where mismatch got ignored"). It used to be refused with
		// InvalidAction, after the client-side guard.
		//
		// DataVersion mismatch check on a concrete endpoint. Mirrors
		// matter.js AttributeWriteResponse.ts:254-257 and chip
		// WriteHandler.cpp DataVersion guard. A zero DataVersion in the
		// request means "caller does not constrain the version" — skip.
		if hasDV && w.HasDataVersion && w.DataVersion != 0 && w.Path.HasEndpoint {
			if current, ok := dvReader.CurrentDataVersion(ctx, w.Path.Endpoint, w.Path.Cluster); ok && current != 0 {
				if current != w.DataVersion && versionCheckApplies(ctx, d, w.Path) {
					wr.Responses = append(wr.Responses, AttributeStatus{
						Path:   w.Path,
						Status: StatusIB{Status: StatusDataVersionMismatch},
					})
					continue
				}
			}
		}
		// Dispatch. A wildcard-endpoint write expands inside the
		// dispatcher and mutates state as it goes, so every resolved
		// location has to be authorized there — a check on the returned
		// results would run after the value already landed. Mirrors
		// matter.js AttributeWriteResponse.ts:324-343.
		//
		// A wildcard write skips, silently, an attribute another
		// interaction's chunked write holds (matter.js
		// AttributeWriteResponse.ts:355-358 isHeldByOtherWrite).
		var results []WriteResult
		switch {
		case w.Path.HasEndpoint:
			results = d.Write(ctx, w.Path, w.Value)
		case hasAuthWriter:
			results = authWriter.WriteAuthorized(ctx, w.Path, w.Value, authorizeWildcard)
		case !aclActive:
			results = d.Write(ctx, w.Path, w.Value)
		default:
			// ACL enforcement is on but this dispatcher cannot authorize
			// the endpoints a wildcard resolves to. Fail closed rather
			// than write everywhere on the strength of one check against
			// a zero-value endpoint.
			wr.Responses = append(wr.Responses, AttributeStatus{
				Path:   w.Path,
				Status: StatusIB{Status: StatusUnsupportedAccess},
			})
			continue
		}
		lastWritten = nil
		if w.Path.HasEndpoint && len(results) > 0 && allSucceeded(results) {
			p := w.Path
			lastWritten = &p
		}
		for _, res := range results {
			// When a cluster-specific status is conveyed, the outer global
			// status MUST be FAILURE (not a more specific global code).
			// Mirrors matter.js
			// packages/protocol/src/action/server/AttributeWriteResponse.ts:32
			// (#3988, Matter §7.10.7).
			if res.HasClusterStatus && res.Status != StatusSuccess {
				res.Status = StatusFailure
			}
			wr.Responses = append(wr.Responses, AttributeStatus{
				Path:   res.Path,
				Status: StatusIB{Status: res.Status, ClusterStatus: res.ClusterStatus, HasClusterStatus: res.HasClusterStatus},
			})
		}
	}
	// The chunk's writes are done: a chunk that announces more keeps only
	// the hold of its last accepted attribute, the interaction's final
	// message releases every hold (InteractionServer.ts:555-560
	// releaseHeldPaths(previousProcessedAttributePath), :469 the disposal).
	if req.MoreChunkedMessages {
		tx.releaseHolds(tx.lastQueued)
	} else {
		tx.releaseHolds(nil)
	}
	return wr
}

// versionCheckApplies reports whether a write's DataVersion is judged at
// all: matter.js answers a path that does not exist, an attribute that is
// not writable and a timed attribute outside a timed interaction with their
// own statuses before it compares the version
// (packages/protocol/src/action/server/AttributeWriteResponse.ts:221-257 —
// existence, writable, write privilege, timed, then the version), so the
// dispatcher's answer is left to those. InteractionProtocolTest.ts:557-627
// (WRITE_REQUEST: the read-only ProductName with a version is
// UNSUPPORTED_WRITE).
func versionCheckApplies(ctx context.Context, d Dispatcher, p ConcreteAttributePath) bool {
	if writable, known := schema.AttributeWritable(p.Cluster, p.Attribute); known && !writable {
		return false
	}
	if timed, known := TimedInteractionFromContext(ctx); known && !timed && schema.IsTimedWrite(p.Cluster, p.Attribute) {
		return false
	}
	for _, r := range d.Read(ctx, p) {
		switch r.Status {
		case StatusUnsupportedEndpoint, StatusUnsupportedCluster, StatusUnsupportedAttribute:
			return false
		}
	}
	return true
}

// allSucceeded reports whether every result of a write is Success.
func allSucceeded(results []WriteResult) bool {
	for _, r := range results {
		if r.Status != StatusSuccess {
			return false
		}
	}
	return true
}

// WriteTransaction carries what one Write interaction remembers across the
// WriteRequest messages of a chunked write: the attribute its last data
// element was written to successfully (chip WriteHandler
// mLastSuccessfullyWrittenPath, which lives as long as the interaction),
// and the attributes it holds in the node's [WriteHolds].
// The transport keeps one per chunked write and hands it to every chunk
// through [WithWriteTransaction]; the zero value starts a transaction that
// holds nothing, [NewWriteTransaction] one that takes part in holds.
type WriteTransaction struct {
	lastWritten *ConcreteAttributePath

	holds *WriteHolds
	// held are the attributes this transaction holds in holds; guarded
	// by holds.mu.
	held map[writeHoldKey]struct{}
	// lastQueued is the last concrete attribute a chunk accepted, the one
	// hold that moves on to the next chunk (matter.js
	// previousProcessedAttributePath, InteractionServer.ts:458, :536-537).
	lastQueued *writeHoldKey
}

// NewWriteTransaction starts a write interaction that holds the attributes
// of its chunks in holds and is answered BUSY on an attribute another
// interaction holds there.
func NewWriteTransaction(holds *WriteHolds) *WriteTransaction {
	return &WriteTransaction{holds: holds}
}

// writeHoldKey names one concrete attribute (matter.js
// InteractionServer.ts:86-88 writePathKey).
type writeHoldKey struct {
	endpoint  uint16
	cluster   uint32
	attribute uint32
}

// WriteHolds is the table of attributes a chunked write holds, shared by
// every write interaction of a node: matter.js InteractionServer's
// #writingExchanges (packages/node/src/node/server/InteractionServer.ts:180,
// :211-214 #isHeldByOther). The zero value is ready to use.
type WriteHolds struct {
	mu   sync.Mutex
	held map[writeHoldKey]*WriteTransaction
}

// heldByOther reports whether another interaction holds the attribute
// (InteractionServer.ts:211-214).
func (tx *WriteTransaction) heldByOther(endpoint uint16, cluster, attribute uint32) bool {
	if tx.holds == nil {
		return false
	}
	tx.holds.mu.Lock()
	defer tx.holds.mu.Unlock()
	holder, ok := tx.holds.held[writeHoldKey{endpoint, cluster, attribute}]
	return ok && holder != tx
}

// holdUnlessBusy reports false when another interaction holds the
// attribute (the write is answered BUSY); otherwise it takes the hold
// [WriteTransaction.hold] decides on and records the attribute as the
// chunk's last accepted one.
func (tx *WriteTransaction) holdUnlessBusy(p ConcreteAttributePath, more bool) bool {
	if tx.heldByOther(p.Endpoint, p.Cluster, p.Attribute) {
		return false
	}
	tx.hold(p, more)
	tx.lastQueued = &writeHoldKey{p.Endpoint, p.Cluster, p.Attribute}
	return true
}

// hold takes the attribute for this interaction when the write is chunked
// or already holds an attribute — taken before the write is applied, so no
// other exchange passes the BUSY check meanwhile (InteractionServer.ts:
// 536-543).
func (tx *WriteTransaction) hold(p ConcreteAttributePath, more bool) {
	if tx.holds == nil {
		return
	}
	tx.holds.mu.Lock()
	defer tx.holds.mu.Unlock()
	if !more && len(tx.held) == 0 {
		return
	}
	k := writeHoldKey{p.Endpoint, p.Cluster, p.Attribute}
	if tx.holds.held == nil {
		tx.holds.held = make(map[writeHoldKey]*WriteTransaction)
	}
	if tx.held == nil {
		tx.held = make(map[writeHoldKey]struct{})
	}
	tx.held[k] = struct{}{}
	tx.holds.held[k] = tx
}

// releaseHolds releases every attribute this interaction holds but keep
// (InteractionServer.ts:460-468 releaseHeldPaths): after a chunk's writes
// with keep its last written attribute, and with no keep when the
// interaction ends.
func (tx *WriteTransaction) releaseHolds(keep *writeHoldKey) {
	if tx.holds == nil {
		return
	}
	tx.holds.mu.Lock()
	defer tx.holds.mu.Unlock()
	for k := range tx.held {
		if keep != nil && k == *keep {
			continue
		}
		if tx.holds.held[k] == tx {
			delete(tx.holds.held, k)
		}
		delete(tx.held, k)
	}
}

// Release ends the interaction's holds: the transport calls it for a
// chunked write it abandons before the final chunk.
func (tx *WriteTransaction) Release() { tx.releaseHolds(nil) }

type writeTransactionKey struct{}

// WithWriteTransaction attaches tx to ctx for [HandleWriteRequest].
func WithWriteTransaction(ctx context.Context, tx *WriteTransaction) context.Context {
	return context.WithValue(ctx, writeTransactionKey{}, tx)
}

func writeTransactionFromContext(ctx context.Context) *WriteTransaction {
	if tx, ok := ctx.Value(writeTransactionKey{}).(*WriteTransaction); ok && tx != nil {
		return tx
	}
	return &WriteTransaction{}
}

// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"net"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/contract"
	endpointpkg "github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/im/subscription"
	"github.com/SukramJ/go-fabric/secure/channel"
	"github.com/SukramJ/go-fabric/tlv"
	"github.com/SukramJ/go-fabric/transport/message"
	"github.com/SukramJ/go-fabric/transport/mrp"
)

// errChunkRejected is returned by a chunk loop when the peer answered a
// ReportData chunk with an error StatusResponse; the interaction is
// abandoned, exactly as matter.js's waitForSuccess throws
// (packages/protocol/src/interaction/InteractionMessenger.ts:221-227 at db021da0).
var errChunkRejected = errors.New("bridge: peer rejected ReportData chunk")

// errChunkUnanswered is returned by a chunk loop when the peer never
// answered a ReportData chunk within the exchange timeout. matter.js
// aborts the interaction on that timeout (MessageExchange.ts:872-898 at db021da0,
// PeerMessageMissingError); continuing would stream every remaining
// chunk to a peer that is not listening, holding the dispatch slot for
// the whole report.
var errChunkUnanswered = errors.New("bridge: peer did not answer ReportData chunk")

// errChunkAnswerUndecodable is returned by a chunk loop when the peer
// answered a ReportData chunk with a StatusResponse whose Status could not
// be decoded: no status of the peer's, so no refusal either (review II,
// R2-P6-wire-0503, owner decision 176). matter.js decodes the Status
// strictly and the decode error is not a StatusResponseError (fable-6a-w3
// H2: packages/types/src/tlv/TlvNumber.ts:124-131,
// packages/general/src/MatterError.ts:188 at db021da0): on the request's
// own exchange the interaction answers FAILURE and closes the exchange
// (packages/protocol/src/interaction/InteractionMessenger.ts:431,
// :458-463); on a subscription update it is a failed send that is retried
// (packages/node/src/node/server/ServerSubscription.ts:938-944, :558-608).
var errChunkAnswerUndecodable = errors.New("bridge: peer's answer to a ReportData chunk is undecodable")

// errChunkWaitDisarmed ends a chunk wait disarmed by another hand: the
// close of its session. It is an [errChunkWaitAborted] too.
var errChunkWaitDisarmed = errors.New("bridge: chunk wait disarmed")

// chunkRejectedError is [errChunkRejected] with the status the peer
// answered.
type chunkRejectedError struct{ status im.StatusCode }

func (e *chunkRejectedError) Error() string {
	return errChunkRejected.Error() + ": " + e.status.String()
}

func (e *chunkRejectedError) Is(target error) bool { return target == errChunkRejected }

// absorbStatusResponse handles an inbound StatusResponse opcode.
// StatusResponse is the spec-mandated ACK for a ReportData /
// SubscribeResponse / Invoke / Write reply we sent earlier (Matter
// §8.6.2). Apple Home, Google Home, and chip-tool all emit it
// after consuming our reply; silently absorbing it satisfies the
// MRP / Reliable Messaging contract without making the dispatcher
// see a bogus "request". Without this branch the commissioner
// retransmits its previous request indefinitely after pairing,
// which Apple eventually surfaces as "device added" → immediate
// disconnect.
func (b *Bridge) absorbStatusResponse(src net.Addr, requestHdr *message.Header, proto message.ProtocolHeader, payload []byte) error {
	// Decode the carried status: the peer's answer is only a go-ahead
	// when it is Success. matter.js reads it on every inbound
	// StatusResponse (InteractionMessenger.ts:183-196
	// throwIfErrorStatusMessage) and throws on anything else; an
	// undecodable one is treated as Failure so the waiting interaction
	// stops rather than carries on blind.
	status, undecodable := im.StatusFailure, false
	if sr, err := im.UnmarshalStatusResponseTLV(tlv.NewDecoder(payload)); err != nil {
		undecodable = true
		b.logger.Warn("matter.rx.im.status_decode",
			slog.String("src", srcString(src)),
			slog.Int("exchange_id", int(proto.ExchangeID)),
			slog.Any("err", err))
	} else {
		status = sr.Status
	}
	// Do NOT call dischargeOwedAck here. The previous code did
	// — the rationale "we just piggyback-acked on an outbound reply"
	// is correct for *request* opcodes that immediately produce a
	// reply, but a StatusResponse is itself the peer's IM-level
	// reply; there is no outbound on which to piggyback. Cancelling
	// the obligation that `owedInboundAck` (line ~147) registered a
	// moment earlier leaves the inbound StatusResponse silently
	// un-ACKed at the MRP layer, and chip-tool's ReliableMessaging
	// retransmits it 4 times before giving up (symptom:
	// `Dropping message without piggyback ack when we are waiting
	// for an ack`, then `CHIP Error 0x32 Timeout`). Let the ack pump
	// fire the synthesised StandaloneAck so the peer's MRP layer can
	// advance to receive subsequent retransmits / new exchanges — at
	// once when the StatusResponse ends our exchange (below), else at
	// its normal cadence.
	//
	// Subscribe-Initial chunk-streaming loop in subscribe.go blocks
	// on this signal between chunks to mirror matter.js's
	// `sendDataReportMessage(_, waitForAck=true)` round-trip
	// pattern. Apple's ReadClient emits one IM:StatusResponse per
	// inbound ReportData chunk; without per-chunk sync our burst
	// fires past Apple's state-machine and
	// `ProcessSubscribeResponse` never triggers (Run 19 of the
	// Apple-pair-diagnose cycle).
	// An undecodable one reaches the waiter as such: the waiter decides —
	// a failed send of a subscription update, FAILURE on the request's
	// own exchange (owner decision 176; [errChunkAnswerUndecodable]).
	var waited bool
	if undecodable {
		waited = b.signalUndecodableStatusResponseRX(requestHdr.SessionID, proto.ExchangeID, !proto.Initiator)
	} else {
		waited = b.signalStatusResponseRX(requestHdr.SessionID, proto.ExchangeID, !proto.Initiator, status)
	}
	// A StatusResponse that opens an exchange — the peer speaks as the
	// initiator and no exchange of ours waits for it — is no answer to
	// anything: it is answered like any other unsupported opcode.
	// matter.js InteractionMessenger.ts:347-419 has no case for it in the
	// request switch, so it reaches the default arm (InvalidAction).
	if !waited && proto.Initiator {
		return b.rejectUnsupportedOpcode(src, requestHdr, proto)
	}
	// A StatusResponse no chunk loop waits for, on an exchange we
	// initiated (the peer speaks as the responder), is the last message of
	// a report exchange: nothing of ours follows to piggyback its ack on,
	// so the exchange closes and the ack goes out now. Mirrors matter.js
	// packages/protocol/src/protocol/MessageExchange.ts:1150-1157 at
	// db021da0 (close sends the standalone ack of the last received
	// message at once, not after STANDALONE_ACK_TIMEOUT). Left to the ack
	// pump's cadence, the peer's MRP layer holds the exchange open for the
	// grace window and retransmits past it.
	if !waited && !proto.Initiator {
		b.expediteDuplicateAck(requestHdr.SessionID, proto.ExchangeID, !proto.Initiator)
		b.RunAckPumpOnce(b.clk().Now())
	}
	// An error status answering an ongoing subscription report — a
	// bridge-initiated exchange, so the peer speaks as the responder —
	// means the peer no longer honours the subscription
	// (InvalidSubscription) or could not process the report (Failure).
	// matter.js ServerSubscription.ts:938-950 at db021da0 closes the
	// subscription on either, as on any other error status (:947);
	// nothing else ever would, because the report itself was
	// MRP-acked and the reply refreshes the session's activity.
	// A reporter that waits for this answer got it and ends the
	// subscription itself (closeRefusedSubscription): only an answer nobody
	// waits for ends it here, once (review II, DR5b-c1-04). An undecodable
	// answer is no refusal and ends nothing (owner decision 176).
	if !waited && !undecodable && !status.IsSuccess() && !proto.Initiator {
		b.closeSubscriptionByExchange(requestHdr.SessionID, proto.ExchangeID, status)
	}
	b.logger.Debug("matter.rx.im.status_ack",
		slog.String("src", srcString(src)),
		slog.Int("exchange_id", int(proto.ExchangeID)),
		slog.String("status", status.String()))
	return nil
}

// rejectUnsupportedOpcode handles any opcode that is not a request opcode,
// and a StatusResponse that opens an exchange. As the first message of an
// exchange on a unicast session it is answered StatusResponse(InvalidAction)
// and the owed ack discharged, so the peer's exchange closes at once
// instead of timing out. Mirrors
// matter.js packages/protocol/src/interaction/InteractionMessenger.ts
// :415-419 (the default arm throws StatusResponseError InvalidAction) and
// :431-461 (sendStatus unless the session is a group session); chip
// src/app/InteractionModelEngine.cpp:1095-1099 ("Msg type %d not
// supported", InvalidAction) and :1112-1115 (StatusResponse::Send for a
// non-group exchange). A responder-role message no exchange waits for,
// and any such message on a group session, is dropped with
// ErrUnsupportedOpcode.
func (b *Bridge) rejectUnsupportedOpcode(src net.Addr, requestHdr *message.Header, proto message.ProtocolHeader) error {
	err := fmt.Errorf("%w: opcode=0x%02X", ErrUnsupportedOpcode, proto.Opcode)
	b.logger.Debug("matter.rx.im.unsupported",
		slog.String("src", srcString(src)),
		slog.Int("opcode", int(proto.Opcode)),
		slog.Any("err", err))
	if !proto.Initiator || requestHdr.SessionType == message.SessionGroup {
		return err
	}
	body, encErr := EncodeStatusResponse(im.StatusResponse{Status: im.StatusInvalidAction})
	if encErr != nil {
		debugReplyError(b.logger, "encode_unsupported_reject", src, encErr)
		return encErr
	}
	if sendErr := b.sendReply(src, requestHdr, proto, im.OpcodeStatusResponse, body); sendErr != nil {
		debugReplyError(b.logger, "send_unsupported_reject", src, sendErr)
		return sendErr
	}
	b.dischargeOwedAck(requestHdr.SessionID, proto.ExchangeID, !proto.Initiator)
	return nil
}

// dispatchReadRequest handles a decoded ReadRequest. The TLV decode and
// dispatcher nil-check are done by the caller (handleIMOpcode); this method
// receives the already-decoded req and runs fabric-context stamping, event
// merging, chunked report encoding, and the per-chunk IM:StatusResponse wait.
func (b *Bridge) dispatchReadRequest(ctx context.Context, src net.Addr, requestHdr *message.Header, proto message.ProtocolHeader, dispatcher im.Dispatcher, req im.ReadRequest) error {
	// Diagnostic: dump every requested attribute path so we can
	// identify which spec-conformance probe Apple Home runs
	// post-CommissioningComplete. The 27/28-byte read requests
	// that immediately precede RemoveFabric carry one
	// AttributePath each — logging endpoint/cluster/attribute
	// triples reveals exactly what Apple's iCloud-Heim sync is
	// reading and rejecting.
	for _, p := range req.AttributeRequests {
		b.logger.Debug("matter.rx.im.read_path",
			slog.String("src", srcString(src)),
			slog.Any("endpoint", p.Endpoint),
			slog.String("endpoint_set", strconv.FormatBool(p.HasEndpoint)),
			slog.Any("cluster", p.Cluster),
			slog.String("cluster_set", strconv.FormatBool(p.HasCluster)),
			slog.Any("attribute", p.Attribute),
			slog.String("attribute_set", strconv.FormatBool(p.HasAttribute)))
	}
	// Reject illegal paths up front (wildcard cluster + concrete non-global
	// attribute, or wildcard cluster + concrete event) with a top-level
	// InvalidAction StatusResponse, and a request beyond the path ceiling
	// with PathsExhausted. Mirrors matter.js InteractionServer.ts
	// validateReadPaths (#3926, Matter §8.4.3.2) and the MAX_READ_PATHS
	// check that follows it (:373, :375-381 at db021da0).
	if status := im.ValidateReadPaths(req.AttributeRequests, req.EventRequests); status != im.StatusSuccess {
		body, err := EncodeStatusResponse(im.StatusResponse{Status: status})
		if err != nil {
			debugReplyError(b.logger, "encode_read_path_reject", src, err)
			return err
		}
		if err := b.sendReply(src, requestHdr, proto, im.OpcodeStatusResponse, body); err != nil {
			debugReplyError(b.logger, "send_read_path_reject", src, err)
			return err
		}
		b.dischargeOwedAck(requestHdr.SessionID, proto.ExchangeID, !proto.Initiator)
		return nil
	}
	// Stamp the FabricFiltered flag + the requesting FabricIndex
	// into the context so fabric-scoped cluster servers
	// (OperationalCredentials, AccessControl) can project their
	// fabric-sensitive list attributes to the requesting fabric.
	// Mirrors matter.js InteractionServer.ts:startReadInteraction →
	// OnlineContext.forFabricFilteredRead + fabricIndex.
	readFabricIndex := b.resolveSessionFabric(requestHdr.SessionID)
	readSubjectNodeID, readSubjectCATs := b.resolveSessionSubject(requestHdr.SessionID)
	readCtx := im.WithFabricFilter(ctx, req.FabricFiltered, readFabricIndex)
	readCtx = im.WithSubject(readCtx, readSubjectNodeID, readSubjectCATs)
	// A path naming a node is answered for this node's id on the
	// session's fabric only (review II, R2-P2-alloc-07).
	readLocalNodeID := b.localNodeIDFor(requestHdr.SessionID, readFabricIndex)
	readCtx = im.WithLocalNodeID(readCtx, readLocalNodeID)
	readPASE := b.resolveSessionPASE(requestHdr.SessionID)
	if readPASE {
		readCtx = im.WithAuthModePASE(readCtx)
	}
	// The response is generated chunk by chunk (review II, decision 44):
	// chunk N+1 is pulled from the reports only once chunk N went out and
	// the peer answered it, so what the read holds across a chunk wait is
	// the expansion's cursor and the chunk in flight, not the whole
	// response. Mirrors matter.js
	// packages/protocol/src/action/server/AttributeReadResponse.ts:64 at
	// db021da0 (*process, a generator) and its consumer,
	// InteractionMessenger.ts:470-797 (sendDataReport, which pulls the next
	// report at :555).
	reports := im.NewReadReports(im.WithElementQualities(readCtx, specElementQualities{}), dispatcher, req)
	attributeReports := 0
	attributes := func() (im.AttributeReport, bool) {
		rep, ok := reports.Next()
		if ok {
			b.logReadAttributeReport(attributeReports, rep)
			attributeReports++
		}
		return rep, ok
	}
	// Evaluate EventRequests against the persistent event log so
	// chip-tool `read-event-by-id` and Apple MTRDevice liveness
	// checks return the buffered StartUp / BootReason / etc. events.
	// Matter §10.6.6: a ReadRequest may carry both AttributeRequests
	// and EventRequests; the bridge merges both into one ReportData,
	// the events after the attributes — read once the attributes are
	// out.
	//
	// Gate the event reads by ACL + fabric-sensitive filtering the same
	// way HandleReadRequest gates attribute reads: a wildcard event read
	// must not leak another fabric's AccessControl events, and a
	// non-Administer subject must not read AccessControl events (Matter
	// §8.4.3.2 / §9.10.7.1). Mirrors matter.js EventReadResponse.ts
	// #readAllowedEvents.
	var events func() []im.EventReport
	if len(req.EventRequests) > 0 {
		events = func() []im.EventReport {
			auth := b.eventReadAuthorizer(dispatcher, readFabricIndex, readPASE, readSubjectNodeID, readSubjectCATs)
			// One EventFilterIB applies: the first for this node (matter.js
			// EventReadResponse.ts:68-77; review 2026-11 W2-wire-05). A
			// path naming another node reads no event; a concrete one is
			// answered UNSUPPORTED_NODE by DeniedEventPathStatuses, which
			// reads the request's own paths (review II, R2-P4-imread-07).
			eventReq := im.ReadRequest{
				EventRequests: im.EventPathsForNode(req.EventRequests, readLocalNodeID),
				EventFilters:  im.EventFiltersFor(req.EventFilters, readLocalNodeID),
			}
			evs := append(im.DeniedEventPathStatuses(readCtx, auth, req.EventRequests),
				im.AuthorizeEventReports(readCtx, auth, im.HandleReadEventRequest(eventReq, b.eventLog))...)
			if len(evs) > 0 {
				b.logger.Debug("matter.tx.im.read_event_reports",
					slog.Int("event_reports", len(evs)))
			}
			return evs
		}
	}
	limits := b.chunkLimitsFor(src, requestHdr.SessionID)
	// A plain Read's response suppresses the answer to its last chunk
	// (see [im.HandleReadRequest]).
	chunker := newReportChunker(im.ReportData{SuppressResponse: true}, limits.budget, limits.hardCap, attributes, events)
	chunk, have, err := chunker.Next()
	if err != nil {
		debugReplyError(b.logger, "chunk_report", src, err)
		return err
	}
	// A read that waits for the peer's answer to a chunk takes one of the
	// session's [maxChunkWaitsPerSession] claims; beyond them it is
	// answered Busy (review II, R2-P1-req2-01). Its first chunk tells: only
	// the last chunk suppresses the answer.
	release, claimed := b.claimChunkWait(ctx, requestHdr.SessionID, readWaitsForChunks([]im.ReportData{chunk}))
	if !claimed {
		return b.replyStatusUnlessSuppressed(false, src, requestHdr, proto, "read_chunk_waits", im.StatusBusy)
	}
	defer release()
	chunks := 0
	for i := 0; have; i++ {
		if err := b.sendReadChunk(ctx, src, requestHdr, proto, i, chunk); err != nil {
			return err
		}
		chunks++
		if chunk, have, err = chunker.Next(); err != nil {
			debugReplyError(b.logger, "chunk_report", src, err)
			return err
		}
	}
	b.dischargeOwedAck(requestHdr.SessionID, proto.ExchangeID, !proto.Initiator)
	b.logger.Debug("matter.rx.im.read",
		slog.String("src", srcString(src)),
		slog.Int("attribute_reports", attributeReports),
		slog.Int("chunks", chunks))
	return nil
}

// sendReadChunk sends chunk i of a read's response and, unless the chunk
// suppresses the answer, waits for the peer's StatusResponse to it.
func (b *Bridge) sendReadChunk(ctx context.Context, src net.Addr, requestHdr *message.Header, proto message.ProtocolHeader, i int, chunk im.ReportData) error {
	body, err := EncodeReportData(chunk)
	if err != nil {
		debugReplyError(b.logger, "encode_report", src, err)
		return err
	}
	// Mirror the Subscribe path's per-chunk IM:StatusResponse wait
	// so Apple Home's MTRDevice processes each ReportData frame on
	// the IM layer before the next arrives. matter.js's
	// InteractionMessenger acks every non-final chunk on the IM layer
	// (not just MRP); without the wait go-fabric
	// burst-fires all chunks back-to-back and Apple's
	// `ProcessReadResponse` state machine drops late chunks.
	//
	// A chunk carrying SuppressResponse=true (only the terminal chunk
	// of a plain Read — see [im.HandleReadRequest]) expects nothing but
	// a Standalone MRP-Ack, so the controller never emits an IM
	// StatusResponse for it; waiting would only run out the exchange timeout.
	// Mirrors matter.js
	// packages/protocol/src/interaction/InteractionMessenger.ts:824-845 at db021da0
	// (`suppressResponse` chunk → `expectAckOnly: true`, no StatusResponse
	// wait). Reliable delivery of the final chunk is still guaranteed by
	// sendReplyReliable's MRP retransmit + the post-loop dischargeOwedAck.
	wait := chunkWaitHere
	if chunk.SuppressResponse {
		wait = chunkNoWait
	}
	if _, err := b.sendChunk(b.replyChunkLeg(ctx, "read", src, requestHdr, proto, im.OpcodeReportData), i, body, chunk, wait); err != nil {
		if errors.Is(err, errChunkAnswerUndecodable) {
			// The peer's answer carried no status: the read ends with
			// FAILURE on its exchange (owner decision 176).
			_ = b.replyStatusUnlessSuppressed(false, src, requestHdr, proto, "read_answer_undecodable", im.StatusFailure)
		}
		return err
	}
	b.logger.Debug("matter.rx.im.read.chunk",
		slog.String("src", srcString(src)),
		slog.Int("chunk", i),
		slog.Int("bytes", len(body)),
		slog.Bool("more", chunk.MoreChunkedMessages))
	return nil
}

// logReadAttributeReport is the read's diagnostic: what it returns per
// path, as each report is generated.
func (b *Bridge) logReadAttributeReport(i int, r im.AttributeReport) {
	b.logger.Debug("matter.tx.im.read_report",
		slog.Int("path_index", i),
		slog.Any("endpoint", r.Path.Endpoint),
		slog.Any("cluster", r.Path.Cluster),
		slog.Any("attribute", r.Path.Attribute),
		slog.Bool("status", r.IsStatus),
		slog.Any("status_code", r.Status.Status),
		slog.String("value_type", fmt.Sprintf("%T", r.Value.Value)),
		slog.Any("value", r.Value.Value))
}

// readWaitsForChunks reports whether the chunk loop of a read waits for
// the peer's StatusResponse to any of chunks: every chunk but one that
// suppresses the response.
func readWaitsForChunks(chunks []im.ReportData) bool {
	for _, c := range chunks {
		if !c.SuppressResponse {
			return true
		}
	}
	return false
}

// replyChunkLeg is the chunk leg of a request's reply on its own exchange
// (a read's, a priming report's, an InvokeResponse's): each chunk goes out
// reliably with the latest acknowledgement owed on the exchange
// piggybacked — without it every chunk carries the request's counter, and
// a controller that acknowledged chunk N's StatusResponse drops chunk N+1
// ("Dropping message without piggyback ack when we are waiting for an
// ack", python-matter-server's ReliableMessaging) — and the wait for its
// answer ends with ctx, after the handler handed its session's turn on.
func (b *Bridge) replyChunkLeg(ctx context.Context, op string, src net.Addr, requestHdr *message.Header, proto message.ProtocolHeader, opcode uint8) chunkLeg {
	return chunkLeg{
		op: op, src: src,
		sessionID: requestHdr.SessionID, exchangeID: proto.ExchangeID, role: !proto.Initiator,
		send: func(_ int, body []byte) (uint32, error) {
			chunkHdr := *requestHdr
			b.refreshAckCounter(&chunkHdr, proto.ExchangeID, !proto.Initiator)
			if err := b.sendReplyReliable(src, &chunkHdr, proto, opcode, body); err != nil {
				debugReplyError(b.logger, "send_"+op+"_chunk", src, err)
				return 0, err
			}
			return 0, nil
		},
		done:       ctx.Done(),
		beforeWait: func() { leaveSessionOrder(ctx) }, // the answer arrives on this session
	}
}

// errChunkWaitAborted ends a chunk wait whose sender stopped: the
// subscription engine, for an ongoing report ([subTarget.abort]). It is
// no verdict on the peer.
var errChunkWaitAborted = errors.New("bridge: chunk wait aborted")

// awaitChunkStatusResponseWithin blocks until the peer answers the chunk
// just sent on (session, exchange) with an IM StatusResponse, then decides
// whether the chunk loop may go on ([Bridge.sendChunk]). Mirrors matter.js
// InteractionMessenger.ts:839-844 at db021da0, sendDataReportMessage →
// waitForSuccess: a non-Success status ([chunkRejectedError]) or no answer
// within timeout ([errChunkUnanswered]) aborts the interaction — the
// caller returns the error and sends nothing more on the exchange. op
// names the loop for the log line.
//
// The wait also ends, with [errChunkWaitAborted], when done — a handler's
// context, which the bridge's Stop cancels — or abort closes (nil: never),
// and when the wait is disarmed by another hand: the close of its session
// ([Bridge.SessionClosed]; [errChunkWaitDisarmed] too). Review II,
// R2-P1-req2-01.
func (b *Bridge) awaitChunkStatusResponseWithin(timeout time.Duration, done, abort <-chan struct{}, waitCh <-chan statusAnswer, op string, src net.Addr, sessionID, exchangeID uint16, initiator bool, chunkIdx int, chunk im.ReportData) error {
	timer := b.clk().NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-done:
		b.disarmStatusResponseWait(sessionID, exchangeID, initiator)
		return fmt.Errorf("%w: exchange %d chunk %d", errChunkWaitAborted, exchangeID, chunkIdx)
	case <-abort:
		b.disarmStatusResponseWait(sessionID, exchangeID, initiator)
		return fmt.Errorf("%w: exchange %d chunk %d", errChunkWaitAborted, exchangeID, chunkIdx)
	case answer, ok := <-waitCh:
		b.disarmStatusResponseWait(sessionID, exchangeID, initiator)
		if !ok {
			// Disarmed without an answer: the zero status would read
			// as Success and release the next chunk to a session that
			// is gone.
			return fmt.Errorf("%w: %w: exchange %d chunk %d", errChunkWaitAborted, errChunkWaitDisarmed, exchangeID, chunkIdx)
		}
		if answer.undecodable {
			b.logger.Debug("matter.tx."+op+".chunk_answer_undecodable",
				slog.String("src", srcString(src)),
				slog.Int("chunk", chunkIdx),
				slog.Int("exchange_id", int(exchangeID)))
			return fmt.Errorf("%w: exchange %d chunk %d", errChunkAnswerUndecodable, exchangeID, chunkIdx)
		}
		status := answer.status
		if status.IsSuccess() {
			return nil
		}
		b.logger.Debug("matter.tx."+op+".chunk_rejected",
			slog.String("src", srcString(src)),
			slog.Int("chunk", chunkIdx),
			slog.Int("exchange_id", int(exchangeID)),
			slog.String("status", status.String()))
		return &chunkRejectedError{status: status}
	case <-timer.C():
		b.disarmStatusResponseWait(sessionID, exchangeID, initiator)
		b.logger.Debug("matter.tx."+op+".chunk_ack_timeout",
			slog.String("src", srcString(src)),
			slog.Int("chunk", chunkIdx),
			slog.Int("exchange_id", int(exchangeID)),
			slog.Bool("final", !chunk.MoreChunkedMessages),
			slog.String("timeout", timeout.String()))
		return fmt.Errorf("%w: exchange %d chunk %d after %s", errChunkUnanswered, exchangeID, chunkIdx, timeout)
	}
}

// dispatchWriteRequest handles a decoded WriteRequest. The TLV decode and
// dispatcher nil-check are done by the caller (handleIMOpcode).
func (b *Bridge) dispatchWriteRequest(ctx context.Context, src net.Addr, requestHdr *message.Header, proto message.ProtocolHeader, dispatcher im.Dispatcher, req im.WriteRequest) error {
	for _, w := range req.Writes {
		b.logger.Debug("matter.rx.im.write_path",
			slog.String("src", srcString(src)),
			slog.Any("endpoint", w.Path.Endpoint),
			slog.Any("cluster", w.Path.Cluster),
			slog.Any("attribute", w.Path.Attribute),
			slog.String("value_type", fmt.Sprintf("%T", w.Value.Value)))
	}
	txKey := mrp.ExchangeKey{SessionID: requestHdr.SessionID, ExchangeID: proto.ExchangeID, Initiator: !proto.Initiator}
	// A chunk that continues a chunked write is not judged as a new
	// request: of it matter.js reads only its writes, MoreChunkedMessages
	// and SuppressResponse (packages/node/src/node/server/InteractionServer.ts:582-584
	// at db021da0) — its Timed flag is not looked at, the timed checks ran
	// for the first chunk, and a chunked write is never timed (:417-424).
	// A SuppressResponse on it ends the interaction with InvalidAction,
	// the chunk not applied and the status itself suppressed (:594-599;
	// InteractionMessenger.ts:317-322 sends no status for a suppressed request).
	if b.writeContinues(txKey) {
		if req.SuppressResponse {
			b.endWriteInteraction(txKey)
			return b.replyStatusUnlessSuppressed(true, src, requestHdr, proto, "write_chunk_suppress", im.StatusInvalidAction)
		}
		req.TimedRequest = false
		return b.applyWrite(ctx, src, requestHdr, proto, dispatcher, req, txKey)
	}
	// A chunked write may not suppress the response — matter.js
	// packages/node/src/node/server/InteractionServer.ts:410-415 at
	// db021da0 rejects the combination with InvalidAction before any
	// timed-interaction handling.
	if req.MoreChunkedMessages && req.SuppressResponse {
		return b.replyStatusUnlessSuppressed(req.SuppressResponse, src, requestHdr, proto, "write_chunked_suppress", im.StatusInvalidAction)
	}
	// A write inside a running timed window may not be chunked, whatever
	// its own Timed flag says, and this is judged before the timed gate —
	// matter.js InteractionServer.ts:417-424 at db021da0
	// (`receivedWithinTimedInteraction && moreChunkedMessages`, "Write
	// Request action that is part of a Timed Write Interaction SHALL NOT
	// be chunked"). The window goes with the refusal, as the exchange does
	// in matter.js.
	if req.MoreChunkedMessages && b.timedWindowRunning(requestHdr.SessionID, proto.ExchangeID) {
		b.routing.timedDeadlines.Delete(timedKey{sessionID: requestHdr.SessionID, exchangeID: proto.ExchangeID})
		return b.replyStatusUnlessSuppressed(req.SuppressResponse, src, requestHdr, proto, "write_timed_chunked", im.StatusInvalidAction)
	}
	if status, gated := b.checkTimedGate(req.TimedRequest, requestHdr.SessionID, proto.ExchangeID); gated {
		return b.replyStatusUnlessSuppressed(req.SuppressResponse, src, requestHdr, proto, "write", status)
	}
	return b.applyWrite(ctx, src, requestHdr, proto, dispatcher, req, txKey)
}

// writeContinues reports whether key's exchange has a chunked write in
// progress — a previous chunk announced more and the interaction has not
// lapsed ([writeTxTTL]) — so the chunk that arrives on it continues that
// write.
func (b *Bridge) writeContinues(key mrp.ExchangeKey) bool {
	v, ok := b.routing.writeTxs.Load(key)
	if !ok {
		return false
	}
	w, isTx := v.(*writeTx)
	return isTx && b.clk().Now().Sub(time.Unix(0, w.lastChunk.Load())) <= writeTxTTL
}

// applyWrite runs a WriteRequest that passed the interaction's checks —
// a first message, or a chunk that continues one — and answers it.
func (b *Bridge) applyWrite(ctx context.Context, src net.Addr, requestHdr *message.Header, proto message.ProtocolHeader, dispatcher im.Dispatcher, req im.WriteRequest, txKey mrp.ExchangeKey) error {
	// Stamp the session FabricIndex onto ctx so fabric-scoped writes
	// (AccessControl.ACL above all) resolve the caller's fabric the
	// same way reads do via [im.FabricFilterFromContext]. Without
	// this stamp the write has no accessing fabric and is refused
	// with UNSUPPORTED_ACCESS — by the write handler's gate for a
	// fabric-scoped attribute (im/write.go, HasAccessingFabric) and
	// by AccessControl's own writeFabric — so Apple's
	// post-CommissioningComplete ACL update — which carries the new
	// Resident NodeID + CAT subjects — never reaches the requesting
	// fabric. Apple then reads the ACL from its own fabric on the
	// Subscribe-Initial, sees only the pre-existing case_admin_subject
	// entry, classifies the bridge as missing Administer privilege, and
	// tears the pair down with the iOS "accessory could not be added"
	// dialog.
	// Mirrors the Invoke pathway below.
	writeFabricIndex := b.resolveSessionFabric(requestHdr.SessionID)
	writeSubjectNodeID, writeSubjectCATs := b.resolveSessionSubject(requestHdr.SessionID)
	writeCtx := im.WithFabricFilter(ctx, false, writeFabricIndex)
	writeCtx = im.WithSubject(writeCtx, writeSubjectNodeID, writeSubjectCATs)
	// A path naming another node is not written ([im.HandleWriteRequest]),
	// against this node's id on the session's fabric, as for a read.
	writeCtx = im.WithLocalNodeID(writeCtx, b.localNodeIDFor(requestHdr.SessionID, writeFabricIndex))
	// The ACL write Apple sends right after AddNOC still travels the PASE
	// channel; without this stamp it is evaluated as fabric N / node-id 0
	// and denied.
	if b.resolveSessionPASE(requestHdr.SessionID) {
		writeCtx = im.WithAuthModePASE(writeCtx)
	}
	// The gate above passed, so a set TimedRequest flag means a valid
	// window; the dispatcher answers an attribute with the timed quality
	// outside one with NeedsTimedInteraction (endpoint.TopologyDispatcher,
	// matter.js AttributeWriteResponse.ts:243-247).
	writeCtx = im.WithTimedInteraction(writeCtx, req.TimedRequest)
	// The chunks of one chunked write share a transaction, as chip's
	// WriteHandler serves the whole interaction (TC-ACL-2.6, 2.8).
	// A chunk that would open one chunked write more than the session may
	// have is answered Busy before anything is held or written
	// ([maxChunkedWritesPerSession]).
	tx := b.routing.writeTransaction(txKey, req.MoreChunkedMessages, b.clk().Now())
	if tx == nil {
		return b.replyStatusUnlessSuppressed(req.SuppressResponse, src, requestHdr, proto, "write_chunked_open", im.StatusBusy)
	}
	writeCtx = im.WithWriteTransaction(writeCtx, tx)
	// Each result names the attributes its write changed
	// ([im.WriteResult.Commit]); only those are reported. A host notifier's
	// changes of the endpoints written meanwhile are settled against the
	// same results when the write ends ([Bridge.applyWriteHeld]).
	outcome := b.applyWriteHeld(writeCtx, dispatcher, req)
	resp := outcome.Response
	// An interaction that ended at a data element (im.WriteResponse.Status)
	// ends the transaction: the elements before it were written and are
	// reported, and the controller gets the StatusResponse — matter.js's
	// handleWriteRequest leaves with the error, the messenger answers it
	// (InteractionMessenger.ts:431-460 at db021da0).
	if !resp.Status.IsSuccess() {
		b.endWriteInteraction(txKey)
		b.reportWrittenAttributes(outcome.Results)
		return b.replyStatusUnlessSuppressed(req.SuppressResponse, src, requestHdr, proto, "write_ended", resp.Status)
	}
	b.endWriteChunk(txKey, tx, req.MoreChunkedMessages)
	b.reportWrittenAttributes(outcome.Results)
	// Honor SuppressResponse=true per Matter §10.6.3.1: when the
	// initiator opts out of the WriteResponse the server MUST
	// elide it. matter.js InteractionServer.ts and chip
	// WriteHandler.cpp both gate the reply on this flag. We still
	// drive the side-effects (cluster writes already happened
	// inside HandleWriteRequest) and send the owed MRP ack, which no
	// reply carries ([Bridge.ackUnansweredRequest]).
	if req.SuppressResponse {
		b.ackUnansweredRequest(requestHdr, proto)
		b.logger.Debug("matter.rx.im.write.suppressed",
			slog.String("src", srcString(src)),
			slog.Int("attribute_statuses", len(resp.Responses)))
		return nil
	}
	body, err := EncodeWriteResponse(resp)
	if err != nil {
		debugReplyError(b.logger, "encode_write", src, err)
		return err
	}
	// Reliable: a lost WriteResponse leaves the controller waiting and
	// retrying the whole write. matter.js ships every non-standalone-ack
	// reply on an MRP session reliably (MessageExchange.ts:602
	// `requiresAck ?? (session.usesMrp && !isStandaloneAck)`); the ACK
	// pump retransmits until the peer acks. The response is a pure
	// outcome report, so it meets sendReplyReliable's idempotency
	// contract.
	if err := b.sendReplyReliable(src, requestHdr, proto, im.OpcodeWriteResponse, body); err != nil {
		debugReplyError(b.logger, "send_write", src, err)
		return err
	}
	b.dischargeOwedAck(requestHdr.SessionID, proto.ExchangeID, !proto.Initiator)
	b.logger.Debug("matter.rx.im.write",
		slog.String("src", srcString(src)),
		slog.Int("attribute_statuses", len(resp.Responses)))
	// Surface non-Success statuses one-per-line so failed writes
	// (notably AccessControl CONSTRAINT_ERROR during Apple pair)
	// leave a forensic trail. Apple's homed prints only its own
	// MTRInteractionErrorDomain mapping, never the raw IM status
	// code — without daemon-side logging the chain of cause is
	// impossible to reconstruct from the wire alone.
	for _, r := range resp.Responses {
		if r.Status.Status.IsSuccess() {
			continue
		}
		b.logger.Warn("matter.tx.im.write_status",
			slog.String("src", srcString(src)),
			slog.Any("endpoint", r.Path.Endpoint),
			slog.Any("cluster", r.Path.Cluster),
			slog.Any("attribute", r.Path.Attribute),
			slog.Any("status_code", uint8(r.Status.Status)),
			slog.String("status_name", r.Status.Status.String()),
			slog.Uint64("fabric_index", uint64(writeFabricIndex)))
	}
	return nil
}

// allConcreteInvokes reports whether every command of a request names its
// endpoint.
func allConcreteInvokes(invokes []im.CommandInvocation) bool {
	for _, inv := range invokes {
		if !inv.Path.HasEndpoint {
			return false
		}
	}
	return true
}

// Timed interactions, as matter.js splits them: the interaction-level gate
// compares the request's own Timed flag with the exchange's timed window
// (a mismatch either way is TIMED_REQUEST_MISMATCH, an expired window
// TIMEOUT — InteractionServer.ts:993-1004 at db021da0), and a command marked "T" in the
// matter.js model (the resolved server's contract.TimedCommander, else
// schema.IsTimedInvoke) invoked outside a timed interaction answers
// NEEDS_TIMED_INTERACTION for its own path in the InvokeResponse
// (CommandInvokeResponse.ts:292-295), via [im.WithTimedInteraction].
func (b *Bridge) dispatchInvokeRequest(ctx context.Context, src net.Addr, requestHdr *message.Header, proto message.ProtocolHeader, dispatcher im.Dispatcher, req im.InvokeRequest) error { //nolint:funlen // the invoke path's gates in their wire order, read top to bottom
	if status, gated := b.checkTimedGate(req.TimedRequest, requestHdr.SessionID, proto.ExchangeID); gated {
		return b.replyStatusUnlessSuppressed(req.SuppressResponse, src, requestHdr, proto, "invoke", status)
	}
	// Batch-invoke path validation: a malformed batch (wildcard-endpoint path
	// mixed with others, a concrete path missing its CommandRef, a duplicate
	// CommandRef, or a duplicate concrete path) is rejected up front with a
	// top-level StatusResponse(InvalidAction) instead of dispatching any
	// command. Mirrors matter.js
	// packages/protocol/src/action/server/CommandInvokeResponse.ts:68-99 at
	// db021da0 (process) and :196-212 (#processConcrete), whose StatusResponseError(InvalidAction)
	// aborts the whole invoke before a producer runs. Runs after the timed gate
	// because matter.js validates the timed window first (InteractionServer.ts
	// handleInvokeRequest).
	if status := im.ValidateInvokeBatchWithin(req, int(b.cfg.MaxPathsPerInvoke)); status != im.StatusSuccess {
		if req.SuppressResponse {
			return b.replyStatusUnlessSuppressed(true, src, requestHdr, proto, "invoke_batch", status)
		}
		body, err := EncodeStatusResponse(im.StatusResponse{Status: status})
		if err != nil {
			debugReplyError(b.logger, "encode_invoke_batch_reject", src, err)
			return err
		}
		if err := b.sendReply(src, requestHdr, proto, im.OpcodeStatusResponse, body); err != nil {
			debugReplyError(b.logger, "send_invoke_batch_reject", src, err)
			return err
		}
		b.dischargeOwedAck(requestHdr.SessionID, proto.ExchangeID, !proto.Initiator)
		b.logger.Debug("matter.rx.im.invoke.batch_reject",
			slog.String("src", srcString(src)),
			slog.Int("invokes", len(req.Invokes)),
			slog.Any("status_code", uint8(status)))
		return nil
	}
	// Only a batch can answer in several chunks, each but the last
	// waiting for the peer's StatusResponse: a batch takes one of the
	// session's [maxChunkWaitsPerSession] claims before any command runs,
	// and beyond them is answered Busy (review II, R2-P1-req2-01).
	release, claimed := b.claimChunkWait(ctx, requestHdr.SessionID, len(req.Invokes) > 1)
	if !claimed {
		return b.replyStatusUnlessSuppressed(req.SuppressResponse, src, requestHdr, proto, "invoke_chunk_waits", im.StatusBusy)
	}
	defer release()
	// Stamp the FabricIndex into the context so cluster handlers
	// can distinguish PASE (0) from CASE (>0) — required by
	// GeneralCommissioning.CommissioningComplete (matter.js
	// GeneralCommissioningServer.ts:218-243), AccessControl Write
	// validation (AuthMode-vs-fabric coupling), and
	// OperationalCredentials AddNOC PASE-only guard.
	invokeFabricIndex := b.resolveSessionFabric(requestHdr.SessionID)
	invokeSubjectNodeID, invokeSubjectCATs := b.resolveSessionSubject(requestHdr.SessionID)
	invokeCtx := im.WithFabricFilter(ctx, false, invokeFabricIndex)
	invokeCtx = im.WithSubject(invokeCtx, invokeSubjectNodeID, invokeSubjectCATs)
	if b.resolveSessionPASE(requestHdr.SessionID) {
		invokeCtx = im.WithAuthModePASE(invokeCtx)
	}
	// Stamp the operational session ID so
	// OperationalCredentials.handleAddNOC can verify it matches
	// the session that issued the CSRRequest (matter.js
	// OperationalCredentialsServer.ts session-ID binding guard).
	invokeCtx = core.WithInvokeSessionID(invokeCtx, requestHdr.SessionID)
	// And the session's identity, which a session of another stack (a
	// node's client stack) never shares: the CSR of a fail-safe binds to it
	// whichever stack holds the session ([core.WithInvokeSession]; review
	// II, Fable R2-1).
	if sess := b.lookupSession(requestHdr.SessionID); sess != nil {
		invokeCtx = core.WithInvokeSession(invokeCtx, sess)
	}
	if challenge, ok := b.sessionAttestationChallenge(requestHdr.SessionID); ok {
		invokeCtx = core.WithInvokeAttestationChallenge(invokeCtx, challenge)
	}
	// The gate above passed, so a set Timed flag means a valid window.
	invokeCtx = im.WithTimedInteraction(invokeCtx, req.TimedRequest)
	// Whether the session carries Large Messages: it does exactly over a
	// TCP connection ([routeCarriesLargeMessages]); the invoke path refuses
	// a Large Message command otherwise ([im.WithLargeMessageSession]).
	invokeCtx = im.WithLargeMessageSession(invokeCtx, routeCarriesLargeMessages(src))
	// A server that sizes its response by the exchange (GeneralDiagnostics
	// PayloadTestRequest) reads the largest payload one message of it
	// carries ([Bridge.invokeMaxPayloadSize]).
	invokeCtx = im.WithMaxPayloadSize(invokeCtx, b.invokeMaxPayloadSize(src, requestHdr.SessionID))
	// Work a command defers until its response is out — the PASE session
	// a RevokeCommissioning over PASE closes — runs when this returns,
	// whichever way: the reply sent, suppressed, or failed.
	after := &im.AfterResponse{}
	defer after.Run()
	invokeCtx = im.WithAfterResponse(invokeCtx, after)
	if req.DelayReportData != nil {
		b.deferReportsFor(invokeCtx, dispatcher, req)
	}
	before := b.snapshotInvokedClusters(invokeCtx, dispatcher, req)
	var resp im.InvokeResponse
	func() {
		// Deferred, so a command that panics still ends the commits
		// snapshotInvokedClusters opened.
		defer b.reportInvokeChanges(invokeCtx, dispatcher, before)
		resp = im.HandleInvokeRequest(invokeCtx, dispatcher, req)
		b.adoptPASESessionOnAddNOC(requestHdr.SessionID, resp)
		b.closePASESessionsOnCommissioningComplete(invokeCtx, resp)
	}()
	for i := range resp.Responses {
		rewriteInvokeResponseCommand(&resp.Responses[i])
	}
	// SuppressResponse handling per Matter §8.8.3.2.1: a suppress-response invoke
	// still ships its InvokeResponse when a CommandDataIB was generated, but sends
	// nothing when the response carries only CommandStatusIB entries. Mirrors
	// matter.js packages/node/src/node/server/InteractionServer.ts:1168-1199 at db021da0
	// (the held `suppressedBuffer` is discarded — no message sent — when no
	// cmd-response is produced). The command side-effects already ran inside
	// HandleInvokeRequest; we only elide the wire reply and send the owed
	// MRP ack (the controller opted out of the StatusResponse handshake, so the
	// Standalone Ack is all it expects; [Bridge.ackUnansweredRequest]).
	// Every command of the request was answered with nothing — a handler
	// returned [im.NoResponse], which leaves its command out: no
	// InvokeResponse is sent, only the owed acknowledgement (review II,
	// R2-P6-fsm-1001, F-db021da0-10; the NetworkCommissioning server's
	// ConnectNetwork that outlived its fail-safe, as connectedhomeip). A
	// concrete command always leaves an entry otherwise, so the test is
	// exact for a request of concrete commands. A request with a
	// wildcard-endpoint command is always answered, a target that
	// returned [im.NoResponse] left out of the response message — empty when
	// every target did (decision 182): matter.js expands a path without
	// an endpoint and answers every target it invokes
	// (packages/protocol/src/action/server/CommandInvokeResponse.ts:78-85,
	// :495-508 at db021da0), and connectedhomeip never dispatches such a
	// unicast command (src/app/CommandHandlerImpl.cpp:278-282 at
	// 6170af84), so leaving a target out is this module's rule
	// (by_design).
	if len(resp.Responses) == 0 && len(req.Invokes) > 0 && allConcreteInvokes(req.Invokes) {
		b.ackUnansweredRequest(requestHdr, proto)
		b.logger.Debug("matter.rx.im.invoke.no_response",
			slog.String("src", srcString(src)),
			slog.Int("invokes", len(req.Invokes)))
		return nil
	}
	if req.SuppressResponse && !resp.HasCommandData() {
		b.ackUnansweredRequest(requestHdr, proto)
		b.logger.Debug("matter.rx.im.invoke.suppressed",
			slog.String("src", srcString(src)),
			slog.Int("statuses", len(resp.Responses)))
		return nil
	}
	limits := b.chunkLimitsFor(src, requestHdr.SessionID)
	chunks, err := chunkInvokeResponse(resp, limits.budget, limits.hardCap)
	if err != nil {
		debugReplyError(b.logger, "encode_invoke", src, err)
		return err
	}
	// All but the last chunk wait for the controller's StatusResponse
	// before the next goes out, as for a chunked read (matter.js
	// InteractionMessenger.ts sendInvokeResponse → waitForSuccess).
	leg := b.replyChunkLeg(ctx, "invoke", src, requestHdr, proto, im.OpcodeInvokeResponse)
	for i, chunk := range chunks[:len(chunks)-1] {
		if _, err := b.sendChunk(leg, i, chunk, im.ReportData{MoreChunkedMessages: true}, chunkWaitHere); err != nil {
			return err
		}
	}
	body := chunks[len(chunks)-1]
	if len(chunks) > 1 {
		lastHdr := *requestHdr
		b.refreshAckCounter(&lastHdr, proto.ExchangeID, !proto.Initiator)
		requestHdr = &lastHdr
	}
	// Reliable: a lost InvokeResponse surfaces to the controller as
	// "Not Responding" (Apple Home) even though the command executed.
	// matter.js ships every non-standalone-ack reply on an MRP session
	// reliably (MessageExchange.ts:602); the ACK pump retransmits the
	// identical datagram until the peer acks. The response is a pure
	// outcome report, meeting sendReplyReliable's idempotency contract.
	if err := b.sendReplyReliable(src, requestHdr, proto, im.OpcodeInvokeResponse, body); err != nil {
		debugReplyError(b.logger, "send_invoke", src, err)
		return err
	}
	b.dischargeOwedAck(requestHdr.SessionID, proto.ExchangeID, !proto.Initiator)
	// Diagnose: capture Endpoint/Cluster/Command per InvokeRequest +
	// Status per InvokeResponse. Apple retransmit-loops on Invokes
	// whose response is malformed or contains an unexpected status,
	// so we need to see which command Apple insists on and what we
	// reply.
	reqPaths := make([]string, 0, len(req.Invokes))
	for _, ir := range req.Invokes {
		reqPaths = append(reqPaths, fmt.Sprintf("ep=%d cl=0x%04X cmd=0x%X", ir.Path.Endpoint, ir.Path.Cluster, ir.Path.Command))
	}
	respStatuses := make([]string, 0, len(resp.Responses))
	for _, r := range resp.Responses {
		if r.IsStatus {
			respStatuses = append(respStatuses, fmt.Sprintf("status=%d (path=ep=%d cl=0x%04X cmd=0x%X)", r.Status.Status, r.Path.Endpoint, r.Path.Cluster, r.Path.Command))
		} else {
			respStatuses = append(respStatuses, fmt.Sprintf("cmd_data cmd=0x%X has_response=%v", r.Path.Command, r.HasResponse))
		}
	}
	b.logger.Debug("matter.rx.im.invoke",
		slog.String("src", srcString(src)),
		slog.Int("responses", len(resp.Responses)),
		slog.Int("session_id", int(requestHdr.SessionID)),
		slog.Int("invoke_fabric_index", int(invokeFabricIndex)),
		slog.String("req_paths", strings.Join(reqPaths, ",")),
		slog.String("resp", strings.Join(respStatuses, ",")))
	return nil
}

// endWriteInteraction ends the Write interaction of key: its transaction
// leaves the exchange's table and gives back every hold, as matter.js's
// handleWriteRequest releases its held paths whichever way it leaves
// (packages/node/src/node/server/InteractionServer.ts:460-469 at db021da0,
// the disposal of `_held`). A transaction no longer in the table is a
// fresh one, whose release is a no-op.
func (b *Bridge) endWriteInteraction(key mrp.ExchangeKey) {
	b.routing.writeTransaction(key, false, b.clk().Now()).Release()
}

// dispatchTimedRequest handles a decoded TimedRequest.
// TimedRequest gates a follow-up Write / Invoke against a
// per-exchange deadline (Matter §8.7). The bridge captures
// the deadline in `exchangeRouting.timedDeadlines` so the next Write / Invoke
// on the same exchange can be checked against it; expired
// or missing-prior-TimedRequest cases are rejected via
// StatusResponse with the spec-mandated codes.
func (b *Bridge) dispatchTimedRequest(src net.Addr, requestHdr *message.Header, proto message.ProtocolHeader, req im.TimedRequest) error {
	now := b.clk().Now()
	// A session that already has [maxPendingTimedRequests] timed
	// interactions waiting gets Busy and no deadline: without the bound
	// a session could register one entry per exchange id until the
	// deadlines ran out (review II, W2-conc-12). connectedhomeip answers
	// Busy when its pool of timed handlers is exhausted
	// (src/app/InteractionModelEngine.cpp:963-973 at 6170af84).
	if b.routing.pendingTimedRequests(requestHdr.SessionID, proto.ExchangeID, now) >= maxPendingTimedRequests {
		return b.replyTimedStatus(src, requestHdr, proto, "too_many_pending", im.StatusBusy)
	}
	// A TimedRequest on an exchange whose timed window still runs ends
	// that window and is answered InvalidAction. Mirrors matter.js
	// packages/protocol/src/protocol/MessageExchange.ts:1066-1070 at
	// db021da0 (startTimedInteraction stops the running timer and throws
	// "Timed interaction already running for this exchange"); connectedhomeip
	// src/app/TimedHandler.cpp:91-96 at 6170af84 answers anything but the
	// Invoke or Write the window waits for InvalidAction. A window that
	// expired is replaced.
	if b.timedWindowRunning(requestHdr.SessionID, proto.ExchangeID) {
		b.routing.timedDeadlines.Delete(timedKey{sessionID: requestHdr.SessionID, exchangeID: proto.ExchangeID})
		return b.replyTimedStatus(src, requestHdr, proto, "already_running", im.StatusInvalidAction)
	}
	deadline := now.Add(time.Duration(req.TimeoutMs) * time.Millisecond)
	// Key on (sessionID, exchangeID) so a different session cannot
	// consume a deadline registered by another session.
	b.routing.timedDeadlines.Store(timedKey{sessionID: requestHdr.SessionID, exchangeID: proto.ExchangeID}, deadline)
	b.wakeAckPump() // the pump sweeps a deadline its follow-up never consumes
	// Reclaim abandoned deadlines at the site that creates them: a
	// controller whose follow-up Write / Invoke never arrives leaves an
	// entry nothing consumes, so the table would otherwise grow for the
	// daemon's whole uptime. Amortised — see [timedSweepInterval].
	b.routing.maybeSweepExpiredTimedDeadlines(now)
	body, err := EncodeStatusResponse(im.StatusResponse{Status: im.StatusSuccess})
	if err != nil {
		debugReplyError(b.logger, "encode_status", src, err)
		return err
	}
	// Reliable: this StatusResponse is the go-ahead the controller waits
	// for before sending its timed Write/Invoke (Matter §8.7). If it is
	// dropped the timed action never arrives and the exchange dies.
	// matter.js ships it reliably (MessageExchange.ts:602).
	if err := b.sendReplyReliable(src, requestHdr, proto, im.OpcodeStatusResponse, body); err != nil {
		debugReplyError(b.logger, "send_status", src, err)
		return err
	}
	b.dischargeOwedAck(requestHdr.SessionID, proto.ExchangeID, !proto.Initiator)
	b.logger.Debug("matter.rx.im.timed",
		slog.String("src", srcString(src)),
		slog.Int("timeout_ms", int(req.TimeoutMs)))
	return nil
}

// reportWrittenAttributes marks every attribute a write changed dirty for
// the subscriptions that cover it. (The dispatcher has already advanced
// the cluster's DataVersion: endpoint/dispatcher.go WriteAuthorized for a
// bridged endpoint, the server's own tracker for the root.) A write is a state change
// like any other: matter.js commits it to the behavior's state, whose
// Datasource advances the version and broadcasts the changed property to
// every subscriber (Datasource.ts). Without this a subscriber learned of a
// write — its own or another controller's — only when the cluster happened
// to fire a change notification of its own; a root attribute such as
// BasicInformation.NodeLabel was never reported at all. Found by the CHIP
// Python harness (TC-IDM-2.3 step 4).
//
// The attributes marked are the ones each successful write's verdict names
// ([im.WriteResult.Commit]): the written one only when it changed, and any
// other the write moved with it; a write with no verdict marks its written
// attribute. matter.js broadcasts only the properties a commit changed
// (packages/node/src/behavior/state/managed/Datasource.ts:1319 at
// db021da0, onChange with the change list; a name noted only when its
// value differs, :1476-1484) and advances no version for an equal value
// (:1456-1457), and the dispatcher left the version as it was.
func (b *Bridge) reportWrittenAttributes(results []im.WriteResult) {
	mgr := b.subscriptionManagerLocked()
	if mgr == nil {
		return
	}
	rec := recordOf(results)
	for _, p := range slices.Concat(rec.changed, rec.unjudged) {
		mgr.OnAttributeChanged(p)
	}
}

// invokedCluster names one cluster instance an invoke ran against.
type invokedCluster struct {
	endpoint uint16
	cluster  uint32
}

// snapshotInvokedClusters reads every attribute of each cluster an invoke
// path reaches, before the commands run, and opens the commit of each
// ([invokeCommitTable]) that [Bridge.reportInvokeChanges] ends. A concrete
// path reaches its endpoint; a wildcard-endpoint path — a group command's
// only shape — every endpoint the command may run at ([commandEndpoints]),
// so its change is one commit for subscribers and the DataVersion as a
// concrete path's is (R2-P6-par-0401): matter.js runs and commits a
// wildcard-endpoint or group invoke per endpoint exactly as a concrete
// one, and the commit is what advances the DataVersion and announces the
// change (CommandInvokeResponse.ts:330-358, :493; Datasource.ts:1455-1457,
// :1319 at db021da0). A subscription manager is required for the
// comparison to matter; without one this is a no-op.
func (b *Bridge) snapshotInvokedClusters(ctx context.Context, d im.Dispatcher, req im.InvokeRequest) map[invokedCluster]map[uint32]attributeReading {
	if b.subscriptionManagerLocked() == nil {
		return nil
	}
	out := map[invokedCluster]map[uint32]attributeReading{}
	for _, inv := range req.Invokes {
		for _, endpoint := range commandEndpoints(ctx, d, inv.Path) {
			key := invokedCluster{endpoint, inv.Path.Cluster}
			if _, done := out[key]; !done {
				out[key] = readCluster(ctx, d, key)
			}
		}
	}
	for key := range out {
		b.invokeCommits.begin(key)
	}
	return out
}

// commandTargetLister is the part of a dispatcher that names the
// endpoints a wildcard-endpoint command runs at
// (endpoint.TopologyDispatcher.CommandTargets).
type commandTargetLister interface {
	CommandTargets(path im.ConcreteCommandPath, authorize im.CommandAuthorizer) []uint16
}

// commandEndpoints is the endpoints path may run at: its own for a
// concrete path; for a wildcard one every endpoint whose server accepts
// the command, narrowed to the group's endpoints for a group command. The
// access check is not asked — a snapshot of an endpoint the command then
// does not reach compares equal and reports nothing.
func commandEndpoints(ctx context.Context, d im.Dispatcher, path im.ConcreteCommandPath) []uint16 {
	if path.HasEndpoint {
		return []uint16{path.Endpoint}
	}
	lister, ok := d.(commandTargetLister)
	if !ok || !path.HasCluster || !path.HasCommand {
		return nil
	}
	targets := lister.CommandTargets(path, nil)
	if group, isGroup := im.GroupSubjectFromContext(ctx); isGroup {
		targets = slices.DeleteFunc(slices.Clone(targets), func(e uint16) bool { return !slices.Contains(group.Endpoints, e) })
	}
	return targets
}

// invokeCommitTable makes a command one commit with one DataVersion
// advance (review II, R2-P2-dr-req-03). A cluster server that reports its
// own changes — [contract.AttributeChangeNotifier], [contract.ChangeNotifier]
// — would advance the version on each report, and the before/after
// comparison of [Bridge.reportInvokeChanges] once more. While a command
// runs against a cluster instance, its servers' reports are held here
// instead of advancing the version and marking the attributes dirty; the
// command's end advances once and marks the held attributes with the
// compared ones (matter.js packages/node/src/behavior/state/managed/
// Datasource.ts:1446-1457 at db021da0: a commit notes each changed
// property, then advances the version once). A report outside a command is not held. Nothing is reported
// before the version moves, so no report carries the command's change at
// the version before it.
type invokeCommitTable struct {
	mu   sync.Mutex
	open map[invokedCluster]*invokeCommit
}

// invokeCommit is one cluster instance's open commit: the commands
// running against it and the attributes its servers reported meanwhile.
type invokeCommit struct {
	running int
	held    []uint32
	// advance: a server changed an attribute meanwhile whose report waits
	// for its throttle ([Bridge.quieterChanged]); the commit advances the
	// version for it without marking it dirty.
	advance bool
}

// begin opens (or joins) the commit of key for one command.
func (t *invokeCommitTable) begin(key invokedCluster) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.open == nil {
		t.open = make(map[invokedCluster]*invokeCommit)
	}
	c, ok := t.open[key]
	if !ok {
		c = &invokeCommit{}
		t.open[key] = c
	}
	c.running++
}

// hold records attrs as changed in the commit of (endpoint, cluster) and
// reports true when one is open; false leaves the change to the caller.
func (t *invokeCommitTable) hold(endpoint uint16, cluster uint32, attrs []uint32) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	c, ok := t.open[invokedCluster{endpoint, cluster}]
	if !ok {
		return false
	}
	for _, attr := range attrs {
		if !slices.Contains(c.held, attr) {
			c.held = append(c.held, attr)
		}
	}
	return true
}

// end ends one command's part in the commit of key and returns the
// attributes held so far and whether an advance without a report is owed
// ([invokeCommitTable.advanceAtEnd]); the commit closes with its last
// command.
func (t *invokeCommitTable) end(key invokedCluster) (held []uint32, advance bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	c, ok := t.open[key]
	if !ok {
		return nil, false
	}
	held, advance = c.held, c.advance
	c.held, c.advance = nil, false
	c.running--
	if c.running <= 0 {
		delete(t.open, key)
	}
	return held, advance
}

// reportInvokeChanges marks dirty every attribute whose value a command
// changed or whose server reported it changed while the command ran
// ([invokeCommitTable]), after advancing the cluster's DataVersion once,
// and ends the commits [Bridge.snapshotInvokedClusters] opened. A command is a
// state change like a write: matter.js commits whatever the command handler
// assigned to the behavior's state, and its Datasource advances the version
// and reports the changed properties (Datasource.ts). Root servers here
// change their state inside MatterInvoke without a change notification —
// GeneralCommissioning's Breadcrumb on ArmFailSafe, OperationalCredentials'
// Fabrics on AddNOC — so without this comparison no subscriber ever saw
// those changes. Found by the CHIP Python harness (TC-IDM-1.5 subscribes to
// Breadcrumb and arms the fail-safe). The comparison is by value
// ([attributeReading]), as matter.js's Datasource deep-compares.
func (b *Bridge) reportInvokeChanges(ctx context.Context, d im.Dispatcher, before map[invokedCluster]map[uint32]attributeReading) {
	if len(before) == 0 {
		return
	}
	// A manager detached while the command ran still gets the advance:
	// the commit changed the cluster whether or not anyone subscribes, and
	// a later read with a DataVersionFilter must see it; only the marks
	// need a manager (N-findev-2).
	mgr := b.subscriptionManagerLocked()
	topo := b.Topology()
	// Each commit ends after its cluster's read, so a report between the
	// two is returned by end, not taken twice. A read that panics still
	// ends every commit not ended yet — its own and those after it — and
	// what their servers reported while the command ran is reported without
	// the comparison: an open commit would hold every later report of its
	// cluster (review II, R2-P2-rr-dev-04).
	ended := make(map[invokedCluster]bool, len(before))
	defer func() {
		type endedCommit struct {
			held    []uint32
			advance bool
		}
		left := make(map[invokedCluster]endedCommit)
		for key := range before {
			if !ended[key] {
				ended[key] = true
				var e endedCommit
				e.held, e.advance = b.invokeCommits.end(key)
				left[key] = e
			}
		}
		for key, e := range left {
			reportCommandChange(topo, mgr, key, e.held, e.advance)
		}
	}()
	for key, old := range before {
		now := readCluster(ctx, d, key)
		changed, advance := b.invokeCommits.end(key)
		ended[key] = true
		selfReported := selfReportedAttributes(topo, key)
		for attr, v := range now {
			if attr >= 0xFFF8 || selfReported[attr] {
				continue
			}
			if prev, ok := old[attr]; ok && prev.equal(v) {
				continue
			}
			if !slices.Contains(changed, attr) {
				changed = append(changed, attr)
			}
		}
		reportCommandChange(topo, mgr, key, changed, advance)
	}
}

// reportCommandChange advances key's DataVersion once and marks changed
// dirty; advance owes the advance with nothing to mark — a quieter
// attribute whose report waits for its throttle — and nothing changed
// without it moves nothing. A nil mgr advances without marking. matter.js advances the version at the commit
// that changed a value, a quieter one's included
// (packages/node/src/behavior/state/managed/Datasource.ts:1446-1457 at
// db021da0).
func reportCommandChange(topo *endpointpkg.Topology, mgr *subscription.Manager, key invokedCluster, changed []uint32, advance bool) {
	if len(changed) == 0 && !advance {
		return
	}
	if topo != nil {
		if ep := topo.FindByID(key.endpoint); ep != nil {
			ep.BumpDataVersionOf(key.cluster)
		}
	}
	if mgr == nil {
		return
	}
	for _, attr := range changed {
		mgr.OnAttributeChanged(im.ConcreteAttributePath{
			Endpoint: key.endpoint, Cluster: key.cluster, Attribute: attr,
			HasEndpoint: true, HasCluster: true, HasAttribute: true,
		})
	}
}

// selfReportedAttributes lists the attributes the invoked cluster's server
// reports by itself ([contract.SelfReportedAttributeLister]): a quieter
// attribute a transition moves, or RemainingTime. Their reports follow
// the server's own rules, as matter.js reports a quieter property only
// when its QuietEvent emits, so the before/after comparison leaves them
// out.
func selfReportedAttributes(topo *endpointpkg.Topology, key invokedCluster) map[uint32]bool {
	if topo == nil {
		return nil
	}
	ep := topo.FindByID(key.endpoint)
	if ep == nil {
		return nil
	}
	for _, srv := range endpointpkg.ClusterServers(ep) {
		if srv == nil || srv.MatterClusterID() != key.cluster {
			continue
		}
		lister, ok := srv.(contract.SelfReportedAttributeLister)
		if !ok {
			return nil
		}
		out := map[uint32]bool{}
		for _, attr := range lister.MatterSelfReportedAttributes() {
			out[attr] = true
		}
		return out
	}
	return nil
}

// readCluster reads every attribute of one cluster instance and captures
// each value for comparison.
//
// The snapshot is the bridge's own and is never delivered, so it reads
// without the request behind the command: a request-free read sees every
// attribute whole — no access check, no read-access refusal, no fabric
// filter — and a change the command makes is seen whoever invoked it; each
// subscriber's report stays filtered by its own request (fable-read-6a-gate
// §6).
func readCluster(_ context.Context, d im.Dispatcher, key invokedCluster) map[uint32]attributeReading {
	out := map[uint32]attributeReading{}
	path := im.ConcreteAttributePath{Endpoint: key.endpoint, Cluster: key.cluster, HasEndpoint: true, HasCluster: true}
	//nolint:contextcheck // the snapshot reads without the request behind the command (above)
	for _, r := range d.Read(context.Background(), path) {
		if r.Status == im.StatusSuccess {
			out[r.Path.Attribute] = readingOf(r.Value)
		}
	}
	return out
}

// chunkInvokeResponse encodes an InvokeResponse into one message, or — when
// its responses do not fit one — into as many as needed, every one but the
// last carrying MoreChunkedMessages. Responses are never split. Mirrors
// matter.js InteractionServer invoke chunking (the InvokeResponse split by
// maxPayloadSize between responses,
// packages/node/src/node/server/InteractionServer.ts:1123-1151 at db021da0).
//
// A single response too large for a message on its own (hardCap, from
// udp.MaxDatagramSize over UDP) is replaced by a ResourceExhausted status
// for its path — after the command ran. That is this module's own answer:
// matter.js has none, it pushes a lone response whatever its size and sends
// the oversized message over UDP with a warning ("This only works if both
// nodes support it"), throwing MessageTooLargeError over TCP
// (packages/protocol/src/protocol/MessageChannel.ts:106-116); here the UDP
// listener refuses a datagram above its cap, so the controller would get
// nothing at all. Found by the CHIP Python harness
// (TC-IDM-1.4 step 11 batches two commands whose responses exceed one
// message).
func chunkInvokeResponse(resp im.InvokeResponse, budget, hardCap int) ([][]byte, error) { //nolint:unparam // the budget is the caller's; the tests pin the split at the production one
	whole, err := EncodeInvokeResponse(resp)
	if err != nil {
		return nil, err
	}
	if len(whole) <= budget || len(resp.Responses) <= 1 {
		if len(whole) > hardCap && len(resp.Responses) == 1 {
			resp.Responses[0] = oversizedInvokeEntry(resp.Responses[0])
			whole, err = EncodeInvokeResponse(resp)
			if err != nil {
				return nil, err
			}
		}
		return [][]byte{whole}, nil
	}
	var chunks [][]byte
	cur := im.InvokeResponse{SuppressResponse: resp.SuppressResponse}
	flush := func(more bool) error {
		cur.MoreChunkedMessages = more
		body, err := EncodeInvokeResponse(cur)
		if err != nil {
			return err
		}
		chunks = append(chunks, body)
		cur = im.InvokeResponse{SuppressResponse: resp.SuppressResponse}
		return nil
	}
	for _, ent := range resp.Responses {
		trial := cur
		trial.Responses = append(append([]im.InvokeResponseEntry(nil), cur.Responses...), ent)
		trial.MoreChunkedMessages = true
		body, err := EncodeInvokeResponse(trial)
		if err != nil {
			return nil, err
		}
		if len(body) > budget && len(cur.Responses) > 0 {
			if err := flush(true); err != nil {
				return nil, err
			}
			trial = im.InvokeResponse{SuppressResponse: resp.SuppressResponse, Responses: []im.InvokeResponseEntry{ent}, MoreChunkedMessages: true}
			if body, err = EncodeInvokeResponse(trial); err != nil {
				return nil, err
			}
		}
		if len(body) > hardCap {
			ent = oversizedInvokeEntry(ent)
		}
		cur.Responses = append(cur.Responses, ent)
	}
	if err := flush(false); err != nil {
		return nil, err
	}
	return chunks, nil
}

// oversizedInvokeEntry answers a response that cannot fit a message with
// ResourceExhausted for its path.
func oversizedInvokeEntry(ent im.InvokeResponseEntry) im.InvokeResponseEntry {
	return im.InvokeResponseEntry{
		Path: ent.Path, CommandRef: ent.CommandRef, HasCommandRef: ent.HasCommandRef,
		IsStatus: true, Status: im.StatusIB{Status: im.StatusResourceExhausted},
	}
}

// sessionFabricAdopter is the optional capability of the session table to
// move a session onto a fabric (operational.Manager.AdoptFabricIndex).
type sessionFabricAdopter interface {
	AdoptFabricIndex(sessionID uint16, fabricIndex uint8) error
}

// adoptPASESessionOnAddNOC moves the PASE session an AddNOC succeeded on
// onto the fabric it installed, so the commands that follow on it have
// that fabric as their accessing fabric — matter.js
// OperationalCredentialsServer.addNoc sets `session.fabric = fabric` for a
// PASE session, chip calls SecureSession::AdoptFabricIndex. Without it a
// CommissioningComplete sent over PASE after AddNOC was refused by the
// fabric-scoped access gate (UnsupportedAccess) instead of reaching the
// cluster, which answers InvalidAuthentication (TC-CGEN-2.4). The session
// stays a PASE session (its implicit Administer grant and the PASE-only
// rules still apply).
func (b *Bridge) adoptPASESessionOnAddNOC(sessionID uint16, resp im.InvokeResponse) {
	if sessionID == 0 || !b.resolveSessionPASE(sessionID) {
		return
	}
	for _, e := range resp.Responses {
		if e.Path.Cluster != 0x003E || !e.HasResponse { // OperationalCredentials
			continue
		}
		noc, ok := e.Response.(core.NOCResponse)
		if !ok || noc.StatusCode != core.NOCStatusOK || noc.FabricIndex == 0 {
			continue
		}
		b.mu.RLock()
		adopter, ok := b.sessionRegistry.(sessionFabricAdopter)
		if !ok {
			adopter, ok = b.sessions.(sessionFabricAdopter)
		}
		b.mu.RUnlock()
		if !ok {
			return
		}
		if err := adopter.AdoptFabricIndex(sessionID, noc.FabricIndex); err != nil {
			b.logger.Debug("matter.rx.im.addnoc.adopt_failed",
				slog.Int("session_id", int(sessionID)), slog.Any("err", err))
		}
	}
}

// lookupSession is the live session of the id, nil for none (or the
// unsecured id 0).
// endWriteChunk is [exchangeRouting.endWriteChunk] for a chunk of the
// session key names, then ends the session's chunked writes when the
// session is gone: a chunk that announces more and stored its transaction
// after the session's close dropped the table's entries
// ([exchangeRouting.dropSessionWriteTxs]) would otherwise keep its hold
// until a later write's sweep (N-req3-1). Without a session registry
// nothing is known to be gone.
func (b *Bridge) endWriteChunk(key mrp.ExchangeKey, tx *im.WriteTransaction, more bool) {
	b.routing.endWriteChunk(key, tx, more)
	if !more {
		return
	}
	b.mu.RLock()
	lookup := b.sessions
	b.mu.RUnlock()
	if lookup == nil {
		return
	}
	if _, live := lookup.Lookup(key.SessionID); !live {
		b.routing.dropSessionWriteTxs(key.SessionID)
	}
}

func (b *Bridge) lookupSession(sessionID uint16) *channel.Session {
	if sessionID == 0 {
		return nil
	}
	b.mu.RLock()
	lookup := b.sessions
	b.mu.RUnlock()
	if lookup == nil {
		return nil
	}
	sess, ok := lookup.Lookup(sessionID)
	if !ok {
		return nil
	}
	return sess
}

// sessionAttestationChallenger is the optional capability of the session
// table to name a session's attestation challenge
// (operational.Manager.AttestationChallengeFor).
type sessionAttestationChallenger interface {
	AttestationChallengeFor(sessionID uint16) ([]byte, bool)
}

// sessionAttestationChallenge returns the attestation challenge of the
// session an invoke arrived on, when the session table can name it.
func (b *Bridge) sessionAttestationChallenge(sessionID uint16) ([]byte, bool) {
	if sessionID == 0 {
		return nil, false
	}
	b.mu.RLock()
	c, ok := b.sessionRegistry.(sessionAttestationChallenger)
	if !ok {
		c, ok = b.sessions.(sessionAttestationChallenger)
	}
	b.mu.RUnlock()
	if !ok {
		return nil, false
	}
	return c.AttestationChallengeFor(sessionID)
}

// deferReportsFor holds off the next report of every subscription that
// selects an endpoint the invoke dispatches to, by DelayMinMs plus a
// random jitter below DelayJitterWindowMs: matter.js InteractionServer
// #deferReports, run before dispatch whether or not the commands succeed.
// The endpoints are those a command passes its checks on — access, fabric
// scope, timed quality, existence — and those a wildcard-endpoint command
// expands to ([im.DispatchEndpoints]); a command that fails its checks
// defers nothing (matter.js CommandInvokeResponse's dispatch endpoints,
// packages/protocol/src/action/server/CommandInvokeResponse.ts:159-165,
// :297 at db021da0). matter.js gates this behind its "delay-report-data"
// forward feature, which its own CHIP test support switches on
// (TC-IDM-1.5); the module acts on it always (BD-Matter-DelayReportData).
func (b *Bridge) deferReportsFor(ctx context.Context, dispatcher im.Dispatcher, req im.InvokeRequest) {
	mgr := b.subscriptionManagerLocked()
	if mgr == nil {
		return
	}
	endpoints := im.DispatchEndpoints(ctx, dispatcher, req)
	delay := time.Duration(req.DelayReportData.DelayMinMs) * time.Millisecond
	if w := req.DelayReportData.DelayJitterWindowMs; w > 0 {
		delay += time.Duration(rand.IntN(int(w))) * time.Millisecond //nolint:gosec // a report jitter, not a secret
	}
	mgr.DeferReports(endpoints, delay)
}

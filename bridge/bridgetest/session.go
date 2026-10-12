// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package bridgetest

// A secure session (PASE or CASE) and the Interaction Model client over
// it. Request shapes are the module's own IM encoders (im.ReadRequest,
// im.SubscribeRequest, im.MarshalInvokeRequestTLV, im.StatusResponse) and,
// for WriteRequest, which has no client encoder, the tags of the server
// decoder (im/write.go:16-19, im/read.go:40-42).

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"

	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/secure/channel"
	"github.com/SukramJ/go-fabric/tlv"
	"github.com/SukramJ/go-fabric/transport/message"
	"github.com/SukramJ/go-fabric/transport/mrp"
)

// ErrSessionClosed is returned by a call on a session that was closed —
// by [Session.Close], or by the device with a CloseSession StatusReport.
var ErrSessionClosed = errors.New("bridgetest: session closed")

// StatusError is an interaction the device ended with an IM
// StatusResponse instead of the expected message.
type StatusError struct {
	Status im.StatusCode
}

func (e *StatusError) Error() string {
	return "bridgetest: interaction ended with status " + e.Status.String()
}

// Session is one secure session with the device. It is safe for
// concurrent use.
type Session struct {
	c         *Commissioner
	ch        *channel.Session
	localID   uint16
	peer      *net.UDPAddr
	challenge []byte // the attestation challenge, PASE only
	reports   chan *Report

	mu      sync.Mutex
	partial map[uint16]*Report
	closed  bool
}

// PeerNodeID is the device's operational node id (0 on a PASE session).
func (s *Session) PeerNodeID() uint64 { return s.ch.PeerNodeID() }

// LocalNodeID is the controller node id the session authenticates (0 on
// a PASE session).
func (s *Session) LocalNodeID() uint64 { return s.ch.LocalNodeID() }

// Close ends the session: a CloseSession StatusReport to the device, then
// the keys are dropped. It is idempotent.
func (s *Session) Close() {
	if s.isClosed() {
		return
	}
	xid, _, release := s.c.openExchange(s.localID)
	defer release()
	_ = s.send(xid, mrp.SecureChannelProtocolID, mrp.SCOpcodeStatusReport,
		mrp.EncodeStatusReport(mrp.SCStatusGeneralSuccess, uint32(mrp.SecureChannelProtocolID), statusCloseSession, nil))
	s.drop()
}

// drop forgets the session without telling the device.
func (s *Session) drop() {
	s.markClosed()
	s.c.mu.Lock()
	delete(s.c.sessions, s.localID)
	s.c.mu.Unlock()
}

func (s *Session) markClosed() {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
}

func (s *Session) isClosed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

// sendProto seals one message with protocol header proto.
func (s *Session) sendProto(proto message.ProtocolHeader, payload []byte) error {
	hdr := message.Header{SessionID: s.ch.PeerSessionID()}
	sealed, err := s.ch.Encrypt(&hdr, hdr.SecurityFlags(), append(proto.Marshal(), payload...))
	if err != nil {
		return ErrSessionClosed
	}
	_, err = s.c.conn.WriteToUDP(append(hdr.Marshal(), sealed.Ciphertext...), s.peer)
	return err
}

// send sends a reliable message on an exchange the commissioner opened.
func (s *Session) send(xid, protocol uint16, opcode uint8, payload []byte) error {
	return s.sendProto(message.ProtocolHeader{Initiator: true, NeedsAck: true, Opcode: opcode, ExchangeID: xid, ProtocolID: protocol}, payload)
}

// reply sends a reliable IM message on an exchange the device opened.
func (s *Session) reply(xid uint16, opcode uint8, payload []byte) error {
	return s.sendProto(message.ProtocolHeader{NeedsAck: true, Opcode: opcode, ExchangeID: xid, ProtocolID: im.InteractionModelProtocolID}, payload)
}

// interaction is one IM exchange the commissioner opened.
type interaction struct {
	s       *Session
	xid     uint16
	ch      chan inbound
	release func()
}

// begin opens an exchange and sends its first message.
func (s *Session) begin(opcode uint8, payload []byte) (*interaction, error) {
	if s.isClosed() {
		return nil, ErrSessionClosed
	}
	xid, ch, release := s.c.openExchange(s.localID)
	if err := s.send(xid, im.InteractionModelProtocolID, opcode, payload); err != nil {
		release()
		return nil, err
	}
	return &interaction{s: s, xid: xid, ch: ch, release: release}, nil
}

// next waits for the device's next message on the exchange, which must
// carry opcode want; a StatusResponse in its place is a [StatusError].
func (x *interaction) next(ctx context.Context, want uint8) ([]byte, error) {
	msg, err := await(ctx, x.ch)
	if err != nil {
		return nil, err
	}
	if msg.proto.Opcode == want {
		return msg.payload, nil
	}
	if msg.proto.Opcode == im.OpcodeStatusResponse {
		tree, err := DecodeTLV(msg.payload)
		if err != nil {
			return nil, err
		}
		status, _ := tree.Child(0)
		return nil, &StatusError{Status: im.StatusCode(status.u8())}
	}
	return nil, fmt.Errorf("bridgetest: IM opcode 0x%02X, want 0x%02X", msg.proto.Opcode, want)
}

// reports collects a (possibly chunked) ReportData, answering every chunk
// that does not suppress its response.
func (x *interaction) reports(ctx context.Context) (*Report, error) {
	var report *Report
	for {
		payload, err := x.next(ctx, im.OpcodeReportData)
		if err != nil {
			return nil, err
		}
		chunk, err := decodeReport(payload)
		if err != nil {
			return nil, err
		}
		report = report.merge(chunk)
		if !chunk.suppress {
			if err := x.s.send(x.xid, im.InteractionModelProtocolID, im.OpcodeStatusResponse, statusResponse(im.StatusSuccess)); err != nil {
				return nil, err
			}
		}
		if !chunk.more {
			return report, nil
		}
	}
}

// Read reads paths (fabric-filtered) and returns the report.
func (s *Session) Read(ctx context.Context, paths ...im.ConcreteAttributePath) (*Report, error) {
	ctx, cancel := withDefaultTimeout(ctx)
	defer cancel()
	enc := tlv.NewEncoder()
	im.ReadRequest{AttributeRequests: paths, FabricFiltered: true}.MarshalTLV(enc)
	x, err := s.begin(im.OpcodeReadRequest, mustBytes(enc))
	if err != nil {
		return nil, err
	}
	defer x.release()
	return x.reports(ctx)
}

// Subscription is an established subscription.
type Subscription struct {
	// ID is the SubscriptionID the device assigned.
	ID uint32
	// MaxInterval is the negotiated maximum interval, in seconds.
	MaxInterval uint16
	// Priming is the priming report.
	Priming *Report
}

// Subscribe subscribes (fabric-filtered, KeepSubscriptions false) to
// paths. Later reports arrive through [Session.NextReport].
func (s *Session) Subscribe(ctx context.Context, minInterval, maxInterval uint16, paths ...im.ConcreteAttributePath) (*Subscription, error) {
	ctx, cancel := withDefaultTimeout(ctx)
	defer cancel()
	enc := tlv.NewEncoder()
	im.SubscribeRequest{MinIntervalFloor: minInterval, MaxIntervalCeiling: maxInterval, AttributeRequests: paths, FabricFiltered: true}.MarshalTLV(enc)
	x, err := s.begin(im.OpcodeSubscribeRequest, mustBytes(enc))
	if err != nil {
		return nil, err
	}
	defer x.release()
	priming, err := x.reports(ctx)
	if err != nil {
		return nil, err
	}
	payload, err := x.next(ctx, im.OpcodeSubscribeResponse)
	if err != nil {
		return nil, err
	}
	tree, err := DecodeTLV(payload)
	if err != nil {
		return nil, err
	}
	id, _ := tree.Child(0)
	interval, _ := tree.Child(2)
	return &Subscription{ID: id.u32(), MaxInterval: interval.u16(), Priming: priming}, nil
}

// NextReport returns the next report the device sent on its own — of a
// subscription made on this session, or one it re-established onto it.
func (s *Session) NextReport(ctx context.Context) (*Report, error) {
	ctx, cancel := withDefaultTimeout(ctx)
	defer cancel()
	select {
	case r := <-s.reports:
		return r, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Write writes one attribute and returns the status the device reported
// for it. value encodes the attribute value under the tag it is given.
func (s *Session) Write(ctx context.Context, path im.ConcreteAttributePath, value func(e *tlv.Encoder, tag tlv.Tag)) (im.StatusCode, error) {
	ctx, cancel := withDefaultTimeout(ctx)
	defer cancel()
	enc := tlv.NewEncoder()
	enc.StartStruct(tlv.AnonymousTag())
	enc.PutBool(tlv.ContextTag(0), false) // SuppressResponse
	enc.PutBool(tlv.ContextTag(1), false) // TimedRequest
	enc.StartArray(tlv.ContextTag(2))     // WriteRequests
	enc.StartStruct(tlv.AnonymousTag())   // AttributeDataIB
	path.MarshalTLV(enc, tlv.ContextTag(1))
	value(enc, tlv.ContextTag(2))
	_ = enc.EndContainer()
	_ = enc.EndContainer()
	enc.PutUint(tlv.ContextTag(0xFF), uint64(im.MatterInteractionModelRevision))
	_ = enc.EndContainer()
	x, err := s.begin(im.OpcodeWriteRequest, mustBytes(enc))
	if err != nil {
		return 0, err
	}
	defer x.release()
	payload, err := x.next(ctx, im.OpcodeWriteResponse)
	if err != nil {
		return 0, err
	}
	// WriteResponses [0] → AttributeStatusIB → [1] StatusIB → [0] Status.
	tree, err := DecodeTLV(payload)
	if err != nil {
		return 0, err
	}
	responses, _ := tree.Child(0)
	if len(responses.Children) != 1 {
		return 0, fmt.Errorf("bridgetest: WriteResponse with %d statuses, want 1", len(responses.Children))
	}
	status, _ := responses.Children[0].Child(1)
	code, _ := status.Child(0)
	return im.StatusCode(code.u8()), nil
}

// InvokeResult is the device's answer to one command: response fields, or
// a status.
type InvokeResult struct {
	// Command is the response command id; Fields its fields. Both are
	// zero when the device answered with a status.
	Command uint32
	Fields  TLV
	// Status is the CommandStatusIB status; Success alongside Fields.
	Status im.StatusCode
	// ClusterStatus is the cluster-specific status, when HasClusterStatus.
	ClusterStatus    uint8
	HasClusterStatus bool
}

// Invoke invokes one command. fields encodes the request's fields under
// the tag it is given — a generated request's EncodeTLV fits — and nil
// sends an empty structure.
func (s *Session) Invoke(ctx context.Context, path im.ConcreteCommandPath, fields func(e *tlv.Encoder, tag tlv.Tag)) (*InvokeResult, error) {
	ctx, cancel := withDefaultTimeout(ctx)
	defer cancel()
	path.HasEndpoint, path.HasCluster, path.HasCommand = true, true, true
	enc := tlv.NewEncoder()
	im.MarshalInvokeRequestTLV(enc, im.InvokeRequest{Invokes: []im.CommandInvocation{{Path: path}}}, func(e *tlv.Encoder, tag tlv.Tag, _ any) {
		if fields == nil {
			e.StartStruct(tag)
			_ = e.EndContainer()
			return
		}
		fields(e, tag)
	})
	x, err := s.begin(im.OpcodeInvokeRequest, mustBytes(enc))
	if err != nil {
		return nil, err
	}
	defer x.release()
	payload, err := x.next(ctx, im.OpcodeInvokeResponse)
	if err != nil {
		return nil, err
	}
	// InvokeResponses [1] → InvokeResponseIB → [0] CommandDataIB {[0] path,
	// [1] fields} | [1] CommandStatusIB {[0] path, [1] StatusIB}
	// (im/invoke.go:30-58).
	tree, err := DecodeTLV(payload)
	if err != nil {
		return nil, err
	}
	responses, _ := tree.Child(1)
	if len(responses.Children) != 1 {
		return nil, fmt.Errorf("bridgetest: InvokeResponse with %d entries, want 1", len(responses.Children))
	}
	entry := responses.Children[0]
	if data, ok := entry.Child(0); ok {
		cmdPath, _ := data.Child(0)
		command, _ := cmdPath.Child(2)
		f, _ := data.Child(1)
		return &InvokeResult{Command: command.u32(), Fields: f}, nil
	}
	status, _ := entry.Child(1)
	ib, _ := status.Child(1)
	code, _ := ib.Child(0)
	res := &InvokeResult{Status: im.StatusCode(code.u8())}
	if cs, ok := ib.Child(1); ok {
		res.ClusterStatus, res.HasClusterStatus = cs.u8(), true
	}
	return res, nil
}

// command invokes a root-endpoint command and returns its response
// fields; want 0 expects a status, which must be Success.
func (s *Session) command(ctx context.Context, cluster, cmd uint32, fields func(e *tlv.Encoder, tag tlv.Tag), want uint32) (TLV, error) {
	res, err := s.Invoke(ctx, im.ConcreteCommandPath{Cluster: cluster, Command: cmd}, fields)
	if err != nil {
		return TLV{}, err
	}
	if res.Command != want || !res.Status.IsSuccess() {
		return TLV{}, fmt.Errorf("bridgetest: command 0x%04X:0x%02X answered %d with status %s", cluster, cmd, res.Command, res.Status)
	}
	return res.Fields, nil
}

// statusResponse encodes an IM StatusResponse.
func statusResponse(status im.StatusCode) []byte {
	enc := tlv.NewEncoder()
	im.StatusResponse{Status: status}.MarshalTLV(enc)
	return mustBytes(enc)
}

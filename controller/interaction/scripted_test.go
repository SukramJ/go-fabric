// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package interaction_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/cluster/spec"
	gencomm "github.com/SukramJ/go-fabric/cluster/spec/generalcommissioning"
	"github.com/SukramJ/go-fabric/controller/exchange"
	"github.com/SukramJ/go-fabric/controller/interaction"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
)

var armPath = im.ConcreteCommandPath{Endpoint: 0, Cluster: gencomm.ClusterID, Command: gencomm.CmdArmFailSafe}

type invokeResult struct {
	res interaction.Result
	err error
}

func invokeAsync(c *interaction.Interaction, path im.ConcreteCommandPath, fields spec.Encodable, opts interaction.InvokeOptions) <-chan invokeResult {
	ch := make(chan invokeResult, 1)
	go func() {
		res, err := c.Invoke(context.Background(), path, fields, opts)
		ch <- invokeResult{res, err}
	}()
	return ch
}

func await[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(5 * time.Second):
		t.Fatal("the call did not return")
		var zero T
		return zero
	}
}

func encode(t *testing.T, f func(e *tlv.Encoder)) []byte {
	t.Helper()
	e := tlv.NewEncoder()
	f(e)
	b, err := e.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// invokeResponse encodes an InvokeResponse with the given entries.
func invokeResponse(t *testing.T, more bool, entries ...im.InvokeResponseEntry) []byte {
	t.Helper()
	return encode(t, func(e *tlv.Encoder) {
		im.InvokeResponse{Responses: entries, MoreChunkedMessages: more}.MarshalTLV(e, func(e *tlv.Encoder, tag tlv.Tag, v any) {
			v.(spec.Encodable).EncodeTLV(e, tag)
		})
	})
}

func statusResponse(t *testing.T, s im.StatusCode) []byte {
	t.Helper()
	return encode(t, func(e *tlv.Encoder) { im.StatusResponse{Status: s}.MarshalTLV(e) })
}

func decodeStatus(t *testing.T, b []byte) im.StatusCode {
	t.Helper()
	sr, err := im.UnmarshalStatusResponseTLV(tlv.NewDecoder(b))
	if err != nil {
		t.Fatal(err)
	}
	return sr.Status
}

// TestInvokeChunkedResponse: a response with fields over two chunks, the
// first acknowledged with a StatusResponse Success
// (InteractionMessenger.ts:1082-1105); the single command carries no
// commandRef (ClientInteraction.ts:457-464).
func TestInvokeChunkedResponse(t *testing.T) {
	t.Parallel()
	s := newScripted(t)
	c := interaction.New(s.mgr, s.session, nil)
	done := invokeAsync(c, armPath, gencomm.ArmFailSafeRequest{ExpiryLengthSeconds: 60, Breadcrumb: 1}, interaction.InvokeOptions{})
	req := s.next(t)
	if req.proto.Opcode != im.OpcodeInvokeRequest {
		t.Fatalf("opcode 0x%02X", req.proto.Opcode)
	}
	parsed, err := im.UnmarshalInvokeRequestTLV(tlv.NewDecoder(req.payload), nil)
	if err != nil || len(parsed.Invokes) != 1 || parsed.Invokes[0].HasCommandRef || parsed.TimedRequest {
		t.Fatalf("request %+v (%v)", parsed, err)
	}
	path := im.ConcreteCommandPath{Endpoint: 0, Cluster: gencomm.ClusterID, Command: gencomm.CmdArmFailSafeResponse, HasEndpoint: true, HasCluster: true, HasCommand: true}
	s.reply(t, req, im.InteractionModelProtocolID, im.OpcodeInvokeResponse, invokeResponse(t, true,
		im.InvokeResponseEntry{Path: path, HasResponse: true, Response: gencomm.ArmFailSafeResponse{ErrorCode: gencomm.CommissioningErrorBusyWithOtherAdmin, DebugText: "busy"}}))
	status := s.next(t)
	if status.proto.Opcode != im.OpcodeStatusResponse || decodeStatus(t, status.payload) != im.StatusSuccess {
		t.Fatalf("chunk acknowledgement opcode 0x%02X", status.proto.Opcode)
	}
	s.reply(t, status, im.InteractionModelProtocolID, im.OpcodeInvokeResponse, invokeResponse(t, false))
	r := await(t, done)
	if r.err != nil {
		t.Fatal(r.err)
	}
	var resp gencomm.ArmFailSafeResponse
	if !r.res.HasFields || r.res.Command != gencomm.CmdArmFailSafeResponse || resp.DecodeTLV(r.res.Fields) != nil ||
		resp.ErrorCode != gencomm.CommissioningErrorBusyWithOtherAdmin || resp.DebugText != "busy" {
		t.Fatalf("result %+v, fields %+v", r.res, resp)
	}
}

// TestInvokeStatuses: a CommandStatusIB Success is a result, another
// status a *StatusError with its cluster status, a StatusResponse in place
// of the InvokeResponse a *StatusError (InteractionMessenger.ts:201-214),
// a StatusResponse Success in its place an unexpected message, an empty
// response ErrNoResponse.
func TestInvokeStatuses(t *testing.T) {
	t.Parallel()
	cs := uint8(3)
	path := im.ConcreteCommandPath{Cluster: gencomm.ClusterID, Command: gencomm.CmdArmFailSafe, HasEndpoint: true, HasCluster: true, HasCommand: true}
	rows := []struct {
		name   string
		opcode uint8
		reply  func() []byte
		check  func(r invokeResult) bool
	}{
		{"status success", im.OpcodeInvokeResponse, func() []byte {
			return invokeResponse(t, false, im.InvokeResponseEntry{Path: path, IsStatus: true, Status: im.StatusIB{Status: im.StatusSuccess}})
		}, func(r invokeResult) bool { return r.err == nil && !r.res.HasFields && r.res.Status == im.StatusSuccess }},
		{"status failure", im.OpcodeInvokeResponse, func() []byte {
			return invokeResponse(t, false, im.InvokeResponseEntry{Path: path, IsStatus: true, Status: im.StatusIB{Status: im.StatusFailure, ClusterStatus: cs, HasClusterStatus: true}})
		}, func(r invokeResult) bool {
			var se *interaction.StatusError
			return errors.As(r.err, &se) && se.Status == im.StatusFailure && se.ClusterStatus != nil && *se.ClusterStatus == 3 && se.Error() != ""
		}},
		{
			"status response", im.OpcodeStatusResponse, func() []byte { return statusResponse(t, im.StatusUnsupportedAccess) },
			func(r invokeResult) bool {
				var se *interaction.StatusError
				return errors.As(r.err, &se) && se.Status == im.StatusUnsupportedAccess && se.ClusterStatus == nil
			},
		},
		{
			"status response success", im.OpcodeStatusResponse, func() []byte { return statusResponse(t, im.StatusSuccess) },
			func(r invokeResult) bool { return errors.Is(r.err, interaction.ErrUnexpectedMessage) },
		},
		{
			"empty response", im.OpcodeInvokeResponse, func() []byte { return invokeResponse(t, false) },
			func(r invokeResult) bool { return errors.Is(r.err, interaction.ErrNoResponse) },
		},
		{
			"undecodable response", im.OpcodeInvokeResponse, func() []byte { return []byte{0x15, 0x36} },
			func(r invokeResult) bool { return errors.Is(r.err, interaction.ErrUnexpectedMessage) },
		},
	}
	for _, row := range rows {
		s := newScripted(t)
		c := interaction.New(s.mgr, s.session, nil)
		done := invokeAsync(c, armPath, nil, interaction.InvokeOptions{})
		s.reply(t, s.next(t), im.InteractionModelProtocolID, row.opcode, row.reply())
		if r := await(t, done); !row.check(r) {
			t.Errorf("%s: %+v %v", row.name, r.res, r.err)
		}
	}
}

// TestTimedInvoke: a TimedRequest of the default 10 s first, its Success
// awaited, then the invoke marked timed (ClientInteraction.ts:438-440).
func TestTimedInvoke(t *testing.T) {
	t.Parallel()
	s := newScripted(t)
	c := interaction.New(s.mgr, s.session, nil)
	done := invokeAsync(c, armPath, nil, interaction.InvokeOptions{Timed: true})
	timed := s.next(t)
	if timed.proto.Opcode != im.OpcodeTimedRequest {
		t.Fatalf("opcode 0x%02X", timed.proto.Opcode)
	}
	tr, err := im.UnmarshalTimedRequestTLV(tlv.NewDecoder(timed.payload))
	if err != nil || tr.TimeoutMs != 10000 {
		t.Fatalf("TimedRequest %+v (%v)", tr, err)
	}
	s.reply(t, timed, im.InteractionModelProtocolID, im.OpcodeStatusResponse, statusResponse(t, im.StatusSuccess))
	req := s.next(t)
	parsed, err := im.UnmarshalInvokeRequestTLV(tlv.NewDecoder(req.payload), nil)
	if err != nil || !parsed.TimedRequest {
		t.Fatalf("invoke %+v (%v)", parsed, err)
	}
	s.reply(t, req, im.InteractionModelProtocolID, im.OpcodeInvokeResponse, invokeResponse(t, false,
		im.InvokeResponseEntry{Path: armPath, IsStatus: true, Status: im.StatusIB{Status: im.StatusSuccess}}))
	if r := await(t, done); r.err != nil {
		t.Fatal(r.err)
	}
	// A refused TimedRequest ends the invoke.
	done = invokeAsync(c, armPath, nil, interaction.InvokeOptions{Timed: true, TimedTimeout: time.Second})
	s.reply(t, s.next(t), im.InteractionModelProtocolID, im.OpcodeStatusResponse, statusResponse(t, im.StatusTimeout))
	var se *interaction.StatusError
	if r := await(t, done); !errors.As(r.err, &se) || se.Status != im.StatusTimeout {
		t.Fatalf("refused TimedRequest: %v", r.err)
	}
}

// TestInvokeTimeouts: the wait for the response is MaxPeerResponseTime of
// the processing time — the option's, else 30 s with the extended failsafe
// timeout, else the 2 s default (ClientInteraction.ts:466-474) — and its
// expiry a *PeerMessageMissingError.
func TestInvokeTimeouts(t *testing.T) {
	t.Parallel()
	rows := []struct {
		name string
		opts interaction.InvokeOptions
		ept  time.Duration
	}{
		{"default", interaction.InvokeOptions{}, exchange.DefaultExpectedProcessingTime},
		{"extended failsafe", interaction.InvokeOptions{ExtendedFailSafeTimeout: true}, 30 * time.Second},
		{"explicit", interaction.InvokeOptions{ExpectedProcessingTime: 7 * time.Second, ExtendedFailSafeTimeout: true}, 7 * time.Second},
	}
	for _, row := range rows {
		s := newScripted(t)
		c := interaction.New(s.mgr, s.session, nil)
		want := exchange.MaxPeerResponseTime(nil, false, exchange.IntervalsOf(s.mgr.LocalParameters()), row.ept)
		if got := c.MaximumPeerResponseTime(row.ept, false); got != want {
			t.Errorf("%s: MaximumPeerResponseTime %v, want %v", row.name, got, want)
		}
		done := invokeAsync(c, armPath, nil, row.opts)
		s.ack(t, s.next(t))
		s.clock.waitTimer(t, want)
		s.clock.Advance(want)
		r := await(t, done)
		var missing *exchange.PeerMessageMissingError
		if !errors.As(r.err, &missing) || missing.Timeout != want {
			t.Errorf("%s: %v, want a PeerMessageMissingError after %v", row.name, r.err, want)
		}
	}
	s := newScripted(t)
	c := interaction.New(s.mgr, s.session, nil)
	if c.MaximumPeerResponseTime(time.Second, true) <= c.MaximumPeerResponseTime(time.Second, false) {
		t.Error("the sending time of the peer is not included")
	}
	if c.MaximumPeerResponseTime(0, false) != c.MaximumPeerResponseTime(exchange.DefaultExpectedProcessingTime, false) || c.Session() != s.session {
		t.Error("the default processing time differs")
	}
}

// reportChunk encodes a ReportData chunk with attribute data entries.
func reportChunk(t *testing.T, more, suppress bool, entries ...func(e *tlv.Encoder)) []byte {
	t.Helper()
	return encode(t, func(e *tlv.Encoder) {
		e.StartStruct(tlv.AnonymousTag())
		e.StartArray(tlv.ContextTag(1))
		for _, f := range entries {
			e.StartStruct(tlv.AnonymousTag())
			f(e)
			_ = e.EndContainer()
		}
		_ = e.EndContainer()
		if more {
			e.PutBool(tlv.ContextTag(3), true)
		}
		if suppress {
			e.PutBool(tlv.ContextTag(4), true)
		}
		_ = e.EndContainer()
	})
}

func data(path im.ConcreteAttributePath, value func(e *tlv.Encoder, tag tlv.Tag)) func(e *tlv.Encoder) {
	return func(e *tlv.Encoder) {
		e.StartStruct(tlv.ContextTag(1))
		e.PutUint(tlv.ContextTag(0), 7)
		path.MarshalTLV(e, tlv.ContextTag(1))
		value(e, tlv.ContextTag(2))
		_ = e.EndContainer()
	}
}

func attrStatus(path im.ConcreteAttributePath, status im.StatusCode) func(e *tlv.Encoder) {
	return func(e *tlv.Encoder) {
		e.StartStruct(tlv.ContextTag(0))
		path.MarshalTLV(e, tlv.ContextTag(0))
		im.StatusIB{Status: status, ClusterStatus: 2, HasClusterStatus: true}.MarshalTLV(e, tlv.ContextTag(1))
		_ = e.EndContainer()
	}
}

// TestReadTwoChunks: the ReadRequest's shape (TlvReadRequest: the paths,
// fabric-filtered, the revision), a report in two chunks with a list
// that ends the first and is continued by a null ListIndex in the second,
// the Success after the first chunk and after the last
// (InteractionMessenger.ts:938-972).
func TestReadTwoChunks(t *testing.T) {
	t.Parallel()
	s := newScripted(t)
	c := interaction.New(s.mgr, s.session, nil)
	parts := im.ConcreteAttributePath{Endpoint: 0, Cluster: 0x001D, Attribute: 0x0003, HasEndpoint: true, HasCluster: true, HasAttribute: true}
	vendor := im.ConcreteAttributePath{Endpoint: 0, Cluster: 0x0028, Attribute: 0x0002, HasEndpoint: true, HasCluster: true, HasAttribute: true}
	missing := im.ConcreteAttributePath{Endpoint: 0, Cluster: 0x0031, Attribute: 0xFFFC, HasEndpoint: true, HasCluster: true, HasAttribute: true}
	type readResult struct {
		r   *interaction.Report
		err error
	}
	done := make(chan readResult, 1)
	go func() {
		r, err := c.Read(context.Background(), parts, vendor, missing)
		done <- readResult{r, err}
	}()
	req := s.next(t)
	rr, err := im.UnmarshalReadRequestTLV(tlv.NewDecoder(req.payload))
	if err != nil || len(rr.AttributeRequests) != 3 || !rr.FabricFiltered {
		t.Fatalf("ReadRequest %+v (%v)", rr, err)
	}
	appendPath := parts
	appendPath.ListAppend = true
	// The list ends the first chunk and continues in the second
	// (InputChunk.ts:233-241).
	s.reply(t, req, im.InteractionModelProtocolID, im.OpcodeReportData, reportChunk(t, true, false,
		data(vendor, func(e *tlv.Encoder, tag tlv.Tag) { e.PutUint(tag, 0xFFF1) }),
		data(parts, func(e *tlv.Encoder, tag tlv.Tag) {
			e.StartArray(tag)
			e.PutUint(tlv.AnonymousTag(), 1)
			_ = e.EndContainer()
		}),
	))
	ack := s.next(t)
	if ack.proto.Opcode != im.OpcodeStatusResponse || decodeStatus(t, ack.payload) != im.StatusSuccess {
		t.Fatalf("chunk acknowledgement opcode 0x%02X", ack.proto.Opcode)
	}
	s.reply(t, ack, im.InteractionModelProtocolID, im.OpcodeReportData, reportChunk(t, false, false,
		data(appendPath, func(e *tlv.Encoder, tag tlv.Tag) { e.PutUint(tag, 2) }),
		attrStatus(missing, im.StatusUnsupportedCluster),
	))
	final := s.next(t)
	if final.proto.Opcode != im.OpcodeStatusResponse {
		t.Fatalf("final status opcode 0x%02X", final.proto.Opcode)
	}
	s.ack(t, final)
	r := await(t, done)
	if r.err != nil {
		t.Fatal(r.err)
	}
	list, ok := r.r.Value(0, 0x001D, 0x0003)
	if !ok || len(list.Children) != 2 || list.Children[1].El.Uint != 2 {
		t.Fatalf("PartsList %+v", list)
	}
	if v, ok := r.r.Value(0, 0x0028, 0x0002); !ok || v.El.Uint != 0xFFF1 || r.r.Attributes[0].DataVersion != 7 {
		t.Fatalf("VendorID %+v", v)
	}
	if _, ok := r.r.Value(1, 0x0028, 0x0002); ok {
		t.Fatal("a value for another endpoint")
	}
	if r.r.SubscriptionID != 0 || r.r.Session != s.session {
		t.Fatalf("a read's report on subscription %d over session %p, want 0 over %p", r.r.SubscriptionID, r.r.Session, s.session)
	}
	if len(r.r.Statuses) != 1 || r.r.Statuses[0].Status != im.StatusUnsupportedCluster || *r.r.Statuses[0].ClusterStatus != 2 {
		t.Fatalf("statuses %+v", r.r.Statuses)
	}
}

// TestReadSuppressedAndRefused: a final chunk that suppresses the response
// gets no StatusResponse; a StatusResponse in place of the report is a
// *StatusError.
func TestReadSuppressedAndRefused(t *testing.T) {
	t.Parallel()
	s := newScripted(t)
	c := interaction.New(s.mgr, s.session, nil)
	done := make(chan error, 1)
	go func() {
		_, err := c.Read(context.Background())
		done <- err
	}()
	req := s.next(t)
	if rr, err := im.UnmarshalReadRequestTLV(tlv.NewDecoder(req.payload)); err != nil || len(rr.AttributeRequests) != 0 {
		t.Fatalf("empty ReadRequest %+v (%v)", rr, err)
	}
	s.reply(t, req, im.InteractionModelProtocolID, im.OpcodeReportData, reportChunk(t, false, true))
	if err := await(t, done); err != nil {
		t.Fatal(err)
	}
	go func() {
		_, err := c.Read(context.Background())
		done <- err
	}()
	s.reply(t, s.next(t), im.InteractionModelProtocolID, im.OpcodeStatusResponse, statusResponse(t, im.StatusInvalidAction))
	var se *interaction.StatusError
	if err := await(t, done); !errors.As(err, &se) || se.Status != im.StatusInvalidAction {
		t.Fatalf("refused read: %v", err)
	}
	go func() {
		_, err := c.Read(context.Background())
		done <- err
	}()
	s.reply(t, s.next(t), 0x0001, 0x40, nil)
	if err := await(t, done); !errors.Is(err, interaction.ErrUnexpectedMessage) {
		t.Fatalf("foreign protocol: %v", err)
	}
}

// TestReadEvents: ReadWith sends the event paths, the event filters and the
// DataVersionFilters, unfiltered when asked (Read.ts:40, :171-176), and
// decodes the event reports (InputChunk.ts:356-432).
func TestReadEvents(t *testing.T) {
	t.Parallel()
	s := newScripted(t)
	c := interaction.New(s.mgr, s.session, nil)
	ev := im.ConcreteEventPath{Endpoint: 0, Cluster: 0x0028, Event: 0, HasEndpoint: true, HasCluster: true, HasEvent: true}
	type readResult struct {
		r   *interaction.Report
		err error
	}
	done := make(chan readResult, 1)
	go func() {
		r, err := c.ReadWith(context.Background(), interaction.ReadRequest{
			Events:             []im.ConcreteEventPath{ev},
			EventFilters:       []im.EventMinimumNumber{{EventMin: 3}},
			DataVersionFilters: []im.DataVersionFilter{{Endpoint: 0, Cluster: 0x28, DataVersion: 4}},
			NoFabricFilter:     true,
		})
		done <- readResult{r, err}
	}()
	req := s.next(t)
	rr, err := im.UnmarshalReadRequestTLV(tlv.NewDecoder(req.payload))
	if err != nil || rr.FabricFiltered || len(rr.AttributeRequests) != 0 || len(rr.EventRequests) != 1 || len(rr.EventFilters) != 1 ||
		rr.EventFilters[0].EventMin != 3 || len(rr.DataVersionFilters) != 1 {
		t.Fatalf("ReadRequest %+v (%v)", rr, err)
	}
	s.reply(t, req, im.InteractionModelProtocolID, im.OpcodeReportData, encode(t, func(e *tlv.Encoder) {
		im.ReportData{EventReports: []im.EventReport{{Path: ev, Number: 7, Priority: 2, Timestamp: 1234, Data: im.AttributeValue{Value: uint64(1)}}}, SuppressResponse: true}.
			MarshalTLV(e, func(e *tlv.Encoder, tag tlv.Tag, v im.AttributeValue) {
				e.StartStruct(tag)
				e.PutUint(tlv.ContextTag(0), v.Value.(uint64))
				_ = e.EndContainer()
			})
	}))
	r := await(t, done)
	if r.err != nil || len(r.r.Events) != 1 {
		t.Fatalf("report %+v, %v", r.r, r.err)
	}
	if e := r.r.Events[0]; e.Number != 7 || e.Priority != 2 || e.Timestamp != 1234 || e.EpochTimestamp == nil || !e.HasValue || len(e.Value.Children) != 1 {
		t.Fatalf("event %+v", e)
	}
}

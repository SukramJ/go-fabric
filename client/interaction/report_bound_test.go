// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package interaction_test

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"testing/synctest"

	"thingfabric.dev/go-fabric/client/interaction"
	gencomm "thingfabric.dev/go-fabric/cluster/spec/generalcommissioning"
	"thingfabric.dev/go-fabric/im"
	"thingfabric.dev/go-fabric/tlv"
)

// TestReportAssemblyIsBounded: a peer that keeps MoreChunkedMessages set
// gets its read answered with ResourceExhausted once the chunks the
// session's interactions hold pass the bound, and the read ends with
// ErrReportTooLarge (review II, R2-P2-alloc-05).
func TestReportAssemblyIsBounded(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		s := newScripted(t)
		c := interaction.New(s.mgr, s.session, nil)
		type readResult struct {
			r   *interaction.Report
			err error
		}
		done := make(chan readResult, 1)
		path := im.ConcreteAttributePath{Endpoint: 1, Cluster: 0x0050, Attribute: 0}
		go func() {
			r, err := c.Read(context.Background(), path)
			done <- readResult{r, err}
		}()
		value := bytes.Repeat([]byte{0xA5}, 1000)
		chunk := encode(t, func(e *tlv.Encoder) {
			im.ReportData{MoreChunkedMessages: true, Reports: []im.AttributeReport{{Path: path, Value: im.AttributeValue{Value: value}}}}.
				MarshalTLV(e, func(e *tlv.Encoder, tag tlv.Tag, v im.AttributeValue) { e.PutOctets(tag, v.Value.([]byte)) })
		})
		to := s.next(t)
		sent, charge := 0, interaction.ReportChunkCharge(chunk)
		for limit := interaction.MaxAssemblyBytesPerSession/charge + 2; ; limit-- {
			if limit == 0 {
				t.Fatalf("the client took %d charged bytes of chunks, the bound is %d", sent, interaction.MaxAssemblyBytesPerSession)
			}
			s.reply(t, to, im.InteractionModelProtocolID, im.OpcodeReportData, chunk)
			sent += charge
			to = s.next(t)
			if to.proto.Opcode != im.OpcodeStatusResponse {
				t.Fatalf("opcode 0x%02X", to.proto.Opcode)
			}
			if st := decodeStatus(t, to.payload); st != im.StatusSuccess {
				if st != im.StatusResourceExhausted || sent <= interaction.MaxAssemblyBytesPerSession {
					t.Fatalf("status %v after %d bytes", st, sent)
				}
				break
			}
		}
		s.ack(t, to)
		if r := await(t, done); !errors.Is(r.err, interaction.ErrReportTooLarge) {
			t.Fatalf("read returned %v, %v", r.r, r.err)
		}
	})
}

// TestInvokeResponseAssemblyIsBounded: the same bound holds for the
// chunks of an InvokeResponse (review II, R2-P2-alloc-05).
func TestInvokeResponseAssemblyIsBounded(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		s := newScripted(t)
		c := interaction.New(s.mgr, s.session, nil)
		done := invokeAsync(c, armPath, gencomm.ArmFailSafeRequest{ExpiryLengthSeconds: 60, Breadcrumb: 1}, interaction.InvokeOptions{})
		entries := make([]im.InvokeResponseEntry, 60)
		for i := range entries {
			entries[i] = im.InvokeResponseEntry{Path: armPath, IsStatus: true, Status: im.StatusIB{Status: im.StatusSuccess}}
		}
		chunk := invokeResponse(t, true, entries...)
		to := s.next(t)
		sent, charge := 0, interaction.InvokeChunkCharge(chunk)
		for limit := interaction.MaxAssemblyBytesPerSession/charge + 2; ; limit-- {
			if limit == 0 {
				t.Fatalf("the client took %d charged bytes of chunks, the bound is %d", sent, interaction.MaxAssemblyBytesPerSession)
			}
			s.reply(t, to, im.InteractionModelProtocolID, im.OpcodeInvokeResponse, chunk)
			sent += charge
			to = s.next(t)
			if st := decodeStatus(t, to.payload); st != im.StatusSuccess {
				if st != im.StatusResourceExhausted || sent <= interaction.MaxAssemblyBytesPerSession {
					t.Fatalf("status %v after %d bytes", st, sent)
				}
				break
			}
		}
		s.ack(t, to)
		if r := await(t, done); !errors.Is(r.err, interaction.ErrReportTooLarge) {
			t.Fatalf("invoke returned %+v, %v", r.res, r.err)
		}
	})
}

// nullsArray encodes an array of n anonymous nulls, one byte each: the
// value shape that holds the most heap a payload byte once decoded.
func nullsArray(e *tlv.Encoder, tag tlv.Tag, n int) {
	e.StartArray(tag)
	for range n {
		e.PutNull(tlv.AnonymousTag())
	}
	_ = e.EndContainer()
}

// TestReportBoundCountsDecodedElements: chunks whose values are made of
// one-byte elements are refused once their decoded elements pass the
// bound, long before their payload bytes would (review II,
// R2-P6-bound-2102).
func TestReportBoundCountsDecodedElements(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		s := newScripted(t)
		c := interaction.New(s.mgr, s.session, nil)
		done := make(chan error, 1)
		path := im.ConcreteAttributePath{Endpoint: 1, Cluster: 0x0050, Attribute: 0}
		go func() {
			_, err := c.Read(context.Background(), path)
			done <- err
		}()
		const elements = 1000
		chunk := encode(t, func(e *tlv.Encoder) {
			im.ReportData{MoreChunkedMessages: true, Reports: []im.AttributeReport{{Path: path, Value: im.AttributeValue{Value: struct{}{}}}}}.
				MarshalTLV(e, func(e *tlv.Encoder, tag tlv.Tag, _ im.AttributeValue) { nullsArray(e, tag, elements) })
		})
		// Every chunk holds more than elements × 100 heap bytes once decoded.
		admitted := interaction.MaxAssemblyBytesPerSession / (elements * 100)
		to := s.next(t)
		for n := 1; ; n++ {
			if n > admitted+1 {
				t.Fatalf("the client took %d chunks of %d one-byte elements; the bound is %d bytes", n-1, elements, interaction.MaxAssemblyBytesPerSession)
			}
			s.reply(t, to, im.InteractionModelProtocolID, im.OpcodeReportData, chunk)
			to = s.next(t)
			if st := decodeStatus(t, to.payload); st != im.StatusSuccess {
				if st != im.StatusResourceExhausted {
					t.Fatalf("status %v after %d chunks", st, n)
				}
				break
			}
		}
		s.ack(t, to)
		if err := await(t, done); !errors.Is(err, interaction.ErrReportTooLarge) {
			t.Fatalf("read returned %v, want ErrReportTooLarge", err)
		}
	})
}

// TestInvokeBoundCountsDecodedElements: the same for the fields of a
// chunked InvokeResponse (review II, R2-P6-bound-2150).
func TestInvokeBoundCountsDecodedElements(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		s := newScripted(t)
		c := interaction.New(s.mgr, s.session, nil)
		done := invokeAsync(c, armPath, gencomm.ArmFailSafeRequest{ExpiryLengthSeconds: 60, Breadcrumb: 1}, interaction.InvokeOptions{})
		const elements = 1000
		respPath := im.ConcreteCommandPath{Endpoint: armPath.Endpoint, Cluster: armPath.Cluster, Command: 1}
		chunk := encode(t, func(e *tlv.Encoder) {
			im.InvokeResponse{MoreChunkedMessages: true, Responses: []im.InvokeResponseEntry{{Path: respPath, Response: struct{}{}, HasResponse: true}}}.
				MarshalTLV(e, func(e *tlv.Encoder, tag tlv.Tag, _ any) {
					e.StartStruct(tag)
					nullsArray(e, tlv.ContextTag(0), elements)
					_ = e.EndContainer()
				})
		})
		admitted := interaction.MaxAssemblyBytesPerSession / (elements * 100)
		to := s.next(t)
		for n := 1; ; n++ {
			if n > admitted+1 {
				t.Fatalf("the client took %d chunks of %d one-byte elements; the bound is %d bytes", n-1, elements, interaction.MaxAssemblyBytesPerSession)
			}
			s.reply(t, to, im.InteractionModelProtocolID, im.OpcodeInvokeResponse, chunk)
			to = s.next(t)
			if st := decodeStatus(t, to.payload); st != im.StatusSuccess {
				if st != im.StatusResourceExhausted {
					t.Fatalf("status %v after %d chunks", st, n)
				}
				break
			}
		}
		s.ack(t, to)
		if r := await(t, done); !errors.Is(r.err, interaction.ErrReportTooLarge) {
			t.Fatalf("invoke returned %+v, %v", r.res, r.err)
		}
	})
}

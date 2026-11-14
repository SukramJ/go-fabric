// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package interaction_test

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"testing/synctest"

	"github.com/SukramJ/go-fabric/client/interaction"
	gencomm "github.com/SukramJ/go-fabric/cluster/spec/generalcommissioning"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
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
		sent := 0
		for limit := interaction.MaxAssemblyBytesPerSession/len(chunk) + 2; ; limit-- {
			if limit == 0 {
				t.Fatalf("the client took %d bytes of chunks, the bound is %d", sent, interaction.MaxAssemblyBytesPerSession)
			}
			s.reply(t, to, im.InteractionModelProtocolID, im.OpcodeReportData, chunk)
			sent += len(chunk)
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
		sent := 0
		for limit := interaction.MaxAssemblyBytesPerSession/len(chunk) + 2; ; limit-- {
			if limit == 0 {
				t.Fatalf("the client took %d bytes of chunks, the bound is %d", sent, interaction.MaxAssemblyBytesPerSession)
			}
			s.reply(t, to, im.InteractionModelProtocolID, im.OpcodeInvokeResponse, chunk)
			sent += len(chunk)
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

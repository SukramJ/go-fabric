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
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
)

// readPastBound runs a read on c whose peer keeps MoreChunkedMessages set
// with chunks of 1000-byte values, and returns the bytes it had sent when
// the client answered ResourceExhausted, and the chunk's size.
func readPastBound(t *testing.T, s *scripted, c *interaction.Interaction) (sent, chunkSize int) {
	t.Helper()
	done := make(chan error, 1)
	path := im.ConcreteAttributePath{Endpoint: 1, Cluster: 0x0050, Attribute: 0}
	go func() {
		_, err := c.Read(context.Background(), path)
		done <- err
	}()
	value := bytes.Repeat([]byte{0xA5}, 1000)
	chunk := encode(t, func(e *tlv.Encoder) {
		im.ReportData{MoreChunkedMessages: true, Reports: []im.AttributeReport{{Path: path, Value: im.AttributeValue{Value: value}}}}.
			MarshalTLV(e, func(e *tlv.Encoder, tag tlv.Tag, v im.AttributeValue) { e.PutOctets(tag, v.Value.([]byte)) })
	})
	to := s.next(t)
	for limit := interaction.MaxAssemblyBytesPerSession/len(chunk) + 2; ; limit-- {
		if limit == 0 {
			t.Fatalf("the client took %d bytes of chunks without ResourceExhausted", sent)
		}
		s.reply(t, to, im.InteractionModelProtocolID, im.OpcodeReportData, chunk)
		sent += len(chunk)
		to = s.next(t)
		if st := decodeStatus(t, to.payload); st != im.StatusSuccess {
			if st != im.StatusResourceExhausted {
				t.Fatalf("status %v after %d bytes", st, sent)
			}
			break
		}
	}
	s.ack(t, to)
	if err := await(t, done); !errors.Is(err, interaction.ErrReportTooLarge) {
		t.Fatalf("read returned %v, want ErrReportTooLarge", err)
	}
	return sent, len(chunk)
}

// TestReportBoundFromConfig: Config.MaxReportBytesPerSession replaces the
// default bound of an interaction's report chunks (owner decision 48).
func TestReportBoundFromConfig(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		s := newScripted(t)
		const bound = 4096
		c := interaction.Bind(interaction.SessionProvider(s.mgr, s.session), interaction.Config{MaxReportBytesPerSession: bound})
		sent, size := readPastBound(t, s, c)
		if sent <= bound || sent > bound+size {
			t.Fatalf("ResourceExhausted after %d bytes, want the chunk that passes the configured bound %d", sent, bound)
		}
	})
}

// TestReportBoundFromSubscriptions: an interaction without a bound of its
// own takes its subscription registry's
// (SubscriptionsConfig.MaxReportBytesPerSession), which is how the
// controller hands its configured bound to its nodes' interactions.
func TestReportBoundFromSubscriptions(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		s := newScripted(t)
		const bound = 4096
		reg := interaction.NewSubscriptions(interaction.SubscriptionsConfig{Clock: s.clock, MaxReportBytesPerSession: bound})
		c := interaction.Bind(interaction.SessionProvider(s.mgr, s.session), interaction.Config{Subscriptions: reg})
		sent, size := readPastBound(t, s, c)
		if sent <= bound || sent > bound+size {
			t.Fatalf("ResourceExhausted after %d bytes, want the chunk that passes the registry's bound %d", sent, bound)
		}
	})
}

// TestPushedReportBoundFromSubscriptions: a report the peer pushes on a
// subscription is held to the registry's bound: the chunk that passes it
// is answered ResourceExhausted. (The first chunk, which opens the
// exchange, is read before the bound counts.)
func TestPushedReportBoundFromSubscriptions(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		s := newScripted(t)
		chunk := pushedReport(t, 5, true, true, false, true)
		reg := interaction.NewSubscriptions(interaction.SubscriptionsConfig{Clock: s.clock, MaxReportBytesPerSession: len(chunk)})
		reg.Register(s.mgr)
		t.Cleanup(func() { s.mgr.SetProtocolHandler(im.InteractionModelProtocolID, nil) })
		// The interaction's own bound lets the priming report through.
		c := interaction.Bind(interaction.SessionProvider(s.mgr, s.session), interaction.Config{Subscriptions: reg, MaxReportBytesPerSession: interaction.MaxAssemblyBytesPerSession})
		subscribed(t, s, c, interaction.SubscribeRequest{
			Attributes: []im.ConcreteAttributePath{onOff},
			Updated:    func(*interaction.Report) error { return nil },
		}, 5)
		s.push(t, 0x7001, im.OpcodeReportData, chunk)
		to := expectStatus(t, s, im.StatusSuccess)
		s.continueOn(t, to, im.OpcodeReportData, chunk) // holds the bound exactly
		to = expectStatus(t, s, im.StatusSuccess)
		s.continueOn(t, to, im.OpcodeReportData, chunk)
		if m := s.next(t); m.proto.Opcode != im.OpcodeStatusResponse || decodeStatus(t, m.payload) != im.StatusResourceExhausted {
			t.Fatalf("the chunk past the bound answered opcode 0x%02X status %v, want ResourceExhausted", m.proto.Opcode, decodeStatus(t, m.payload))
		}
	})
}

// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package interaction_test

import (
	"context"
	"errors"
	"net"
	"testing"
	"testing/synctest"
	"time"

	"github.com/SukramJ/go-fabric/client/interaction"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
)

// probeAsync runs c.Probe in its own goroutine.
func probeAsync(ctx context.Context, c *interaction.Interaction, opts interaction.ProbeOptions) <-chan bool {
	done := make(chan bool, 1)
	go func() { done <- c.Probe(ctx, opts) }()
	return done
}

// TestProbe holds Interaction.Probe to matter.js ClientInteraction.probe
// (packages/protocol/src/action/client/ClientInteraction.ts:352-412 at
// 862ad75f): an empty, unfiltered read on the existing session, to the
// override address when one is set, true once the reports are drained and
// false on silence, a closed interaction or an ended ctx — the session's
// own address and liveness untouched.
func TestProbe(t *testing.T) {
	t.Parallel()
	alternate := &net.UDPAddr{IP: net.ParseIP("fd00::2"), Port: 5540}
	t.Run("sends an empty unfiltered read to the override and drains the reports", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			s := newScripted(t)
			c := interaction.New(s.mgr, s.session, nil)
			done := probeAsync(context.Background(), c, interaction.ProbeOptions{AddressOverride: alternate, SuppressPeerLoss: true})
			req := s.next(t)
			rr, err := im.UnmarshalReadRequestTLV(tlv.NewDecoder(req.payload))
			if err != nil || req.proto.Opcode != im.OpcodeReadRequest || len(rr.AttributeRequests) != 0 || len(rr.EventRequests) != 0 || rr.FabricFiltered {
				t.Fatalf("probe request opcode 0x%02X %+v (%v), want an empty ReadRequest without fabric filter (:383)", req.proto.Opcode, rr, err)
			}
			if d := s.conn.destinations(); len(d) != 1 || !d[0].IP.Equal(alternate.IP) {
				t.Fatalf("probe sent to %v, want the override %v (:372)", d, alternate)
			}
			// A chunk with more to come is acknowledged with a Success and
			// the next one read (:388, InteractionMessenger.readDataReports).
			s.reply(t, req, im.InteractionModelProtocolID, im.OpcodeReportData, reportChunk(t, true, false))
			st := s.next(t)
			if st.proto.Opcode != im.OpcodeStatusResponse || decodeStatus(t, st.payload) != im.StatusSuccess {
				t.Fatalf("chunk answer opcode 0x%02X", st.proto.Opcode)
			}
			s.reply(t, st, im.InteractionModelProtocolID, im.OpcodeReportData, reportChunk(t, false, true))
			if !await(t, done) {
				t.Fatal("probe false after the final report")
			}
			if !s.session.Addr().IP.Equal(peerAddr.IP) {
				t.Fatalf("session address %v after the probe, want %v unchanged", s.session.Addr(), peerAddr)
			}
		})
	})
	t.Run("is false after its capped transmissions go unanswered and leaves the session alive", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			s := newScripted(t)
			c := interaction.New(s.mgr, s.session, nil)
			done := probeAsync(context.Background(), c, interaction.ProbeOptions{AddressOverride: alternate, SuppressPeerLoss: true, MaxRetransmissions: 2})
			var result bool
			for finished := false; !finished; {
				synctest.Wait()
				select {
				case result = <-done:
					finished = true
				default:
					s.clock.Advance(time.Second)
				}
			}
			if result {
				t.Fatal("probe true without an answer")
			}
			if n := len(s.conn.destinations()); n != 2 {
				t.Fatalf("%d transmissions, want maxRetransmissions 2 (:386)", n)
			}
			for _, d := range s.conn.destinations() {
				if !d.IP.Equal(alternate.IP) {
					t.Fatalf("a transmission went to %v, want the override %v", d, alternate)
				}
			}
			if s.session.IsClosed() || s.session.IsPeerLost() {
				t.Fatal("a failed probe with suppressPeerLoss closed the session or reported the peer lost (:385)")
			}
		})
	})
	t.Run("is false on a closed interaction and an ended ctx without sending", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			s := newScripted(t)
			c := interaction.New(s.mgr, s.session, nil)
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			if c.Probe(ctx, interaction.ProbeOptions{}) {
				t.Fatal("probe true on an ended ctx")
			}
			c.Close(errors.New("closed"))
			if c.Probe(context.Background(), interaction.ProbeOptions{}) {
				t.Fatal("probe true on a closed interaction (:361-363)")
			}
			synctest.Wait()
			if n := len(s.conn.destinations()); n != 0 {
				t.Fatalf("%d datagrams sent", n)
			}
		})
	})
}

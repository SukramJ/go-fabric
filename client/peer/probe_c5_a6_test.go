// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package peer

import (
	"context"
	"log/slog"
	"testing"
	"testing/synctest"

	"thingfabric.dev/go-fabric/im"
	"thingfabric.dev/go-fabric/transport/message"
)

// TestProbeEndsAPeerThatKeepsChunking: a peer answering the probe's empty
// read with report after report, each with more to come, holds the probe
// — and its network slot — for at most probeMaxReportChunks reports; the
// probe then fails and the slot is free again (review II,
// R2-C2-bound-2159).
func TestProbeEndsAPeerThatKeepsChunking(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		h := newHarness(t, nil)
		s, ch := h.securePair(t, 0x40, udpAddr(addr1))
		n := nodeSide{h: h, s: s, ch: ch}
		network := &NetworkProfile{ID: "probe", Semaphore: NewSemaphore(h.clock, "probe", 1, 0, 0)}
		res := make(chan bool, 1)
		go func() {
			res <- probe(context.Background(), h.mgr, s, nil, slog.New(slog.DiscardHandler), network, nil)
		}()
		hdr, proto, _ := n.read(t, h.udp.next(t))
		for i := range probeMaxReportChunks {
			n.send(t, message.ProtocolHeader{NeedsAck: true, HasAck: true, AckCounter: hdr.MessageCounter, Opcode: im.OpcodeReportData, ExchangeID: proto.ExchangeID, ProtocolID: im.InteractionModelProtocolID}, reportData(true, false))
			if i == probeMaxReportChunks-1 {
				break
			}
			var status message.ProtocolHeader
			hdr, status, _ = n.read(t, h.udp.next(t))
			if status.Opcode != im.OpcodeStatusResponse {
				t.Fatalf("after chunk %d: %+v", i+1, status)
			}
		}
		if waitBool(t, res) {
			t.Fatal("a probe answered only by chunks with more to come succeeded")
		}
		synctest.Wait()
		if got := network.Semaphore.Running(); got != 0 {
			t.Fatalf("%d network slots held after the probe ended", got)
		}
	})
}

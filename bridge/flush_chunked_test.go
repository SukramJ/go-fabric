// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"net"
	"runtime"
	"slices"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock/clocktest"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/mdns"
	"github.com/SukramJ/go-fabric/secure/channel"
	"github.com/SukramJ/go-fabric/tlv"
	"github.com/SukramJ/go-fabric/transport/message"
	"github.com/SukramJ/go-fabric/transport/udp"
)

// TestChunkedFlushAwaitsTheLastChunk: a flushed report over the chunk
// budget waits for the peer's answer to its last chunk — the one carrying
// the Leave event, as events follow the attributes — not only to the
// first. Answering chunk 0 leaves the flush waiting; the last chunk's
// answer, or 500 ms on the bridge's clock, ends it. A flush that fits one
// chunk waits for that chunk the same way. matter.js waits for the
// Success to every chunk sendDataReportMessage sends, 500 ms for a closed
// subscription's report (InteractionMessenger.ts:799-844, :844 at
// db021da0) (issue 473, review M1 of #474).
func TestChunkedFlushAwaitsTheLastChunk(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name              string
		chunked, answered bool
	}{
		{"chunked/answered", true, true},
		{"chunked/unanswered", true, false},
		{"single/answered", false, true},
		{"single/unanswered", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			const sessionID uint16 = 91
			fake := clocktest.New(time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC))
			b, err := New(wbEmptySnapshotter, mdns.NewNoop(), Config{Listen: "127.0.0.1:0", Clock: fake}, nil)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if err := b.Start(ctx); err != nil {
				t.Fatalf("Start: %v", err)
			}
			t.Cleanup(func() { _ = b.Stop(context.Background()) })
			peerConn, peerSess, peerAddr := chunkTestPeer(t, b, sessionID)

			report := im.ReportData{HasSubscription: true, SubscriptionID: 0x473}
			attributes := 1
			if tc.chunked {
				attributes = 200
			}
			for i := range attributes {
				report.Reports = append(report.Reports, im.AttributeReport{
					Path:        im.ConcreteAttributePath{Endpoint: 1, Cluster: 0x0028, Attribute: uint32(i)},
					DataVersion: 1,
					Value:       im.AttributeValue{Value: "a string long enough to need a second chunk"},
				})
			}
			report.EventReports = []im.EventReport{{
				Path:     im.ConcreteEventPath{Endpoint: 0, Cluster: 0x0028, Event: 0x02, HasEndpoint: true, HasCluster: true, HasEvent: true},
				Number:   7,
				Priority: im.EventPriorityInfo,
				Data:     im.AttributeValue{Value: uint8(1)},
			}}
			target := subTarget{src: peerAddr, exchangeID: 5, sessionID: sessionID, peerInitiator: true, flush: true}

			done := make(chan struct{})
			go func() {
				defer close(done)
				_, exch, wait, err := b.sendInitiatedReportArmed(target, report, true)
				if err != nil {
					t.Errorf("sendInitiatedReportArmed: %v", err)
					return
				}
				b.awaitFlushedReport(ctx, "subscribe.flush", sessionID, exch, wait)
			}()

			// Read to the last chunk, answering the ones that announce more.
			exch, more, events := readFlushChunk(t, peerConn, peerSess)
			if more != tc.chunked {
				t.Fatalf("chunk 0: more=%v, want %v", more, tc.chunked)
			}
			chunks := 1
			for more {
				if events != 0 {
					t.Fatalf("chunk %d announces more and carries %d events, want the Leave last", chunks-1, events)
				}
				b.signalStatusResponseRX(sessionID, exch, true, im.StatusSuccess)
				exch, more, events = readFlushChunk(t, peerConn, peerSess)
				chunks++
			}
			if tc.chunked && chunks < 2 {
				t.Fatalf("the report went out in %d chunk, want more than one", chunks)
			}
			if events != 1 {
				t.Fatalf("the last chunk carries %d events, want the Leave", events)
			}
			// The flush now waits for the last chunk's answer on its 500 ms
			// timer.
			for !slices.Contains(fake.Armed(), flushStatusResponseTimeout) {
				select {
				case <-done:
					t.Fatal("the flush returned before the last chunk's answer")
				case <-ctx.Done():
					t.Fatal("the flush never waited for the last chunk")
				default:
					runtime.Gosched()
				}
			}
			fake.Advance(flushStatusResponseTimeout - time.Millisecond)
			select {
			case <-done:
				t.Fatal("the flush returned before the last chunk's answer and before its bound")
			default:
			}
			if tc.answered {
				if !b.signalStatusResponseRX(sessionID, exch, true, im.StatusSuccess) {
					t.Fatal("the last chunk's answer found no waiter")
				}
			} else {
				fake.Advance(time.Millisecond)
			}
			select {
			case <-done:
			case <-ctx.Done():
				t.Fatal("the flush did not end")
			}
			if b.signalStatusResponseRX(sessionID, exch, true, im.StatusSuccess) {
				t.Fatal("the flush's waiter stayed armed after it ended")
			}
		})
	}
}

// readFlushChunk reads one ReportData chunk the bridge sent to peerConn
// and returns its exchange, its MoreChunkedMessages flag and its number
// of event reports.
func readFlushChunk(t *testing.T, peerConn *net.UDPConn, peerSess *channel.Session) (exchangeID uint16, more bool, events int) {
	t.Helper()
	buf := make([]byte, udp.MaxDatagramSize)
	for {
		if err := peerConn.SetReadDeadline(time.Now().Add(10 * time.Second)); err != nil {
			t.Fatalf("SetReadDeadline: %v", err)
		}
		n, from, err := peerConn.ReadFromUDP(buf)
		if err != nil {
			t.Fatalf("no chunk: %v", err)
		}
		if port, ok := chunkPeerBridgePort.Load(peerConn); ok && from.Port != port.(int) {
			continue // another test's traffic
		}
		hdr, hdrLen, err := message.UnmarshalHeader(buf[:n])
		if err != nil {
			t.Fatalf("UnmarshalHeader: %v", err)
		}
		plain, _, err := peerSess.Decrypt(&hdr, securityFlagsByte(&hdr), buf[hdrLen:n])
		if err != nil {
			t.Fatalf("Decrypt: %v", err)
		}
		proto, protoLen, err := message.UnmarshalProtocolHeader(plain)
		if err != nil {
			t.Fatalf("UnmarshalProtocolHeader: %v", err)
		}
		if proto.Opcode != im.OpcodeReportData {
			continue // a standalone ack
		}
		rd, err := im.UnmarshalReportDataTLV(tlv.NewDecoder(plain[protoLen:]), nil, nil)
		if err != nil {
			t.Fatalf("UnmarshalReportDataTLV: %v", err)
		}
		return proto.ExchangeID, rd.MoreChunkedMessages, len(rd.EventReports)
	}
}

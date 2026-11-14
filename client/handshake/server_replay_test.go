// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package handshake_test

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/client/handshake"
	"github.com/SukramJ/go-fabric/transport/message"
	"github.com/SukramJ/go-fabric/transport/mrp"
)

// captureSigma1 is the first datagram a node's CASE initiator sends to an
// address that answers nothing — a Sigma1 the controller never saw.
func captureSigma1(t *testing.T, r *responderRig) []byte {
	t.Helper()
	sink, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer sink.Close()
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _, _ = handshake.CASE(ctx, r.nodeMgr, sink.LocalAddr().(*net.UDPAddr), r.nodeFabric, controllerNodeID, handshake.CASEOptions{})
	}()
	defer func() { cancel(); <-done }()
	_ = sink.SetReadDeadline(time.Now().Add(10 * time.Second))
	buf := make([]byte, 2048)
	n, _, err := sink.ReadFromUDP(buf)
	if err != nil {
		t.Fatalf("no Sigma1 captured: %v", err)
	}
	return append([]byte(nil), buf[:n]...)
}

// replay sends sigma1 count times from conn to the controller, each under
// a fresh source node id and message counter.
func replay(t *testing.T, conn *net.UDPConn, to *net.UDPAddr, sigma1 []byte, count, base uint64) {
	t.Helper()
	hdr, hl, err := message.UnmarshalHeader(sigma1)
	if err != nil {
		t.Fatal(err)
	}
	for i := range count {
		h := hdr
		h.SourceNodeID = base + i
		h.MessageCounter = uint32(base + i) //nolint:gosec // a test counter
		if _, err := conn.WriteToUDP(append(h.Marshal(), sigma1[hl:]...), to); err != nil {
			t.Fatal(err)
		}
	}
}

// sigma2Answers counts the distinct Sigma2 messages conn receives until
// want arrived or none came for quiet.
func sigma2Answers(conn *net.UDPConn, want int, quiet time.Duration) int {
	seen := map[uint32]bool{}
	buf := make([]byte, 2048)
	for len(seen) < want {
		_ = conn.SetReadDeadline(time.Now().Add(quiet))
		n, _, err := conn.ReadFromUDP(buf)
		if err != nil {
			break
		}
		if op, _, ok := unsecuredMessage(buf[:n]); ok && op == mrp.SCOpcodeSigma2 {
			h, _, _ := message.UnmarshalHeader(buf[:n])
			seen[h.MessageCounter] = true
		}
	}
	return len(seen)
}

// TestCASEServerAnswersASigma1Once: the responder runs one handshake per
// Sigma1 — the initiator random a destination id was computed with — so
// one Sigma1 sent again under fresh source node ids is answered once, the
// repeats with nothing.
func TestCASEServerAnswersASigma1Once(t *testing.T) {
	t.Parallel()
	r := newResponderRig(t)
	sigma1 := captureSigma1(t, r)
	if op, _, ok := unsecuredMessage(sigma1); !ok || op != mrp.SCOpcodeSigma1 {
		t.Fatalf("captured opcode %#x, want Sigma1", op)
	}
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	const repeats = 300
	replay(t, conn, r.serverAddr, sigma1, repeats, 0x1000)
	if got := sigma2Answers(conn, repeats, 2*time.Second); got != 1 {
		t.Fatalf("one Sigma1 sent under %d source node ids was answered with %d Sigma2s; want 1", repeats, got)
	}
}

// TestCASEServerCompletesAHandshakeWhileASigma1IsRepeated: a node's CASE
// to the controller completes while one Sigma1 is sent again under 256
// fresh source node ids between the node's Sigma2 and its Sigma3 — the
// repeats hold no session that could take the node's place in the table.
func TestCASEServerCompletesAHandshakeWhileASigma1IsRepeated(t *testing.T) {
	t.Parallel()
	r := newResponderRig(t)
	captured := captureSigma1(t, r)
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	relay, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer relay.Close()
	go func() {
		var node *net.UDPAddr
		repeated := false
		buf := make([]byte, 4096)
		for {
			n, from, err := relay.ReadFromUDP(buf)
			if err != nil {
				return
			}
			d := append([]byte(nil), buf[:n]...)
			if from.Port == r.serverAddr.Port {
				if node != nil {
					_, _ = relay.WriteToUDP(d, node)
				}
				continue
			}
			node = from
			if op, _, ok := unsecuredMessage(d); ok && op == mrp.SCOpcodeSigma3 && !repeated {
				repeated = true
				replay(t, conn, r.serverAddr, captured, 256, 0x9000)
				sigma2Answers(conn, 256, time.Second)
			}
			_, _ = relay.WriteToUDP(d, r.serverAddr)
		}
	}()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	if _, _, err := handshake.CASE(ctx, r.nodeMgr, relay.LocalAddr().(*net.UDPAddr), r.nodeFabric, controllerNodeID, handshake.CASEOptions{}); err != nil {
		t.Fatalf("the node's CASE to the controller failed while a Sigma1 was repeated: %v", err)
	}
}

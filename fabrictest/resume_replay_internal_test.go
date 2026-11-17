// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package fabrictest

import (
	"context"
	"encoding/binary"
	"log/slog"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/client/handshake"
	"github.com/SukramJ/go-fabric/client/peer"
	"github.com/SukramJ/go-fabric/clock"
	"github.com/SukramJ/go-fabric/device"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/node"
	"github.com/SukramJ/go-fabric/transport/message"
	"github.com/SukramJ/go-fabric/transport/mrp"
)

// TestR2P6Sec3320Replay: review II, R2-P6-sec-3320 — a Sigma1 replayed
// after its resume was accepted takes the full path. Node a resumes with
// node b after a restart; the genuine resuming Sigma1 is held back from b
// and sent to it from another address instead, then sent again on another
// exchange with another initiator session id (a field the resume MIC does
// not cover): the first is answered with Sigma2_Resume, the second with
// Sigma2.
func TestR2P6Sec3320Replay(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	site := NewSite(t, Options{Clock: clock.System{}, Logger: slog.New(slog.DiscardHandler)})
	light, err := device.NewOnOffLight(device.Options{StableKey: endpoint.StringKey("b:1"), Name: "b"})
	if err != nil {
		t.Fatal(err)
	}
	pair := site.CommissionedPair(PairOptions{Device: Topology{Bridged: []node.Endpoint{light}}})
	b := pair.Device
	a := site.AddDevice(Topology{Configure: func(cfg *node.Config) { cfg.Client = &node.ClientConfig{} }})
	if _, err := pair.Controller.Commission(ctx, a, CommissionOptions{}); err != nil {
		t.Fatal(err)
	}
	af, err := a.Node().FabricInfos(ctx)
	if err != nil || len(af) != 1 {
		t.Fatalf("a's fabrics %v, %v", af, err)
	}
	bf, err := b.Node().FabricInfos(ctx)
	if err != nil || len(bf) != 1 {
		t.Fatalf("b's fabrics %v, %v", bf, err)
	}
	addr := handshake.PeerAddress{FabricIndex: af[0].FabricIndex, NodeID: bf[0].NodeID}
	connect := func(ctx context.Context) error {
		return site.Run(ctx, func(ctx context.Context) error {
			pr, err := a.Node().Peers().For(addr)
			if err != nil {
				return err
			}
			_, err = pr.Connect(ctx, peer.ConnectOptions{})
			return err
		})
	}
	if err := connect(ctx); err != nil {
		t.Fatal(err)
	}
	if err := site.Run(ctx, a.Restart); err != nil {
		t.Fatal(err)
	}

	other := &net.UDPAddr{IP: net.ParseIP("fe80::77"), Port: 5540}
	var (
		mu       sync.Mutex
		captured []byte
		answers  = map[uint16][]uint8{} // exchange id → opcodes b sent to other
	)
	bAddr := b.Addr().String()
	site.Net().Filter(func(d Datagram) bool {
		h, n, err := message.UnmarshalHeader(d.Payload)
		if err != nil || h.SessionID != 0 || len(d.Payload) < n+4 {
			return true
		}
		op := d.Payload[n+1]
		mu.Lock()
		defer mu.Unlock()
		if d.To.String() == bAddr && op == mrp.SCOpcodeSigma1 && d.From.String() != other.String() {
			if captured == nil {
				captured = append([]byte(nil), d.Payload...)
			}
			return false
		}
		if d.To.String() == other.String() {
			x := binary.LittleEndian.Uint16(d.Payload[n+2 : n+4])
			answers[x] = append(answers[x], op)
			return false
		}
		return true
	})
	held, stop := context.WithTimeout(ctx, 2*time.Second)
	_ = connect(held)
	stop()
	mu.Lock()
	genuine := captured
	mu.Unlock()
	if genuine == nil {
		t.Fatal("no resuming Sigma1 was sent")
	}
	hdr, n, err := message.UnmarshalHeader(genuine)
	if err != nil {
		t.Fatal(err)
	}
	copyOn := func(exchange uint16, counter uint32, sessionID uint16) []byte {
		c := append([]byte(nil), genuine...)
		binary.LittleEndian.PutUint32(c[4:8], hdr.MessageCounter+counter)
		binary.LittleEndian.PutUint16(c[n+2:n+4], exchange)
		if sessionID != 0 {
			patched := false
			for j := n + 6; j+4 <= len(c); j++ {
				if c[j] == 0x25 && c[j+1] == 0x02 { // context tag 2, uint16: initiatorSessionId
					binary.LittleEndian.PutUint16(c[j+2:j+4], sessionID)
					patched = true
					break
				}
			}
			if !patched {
				t.Fatal("no initiatorSessionId in the Sigma1")
			}
		}
		return c
	}
	await := func(exchange uint16, want uint8) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			mu.Lock()
			got := append([]uint8(nil), answers[exchange]...)
			mu.Unlock()
			for _, op := range got {
				if op == want {
					return
				}
				if op != mrp.StandaloneAckOpcode {
					t.Fatalf("exchange %#x answered with opcode %#x, want %#x", exchange, op, want)
				}
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatalf("exchange %#x: no opcode %#x", exchange, want)
	}
	site.net.send(other, b.Addr(), copyOn(0x7001, 1000, 0))
	await(0x7001, mrp.SCOpcodeSigma2Resume)
	site.net.send(other, b.Addr(), copyOn(0x7002, 1001, 0x5002))
	await(0x7002, mrp.SCOpcodeSigma2)
}

// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package fabrictest

import (
	"context"
	"encoding/binary"
	"fmt"
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
	"github.com/SukramJ/go-fabric/secure/sigma"
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
	rig := newResumeReplayRig(t)
	rig.send(rig.copyOn(0x7001, 1000, 0, 0))
	rig.await(0x7001, mrp.SCOpcodeSigma2Resume)
	rig.send(rig.copyOn(0x7002, 1001, 0x5002, 0))
	rig.await(0x7002, mrp.SCOpcodeSigma2)
}

// TestR2P6Sec3320Concurrent: review II, R2-P6-sec-3320 (owner decision
// 231) — copies of one resuming Sigma1 sent at once, each on its own
// exchange with its own initiator session id and source node id (fields
// the resume MIC does not cover), resume exactly once; every other copy
// takes the full path.
func TestR2P6Sec3320Concurrent(t *testing.T) {
	rig := newResumeReplayRig(t)
	const copies = 16
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := range uint16(copies) {
		c := rig.copyOn(0x7100+i, 2000+uint32(i), 0x5100+i, uint64(i)+1)
		wg.Go(func() {
			<-start
			rig.send(c)
		})
	}
	close(start)
	wg.Wait()
	resumed := 0
	for i := range uint16(copies) {
		if rig.answer(0x7100+i) == mrp.SCOpcodeSigma2Resume {
			resumed++
		}
	}
	if resumed != 1 {
		t.Fatalf("%d of %d concurrent copies resumed, want exactly 1", resumed, copies)
	}
}

// resumeReplayRig holds node b's genuine resuming Sigma1 from node a and
// the opcodes b answered, per exchange, to copies of it sent from another
// address.
type resumeReplayRig struct {
	t       *testing.T
	site    *Site
	b       *Device
	other   *net.UDPAddr
	genuine []byte
	hdr     message.Header
	n       int
	mu      *sync.Mutex
	answers map[uint16][]uint8
}

func newResumeReplayRig(t *testing.T) *resumeReplayRig {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	t.Cleanup(cancel)
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
	return &resumeReplayRig{t: t, site: site, b: b, other: other, genuine: genuine, hdr: hdr, n: n, mu: &mu, answers: answers}
}

// copyOn is the genuine Sigma1 on exchange with a later counter, and —
// when not zero — another initiator session id and another source node id.
func (r *resumeReplayRig) copyOn(exchange uint16, counter uint32, sessionID uint16, sourceDelta uint64) []byte {
	t, n := r.t, r.n
	t.Helper()
	{
		c := append([]byte(nil), r.genuine...)
		binary.LittleEndian.PutUint32(c[4:8], r.hdr.MessageCounter+counter)
		binary.LittleEndian.PutUint16(c[n+2:n+4], exchange)
		if sourceDelta != 0 && r.hdr.HasSourceNodeID {
			binary.LittleEndian.PutUint64(c[8:16], r.hdr.SourceNodeID+sourceDelta)
		}
		if sessionID != 0 {
			// Decoded and encoded again, not patched in place: TLV encodes
			// the genuine id at its minimal width, one byte below 256, and
			// a byte scan for its tag also matches inside the randoms.
			sigma1, err := sigma.UnmarshalSigma1(c[n+6:])
			if err != nil {
				t.Fatalf("the genuine Sigma1: %v", err)
			}
			sigma1.InitiatorSessionID = sessionID
			c = append(c[:n+6:n+6], sigma1.Marshal()...)
		}
		return c
	}
}

// send hands datagram to b from the other address.
func (r *resumeReplayRig) send(datagram []byte) { r.site.net.send(r.other, r.b.Addr(), datagram) }

// answer is the first opcode other than an acknowledgement b answered on
// exchange, waited for under the site's quiet protocol ([Site.Eventually],
// bounded by [Options].Wait).
func (r *resumeReplayRig) answer(exchange uint16) uint8 {
	r.t.Helper()
	var answered uint8
	err := r.site.Eventually(context.Background(), fmt.Sprintf("an answer on exchange %#x", exchange), func() bool {
		r.mu.Lock()
		defer r.mu.Unlock()
		for _, op := range r.answers[exchange] {
			if op != mrp.StandaloneAckOpcode {
				answered = op
				return true
			}
		}
		return false
	})
	if err != nil {
		r.t.Fatal(err)
	}
	return answered
}

// await fails unless b answered exchange with want.
func (r *resumeReplayRig) await(exchange uint16, want uint8) {
	r.t.Helper()
	if op := r.answer(exchange); op != want {
		r.t.Fatalf("exchange %#x answered with opcode %#x, want %#x", exchange, op, want)
	}
}

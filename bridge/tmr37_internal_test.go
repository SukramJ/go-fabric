// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/im"
)

// fabricPeersLookup is a pasePeersLookup whose sessions belong to the
// fabrics it names.
type fabricPeersLookup struct {
	pasePeersLookup
	fabric map[uint16]uint8
}

func (l fabricPeersLookup) FabricFor(id uint16) (uint8, bool) {
	f, ok := l.fabric[id]
	return f, ok
}

func (c *chunkWaitClaims) heldTotal() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.total
}

// TestTMR37Claims (review II, TM R3.7, decision 245).
func TestTMR37Claims(t *testing.T) {
	t.Parallel()
	var c chunkWaitClaims
	for i := range chunkWaitFabricThreshold {
		if !c.claim(uint16(i/maxChunkWaitsPerSession+1), 1, 0) {
			t.Fatalf("claim %d refused below the threshold", i)
		}
	}
	if !c.claim(0x900, 1, 0) {
		t.Fatal("no share (one fabric, or no counter): a claim at the threshold was refused")
	}
	if c.claim(0x901, 1, 256) {
		t.Fatal("a fabric over its share took a claim at the threshold")
	}
	if !c.claim(0x902, 2, 256) {
		t.Fatal("a fabric under its share was refused at the threshold")
	}
	c.release(0x902, 2)
	c.release(0x902, 2) // a second release of nothing held changes nothing
	if got := c.heldTotal(); got != chunkWaitFabricThreshold+1 {
		t.Fatalf("total %d, want %d", got, chunkWaitFabricThreshold+1)
	}
}

// TestTMR37 (review II, TM R3.7, decision 245). Every read is answered
// once, at once: a read that took a claim with its first chunk, one
// refused with Busy. The test waits for every answer, not for a time.
func TestTMR37(t *testing.T) {
	t.Parallel()
	b := newStartedBridgeWithSnapshotter(t, manyTempSensorsSnapshotterForTest())
	b.chunkStatusResponseTimeoutOverride = time.Minute
	b.SetFabricCounter(func(context.Context) (int, error) { return 2, nil })
	const holders = 256
	ids := make([]uint16, 0, holders+1)
	for i := range holders + 1 {
		ids = append(ids, uint16(0x100+i))
	}
	peers := chunkWaitPeers(t, b, ids...)
	b.mu.RLock()
	base, ok := b.sessions.(pasePeersLookup)
	b.mu.RUnlock()
	if !ok {
		t.Fatal("chunkWaitPeers attached no pasePeersLookup")
	}
	lookup := fabricPeersLookup{pasePeersLookup: base, fabric: map[uint16]uint8{}}
	for _, id := range ids[:holders] {
		lookup.fabric[id] = 1
	}
	lookup.fabric[ids[holders]] = 2
	b.AttachSessionLookup(lookup)
	others, last := peers[:holders], peers[holders]
	// Answers are read from the bridge's socket only: a peer socket on a
	// loopback port another test process also reached (a wildcard bind
	// handed the same port out) would otherwise fail its decrypt on that
	// process's datagram (as 7c15bb7a's reader does; P3c, aesccm failure
	// under make ci).
	bridgeAddr, err := net.ResolveUDPAddr("udp", b.LocalAddr())
	if err != nil {
		t.Fatalf("the bridge's address %q: %v", b.LocalAddr(), err)
	}
	bridgeAddr.IP = net.IPv4(127, 0, 0, 1)

	// One session at a time: its reads, then their answers. A burst of
	// every session's reads overflowed the bridge socket's receive buffer
	// (in one run under Linux's default 212992 bytes, 762 of the 1024 dropped, by
	// /proc/net/udp), and this peer never retransmits. Parked reads keep
	// their claims (the chunk wait runs a minute), so the claims still
	// climb to the threshold and the share still refuses the rest.
	// One answer per read, told apart by exchange: a retransmitted first
	// chunk names an exchange already seen.
	var parked, busy int
	for _, p := range others {
		for i := range maxChunkWaitsPerSession {
			sendToBridge(t, b, p, wildcardRead(t, p, uint16(0x5000+i)))
		}
		seen := map[uint16]bool{}
		for len(seen) < maxChunkWaitsPerSession {
			proto, body, answered := readOneIMDatagramFrom(t, p.conn, bridgeAddr, p.sess, 10*time.Second)
			if !answered {
				t.Fatalf("session %#x: %d of its %d reads answered", p.id, len(seen), maxChunkWaitsPerSession)
			}
			if seen[proto.ExchangeID] {
				continue
			}
			seen[proto.ExchangeID] = true
			switch proto.Opcode {
			case im.OpcodeReportData:
				parked++
			case im.OpcodeStatusResponse:
				if got := decodeStatusResponsePayload(t, body); got != im.StatusBusy {
					t.Fatalf("session %#x: a refused read was answered %v, want Busy", p.id, got)
				}
				busy++
			default:
				t.Fatalf("session %#x: unexpected opcode %#x", p.id, proto.Opcode)
			}
		}
	}
	if got := b.routing.chunkWaits.heldTotal(); got != parked || got > chunkWaitFabricThreshold {
		t.Fatalf("one fabric holds %d chunk waits (%d reads parked, %d refused), want at most %d", got, parked, busy, chunkWaitFabricThreshold)
	}
	if busy == 0 {
		t.Fatal("no read was refused: the share was never applied")
	}

	sendToBridge(t, b, last, wildcardRead(t, last, 0x6000))
	proto, _, answered := readOneIMDatagramFrom(t, last.conn, bridgeAddr, last.sess, 5*time.Second)
	for _, p := range others {
		b.SessionClosed(p.id)
	}
	if !answered || proto.Opcode != im.OpcodeReportData {
		t.Fatalf("the other fabric's read: answered %v, opcode %#x; want a ReportData", answered, proto.Opcode)
	}
}

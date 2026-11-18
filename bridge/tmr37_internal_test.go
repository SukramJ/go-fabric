// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
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

// TestTMR37 (review II, TM R3.7, decision 245).
func TestTMR37(t *testing.T) {
	if testing.Short() {
		t.Skip("starts 1024 reads")
	}
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

	for _, p := range others {
		for i := range maxChunkWaitsPerSession {
			sendToBridge(t, b, p, wildcardRead(t, p, uint16(0x5000+i)))
		}
	}
	deadline := time.Now().Add(60 * time.Second)
	for b.routing.chunkWaits.heldTotal() < chunkWaitFabricThreshold && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond) //nolint:forbidigo // polls the parked count
	}
	time.Sleep(500 * time.Millisecond) //nolint:forbidigo // lets the refused reads settle
	if got := b.routing.chunkWaits.heldTotal(); got > chunkWaitFabricThreshold {
		t.Fatalf("one fabric holds %d chunk waits, want at most %d", got, chunkWaitFabricThreshold)
	}

	sendToBridge(t, b, last, wildcardRead(t, last, 0x6000))
	proto, _, answered := readOneIMDatagram(t, last.conn, last.sess, 5*time.Second)
	for _, p := range others {
		b.SessionClosed(p.id)
	}
	if !answered || proto.Opcode != im.OpcodeReportData {
		t.Fatalf("the other fabric's read: answered %v, opcode %#x; want a ReportData", answered, proto.Opcode)
	}
}

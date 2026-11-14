// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package ble

import (
	"encoding/binary"
	"fmt"
	"testing"
	"time"
)

// TestScanner_RecordsAreBoundedDuringADiscovery: the scanner holds at most
// maxScannerRecords records while a discovery runs, however many distinct
// addresses advertise; past it the record seen longest ago goes (review
// II, R2-P3-ble-02).
func TestScanner_RecordsAreBoundedDuringADiscovery(t *testing.T) {
	t.Parallel()
	clock := newFakeClock()
	c := &mockClient{}
	s := NewScanner(c, ScannerOptions{Clock: clock})
	stop := startScan(t, s)
	defer stop()

	n := maxScannerRecords + 1000
	addr := func(i int) string {
		return fmt.Sprintf("%02x:%02x:%02x:%02x:%02x:%02x", 0xC0, byte(i>>24), byte(i>>16), byte(i>>8), byte(i), 0x01)
	}
	data := make([]byte, 8)
	for i := range n {
		// Opcode 0, discriminator 1737 (version 0), and a vendor and
		// product id that differ per advertisement, so no record is taken
		// for the rotated address of another.
		binary.LittleEndian.PutUint16(data[1:3], 1737)
		binary.LittleEndian.PutUint32(data[3:7], uint32(i)+1) //nolint:gosec // i < n
		c.discover(addr(i), append([]byte(nil), data...))
		clock.Advance(100 * time.Millisecond)
	}
	s.mu.Lock()
	records := len(s.records)
	_, first := s.records[addr(0)]
	_, last := s.records[addr(n-1)]
	s.mu.Unlock()
	if records > maxScannerRecords {
		t.Fatalf("%d distinct advertising addresses during one discovery left %d records, want at most %d", n, records, maxScannerRecords)
	}
	if first || !last {
		t.Fatalf("the record seen first kept: %v, the record seen last kept: %v; want the first gone and the last kept", first, last)
	}
}

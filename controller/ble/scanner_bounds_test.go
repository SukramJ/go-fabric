// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package ble

import (
	"context"
	"encoding/binary"
	"fmt"
	"sync"
	"testing"
	"time"

	"thingfabric.dev/go-fabric/client/discovery"
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

// TestScanner_ContinuousDiscoveryForgetsWithItsRecords: the peripherals a
// continuous discovery has reported are bounded as the records are — one
// whose record the scanner forgot is dropped from them, and reported again
// when it advertises again (review II, R2-P6-bound-2029). The callback
// holds the discovery at one report while the forgotten peripheral
// advertises again, so its new record is stored inside the round, at the
// two points that once hid it: before the round's drop, which kept the
// address because a record of it was held again, and before the round
// read the devices, which passed the new record over as reported.
func TestScanner_ContinuousDiscoveryForgetsWithItsRecords(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name string
		// last is the last peripheral advertised before the hold, and the
		// one whose report holds the round. At maxScannerRecords, the
		// report that takes the reported set past the bound: peripheral 0
		// is forgotten before the round and seen again before its drop. At
		// one less, the round stays within the bound: peripheral 0 is
		// forgotten and seen again before the next round reads the
		// devices.
		last int
	}{
		{"seen again before the drop", maxScannerRecords},
		{"seen again before the round read it", maxScannerRecords - 1},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			continuousForgetting(t, c.last)
		})
	}
}

func continuousForgetting(t *testing.T, last int) {
	t.Helper()
	clock := newFakeClock()
	c := &mockClient{}
	s := NewScanner(c, ScannerOptions{Clock: clock})
	addr := func(i int) string {
		return fmt.Sprintf("%02x:%02x:%02x:%02x:%02x:%02x", 0xC1, byte(i>>24), byte(i>>16), byte(i>>8), byte(i), 0x01)
	}
	var mu sync.Mutex
	reported := map[string]int{}
	held, release := make(chan struct{}), make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	id := discovery.ByLongDiscriminator(1737)
	n := s.waiterCount()
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = s.FindContinuously(ctx, id, func(d Device) {
			mu.Lock()
			reported[d.Peripheral.Address]++
			mu.Unlock()
			if d.Peripheral.Address == addr(last) {
				close(held)
				<-release
			}
		}, 0)
	}()
	settle(t, s, n+1)
	advertise := func(i, vendor int) {
		data := make([]byte, 8)
		binary.LittleEndian.PutUint16(data[1:3], 1737)
		binary.LittleEndian.PutUint32(data[3:7], uint32(vendor)) //nolint:gosec // small
		c.discover(addr(i), data)
		clock.Advance(time.Millisecond)
	}
	times := func(a string) int { mu.Lock(); defer mu.Unlock(); return reported[a] }
	waitFor := func(what string, cond func() bool) {
		t.Helper()
		deadline := time.Now().Add(30 * time.Second)
		for !cond() {
			if time.Now().After(deadline) {
				t.Fatalf("waiting for %s", what)
			}
			yield(s)
		}
	}
	advertise(0, 1)
	waitFor("the first report", func() bool { return times(addr(0)) == 1 })
	for i := 1; i <= last; i++ {
		advertise(i, i+1)
	}
	recv(t, held)
	// Peripheral 0's record is forgotten once maxScannerRecords others
	// are newer; it advertises again while the round is held.
	advertise(last+1, last+2)
	advertise(0, last+3)
	close(release)
	waitFor("the forgotten peripheral reported again", func() bool { return times(addr(0)) == 2 })
	cancel()
	s.Cancel(id)
	<-done
}

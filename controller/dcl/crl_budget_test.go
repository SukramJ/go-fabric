// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package dcl

import (
	"bytes"
	"runtime"
	"testing"
)

// derSequenceOf is a SEQUENCE with a four-byte length around content.
func derSequenceOf(content []byte) []byte {
	n := len(content)
	return append([]byte{0x30, 0x84, byte(n >> 24), byte(n >> 16), byte(n >> 8), byte(n)}, content...)
}

// TestDERNodeBudgetBoundsTheWalk holds R2-C2-bound-1901: a body of
// maxCRLSize made of two-byte elements (one derNode per two bytes, the
// densest DER there is) is refused as malformed, and refusing it does not
// allocate in proportion to the nodes it names — the walk counts a
// constructed node's elements before it allocates them. A CRL of real
// entries (serial, revocation date, a reason-code extension) filling
// maxCRLSize still decodes.
func TestDERNodeBudgetBoundsTheWalk(t *testing.T) {
	hostile := derSequenceOf(bytes.Repeat([]byte{0x05, 0x00}, (maxCRLSize-6)/2))
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	_, err := parseCRL(hostile, nil)
	runtime.ReadMemStats(&after)
	if err == nil {
		t.Fatal("a body of two-byte elements decoded; want it refused")
	}
	if delta := after.TotalAlloc - before.TotalAlloc; delta > 1<<20 {
		t.Fatalf("refusing %d bytes allocated %d bytes; want at most 1 MiB", len(hostile), delta)
	}

	entry := []byte{
		0x30, 0x20, // revoked certificate
		0x02, 0x01, 0x01, // serial
		0x17, 0x0d, '2', '6', '0', '1', '0', '1', '0', '0', '0', '0', '0', '0', 'Z', // revocationDate
		0x30, 0x0c, 0x30, 0x0a, 0x06, 0x03, 0x55, 0x1d, 0x15, 0x04, 0x03, 0x0a, 0x01, 0x01, // reasonCode
	}
	real := derSequenceOf(bytes.Repeat(entry, (maxCRLSize-6)/len(entry)))
	n, err := derDecode(real)
	if err != nil {
		t.Fatalf("a %d-byte list of real entries: %v", len(real), err)
	}
	if want := (maxCRLSize - 6) / len(entry); len(n.elements) != want {
		t.Fatalf("decoded %d entries, want %d", len(n.elements), want)
	}
}

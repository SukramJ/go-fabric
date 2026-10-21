// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package peer

// A port of matter.js packages/protocol/test/peer/PeerEstablishmentProgressTest.ts at
// 862ad75f: the establishment-unresponsive latch (the other cases are
// TestParityUnresponsiveDetector and TestEstablishmentUnresponsive).

import (
	"testing"

	"github.com/SukramJ/go-fabric/client/exchange"
)

// TestPeerEstablishmentProgressDetector is the describe
// "establishmentUnresponsiveDetector".
func TestPeerEstablishmentProgressDetector(t *testing.T) {
	t.Parallel()
	t.Run("keeps an independent latch per detector instance", func(t *testing.T) {
		t.Parallel()
		firedA, firedB := 0, 0
		detectA := unresponsiveDetector(func() { firedA++ })
		detectB := unresponsiveDetector(func() { firedB++ })

		detectA(exchange.MaxTransmissions + 1)
		if firedA != 1 || firedB != 0 {
			t.Fatalf("after A: firedA %d firedB %d, want 1 0", firedA, firedB)
		}
		detectB(exchange.MaxTransmissions + 1)
		if firedA != 1 || firedB != 1 {
			t.Fatalf("after B: firedA %d firedB %d, want 1 1", firedA, firedB)
		}
	})
}

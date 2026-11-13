// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/mdns"
)

// TestAnnounceCommissioningRefusesInconsistentPairingInstructions: a
// pairing hint and a pairing instruction that do not belong together are
// refused before anything is published (review II, W2-wire-13) — matter.js
// throws in the advertisement's constructor
// (packages/protocol/src/advertisement/mdns/CommissionableMdnsAdvertisement.ts
// :40, :106-135 at db021da0). Before, the record went out with whatever
// PH and PI the host passed. The cases themselves are in
// mdns/pairing_instructions_parity_matterjs_test.go.
func TestAnnounceCommissioningRefusesInconsistentPairingInstructions(t *testing.T) {
	t.Parallel()
	b := newStartedBridge(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := b.AnnounceCommissioning(ctx, CommissioningAdvertisement{
		Discriminator: 0xABC, VendorID: 0x1234, ProductID: 0x5678, NodeLabel: "test",
		PairingInstruction: "5", // an instruction, and no hint that asks for one
	})
	if !errors.Is(err, mdns.ErrInvalidService) {
		t.Fatalf("AnnounceCommissioning with a pairing instruction and no pairing hint = %v, want mdns.ErrInvalidService", err)
	}
	b.mu.RLock()
	published := b.commissioningInstanceName
	b.mu.RUnlock()
	if published != "" {
		t.Errorf("a refused advertisement was recorded as published (%q)", published)
	}
}

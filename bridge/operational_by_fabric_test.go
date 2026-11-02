// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"testing"

	"github.com/SukramJ/go-fabric/mdns"
)

// TestOperationalInstanceFor_TracksEachFabricsService pins the table the
// broadcast events resolve a session's fabric through: AnnounceFabric
// records each fabric's operational instance, WithdrawFabric forgets it,
// index 0 and an unknown index find nothing (matter.js
// packages/protocol/src/advertisement/Advertiser.ts:69-75 at 862ad75f,
// advertisementForFabric), and the withdraw of a replaced identity does
// not drop the identity announced after it.
func TestOperationalInstanceFor_TracksEachFabricsService(t *testing.T) {
	t.Parallel()
	b := newStartedBridge(t)
	ctx := context.Background()

	instance := func(cfid byte, nodeID uint64) string {
		return mdns.BuildOperationalService(mdns.OperationalServiceConfig{
			CompressedFabricID: [8]byte{cfid},
			NodeID:             nodeID,
		}).InstanceName
	}

	b.AnnounceFabric(ctx, 1, [8]byte{0x01}, 0x11)
	b.AnnounceFabric(ctx, 2, [8]byte{0x02}, 0x22)
	for _, tc := range []struct {
		fabric uint8
		want   string
		ok     bool
	}{
		{1, instance(0x01, 0x11), true},
		{2, instance(0x02, 0x22), true},
		{0, "", false},
		{3, "", false},
	} {
		got, ok := b.operationalInstanceFor(tc.fabric)
		if got != tc.want || ok != tc.ok {
			t.Errorf("operationalInstanceFor(%d) = %q, %v; want %q, %v", tc.fabric, got, ok, tc.want, tc.ok)
		}
	}

	// UpdateNOC order in node.fabricUpdated: withdraw the old identity,
	// announce the new one. Withdraw-after-announce must not drop it
	// either.
	b.WithdrawFabric(ctx, 2, [8]byte{0x02}, 0x22)
	b.AnnounceFabric(ctx, 2, [8]byte{0x02}, 0x33)
	b.WithdrawFabric(ctx, 2, [8]byte{0x02}, 0x22)
	if got, ok := b.operationalInstanceFor(2); !ok || got != instance(0x02, 0x33) {
		t.Errorf("after UpdateNOC operationalInstanceFor(2) = %q, %v; want %q, true", got, ok, instance(0x02, 0x33))
	}

	b.WithdrawFabric(ctx, 1, [8]byte{0x01}, 0x11)
	if got, ok := b.operationalInstanceFor(1); ok {
		t.Errorf("after WithdrawFabric operationalInstanceFor(1) = %q, true; want none", got)
	}

	if err := b.Stop(ctx); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if got, ok := b.operationalInstanceFor(2); ok {
		t.Errorf("after Stop operationalInstanceFor(2) = %q, true; want none", got)
	}
}

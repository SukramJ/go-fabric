// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core_test

import (
	"context"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/im"
	mstore "github.com/SukramJ/go-fabric/store"
)

// TestFabricFilteredNOCsReadFailsWithoutTheFabricsIdentity: a
// fabric-filtered read of NOCs whose fabric's identity the store cannot
// give is not answered — the reader returns (nil, false), which the
// dispatcher answers FAILURE for this fabric-scoped attribute, as it does
// when the fabric list cannot be read — rather than SUCCESS with a list
// that lacks the requester's own NOC. matter.js derives every fabric's
// NOCStruct from the fabric it holds (OperationalCredentialsServer.ts
// #updateFabrics, :540-545 at db021da0), so its list always carries the
// requester's entry.
func TestFabricFilteredNOCsReadFailsWithoutTheFabricsIdentity(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	fs := newFakeStore()
	idx, err := fs.AddFabric(ctx, mstore.FabricRecord{
		FabricID:      0xAAAA0001,
		NodeID:        0xBBBB0001,
		RootPublicKey: []byte{0x04, 0x01},
		VendorID:      0x1234,
	})
	if err != nil {
		t.Fatalf("AddFabric: %v", err)
	}
	oc, err := core.NewOperationalCredentials(fs, core.OpcredsConfig{SupportedFabrics: 5})
	if err != nil {
		t.Fatalf("NewOperationalCredentials: %v", err)
	}
	v, ok := oc.MatterReadFiltered(im.WithFabricFilter(ctx, true, idx), 0x0000 /* NOCs */)
	if ok {
		t.Fatalf("MatterReadFiltered(NOCs) with the fabric's identity unreadable: ok=true, value %#v; want (nil, false)", v)
	}
}

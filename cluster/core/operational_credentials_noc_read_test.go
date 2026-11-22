// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core_test

import (
	"context"
	"testing"

	"thingfabric.dev/go-fabric/cluster/core"
	"thingfabric.dev/go-fabric/im"
	mstore "thingfabric.dev/go-fabric/store"
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

// TestNOCsReadSkipsAFabricWhoseIdentityIsNotStoredYet: an unfiltered read
// of NOCs leaves out a fabric the store holds no identity for — the state
// of a fabric between handleAddNOC's AddFabric and its UpsertIdentity — and
// answers the others, the list as it was before that AddNOC (review II,
// R2-P4-fr-sec-04). A store error other than "not found" fails the read:
// endpoint's TestFabricScopedReadFailureAnswersFailure.
func TestNOCsReadSkipsAFabricWhoseIdentityIsNotStoredYet(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	fs := newFakeStore()
	var stored uint8
	for i, withIdentity := range []bool{true, false} {
		idx, err := fs.AddFabric(ctx, mstore.FabricRecord{
			FabricID:      0xAAAA0001 + uint64(i),
			NodeID:        0xBBBB0001,
			RootPublicKey: []byte{0x04, byte(i + 1)},
			VendorID:      0x1234,
		})
		if err != nil {
			t.Fatalf("AddFabric: %v", err)
		}
		if withIdentity {
			if err := fs.UpsertIdentity(ctx, mstore.IdentityRecord{FabricIndex: idx, NOC: []byte{0x15, 0x18}}); err != nil {
				t.Fatalf("UpsertIdentity: %v", err)
			}
			stored = idx
		}
	}
	oc, err := core.NewOperationalCredentials(fs, core.OpcredsConfig{SupportedFabrics: 5})
	if err != nil {
		t.Fatalf("NewOperationalCredentials: %v", err)
	}
	v, ok := oc.MatterRead(0x0000 /* NOCs */)
	if !ok {
		t.Fatal("MatterRead(NOCs) with one fabric's identity not stored yet: ok=false, want the list without it")
	}
	nocs, _ := v.([]core.NOCStruct)
	if len(nocs) != 1 || nocs[0].FabricIndex != stored {
		t.Fatalf("MatterRead(NOCs) = %#v, want the one entry of fabric %d", v, stored)
	}
}

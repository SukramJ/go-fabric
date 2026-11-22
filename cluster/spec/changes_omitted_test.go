// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package spec_test

import (
	"context"
	"slices"
	"sync"
	"testing"

	"thingfabric.dev/go-fabric/cluster/spec"
	eth "thingfabric.dev/go-fabric/cluster/spec/ethernetnetworkdiagnostics"
	"thingfabric.dev/go-fabric/contract"
)

// TestChangesOmittedIsNotReported (review II, Fable read 5b-write F6): a
// change of a changes-omitted attribute ("C") advances the version and is
// told to the change listeners, and is left out of the report listeners'
// broadcast; a plain attribute changed with it is reported. matter.js
// filters the suppressed names out before broadcastChanges
// (packages/node/src/behavior/internal/ServerBehaviorBacking.ts:163-168 at
// db021da0).
func TestChangesOmittedIsNotReported(t *testing.T) {
	t.Parallel()
	srv, err := spec.NewServer(eth.Definition, spec.Options{
		Features:   uint32(eth.FeaturePacketCounts),
		Attributes: []uint32{eth.AttrPhyRate},
		Commands:   []uint32{eth.CmdResetCounts},
	}, spec.ServerConfig{
		Initial:  map[uint32]any{eth.AttrPacketRxCount: uint64(0), eth.AttrPhyRate: nil},
		Handlers: map[uint32]spec.CommandHandler{eth.CmdResetCounts: func(context.Context, any) (any, error) { return nil, nil }},
	})
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var changed, reported []uint32
	srv.OnAttributeChange(func(c contract.Change) { mu.Lock(); changed = append(changed, c.Attributes...); mu.Unlock() })
	srv.OnAttributeReport(func(c contract.Change) { mu.Lock(); reported = append(reported, c.Attributes...); mu.Unlock() })
	v0 := srv.MatterDataVersion()
	if err := srv.SetAttributes(map[uint32]any{eth.AttrPacketRxCount: uint64(5), eth.AttrPhyRate: eth.PHYRateRate1G}); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if d := srv.MatterDataVersion() - v0; d != 1 {
		t.Errorf("the version advanced %d times, want once", d)
	}
	if !slices.Contains(changed, eth.AttrPacketRxCount) || !slices.Contains(changed, eth.AttrPhyRate) {
		t.Errorf("the change listeners heard %v, want PacketRxCount and PHYRate", changed)
	}
	if slices.Contains(reported, eth.AttrPacketRxCount) || !slices.Contains(reported, eth.AttrPhyRate) {
		t.Errorf("the report listeners heard %v, want PHYRate alone", reported)
	}
}

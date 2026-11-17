// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"errors"
	"testing"

	userlabeldef "github.com/SukramJ/go-fabric/cluster/spec/userlabel"
	"github.com/SukramJ/go-fabric/device"
	"github.com/SukramJ/go-fabric/endpoint"
	epmem "github.com/SukramJ/go-fabric/endpoint/memstore"
	"github.com/SukramJ/go-fabric/node"
	"github.com/SukramJ/go-fabric/store"
	"github.com/SukramJ/go-fabric/store/memstore"
)

// TestRootRestoreReadFailureFailsNew (review II, R2-P6-store-2403): a
// store that fails the read of a root value the node restores — NodeLabel,
// Location, ConfigurationVersion, the root's and the Aggregator's UserLabel
// list — fails New with the store's error, as a failed RebootCount read
// does. Served as the default, the value would be stored over the stored
// one at the next write.
func TestRootRestoreReadFailureFailsNew(t *testing.T) {
	t.Parallel()
	for _, key := range []string{
		"basic_information.node_label",
		"basic_information.location",
		"basic_information.configuration_version",
		"user_label.root",
		"user_label.aggregator",
	} {
		t.Run(key, func(t *testing.T) {
			t.Parallel()
			st := &refusingKeys{Port: memstore.New(), keys: []string{key}}
			st.refuseRead.Store(true)
			cfg := minimalConfig(nil)
			cfg.Store = node.Store{Credentials: st, Endpoints: epmem.New()}
			n, err := node.New(t.Context(), cfg)
			if err == nil {
				_ = n.Stop(t.Context())
			}
			if !errors.Is(err, errKeyRefused) {
				t.Fatalf("New with the read of %s failing: %v, want the store's error", key, err)
			}
		})
	}
}

// TestBridgedRestoreReadFailureFailsTheAssembly (review II,
// R2-P6-store-2403, -2404): a store that fails the read of a bridged
// endpoint's NodeLabel, ConfigurationVersion, UserLabel list or Binding
// list fails the assembly that would serve it — the Start here — so no
// server is served with a default in the stored value's place (or, for
// Binding, without its store); the stored value is kept, and the start
// after the store recovers serves it.
func TestBridgedRestoreReadFailureFailsTheAssembly(t *testing.T) {
	t.Parallel()
	const key = "switch:1"
	for prefix, stored := range map[string]string{
		"bridged_node_label.":            "stored label",
		"bridged_configuration_version.": "7",
		"bridged_user_label.":            `[{"Label":"room","Value":"kitchen"}]`,
		"binding.":                       `[{"Node":5,"FabricIndex":1}]`,
	} {
		t.Run(prefix, func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()
			mem := memstore.New()
			// The fabric the stored Binding entry names.
			if _, err := mem.AddFabric(ctx, store.FabricRecord{FabricID: 1, NodeID: 2, VendorID: 0xFFF1, RootPublicKey: make([]byte, 65)}); err != nil {
				t.Fatal(err)
			}
			if err := mem.SetSetting(ctx, prefix+key, stored); err != nil {
				t.Fatal(err)
			}
			st := &refusingKeys{Port: mem, keys: []string{prefix + key}}
			st.refuseRead.Store(true)
			cfg := minimalConfig(nil)
			cfg.Store = node.Store{Credentials: st, Endpoints: epmem.New()}
			n := newNode(t, cfg)
			sw, err := device.NewOnOffLightSwitch(device.Options{StableKey: endpoint.StringKey(key), Name: key}, userlabeldef.Declare{})
			if err != nil {
				t.Fatal(err)
			}
			if err := n.Aggregator().Add(ctx, sw); err != nil {
				t.Fatal(err)
			}
			if err := n.Start(ctx); !errors.Is(err, errKeyRefused) {
				t.Fatalf("Start with the read of %s failing: %v, want the store's error", prefix+key, err)
			}
			st.refuseRead.Store(false)
			if err := n.Start(ctx); err != nil {
				t.Fatalf("Start after the store recovered: %v", err)
			}
			if v, ok, err := mem.GetSetting(ctx, prefix+key); err != nil || !ok || v != stored {
				t.Errorf("stored %q (found %v, %v), want %q kept", v, ok, err, stored)
			}
		})
	}
}

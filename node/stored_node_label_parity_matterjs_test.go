// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"context"
	"strings"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	bidef "github.com/SukramJ/go-fabric/cluster/spec/basicinformation"
	userlabeldef "github.com/SukramJ/go-fabric/cluster/spec/userlabel"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/endpoint"
	epmem "github.com/SukramJ/go-fabric/endpoint/memstore"
	"github.com/SukramJ/go-fabric/node"
	"github.com/SukramJ/go-fabric/store/memstore"
	"github.com/SukramJ/go-fabric/tlv"
)

// TestStoredNodeLabelParityMatterJS ports
// packages/node/test/behaviors/basic-information/StoredNodeLabelTest.ts at
// b25ed960 (#4995): a NodeLabel stored while the bound counted UTF-16 code
// units — 11 × "日", 33 bytes — starts the node, is served cut to the 32
// bytes it may hold, and is stored cut with the next write.
func TestStoredNodeLabelParityMatterJS(t *testing.T) {
	t.Parallel()
	t.Run("starts with a stored node label over 32 bytes and stores it shortened", func(t *testing.T) {
		t.Parallel()
		ctx := t.Context()
		st := memstore.New()
		if err := st.SetSetting(ctx, "basic_information.node_label", strings.Repeat("日", 11)); err != nil {
			t.Fatal(err)
		}
		cfg := minimalConfig(nil)
		cfg.Store = node.Store{Credentials: st, Endpoints: epmem.New()}
		n := newNode(t, cfg)
		bi := n.Root().BasicInformation
		if v, _ := bi.MatterRead(bidef.AttrNodeLabel); v.(tlv.BoundedString).Value != strings.Repeat("日", 10) {
			t.Fatalf("NodeLabel %v, want 10 × 日", v)
		}
		if err := bi.MatterWrite(contract.WithOrigin(context.WithoutCancel(ctx), contract.OriginHostSet), bidef.AttrLocation, "DE"); err != nil {
			t.Fatalf("write Location: %v", err)
		}
		if v, _, err := st.GetSetting(ctx, "basic_information.node_label"); err != nil || v != strings.Repeat("日", 10) {
			t.Fatalf("stored NodeLabel %q (%v), want 10 × 日", v, err)
		}
	})
}

// TestStoredOverlongUserLabelStartsShortened: a root and an Aggregator
// UserLabel stored while "max 16" counted UTF-16 code units (9 × "é", 18
// bytes) no longer stop node.New — the restore cuts the Label to its 16
// bytes, as matter.js's datasource cuts a stored string at load
// (packages/node/src/behavior/state/managed/Datasource.ts:149-165 at
// b25ed960, #4995) — and the list is served cut.
func TestStoredOverlongUserLabelStartsShortened(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	st := memstore.New()
	stored := `[{"Label":"` + strings.Repeat("é", 9) + `","Value":"v"}]`
	for _, k := range []string{"user_label.root", "user_label.aggregator"} {
		if err := st.SetSetting(ctx, k, stored); err != nil {
			t.Fatal(err)
		}
	}
	cfg := minimalConfig(nil)
	cfg.Store = node.Store{Credentials: st, Endpoints: epmem.New()}
	n, err := node.New(ctx, cfg)
	if err != nil {
		t.Fatalf("New with a stored 18-byte UserLabel: %v", err)
	}
	t.Cleanup(func() { _ = n.Stop(context.Background()) })
	if err := n.Start(ctx); err != nil {
		t.Fatal(err)
	}
	served := 0
	for _, ep := range n.Bridge().Topology().Endpoints {
		if ep == nil {
			continue
		}
		for _, srv := range endpoint.ClusterServers(ep) {
			if srv.MatterClusterID() != userlabeldef.ClusterID {
				continue
			}
			v, _ := srv.MatterRead(userlabeldef.AttrLabelList)
			list, _ := v.(spec.List[userlabeldef.LabelStruct])
			if len(list) != 1 || list[0].Label != strings.Repeat("é", 8) || list[0].Value != "v" {
				t.Errorf("endpoint %d LabelList %+v, want one entry 8 × é = v", ep.ID, v)
			}
			served++
		}
	}
	if served < 2 {
		t.Fatalf("%d UserLabel servers on the root and the Aggregator, want 2", served)
	}
}

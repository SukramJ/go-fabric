// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"context"
	"strings"
	"testing"

	bidef "github.com/SukramJ/go-fabric/cluster/spec/basicinformation"
	"github.com/SukramJ/go-fabric/contract"
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

// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controller_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/controller"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/store"
)

// TestSetGroupKeyAfterClose (review II, R2-P1-ctl-06): SetGroupKey on a
// closed controller returns an error and writes nothing. Before, it wrote
// the key set and its mapping and then panicked on the closed group
// manager.
func TestSetGroupKeyAfterClose(t *testing.T) {
	t.Parallel()
	var db *store.Store
	site, _, c := siteController(t, fabrictest.Topology{}, func(cfg *controller.Config) { db = store.New(cfg.DB) })
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	if err := site.Run(ctx, c.Close); err != nil {
		t.Fatal(err)
	}
	keySet := store.GroupKeySet{GroupKeySetID: 0x01A1, SecurityPolicy: store.SecurityPolicyTrustFirst, EpochKey0: make([]byte, 16), EpochStart0: 1}
	if err := c.SetGroupKey(ctx, keySet, 0x0101); !errors.Is(err, controller.ErrConfiguration) {
		t.Fatalf("SetGroupKey after Close = %v, want ErrConfiguration", err)
	}
	if sets, err := db.ListGroupKeySets(ctx, c.Fabric().FabricIndex); err != nil || len(sets) != 0 {
		t.Fatalf("key sets after the refused SetGroupKey: %v (%v), want none", sets, err)
	}
}

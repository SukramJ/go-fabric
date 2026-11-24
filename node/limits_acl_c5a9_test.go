// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"thingfabric.dev/go-fabric/node"
)

// TestAccessControlLimitsJudgedOnce (review II, R4-api-4157, #595 call
// site): the node hands its access-control limits to the AccessControl
// constructor, which judges them; Config.Validate judges them first with
// the same bounds, so every limit Validate admits — the floors and the
// ceiling — builds, and one outside them fails New with ErrInvalidConfig
// naming the field before the constructor runs.
func TestAccessControlLimitsJudgedOnce(t *testing.T) {
	t.Parallel()
	for _, l := range []node.Limits{
		{AccessControlEntriesPerFabric: 4, SubjectsPerAccessControlEntry: 4, TargetsPerAccessControlEntry: 3},
		{AccessControlEntriesPerFabric: 65534, SubjectsPerAccessControlEntry: 65534, TargetsPerAccessControlEntry: 65534},
	} {
		cfg := minimalConfig(openDB(t, filepath.Join(t.TempDir(), "node.db")))
		cfg.Limits = l
		n, err := node.New(context.Background(), cfg)
		if err != nil {
			t.Fatalf("New with %+v: %v", l, err)
		}
		_ = n.Stop(context.Background())
	}
	cfg := minimalConfig(openDB(t, filepath.Join(t.TempDir(), "node.db")))
	cfg.Limits = node.Limits{TargetsPerAccessControlEntry: 65535}
	_, err := node.New(context.Background(), cfg)
	if !errors.Is(err, node.ErrInvalidConfig) || !strings.Contains(err.Error(), "Limits.TargetsPerAccessControlEntry") || strings.Contains(err.Error(), "access control:") {
		t.Fatalf("New with TargetsPerAccessControlEntry 65535 = %v; want ErrInvalidConfig from Validate, not the constructor", err)
	}
}

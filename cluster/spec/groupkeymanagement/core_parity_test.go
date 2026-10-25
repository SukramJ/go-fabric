// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package groupkeymanagement_test

import (
	"testing"

	"github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/cluster/spec/groupkeymanagement"
	"github.com/SukramJ/go-fabric/cluster/spec/spectest"
	"github.com/SukramJ/go-fabric/store/memstore"
)

// TestCoreServerMatchesDefinition holds the hand-written
// GroupKeyManagement server of cluster/core (ADR 0013's skip list) against
// the generated definition: its cluster id, revision, FeatureMap, lists
// and privileges (spectest.CheckHandWrittenServer). The server exports no
// wire constant of its own; its key-set fields are checked by the
// cluster/core parity tests. The definition is the client role's typed
// view of the same cluster (ADR 0013, amendment of 2026-10-25).
func TestCoreServerMatchesDefinition(t *testing.T) {
	t.Parallel()
	srv, err := core.NewGroupKeyManagement(memstore.New(), core.GroupKeyMgmtConfig{})
	if err != nil {
		t.Fatal(err)
	}
	spectest.CheckHandWrittenServer(t, srv, groupkeymanagement.Definition)
}

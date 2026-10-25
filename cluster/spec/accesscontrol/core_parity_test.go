// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package accesscontrol_test

import (
	"testing"

	"github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/cluster/spec/accesscontrol"
	"github.com/SukramJ/go-fabric/cluster/spec/spectest"
	"github.com/SukramJ/go-fabric/store/memstore"
)

// TestCoreServerMatchesDefinition holds the hand-written AccessControl
// server of cluster/core (ADR 0013's skip list) against the generated
// definition: its cluster id, revision, lists and privileges
// (spectest.CheckServer) and its exported wire constants. The server is
// not built on the definition; the definition is the client role's typed
// view of the same cluster (ADR 0013, amendment of 2026-10-25).
func TestCoreServerMatchesDefinition(t *testing.T) {
	t.Parallel()
	srv, err := core.NewAccessControl(memstore.New())
	if err != nil {
		t.Fatal(err)
	}
	spectest.CheckHandWrittenServer(t, srv, accesscontrol.Definition)
	for _, c := range []struct {
		name      string
		got, want uint8
	}{
		{"ChangeType Changed", core.AccessControlChangeTypeChanged, uint8(accesscontrol.ChangeTypeChanged)},
		{"ChangeType Added", core.AccessControlChangeTypeAdded, uint8(accesscontrol.ChangeTypeAdded)},
		{"ChangeType Removed", core.AccessControlChangeTypeRemoved, uint8(accesscontrol.ChangeTypeRemoved)},
		{"AuxiliaryType System", core.AccessControlAuxiliaryTypeSystem, uint8(accesscontrol.AccessControlAuxiliaryTypeSystem)},
		{"AuxiliaryType Groupcast", core.AccessControlAuxiliaryTypeGroupcast, uint8(accesscontrol.AccessControlAuxiliaryTypeGroupcast)},
	} {
		if c.got != c.want {
			t.Errorf("%s: server 0x%02X, definition 0x%02X", c.name, c.got, c.want)
		}
	}
}

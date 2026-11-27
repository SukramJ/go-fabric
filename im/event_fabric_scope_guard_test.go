// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package im

import (
	"encoding/json"
	"testing"

	"thingfabric.dev/go-fabric/parity"
)

// TestEveryFabricScopedEventIsFabricSensitive (R2-P6-par-0444) guards the
// one filter AuthorizeEventReports applies to event records: the
// fabric-sensitive one. An event whose payload carries a FabricIndex
// (field 254) is fabric-scoped; matter.js filters a fabric-scoped event
// that is not fabric-sensitive too when the read asks for fabric
// filtering (packages/protocol/src/action/server/EventReadResponse.ts:353-364
// at db91886c), a branch this module does not have. At the pinned snapshot
// every such event is fabric-sensitive, so the branch has no case; this
// test fails when a snapshot brings one that is not, naming the event —
// the branch is then owed in AuthorizeEventReports.
func TestEveryFabricScopedEventIsFabricSensitive(t *testing.T) {
	t.Parallel()
	var snap struct {
		Clusters []struct {
			Name   string `json:"name"`
			Events []struct {
				Name      string `json:"name"`
				Effective struct {
					Access struct {
						Fabric string `json:"fabric"`
					} `json:"access"`
					Fields []struct {
						ID uint32 `json:"id"`
					} `json:"fields"`
				} `json:"effective"`
			} `json:"events"`
		} `json:"clusters"`
	}
	if err := json.Unmarshal(parity.SchemaJSON(), &snap); err != nil {
		t.Fatal(err)
	}
	scoped := 0
	for _, c := range snap.Clusters {
		for _, e := range c.Events {
			hasIndex := false
			for _, f := range e.Effective.Fields {
				if f.ID == 254 {
					hasIndex = true
				}
			}
			if !hasIndex {
				continue
			}
			scoped++
			if e.Effective.Access.Fabric != "S" {
				t.Errorf("%s.%s is fabric-scoped (field 254) but not fabric-sensitive (access fabric %q): AuthorizeEventReports has no filter for it",
					c.Name, e.Name, e.Effective.Access.Fabric)
			}
		}
	}
	if scoped == 0 {
		t.Fatal("no event with a FabricIndex field found — the snapshot's shape moved")
	}
}

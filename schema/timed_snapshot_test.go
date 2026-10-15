// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package schema_test

import (
	"encoding/json"
	"testing"

	"github.com/SukramJ/go-fabric/parity"
	"github.com/SukramJ/go-fabric/schema"
)

// TestTimedInvokeMatchesSnapshot holds the generated timed pin against the
// model it is generated from: IsTimedInvoke answers true for exactly the
// request commands whose effective access in parity/schema.json carries
// "timed", across every cluster of the snapshot. A stale timed_gen.go (the
// generator skipped after a snapshot refresh) fails here.
func TestTimedInvokeMatchesSnapshot(t *testing.T) {
	t.Parallel()
	var snap struct {
		Clusters []struct {
			ID       uint32 `json:"id"`
			Name     string `json:"name"`
			Commands []struct {
				ID        uint32 `json:"id"`
				Name      string `json:"name"`
				Direction string `json:"direction"`
				Effective *struct {
					Access struct {
						Timed bool `json:"timed"`
					} `json:"access"`
				} `json:"effective"`
			} `json:"commands"`
		} `json:"clusters"`
	}
	if err := json.Unmarshal(parity.SchemaJSON(), &snap); err != nil {
		t.Fatalf("parse snapshot: %v", err)
	}
	total := 0
	for _, c := range snap.Clusters {
		want := map[uint32]string{}
		for _, m := range c.Commands {
			if m.Direction == "request" && m.Effective != nil && m.Effective.Access.Timed {
				want[m.ID] = m.Name
			}
		}
		total += len(want)
		for id := range uint32(0x100) {
			name, wantTimed := want[id]
			if got := schema.IsTimedInvoke(c.ID, id); got != wantTimed {
				t.Errorf("%s (0x%04X) command 0x%02X %s: IsTimedInvoke = %v, snapshot timed = %v", c.Name, c.ID, id, name, got, wantTimed)
			}
		}
	}
	if total == 0 {
		t.Fatal("snapshot carries no timed request command: the extractor lost access.timed")
	}
}

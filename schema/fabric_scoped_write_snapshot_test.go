// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package schema_test

import (
	"encoding/json"
	"testing"

	"github.com/SukramJ/go-fabric/parity"
	"github.com/SukramJ/go-fabric/schema"
)

// TestFabricScopedWriteMatchesSnapshot holds the generated table against the
// model it is generated from: IsFabricScopedWrite answers true for exactly
// the attributes whose effective access in parity/schema.json is writable
// (an rw other than "R", matter.js Access.writable) and fabric-scoped or
// fabric-sensitive (matter.js limits.fabricScoped,
// packages/protocol/src/action/server/AccessControl.ts:524 at db021da0),
// across every cluster of the snapshot. A stale attribute_access_gen.go
// fails here.
func TestFabricScopedWriteMatchesSnapshot(t *testing.T) {
	t.Parallel()
	var snap struct {
		Clusters []struct {
			ID         uint32 `json:"id"`
			Name       string `json:"name"`
			Attributes []struct {
				ID        uint32 `json:"id"`
				Name      string `json:"name"`
				Effective *struct {
					Access struct {
						RW     string `json:"rw"`
						Fabric string `json:"fabric"`
					} `json:"access"`
				} `json:"effective"`
			} `json:"attributes"`
		} `json:"clusters"`
	}
	if err := json.Unmarshal(parity.SchemaJSON(), &snap); err != nil {
		t.Fatalf("parse snapshot: %v", err)
	}
	scoped := 0
	for _, c := range snap.Clusters {
		for _, a := range c.Attributes {
			if a.Effective == nil {
				t.Fatalf("%s.%s: the snapshot carries no effective layer", c.Name, a.Name)
			}
			acc := a.Effective.Access
			writable := acc.RW != "" && acc.RW != "R"
			want := writable && (acc.Fabric == "F" || acc.Fabric == "S")
			if want {
				scoped++
			}
			if got := schema.IsFabricScopedWrite(c.ID, a.ID); got != want {
				t.Errorf("%s (0x%04X) %s (0x%04X), rw %q fabric %q: IsFabricScopedWrite = %v, want %v",
					c.Name, c.ID, a.Name, a.ID, acc.RW, acc.Fabric, got, want)
			}
		}
	}
	if scoped == 0 {
		t.Fatal("the snapshot names no writable fabric-scoped attribute: the test compares nothing")
	}
	if schema.IsFabricScopedWrite(0x0028, 0x0005) {
		t.Error("BasicInformation.NodeLabel is not fabric-scoped")
	}
}

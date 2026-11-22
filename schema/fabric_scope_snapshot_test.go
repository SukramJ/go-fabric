// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package schema_test

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"thingfabric.dev/go-fabric/parity"
	"thingfabric.dev/go-fabric/schema"
)

// scopeSnapshot is the part of parity/schema.json the scope table is
// generated from, read here on its own so the comparison does not share the
// generator's decoding.
type scopeSnapshot struct {
	Clusters []struct {
		ID         uint32 `json:"id"`
		Name       string `json:"name"`
		Attributes []struct {
			ID        uint32 `json:"id"`
			Name      string `json:"name"`
			Effective struct {
				Access struct {
					RW     string `json:"rw"`
					Fabric string `json:"fabric"`
				} `json:"access"`
				Entry *struct {
					Type  string `json:"type"`
					Scope string `json:"scope"`
				} `json:"entry"`
			} `json:"effective"`
		} `json:"attributes"`
		Events []struct {
			ID        uint32 `json:"id"`
			Name      string `json:"name"`
			Effective struct {
				Access struct {
					ReadPriv string `json:"readPriv"`
					Fabric   string `json:"fabric"`
				} `json:"access"`
			} `json:"effective"`
		} `json:"events"`
		Datatypes []scopeDatatype `json:"datatypes"`
	} `json:"clusters"`
	GlobalDatatypes []scopeDatatype `json:"globalDatatypes"`
	Definitions     []struct {
		Name      string          `json:"name"`
		Datatypes []scopeDatatype `json:"datatypes"`
	} `json:"definitions"`
}

type scopeDatatype struct {
	Name   string `json:"name"`
	Fields []struct {
		ID     uint32 `json:"id"`
		Access struct {
			Fabric string `json:"fabric"`
		} `json:"access"`
	} `json:"fields"`
}

// TestFabricScopeTableMatchesSnapshot derives the three classes of
// fabric-sensitive data from parity/schema.json and holds the generated
// tables to them: class A, a fabric-scoped ("F") list whose entry struct has
// fabric-sensitive ("S") fields — Fields lists exactly those ids; class B, a
// list whose own access is "S" — Sensitive; class C, an event whose access
// is "S" — EventReadScope's Sensitive. Every other "F" list has a row with
// neither, every readable attribute without a fabric flag has none, and
// every event's read privilege is its effective readPriv. The counts are the
// model's at the pin (10 / 6 / 10, and 7 plain "F" lists); a regenerated
// snapshot that moves one changes the count here on purpose.
func TestFabricScopeTableMatchesSnapshot(t *testing.T) {
	t.Parallel()
	var snap scopeSnapshot
	if err := json.Unmarshal(parity.SchemaJSON(), &snap); err != nil {
		t.Fatalf("parse snapshot: %v", err)
	}
	global := map[string]scopeDatatype{}
	for _, d := range snap.GlobalDatatypes {
		global[d.Name] = d
	}
	shared := map[string]scopeDatatype{}
	for _, def := range snap.Definitions {
		for _, d := range def.Datatypes {
			shared[def.Name+"."+d.Name] = d
		}
	}
	privilege := map[string]uint8{"V": 1, "O": 3, "M": 4, "A": 5}
	var classA, classB, classC, plain int
	for _, c := range snap.Clusters {
		own := map[string]scopeDatatype{}
		for _, d := range c.Datatypes {
			own[d.Name] = d
		}
		for _, a := range c.Attributes {
			acc := a.Effective.Access
			got, ok := schema.AttributeFabricScope(c.ID, a.ID)
			if acc.Fabric == "" || acc.RW == "W" {
				if ok {
					t.Errorf("%s.%s: a scope row for an attribute that is not fabric-scoped", c.Name, a.Name)
				}
				continue
			}
			if !ok {
				t.Errorf("%s.%s (access fabric %q): no scope row", c.Name, a.Name, acc.Fabric)
				continue
			}
			if a.Effective.Entry == nil {
				t.Fatalf("%s.%s: fabric %q on an attribute without a list entry", c.Name, a.Name, acc.Fabric)
			}
			name := a.Effective.Entry.Type
			entry, found := shared[name]
			if !found && !strings.Contains(name, ".") {
				entry, found = own[name]
				if !found {
					entry, found = global[name]
				}
			}
			if !found {
				t.Fatalf("%s.%s: entry struct %q not found", c.Name, a.Name, name)
			}
			var want []uint32
			for _, f := range entry.Fields {
				if f.Access.Fabric == "S" {
					want = append(want, f.ID)
				}
			}
			if !slices.Equal(got.Fields, want) {
				t.Errorf("%s.%s: Fields = %v, want %v", c.Name, a.Name, got.Fields, want)
			}
			if got.Sensitive != (acc.Fabric == "S") {
				t.Errorf("%s.%s: Sensitive = %v, access fabric %q", c.Name, a.Name, got.Sensitive, acc.Fabric)
			}
			switch {
			case acc.Fabric == "S":
				classB++
			case len(want) > 0:
				classA++
			default:
				plain++
			}
		}
		for _, e := range c.Events {
			acc := e.Effective.Access
			got, ok := schema.EventReadScope(c.ID, e.ID)
			if !ok {
				t.Errorf("%s.%s: no event row", c.Name, e.Name)
				continue
			}
			if got.Sensitive != (acc.Fabric == "S") {
				t.Errorf("%s.%s: Sensitive = %v, access fabric %q", c.Name, e.Name, got.Sensitive, acc.Fabric)
			}
			if p, known := privilege[acc.ReadPriv]; !known || got.ReadPrivilege != p {
				t.Errorf("%s.%s: ReadPrivilege = %d, readPriv %q", c.Name, e.Name, got.ReadPrivilege, acc.ReadPriv)
			}
			if acc.Fabric == "S" {
				classC++
			}
		}
	}
	if classA != 10 || classB != 6 || classC != 10 || plain != 7 {
		t.Errorf("classes A/B/C = %d/%d/%d with %d plain F lists, want 10/6/10 with 7", classA, classB, classC, plain)
	}
}

// TestAttributeFabricScopeFieldsAreTheCallersCopy holds the accessor to
// handing out a copy: a caller that edits Fields does not edit the table.
func TestAttributeFabricScopeFieldsAreTheCallersCopy(t *testing.T) {
	t.Parallel()
	s, ok := schema.AttributeFabricScope(0x001F, 0x0000)
	if !ok || len(s.Fields) == 0 {
		t.Fatalf("AccessControl.Acl: scope %+v, %v", s, ok)
	}
	s.Fields[0] = 0xFF
	again, _ := schema.AttributeFabricScope(0x001F, 0x0000)
	if again.Fields[0] == 0xFF {
		t.Error("the table's Fields were edited through a returned scope")
	}
	if s := schema.ClusterEventScope(0x0006); s.ReadPrivilege != 1 || s.Sensitive {
		t.Errorf("OnOff: ClusterEventScope = %+v, want View, not sensitive", s)
	}
	if s := schema.ClusterEventScope(0x001F); s.ReadPrivilege != 5 || !s.Sensitive {
		t.Errorf("AccessControl: ClusterEventScope = %+v, want Administer, sensitive", s)
	}
	if s := schema.ClusterEventScope(0xFFF1FC00); s.ReadPrivilege != 1 || s.Sensitive {
		t.Errorf("a cluster outside the snapshot: ClusterEventScope = %+v, want View, not sensitive", s)
	}
}

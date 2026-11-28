// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"strings"
	"testing"
)

// TestNullableStructAttributeGetsNoStructDefaultP10 (P10-doc-6905): a
// struct-typed attribute without a stated default gets the struct default
// its fields state, a nullable one does not — structDefault's comment
// states the rule, the generator holds it for every snapshot, not only
// the current one.
func TestNullableStructAttributeGetsNoStructDefaultP10(t *testing.T) {
	t.Parallel()
	m := `"conformance":{"text":"M","ast":{"type":"M"}}`
	snap := synthetic(t, `{"id":1,"name":"Defaults","revision":1,
		"attributes":[
			{"id":0,"name":"Plain","effective":{"type":"Pt","metatype":"object","primitive":"struct"}},
			{"id":1,"name":"Nul","effective":{"type":"Pt","metatype":"object","primitive":"struct","quality":{"nullable":true}}}
		],
		"commands":[],"events":[],
		"datatypes":[{"name":"Pt","type":"struct","metatype":"object","primitive":"struct","fields":[{"id":0,"name":"X","type":"uint8","metatype":"integer","primitive":"uint8",`+m+`,"default":0}]}]}`)
	o, err := generate(snap, "Defaults")
	if err != nil {
		t.Fatal(err)
	}
	attr := func(name string) string {
		src := string(o.Source)
		i := strings.Index(src, "ID: Attr"+name+",")
		if i < 0 {
			t.Fatalf("no attribute %s in the definition", name)
		}
		j := strings.Index(src[i:], "\n\t\t},")
		if j < 0 {
			t.Fatalf("attribute %s not closed", name)
		}
		return strings.Join(strings.Fields(src[i:i+j]), " ") // gofmt's column alignment aside
	}
	if got := attr("Plain"); !strings.Contains(got, "Default: Pt{X: 0},") {
		t.Errorf("Plain: no struct default: %s", got)
	}
	if got := attr("Nul"); strings.Contains(got, "Default:") {
		t.Errorf("Nul (nullable): has a default: %s", got)
	}
}

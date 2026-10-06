// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// synthetic builds a one-cluster snapshot around the given members.
func synthetic(t *testing.T, cluster string) *snapshot {
	t.Helper()
	s, err := parseSnapshot([]byte(`{"matter":{"revision":"1.6.1","sourceCommit":"0123456789"},"clusters":[` + cluster + `]}`))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestGenerateRejects(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, cluster, want string
	}{
		{"unknown metatype", `{"id":1,"name":"A","attributes":[{"id":0,"name":"X","effective":{"type":"t","metatype":"weird"}}],"commands":[],"events":[]}`, "unsupported metatype"},
		{"list without entry", `{"id":1,"name":"A","attributes":[{"id":0,"name":"X","effective":{"type":"list","metatype":"array"}}],"commands":[],"events":[]}`, "no entry type"},
		{"integer without width", `{"id":1,"name":"A","attributes":[{"id":0,"name":"X","effective":{"type":"t","metatype":"integer","primitive":"huge"}}],"commands":[],"events":[]}`, "no integer width"},
		{"bitmap member without bit", `{"id":1,"name":"A","attributes":[],"commands":[],"events":[],"datatypes":[{"name":"FlagsBitmap","type":"map8","metatype":"bitmap","primitive":"uint8","fields":[{"name":"F"}]}]}`, "has no bit"},
		{"colliding datatype", `{"id":1,"name":"A","attributes":[],"commands":[],"events":[],"datatypes":[{"name":"Feature","type":"enum8","metatype":"enum","primitive":"uint8","fields":[]}]}`, "collides"},
		{"colliding command", `{"id":1,"name":"A","attributes":[],"commands":[{"id":0,"name":"Definition","effective":{"direction":"response","fields":[]}},{"id":1,"name":"Definition","effective":{"direction":"response","fields":[]}}],"events":[]}`, "collides"},
		{"colliding event", `{"id":1,"name":"A","attributes":[],"commands":[],"events":[{"id":0,"name":"E","effective":{"fields":[]}},{"id":1,"name":"E","effective":{"fields":[]}}]}`, "collides"},
		{"colliding attribute", `{"id":1,"name":"A","attributes":[{"id":0,"name":"X","effective":{"type":"bool","metatype":"boolean"}},{"id":1,"name":"X","effective":{"type":"bool","metatype":"boolean"}}],"commands":[],"events":[]}`, "collides"},
		{"colliding feature", `{"id":1,"name":"A","attributes":[],"commands":[],"events":[],"features":[{"name":"F","bit":0,"effective":{"title":"T"}},{"name":"G","bit":1,"effective":{"title":"T"}}]}`, "collides"},
		{"colliding enum value", `{"id":1,"name":"A","attributes":[],"commands":[],"events":[],"datatypes":[{"name":"E","type":"enum8","metatype":"enum","primitive":"uint8","fields":[{"id":0,"name":"V"},{"id":1,"name":"V"}]}]}`, "collides"},
		{"colliding bitmap member", `{"id":1,"name":"A","attributes":[],"commands":[],"events":[],"datatypes":[{"name":"B","type":"map8","metatype":"bitmap","primitive":"uint8","fields":[{"name":"V","constraint":{"value":0}},{"name":"V","constraint":{"value":1}}]}]}`, "collides"},
		{"bad struct field", `{"id":1,"name":"A","attributes":[],"commands":[],"events":[],"datatypes":[{"name":"S","type":"struct","metatype":"object","primitive":"struct","fields":[{"id":0,"name":"F","type":"t","metatype":"weird"}]}]}`, "unsupported metatype"},
		{"bad command field", `{"id":1,"name":"A","attributes":[],"commands":[{"id":0,"name":"C","effective":{"direction":"request","fields":[{"id":0,"name":"F","type":"t","metatype":"weird"}]}}],"events":[]}`, "command C"},
		{"bad event field", `{"id":1,"name":"A","attributes":[],"commands":[],"events":[{"id":0,"name":"E","effective":{"fields":[{"id":0,"name":"F","type":"t","metatype":"weird"}]}}]}`, "event E"},
		{"colliding anonymous struct", `{"id":1,"name":"A","attributes":[{"id":0,"name":"X","effective":{"type":"struct","metatype":"object","fields":[]}},{"id":1,"name":"X","effective":{"type":"struct","metatype":"object","fields":[]}}],"commands":[],"events":[]}`, "collides"},
		{"bad anonymous field", `{"id":1,"name":"A","attributes":[{"id":0,"name":"X","effective":{"type":"struct","metatype":"object","fields":[{"id":0,"name":"F","metatype":"weird"}]}}],"commands":[],"events":[]}`, "unsupported metatype"},
		{"bad named struct field", `{"id":1,"name":"A","attributes":[{"id":0,"name":"X","effective":{"type":"S","metatype":"object"}}],"commands":[],"events":[],"datatypes":[{"name":"S","metatype":"object","fields":[{"id":0,"name":"F","metatype":"weird"}]}]}`, "unsupported metatype"},
		{"bad named enum", `{"id":1,"name":"A","attributes":[{"id":0,"name":"Feature","effective":{"type":"Feature","metatype":"enum","primitive":"uint8"}}],"commands":[],"events":[],"datatypes":[]}`, ""},
	}
	for _, tc := range cases {
		_, err := generate(synthetic(t, tc.cluster), "A")
		if tc.want == "" {
			continue
		}
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: %v, want %q", tc.name, err, tc.want)
		}
	}
	if _, err := generate(synthetic(t, `{"id":1,"name":"A","attributes":[],"commands":[],"events":[]}`), "B"); err == nil {
		t.Error("an unknown cluster generated")
	}
}

// TestGenerateShapes covers the shapes the committed clusters do not: an
// anonymous struct, a nullable list entry, signed and bounded integers,
// octet strings, floats, a struct-typed attribute and a status-only
// response.
func TestGenerateShapes(t *testing.T) {
	t.Parallel()
	m := `"conformance":{"text":"M","ast":{"type":"M"}}`
	snap := synthetic(t, `{"id":1,"name":"Shapes","revision":2,"base":"Base",
		"attributes":[
			{"id":0,"name":"Anon","effective":{"type":"list","metatype":"array","entry":{"type":"struct","metatype":"object","fields":[{"id":0,"name":"F","type":"int8","metatype":"integer","primitive":"int8",`+m+`,"constraint":{"text":"-5 to 5","min":-5,"max":5}}]}}},
			{"id":1,"name":"Real","effective":{"type":"double","metatype":"float","primitive":"double","default":1.5}},
			{"id":2,"name":"Rec","effective":{"type":"Pair","metatype":"object","primitive":"struct"}},
			{"id":3,"name":"Untyped","effective":{"conformance":{"text":"X","ast":{"type":"X"}}}},
			{"id":4,"name":"Inline","effective":{"type":"struct","metatype":"object","fields":[{"id":0,"name":"G","type":"bool","metatype":"boolean",`+m+`}]}},
			{"id":65532,"name":"FeatureMap","effective":{"type":"FeatureMap","metatype":"bitmap","primitive":"uint32"}}
		],
		"commands":[
			{"id":0,"name":"Ping","effective":{"direction":"request","response":"status","fields":[{"id":0,"name":"Text","type":"string","metatype":"string","primitive":"string","constraint":{"text":"max 8","max":8}},{"id":1,"name":"Ignored"}]}},
			{"id":1,"name":"Pair","effective":{"direction":"response","fields":[]}}
		],
		"events":[{"id":0,"name":"Ev","effective":{"priority":"debug","fields":[{"id":0,"name":"Rec","type":"Pair","metatype":"object","primitive":"struct","quality":{"nullable":true}}]}}],
		"datatypes":[
			{"name":"Pair","type":"struct","metatype":"object","primitive":"struct","fields":[{"id":0,"name":"EncodeTLV","type":"bool","metatype":"boolean",`+m+`}]},
			{"name":"Shape","type":"struct","metatype":"object","primitive":"struct","fields":[
				{"id":0,"name":"Nulls","type":"list","metatype":"array",`+m+`,"entry":{"type":"uint8","metatype":"integer","primitive":"uint8","quality":{"nullable":true}}},
				{"id":1,"name":"Blob","type":"octstr","metatype":"bytes","primitive":"octstr",`+m+`,"constraint":{"text":"4","value":4}},
				{"id":2,"name":"Single","type":"single","metatype":"float","primitive":"single",`+m+`},
				{"id":3,"name":"Double","type":"double","metatype":"float","primitive":"double",`+m+`},
				{"id":4,"name":"Fifty","type":"uint8","metatype":"integer","primitive":"uint8",`+m+`,"constraint":{"text":"min 50","min":50}},
				{"id":5,"name":"Signed","type":"int16","metatype":"integer","primitive":"int16",`+m+`},
				{"id":6,"name":"Bytes","type":"octstr","metatype":"bytes","primitive":"octstr",`+m+`}
			]},
			{"name":"Alias","type":"uint16","metatype":"integer","primitive":"uint16","fields":[]}]}`)
	o, err := generate(snap, "Shapes")
	if err != nil {
		t.Fatal(err)
	}
	src := string(o.Source)
	for _, want := range []string{
		"type AnonEntry struct", "spec.DecodeIntIn[int8](8, -5, 5)", "[]spec.Nullable[uint8]", "spec.DecodeBytesIn(4, 4)",
		"Single float32", "Double float64", "Struct: PairDef", "float64(1.5)", "PairResponseCommand", "EncodeTLVField",
		"spec.DecodeUintIn[uint8](8, 50, 255)", "spec.DecodeStringIn(0, 8)", "spec.PriorityDebug", `Base: "Base"`,
		"spec.DecodeOptional(spec.DecodeNullable(spec.DecodeStruct[Pair]))", "decodeAttrRec", "spec.DecodeInt[int16](16)",
		"type Inline struct", "spec.DecodeBytes)",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("source lacks %q", want)
		}
	}
	if strings.Contains(src, "AttrFeatureMap") {
		t.Error("a global attribute was generated")
	}
	for _, want := range []string{"spectest.RoundTripAttribute(t, Definition, AttrRec", "spectest.RoundTripEvent", "[]byte{1, 1, 1, 1}"} {
		if !strings.Contains(string(o.Test), want) {
			t.Errorf("test lacks %q", want)
		}
	}
}

func TestParseSnapshot(t *testing.T) {
	t.Parallel()
	if _, err := parseSnapshot([]byte(`[`)); err == nil {
		t.Error("broken JSON parsed")
	}
	if _, err := parseSnapshot([]byte(`{"clusters":[]}`)); err == nil {
		t.Error("an empty snapshot parsed")
	}
	if _, err := parseSnapshot([]byte(`{"clusters":[{"id":1,"name":"A","attributes":[{"id":0,"name":"X"}]}]}`)); err == nil {
		t.Error("a snapshot without the resolved layer parsed")
	}
	if _, err := loadSnapshot(filepath.Join(t.TempDir(), "none.json")); err == nil {
		t.Error("a missing file loaded")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "definition_gen.go"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := write(filepath.Join(dir, "definition_gen.go"), &output{Package: "p"}); err == nil {
		t.Error("write below a file succeeded")
	}
}

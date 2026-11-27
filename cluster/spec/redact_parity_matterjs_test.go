// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package spec_test

import (
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"

	"thingfabric.dev/go-fabric/cluster/spec"
	accountlogin "thingfabric.dev/go-fabric/cluster/spec/accountlogin"
	admincomm "thingfabric.dev/go-fabric/cluster/spec/administratorcommissioning"
	commctrl "thingfabric.dev/go-fabric/cluster/spec/commissionercontrol"
	contentcontrol "thingfabric.dev/go-fabric/cluster/spec/contentcontrol"
	doorlock "thingfabric.dev/go-fabric/cluster/spec/doorlock"
	gendiag "thingfabric.dev/go-fabric/cluster/spec/generaldiagnostics"
	groupcast "thingfabric.dev/go-fabric/cluster/spec/groupcast"
	gkm "thingfabric.dev/go-fabric/cluster/spec/groupkeymanagement"
	icdm "thingfabric.dev/go-fabric/cluster/spec/icdmanagement"
	jfadmin "thingfabric.dev/go-fabric/cluster/spec/jointfabricadministrator"
	jfds "thingfabric.dev/go-fabric/cluster/spec/jointfabricdatastore"
	netcomm "thingfabric.dev/go-fabric/cluster/spec/networkcommissioning"
	onoff "thingfabric.dev/go-fabric/cluster/spec/onoff"
	opcreds "thingfabric.dev/go-fabric/cluster/spec/operationalcredentials"
	tbrm "thingfabric.dev/go-fabric/cluster/spec/threadborderroutermanagement"
	tnd "thingfabric.dev/go-fabric/cluster/spec/threadnetworkdirectory"
	webrtcp "thingfabric.dev/go-fabric/cluster/spec/webrtctransportprovider"
	webrtcr "thingfabric.dev/go-fabric/cluster/spec/webrtctransportrequestor"
	wifinm "thingfabric.dev/go-fabric/cluster/spec/wifinetworkmanagement"
	"thingfabric.dev/go-fabric/parity"
	"thingfabric.dev/go-fabric/tlv"
)

// Parity with matter.js packages/model/test/logic/SecretRedactionTest.ts at db91886c,
// ported by meaning: matter.js redacts a JavaScript value keyed by property
// name, this module a TLV [spec.Node] keyed by field id, so a value is
// written here as the JSON form of [spec.EncodeJSON] and the result read
// as the untyped form of [spec.DecodeJSON] (members keyed by field id).
// Not ported: "replaces a field a derived cluster redefines without marking
// it" (:87, the base chain is flattened into the snapshot at extraction —
// TestGeneratedSecretMarksMatchSnapshot holds the result), "returns its
// input while redaction is off" (:154) and "keeps an entry of a secret list
// while redaction is off" (:228; this module has no off switch), "keeps the
// mark in elemental form" (:172, matter.js's toElement API; the generated
// definitions carrying the mark is TestGeneratedSecretMarksMatchSnapshot).

func commandNamed(t *testing.T, def *spec.Cluster, name string) *spec.Field {
	t.Helper()
	for i := range def.Commands {
		if def.Commands[i].Name == name {
			return spec.CommandField(&def.Commands[i])
		}
	}
	t.Fatalf("%s has no command %s", def.Name, name)
	return nil
}

func encode(t *testing.T, f *spec.Field, js string) spec.Node {
	t.Helper()
	n, err := spec.EncodeJSON(f, json.RawMessage(js))
	if err != nil {
		t.Fatalf("encode %s: %v", js, err)
	}
	return n
}

func untyped(t *testing.T, n spec.Node) string {
	t.Helper()
	out, err := spec.DecodeJSON(nil, n)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	return string(out)
}

// sameNode reports whether b is a as it is: the same element and the very
// same children — Go's counterpart of matter.js's `.equals(input)`.
func sameNode(a, b spec.Node) bool {
	if !reflect.DeepEqual(a, b) {
		return false
	}
	return len(a.Children) == 0 || &a.Children[0] == &b.Children[0]
}

func TestSecretRedactionParityMatterJS(t *testing.T) {
	t.Parallel()
	const p = `"(redacted)"`

	// SecretRedactionTest.ts:28-37 at db91886c
	t.Run("replaces a secret field and keeps the others", func(t *testing.T) {
		t.Parallel()
		f := commandNamed(t, netcomm.Definition, "AddOrUpdateThreadNetwork")
		got := untyped(t, spec.Redact(f, encode(t, f, `{"operationalDataset":"hex:010203","breadcrumb":5}`)))
		if want := `{"0":` + p + `,"1":5}`; got != want {
			t.Errorf("got %s, want %s", got, want)
		}
	})
	// SecretRedactionTest.ts:39-45 at db91886c
	t.Run("replaces a secret in a response", func(t *testing.T) {
		t.Parallel()
		f := commandNamed(t, wifinm.Definition, "NetworkPassphraseResponse")
		got := untyped(t, spec.Redact(f, encode(t, f, `{"passphrase":"hex:01"}`)))
		if want := `{"0":` + p + `}`; got != want {
			t.Errorf("got %s, want %s", got, want)
		}
	})
	// SecretRedactionTest.ts:47-53 at db91886c
	t.Run("replaces a secret inside a struct and keeps a null secret", func(t *testing.T) {
		t.Parallel()
		f := commandNamed(t, gkm.Definition, "KeySetWrite")
		in := encode(t, f, `{"groupKeySet":{"groupKeySetId":1,"groupKeySecurityPolicy":0,`+
			`"epochKey0":"hex:00000000000000000000000000000000","epochStartTime0":1,`+
			`"epochKey1":null,"epochStartTime1":null,"epochKey2":null,"epochStartTime2":null}}`)
		got := untyped(t, spec.Redact(f, in))
		want := `{"0":{"0":1,"1":0,"2":` + p + `,"3":1,"4":null,"5":null,"6":null,"7":null}}`
		if got != want {
			t.Errorf("got %s, want %s", got, want)
		}
	})
	// SecretRedactionTest.ts:55-65 at db91886c
	t.Run("replaces secrets in list entries of a type from another cluster", func(t *testing.T) {
		t.Parallel()
		f := commandNamed(t, webrtcp.Definition, "SolicitOffer")
		in := encode(t, f, `{"streamUsage":1,"originatingEndpointId":2,"iceServers":[{"urls":["stun:example"],"username":"user","credential":"password"}]}`)
		got := untyped(t, spec.Redact(f, in))
		want := `{"0":1,"1":2,"4":[{"0":["stun:example"],"1":"user","2":` + p + `}]}`
		if got != want {
			t.Errorf("got %s, want %s", got, want)
		}
	})
	// SecretRedactionTest.ts:67-73 at db91886c
	t.Run("does not modify its input", func(t *testing.T) {
		t.Parallel()
		f := commandNamed(t, netcomm.Definition, "AddOrUpdateThreadNetwork")
		const js = `{"operationalDataset":"hex:01","breadcrumb":5}`
		in := encode(t, f, js)
		spec.Redact(f, in)
		if !reflect.DeepEqual(in, encode(t, f, js)) {
			t.Errorf("input modified: %s", untyped(t, in))
		}
	})
	// SecretRedactionTest.ts:75-79 at db91886c
	t.Run("returns its input for a model without secrets", func(t *testing.T) {
		t.Parallel()
		f := commandNamed(t, onoff.Definition, "OffWithEffect")
		in := encode(t, f, `{"effectIdentifier":0,"effectVariant":1}`)
		if out := spec.Redact(f, in); !sameNode(in, out) {
			t.Errorf("got a new value %s", untyped(t, out))
		}
	})
	// SecretRedactionTest.ts:81-85 at db91886c
	t.Run("returns its input without a model", func(t *testing.T) {
		t.Parallel()
		f := commandNamed(t, netcomm.Definition, "AddOrUpdateThreadNetwork")
		in := encode(t, f, `{"operationalDataset":"hex:01"}`)
		if out := spec.Redact(nil, in); !sameNode(in, out) {
			t.Errorf("got a new value %s", untyped(t, out))
		}
	})
	// SecretRedactionTest.ts:104-118 at db91886c
	t.Run("replaces a field a custom cluster marks secret", func(t *testing.T) {
		t.Parallel()
		login := spec.CommandField(&spec.Command{ID: 0, Name: "Login", Fields: []spec.Field{
			{ID: 0, Name: "User", Type: spec.Type{Name: "string", Kind: spec.KindString}},
			{ID: 1, Name: "Token", Type: spec.Type{Name: "octstr", Kind: spec.KindBytes}, Secret: true},
		}})
		got := untyped(t, spec.Redact(login, encode(t, login, `{"user":"me","token":"hex:01"}`)))
		if want := `{"0":"me","1":` + p + `}`; got != want {
			t.Errorf("got %s, want %s", got, want)
		}
	})
	// SecretRedactionTest.ts:120-138 at db91886c
	t.Run("replaces a secret in a self-referencing struct", func(t *testing.T) {
		t.Parallel()
		s := &spec.Struct{Name: "NodeStruct"}
		s.Fields = []spec.Field{
			{ID: 0, Name: "Key", Type: spec.Type{Name: "octstr", Kind: spec.KindBytes}, Secret: true},
			{ID: 1, Name: "Child", Type: spec.Type{Name: "NodeStruct", Kind: spec.KindStruct, Struct: s}},
		}
		root := spec.AttributeField(&spec.Attribute{ID: 0, Name: "Root", Type: spec.Type{Name: "NodeStruct", Kind: spec.KindStruct, Struct: s}})
		got := untyped(t, spec.Redact(root, encode(t, root, `{"key":"hex:01","child":{"key":"hex:02"}}`)))
		if want := `{"0":` + p + `,"1":{"0":` + p + `}}`; got != want {
			t.Errorf("got %s, want %s", got, want)
		}
	})
	// SecretRedactionTest.ts:140-152 at db91886c
	t.Run("reports no secret in a self-referencing struct without one", func(t *testing.T) {
		t.Parallel()
		s := &spec.Struct{Name: "NodeStruct"}
		s.Fields = []spec.Field{{ID: 1, Name: "Child", Type: spec.Type{Name: "NodeStruct", Kind: spec.KindStruct, Struct: s}}}
		root := spec.AttributeField(&spec.Attribute{ID: 0, Name: "Root", Type: spec.Type{Name: "NodeStruct", Kind: spec.KindStruct, Struct: s}})
		if spec.ContainsSecret(root) {
			t.Error("ContainsSecret = true")
		}
	})
	// SecretRedactionTest.ts:179-186 at db91886c — redactAttributes over
	// a cluster's state keyed by property name has no Go counterpart (no
	// property-named cluster state); its meaning, attribute by attribute,
	// is Redact on each attribute's field, as the bridge logs it.
	t.Run("redactAttributes: replaces a secret attribute value and keeps the others", func(t *testing.T) {
		t.Parallel()
		key := spec.AttributeField(doorlock.Definition.Attribute(doorlock.AttrAliroGroupResolvingKey))
		got := untyped(t, spec.Redact(key, encode(t, key, `"hex:00000000000000000000000000000000"`)))
		if got != p {
			t.Errorf("aliroGroupResolvingKey: got %s, want %s", got, p)
		}
		users := spec.AttributeField(doorlock.Definition.Attribute(doorlock.AttrNumberOfTotalUsersSupported))
		in := encode(t, users, `10`)
		if out := spec.Redact(users, in); !sameNode(in, out) || untyped(t, out) != "10" {
			t.Errorf("numberOfTotalUsersSupported: got %s, want 10", untyped(t, out))
		}
	})
	// SecretRedactionTest.ts:188-191 at db91886c, by meaning as above.
	t.Run("redactAttributes: returns its input for a cluster without secrets", func(t *testing.T) {
		t.Parallel()
		for i := range onoff.Definition.Attributes {
			if f := spec.AttributeField(&onoff.Definition.Attributes[i]); spec.ContainsSecret(f) {
				t.Errorf("OnOff.%s holds a secret", f.Name)
			}
		}
		f := spec.AttributeField(onoff.Definition.Attribute(onoff.AttrOnOff))
		in := encode(t, f, `true`)
		if out := spec.Redact(f, in); !sameNode(in, out) {
			t.Errorf("got a new value %s", untyped(t, out))
		}
	})

	// SecretRedactionTest.ts:194-210 at db91886c (describe "redactEntry")
	octstr := spec.Type{Name: "octstr", Kind: spec.KindBytes}
	secretList := spec.AttributeField(&spec.Attribute{
		ID: 0, Name: "SecretList", Secret: true,
		Type: spec.Type{Name: "list", Kind: spec.KindList, Entry: &spec.Field{Name: "entry", Type: octstr}},
	})
	listOfSecrets := spec.AttributeField(&spec.Attribute{
		ID: 1, Name: "ListOfSecrets",
		Type: spec.Type{Name: "list", Kind: spec.KindList, Entry: &spec.Field{Name: "entry", Type: octstr, Secret: true}},
	})
	plainList := spec.AttributeField(&spec.Attribute{
		ID: 2, Name: "PlainList",
		Type: spec.Type{Name: "list", Kind: spec.KindList, Entry: &spec.Field{Name: "entry", Type: octstr}},
	})
	entry := func(t *testing.T) spec.Node {
		t.Helper()
		return encode(t, &spec.Field{Name: "entry", Type: octstr}, `"hex:01"`)
	}

	// SecretRedactionTest.ts:212-216 at db91886c
	t.Run("redactEntry: replaces an entry of a secret list", func(t *testing.T) {
		t.Parallel()
		if got := untyped(t, spec.RedactEntry(secretList, entry(t))); got != p {
			t.Errorf("got %s, want %s", got, p)
		}
	})
	// SecretRedactionTest.ts:218-222 at db91886c
	t.Run("redactEntry: replaces a secret entry", func(t *testing.T) {
		t.Parallel()
		if got := untyped(t, spec.RedactEntry(listOfSecrets, entry(t))); got != p {
			t.Errorf("got %s, want %s", got, p)
		}
	})
	// SecretRedactionTest.ts:224-226 at db91886c
	t.Run("redactEntry: keeps a null entry of a secret list", func(t *testing.T) {
		t.Parallel()
		null := encode(t, nil, `null`)
		if got := untyped(t, spec.RedactEntry(secretList, null)); got != "null" {
			t.Errorf("got %s, want null", got)
		}
	})
	// SecretRedactionTest.ts:239-242 at db91886c
	t.Run("redactEntry: keeps an entry of a list without secrets", func(t *testing.T) {
		t.Parallel()
		in := entry(t)
		if out := spec.RedactEntry(plainList, in); !reflect.DeepEqual(in, out) {
			t.Errorf("got %s", untyped(t, out))
		}
	})
}

// markedDefinitions are the generated definitions of every cluster the
// snapshot marks a secret in, by name.
var markedDefinitions = map[string]*spec.Cluster{}

func init() {
	for _, d := range []*spec.Cluster{
		accountlogin.Definition, admincomm.Definition, commctrl.Definition, contentcontrol.Definition,
		doorlock.Definition, gendiag.Definition, groupcast.Definition, gkm.Definition, icdm.Definition,
		jfadmin.Definition, jfds.Definition, netcomm.Definition, opcreds.Definition, tbrm.Definition,
		tnd.Definition, webrtcp.Definition, webrtcr.Definition, wifinm.Definition,
	} {
		markedDefinitions[d.Name] = d
	}
}

// TestGeneratedSecretMarksMatchSnapshot holds the generated definitions'
// Secret marks to the snapshot's isSecret marks, cluster by cluster: the
// 41 marks of parity/schema.json at b25ed960, the mark on
// WebRtcTransportDefinitions' ICEServerStruct counted in each cluster that
// uses the struct. A mark is named by where it stands: "attr <id>", "cmd
// <id> <direction>.<field id>", "event <id>.<field id>", "<struct>.<field
// id>" for a datatype's field, "[]" for a list's entry.
func TestGeneratedSecretMarksMatchSnapshot(t *testing.T) {
	t.Parallel()
	var snap struct {
		Clusters    []map[string]any `json:"clusters"`
		Definitions []map[string]any `json:"definitions"`
	}
	if err := json.Unmarshal(parity.SchemaJSON(), &snap); err != nil {
		t.Fatal(err)
	}
	total := 0
	// The marks of each datatype, by "Owner.Name", for the clusters that
	// use another's datatype.
	datatypeMarks := map[string][]string{}
	for _, c := range append(slices.Clone(snap.Clusters), snap.Definitions...) {
		for _, d := range list(c["datatypes"]) {
			name := str(d["name"])
			var marks []string
			for _, f := range list(d["fields"]) {
				walkSnapshotValue(t, fmt.Sprintf("%s.%v", name, f["id"]), f, &marks)
			}
			total += len(marks)
			datatypeMarks[str(c["name"])+"."+name] = marks
		}
	}
	foreign := regexp.MustCompile(`"type":"([A-Za-z]+\.[A-Za-z]+)"`)
	want := map[string][]string{}
	for _, c := range snap.Clusters {
		name := str(c["name"])
		var marks []string
		for _, a := range list(c["attributes"]) {
			eff := obj(a["effective"])
			if len(list(eff["fields"])) > 0 && strings.Contains(fmt.Sprint(eff["fields"]), "isSecret") {
				t.Errorf("%s.%s: a mark on an attribute's inline struct field — extend this test", name, a["name"])
			}
			walkSnapshotValue(t, fmt.Sprintf("attr %v", a["id"]), eff, &marks)
		}
		for _, cmd := range list(c["commands"]) {
			eff := obj(cmd["effective"])
			for _, f := range list(eff["fields"]) {
				walkSnapshotValue(t, fmt.Sprintf("cmd %v %s.%v", cmd["id"], str(eff["direction"]), f["id"]), f, &marks)
			}
		}
		for _, ev := range list(c["events"]) {
			for _, f := range list(obj(ev["effective"])["fields"]) {
				walkSnapshotValue(t, fmt.Sprintf("event %v.%v", ev["id"], f["id"]), f, &marks)
			}
		}
		total += len(marks)
		for _, d := range list(c["datatypes"]) {
			marks = append(marks, datatypeMarks[name+"."+str(d["name"])]...)
		}
		raw, err := json.Marshal(c)
		if err != nil {
			t.Fatal(err)
		}
		seen := map[string]bool{}
		for _, m := range foreign.FindAllStringSubmatch(string(raw), -1) {
			if owner, _, _ := strings.Cut(m[1], "."); owner != name && !seen[m[1]] {
				seen[m[1]] = true
				marks = append(marks, datatypeMarks[m[1]]...)
			}
		}
		if len(marks) > 0 {
			slices.Sort(marks)
			want[name] = marks
		}
	}
	if total != 41 {
		t.Errorf("the snapshot carries %d isSecret marks, want the 41 of b25ed960", total)
	}
	for name := range want {
		if markedDefinitions[name] == nil {
			t.Errorf("%s: the snapshot marks a secret in it; add its definition to markedDefinitions", name)
		}
	}
	for name, def := range markedDefinitions {
		got := generatedMarks(def)
		if !slices.Equal(got, want[name]) {
			t.Errorf("%s: generated marks %q, snapshot %q", name, got, want[name])
		}
	}
}

func walkSnapshotValue(t *testing.T, key string, v map[string]any, marks *[]string) {
	t.Helper()
	if v["isSecret"] == true {
		*marks = append(*marks, key)
	}
	if e := obj(v["entry"]); e != nil {
		walkSnapshotValue(t, key+"[]", e, marks)
	}
}

func generatedMarks(def *spec.Cluster) []string {
	var marks []string
	visited := map[*spec.Struct]bool{}
	var walk func(key string, f *spec.Field)
	walk = func(key string, f *spec.Field) {
		if f.Secret {
			marks = append(marks, key)
		}
		switch f.Type.Kind {
		case spec.KindList:
			if f.Type.Entry != nil {
				walk(key+"[]", f.Type.Entry)
			}
		case spec.KindStruct:
			s := f.Type.Struct
			if s == nil || visited[s] {
				return
			}
			visited[s] = true
			for i := range s.Fields {
				walk(fmt.Sprintf("%s.%d", s.Name, s.Fields[i].ID), &s.Fields[i])
			}
		default:
		}
	}
	for i := range def.Attributes {
		walk(fmt.Sprintf("attr %d", def.Attributes[i].ID), spec.AttributeField(&def.Attributes[i]))
	}
	for i := range def.Commands {
		c := &def.Commands[i]
		dir := "request"
		if c.Direction == spec.Response {
			dir = "response"
		}
		for fi := range c.Fields {
			walk(fmt.Sprintf("cmd %d %s.%d", c.ID, dir, c.Fields[fi].ID), &c.Fields[fi])
		}
	}
	for i := range def.Events {
		e := &def.Events[i]
		for fi := range e.Fields {
			walk(fmt.Sprintf("event %d.%d", e.ID, e.Fields[fi].ID), &e.Fields[fi])
		}
	}
	slices.Sort(marks)
	return slices.Compact(marks)
}

func list(v any) []map[string]any {
	raw, _ := v.([]any)
	out := make([]map[string]any, 0, len(raw))
	for _, x := range raw {
		if m, ok := x.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

func obj(v any) map[string]any { m, _ := v.(map[string]any); return m }

func str(v any) string { s, _ := v.(string); return s }

// TestRedactShapes holds the walk to matter.js's guards for a value whose
// shape is not the model's — SecretRedaction.ts:107-109 (no entry model,
// or not an array) and :114-116 (not an object) at b25ed960: the value
// comes back as it is — and the cases without a model.
func TestRedactShapes(t *testing.T) {
	t.Parallel()
	octstr := spec.Type{Name: "octstr", Kind: spec.KindBytes}
	inner := &spec.Struct{Name: "S", Fields: []spec.Field{{ID: 0, Name: "Key", Type: octstr, Secret: true}}}
	structField := &spec.Field{Name: "s", Type: spec.Type{Name: "S", Kind: spec.KindStruct, Struct: inner}}
	listField := &spec.Field{Name: "l", Type: spec.Type{Name: "list", Kind: spec.KindList, Entry: structField}}
	// A secret-bearing list or struct whose node is another type than its
	// model is redacted whole — fail closed, ahead of matter.js, whose
	// redactValue returns such a value unchanged (review II,
	// R2-C2-sec-1303, owner decision 525).
	scalar := encode(t, nil, `5`)
	key := spec.Node{El: tlv.Element{Tag: tlv.ContextTag(0), Type: tlv.TypeOctetStr1, Octets: []byte{0xAA}}}
	structAsList := spec.Node{El: tlv.Element{Type: tlv.TypeList, IsContainer: true}, Children: []spec.Node{key}}
	listAsStruct := spec.Node{El: tlv.Element{Type: tlv.TypeStructure, IsContainer: true}, Children: []spec.Node{
		{El: tlv.Element{Type: tlv.TypeStructure, IsContainer: true}, Children: []spec.Node{key}},
	}}
	for name, c := range map[string]struct {
		f *spec.Field
		n spec.Node
	}{
		"a list model over a scalar":        {listField, scalar},
		"a struct model over a scalar":      {structField, scalar},
		"a struct model over a TLV list":    {structField, structAsList},
		"a list model over a TLV structure": {listField, listAsStruct},
	} {
		if out := spec.Redact(c.f, c.n); untyped(t, out) != `"(redacted)"` || out.El.Tag != c.n.El.Tag {
			t.Errorf("%s: got %s, want the node redacted whole under its tag", name, untyped(t, out))
		}
	}
	// A member without a context tag is not a field the model names: kept.
	member := spec.Node{El: tlv.Element{Tag: tlv.AnonymousTag(), Type: tlv.TypeOctetStr1, Octets: []byte{1}}}
	anon := spec.Node{El: tlv.Element{Type: tlv.TypeStructure, IsContainer: true}, Children: []spec.Node{member}}
	if out := spec.Redact(structField, anon); len(out.Children) != 1 || !reflect.DeepEqual(out.Children[0], member) {
		t.Errorf("anonymous member: got %+v", out.Children)
	}
	if spec.ContainsSecret(nil) {
		t.Error("ContainsSecret(nil) = true")
	}
	if in := encode(t, nil, `"hex:01"`); !reflect.DeepEqual(spec.RedactEntry(nil, in), in) {
		t.Error("RedactEntry without a model changed the entry")
	}
}

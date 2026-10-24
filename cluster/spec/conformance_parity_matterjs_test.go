// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of matter.js
// packages/node/test/behavior/state/validation/conformanceTest.ts at
// 862ad75f: a record's fields judged by their conformance under the
// selected features, the sibling fields, the cluster's other elements and
// its revision (conformance-compiler.ts astToFunction, ValueValidator.ts
// createStructValidator).
//
// matter.js validates a cluster record whose fields the test declares
// (validation-test-utils.ts:73-113 validate); the Go subject is a struct
// value the definition types — the test's fields are the struct "Test" of
// the cluster's attribute "Record", judged by spec.Instance.CheckConformance.
// The generated cases, testValidation over AllTests (:49-1140), run as a
// table over testdata/conformance_matterjs.json: the tree flattened by a
// node script at the pin, one entry per `it` with its describe path, the
// fields, features, selection, record and the error the case expects.
// Conformance text is parsed with matter.js's grammar
// (internal/chipdm.ParseConformance). A record's field is a *any, nil
// when the record leaves it out and a pointer to a nil interface for its
// null, so absence and null are told apart as matter.js tells undefined
// from null. A case expecting an error asserts its kind (the sentinel of
// ConformanceError, EnumValueConformanceError or UnknownEnumValueError)
// and the message matter.js prints after its "Validating Test.<field>: "
// path, which the Go message carries after its own.
package spec_test

import (
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/internal/chipdm"
)

// confField is a Fields entry (validation-test-utils.ts:21-40) or a
// FieldElement child of one.
type confField struct {
	ID          *uint32     `json:"id"`
	Name        string      `json:"name"`
	Type        string      `json:"type"`
	Conformance string      `json:"conformance"`
	Constraint  string      `json:"constraint"`
	Quality     string      `json:"quality"`
	Children    []confField `json:"children"`
}

type confCase struct {
	Path     []string                        `json:"path"`
	Fields   []confField                     `json:"fields"`
	Features []struct{ Name, Title string }  `json:"features"`
	Supports []string                        `json:"supports"`
	Record   map[string]any                  `json:"record"`
	Error    *struct{ Type, Message string } `json:"error"`
}

// specConformance parses text with matter.js's grammar into the tree the
// spec package evaluates.
func specConformance(t *testing.T, text string) spec.Conformance {
	t.Helper()
	if text == "" {
		return spec.Conformance{}
	}
	c, err := chipdm.ParseConformance(text)
	if err != nil {
		t.Fatalf("conformance %q: %v", text, err)
	}
	// The text a message names is matter.js's serialization of the tree
	// (Conformance.toString), as the generated definitions carry it.
	out := toSpecConformance(c)
	out.Text = c.String()
	return out
}

func toSpecConformance(c *chipdm.Conf) spec.Conformance {
	if c == nil {
		return spec.Conformance{}
	}
	out := spec.Conformance{Op: spec.ConformanceOp(c.Op), Rev: c.Rev}
	switch c.Op {
	case "name":
		out.Name = c.Atom
	case "value":
		out.Value = c.Atom
	case "choice":
		out.Choice = &spec.Choice{Name: c.Choice, Num: c.ChoiceNum, OrMore: c.OrMore, OrLess: c.OrLess}
	}
	for _, a := range c.Args {
		out.Args = append(out.Args, toSpecConformance(a))
	}
	return out
}

// rangeOf parses an enum range member's "128 to 191".
func rangeOf(text string) (lo, hi uint64, ok bool) {
	parts := strings.Split(text, " to ")
	if len(parts) != 2 {
		return 0, 0, false
	}
	a, err1 := strconv.ParseUint(strings.TrimSpace(parts[0]), 10, 64)
	b, err2 := strconv.ParseUint(strings.TrimSpace(parts[1]), 10, 64)
	return a, b, err1 == nil && err2 == nil
}

// specField is the definition field of a test field; index is its
// position, the id matter.js assigns one that states none.
func specField(t *testing.T, f confField, index int) spec.Field {
	t.Helper()
	out := spec.Field{ID: uint32(index), Name: f.Name, Conformance: specConformance(t, f.Conformance)}
	if f.ID != nil {
		out.ID = *f.ID
	}
	out.Quality.Nullable = strings.Contains(f.Quality, "X")
	switch f.Type {
	case "int16":
		out.Type = spec.Type{Name: f.Type, Kind: spec.KindInt, Bits: 16}
	case "uint8", "uint16", "uint32":
		bits, _ := strconv.Atoi(strings.TrimPrefix(f.Type, "uint"))
		out.Type = spec.Type{Name: f.Type, Kind: spec.KindUint, Bits: bits}
	case "epoch-s":
		out.Type = spec.Type{Name: f.Type, Kind: spec.KindUint, Bits: 32}
	case "bool":
		out.Type = spec.Type{Name: f.Type, Kind: spec.KindBool}
	case "string":
		out.Type = spec.Type{Name: f.Type, Kind: spec.KindString}
	case "enum8":
		e := &spec.Enum{Name: f.Name, Bits: 8}
		for n, m := range f.Children {
			conf := specConformance(t, m.Conformance)
			if lo, hi, ok := rangeOf(m.Constraint); ok && m.ID == nil {
				e.Ranges = append(e.Ranges, spec.EnumRange{Name: m.Name, Min: lo, Max: hi, Conformance: conf})
				continue
			}
			id := uint64(n)
			if m.ID != nil {
				id = uint64(*m.ID)
			}
			e.Values = append(e.Values, spec.EnumValue{Value: id, Name: m.Name, Conformance: conf})
		}
		out.Type = spec.Type{Name: f.Type, Kind: spec.KindEnum, Bits: 8, Enum: e}
	case "struct":
		s := &spec.Struct{Name: f.Name}
		for n, m := range f.Children {
			s.Fields = append(s.Fields, specField(t, m, n))
		}
		out.Type = spec.Type{Name: f.Name, Kind: spec.KindStruct, Struct: s}
	default:
		t.Fatalf("field %s: type %q has no Go form here", f.Name, f.Type)
	}
	return out
}

var anyPtr = reflect.TypeFor[*any]()

// recordOf is the Go struct value of record for the struct s: one *any
// per field, named as the generator names it.
func recordOf(t *testing.T, s *spec.Struct, record map[string]any) any {
	t.Helper()
	fields := make([]reflect.StructField, len(s.Fields))
	for i := range s.Fields {
		fields[i] = reflect.StructField{Name: goName(s.Fields[i].Name), Type: anyPtr}
	}
	v := reflect.New(reflect.StructOf(fields)).Elem()
	for key, raw := range record {
		found := false
		for i := range s.Fields {
			f := &s.Fields[i]
			if !strings.EqualFold(goName(f.Name), goName(key)) {
				continue
			}
			found = true
			p := new(any)
			if raw != nil {
				*p = valueOf(t, f, raw)
			}
			v.Field(i).Set(reflect.ValueOf(p))
		}
		if !found {
			t.Fatalf("record key %q names no field of %s", key, s.Name)
		}
	}
	return v.Interface()
}

func valueOf(t *testing.T, f *spec.Field, raw any) any {
	t.Helper()
	switch f.Type.Kind {
	case spec.KindStruct:
		return recordOf(t, f.Type.Struct, raw.(map[string]any))
	case spec.KindInt:
		return int64(raw.(float64))
	case spec.KindUint, spec.KindEnum:
		return uint64(raw.(float64))
	default:
		return raw
	}
}

// goName is the generator's Go name of a specification name (its first
// letter upper-cased, as cluster/spec's goFieldName for these names).
func goName(name string) string {
	if name == "" {
		return name
	}
	return strings.ToUpper(name[:1]) + name[1:]
}

// confCluster is the cluster "Test" the case validates: the features and
// the struct attribute "Record" of the test's fields, plus extra.
func confCluster(t *testing.T, features []struct{ Name, Title string }, fields []confField, extra func(*spec.Cluster)) *spec.Cluster {
	t.Helper()
	s := &spec.Struct{Name: "Test"}
	for i, f := range fields {
		s.Fields = append(s.Fields, specField(t, f, i))
	}
	def := &spec.Cluster{
		ID: 0xFFF1FC01, Name: "Test", Revision: 1,
		Attributes: []spec.Attribute{{ID: 0, Name: "Record", Conformance: spec.Conformance{Op: spec.ConfMandatory}, Type: spec.Type{Name: "Test", Kind: spec.KindStruct, Struct: s}}},
	}
	for i, f := range features {
		def.Features = append(def.Features, spec.Feature{Name: f.Name, Title: f.Title, Bit: uint(i), Conformance: spec.Conformance{Op: spec.ConfOptional}})
	}
	if extra != nil {
		extra(def)
	}
	return def
}

// selection is the FeatureMap of supports: a feature named by its code or
// its camelized title (FeatureSet, validation-test-utils.ts:94).
func selection(def *spec.Cluster, supports []string) uint32 {
	var mask uint32
	for _, want := range supports {
		for i := range def.Features {
			f := &def.Features[i]
			if f.Name == want || strings.EqualFold(f.Title, want) {
				mask |= f.Mask()
			}
		}
	}
	return mask
}

// judge validates record against the cluster's struct, peers resolving
// the cluster's other attributes.
func judge(t *testing.T, def *spec.Cluster, features uint32, record map[string]any, peers spec.Peers) error {
	t.Helper()
	inst, err := spec.New(def, spec.Options{Features: features})
	if err != nil {
		t.Fatal(err)
	}
	return inst.CheckConformance(0, recordOf(t, def.Attributes[0].Type.Struct, record), peers)
}

// expectError holds err to the error a case expects: its kind and the
// message matter.js prints after the record path.
func expectError(t *testing.T, err error, kind, message string) {
	t.Helper()
	sentinel := map[string]error{
		"ConformanceError":          spec.ErrConformance,
		"EnumValueConformanceError": spec.ErrEnumValueConformance,
		"UnknownEnumValueError":     spec.ErrUnknownEnumValue,
	}[kind]
	if sentinel == nil {
		t.Fatalf("unknown error kind %q", kind)
	}
	if !errors.Is(err, sentinel) {
		t.Fatalf("got %v, want %s", err, kind)
	}
	rest := message
	if i := strings.Index(message, ": "); i >= 0 && strings.HasPrefix(message, "Validating ") {
		rest = message[i+2:]
		if path := message[len("Validating "):i]; strings.Contains(path, ".") {
			field := path[strings.Index(path, ".")+1:]
			if !strings.Contains(strings.ToLower(err.Error()), "."+strings.ToLower(field)+":") {
				t.Errorf("%v does not name the field %s", err, field)
			}
		}
	}
	if !strings.Contains(err.Error(), rest) {
		t.Errorf("message %q does not carry %q", err.Error(), rest)
	}
}

func TestConformanceParityMatterJS(t *testing.T) {
	t.Run("conformance", func(t *testing.T) {
		raw, err := os.ReadFile("testdata/conformance_matterjs.json")
		if err != nil {
			t.Fatal(err)
		}
		var table struct {
			Cases []confCase `json:"cases"`
		}
		if err := json.Unmarshal(raw, &table); err != nil {
			t.Fatal(err)
		}
		if len(table.Cases) == 0 {
			t.Fatal("no cases")
		}
		for _, c := range table.Cases {
			// The first element is testValidation's root describe, this
			// subtest.
			t.Run(strings.Join(c.Path[1:], "/"), func(t *testing.T) {
				def := confCluster(t, c.Features, c.Fields, nil)
				err := judge(t, def, selection(def, c.Supports), c.Record, nil)
				if c.Error == nil {
					if err != nil {
						t.Fatalf("refused: %v", err)
					}
					return
				}
				expectError(t, err, c.Error.Type, c.Error.Message)
			})
		}
	})

	t.Run("outerResolve", func(t *testing.T) {
		// Dependent's conformance "ExtValue" names no sibling: the Go
		// counterpart of outerResolve is the cluster's attribute ExtValue,
		// read through peers.
		def := confCluster(t, nil, []confField{{Name: "Dependent", Type: "uint8", Conformance: "ExtValue"}}, func(def *spec.Cluster) {
			def.Attributes = append(def.Attributes, spec.Attribute{ID: 1, Name: "ExtValue", Type: spec.Type{Kind: spec.KindUint, Bits: 8}, Conformance: spec.Conformance{Op: spec.ConfOptional}})
		})
		extValue := func(v any, ok bool) spec.Peers {
			return func(attrID uint32) (any, bool) {
				if attrID == 1 {
					return v, ok
				}
				return nil, false
			}
		}
		t.Run("resolves name via outerResolve when not in siblings", func(t *testing.T) {
			if err := judge(t, def, 0, map[string]any{"dependent": 42.0}, extValue(uint8(1), true)); err != nil {
				t.Fatal(err)
			}
		})
		t.Run("requires field when outerResolve returns truthy", func(t *testing.T) {
			if err := judge(t, def, 0, map[string]any{}, extValue(uint8(1), true)); !errors.Is(err, spec.ErrConformance) {
				t.Fatalf("got %v, want ErrConformance", err)
			}
		})
		t.Run("disallows field when outerResolve returns undefined", func(t *testing.T) {
			if err := judge(t, def, 0, map[string]any{"dependent": 42.0}, extValue(nil, false)); !errors.Is(err, spec.ErrConformance) {
				t.Fatalf("got %v, want ErrConformance", err)
			}
		})
		t.Run("allows omission when outerResolve returns undefined", func(t *testing.T) {
			if err := judge(t, def, 0, map[string]any{}, extValue(nil, false)); err != nil {
				t.Fatal(err)
			}
		})
		t.Run("allows omission without outerResolve", func(t *testing.T) {
			if err := judge(t, def, 0, map[string]any{}, nil); err != nil {
				t.Fatal(err)
			}
		})
	})

	t.Run("element references", func(t *testing.T) {
		// A conformance naming a command is conformant: element conformance
		// is structural (spec.New), not the value check's.
		def := confCluster(t, nil, []confField{{Name: "Dependent", Type: "uint8", Conformance: "Pause | Resume"}}, func(def *spec.Cluster) {
			def.Commands = []spec.Command{
				{ID: 1, Name: "Pause", Direction: spec.Request, Response: "status"},
				{ID: 2, Name: "Resume", Direction: spec.Request, Response: "status"},
			}
		})
		t.Run("accepts field when conformance references cluster element", func(t *testing.T) {
			if err := judge(t, def, 0, map[string]any{"dependent": 42.0}, nil); err != nil {
				t.Fatal(err)
			}
		})
		t.Run("requires field when conformance references cluster element", func(t *testing.T) {
			if err := judge(t, def, 0, map[string]any{}, nil); !errors.Is(err, spec.ErrConformance) {
				t.Fatalf("got %v, want ErrConformance", err)
			}
		})
	})

	t.Run("operational extension", func(t *testing.T) {
		const reason = "conformanceTest.ts:1216-1264: ClusterModel.extend has no Go subject — the generated definitions do not extend one another"
		for _, title := range []string{
			"disallows a field its base disallows",
			"disallows a field its base disallows when the field is overridden",
			"disallows an enum value its base disallows when the member is overridden",
			"treats a field its base declares nullable as nullable",
			"names the conformance it judged",
		} {
			t.Run(title, func(t *testing.T) { t.Skip(reason) })
		}
	})

	t.Run("revision", func(t *testing.T) {
		managerFor := func(revision uint16, conformance string) *spec.Cluster {
			def := confCluster(t, nil, []confField{{Name: "Dependent", Type: "uint8", Conformance: conformance}}, nil)
			def.Revision = revision
			return def
		}
		accepts := func(t *testing.T, def *spec.Cluster, record map[string]any) {
			t.Helper()
			if err := judge(t, def, 0, record, nil); err != nil {
				t.Fatalf("%v refused: %v", record, err)
			}
		}
		refuses := func(t *testing.T, def *spec.Cluster, record map[string]any) {
			t.Helper()
			if err := judge(t, def, 0, record, nil); !errors.Is(err, spec.ErrConformance) {
				t.Fatalf("%v: %v, want ErrConformance", record, err)
			}
		}
		t.Run("requires field when revision satisfies mandatory condition", func(t *testing.T) {
			def := managerFor(2, "Rev >= v2")
			refuses(t, def, map[string]any{})
			accepts(t, def, map[string]any{"dependent": 42.0})
		})
		t.Run("disallows field when revision below mandatory condition", func(t *testing.T) {
			def := managerFor(1, "Rev >= v2")
			refuses(t, def, map[string]any{"dependent": 42.0})
			accepts(t, def, map[string]any{})
		})
		t.Run("allows field when revision satisfies optional condition", func(t *testing.T) {
			def := managerFor(2, "[Rev >= v2]")
			accepts(t, def, map[string]any{"dependent": 42.0})
			accepts(t, def, map[string]any{})
		})
		t.Run("disallows field when revision below optional condition", func(t *testing.T) {
			def := managerFor(1, "[Rev >= v2]")
			refuses(t, def, map[string]any{"dependent": 42.0})
			accepts(t, def, map[string]any{})
		})
	})
}

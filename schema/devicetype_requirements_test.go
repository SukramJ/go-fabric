// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package schema_test

import (
	"encoding/json"
	"fmt"
	"slices"
	"testing"

	"github.com/SukramJ/go-fabric/parity"
	"github.com/SukramJ/go-fabric/schema"
)

// The generated device-type layer (devicetype_requirements_gen.go) against
// the snapshot it is generated from: every device type, every requirement
// node, every conformance AST. A hand edit of the generated file, or a
// generator that drops or misrenders a field, fails here.

type goldenReq struct {
	Element     string  `json:"element"`
	Name        string  `json:"name"`
	ID          *uint32 `json:"id"`
	Type        string  `json:"type"`
	Instance    int     `json:"instance"`
	Location    string  `json:"location"`
	Conformance *struct {
		Text string          `json:"text"`
		AST  json.RawMessage `json:"ast"`
	} `json:"conformance"`
	Constraint *struct {
		Text string `json:"text"`
	} `json:"constraint"`
	Quality  map[string]bool `json:"quality"`
	Referent *struct {
		ID          *uint32 `json:"id"`
		Bit         *uint32 `json:"bit"`
		Name        string  `json:"name"`
		Command     string  `json:"command"`
		Declarer    string  `json:"declarer"`
		Provisional bool    `json:"provisional"`
	} `json:"referent"`
	Requirements []goldenReq `json:"requirements"`
}

type goldenDT struct {
	ID             uint32 `json:"id"`
	Name           string `json:"name"`
	Classification string `json:"classification"`
	Revision       uint16 `json:"revision"`
	Effective      struct {
		Composition  string      `json:"composition"`
		Base         string      `json:"base"`
		Conditions   []string    `json:"conditions"`
		Requirements []goldenReq `json:"requirements"`
	} `json:"effective"`
}

func loadGolden(t *testing.T) (dts, bases []goldenDT) {
	t.Helper()
	var f struct {
		DeviceTypes     []goldenDT `json:"deviceTypes"`
		BaseDeviceTypes []goldenDT `json:"baseDeviceTypes"`
	}
	if err := json.Unmarshal(parity.SchemaJSON(), &f); err != nil {
		t.Fatal(err)
	}
	return f.DeviceTypes, f.BaseDeviceTypes
}

func TestGeneratedDeviceTypeDefinitionsMatchSnapshot(t *testing.T) {
	t.Parallel()
	dts, bases := loadGolden(t)
	for _, want := range dts {
		got, ok := schema.DeviceTypeDefinitionOf(want.ID)
		if !ok {
			t.Errorf("%s (0x%04X): no generated definition", want.Name, want.ID)
			continue
		}
		compareDT(t, want, got)
	}
	if len(bases) != len(schema.BaseDeviceTypes()) {
		t.Fatalf("base device types: snapshot %d, generated %d", len(bases), len(schema.BaseDeviceTypes()))
	}
	for i, want := range bases {
		compareDT(t, want, schema.BaseDeviceTypes()[i])
	}
	if _, ok := schema.DeviceTypeDefinitionOf(0xFFFF); ok {
		t.Error("an unknown device type has a definition")
	}
}

func compareDT(t *testing.T, want goldenDT, got *schema.DeviceTypeDefinition) {
	t.Helper()
	if got.ID != want.ID || got.Name != want.Name || got.Classification != want.Classification ||
		got.Revision != want.Revision || string(got.Composition) != want.Effective.Composition ||
		got.Base != want.Effective.Base || !slices.Equal(got.Conditions, want.Effective.Conditions) {
		t.Errorf("%s: header differs: generated %+v", want.Name, *got)
	}
	compareReqs(t, want.Name, want.Effective.Requirements, got.Requirements)
}

func compareReqs(t *testing.T, path string, want []goldenReq, got []schema.DeviceTypeRequirement) {
	t.Helper()
	if len(want) != len(got) {
		t.Errorf("%s: %d requirements generated, snapshot has %d", path, len(got), len(want))
		return
	}
	for i := range want {
		w, g := &want[i], &got[i]
		p := path + "/" + w.Element + ":" + w.Name
		if string(g.Element) != w.Element || g.Name != w.Name || g.Type != w.Type || g.Instance != w.Instance ||
			string(g.Location) != w.Location || g.Singleton != w.Quality["singleton"] {
			t.Errorf("%s: generated %+v", p, g)
		}
		if (w.ID != nil) != g.HasID || (w.ID != nil && *w.ID != g.ID) {
			t.Errorf("%s: id generated %v/%d", p, g.HasID, g.ID)
		}
		switch {
		case w.Conformance == nil && g.Conformance.Op != "":
			t.Errorf("%s: generated conformance %q, snapshot none", p, g.Conformance.Text)
		case w.Conformance != nil:
			if g.Conformance.Text != w.Conformance.Text {
				t.Errorf("%s: conformance text %q, want %q", p, g.Conformance.Text, w.Conformance.Text)
			}
			if err := sameAST(w.Conformance.AST, g.Conformance); err != nil {
				t.Errorf("%s: conformance AST: %v", p, err)
			}
		}
		if wc := ""; w.Constraint != nil && g.Constraint != w.Constraint.Text || w.Constraint == nil && g.Constraint != wc {
			t.Errorf("%s: constraint %q", p, g.Constraint)
		}
		r := w.Referent
		if r == nil {
			t.Errorf("%s: snapshot requirement resolves to nothing", p)
		} else {
			id := uint32(0)
			switch {
			case r.ID != nil:
				id = *r.ID
			case r.Bit != nil:
				id = *r.Bit
			}
			want := schema.Referent{Resolved: true, ID: id, Name: r.Name, Command: r.Command, Declarer: r.Declarer, Provisional: r.Provisional}
			if g.Referent != want {
				t.Errorf("%s: referent %+v, want %+v", p, g.Referent, want)
			}
		}
		compareReqs(t, p, w.Requirements, g.Requirements)
	}
}

// sameAST compares a matter.js conformance AST (JSON) with its generated
// rendering.
func sameAST(raw json.RawMessage, c schema.Conformance) error {
	var node struct {
		Type  string          `json:"type"`
		Param json.RawMessage `json:"param"`
	}
	if err := json.Unmarshal(raw, &node); err != nil {
		return err
	}
	if node.Type != c.Op {
		return fmt.Errorf("op %q, want %q", c.Op, node.Type)
	}
	args := func(raws ...json.RawMessage) error {
		if len(raws) != len(c.Args) {
			return fmt.Errorf("%s: %d operands, want %d", c.Op, len(c.Args), len(raws))
		}
		for i, r := range raws {
			if err := sameAST(r, c.Args[i]); err != nil {
				return err
			}
		}
		return nil
	}
	switch node.Type {
	case "name":
		var name string
		_ = json.Unmarshal(node.Param, &name)
		if name != c.Name {
			return fmt.Errorf("name %q, want %q", c.Name, name)
		}
	case "value":
		if string(node.Param) != c.Value {
			return fmt.Errorf("value %q, want %s", c.Value, node.Param)
		}
	case "revision":
		var rev int
		_ = json.Unmarshal(node.Param, &rev)
		if rev != c.Rev {
			return fmt.Errorf("revision %d, want %d", c.Rev, rev)
		}
	case "choice":
		var ch struct {
			Name   string          `json:"name"`
			Num    int             `json:"num"`
			OrMore bool            `json:"orMore"`
			OrLess bool            `json:"orLess"`
			Expr   json.RawMessage `json:"expr"`
		}
		_ = json.Unmarshal(node.Param, &ch)
		if c.Choice == nil || *c.Choice != (schema.ConformanceChoice{Name: ch.Name, Num: ch.Num, OrMore: ch.OrMore, OrLess: ch.OrLess}) {
			return fmt.Errorf("choice %+v", c.Choice)
		}
		return args(ch.Expr)
	case "otherwise":
		var terms []json.RawMessage
		_ = json.Unmarshal(node.Param, &terms)
		return args(terms...)
	case "!", "optionalIf":
		return args(node.Param)
	case "==", "!=", "|", "^", "&", ".", ">", "<", ">=", "<=":
		var bin struct{ LHS, RHS json.RawMessage }
		_ = json.Unmarshal(node.Param, &bin)
		return args(bin.LHS, bin.RHS)
	default:
		if len(c.Args) != 0 {
			return fmt.Errorf("%s carries operands", c.Op)
		}
	}
	return nil
}

func TestDeviceTypeConditionScope(t *testing.T) {
	t.Parallel()
	root := schema.DeviceTypeConditionScope(0x0016)
	for _, want := range []string{"CustomNetworkConfig", "GroupcastListenerCond", "Duplicate", "Ethernet"} {
		if !slices.Contains(root, want) {
			t.Errorf("RootNode scope lacks %s: %v", want, root)
		}
	}
	// DimmableLight derives from OnOffLight; neither declares a condition,
	// so the scope is Base's.
	base := schema.ConditionScopeOf(schema.BaseDeviceTypes()[0])
	if got := schema.DeviceTypeConditionScope(0x0101); !slices.Equal(got, base) {
		t.Errorf("DimmableLight scope = %v, want Base's %v", got, base)
	}
	if schema.DeviceTypeConditionScope(0xFFFF) != nil || schema.ConditionScopeOf(nil) != nil {
		t.Error("an unknown device type has a condition scope")
	}
	if got := schema.DeviceTypeConditions(0x000E); !slices.Equal(got, []string{"FabricSynchronization"}) {
		t.Errorf("Aggregator conditions = %v", got)
	}
	if schema.DeviceTypeConditions(0xFFFF) != nil {
		t.Error("an unknown device type declares conditions")
	}
}

func TestDeviceTypeElementRequirements(t *testing.T) {
	t.Parallel()
	got := schema.DeviceTypeElementRequirements(0x0100, 0x0006) // OnOffLight, OnOff
	if len(got) != 1 || got[0].Name != "LT" || got[0].Element != schema.RequirementFeature {
		t.Errorf("OnOffLight OnOff element requirements = %+v, want [LT]", got)
	}
	// Descriptor's DeviceTypeList default requirement is an attribute
	// requirement too.
	if d := schema.DeviceTypeElementRequirements(0x0100, 0x001D); len(d) != 1 || d[0].Name != "DeviceTypeList" {
		t.Errorf("OnOffLight Descriptor element requirements = %+v", d)
	}
	// A component requirement nested under RoboticVacuumCleaner is no
	// element requirement of the cluster.
	if schema.DeviceTypeElementRequirements(0x0100, 0x0406) != nil {
		t.Error("a client-only cluster has server element requirements")
	}
	if schema.DeviceTypeElementRequirements(0xFFFF, 0x0006) != nil {
		t.Error("an unknown device type has element requirements")
	}
	if r, ok := schema.DeviceTypeClusterRequirement(0x0100, 0x0406, schema.RequirementClientCluster); !ok || r.Conformance.Text != "O" {
		t.Errorf("OnOffLight client OccupancySensing = %+v, %v", r, ok)
	}
	if _, ok := schema.DeviceTypeClusterRequirement(0x0100, 0x0101, schema.RequirementServerCluster); ok {
		t.Error("OnOffLight requires DoorLock")
	}
	// Components carry no element requirements of the cluster they name.
	kinds := make([]schema.RequirementElement, 0, 3)
	for _, r := range schema.DeviceTypeElementRequirements(0x0016, 0x001F) { // RootNode, AccessControl
		kinds = append(kinds, r.Element)
	}
	if !slices.Equal(kinds, []schema.RequirementElement{schema.RequirementFeature, schema.RequirementFeature, schema.RequirementAttribute}) {
		t.Errorf("RootNode AccessControl element requirements = %v", kinds)
	}
	// A command-field requirement is not an element requirement.
	for id, dt := range map[uint32]string{0x0016: "RootNode"} {
		for _, r := range schema.DeviceTypeElementRequirements(id, 0x0038) {
			if r.Element == schema.RequirementCommandField {
				t.Errorf("%s: command field listed", dt)
			}
		}
	}
}

func TestClusterFeatureAndClassificationLookups(t *testing.T) {
	t.Parallel()
	if got := schema.ClusterFeatureNames(0x0008, 0b11); !slices.Equal(got, []string{"OO", "LT"}) {
		t.Errorf("LevelControl features of 0b11 = %v", got)
	}
	if schema.ClusterFeatureNames(0x0008, 1<<31) != nil || schema.ClusterFeatures(0xFFFF) != nil {
		t.Error("an undefined bit or cluster yields features")
	}
	if fs := schema.ClusterFeatures(0x0006); len(fs) == 0 || fs[0] != (schema.ClusterFeature{Name: "LT", Bit: 0}) {
		t.Errorf("OnOff features = %v", fs)
	}
	if c, ok := schema.ClusterClassification(0x0028); !ok || c != "node" {
		t.Errorf("BasicInformation classification = %q, %v", c, ok)
	}
	if _, ok := schema.ClusterClassification(0xFFFF); ok {
		t.Error("an unknown cluster is classified")
	}
	if schema.ClusterBindable(0x0029) || !schema.ClusterBindable(0x0006) {
		t.Error("bindability: OtaSoftwareUpdateProvider must be unbindable, OnOff bindable")
	}
}

func TestCountRangeContains(t *testing.T) {
	t.Parallel()
	min2 := schema.CountRange{Set: true, Min: 2, HasMin: true}
	if min2.Contains(1) || !min2.Contains(2) || !min2.Contains(9) {
		t.Error("min 2")
	}
	max1 := schema.CountRange{Set: true, Max: 1, HasMax: true}
	if !max1.Contains(0) || !max1.Contains(1) || max1.Contains(2) {
		t.Error("max 1")
	}
}

// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package surface_test

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	thermostatdef "github.com/SukramJ/go-fabric/cluster/spec/thermostat"
	"github.com/SukramJ/go-fabric/schema"
	"github.com/SukramJ/go-fabric/surface"
)

// set is the LevelSet of the named levels.
func set(ls ...schema.Level) schema.LevelSet {
	var s schema.LevelSet
	for i, l := range schema.Levels() {
		if slices.Contains(ls, l) {
			s |= 1 << i
		}
	}
	return s
}

// all is every level.
func all() schema.LevelSet { return set(schema.Levels()...) }

// decisionOf is the decision of policy on el; false when there is none.
func decisionOf(ds []surface.Decision, el surface.Element, policy string) (surface.Decision, bool) {
	for _, d := range ds {
		if d.Element == el && d.Policy == policy {
			return d, true
		}
	}
	return surface.Decision{}, false
}

func TestPoliciesValidate(t *testing.T) {
	t.Parallel()
	for _, l := range append(schema.Levels(), "") {
		if err := (surface.Policies{Level: l}).Validate(); err != nil {
			t.Errorf("level %q: %v", l, err)
		}
	}
	if err := (surface.Policies{Level: "1.7"}).Validate(); !errors.Is(err, surface.ErrLevelAbovePin) {
		t.Errorf("1.7: %v, want ErrLevelAbovePin", err)
	}
	for _, l := range []schema.Level{"1.5", "1.3", "x"} {
		err := (surface.Policies{Level: l}).Validate()
		if !errors.Is(err, surface.ErrUnknownLevel) || !strings.Contains(err.Error(), "1.4.2") {
			t.Errorf("%s: %v, want ErrUnknownLevel listing the levels", l, err)
		}
	}
	if _, _, err := surface.Evaluate(candidates(), surface.Policies{Level: "1.5"}); !errors.Is(err, surface.ErrUnknownLevel) {
		t.Errorf("Evaluate at an unknown level: %v", err)
	}
	if got := (surface.Policies{}).EffectiveLevel(); got != surface.PinLevel() || got != schema.Levels()[len(schema.Levels())-1] {
		t.Errorf("EffectiveLevel of the zero value = %s", got)
	}
}

func TestPoliciesDigestLevel(t *testing.T) {
	t.Parallel()
	zero := surface.Policies{}.Digest()
	if (surface.Policies{Level: surface.PinLevel()}).Digest() != zero {
		t.Error("the pin's level must digest as the zero value")
	}
	a, b := surface.Policies{Level: "1.4.2"}.Digest(), surface.Policies{Level: "1.5.1"}.Digest()
	if a == zero || b == zero || a == b {
		t.Error("each level below the pin has its own digest")
	}
}

// levelCandidates is a device at a level: an optional feature, attribute,
// command, event and cluster absent at 1.4.2, a mandatory attribute
// absent there, and a known feature.
func levelCandidates() []surface.Element {
	recent := set("1.5.1", "1.6.0", "1.6.1")
	return []surface.Element{
		{Endpoint: "t", Kind: surface.KindDeviceType, ID: 0x0301, Origin: surface.OriginDeviceType, Mandatory: true, Levels: all()},
		{Endpoint: "t", Cluster: 0x0201, Kind: surface.KindCluster, ID: 0x0201, Origin: surface.OriginDeviceType, Mandatory: true, Levels: all()},
		{Endpoint: "t", Cluster: 0x0201, Kind: surface.KindFeature, ID: 0x1, Origin: surface.OriginHostDeclared, Mandatory: true, Levels: all()},
		{Endpoint: "t", Cluster: 0x0201, Kind: surface.KindFeature, ID: 0x200, Origin: surface.OriginHostDeclared, Levels: recent},
		{Endpoint: "t", Cluster: 0x0201, Kind: surface.KindAttribute, ID: 0x48, Origin: surface.OriginHostDeclared, Levels: recent},
		{Endpoint: "t", Cluster: 0x0201, Kind: surface.KindAttribute, ID: 0x49, Origin: surface.OriginClusterMandatory, Mandatory: true, Levels: recent},
		{Endpoint: "t", Cluster: 0x0201, Kind: surface.KindCommand, ID: 0x07, Origin: surface.OriginHostDeclared, Levels: recent},
		{Endpoint: "t", Cluster: 0x0201, Kind: surface.KindEvent, ID: 0x00, Origin: surface.OriginHostDeclared, Levels: recent},
		{Endpoint: "t", Cluster: 0x0039, Kind: surface.KindAttribute, ID: 0x0018, Origin: surface.OriginHostDeclared, Levels: set("1.4.2", "1.5.1", "1.6.0", "1.6.1")},
		{Endpoint: "t", Cluster: 0x0408, Kind: surface.KindCluster, ID: 0x0408, Origin: surface.OriginHostDeclared, Levels: recent},
		{Endpoint: "t", Cluster: 0xFFF1_FC00, Kind: surface.KindCluster, ID: 0xFFF1_FC00, Origin: surface.OriginHostDeclared},
	}
}

func TestLevelPolicyRemovesKeepsAndNamesTheLevel(t *testing.T) {
	t.Parallel()
	cands := levelCandidates()
	kept, ds, err := surface.Evaluate(cands, surface.Policies{Level: "1.4.1"})
	if err != nil {
		t.Fatal(err)
	}
	coversEveryCandidate(t, cands, ds)
	removed := []int{3, 4, 6, 7, 9}
	for i, el := range cands {
		in := slices.Contains(kept, el)
		if slices.Contains(removed, i) == in {
			t.Errorf("%s kept = %v", el, in)
		}
	}
	if d, ok := decisionOf(ds, cands[3], "level"); !ok || d.Verdict != surface.Remove || d.Reason != "absent at level 1.4.1" {
		t.Errorf("optional feature: %+v", d)
	}
	if d, ok := decisionOf(ds, cands[5], "level"); !ok || d.Verdict != surface.Keep || d.Reason != "kept: mandatory at pin (absent at level 1.4.1)" {
		t.Errorf("mandatory attribute: %+v", d)
	}
	if d, ok := decisionOf(ds, cands[8], "level"); !ok || d.Verdict != surface.Keep || d.Reason != surface.ReasonSurfaceChangeSignal {
		t.Errorf("ConfigurationVersion on BridgedDeviceBasicInformation: %+v", d)
	}
	if _, ok := decisionOf(ds, cands[10], "level"); ok {
		t.Error("an element no level knows is not level-controlled")
	}
	// Negative control: at the pin, and with the zero value, nothing is
	// removed and the level policy says nothing.
	for _, p := range []surface.Policies{{}, {Level: surface.PinLevel()}} {
		kept, ds, err := surface.Evaluate(cands, p)
		if err != nil || len(kept) != len(cands) {
			t.Fatalf("at the pin: kept %d of %d, %v", len(kept), len(cands), err)
		}
		for _, d := range ds {
			if d.Policy == "level" {
				t.Errorf("a level decision at the pin: %+v", d)
			}
		}
	}
}

func TestLevelPolicyWithholds(t *testing.T) {
	t.Parallel()
	cands := levelCandidates()
	cands[0].Levels = set("1.5.1", "1.6.0", "1.6.1") // the device type
	cands[1].Levels = cands[0].Levels                // its mandated cluster
	kept, ds, err := surface.Evaluate(cands, surface.Policies{Level: "1.4.2"})
	var w *surface.WithheldError
	if !errors.As(err, &w) || !errors.Is(err, surface.ErrWithheld) {
		t.Fatalf("err = %v, want a *WithheldError", err)
	}
	if kept != nil || len(ds) == 0 || w.Endpoint != "t" || len(w.Elements) != 2 || w.Elements[0] != cands[0] || w.Elements[1] != cands[1] {
		t.Fatalf("withheld %+v, kept %v, %d decisions", w, kept, len(ds))
	}
	if w.Report.Level != "1.4.2" || len(w.Report.Withheld) != 2 || len(w.Report.Decisions) != len(ds) {
		t.Errorf("report %+v", w.Report)
	}
	if d, ok := decisionOf(ds, cands[0], "level"); !ok || d.Reason != "withheld: absent at level 1.4.2" {
		t.Errorf("device type decision %+v", d)
	}
	if !strings.Contains(err.Error(), `endpoint "t"`) || !strings.Contains(err.Error(), "1.4.2") {
		t.Errorf("error text %q", err)
	}
	// A refusal wins over a withhold.
	cands[2].Reference.Ahead = "BD-x"
	if _, _, err := surface.Evaluate(cands, surface.Policies{Level: "1.4.2"}); !errors.Is(err, surface.ErrRefused) || errors.Is(err, surface.ErrWithheld) {
		t.Errorf("refuse and withhold: %v", err)
	}
}

// TestLevelPolicyFeatureSet: Thermostat's HEAT and COOL are "O.a+" — one
// of them at least. Both absent at a level (set here), neither mandatory
// alone: the set check re-adds HEAT, the smaller bit.
func TestLevelPolicyFeatureSet(t *testing.T) {
	t.Parallel()
	_ = thermostatdef.ClusterID // registers the definition
	recent := set("1.6.1")
	heat := surface.Element{Endpoint: "t", Cluster: 0x0201, Kind: surface.KindFeature, ID: 0x1, Origin: surface.OriginHostDeclared, Levels: recent}
	cool := surface.Element{Endpoint: "t", Cluster: 0x0201, Kind: surface.KindFeature, ID: 0x2, Origin: surface.OriginHostDeclared, Levels: recent}
	kept, ds, err := surface.Evaluate([]surface.Element{heat, cool}, surface.Policies{Level: "1.4"})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(kept, []surface.Element{heat}) {
		t.Fatalf("kept %v, want HEAT", kept)
	}
	if d, _ := decisionOf(ds, heat, "level"); d.Reason != surface.ReasonFeatureSetInvalid {
		t.Errorf("HEAT %+v", d)
	}
	// A selection no re-adding repairs withholds: feature A of a
	// synthetic cluster is mandatory and not selected.
	const id = 0xFFF1_FC01
	spec.Register(&spec.Cluster{ID: id, Name: "Synthetic", Features: []spec.Feature{
		{Name: "A", Bit: 0, Conformance: spec.Conformance{Op: spec.ConfMandatory}},
		{Name: "B", Bit: 1, Conformance: spec.Conformance{Op: spec.ConfOptional}},
	}})
	b := surface.Element{Endpoint: "s", Cluster: id, Kind: surface.KindFeature, ID: 0x2, Origin: surface.OriginHostDeclared, Levels: recent}
	_, _, err = surface.Evaluate([]surface.Element{b}, surface.Policies{Level: "1.4"})
	var w *surface.WithheldError
	if !errors.As(err, &w) || len(w.Elements) != 1 || w.Elements[0].Kind != surface.KindCluster || w.Elements[0].ID != id {
		t.Fatalf("err = %v", err)
	}
}

func TestServerInternal(t *testing.T) {
	t.Parallel()
	ep := surface.Endpoint{Key: "t", Clusters: []surface.Cluster{{
		ID: 0x0201, FeatureMap: 0x1 | 0x8 | 0x200,
		Attributes: []uint32{0x0000, 0x0053, 0xFFFD}, Accepted: []uint32{0x00, 0x07}, Generated: []uint32{0x07}, Events: []uint32{0x00},
	}}}
	decided := []surface.Element{{Endpoint: "t", Cluster: 0x0201, Kind: surface.KindFeature, ID: 0x200}}
	if got := (surface.Policies{}).ServerInternal(ep, decided); got != nil {
		t.Fatalf("at the pin: %v", got)
	}
	got := (surface.Policies{Level: "1.4.2"}).ServerInternal(ep, decided)
	var names []string
	for _, d := range got {
		if d.Policy != "level" || d.Verdict != surface.Keep || d.Reason != surface.ReasonServerInternal {
			t.Errorf("decision %+v", d)
		}
		names = append(names, d.Kind.String())
	}
	// TEVT (0x200) is decided; the attribute 0x53, the command 0x07 (once)
	// and the event 0x00 are 1.6.0 and later (parity/levels, measured).
	if want := []string{"attribute", "command", "event"}; !slices.Equal(names, want) {
		t.Errorf("server-internal kinds %v, want %v", names, want)
	}
	if surface.ElementLevels(surface.Element{Kind: surface.KindFeature, Cluster: 0x0201, ID: 0x8}) != set("1.4", "1.4.1", "1.4.2") {
		t.Error("SCH's levels")
	}
	for _, el := range []surface.Element{
		{Kind: surface.KindDeviceType, ID: 0x0301},
		{Kind: surface.KindCluster, ID: 0x0201},
		{Kind: surface.KindAttribute, Cluster: 0x0201, ID: 0},
		{Kind: surface.KindCommand, Cluster: 0x0201, ID: 0},
		{Kind: surface.KindEvent, Cluster: 0x0201, ID: 0},
	} {
		if surface.ElementLevels(el) == 0 {
			t.Errorf("%s has no levels", el)
		}
	}
	if surface.ElementLevels(surface.Element{Kind: surface.KindFeature, Cluster: 0x0201}) != 0 || surface.ElementLevels(surface.Element{}) != 0 {
		t.Error("no levels for no feature and no kind")
	}
}

func TestReportLevelJSON(t *testing.T) {
	t.Parallel()
	r := surface.Report{Level: "1.4.2"}
	b, err := json.Marshal(r)
	if err != nil || !strings.Contains(string(b), `"level":"1.4.2"`) {
		t.Fatalf("%s, %v", b, err)
	}
	var back surface.Report
	if err := json.Unmarshal(b, &back); err != nil || back.Level != "1.4.2" {
		t.Fatalf("%+v, %v", back, err)
	}
	if r.Clone().Level != "1.4.2" {
		t.Error("Clone drops the level")
	}
}

// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package endpoint

import (
	"slices"
	"testing"

	"github.com/SukramJ/go-fabric/schema"
)

// Parity with matter.js's own device type validation tests,
// packages/model/test/logic/device-types/DeviceTypeConformanceTest.ts at the
// schema pin: the same synthetic models (fixtureModel,
// checkCountFixtureModel, perInstanceFixtureModel, baseServersModel), the
// same trees, the same verdicts — kind, requirement path and detail text —
// from this module's port of DeviceTypeConformance.check.

const (
	rootID      = 0x16
	lightID     = 0xfff1_0001
	composerID  = 0xfff1_0002
	composer2ID = 0xfff1_0003
	composer3ID = 0xfff1_0004
	composer4ID = 0xfff1_0005
	composer5ID = 0xfff1_0006
	plugID      = 0xfff1_0007
	composer6ID = 0xfff1_0008
	singletonID = 0x7ff0
	gate        = "Gated"
)

// fixtureModel: Light requires OnOff with the LT feature under lighting and
// the Pending attribute (M), which OnOff itself defines under pending;
// Composer requires at least two Lights; RootNode declares Singleton a
// singleton.
func fixtureModel(lighting schema.Conformance, pendingProvisional bool) *dtLookups {
	singleton := clusterReq("Singleton", singletonID, confO())
	singleton.Singleton = true
	pending := schema.DeviceTypeRequirement{
		Element: schema.RequirementAttribute, Name: "Pending", Conformance: confM(),
		Referent: schema.Referent{Resolved: true, ID: 0x7ff0, Name: "Pending", Provisional: pendingProvisional},
	}
	return syntheticModel(
		&schema.DeviceTypeDefinition{Name: "Base", Classification: "base", Conditions: []string{"CustomNetworkConfig"}},
		&schema.DeviceTypeDefinition{ID: rootID, Name: "RootNode", Classification: "node", Requirements: []schema.DeviceTypeRequirement{singleton}},
		&schema.DeviceTypeDefinition{
			ID: lightID, Name: "Light", Classification: "simple", Conditions: []string{"Wanted"},
			Requirements: []schema.DeviceTypeRequirement{clusterReq("OnOff", 6, confM(), featureReq("LT", 0, lighting), pending)},
		},
		&schema.DeviceTypeDefinition{
			ID: composerID, Name: "Composer", Classification: "simple",
			Requirements: []schema.DeviceTypeRequirement{componentReq("Light", lightID, confM(), minCount(2), 0)},
		},
	)
}

// "reports a mandatory server cluster that is missing and accepts it when
// present"
func TestParityMatterJSMissingServerCluster(t *testing.T) {
	t.Parallel()
	l := fixtureModel(confO(), true)
	f := newFakeTree()
	root := f.add("root", nil, rootID)
	bare := f.add("bare", root, lightID)
	equipped := f.add("equipped", root, lightID).server(6)
	wantKinds(t, f.check(l, bare), "missing OnOff")
	wantKinds(t, f.check(l, equipped))
}

// "reports a feature a feature term disallows but not one only a condition
// disallows"
func TestParityMatterJSFeatureDisallowedByFeatureNotCondition(t *testing.T) {
	t.Parallel()
	byFeature := fixtureModel(confName("OFFONLY"), true)
	f := newFakeTree()
	light := f.add("light", f.add("root", nil, rootID), lightID).server(6, "LT")
	wantKinds(t, f.check(byFeature, light), "disallowed OnOff.LT")

	byCondition := fixtureModel(confOr("Wanted | OFFONLY", confName("Wanted"), confName("OFFONLY")), true)
	wantKinds(t, f.check(byCondition, light))
}

// "does not report a mandatory element its own definition marks
// provisional as missing"
func TestParityMatterJSProvisionalElementNeverMissing(t *testing.T) {
	t.Parallel()
	f := newFakeTree()
	light := f.add("light", f.add("root", nil, rootID), lightID).server(6)
	wantKinds(t, f.check(fixtureModel(confO(), true), light))

	optional := fixtureModel(confO(), false)
	got := f.check(optional, light)
	wantKinds(t, got, "missing OnOff.Pending")
	if len(got) == 1 && (got[0].Element != schema.RequirementAttribute || got[0].ElementID != 0x7ff0 || !got[0].HasCluster || got[0].Cluster != 6) {
		t.Errorf("violation locates %+v", got[0])
	}
	light.implements(6, schema.RequirementAttribute, 0x7ff0)
	wantKinds(t, f.check(optional, light))
}

// "reports a component device type with fewer endpoints than its
// constraint requires"
func TestParityMatterJSComponentCount(t *testing.T) {
	t.Parallel()
	l := fixtureModel(confO(), true)
	f := newFakeTree()
	root := f.add("root", nil, rootID)
	lonely := f.add("lonely", root, composerID)
	f.add("light", lonely, lightID).server(6)
	complete := f.add("complete", root, composerID)
	f.add("light1", complete, lightID).server(6)
	f.add("light2", complete, lightID).server(6)
	found := f.check(l, lonely)
	wantKinds(t, found, "instanceCount device:Light")
	if d := detailsOf(found); len(d) == 1 && d[0] != "Component device type Light requires min 2 endpoint(s) in the composition; found 1" {
		t.Errorf("detail = %q", d[0])
	}
	wantKinds(t, f.check(l, complete))
}

// "reports a singleton of the node endpoint on another endpoint of its
// node scope"
func TestParityMatterJSSingletonMisplaced(t *testing.T) {
	t.Parallel()
	l := fixtureModel(confO(), true)
	f := newFakeTree()
	root := f.add("root", nil, rootID).server(singletonID)
	light := f.add("light", root, lightID).server(6).server(singletonID)
	got := f.check(l, light)
	wantKinds(t, got, "singletonMisplaced Singleton")
	if len(got) == 1 && got[0].DeviceType != "RootNode" {
		t.Errorf("declaring device type = %q, want RootNode", got[0].DeviceType)
	}
	wantKinds(t, f.check(l, root))
}

// "takes the node conditions the facts state for every endpoint of the
// node scope"
func TestParityMatterJSNodeConditions(t *testing.T) {
	t.Parallel()
	l := fixtureModel(confO(), true)
	f := newFakeTree()
	root := f.add("root", nil, rootID)
	light := f.add("light", root, lightID).server(6)
	if newDTPass(f, l).collect(root.id).conditionsOf(light.id)[condCustomNetworkConfig] {
		t.Error("CustomNetworkConfig holds without the node stating it")
	}
	f.node = []string{condCustomNetworkConfig}
	if !newDTPass(f, l).collect(root.id).conditionsOf(light.id)[condCustomNetworkConfig] {
		t.Error("CustomNetworkConfig does not hold although the node states it")
	}
}

// checkCountFixtureModel: Composer2 requires a mandatory Light instance and
// a second one gated by Gated, at most one endpoint; Composer3 an optional
// Light, at most one endpoint.
func checkCountFixtureModel() *dtLookups {
	onOff := clusterReq("OnOff", 6, confM())
	return syntheticModel(
		nil,
		&schema.DeviceTypeDefinition{ID: rootID, Name: "RootNode", Classification: "node"},
		&schema.DeviceTypeDefinition{ID: lightID, Name: "Light", Classification: "simple", Requirements: []schema.DeviceTypeRequirement{onOff}},
		&schema.DeviceTypeDefinition{
			ID: composer2ID, Name: "Composer2", Classification: "simple", Conditions: []string{gate},
			Requirements: []schema.DeviceTypeRequirement{
				componentReq("Light", lightID, confM(), schema.CountRange{}, 1),
				componentReq("Light", lightID, confName(gate), maxCount(1), 2),
			},
		},
		&schema.DeviceTypeDefinition{
			ID: composer3ID, Name: "Composer3", Classification: "simple",
			Requirements: []schema.DeviceTypeRequirement{componentReq("Light", lightID, confO(), maxCount(1), 0)},
		},
	)
}

// "applies a component instance's count range only while that instance
// itself applies" and "applies an optional component instance's own count
// range"
func TestParityMatterJSInstanceCountRanges(t *testing.T) {
	t.Parallel()
	l := checkCountFixtureModel()
	f := newFakeTree()
	root := f.add("root", nil, rootID)
	ungated := f.add("ungated", root, composer2ID)
	f.add("light1", ungated, lightID).server(6)
	f.add("light2", ungated, lightID).server(6)
	wantKinds(t, f.check(l, ungated))

	gated := f.add("gated", root, composer2ID).state(gate)
	f.add("light3", gated, lightID).server(6)
	f.add("light4", gated, lightID).server(6)
	found := f.check(l, gated)
	wantKinds(t, found, "instanceCount device:Light")
	if d := detailsOf(found); len(d) == 1 && d[0] != "Component device type Light requires max 1 endpoint(s) in the composition; found 2" {
		t.Errorf("detail = %q", d[0])
	}

	composer3 := f.add("composer3", root, composer3ID)
	f.add("light5", composer3, lightID).server(6)
	f.add("light6", composer3, lightID).server(6)
	wantKinds(t, f.check(l, composer3), "instanceCount device:Light")
}

// perInstanceFixtureModel: Composer4 requires light-conformance Light and
// Gated Plug (with OnOff LT) as one choice; Composer5 an optional Light of
// at least two with LT, a mandatory Light with LT and a third of
// conformance third; Composer6 exactly one of Light and Plug through
// tolerated, optional and out-of-choice instances.
func perInstanceFixtureModel(light schema.Conformance, more bool, third schema.Conformance) *dtLookups {
	onOff := clusterReq("OnOff", 6, confM())
	lighting := func() schema.DeviceTypeRequirement {
		return clusterReq("OnOff", 6, confM(), featureReq("LT", 0, confM()))
	}
	choice := func(c schema.Conformance, name string) schema.Conformance {
		return confChoice(c.Text+"."+name, c, name, more)
	}
	return syntheticModel(
		nil,
		&schema.DeviceTypeDefinition{ID: rootID, Name: "RootNode", Classification: "node"},
		&schema.DeviceTypeDefinition{ID: lightID, Name: "Light", Classification: "simple", Requirements: []schema.DeviceTypeRequirement{onOff}},
		&schema.DeviceTypeDefinition{ID: plugID, Name: "Plug", Classification: "simple", Requirements: []schema.DeviceTypeRequirement{onOff}},
		&schema.DeviceTypeDefinition{
			ID: composer4ID, Name: "Composer4", Classification: "simple", Conditions: []string{gate},
			Requirements: []schema.DeviceTypeRequirement{
				componentReq("Light", lightID, choice(light, "a"), schema.CountRange{}, 0),
				componentReq("Plug", plugID, choice(confName(gate), "a"), schema.CountRange{}, 0, lighting()),
			},
		},
		&schema.DeviceTypeDefinition{
			ID: composer5ID, Name: "Composer5", Classification: "simple", Conditions: []string{gate},
			Requirements: []schema.DeviceTypeRequirement{
				componentReq("Light", lightID, confO(), minCount(2), 1, lighting()),
				componentReq("Light", lightID, confM(), schema.CountRange{}, 2, lighting()),
				componentReq("Light", lightID, third, schema.CountRange{}, 3),
			},
		},
		&schema.DeviceTypeDefinition{
			ID: composer6ID, Name: "Composer6", Classification: "simple", Conditions: []string{gate},
			Requirements: []schema.DeviceTypeRequirement{
				componentReq("Light", lightID, confChoice(gate+".b", confName(gate), "b", false), maxCount(1), 1),
				componentReq("Light", lightID, confChoice("O.b", confO(), "b", false), minCount(2), 2),
				componentReq("Light", lightID, confO(), minCount(3), 3),
				componentReq("Plug", plugID, confChoice("O.b", confO(), "b", false), schema.CountRange{}, 1),
				componentReq("Plug", plugID, confChoice(gate+".b", confName(gate), "b", false), maxCount(1), 2),
			},
		},
	)
}

// "judges a choice only over the members whose own requirement applies"
// and "counts a choice member only a condition decides as a filler that is
// never needed"
func TestParityMatterJSChoices(t *testing.T) {
	t.Parallel()
	l := perInstanceFixtureModel(confO(), false, confName(gate))
	f := newFakeTree()
	root := f.add("root", nil, rootID)

	ungated := f.add("ungated", root, composer4ID)
	f.add("light1", ungated, lightID).server(6)
	f.add("plug1", ungated, plugID).server(6)
	wantKinds(t, f.check(l, ungated))

	gated := f.add("gated", root, composer4ID).state(gate)
	f.add("light2", gated, lightID).server(6)
	f.add("plug2", gated, plugID).server(6, "LT")
	found := f.check(l, gated)
	wantKinds(t, found, "instanceCount device:Light|Plug")
	if d := detailsOf(found); len(d) == 1 && d[0] != "Requires exactly 1 of component device types Light, Plug; found 2" {
		t.Errorf("detail = %q", d[0])
	}

	plugOnly := f.add("plugOnly", root, composer4ID)
	plug3 := f.add("plug3", plugOnly, plugID).server(6)
	wantKinds(t, f.check(l, plugOnly))
	wantKinds(t, f.check(l, plug3))

	empty := f.add("empty", root, composer4ID)
	if d := detailsOf(f.check(l, empty)); !slices.Equal(d, []string{"Requires exactly 1 of component device types Light, Plug; found 0"}) {
		t.Errorf("empty composer = %v", d)
	}
}

// "lets a tolerated member fill an at-least choice and does not judge a
// choice no member of which applies"
func TestParityMatterJSToleratedChoiceMembers(t *testing.T) {
	t.Parallel()
	f := newFakeTree()
	root := f.add("root", nil, rootID)
	plugOnly := f.add("plugOnly", root, composer4ID)
	f.add("plug1", plugOnly, plugID).server(6)
	wantKinds(t, f.check(perInstanceFixtureModel(confO(), true, confName(gate)), plugOnly))

	empty := f.add("empty", root, composer4ID)
	wantKinds(t, f.check(perInstanceFixtureModel(confName(gate), false, confName(gate)), empty))
}

// "judges a choice member by the ranges of its applying choice
// requirements only"
func TestParityMatterJSChoiceRanges(t *testing.T) {
	t.Parallel()
	l := perInstanceFixtureModel(confO(), false, confName(gate))
	f := newFakeTree()
	root := f.add("root", nil, rootID)

	twoLights := f.add("twoLights", root, composer6ID)
	f.add("light1", twoLights, lightID).server(6)
	f.add("light2", twoLights, lightID).server(6)
	if d := detailsOf(f.check(l, twoLights)); !slices.Equal(d, []string{
		"Component device type Light requires min 3 endpoint(s) in the composition; found 2",
	}) {
		t.Errorf("twoLights = %v", d)
	}

	oneLight := f.add("oneLight", root, composer6ID)
	f.add("light3", oneLight, lightID).server(6)
	if d := detailsOf(f.check(l, oneLight)); !slices.Equal(d, []string{
		"Component device type Light requires min 2 endpoint(s) in the composition; found 1",
		"Requires exactly 1 of component device types Light, Plug; found 0",
	}) {
		t.Errorf("oneLight = %v", d)
	}

	twoPlugs := f.add("twoPlugs", root, composer6ID)
	f.add("plug1", twoPlugs, plugID).server(6)
	f.add("plug2", twoPlugs, plugID).server(6)
	wantKinds(t, f.check(l, twoPlugs))
}

// "reports a component endpoint that fills only an instance its
// requirement disallows", "does not report a component endpoint that fills
// an instance only a condition decides" and "applies an optional
// instance's count range only once the component has an endpoint"
func TestParityMatterJSComponentEndpoints(t *testing.T) {
	t.Parallel()
	f := newFakeTree()
	root := f.add("root", nil, rootID)

	disallowing := perInstanceFixtureModel(confO(), false, confX())
	composer := f.add("composer", root, composer5ID).state(gate)
	plain := f.add("plain", composer, lightID).server(6)
	found := f.check(disallowing, plain)
	wantKinds(t, found, "missing device:Composer5/Light")
	if d := detailsOf(found); len(d) == 1 && d[0] != "Endpoint is component Light of Composer5 composer but satisfies none of its instances; instance 1, the closest, fails OnOff.LT" {
		t.Errorf("detail = %q", d[0])
	}

	l := perInstanceFixtureModel(confO(), false, confName(gate))
	ungated := f.add("ungated", root, composer5ID)
	wantKinds(t, f.check(l, f.add("plain2", ungated, lightID).server(6)))
	gated := f.add("gated", root, composer5ID).state(gate)
	wantKinds(t, f.check(l, f.add("gatedPlain", gated, lightID).server(6)))

	empty := f.add("empty", root, composer5ID)
	if d := detailsOf(f.check(l, empty)); !slices.Equal(d, []string{
		"Component device type Light requires min 1 endpoint(s) in the composition; found 0",
	}) {
		t.Errorf("empty = %v", d)
	}
}

// baseServersModel: Base mandates Binding under "Simple & Client", Tagged
// under Duplicate and Networked under CustomNetworkConfig.
func baseServersModel() *dtLookups {
	and := schema.Conformance{Text: "Simple & Client", Op: "&", Args: []schema.Conformance{confName("Simple"), confName("Client")}}
	return syntheticModel(
		&schema.DeviceTypeDefinition{
			Name: "Base", Classification: "base", Conditions: []string{"Simple", "Client", "Duplicate", "CustomNetworkConfig"},
			Requirements: []schema.DeviceTypeRequirement{
				clusterReq("Binding", 0x1e, and),
				clusterReq("Tagged", 0x7ff1, confName("Duplicate")),
				clusterReq("Networked", 0x7ff2, confName("CustomNetworkConfig")),
			},
		},
		&schema.DeviceTypeDefinition{ID: rootID, Name: "RootNode", Classification: "node"},
		&schema.DeviceTypeDefinition{ID: 0xfff1_0010, Name: "Switch", Classification: "simple"},
		&schema.DeviceTypeDefinition{ID: 0xfff1_0011, Name: "Utility", Classification: "utility"},
	)
}

// baseMissing is what check reports missing for Base on ep.
func baseMissing(f *fakeTree, l *dtLookups, ep *fakeEP) []string {
	out := []string{}
	got := f.check(l, ep)
	for i := range got {
		if got[i].DeviceType == "Base" && got[i].Kind == ViolationMissing {
			out = append(out, got[i].Requirement)
		}
	}
	return out
}

// The Base requirement cases of "DeviceTypeConformance.missingBaseServersOf"
// as check reports them.
func TestParityMatterJSBaseRequirements(t *testing.T) {
	t.Parallel()
	l := baseServersModel()
	cases := []struct {
		name  string
		build func(f *fakeTree, root *fakeEP) *fakeEP
		node  []string
		want  []string
	}{
		{"names Binding for a simple endpoint with an application client", func(f *fakeTree, root *fakeEP) *fakeEP {
			return f.add("switch", root, 0xfff1_0010).client(6)
		}, nil, []string{"Binding"}},
		{"names nothing for a simple endpoint that carries Binding", func(f *fakeTree, root *fakeEP) *fakeEP {
			return f.add("switch", root, 0xfff1_0010).server(0x1e).client(6)
		}, nil, []string{}},
		{"names nothing for a simple endpoint whose only client is a utility cluster", func(f *fakeTree, root *fakeEP) *fakeEP {
			return f.add("switch", root, 0xfff1_0010).client(3)
		}, nil, []string{}},
		{"names nothing for a utility endpoint with an application client", func(f *fakeTree, root *fakeEP) *fakeEP {
			return f.add("utility", root, 0xfff1_0011).client(6)
		}, nil, []string{}},
		{"names nothing for an endpoint of a device type the model does not define", func(f *fakeTree, root *fakeEP) *fakeEP {
			return f.add("custom", root, 0xfff1_00ff).client(6).state("Simple")
		}, nil, []string{}},
		{"names a requirement a stated condition makes mandatory", func(f *fakeTree, root *fakeEP) *fakeEP {
			return f.add("utility", root, 0xfff1_0011).client(6).state("Simple")
		}, nil, []string{"Binding"}},
		{"judges a condition siblings decide from the node scope", func(f *fakeTree, root *fakeEP) *fakeEP {
			first := f.add("first", root, 0xfff1_0010)
			f.add("second", root, 0xfff1_0010)
			return first
		}, nil, []string{"Tagged"}},
		{"judges a condition the node's configuration decides", func(f *fakeTree, root *fakeEP) *fakeEP {
			return f.add("switch", root, 0xfff1_0010)
		}, []string{condCustomNetworkConfig}, []string{"Networked"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newFakeTree()
			f.node = tc.node
			target := tc.build(f, f.add("root", nil, rootID))
			if got := baseMissing(f, l, target); !slices.Equal(got, tc.want) {
				t.Errorf("Base missing = %v, want %v", got, tc.want)
			}
		})
	}
	// "counts an extended application server for the Server condition"
	f := newFakeTree()
	target := f.add("switch", f.add("root", nil, rootID), 0xfff1_0010).server(6)
	if !newDTPass(f, l).ownStructuralConditions(target.id)[condServer] {
		t.Error("an application server does not make Server true")
	}
}

// "spells every structural condition as Base declares it": the condition
// names the structural derivation uses are Base's own spellings, so a
// conformance naming them matches.
func TestParityMatterJSStructuralConditionSpelling(t *testing.T) {
	t.Parallel()
	base := schema.BaseDeviceTypes()[0]
	for _, name := range []string{condNode, condApp, condSimple, condDynamic, condComposed, condClient, condServer, condDuplicate, condEthernet, condWiFi, condThread} {
		if !slices.Contains(base.Conditions, name) {
			t.Errorf("Base declares no condition spelled %q", name)
		}
	}
	root, _ := schema.DeviceTypeDefinitionOf(rootID)
	if !slices.Contains(root.Conditions, condCustomNetworkConfig) {
		t.Error("RootNode declares no CustomNetworkConfig")
	}
}

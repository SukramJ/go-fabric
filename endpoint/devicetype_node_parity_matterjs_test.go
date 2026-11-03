// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package endpoint

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/SukramJ/go-fabric/schema"
)

// Parity with matter.js packages/node/test/endpoint/validation/
// DeviceTypeConformanceTest.ts at 862ad75f, the node-level twin of the
// model-level port in devicetype_parity_matterjs_test.go: the same
// endpoints matter.js adds to a MockServerNode, as fake endpoints judged in
// the snapshot's model (standardTree, light, nodeTree) or in the stand-in
// models the TS builds (lightingFeatureModel, ownConformanceModel,
// singletonModel and the inline ones), each subtest named by its it case.
// What the assembler decides — the Binding server Base mandates — is in
// binding_mount_test.go (TestBindingMountParityMatterJS).

// Device types and clusters of the cases below.
const (
	dtRainSensor        = 0x0044
	dtPowerSourceDT     = 0x0011
	clBooleanState      = 0x0045
	evStateChange       = 0x00
	clPowerSourceCl     = 0x002F
	clWiFiDiag          = 0x0036
	foreignerID         = 0xfff1_0010
	nodeAsserterID      = 0xfff1_0020
	unknownDeviceTypeID = 0xfff1_0000
	dtClosurePanel      = 0x0231
)

// rainSensor is a RainSensor with Identify and BooleanState; withStateChange
// adds the StateChange event BooleanState emits.
func rainSensor(f *fakeTree, parent *fakeEP, name string, withStateChange bool) *fakeEP {
	ep := f.add(name, parent, dtRainSensor).server(clDescriptor).server(clIdentify).server(clBooleanState).
		implements(clIdentify, schema.RequirementCommand, cmdTriggerEffect)
	if withStateChange {
		ep.implements(clBooleanState, schema.RequirementEvent, evStateChange)
	}
	return ep
}

// lightSwitch is an OnOffLightSwitch carrying Descriptor, the given
// servers and clients (DeviceTypeConformanceTest.ts:91-98 switchWith).
func lightSwitch(f *fakeTree, parent *fakeEP, name string, servers, clients []uint32) *fakeEP {
	ep := f.add(name, parent, dtOnOffLightSwch).server(clDescriptor)
	for _, s := range servers {
		ep.server(s)
		if s == clIdentify {
			ep.implements(clIdentify, schema.RequirementCommand, cmdTriggerEffect)
		}
	}
	return ep.client(clients...)
}

// lightingFeatureModel is DeviceTypeConformanceTest.ts:183-205: OnOffLight
// requires OnOff with its Lighting feature under conformance, which may
// name OnOff's OFFONLY and OnOffLight's condition Wanted.
func lightingFeatureModel(c schema.Conformance) *dtLookups {
	return syntheticModel(
		&schema.DeviceTypeDefinition{Name: "Base", Classification: "base"},
		&schema.DeviceTypeDefinition{
			ID: dtOnOffLight, Name: "OnOffLight", Classification: "simple", Conditions: []string{"Wanted"},
			Requirements: []schema.DeviceTypeRequirement{clusterReq("OnOff", clOnOff, confM(), featureReq("LT", 0, c))},
		},
	)
}

// singletonModel is DeviceTypeConformanceTest.ts:146-171: RootNode
// (classified node unless rootIsNode is false) declares GroupKeyManagement
// a singleton; BridgedNode (classified node when bridgedNodeIsNode)
// declares Identify one; OnOffLight lists nothing.
func singletonModel(rootIsNode, bridgedNodeIsNode bool) *dtLookups {
	rootClass, bridgedClass := "simple", "utility"
	if rootIsNode {
		rootClass = "node"
	}
	if bridgedNodeIsNode {
		bridgedClass = "node"
	}
	return syntheticModel(
		&schema.DeviceTypeDefinition{Name: "Base", Classification: "base"},
		&schema.DeviceTypeDefinition{ID: rootID, Name: "RootNode", Classification: rootClass, Requirements: []schema.DeviceTypeRequirement{
			singletonReq("GroupKeyManagement", clGroupKeyMgmt, schema.Conformance{}),
		}},
		&schema.DeviceTypeDefinition{ID: dtBridgedNode, Name: "BridgedNode", Classification: bridgedClass, Requirements: []schema.DeviceTypeRequirement{
			singletonReq("Identify", clIdentify, schema.Conformance{}),
		}},
		&schema.DeviceTypeDefinition{ID: dtOnOffLight, Name: "OnOffLight", Classification: "simple"},
	)
}

// singletonsOf is DeviceTypeConformanceTest.ts:173-177
// singletonViolationsOf: the misplaced singletons as [deviceType,
// requirement].
func singletonsOf(vs []DeviceTypeViolation) []string {
	out := []string{}
	for i := range vs {
		if vs[i].Kind == ViolationSingletonMisplaced {
			out = append(out, vs[i].DeviceType+" "+vs[i].Requirement)
		}
	}
	return out
}

// requirementsNamed filters violations by requirement path.
func requirementsNamed(vs []DeviceTypeViolation, requirement string) []DeviceTypeViolation {
	var out []DeviceTypeViolation
	for i := range vs {
		if vs[i].Requirement == requirement {
			out = append(out, vs[i])
		}
	}
	return out
}

func TestDeviceTypeConformanceNodeParityMatterJS(t *testing.T) {
	t.Parallel()
	std := deviceTypeLookups()

	// DeviceTypeConformanceTest.ts:288-297: lightWithoutScenes lacks
	// ScenesManagement, whose nested CopyScene is mandatory.
	t.Run("reports a missing cluster once, not again for its nested requirements", func(t *testing.T) {
		f, root := standardTree()
		ep := f.add("light", root, dtOnOffLight).server(clDescriptor).server(clIdentify).server(clGroups).server(clOnOff, featureOnOffLT).
			implements(clIdentify, schema.RequirementCommand, cmdTriggerEffect)
		wantKinds(t, f.check(std, ep), "missing ScenesManagement")
	})

	// DeviceTypeConformanceTest.ts:299-307: DoorLock states Groups X.
	t.Run("reports a disallowed cluster the endpoint carries", func(t *testing.T) {
		f, root := standardTree()
		lock := f.add("lock", root, dtDoorLock).server(clDescriptor).server(clIdentify).server(clDoorLock).server(clGroups).
			implements(clIdentify, schema.RequirementCommand, cmdTriggerEffect)
		wantKinds(t, f.check(std, lock), "disallowed Groups")
	})

	// DeviceTypeConformanceTest.ts:309-318.
	t.Run("accepts a disallowed cluster the endpoint does not carry", func(t *testing.T) {
		f, root := standardTree()
		lock := f.add("lock", root, dtDoorLock).server(clDescriptor).server(clIdentify).server(clDoorLock).
			implements(clIdentify, schema.RequirementCommand, cmdTriggerEffect)
		wantKinds(t, f.check(std, lock))
	})

	// DeviceTypeConformanceTest.ts:342-354: a feature term disallows in a
	// conjunction with a condition, not in a disjunction.
	t.Run("disallows by a feature term whatever condition it is combined with", func(t *testing.T) {
		f, root := standardTree()
		ep := light(f, root, "light")
		and := schema.Conformance{Text: "Wanted & OFFONLY", Op: "&", Args: []schema.Conformance{confName("Wanted"), confName("OFFONLY")}}
		wantKinds(t, f.check(lightingFeatureModel(and), ep), "disallowed OnOff.LT")
		or := confOr("Wanted | OFFONLY", confName("Wanted"), confName("OFFONLY"))
		wantKinds(t, f.check(lightingFeatureModel(or), ep))
	})

	// DeviceTypeConformanceTest.ts:356-366: OnOff without Lighting, which
	// OnOffLight names by its code LT.
	t.Run("reports a mandatory feature by the feature's code", func(t *testing.T) {
		f, root := standardTree()
		ep := light(f, root, "light").server(clOnOff)
		wantKinds(t, f.check(std, ep), "missing OnOff.LT")
	})

	// DeviceTypeConformanceTest.ts:368-377: lightWithoutTriggerEffect.
	t.Run("reports a mandatory command the cluster does not implement", func(t *testing.T) {
		f, root := standardTree()
		ep := f.add("light", root, dtOnOffLight).server(clDescriptor).server(clIdentify).server(clGroups).
			server(clOnOff, featureOnOffLT).server(clScenes).implements(clScenes, schema.RequirementCommand, cmdCopyScene)
		wantKinds(t, f.check(std, ep), "missing Identify.TriggerEffect")
	})

	// DeviceTypeConformanceTest.ts:388-399 with ownConformanceModel("O")
	// (:212-246): OnOff's own definition marks DF, Pending and Pend
	// optional, not provisional, so OnOffLight's M stands.
	t.Run("reports a mandatory element its own definition marks optional but not provisional as missing", func(t *testing.T) {
		f, root := standardTree()
		ep := light(f, root, "light")
		pending := schema.DeviceTypeRequirement{Element: schema.RequirementAttribute, Name: "Pending", Conformance: confM(), Referent: schema.Referent{Resolved: true, ID: 0x7ff0, Name: "Pending"}}
		pend := schema.DeviceTypeRequirement{Element: schema.RequirementCommand, Name: "Pend", Conformance: confM(), Referent: schema.Referent{Resolved: true, ID: 0x7f, Name: "Pend"}}
		l := syntheticModel(
			&schema.DeviceTypeDefinition{Name: "Base", Classification: "base"},
			&schema.DeviceTypeDefinition{ID: dtOnOffLight, Name: "OnOffLight", Classification: "simple", Requirements: []schema.DeviceTypeRequirement{
				clusterReq("OnOff", clOnOff, confM(), featureReq("DF", 1, confM()), pending, pend),
			}},
		)
		wantKinds(t, f.check(l, ep), "missing OnOff.DF", "missing OnOff.Pending", "missing OnOff.Pend")
	})

	// DeviceTypeConformanceTest.ts:401-411: RainSensor states CHGEVENT
	// "Rev >= v2", which the endpoint's facts cannot decide.
	t.Run("does not report a requirement whose conformance it cannot decide", func(t *testing.T) {
		f, root := standardTree()
		ep := rainSensor(f, root, "rain", false)
		for _, v := range f.check(std, ep) {
			if v.Requirement == "BooleanState.CHGEVENT" {
				t.Errorf("undecidable requirement reported: %v", v)
			}
		}
	})

	// DeviceTypeConformanceTest.ts:413-420.
	t.Run("accepts a mandatory event the cluster emits", func(t *testing.T) {
		f, root := standardTree()
		wantKinds(t, f.check(std, rainSensor(f, root, "rain", true)))
	})

	// DeviceTypeConformanceTest.ts:422-431 and :433-442: matter.js tells an
	// emitter inherited from a base with the feature on from no emitter at
	// all; a Go server lists the events it emits (MatterEvents), so both are
	// a BooleanState that does not list StateChange.
	t.Run("reports a mandatory event whose emitter survives from a base with the feature on", func(t *testing.T) {
		f, root := standardTree()
		wantKinds(t, f.check(std, rainSensor(f, root, "rain", false)), "missing BooleanState.StateChange")
	})
	t.Run("reports a mandatory event the cluster has no emitter for", func(t *testing.T) {
		f, root := standardTree()
		ep := rainSensor(f, root, "rain", false)
		ep.implements(clBooleanState, schema.RequirementEvent, 0x7f)
		wantKinds(t, f.check(std, ep), "missing BooleanState.StateChange")
	})

	// DeviceTypeConformanceTest.ts:444-451: completeSwitch (:101).
	t.Run("accepts mandatory client clusters in the client list", func(t *testing.T) {
		f, root := standardTree()
		wantKinds(t, f.check(std, lightSwitch(f, root, "switch", []uint32{clIdentify, clBinding}, []uint32{clIdentify, clOnOff})))
	})

	// DeviceTypeConformanceTest.ts:453-460: switchWithoutOnOffClient (:103).
	t.Run("reports a mandatory client cluster missing from the client list", func(t *testing.T) {
		f, root := standardTree()
		wantKinds(t, f.check(std, lightSwitch(f, root, "switch", []uint32{clIdentify, clBinding}, []uint32{clIdentify})), "missing client:OnOff")
	})

	// DeviceTypeConformanceTest.ts:462-472: switchWithoutIdentify (:105-111).
	t.Run("reports a cluster missing on both sides once per side", func(t *testing.T) {
		f, root := standardTree()
		wantKinds(t, f.check(std, lightSwitch(f, root, "switch", []uint32{clBinding}, []uint32{clOnOff})), "missing Identify", "missing client:Identify")
	})

	// DeviceTypeConformanceTest.ts:474-486: a light lacking Identify whose
	// DeviceTypeList names only a manufacturer-specific device type.
	t.Run("skips a device type the model does not know", func(t *testing.T) {
		f, root := standardTree()
		ep := lightWithoutIdentify(f, root, "custom")
		ep.deviceTypes = []uint32{unknownDeviceTypeID}
		wantKinds(t, f.check(std, ep))
	})

	// DeviceTypeConformanceTest.ts:488-504: a light listing BatteryStorage,
	// whose ElectricalSensor component requires ElectricalPowerMeasurement;
	// the missing components are reported by count only.
	t.Run("does not report a component's requirements against the composing endpoint", func(t *testing.T) {
		f, root := standardTree()
		ep := standIn(f, root, "battery", dtBatteryStorage)
		for _, v := range f.check(std, ep) {
			if v.Kind != ViolationInstanceCount {
				t.Errorf("component requirement reported on the composer: %v", v)
			}
		}
	})

	// DeviceTypeConformanceTest.ts:690-697.
	t.Run("does not report Binding on an endpoint Base does not require it of", func(t *testing.T) {
		f, root := standardTree()
		wantKinds(t, f.check(std, light(f, root, "light").server(clBinding)))
	})

	// DeviceTypeConformanceTest.ts:699-711: two sibling OnOffLights hold
	// Base's Duplicate condition, under which Descriptor.TAGLIST is required.
	t.Run("requires TagList of endpoints that duplicate a sibling's device type", func(t *testing.T) {
		f, root := nodeTree()
		first, second := light(f, root, "first"), light(f, root, "second")
		for _, ep := range []*fakeEP{first, second} {
			got := f.check(std, ep)
			wantKinds(t, got, "missing Descriptor.TAGLIST")
			if len(got) == 1 && got[0].DeviceType != "Base" {
				t.Errorf("%s: TagList owed to %q, want Base", ep.name, got[0].DeviceType)
			}
		}
		// The TagList feature satisfies it.
		first.server(clDescriptor, "TAGLIST")
		wantKinds(t, f.check(std, first))
	})

	// DeviceTypeConformanceTest.ts:713-748: an Aggregator's children that
	// list BridgedNode are told apart by NodeLabel (matter.js
	// DeviceTypeConformance.ts:1189-1215, AGGREGATED and its waiver);
	// duplicates that do not list it, under the Aggregator or under a
	// composing parent, owe TagList to Base.
	t.Run("requires no TagList of an aggregator's bridged devices, which disambiguate by NodeLabel", func(t *testing.T) {
		f, root := nodeTree()
		agg := aggregatorOf(f, root, "aggregator")
		var bridged []*fakeEP
		for _, name := range []string{"first", "second"} {
			ep := light(f, agg, name, dtBridgedNode).server(clBridgedBasic)
			bridged = append(bridged, ep)
		}
		plain := []*fakeEP{light(f, agg, "third"), light(f, agg, "fourth")}
		parent := standIn(f, root, "parent", dtOnOffLight)
		composed := []*fakeEP{light(f, parent, "first"), light(f, parent, "second")}
		pass := newDTPass(f, std)
		tagList := func(ep *fakeEP) []string {
			var out []string
			for _, v := range requirementsNamed(pass.check(ep.id), "Descriptor.TAGLIST") {
				out = append(out, v.DeviceType)
			}
			return out
		}
		for _, ep := range bridged {
			if !pass.isDuplicate(ep.id) {
				t.Errorf("%s: not a duplicate", ep.name)
			}
			if got := tagList(ep); len(got) != 0 {
				t.Errorf("bridged %s owes TagList to %v", ep.name, got)
			}
		}
		for _, ep := range append(plain, composed...) {
			if got := tagList(ep); !slices.Equal(got, []string{"Base"}) {
				t.Errorf("%s owes TagList to %v, want [Base]", ep.name, got)
			}
		}
	})

	// DeviceTypeConformanceTest.ts:761-781: two siblings listing
	// ClosurePanel, which requires TAGLIST itself; reported once, as
	// ClosurePanel's (matter.js DeviceTypeConformance.ts: a requirement the
	// device type states is judged before Base's of the same path).
	t.Run("reports a requirement Base and a device type both state once, as the device type's", func(t *testing.T) {
		f, root := nodeTree()
		panels := []*fakeEP{standIn(f, root, "panel1", dtClosurePanel), standIn(f, root, "panel2", dtClosurePanel)}
		got := requirementsNamed(f.check(std, panels[0]), "Descriptor.TAGLIST")
		if len(got) != 1 || got[0].DeviceType != "ClosurePanel" || got[0].Kind != ViolationMissing {
			t.Errorf("TagList violations = %+v, want one missing, ClosurePanel's", got)
		}
	})

	// DeviceTypeConformanceTest.ts:750-759: the node endpoint, with a
	// completeSwitch below it, owes neither.
	t.Run("requires neither Binding nor TagList of the node endpoint", func(t *testing.T) {
		f, root := nodeTree()
		lightSwitch(f, root, "switch", []uint32{clIdentify, clBinding}, []uint32{clIdentify, clOnOff})
		for _, v := range f.check(std, root) {
			if v.Requirement == "Binding" || v.Requirement == "Descriptor.TAGLIST" {
				t.Errorf("node endpoint owes %v", v)
			}
		}
	})

	// DeviceTypeConformanceTest.ts:785-791: this module never commissions
	// over BLE, so CustomNetworkConfig holds and RootNode's
	// NetworkCommissioning ("!CustomNetworkConfig") is not required.
	t.Run("does not require NetworkCommissioning of a node that does not commission over BLE", func(t *testing.T) {
		f, root := nodeTree()
		wantKinds(t, f.check(std, root))
	})

	// DeviceTypeConformanceTest.ts:813-819: RootNode states
	// WiFiNetworkDiagnostics "[WiFi]", optional under its condition.
	t.Run("does not require the diagnostics cluster of a supported network interface", func(t *testing.T) {
		f, root := nodeTree()
		root.server(clNetworkComm, "WI")
		f.node = append(f.node, condWiFi)
		wantKinds(t, f.check(std, root))
		if slices.Contains(root.order, clWiFiDiag) {
			t.Fatal("fixture carries WiFiNetworkDiagnostics")
		}
	})

	// DeviceTypeConformanceTest.ts:823-829.
	t.Run("accepts the singletons on the endpoint that declares them", func(t *testing.T) {
		f, root := nodeTree()
		if got := singletonsOf(f.check(std, root)); len(got) != 0 {
			t.Errorf("singletons = %v", got)
		}
	})

	// DeviceTypeConformanceTest.ts:845-855: BridgedNode lists
	// AdministratorCommissioning, a RootNode singleton.
	t.Run("accepts a RootNode singleton on a bridged node whose device type lists it", func(t *testing.T) {
		f, root := nodeTree()
		bridged := f.add("bridged", aggregatorOf(f, root, "aggregator"), dtBridgedNode).server(clDescriptor).server(clBridgedBasic).server(clAdminComm)
		if got := singletonsOf(f.check(std, bridged)); len(got) != 0 {
			t.Errorf("singletons = %v", got)
		}
	})

	// DeviceTypeConformanceTest.ts:857-867: RainSensor does not list OnOff,
	// OnOffLight lists it without the singleton quality.
	t.Run("accepts a cluster that another device type lists as no singleton", func(t *testing.T) {
		f, root := nodeTree()
		light(f, root, "light")
		sensor := rainSensor(f, root, "sensor", true).server(clOnOff)
		if got := singletonsOf(f.check(std, sensor)); len(got) != 0 {
			t.Errorf("singletons = %v", got)
		}
	})

	// DeviceTypeConformanceTest.ts:869-878.
	t.Run("judges no singleton outside a node scope", func(t *testing.T) {
		f, root := syntheticNode()
		ep := standIn(f, root, "light", dtOnOffLight).server(clGroupKeyMgmt)
		if got := singletonsOf(f.check(singletonModel(true, false), ep)); !slices.Equal(got, []string{"RootNode GroupKeyManagement"}) {
			t.Errorf("node scope: singletons = %v", got)
		}
		if got := singletonsOf(f.check(singletonModel(false, false), ep)); len(got) != 0 {
			t.Errorf("no node scope: singletons = %v", got)
		}
	})

	// DeviceTypeConformanceTest.ts:880-895.
	t.Run("keeps a nested node scope's singletons to itself", func(t *testing.T) {
		f, root := syntheticNode()
		bridged := f.add("bridged", f.add("aggregator", root, dtAggregator), dtBridgedNode).server(clGroupKeyMgmt)
		lt := standIn(f, root, "light", dtOnOffLight)
		l := singletonModel(true, true)
		for _, ep := range []*fakeEP{bridged, lt} {
			if got := singletonsOf(f.check(l, ep)); len(got) != 0 {
				t.Errorf("%s: singletons = %v", ep.name, got)
			}
		}
	})

	// DeviceTypeConformanceTest.ts:898-912: strict mode constructs a light
	// carrying GroupKeyManagement below a BridgedNode that is a node.
	t.Run("constructs an endpoint carrying an outer singleton below a nested node endpoint", func(t *testing.T) {
		f, root := syntheticNode()
		bridged := f.add("bridged", f.add("aggregator", root, dtAggregator), dtBridgedNode)
		f.add("light", bridged, dtOnOffLight).server(clDescriptor).server(clGroupKeyMgmt)
		s := newService(t, DeviceTypeValidationStrict, singletonModel(true, true), f)
		if _, err := s.pass(); err != nil {
			t.Fatalf("refused: %v", err)
		}
	})

	// DeviceTypeConformanceTest.ts:914-939: no device type is a node;
	// Asserter asserts Rooted on the root, under which Identify is required.
	t.Run("takes the conditions of the whole tree for an endpoint in no node scope", func(t *testing.T) {
		l := syntheticModel(
			&schema.DeviceTypeDefinition{Name: "Base", Classification: "base"},
			&schema.DeviceTypeDefinition{ID: nodeAsserterID, Name: "Asserter", Classification: "simple", Conditions: []string{"Rooted"}, Requirements: []schema.DeviceTypeRequirement{
				condReq("Asserter", "Rooted", confM()),
				clusterReq("Identify", clIdentify, confName("Rooted")),
			}},
		)
		f, root := syntheticNode()
		asserter := lightWithoutIdentify(f, root, "asserter")
		asserter.deviceTypes = []uint32{nodeAsserterID}
		wantKinds(t, f.check(l, asserter))
	})

	// DeviceTypeConformanceTest.ts:941-966: OnOffLight's Groups requirement
	// names a condition only Foreigner declares; undecided, not disallowed.
	t.Run("does not treat another device type's condition as known to a requirement", func(t *testing.T) {
		l := syntheticModel(
			&schema.DeviceTypeDefinition{Name: "Base", Classification: "base"},
			&schema.DeviceTypeDefinition{ID: foreignerID, Name: "Foreigner", Classification: "simple", Conditions: []string{"Foreign"}},
			&schema.DeviceTypeDefinition{ID: dtOnOffLight, Name: "OnOffLight", Classification: "simple", Requirements: []schema.DeviceTypeRequirement{
				clusterReq("Groups", clGroups, confName("Foreign")),
			}},
		)
		f, root := standardTree()
		wantKinds(t, f.check(l, light(f, root, "light")))
	})

	// DeviceTypeConformanceTest.ts:968-987: the refusal names the endpoint
	// and words each violation "<deviceType> <requirement>: <detail>". The
	// MatterAggregateError/ImplementationError class checks have no Go
	// counterpart; errors.As finds the one error type.
	t.Run("aggregates violation errors", func(t *testing.T) {
		f, root := nodeTree()
		ep := lightWithoutIdentify(f, root, "light")
		s := newService(t, DeviceTypeValidationStrict, std, f)
		_, err := s.pass()
		var refusal *DeviceTypeConformanceError
		if !errors.As(err, &refusal) {
			t.Fatalf("err = %v, want a DeviceTypeConformanceError", err)
		}
		if len(refusal.Violations) != 1 {
			t.Fatalf("violations = %v", refusal.Violations)
		}
		v := refusal.Violations[0]
		if v.Endpoint != ep.id || v.DeviceType != "OnOffLight" || v.Requirement != "Identify" || v.Kind != ViolationMissing || v.Detail != "Mandatory server cluster Identify is missing" {
			t.Errorf("violation = %+v", v)
		}
		if want := "OnOffLight Identify: Mandatory server cluster Identify is missing"; !strings.Contains(err.Error(), want) {
			t.Errorf("error %q lacks %q", err, want)
		}
	})
}

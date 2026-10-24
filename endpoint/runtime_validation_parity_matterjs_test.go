// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of matter.js
// packages/node/test/endpoint/validation/RuntimeValidationTest.ts at
// 862ad75f: device-type validation after construction — a child destroyed,
// a device type list changed, siblings made or unmade duplicates, a
// condition asserted on the node endpoint, a refused addition, a later
// addition in strict mode, off mode.
//
// matter.js follows each endpoint and judges what a runtime change
// affects; the Go validator judges the whole tree once per reassembly
// (bridge Start/Reassemble, ADR 0016), and a runtime change is a new
// snapshot. Each case ports as a verdict: the tree before, the change (the
// fake tree mutated, then one pass), what is logged, what is recorded per
// endpoint and what is refused (endpoint.DeviceTypeValidator through the
// service of devicetype_service_helpers_test.go). matter.js logs one line
// per endpoint with new violations; a line here is the same grouping of
// the verdict's Fresh violations, which the bridge logs one record each
// (matter.devicetype.violation). The synthetic models are the test's,
// built as schema definitions; the standard ones are the snapshot's.
//
// What has no Go subject is skipped with its reason: the counts of judged
// and read endpoints (recordingChecks, recordingReads, NodeScopeIndex) —
// the Go pass is linear in the tree per reassembly and keeps no index —,
// a crashed endpoint (never in a snapshot), a judgement that throws (the
// Go pass cannot fail on a tree), peers (no controller-side validation),
// the observers off mode does not register, a factory reset of a followed
// endpoint, and a singleton a runtime change misplaces (refused in every
// mode, ADR 0016). The default-configuration cases, which need a whole
// node, are node:TestDeviceTypeValidationNodeParityMatterJS.
package endpoint

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/SukramJ/go-fabric/schema"
)

const (
	skipCounting = "counts the endpoints a pass judges or reads (recordingChecks, recordingReads, NodeScopeIndex): the Go validator judges the whole tree once per reassembly (ADR 0016), linear in the tree, and keeps no index"
	skipCrash    = "a crashed endpoint has no Go subject: the assembler builds an endpoint whole or leaves it out of the snapshot"
	skipThrows   = "a throwing judgement has no Go subject: the Go pass cannot fail on a tree, so there is nothing to log in place of a failure"
	skipPeer     = "a peer's endpoints have no Go subject: the controller side runs no device-type validation"
	skipNode     = "needs a whole node: ported as node:TestDeviceTypeValidationNodeParityMatterJS"

	missingTagList   = "missing Descriptor.TAGLIST"
	unguardedWidget  = "missing device:RootNode/Widget"
	widgetID         = 0xfff1_0031
	asserterID       = 0xfff1_0032
	duplicateAsserID = 0xfff1_0033
	declarerID       = 0xfff1_0034
	needyID          = 0xfff1_0035
	wifiAsserterID   = 0xfff1_0036
	composer30ID     = 0xfff1_0030
	dtBatteryStorage = 0x0018
)

var brokenFridge = []string{"instanceCount device:TemperatureControlledCabinet", "instanceCount condition:Cooler"}

// singleComponentModel: Composer allows at most one OnOffLight component
// (RuntimeValidationTest.ts:68-87).
func singleComponentModel() *dtLookups {
	return syntheticModel(
		&schema.DeviceTypeDefinition{Name: "Base", Classification: "base"},
		&schema.DeviceTypeDefinition{ID: rootID, Name: "RootNode", Classification: "node"},
		&schema.DeviceTypeDefinition{ID: composer30ID, Name: "Composer", Classification: "simple", Requirements: []schema.DeviceTypeRequirement{
			componentReq("OnOffLight", dtOnOffLight, confO(), maxCount(1), 0),
		}},
		&schema.DeviceTypeDefinition{ID: dtOnOffLight, Name: "OnOffLight", Classification: "simple"},
	)
}

// guardedRootModel: RootNode requires a Widget carrying ColorControl under
// its condition Guarded; Asserter asserts Guarded on the node endpoint,
// DuplicateAsserter while it is a duplicate; Declarer declares ColorControl
// a singleton (RuntimeValidationTest.ts:99-141).
func guardedRootModel() *dtLookups {
	return syntheticModel(
		&schema.DeviceTypeDefinition{Name: "Base", Classification: "base", Conditions: []string{"Duplicate"}},
		&schema.DeviceTypeDefinition{ID: rootID, Name: "RootNode", Classification: "node", Conditions: []string{"Guarded"}, Requirements: []schema.DeviceTypeRequirement{
			componentReq("Widget", widgetID, confName("Guarded"), schema.CountRange{}, 0, clusterReq("ColorControl", clColorControl, confM())),
		}},
		&schema.DeviceTypeDefinition{ID: widgetID, Name: "Widget", Classification: "simple"},
		&schema.DeviceTypeDefinition{ID: asserterID, Name: "Asserter", Classification: "simple", Requirements: []schema.DeviceTypeRequirement{condReq("RootNode", "Guarded", confM())}},
		&schema.DeviceTypeDefinition{ID: duplicateAsserID, Name: "DuplicateAsserter", Classification: "simple", Requirements: []schema.DeviceTypeRequirement{condReq("RootNode", "Guarded", confName("Duplicate"))}},
		&schema.DeviceTypeDefinition{ID: declarerID, Name: "Declarer", Classification: "simple", Requirements: []schema.DeviceTypeRequirement{singletonReq("ColorControl", clColorControl, confO())}},
		&schema.DeviceTypeDefinition{ID: dtOnOffLight, Name: "OnOffLight", Classification: "simple"},
	)
}

// wiFiGuardedRootModel: as guardedRootModel, with WiFiAsserter asserting
// Guarded while the node supports Wi-Fi (RuntimeValidationTest.ts:151-179).
func wiFiGuardedRootModel() *dtLookups {
	return syntheticModel(
		&schema.DeviceTypeDefinition{Name: "Base", Classification: "base", Conditions: []string{"WiFi"}},
		&schema.DeviceTypeDefinition{ID: rootID, Name: "RootNode", Classification: "node", Conditions: []string{"Guarded"}, Requirements: []schema.DeviceTypeRequirement{
			componentReq("Widget", widgetID, confName("Guarded"), schema.CountRange{}, 0, clusterReq("ColorControl", clColorControl, confM())),
		}},
		&schema.DeviceTypeDefinition{ID: widgetID, Name: "Widget", Classification: "simple"},
		&schema.DeviceTypeDefinition{ID: wifiAsserterID, Name: "WiFiAsserter", Classification: "simple", Requirements: []schema.DeviceTypeRequirement{condReq("RootNode", "Guarded", confName("WiFi"))}},
		&schema.DeviceTypeDefinition{ID: dtOnOffLight, Name: "OnOffLight", Classification: "simple"},
	)
}

// onOffSingletonModel: Declarer declares OnOff a singleton; Needy requires
// ColorControl (RuntimeValidationTest.ts:186-208).
func onOffSingletonModel() *dtLookups {
	return syntheticModel(
		&schema.DeviceTypeDefinition{Name: "Base", Classification: "base"},
		&schema.DeviceTypeDefinition{ID: rootID, Name: "RootNode", Classification: "node"},
		&schema.DeviceTypeDefinition{ID: declarerID, Name: "Declarer", Classification: "simple", Requirements: []schema.DeviceTypeRequirement{singletonReq("OnOff", clOnOff, confO())}},
		&schema.DeviceTypeDefinition{ID: needyID, Name: "Needy", Classification: "simple", Requirements: []schema.DeviceTypeRequirement{clusterReq("ColorControl", clColorControl, confM())}},
		&schema.DeviceTypeDefinition{ID: dtOnOffLight, Name: "OnOffLight", Classification: "simple"},
	)
}

// wiFiGatedModel: OnOffLight requires ColorControl while the node supports
// Wi-Fi (RuntimeValidationTest.ts:210-226).
func wiFiGatedModel() *dtLookups {
	return syntheticModel(
		&schema.DeviceTypeDefinition{Name: "Base", Classification: "base", Conditions: []string{"WiFi"}},
		&schema.DeviceTypeDefinition{ID: rootID, Name: "RootNode", Classification: "node"},
		&schema.DeviceTypeDefinition{ID: dtOnOffLight, Name: "OnOffLight", Classification: "simple", Requirements: []schema.DeviceTypeRequirement{clusterReq("ColorControl", clColorControl, confName("WiFi"))}},
	)
}

// syntheticNode is a fake node judged in a synthetic model: its root lists
// RootNode and serves nothing, which those models do not require.
func syntheticNode() (*fakeTree, *fakeEP) {
	f := newFakeTree()
	root := f.add("node", nil, rootID)
	return f, root
}

// guardedNode is a node judged in guardedRootModel with a Widget child
// (RuntimeValidationTest.ts:228-235 createGuardedNode).
func guardedNode(t *testing.T, mode DeviceTypeValidationMode) (s *service, f *fakeTree, root, widget *fakeEP) {
	t.Helper()
	f, root = syntheticNode()
	widget = standIn(f, root, "widget", widgetID)
	s = newService(t, mode, guardedRootModel(), f)
	s.mustPass()
	return s, f, root, widget
}

// standardNode is a node judged in the snapshot's model.
func standardNode(t *testing.T, mode DeviceTypeValidationMode) (*service, *fakeTree, *fakeEP) {
	t.Helper()
	f, root := nodeTree()
	s := newService(t, mode, deviceTypeLookups(), f)
	s.mustPass()
	return s, f, root
}

func TestRuntimeValidationParityMatterJS(t *testing.T) {
	t.Run("device type validation after construction", func(t *testing.T) {
		t.Run("when a child is destroyed", testChildDestroyed)
		t.Run("when a device type list changes", testDeviceTypeListChanges)
		t.Run("siblings", testSiblings)
		t.Run("a bridge of identical lights", func(t *testing.T) {
			t.Run("reads no earlier light when a light asserting a condition on the node endpoint is added", func(t *testing.T) { t.Skip(skipCounting) })
		})
		t.Run("a fact that reaches the node scope", func(t *testing.T) {
			for _, title := range []string{
				"judges the whole node scope when an endpoint supporting a network interface is added",
				"judges the whole node scope when a subtree with such an endpoint is added",
				"judges the whole node scope when an endpoint declaring a singleton is added",
				"judges the whole node scope when an endpoint without a recorded judgement changes",
				"judges the whole node scope when an endpoint without a recorded judgement is destroyed",
				"judges the whole node scope when a destroyed descendant has no recorded judgement",
				"judges only the node endpoint when a nested node endpoint with such a descendant is destroyed",
				"judges the whole node scope when an endpoint above such an endpoint becomes a node endpoint",
				"judges the whole node scope when a subtree with such an endpoint is destroyed",
			} {
				t.Run(title, func(t *testing.T) { t.Skip(skipCounting) })
			}
		})
		t.Run("a condition asserted on the node endpoint", testNodeEndpointConditions)
		t.Run("what passes keep of a node scope", testNodeScopePasses)
		t.Run("a refused addition", testRefusedAddition)
		t.Run("a later addition", testLaterAddition)
		t.Run("node teardown", func(t *testing.T) {
			t.Run("judges and logs nothing and forgets every endpoint", func(t *testing.T) {
				t.Skip("a node's teardown has no Go subject: Stop runs no pass, and the next Start judges the snapshot with a validator that recorded nothing (bridge.SetDeviceTypeValidation)")
			})
		})
		t.Run("a peer", func(t *testing.T) {
			t.Run("judges nothing when an endpoint of a peer is destroyed", func(t *testing.T) { t.Skip(skipPeer) })
			t.Run("refuses to validate an endpoint of a peer", func(t *testing.T) { t.Skip(skipPeer) })
		})
		t.Run("in off mode", testRuntimeOffMode)
		t.Run("in default configuration", func(t *testing.T) {
			t.Run("logs no warning for a light through start, runtime changes and close", func(t *testing.T) { t.Skip(skipNode) })
			t.Run("logs no warning for a bridge through start, runtime changes and close", func(t *testing.T) { t.Skip(skipNode) })
		})
	})
}

func testChildDestroyed(t *testing.T) {
	t.Run("reports the composition it breaks, judging only its ancestors", func(t *testing.T) {
		// Which endpoints are judged is not counted (skipCounting).
		s, f, root := standardNode(t, DeviceTypeValidationWarn)
		fridge, cabinets := fridgeOf(f, root, "fridge", 1)
		s.mustPass()
		wantRequirements(t, s.requirementsOf(fridge))
		f.remove(cabinets[0])
		logged := s.mustPass()
		if !strings.Contains(joined(logged), "instanceCount Refrigerator device:TemperatureControlledCabinet") {
			t.Errorf("logged %v", logged)
		}
		wantRequirements(t, s.requirementsOf(fridge), brokenFridge...)
	})

	t.Run("logs a violation once while it persists and again once it returns", func(t *testing.T) {
		s, f, root := standardNode(t, DeviceTypeValidationWarn)
		fridge, cabinets := fridgeOf(f, root, "fridge", 1)
		part := light(f, fridge, "light")
		s.mustPass()
		f.remove(cabinets[0])
		first := s.mustPass()
		f.remove(part)
		persisting := s.mustPass()
		second := cabinetOf(f, fridge, "second")
		s.mustPass()
		f.remove(second)
		returning := s.mustPass()
		if len(first) != 1 || len(persisting) != 0 || len(returning) != 1 {
			t.Errorf("first %v, persisting %v, returning %v", first, persisting, returning)
		}
	})

	t.Run("forgets the destroyed endpoint and its descendants", func(t *testing.T) {
		s, f, root := standardNode(t, DeviceTypeValidationWarn)
		agg := aggregatorOf(f, root, "aggregator")
		fridge, _ := fridgeOf(f, agg, "fridge", 0)
		s.mustPass()
		if len(s.requirementsOf(fridge)) == 0 {
			t.Fatal("the empty fridge conforms")
		}
		f.remove(agg)
		s.mustPass()
		wantRequirements(t, s.requirementsOf(fridge))
	})

	t.Run("judges once for a destroyed subtree", func(t *testing.T) {
		// The single judgement is not counted (skipCounting); nothing is logged.
		s, f, root := standardNode(t, DeviceTypeValidationWarn)
		agg := aggregatorOf(f, root, "aggregator")
		fridgeOf(f, agg, "fridge", 2)
		s.mustPass()
		f.remove(agg)
		if logged := s.mustPass(); len(logged) != 0 {
			t.Errorf("logged %v", logged)
		}
	})

	for _, order := range []string{"child first", "owner first"} {
		t.Run("judges only above the owner when an endpoint and its owner close together, "+order, func(t *testing.T) {
			// Both leave in one snapshot whatever the order they close in.
			s, f, root := standardNode(t, DeviceTypeValidationWarn)
			fridge, cabinets := fridgeOf(f, root, "fridge", 1)
			s.mustPass()
			if order == "child first" {
				f.remove(cabinets[0])
			}
			f.remove(fridge)
			if logged := s.mustPass(); len(logged) != 0 {
				t.Errorf("logged %v", logged)
			}
			wantRequirements(t, s.requirementsOf(fridge))
		})
	}

	t.Run("judges nothing when the owner crashed", func(t *testing.T) { t.Skip(skipCrash) })
	t.Run("logs rather than fails the destruction when judging it throws", func(t *testing.T) { t.Skip(skipThrows) })

	t.Run("logs rather than refuses in strict mode", func(t *testing.T) {
		s, f, root := standardNode(t, DeviceTypeValidationStrict)
		fridge, cabinets := fridgeOf(f, root, "fridge", 1)
		s.mustPass()
		f.remove(cabinets[0])
		logged, err := s.pass()
		if err != nil {
			t.Fatalf("a removal was refused: %v", err)
		}
		if len(logged) != 1 {
			t.Errorf("logged %v", logged)
		}
		wantRequirements(t, s.requirementsOf(fridge), brokenFridge...)
	})
}

func testDeviceTypeListChanges(t *testing.T) {
	t.Run("reports the endpoint", func(t *testing.T) {
		s, f, root := standardNode(t, DeviceTypeValidationWarn)
		l := light(f, root, "light")
		s.mustPass()
		l.deviceTypes = append(l.deviceTypes, dtTempSensor)
		logged := s.mustPass()
		if len(logged) != 1 || !strings.Contains(logged[0], "missing TemperatureSensor TemperatureMeasurement") {
			t.Errorf("logged %v", logged)
		}
		wantRequirements(t, s.requirementsOf(l), "missing TemperatureMeasurement")
	})

	t.Run("judges the endpoint's descendants", func(t *testing.T) {
		s, f, root := standardNode(t, DeviceTypeValidationWarn)
		l := light(f, root, "light")
		sensor := tempSensor(f, l, "sensor")
		s.mustPass()
		wantRequirements(t, s.requirementsOf(sensor))
		l.deviceTypes = append(l.deviceTypes, dtBatteryStorage)
		s.mustPass()
		wantRequirements(t, s.requirementsOf(sensor), "missing device:BatteryStorage/TemperatureSensor")
	})

	t.Run("judges the endpoint's ancestors", func(t *testing.T) {
		s, f, root := standardNode(t, DeviceTypeValidationWarn)
		fridge, cabinets := fridgeOf(f, root, "fridge", 1)
		s.mustPass()
		// A node endpoint leaves the composition of the endpoints above it.
		cabinets[0].deviceTypes = append(cabinets[0].deviceTypes, dtRootNode)
		s.mustPass()
		wantRequirements(t, s.requirementsOf(fridge), brokenFridge...)
	})

	t.Run("follows the list again after a factory reset", func(t *testing.T) {
		t.Skip("following a list across a factory reset has no Go subject: the validator follows nothing, and every reassembly judges the snapshot as it is")
	})
	t.Run("logs rather than fails the change when judging it throws", func(t *testing.T) { t.Skip(skipThrows) })
	t.Run("judges nothing for a device type an endpoint adds while it is constructed", func(t *testing.T) { t.Skip(skipCounting) })
}

func testSiblings(t *testing.T) {
	t.Run("judges the sibling a second light makes a duplicate", func(t *testing.T) {
		s, f, root := standardNode(t, DeviceTypeValidationWarn)
		first := light(f, root, "first")
		s.mustPass()
		wantRequirements(t, s.requirementsOf(first))
		light(f, root, "second")
		if logged := s.mustPass(); len(logged) != 2 {
			t.Errorf("logged %v", logged)
		}
		wantRequirements(t, s.requirementsOf(first), missingTagList)
	})

	t.Run("clears the violation of the sibling a removal leaves unique", func(t *testing.T) {
		s, f, root := standardNode(t, DeviceTypeValidationWarn)
		first := light(f, root, "first")
		second := light(f, root, "second")
		s.mustPass()
		wantRequirements(t, s.requirementsOf(first), missingTagList)
		f.remove(second)
		s.mustPass()
		wantRequirements(t, s.requirementsOf(first))
	})

	t.Run("judges the siblings a device type change makes duplicates", func(t *testing.T) {
		s, f, root := standardNode(t, DeviceTypeValidationWarn)
		l := light(f, root, "light")
		sensor := tempSensor(f, root, "sensor")
		s.mustPass()
		sensor.deviceTypes = append(sensor.deviceTypes, dtOnOffLight)
		s.mustPass()
		wantRequirements(t, s.requirementsOf(l), missingTagList)
	})

	t.Run("judges a constant number of endpoints per identical bridged light", func(t *testing.T) { t.Skip(skipCounting) })
	t.Run("judges only above the owner when a bridged device with a part is deleted", func(t *testing.T) { t.Skip(skipCounting) })
	t.Run("judges a constant number of endpoints per deleted bridged light", func(t *testing.T) { t.Skip(skipCounting) })
}

func testNodeEndpointConditions(t *testing.T) {
	t.Run("judges a constant number of endpoints per bridged lock", func(t *testing.T) { t.Skip(skipCounting) })

	t.Run("judges the endpoints whose component judgement reads the node endpoint's conditions", func(t *testing.T) {
		s, f, root, widget := guardedNode(t, DeviceTypeValidationWarn)
		bystander := standIn(f, root, "bystander", dtOnOffLight)
		s.mustPass()
		wantRequirements(t, s.requirementsOf(widget))
		standIn(f, root, "asserter", asserterID)
		s.mustPass()
		wantRequirements(t, s.requirementsOf(widget), unguardedWidget)
		wantRequirements(t, s.requirementsOf(bystander))
	})

	t.Run("judges them when a sibling starts asserting because it becomes a duplicate", func(t *testing.T) {
		s, f, root, widget := guardedNode(t, DeviceTypeValidationWarn)
		standIn(f, root, "asserter", duplicateAsserID, dtOnOffLight)
		s.mustPass()
		wantRequirements(t, s.requirementsOf(widget))
		standIn(f, root, "light", dtOnOffLight)
		s.mustPass()
		wantRequirements(t, s.requirementsOf(widget), unguardedWidget)
	})

	t.Run("judges them when an endpoint starts asserting", func(t *testing.T) {
		s, f, root, widget := guardedNode(t, DeviceTypeValidationWarn)
		l := standIn(f, root, "light", dtOnOffLight)
		s.mustPass()
		l.deviceTypes = []uint32{dtOnOffLight, asserterID}
		s.mustPass()
		wantRequirements(t, s.requirementsOf(widget), unguardedWidget)
	})

	t.Run("judges no reader when another endpoint asserts a condition that already holds", func(t *testing.T) {
		// Which endpoints are judged is not counted (skipCounting).
		s, f, root, widget := guardedNode(t, DeviceTypeValidationWarn)
		standIn(f, root, "asserter1", asserterID)
		s.mustPass()
		standIn(f, root, "asserter2", asserterID)
		s.mustPass()
		wantRequirements(t, s.requirementsOf(widget), unguardedWidget)
	})

	t.Run("judges a reader whose recorded judgement was forgotten", func(t *testing.T) {
		// The Go validator forgets nothing on request; the verdict is the same.
		s, f, root, widget := guardedNode(t, DeviceTypeValidationWarn)
		standIn(f, root, "asserter1", asserterID)
		shelf := standIn(f, root, "shelf", dtOnOffLight)
		s.mustPass()
		standIn(f, shelf, "asserter2", asserterID)
		s.mustPass()
		wantRequirements(t, s.requirementsOf(widget), unguardedWidget)
	})

	t.Run("judges them when an endpoint stops asserting", func(t *testing.T) {
		s, f, root, widget := guardedNode(t, DeviceTypeValidationWarn)
		asserter := standIn(f, root, "asserter", asserterID)
		s.mustPass()
		wantRequirements(t, s.requirementsOf(widget), unguardedWidget)
		asserter.deviceTypes = []uint32{dtOnOffLight}
		s.mustPass()
		wantRequirements(t, s.requirementsOf(widget))
	})

	t.Run("judges no reader above a nested node endpoint the assertion targets", func(t *testing.T) {
		s, f, root, widget := guardedNode(t, DeviceTypeValidationWarn)
		nested := standIn(f, root, "nested", rootID)
		standIn(f, nested, "asserter", asserterID)
		s.mustPass()
		wantRequirements(t, s.requirementsOf(widget))
	})

	t.Run("judges no reader when a nested node endpoint starts asserting on itself", func(t *testing.T) {
		s, f, root, widget := guardedNode(t, DeviceTypeValidationWarn)
		standIn(f, root, "light", dtOnOffLight)
		nested := standIn(f, root, "nested", rootID, duplicateAsserID)
		s.mustPass()
		nested.deviceTypes = []uint32{rootID, duplicateAsserID, dtOnOffLight}
		s.mustPass()
		wantRequirements(t, s.requirementsOf(widget))
	})

	t.Run("judges them when a condition returns to what they were last judged under after one was judged apart", func(t *testing.T) {
		f, root := syntheticNode()
		widget := standIn(f, root, "widget", widgetID)
		standIn(f, root, "asserter1", wifiAsserterID)
		shelf := standIn(f, root, "shelf", dtOnOffLight)
		l := standIn(f, root, "light", dtOnOffLight)
		s := newService(t, DeviceTypeValidationWarn, wiFiGuardedRootModel(), f)
		s.mustPass()
		wantRequirements(t, s.requirementsOf(widget))
		l.server(clNetworkComm, "WI")
		s.mustPass()
		wantRequirements(t, s.requirementsOf(widget), unguardedWidget)
		delete(l.servers, clNetworkComm)
		l.order = slices.DeleteFunc(l.order, func(c uint32) bool { return c == clNetworkComm })
		standIn(f, shelf, "asserter2", wifiAsserterID)
		s.mustPass()
		wantRequirements(t, s.requirementsOf(widget))
	})

	t.Run("judges them when an asserting endpoint is destroyed", func(t *testing.T) {
		s, f, root, widget := guardedNode(t, DeviceTypeValidationWarn)
		agg := standIn(f, root, "aggregator", dtAggregator)
		standIn(f, agg, "asserter", asserterID)
		s.mustPass()
		wantRequirements(t, s.requirementsOf(widget), unguardedWidget)
		f.remove(agg)
		s.mustPass()
		wantRequirements(t, s.requirementsOf(widget))
	})
}

func testNodeScopePasses(t *testing.T) {
	t.Run("leaves an unrelated sibling's descendants unread when an addition adds nothing that reaches the node scope", func(t *testing.T) { t.Skip(skipCounting) })
	t.Run("walks the node scope again after a reset instead of reusing what an earlier pass kept", func(t *testing.T) { t.Skip(skipCounting) })

	t.Run("reads the node scope again once a nested node endpoint stops being one", func(t *testing.T) {
		s, f, root, widget := guardedNode(t, DeviceTypeValidationWarn)
		nested := standIn(f, root, "nested", rootID)
		standIn(f, nested, "asserter", asserterID)
		s.mustPass()
		wantRequirements(t, s.requirementsOf(widget))
		nested.deviceTypes = []uint32{dtOnOffLight}
		s.mustPass()
		wantRequirements(t, s.requirementsOf(widget), unguardedWidget)
	})

	t.Run("reads the node scope again once an endpoint that became a node endpoint stops being one", func(t *testing.T) {
		s, f, root, widget := guardedNode(t, DeviceTypeValidationWarn)
		l := standIn(f, root, "light", dtOnOffLight)
		standIn(f, l, "asserter", asserterID)
		s.mustPass()
		wantRequirements(t, s.requirementsOf(widget), unguardedWidget)
		l.deviceTypes = []uint32{rootID}
		s.mustPass()
		wantRequirements(t, s.requirementsOf(widget))
		l.deviceTypes = []uint32{dtOnOffLight}
		s.mustPass()
		wantRequirements(t, s.requirementsOf(widget), unguardedWidget)
	})

	t.Run("reads a network interface of a server cluster added at runtime", func(t *testing.T) {
		// matter.js judges the light alone on request; the Go pass judges
		// the other light too, which the same Wi-Fi gates.
		f, root := syntheticNode()
		l := standIn(f, root, "light", dtOnOffLight)
		other := standIn(f, root, "other", dtOnOffLight)
		s := newService(t, DeviceTypeValidationWarn, wiFiGatedModel(), f)
		s.mustPass()
		wantRequirements(t, s.requirementsOf(l))
		other.server(clNetworkComm, "WI")
		logged := s.mustPass()
		if !strings.Contains(joined(logged), "Endpoint light violates") {
			t.Errorf("logged %v", logged)
		}
		wantRequirements(t, s.requirementsOf(l), "missing ColorControl")
	})

	for _, removal := range []string{"close", "delete"} {
		t.Run("judges the whole node scope when an endpoint whose server clusters changed is removed with "+removal+"()", func(t *testing.T) {
			f, root := syntheticNode()
			l := standIn(f, root, "light", dtOnOffLight)
			shelf := standIn(f, root, "shelf", dtOnOffLight)
			other := standIn(f, shelf, "other", dtOnOffLight)
			s := newService(t, DeviceTypeValidationWarn, wiFiGatedModel(), f)
			s.mustPass()
			other.server(clNetworkComm, "WI")
			s.mustPass()
			wantRequirements(t, s.requirementsOf(l), "missing ColorControl")
			f.remove(other)
			s.mustPass()
			wantRequirements(t, s.requirementsOf(l))
		})
	}

	for _, essential := range []bool{true, false} {
		title := "drops an endpoint whose addition it refused, rolled back"
		if !essential {
			title = "drops an endpoint whose addition it refused, left crashed"
		}
		t.Run(title, func(t *testing.T) {
			if !essential {
				t.Skip(skipCrash)
			}
			s, f, root, widget := guardedNode(t, DeviceTypeValidationStrict)
			asserter := standIn(f, root, "asserter", asserterID)
			if _, err := s.pass(); err == nil {
				t.Fatal("the addition was not refused")
			}
			f.remove(asserter)
			s.mustPass()
			wantRequirements(t, s.requirementsOf(widget))
		})
	}
}

func testRefusedAddition(t *testing.T) {
	t.Run("logs nothing and throws every endpoint strict mode refuses", func(t *testing.T) {
		s, f, root := standardNode(t, DeviceTypeValidationStrict)
		first := light(f, root, "first")
		s.mustPass()
		second := light(f, root, "second")
		logged, err := s.pass()
		var refusal *DeviceTypeConformanceError
		if !errors.As(err, &refusal) || len(logged) != 0 {
			t.Fatalf("pass = %v, %v; want a refusal and nothing logged", logged, err)
		}
		// The error names the endpoints in tree order; matter.js names the
		// added endpoint first.
		if got := refusalNames(f, err); !slices.Equal(got, []string{"first", "second"}) {
			t.Errorf("refused %v", got)
		}
		f.remove(second)
		wantRequirements(t, s.requirementsOf(first))
	})

	t.Run("logs nothing for an endpoint judged in the pass that it does not refuse", func(t *testing.T) {
		f, root := syntheticNode()
		standIn(f, root, "light", dtOnOffLight)
		s := newService(t, DeviceTypeValidationWarn, onOffSingletonModel(), f)
		s.mustPass()
		standIn(f, root, "declarer", declarerID, needyID)
		logged, err := s.pass()
		if err == nil || !strings.Contains(strings.Join(refusalNames(f, err), ","), "light") || len(logged) != 0 {
			t.Errorf("pass = %v, %v; want a refusal naming the light and nothing logged", logged, err)
		}
	})

	t.Run("keeps and follows nothing of an endpoint it rolled back", func(t *testing.T) {
		// The Go validator keeps no index; what it keeps is the recorded
		// verdict, which a refused pass leaves as it was.
		f, root := syntheticNode()
		standIn(f, root, "light", dtOnOffLight)
		s := newService(t, DeviceTypeValidationWarn, onOffSingletonModel(), f)
		s.mustPass()
		before := s.v.Violations()
		declarer := standIn(f, root, "declarer", declarerID, needyID)
		if _, err := s.pass(); err == nil {
			t.Fatal("the addition was not refused")
		}
		f.remove(declarer)
		if got := s.v.Violations(); len(got) != len(before) {
			t.Errorf("recorded %v after the refusal, want %v", got, before)
		}
		for _, key := range violationsAt(s.v.Violations(), declarer) {
			t.Errorf("recorded %s of the rolled-back endpoint", key)
		}
	})
}

func testLaterAddition(t *testing.T) {
	t.Run("is not refused in strict mode for a sibling violation a runtime change caused", func(t *testing.T) {
		s, f, root := standardNode(t, DeviceTypeValidationStrict)
		l := light(f, root, "light")
		sensor := tempSensor(f, root, "sensor")
		s.mustPass()
		sensor.deviceTypes = append(sensor.deviceTypes, dtOnOffLight)
		if _, err := s.pass(); err != nil {
			t.Fatalf("the change was refused: %v", err)
		}
		light(f, l, "child")
		logged, err := s.pass()
		if err != nil || len(logged) != 0 {
			t.Errorf("pass = %v, %v; want the child accepted and nothing logged", logged, err)
		}
	})

	t.Run("is refused in strict mode for a sibling violation it causes", func(t *testing.T) {
		s, f, root := standardNode(t, DeviceTypeValidationStrict)
		light(f, root, "untagged")
		s.mustPass()
		tagged := light(f, root, "tagged")
		tagged.servers[clDescriptor] = []string{"TAGLIST"}
		_, err := s.pass()
		if !slices.Contains(refusalNames(f, err), "untagged") {
			t.Errorf("pass = %v; want a refusal naming untagged", err)
		}
	})

	t.Run("is not refused in strict mode for an ancestor violation a runtime change caused", func(t *testing.T) {
		s, f, root := standardNode(t, DeviceTypeValidationStrict)
		fridge, cabinets := fridgeOf(f, root, "fridge", 1)
		s.mustPass()
		f.remove(cabinets[0])
		if _, err := s.pass(); err != nil {
			t.Fatalf("the removal was refused: %v", err)
		}
		light(f, fridge, "light")
		logged, err := s.pass()
		if err != nil || len(logged) != 0 {
			t.Errorf("pass = %v, %v; want the light accepted and nothing logged", logged, err)
		}
	})

	t.Run("is not refused in strict mode for an ancestor's added device type", func(t *testing.T) {
		s, f, root := standardNode(t, DeviceTypeValidationStrict)
		l := light(f, root, "light")
		s.mustPass()
		l.deviceTypes = append(l.deviceTypes, dtTempSensor)
		if _, err := s.pass(); err != nil {
			t.Fatalf("the change was refused: %v", err)
		}
		light(f, l, "child")
		logged, err := s.pass()
		if err != nil || len(logged) != 0 {
			t.Errorf("pass = %v, %v; want the child accepted and nothing logged", logged, err)
		}
	})

	t.Run("is not refused for a singleton a runtime change misplaced in an ancestor", func(t *testing.T) {
		t.Skip("RuntimeValidationTest.ts:1221: a singleton a runtime change misplaces is refused in every mode in Go (ADR 0016), where matter.js logs it, so the change itself is refused before the later addition")
	})

	t.Run("is refused in strict mode for an ancestor violation it causes", func(t *testing.T) {
		f, root := syntheticNode()
		composer := standIn(f, root, "composer", composer30ID)
		standIn(f, composer, "light1", dtOnOffLight)
		s := newService(t, DeviceTypeValidationStrict, singleComponentModel(), f)
		s.mustPass()
		standIn(f, composer, "light2", dtOnOffLight)
		_, err := s.pass()
		if !slices.Contains(refusalNames(f, err), "composer") {
			t.Errorf("pass = %v; want a refusal naming composer", err)
		}
	})
}

func testRuntimeOffMode(t *testing.T) {
	t.Run("follows no endpoint", func(t *testing.T) {
		t.Skip("the observers matter.js registers per mode have no Go subject: the validator follows nothing in any mode")
	})

	t.Run("judges nothing when a device type list changes or an endpoint is destroyed", func(t *testing.T) {
		f, root := nodeTree()
		fridge, cabinets := fridgeOf(f, root, "fridge", 1)
		l := light(f, root, "light")
		s := newService(t, DeviceTypeValidationOff, deviceTypeLookups(), f)
		s.mustPass()
		l.deviceTypes = append(l.deviceTypes, dtTempSensor)
		f.remove(cabinets[0])
		if logged := s.mustPass(); len(logged) != 0 {
			t.Errorf("logged %v", logged)
		}
		wantRequirements(t, s.requirementsOf(fridge))
	})

	t.Run("judges on request with what the node scope holds now", func(t *testing.T) {
		// On request is the pure judgement (ValidateDeviceTypes), which
		// logs nothing in Go; the mode records nothing.
		f, root := syntheticNode()
		l := standIn(f, root, "light", dtOnOffLight)
		s := newService(t, DeviceTypeValidationOff, wiFiGatedModel(), f)
		s.mustPass()
		if got := violationsAt(s.verdictOf(), l); len(got) != 0 {
			t.Errorf("before Wi-Fi: %v", got)
		}
		wifi := standIn(f, root, "wifi", dtOnOffLight)
		wifi.server(clNetworkComm, "WI")
		if logged := s.mustPass(); len(logged) != 0 {
			t.Errorf("off logged %v", logged)
		}
		if got := violationsAt(s.verdictOf(), l); !slices.Equal(got, []string{"missing ColorControl"}) {
			t.Errorf("on request: %v, want [missing ColorControl]", got)
		}
		wantRequirements(t, s.requirementsOf(l))
	})
}

// violationsAt is the keys of the violations of ep among vs.
func violationsAt(vs []DeviceTypeViolation, ep *fakeEP) []string {
	var out []string
	for i := range vs {
		if vs[i].Endpoint == ep.id {
			out = append(out, vs[i].Key())
		}
	}
	return out
}

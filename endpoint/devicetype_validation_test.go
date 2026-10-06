// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package endpoint

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/SukramJ/go-fabric/cluster"
	mattercore "github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/schema"
)

// Device type validation against the snapshot's model: one test per kind
// of requirement and per condition the module decides, each with the case
// that conforms. The synthetic-model cases are in
// devicetype_parity_matterjs_test.go.

// Cluster and device type ids used below.
const (
	dtRootNode        = 0x0016
	dtAggregator      = 0x000E
	dtBridgedNode     = 0x0013
	dtOnOffLight      = 0x0100
	dtDimmableLight   = 0x0101
	dtOnOffSwitch     = 0x0103
	dtDoorLock        = 0x000A
	dtLaundryWasher   = 0x0073
	dtExtractorHood   = 0x007A
	dtRefrigerator    = 0x0070
	dtCabinet         = 0x0071
	clIdentify        = 0x0003
	clGroups          = 0x0004
	clOnOff           = 0x0006
	clDescriptor      = 0x001D
	clBinding         = 0x001E
	clBasicInfo       = 0x0028
	clLocalization    = 0x002B
	clNetworkComm     = 0x0031
	clTimeSync        = 0x0038
	clAdminComm       = 0x003C
	clIcd             = 0x0046
	clLaundryMode     = 0x0051
	clOperationalSt   = 0x0060
	clScenes          = 0x0062
	clGroupcast       = 0x0065
	clDoorLock        = 0x0101
	clFanControl      = 0x0202
	cmdTriggerEffect  = 0x40
	cmdCopyScene      = 0x40
	evOperationDone   = 0x01
	attrStartUpMode   = 0x0002
	featureOnOffLT    = "LT"
	featureFanWind    = "WND"
	conditionLanguage = "LanguageLocale"
)

// standardTree is a fake tree judged in the snapshot's model, with the
// node condition this module always asserts.
func standardTree() (*fakeTree, *fakeEP) {
	f := newFakeTree()
	f.node = []string{condCustomNetworkConfig}
	root := f.add("root", nil, dtRootNode)
	return f, root
}

// light adds an OnOffLight that meets every requirement OnOffLight states.
func light(f *fakeTree, parent *fakeEP, name string, extra ...uint32) *fakeEP {
	ep := f.add(name, parent, append([]uint32{dtOnOffLight}, extra...)...).
		server(clDescriptor).server(clIdentify).server(clGroups).server(clOnOff, featureOnOffLT).server(clScenes)
	ep.implements(clIdentify, schema.RequirementCommand, cmdTriggerEffect)
	ep.implements(clScenes, schema.RequirementCommand, cmdCopyScene)
	return ep
}

func TestDeviceTypeClusterRequirements(t *testing.T) {
	t.Parallel()
	l := deviceTypeLookups()
	f, root := standardTree()

	wantKinds(t, f.check(l, light(f, root, "light")))

	// A missing server cluster is the one finding for it: its nested
	// requirements (OnOff.LT) are not judged.
	f, root = standardTree()
	bare := f.add("bare", root, dtOnOffLight).server(clDescriptor).server(clIdentify).server(clGroups).server(clScenes)
	bare.implements(clIdentify, schema.RequirementCommand, cmdTriggerEffect).implements(clScenes, schema.RequirementCommand, cmdCopyScene)
	got := f.check(l, bare)
	wantKinds(t, got, "missing OnOff")
	if v := got[0]; v.DeviceType != "OnOffLight" || v.DeviceTypeID != dtOnOffLight || !v.HasCluster || v.Cluster != clOnOff || v.Element != "" || v.Conformance != "M" {
		t.Errorf("violation = %+v", v)
	}

	// DoorLock disallows Groups (X).
	lock := f.add("lock", root, dtDoorLock).server(clDescriptor).server(clIdentify).server(clDoorLock).server(clGroups)
	wantKinds(t, f.check(l, lock), "disallowed Groups")

	// OnOffLightSwitch requires OnOff and Identify as clients.
	sw := f.add("switch", root, dtOnOffSwitch).server(clDescriptor).server(clIdentify)
	wantKinds(t, f.check(l, sw), "missing client:Identify", "missing client:OnOff")
	// With them, Base's Client condition holds for the simple endpoint, so
	// Binding becomes mandatory — Base's requirement.
	sw.client(clIdentify, clOnOff)
	got = f.check(l, sw)
	wantKinds(t, got, "missing Binding")
	if got[0].DeviceType != "Base" || got[0].DeviceTypeID != 0 || !slices.Equal(got[0].Conditions, []string{condSimple, condClient}) {
		t.Errorf("Binding violation = %+v", got[0])
	}
	sw.server(clBinding)
	wantKinds(t, f.check(l, sw))

	// Base requires Descriptor of every endpoint that lists a known device
	// type, and of none that lists only unknown ones.
	wantKinds(t, f.check(l, f.add("nodescriptor", root, dtDoorLock).server(clIdentify).server(clDoorLock)), "missing Descriptor")
	wantKinds(t, f.check(l, f.add("custom", root, 0xFFF1_0001)))
}

func TestDeviceTypeElementRequirements(t *testing.T) {
	t.Parallel()
	l := deviceTypeLookups()
	f, root := standardTree()

	// A feature the device type requires.
	noLT := light(f, root, "noLT").server(clOnOff)
	got := f.check(l, noLT)
	wantKinds(t, got, "missing OnOff.LT")
	if v := got[0]; v.Element != schema.RequirementFeature || v.ElementName != "LT" || v.ElementID != 0 || v.Cluster != clOnOff {
		t.Errorf("feature violation = %+v", v)
	}

	// A command.
	f, root = standardTree()
	noTrigger := light(f, root, "noTrigger")
	noTrigger.elements[clIdentify] = nil
	got = f.check(l, noTrigger)
	wantKinds(t, got, "missing Identify.TriggerEffect")
	if v := got[0]; v.Element != schema.RequirementCommand || v.ElementID != cmdTriggerEffect {
		t.Errorf("command violation = %+v", v)
	}

	// An event: LaundryWasher's OperationalState must emit
	// OperationCompletion; an attribute it disallows: LaundryWasherMode's
	// StartUpMode.
	washer := f.add("washer", root, dtLaundryWasher).server(clDescriptor).server(clIdentify).server(clOperationalSt).server(clLaundryMode)
	washer.implements(clLaundryMode, schema.RequirementAttribute, attrStartUpMode)
	wantKinds(t, f.check(l, washer), "disallowed LaundryWasherMode.StartUpMode", "missing OperationalState.OperationCompletion")
	washer.elements[clLaundryMode] = nil
	washer.implements(clOperationalSt, schema.RequirementEvent, evOperationDone)
	wantKinds(t, f.check(l, washer))

	// A feature the device type disallows: ExtractorHood's FanControl may
	// not have Wind.
	hood := f.add("hood", root, dtExtractorHood).server(clDescriptor).server(clIdentify).server(clFanControl, featureFanWind)
	wantKinds(t, f.check(l, hood), "disallowed FanControl.WND")
	hood.server(clFanControl)
	wantKinds(t, f.check(l, hood))
}

func TestDeviceTypeConditionsRootAssertions(t *testing.T) {
	t.Parallel()
	l := deviceTypeLookups()

	// An OnOffLight asserts RootNode's GroupcastListenerCond on the root
	// (on-off-light.element.ts, location Root), so the root must serve
	// Groupcast.
	f, root := standardTree()
	light(f, root, "light")
	if !newDTPass(f, l).collect(root.id).conditionsOf(root.id)["GroupcastListenerCond"] {
		t.Fatal("an OnOffLight does not assert GroupcastListenerCond on the root")
	}
	got := f.check(l, root)
	if !slices.Contains(kindsOf(got), "missing Groupcast") {
		t.Errorf("root violations %v lack the Groupcast its OnOffLight asserts", kindsOf(got))
	}
	for _, v := range got {
		if v.Requirement == "Groupcast" && !slices.Equal(v.Conditions, []string{"GroupcastListenerCond"}) {
			t.Errorf("Groupcast violation names %v as the conditions that held", v.Conditions)
		}
	}

	// DimmableLight states the assertion under "Rev >= v4", which is not
	// decided, so nothing is asserted.
	f2, root2 := standardTree()
	f2.add("dimmable", root2, dtDimmableLight)
	if newDTPass(f2, l).collect(root2.id).conditionsOf(root2.id)["GroupcastListenerCond"] {
		t.Error("a DimmableLight asserts GroupcastListenerCond")
	}
	if slices.Contains(kindsOf(f2.check(l, root2)), "missing Groupcast") {
		t.Error("Groupcast is required without an asserting endpoint")
	}
}

func TestDeviceTypeConditionsStructural(t *testing.T) {
	t.Parallel()
	l := deviceTypeLookups()
	f, root := standardTree()
	a := light(f, root, "a")
	pass := newDTPass(f, l)
	rc := pass.collect(root.id).conditionsOf(root.id)
	for _, c := range []string{condNode, condComposed, condCustomNetworkConfig} {
		if !rc[c] {
			t.Errorf("root conditions %v lack %s", rc, c)
		}
	}
	ac := pass.collect(root.id).conditionsOf(a.id)
	for _, c := range []string{condApp, condSimple, condServer} {
		if !ac[c] {
			t.Errorf("light conditions %v lack %s", ac, c)
		}
	}
	if ac[condDuplicate] || ac[condClient] || ac[condNode] {
		t.Errorf("light conditions %v hold more than they should", ac)
	}

	// Duplicate: two OnOffLights under one parent need a TagList.
	b := light(f, root, "b")
	got := f.check(l, b)
	wantKinds(t, got, "missing Descriptor.TAGLIST")
	if got[0].DeviceType != "Base" || !slices.Equal(got[0].Conditions, []string{condDuplicate}) {
		t.Errorf("TagList violation = %+v", got[0])
	}
	b.server(clDescriptor, "TAGLIST")
	wantKinds(t, f.check(l, b))

	// … except bridged devices under an Aggregator, which their NodeLabel
	// tells apart (Core § 9.2.9, Device § 11.2.6).
	agg := f.add("aggregator", root, dtAggregator).server(clDescriptor)
	light(f, agg, "bridged1", dtBridgedNode).server(0x0039)
	second := light(f, agg, "bridged2", dtBridgedNode).server(0x0039)
	wantKinds(t, f.check(l, second))
	// A child of an Aggregator that is no bridged device is not waived.
	plain1 := light(f, agg, "plain1")
	light(f, agg, "plain2")
	wantKinds(t, f.check(l, plain1), "missing Descriptor.TAGLIST")
}

func TestDeviceTypeConditionsNode(t *testing.T) {
	t.Parallel()
	l := deviceTypeLookups()

	// CustomNetworkConfig, which this module always asserts, leaves
	// RootNode's NetworkCommissioning unrequired; without it the root must
	// serve one.
	f, root := standardTree()
	if slices.Contains(kindsOf(f.check(l, root)), "missing NetworkCommissioning") {
		t.Error("NetworkCommissioning required under CustomNetworkConfig")
	}
	f.node = nil
	if !slices.Contains(kindsOf(f.check(l, root)), "missing NetworkCommissioning") {
		t.Error("NetworkCommissioning not required without CustomNetworkConfig")
	}

	// The network interfaces a NetworkCommissioning server in the node
	// scope supports.
	f2, root2 := standardTree()
	root2.server(clNetworkComm, "ET")
	f2.add("secondary", root2, 0x0019).server(clNetworkComm, "WI")
	c := newDTPass(f2, l).collect(root2.id).conditionsOf(root2.id)
	if !c[condEthernet] || !c[condWiFi] || c[condThread] {
		t.Errorf("node conditions = %v, want Ethernet and WiFi", c)
	}
}

func TestDeviceTypeConditionsStated(t *testing.T) {
	t.Parallel()
	l := deviceTypeLookups()
	f, root := standardTree()

	// A stated condition can make a requirement mandatory …
	root.state(conditionLanguage)
	if !slices.Contains(kindsOf(f.check(l, root)), "missing LocalizationConfiguration") {
		t.Error("LanguageLocale stated, LocalizationConfiguration not required")
	}
	// … qualified by its declarer too.
	f2, root2 := standardTree()
	root2.state("RootNode.TimeSyncCond")
	if !slices.Contains(kindsOf(f2.check(l, root2)), "missing TimeSynchronization") {
		t.Error("RootNode.TimeSyncCond stated, TimeSynchronization not required")
	}
	// A name that names no condition is reported, with the declared
	// spelling of a case-insensitive match as the suggestion.
	f3, root3 := standardTree()
	root3.state("Bogus", "languagelocale")
	var unknown []DeviceTypeViolation
	for _, v := range f3.check(l, root3) {
		if v.Kind == ViolationUnknownCondition {
			unknown = append(unknown, v)
		}
	}
	if got := detailsOf(unknown); !slices.Equal(got, []string{
		`Unknown condition "Bogus"`,
		`Unknown condition "languagelocale"; did you mean "LanguageLocale"?`,
	}) || unknown[0].DeviceType != "RootNode" || unknown[0].Requirement != "Bogus" {
		t.Errorf("unknown conditions = %v / %+v", got, unknown)
	}
	// An endpoint that lists no known device type resolves names in Base.
	f4, root4 := standardTree()
	custom := f4.add("custom", root4, 0xFFF1_0001).state("Duplicate", "Nope")
	wantKinds(t, f4.check(l, custom), "unknownCondition Nope")
}

func TestDeviceTypeConditionsNeverDisallow(t *testing.T) {
	t.Parallel()
	l := deviceTypeLookups()
	f, root := standardTree()
	// LocalizationConfiguration is required under LanguageLocale; serving
	// it without the condition is not disallowed, and IcdManagement under
	// Sit | Lit — conditions no topology decides — is not required.
	root.server(clLocalization).server(clIcd)
	for _, v := range f.check(l, root) {
		if v.Kind == ViolationDisallowed || v.Requirement == "IcdManagement" {
			t.Errorf("condition-only verdict %v", v)
		}
	}
}

func TestDeviceTypeConditionsDescendant(t *testing.T) {
	t.Parallel()
	l := deviceTypeLookups()
	f, root := standardTree()
	// A Refrigerator asserts Cooler on its TemperatureControlledCabinet
	// components (location Descendant, min 1) and requires one.
	fridge := f.add("fridge", root, dtRefrigerator).server(clDescriptor)
	wantKinds(t, f.check(l, fridge), "instanceCount device:TemperatureControlledCabinet", "instanceCount condition:Cooler")
	got := f.check(l, fridge)
	if got[1].DeviceType != "Refrigerator" || got[1].Conformance != "M" {
		t.Errorf("Cooler count violation = %+v", got[1])
	}
	cabinet := f.add("cabinet", fridge, dtCabinet).server(clDescriptor).server(0x0056, "TN")
	wantKinds(t, f.check(l, fridge))
	if !newDTPass(f, l).collect(root.id).conditionsOf(cabinet.id)["Cooler"] {
		t.Error("the cabinet of a Refrigerator does not hold Cooler")
	}
	wantKinds(t, f.check(l, cabinet))
}

func TestDeviceTypeSingletons(t *testing.T) {
	t.Parallel()
	l := deviceTypeLookups()
	f, root := standardTree()
	agg := f.add("aggregator", root, dtAggregator).server(clDescriptor)
	bridged := light(f, agg, "bridged", dtBridgedNode).server(0x0039)
	// AdministratorCommissioning is a RootNode singleton BridgedNode lists
	// (FabricSynchronizedNode), so a bridged node may carry it …
	bridged.server(clAdminComm)
	wantKinds(t, f.check(l, bridged))
	// … BasicInformation it may not.
	bridged.server(clBasicInfo)
	got := f.check(l, bridged)
	wantKinds(t, got, "singletonMisplaced BasicInformation")
	if got[0].DeviceType != "RootNode" || got[0].Cluster != clBasicInfo {
		t.Errorf("singleton violation = %+v", got[0])
	}
	// The declaring endpoint carries it freely.
	root.server(clBasicInfo)
	if slices.Contains(kindsOf(f.check(l, root)), "singletonMisplaced BasicInformation") {
		t.Error("the root's own singleton reported misplaced")
	}
	// An endpoint in no node scope is not judged for singletons.
	g := newFakeTree()
	orphan := g.add("orphan", nil, dtOnOffLight).server(clBasicInfo)
	if slices.Contains(kindsOf(g.check(l, orphan)), "singletonMisplaced BasicInformation") {
		t.Error("an endpoint without a node scope judged for singletons")
	}
}

func TestDeviceTypeViolationReportedOnce(t *testing.T) {
	t.Parallel()
	l := deviceTypeLookups()
	f, root := standardTree()
	// OnOffLight and DimmableLight both require Identify: one report, the
	// first listed device type's.
	both := f.add("both", root, dtOnOffLight, dtDimmableLight).server(clDescriptor)
	var identify []DeviceTypeViolation
	for _, v := range f.check(l, both) {
		if v.Requirement == "Identify" {
			identify = append(identify, v)
		}
	}
	if len(identify) != 1 || identify[0].DeviceType != "OnOffLight" || identify[0].Endpoint != both.id {
		t.Errorf("Identify reported %+v", identify)
	}
}

func TestDeviceTypeStructuralConditionClasses(t *testing.T) {
	t.Parallel()
	// Application and dynamic device types (none in the snapshot carries
	// either class) make App, and Dynamic for the latter; a Self condition
	// requirement asserts on its own endpoint.
	self := schema.DeviceTypeRequirement{
		Element: schema.RequirementCondition, Name: "Wanted", Type: "Dyn.Wanted", Location: schema.LocationSelf,
		Conformance: confM(), Referent: schema.Referent{Resolved: true, Name: "Wanted", Declarer: "Dyn"},
	}
	l := syntheticModel(
		nil,
		&schema.DeviceTypeDefinition{ID: rootID, Name: "RootNode", Classification: "node"},
		&schema.DeviceTypeDefinition{ID: 0xfff1_0020, Name: "App", Classification: "application"},
		&schema.DeviceTypeDefinition{
			ID: 0xfff1_0021, Name: "Dyn", Classification: "dynamic", Conditions: []string{"Wanted"},
			Requirements: []schema.DeviceTypeRequirement{self, clusterReq("OnOff", 6, confName("Wanted"))},
		},
	)
	f := newFakeTree()
	root := f.add("root", nil, rootID)
	app := f.add("app", root, 0xfff1_0020)
	dyn := f.add("dyn", root, 0xfff1_0021)
	pass := newDTPass(f, l)
	if c := pass.ownStructuralConditions(app.id); !c[condApp] || c[condDynamic] {
		t.Errorf("application conditions = %v", c)
	}
	if c := pass.collect(root.id).conditionsOf(dyn.id); !c[condApp] || !c[condDynamic] || !c["Wanted"] {
		t.Errorf("dynamic conditions = %v", c)
	}
	wantKinds(t, f.check(l, dyn), "missing OnOff")
	// Two application device types under one parent are duplicates.
	f.add("app2", root, 0xfff1_0020)
	if !newDTPass(f, l).isDuplicate(app.id) {
		t.Error("two application endpoints of one type are no duplicates")
	}
}

// dtServer is a cluster server whose global attributes a test sets.
type dtServer struct {
	id       uint32
	features any
	attrs    []uint32
	cmds     []uint32
	events   []uint32
}

func (s dtServer) MatterClusterID() uint32 { return s.id }

func (s dtServer) MatterRead(attrID uint32) (any, bool) {
	if attrID == cluster.AttrGlobalFeatureMap && s.features != nil {
		return s.features, true
	}
	return nil, false
}

func (dtServer) MatterWrite(context.Context, uint32, any) error         { return nil }
func (dtServer) MatterInvoke(context.Context, uint32, any) (any, error) { return nil, nil }
func (s dtServer) MatterReportable() []uint32                           { return s.attrs }
func (s dtServer) MatterAttributes() []uint32                           { return s.attrs }
func (s dtServer) MatterAcceptedCommands() []uint32                     { return s.cmds }
func (dtServer) MatterGeneratedCommands() []uint32                      { return nil }
func (s dtServer) MatterEvents() []uint32                               { return s.events }

// listedServer answers its AttributeList itself, in a shape the facts
// cannot read.
type listedServer struct{ dtServer }

func (s listedServer) MatterRead(attrID uint32) (any, bool) {
	if attrID == cluster.AttrGlobalAttributeList {
		return "not a list", true
	}
	return s.dtServer.MatterRead(attrID)
}

func descriptorFor(t *testing.T, clients []uint32, deviceTypes ...uint32) *mattercore.Descriptor {
	t.Helper()
	dts := make([]mattercore.DeviceTypeStruct, 0, len(deviceTypes))
	for _, id := range deviceTypes {
		dts = append(dts, mattercore.DeviceTypeStruct{DeviceType: id, Revision: 1})
	}
	d, err := mattercore.NewDescriptor(dts, nil, clients, nil)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestTopologyFacts(t *testing.T) {
	t.Parallel()
	root := &Endpoint{ID: 0, DeviceType: dtRootNode}
	root.PublishClusterServers([]contract.ClusterServer{
		descriptorFor(t, []uint32{0x0029}, dtRootNode),
		dtServer{id: clOnOff, features: uint32(1), attrs: []uint32{0x4000}, cmds: []uint32{0x40}, events: []uint32{1}},
		dtServer{id: clFanControl, features: uint16(1 << 3)},
		dtServer{id: 0x0008, features: uint8(3)},
		dtServer{id: 0x0300, features: uint64(1 << 4)},
		dtServer{id: clDoorLock, features: -1},
		dtServer{id: clNetworkComm, features: 4},
		dtServer{id: 0x0102, features: "bogus"},
		listedServer{dtServer{id: clIdentify}},
		nil,
	})
	agg := &Endpoint{ID: 1, DeviceType: dtAggregator}
	orphan := &Endpoint{ID: 7, DeviceType: 0, ParentEndpointID: 9, HasParentEndpointID: true}
	self := &Endpoint{ID: 8, ParentEndpointID: 8, HasParentEndpointID: true}
	f := newTopologyFacts(&Topology{Endpoints: []*Endpoint{root, agg, nil, orphan, self}})

	if _, ok := f.parentOf(0); ok {
		t.Error("the root has a parent")
	}
	if p, ok := f.parentOf(1); !ok || p != 0 {
		t.Error("the aggregator's parent is not the root")
	}
	if _, ok := f.parentOf(7); ok {
		t.Error("an endpoint whose parent is not in the topology has one")
	}
	if _, ok := f.parentOf(8); ok {
		t.Error("an endpoint is its own parent")
	}
	if got := f.partsOf(0); !slices.Equal(got, []uint16{1}) {
		t.Errorf("root parts = %v", got)
	}
	if got := f.deviceTypeIDs(0); !slices.Equal(got, []uint32{dtRootNode}) {
		t.Errorf("root device types = %v", got)
	}
	if got := f.deviceTypeIDs(1); !slices.Equal(got, []uint32{dtAggregator}) {
		t.Errorf("aggregator device types (no Descriptor) = %v", got)
	}
	if f.deviceTypeIDs(7) != nil || f.clientClusters(1) != nil {
		t.Error("an endpoint without device type or Descriptor answers some")
	}
	if got := f.clientClusters(0); !slices.Equal(got, []uint32{0x0029}) {
		t.Errorf("root clients = %v", got)
	}
	if got := f.serverClusters(0); len(got) != 9 || got[0] != clDescriptor {
		t.Errorf("root servers = %v", got)
	}
	for cl, want := range map[uint32][]string{
		clOnOff: {"LT"}, clFanControl: {"WND"}, 0x0008: {"OO", "LT"}, 0x0300: {"CT"},
		clDoorLock: nil, clNetworkComm: {"ET"}, 0x0102: nil, clIdentify: nil, 0x9999: nil,
	} {
		if got := f.features(0, cl); !slices.Equal(got, want) {
			t.Errorf("features of 0x%04X = %v, want %v", cl, got, want)
		}
	}
	if !f.supports(0, clOnOff, schema.RequirementAttribute, 0x4000) || !f.supports(0, clOnOff, schema.RequirementAttribute, cluster.AttrGlobalFeatureMap) ||
		!f.supports(0, clOnOff, schema.RequirementCommand, 0x40) || !f.supports(0, clOnOff, schema.RequirementEvent, 1) ||
		f.supports(0, clOnOff, schema.RequirementFeature, 0) || f.supports(0, 0x9999, schema.RequirementAttribute, 0) ||
		f.supports(0, clIdentify, schema.RequirementAttribute, 0) {
		t.Error("supports answers wrongly")
	}
	if f.supports(0, clDescriptor, schema.RequirementEvent, 0) {
		t.Error("a server that lists no events supports one")
	}
	if f.nodeConditions(0)[0] != condCustomNetworkConfig || f.describe(3) != "endpoint 3" || f.statedConditions(99) != nil {
		t.Error("node conditions, describe or stated conditions answer wrongly")
	}
	if ValidateDeviceTypes(nil) != nil {
		t.Error("a nil topology has violations")
	}
}

// TestValidateDeviceTypesAssembledBridge validates a topology the
// assembler built: a bridged light whose host supplies an OnOff server
// without the Lighting feature.
func TestValidateDeviceTypesAssembledBridge(t *testing.T) {
	t.Parallel()
	a, err := New(&oneEndpointStore{}, Config{VendorID: 1, ProductID: 1, NodeLabel: "x", RootDeviceConditions: []string{conditionLanguage}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	onOff := dtServer{id: clOnOff, features: uint32(0)}
	topo, err := a.Assemble(context.Background(), []Snapshot{{Scope: "s", Endpoints: []Spec{{
		StableKey: StringKey("lamp"), DeviceType: dtOnOffLight, FriendlyName: "Lamp",
		Source:           deviceTypeSource{dt: dtOnOffLight, servers: []contract.ClusterServer{onOff}},
		DeviceConditions: []string{"Nope"},
	}}}})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(topo.FindByID(0).DeviceConditions, []string{conditionLanguage}) {
		t.Fatal("RootDeviceConditions not carried onto the root")
	}
	var lamp []string
	for _, v := range ValidateDeviceTypes(topo) {
		if v.Endpoint == 2 {
			lamp = append(lamp, v.Key())
		}
	}
	// No Groups server without group state, no ScenesManagement; the host's
	// OnOff lacks LT; the stated name is unknown.
	want := []string{"unknownCondition Nope", "missing Groups", "missing OnOff.LT", "missing ScenesManagement"}
	if !slices.Equal(lamp, want) {
		t.Errorf("lamp violations = %v, want %v", lamp, want)
	}
}

func TestDeviceTypeValidatorModes(t *testing.T) {
	t.Parallel()
	topo := func(withSingleton bool) *Topology {
		root := &Endpoint{ID: 0, DeviceType: dtRootNode}
		root.PublishClusterServers([]contract.ClusterServer{descriptorFor(t, nil, dtRootNode)})
		agg := &Endpoint{ID: 1, DeviceType: dtAggregator}
		servers := []contract.ClusterServer{descriptorFor(t, nil, dtAggregator)}
		if withSingleton {
			servers = append(servers, dtServer{id: clBasicInfo})
		}
		agg.PublishClusterServers(servers)
		return &Topology{Endpoints: []*Endpoint{root, agg}}
	}

	warn := NewDeviceTypeValidator(DeviceTypeValidationWarn)
	first, err := warn.Validate(topo(false))
	if err != nil || len(first.Violations) == 0 || len(first.Fresh) != len(first.Violations) {
		t.Fatalf("warn first pass = %+v, %v", first, err)
	}
	second, err := warn.Validate(topo(false))
	if err != nil || len(second.Fresh) != 0 || len(second.Violations) != len(first.Violations) {
		t.Errorf("warn second pass reports again: %+v, %v", second, err)
	}
	if got := warn.Violations(); len(got) != len(first.Violations) {
		t.Errorf("recorded = %d, want %d", len(got), len(first.Violations))
	}
	// A misplaced singleton is refused even in warn mode, and nothing is
	// recorded.
	_, err = warn.Validate(topo(true))
	var refusal *DeviceTypeConformanceError
	if !errors.As(err, &refusal) || len(refusal.Violations) != 1 || refusal.Violations[0].Kind != ViolationSingletonMisplaced {
		t.Fatalf("warn refusal = %v", err)
	}
	if !strings.Contains(err.Error(), "endpoint(s) 1:") || !strings.Contains(err.Error(), "singletonMisplaced RootNode BasicInformation") {
		t.Errorf("refusal message = %q", err)
	}
	if got := warn.Violations(); len(got) != len(first.Violations) {
		t.Error("a refused pass was recorded")
	}

	strict := NewDeviceTypeValidator(DeviceTypeValidationStrict)
	if _, err := strict.Validate(topo(false)); !errors.As(err, &refusal) || len(strict.Violations()) != 0 {
		t.Errorf("strict accepted a non-conforming topology: %v", err)
	}
	if _, err := strict.Validate(&Topology{}); err != nil {
		t.Errorf("strict refused an empty topology: %v", err)
	}

	off := NewDeviceTypeValidator(DeviceTypeValidationOff)
	if v, err := off.Validate(topo(false)); err != nil || len(v.Violations) != 0 || len(off.Violations()) != 0 {
		t.Errorf("off judged: %+v, %v", v, err)
	}
	if _, err := off.Validate(topo(true)); !errors.As(err, &refusal) {
		t.Error("off accepted a misplaced singleton")
	}

	for mode, want := range map[DeviceTypeValidationMode]string{
		DeviceTypeValidationWarn: "warn", DeviceTypeValidationStrict: "strict", DeviceTypeValidationOff: "off",
	} {
		if mode.String() != want || NewDeviceTypeValidator(mode).Mode() != mode {
			t.Errorf("mode %d = %q", mode, mode.String())
		}
	}
	if v := (DeviceTypeViolation{Endpoint: 3, Kind: ViolationMissing, DeviceType: "X", Requirement: "Y", Detail: "Z"}); v.String() != "endpoint 3: missing X Y: Z" || v.Key() != "missing Y" {
		t.Errorf("rendering = %q / %q", v.String(), v.Key())
	}
}

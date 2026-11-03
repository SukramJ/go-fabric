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
// DeviceTypeConformanceServiceTest.ts at 862ad75f: the service's logging,
// recording and refusal over the Go DeviceTypeValidator, each state of a
// fake tree judged as one reassembly (service, devicetype_service_helpers_
// test.go). matter.js validates one endpoint on request; the Go validator
// judges the whole tree, so a case that validates one endpoint judges a
// tree holding it beside a conforming node endpoint.

const dtCamera = 0x0142

// lightWithoutIdentifyAndScenes is DeviceTypeConformanceServiceTest's light
// without Identify and ScenesManagement (validation-helpers.ts).
func lightWithoutIdentifyAndScenes(f *fakeTree, parent *fakeEP, name string) *fakeEP {
	return f.add(name, parent, dtOnOffLight).server(clDescriptor).server(clGroups).server(clOnOff, featureOnOffLT)
}

// cameraNode is a node with a stand-in Camera — a light without Identify
// whose DeviceTypeList lists Camera, which asserts TimeSyncWithNtpcCond and
// the TLS conditions at the Root (DeviceTypeConformanceServiceTest.ts:229-232).
func cameraNode() (*fakeTree, *fakeEP) {
	f, root := nodeTree()
	camera := lightWithoutIdentify(f, root, "camera")
	camera.deviceTypes = []uint32{dtCamera}
	return f, root
}

// keysAt are the keys of vs on ep.
func keysAt(vs []DeviceTypeViolation, ep *fakeEP) []string {
	out := []string{}
	for i := range vs {
		if vs[i].Endpoint == ep.id {
			out = append(out, vs[i].Key())
		}
	}
	return out
}

// linesAt are the lines of lines that name ep.
func linesAt(lines []string, ep *fakeEP) []string {
	var out []string
	for _, l := range lines {
		if strings.HasPrefix(l, "Endpoint "+ep.name+" ") {
			out = append(out, l)
		}
	}
	return out
}

func TestDeviceTypeConformanceServiceParityMatterJS(t *testing.T) {
	t.Parallel()
	std := deviceTypeLookups()

	// :63-77: one line for the endpoint, naming each violation.
	t.Run("logs one warning listing every violation of an endpoint", func(t *testing.T) {
		f, root := nodeTree()
		light := lightWithoutIdentifyAndScenes(f, root, "light")
		s := newService(t, DeviceTypeValidationWarn, std, f)
		logged := s.mustPass()
		if len(logged) != 1 || !strings.Contains(logged[0], "missing OnOffLight Identify") || !strings.Contains(logged[0], "missing OnOffLight ScenesManagement") {
			t.Fatalf("logged %q", logged)
		}
		vs := s.v.Violations()
		if len(keysAt(vs, light)) == 0 || vs[0].Detail != "Mandatory server cluster Identify is missing" {
			t.Errorf("recorded %v", vs)
		}
	})

	// :102-117: DimmableLight adds the LevelControl the light lacks.
	t.Run("logs only the new violation of an endpoint it reported before", func(t *testing.T) {
		f, root := nodeTree()
		light := lightWithoutIdentify(f, root, "light")
		s := newService(t, DeviceTypeValidationWarn, std, f)
		s.mustPass()
		light.deviceTypes = []uint32{dtOnOffLight, dtDimmableLight}
		logged := s.mustPass()
		if len(logged) != 1 || !strings.Contains(logged[0], "LevelControl") || strings.Contains(logged[0], "Identify") {
			t.Errorf("logged %q", logged)
		}
	})

	// :119-134: without a device type the endpoint violates nothing.
	t.Run("logs a violation again once it disappeared and returned", func(t *testing.T) {
		f, root := nodeTree()
		light := lightWithoutIdentify(f, root, "light")
		s := newService(t, DeviceTypeValidationWarn, std, f)
		s.mustPass()
		light.deviceTypes = nil
		if logged := s.mustPass(); len(logged) != 0 {
			t.Errorf("logged %q", logged)
		}
		wantRequirements(t, s.requirementsOf(light))
		light.deviceTypes = []uint32{dtOnOffLight}
		if logged := s.mustPass(); len(logged) != 1 {
			t.Errorf("logged %q, want one line", logged)
		}
	})

	// :136-146: the mode a node takes without one stated is warn — Go's
	// zero DeviceTypeValidationMode (node.Config.DeviceTypeValidation,
	// Bridge's default validator).
	t.Run("is in warn mode unless the environment says otherwise", func(t *testing.T) {
		var unset DeviceTypeValidationMode
		if unset != DeviceTypeValidationWarn || unset.String() != "warn" {
			t.Fatalf("zero mode = %v", unset)
		}
		f, root := nodeTree()
		lightWithoutIdentify(f, root, "light")
		if logged := newService(t, unset, std, f).mustPass(); len(logged) != 1 {
			t.Errorf("logged %q", logged)
		}
	})

	// :171-184: judging on request is ValidateDeviceTypes (here the
	// service's verdictOf), which records nothing in any mode; an off-mode
	// validator records nothing either, and judging again finds the same.
	t.Run("judges on request in off mode, like warn mode, but records nothing", func(t *testing.T) {
		f, root := nodeTree()
		light := lightWithoutIdentify(f, root, "light")
		s := newService(t, DeviceTypeValidationOff, std, f)
		if got := keysAt(s.verdictOf(), light); !slices.Equal(got, []string{"missing Identify"}) {
			t.Errorf("verdict = %v", got)
		}
		if logged := s.mustPass(); len(logged) != 0 {
			t.Errorf("off mode logged %q", logged)
		}
		wantRequirements(t, s.requirementsOf(light))
		if got := keysAt(s.verdictOf(), light); !slices.Equal(got, []string{"missing Identify"}) {
			t.Errorf("second verdict = %v", got)
		}
	})

	// :186-200: the verdict holds every current violation; only the
	// unreported ones are fresh.
	t.Run("returns every violation of each judged endpoint, reported before or not", func(t *testing.T) {
		f, root := nodeTree()
		light := lightWithoutIdentify(f, root, "light")
		s := newService(t, DeviceTypeValidationWarn, std, f)
		s.mustPass()
		shape := treeShape{parent: map[uint16]uint16{light.id: root.id}, order: []uint16{root.id, light.id}}
		verdict, err := s.v.judge(s.verdictOf(), shape)
		if err != nil {
			t.Fatal(err)
		}
		if len(verdict.Fresh) != 0 {
			t.Errorf("fresh %v", verdict.Fresh)
		}
		if got := keysAt(verdict.Violations, light); !slices.Equal(got, []string{"missing Identify"}) {
			t.Errorf("verdict = %v", got)
		}
		wantRequirements(t, s.requirementsOf(light), "missing Identify")
	})

	// :229-247: Camera's Root assertions make the node endpoint owe
	// TlsCertificateManagement; flagged, apart from the warnings, never
	// refused even when strict.
	t.Run("logs at debug and never refuses what only a Root assertion requires, even when strict", func(t *testing.T) {
		f, root := cameraNode()
		s := newService(t, DeviceTypeValidationStrict, std, f)
		all := onlyAt(s.verdictOf(), root)
		var flagged []string
		for i := range all {
			if all[i].AssertedAtRoot {
				flagged = append(flagged, all[i].Requirement)
			}
		}
		if !slices.Contains(flagged, "TlsCertificateManagement") {
			t.Fatalf("flagged %v", flagged)
		}
		verdict, err := s.v.judge(all, shapeOf(f))
		if err != nil {
			t.Fatalf("refused: %v", err)
		}
		if len(verdict.Fresh) != 0 {
			t.Errorf("warned %v", verdict.Fresh)
		}
		if got := keysAt(verdict.FreshAssertedAtRoot, root); !slices.Contains(got, "missing TlsCertificateManagement") {
			t.Errorf("reported apart: %v", got)
		}
	})

	// :249-266: BatteryStorage requires PowerSource components whatever
	// holds, so device:PowerSource binds once the root lists it.
	t.Run("warns of a violation once it no longer only follows from a Root assertion", func(t *testing.T) {
		f, root := cameraNode()
		s := newService(t, DeviceTypeValidationWarn, std, f)
		verdict, err := s.v.judge(onlyAt(s.verdictOf(), root), shapeOf(f))
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Contains(keysAt(verdict.FreshAssertedAtRoot, root), "instanceCount device:PowerSource") || slices.Contains(keysAt(verdict.Fresh, root), "instanceCount device:PowerSource") {
			t.Fatalf("first pass: fresh %v, root-asserted %v", keysAt(verdict.Fresh, root), keysAt(verdict.FreshAssertedAtRoot, root))
		}
		root.deviceTypes = []uint32{dtRootNode, dtBatteryStorage}
		verdict, err = s.v.judge(onlyAt(s.verdictOf(), root), shapeOf(f))
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Contains(keysAt(verdict.Fresh, root), "instanceCount device:PowerSource") {
			t.Errorf("PowerSource not warned of once it binds: fresh %v", keysAt(verdict.Fresh, root))
		}
		if slices.Contains(keysAt(verdict.FreshAssertedAtRoot, root), "instanceCount device:PowerSource") {
			t.Error("PowerSource still reported as Root-asserted")
		}
	})

	// :268-287: OnOffLight on the root adds the OnOff it lacks — warned;
	// TlsCertificateManagement apart.
	t.Run("logs the violations a Root assertion explains apart from the others", func(t *testing.T) {
		f, root := cameraNode()
		root.deviceTypes = []uint32{dtRootNode, dtOnOffLight}
		s := newService(t, DeviceTypeValidationWarn, std, f)
		verdict, err := s.v.judge(onlyAt(s.verdictOf(), root), shapeOf(f))
		if err != nil {
			t.Fatal(err)
		}
		warned, apart := keysAt(verdict.Fresh, root), keysAt(verdict.FreshAssertedAtRoot, root)
		if !slices.Contains(warned, "missing OnOff") || slices.Contains(warned, "missing TlsCertificateManagement") {
			t.Errorf("warned %v", warned)
		}
		if !slices.Contains(apart, "missing TlsCertificateManagement") || slices.Contains(apart, "missing OnOff") {
			t.Errorf("apart %v", apart)
		}
	})

	// :289-312.
	t.Run("refuses an endpoint only for the violations no Root assertion explains", func(t *testing.T) {
		f, root := cameraNode()
		root.deviceTypes = []uint32{dtRootNode, dtOnOffLight}
		s := newService(t, DeviceTypeValidationStrict, std, f)
		_, err := s.v.judge(onlyAt(s.verdictOf(), root), shapeOf(f))
		var refusal *DeviceTypeConformanceError
		if !errors.As(err, &refusal) {
			t.Fatalf("err = %v", err)
		}
		var requirements []string
		for _, v := range refusal.Violations {
			requirements = append(requirements, v.Requirement)
		}
		if !slices.Contains(requirements, "OnOff") || slices.Contains(requirements, "TlsCertificateManagement") {
			t.Errorf("refused for %v", requirements)
		}
	})

	// :314-324.
	t.Run("refuses an endpoint again until it conforms", func(t *testing.T) {
		f, root := nodeTree()
		light := lightWithoutIdentify(f, root, "light")
		s := newService(t, DeviceTypeValidationStrict, std, f)
		for range 2 {
			if _, err := s.pass(); err == nil {
				t.Fatal("not refused")
			}
			wantRequirements(t, s.requirementsOf(light))
		}
	})

	// :356-363: matter.js judges a runtime change with { refuse: false }
	// (DeviceTypeConformanceService.ts:295, :335-338 at 862ad75f); the Go
	// validator has no per-call option and judges a change of an installed
	// endpoint the same way — a strict validator reports what it violates
	// rather than refusing it.
	t.Run("logs rather than throws in strict mode when told not to refuse", func(t *testing.T) {
		f, root := nodeTree()
		lt := light(f, root, "light")
		s := newService(t, DeviceTypeValidationStrict, std, f)
		s.mustPass()
		lt.servers = map[uint32][]string{clDescriptor: nil, clGroups: nil, clOnOff: {featureOnOffLT}, clScenes: nil}
		lt.order = []uint32{clDescriptor, clGroups, clOnOff, clScenes}
		logged, err := s.pass()
		if err != nil {
			t.Fatalf("refused a change: %v", err)
		}
		if len(logged) != 1 || !strings.Contains(logged[0], "missing OnOffLight Identify") {
			t.Errorf("logged %q", logged)
		}
	})

	// :365-374: GroupKeyManagement, a RootNode singleton, on a light.
	t.Run("throws a misplaced singleton when validation is not strict", func(t *testing.T) {
		f, root := nodeTree()
		light(f, root, "light").server(clGroupKeyMgmt)
		s := newService(t, DeviceTypeValidationWarn, std, f)
		if _, err := s.pass(); err == nil {
			t.Fatal("not refused")
		}
	})

	// :376-407: both lights in the refusal, the first first, nothing
	// recorded, each refused again.
	t.Run("throws the first refused endpoint carrying the others and logs none", func(t *testing.T) {
		f, root := nodeTree()
		first, second := lightWithoutIdentify(f, root, "first"), lightWithoutIdentify(f, root, "second")
		s := newService(t, DeviceTypeValidationStrict, std, f)
		_, err := s.pass()
		if got := refusalNames(f, err); !slices.Equal(got, []string{"first", "second"}) {
			t.Fatalf("refused %v (%v)", got, err)
		}
		var refusal *DeviceTypeConformanceError
		if errors.As(err, &refusal) {
			found := false
			for _, v := range refusal.Violations {
				found = found || (v.Endpoint == second.id && v.DeviceType+" "+v.Requirement+": "+v.Detail == "OnOffLight Identify: Mandatory server cluster Identify is missing")
			}
			if !found {
				t.Errorf("second's Identify violation missing: %v", refusal.Violations)
			}
		}
		if len(s.requirementsOf(first)) != 0 || len(s.requirementsOf(second)) != 0 {
			t.Error("a refusal recorded violations")
		}
		if _, err := s.pass(); err == nil {
			t.Error("not refused again")
		}
	})

	// :409-420: a refused addition of a child leaves its parent's record
	// empty.
	t.Run("records nothing for an addition it refuses", func(t *testing.T) {
		f, root := nodeTree()
		parent := lightWithoutIdentify(f, root, "parent")
		lightWithoutIdentify(f, parent, "child")
		s := newService(t, DeviceTypeValidationStrict, std, f)
		if _, err := s.pass(); err == nil {
			t.Fatal("not refused")
		}
		wantRequirements(t, s.requirementsOf(parent))
	})

	// :422-435.
	t.Run("validates every endpoint of a node scope", func(t *testing.T) {
		f, root := nodeTree()
		first, second := lightWithoutIdentify(f, root, "first"), lightWithoutIdentify(f, root, "second")
		s := newService(t, DeviceTypeValidationWarn, std, f)
		logged := s.mustPass()
		if len(linesAt(logged, first)) != 1 || len(linesAt(logged, second)) != 1 {
			t.Errorf("logged %q", logged)
		}
		if len(s.requirementsOf(first)) == 0 || len(s.requirementsOf(second)) == 0 {
			t.Error("not recorded")
		}
	})

	// :437-447 with modelWithoutNodes (:41-55): RootNode is simple, so no
	// endpoint is in a node scope.
	t.Run("does not judge an endpoint in no node scope", func(t *testing.T) {
		l := syntheticModel(
			&schema.DeviceTypeDefinition{Name: "Base", Classification: "base"},
			&schema.DeviceTypeDefinition{ID: rootID, Name: "RootNode", Classification: "simple"},
			&schema.DeviceTypeDefinition{ID: dtOnOffLight, Name: "OnOffLight", Classification: "simple", Requirements: []schema.DeviceTypeRequirement{
				clusterReq("Identify", clIdentify, confM()),
			}},
		)
		f, root := syntheticNode()
		light := lightWithoutIdentify(f, root, "light")
		s := newService(t, DeviceTypeValidationWarn, l, f)
		if logged := s.mustPass(); len(logged) != 0 {
			t.Errorf("logged %q", logged)
		}
		wantRequirements(t, s.requirementsOf(light))
	})

	// :460-472: an endpoint the tree loses is forgotten; added back, its
	// violations are reported again.
	t.Run("reports an endpoint's violations again once it is forgotten", func(t *testing.T) {
		f, root := nodeTree()
		light := lightWithoutIdentify(f, root, "light")
		s := newService(t, DeviceTypeValidationWarn, std, f)
		s.mustPass()
		f.remove(light)
		s.mustPass()
		wantRequirements(t, s.requirementsOf(light))
		again := lightWithoutIdentify(f, root, "light")
		if logged := linesAt(s.mustPass(), again); len(logged) != 1 {
			t.Errorf("logged %q, want one line", logged)
		}
	})

	// :474-486: node.erase(). A Go node.Node.FactoryReset ends the node; the
	// node the host builds after it installs a fresh validator
	// (Bridge.SetDeviceTypeValidation, NewDeviceTypeValidator), so every
	// endpoint is reported again — once.
	t.Run("reports every endpoint again after a factory reset", func(t *testing.T) {
		f, root := nodeTree()
		light := lightWithoutIdentify(f, root, "light")
		s := newService(t, DeviceTypeValidationWarn, std, f)
		s.mustPass()
		if len(s.requirementsOf(light)) == 0 {
			t.Fatal("nothing recorded before the reset")
		}
		after := newService(t, DeviceTypeValidationWarn, std, f)
		if logged := linesAt(after.mustPass(), light); len(logged) != 1 {
			t.Errorf("after the reset logged %q, want one line", logged)
		}
		if logged := after.mustPass(); len(logged) != 0 {
			t.Errorf("logged again %q", logged)
		}
	})
}

// onlyAt keeps vs's violations of ep — the camera stand-in's own aside, as
// matter.js's cases validate the node endpoint alone (service.validate(node)).
func onlyAt(vs []DeviceTypeViolation, ep *fakeEP) []DeviceTypeViolation {
	out := []DeviceTypeViolation{}
	for i := range vs {
		if vs[i].Endpoint == ep.id {
			out = append(out, vs[i])
		}
	}
	return out
}

// shapeOf is f's shape as one reassembly reads it.
func shapeOf(f *fakeTree) treeShape {
	shape := treeShape{parent: map[uint16]uint16{}}
	for _, ep := range f.order() {
		shape.order = append(shape.order, ep.id)
		if ep.parent != nil {
			shape.parent[ep.id] = ep.parent.id
		}
	}
	return shape
}

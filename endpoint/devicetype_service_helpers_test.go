// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package endpoint

import (
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/SukramJ/go-fabric/schema"
)

// The shared support of the ports of matter.js's device-type validation
// service tests (packages/node/test/endpoint/validation/
// validation-helpers.ts at 862ad75f): fake trees standing in for a node's
// endpoints, and a service that judges each state of a tree as one
// reassembly — the Go validator judges the whole tree once per Start or
// Reassemble (ADR 0016), where matter.js judges what each construction
// and each runtime change affects.

// Server clusters of the fakes below.
const (
	clAccessControl  = 0x001F
	clGeneralComm    = 0x0030
	clGeneralDiag    = 0x0033
	clOpCreds        = 0x003E
	clGroupKeyMgmt   = 0x003F
	clBridgedBasic   = 0x0039
	clTempControl    = 0x0056
	clTempMeasure    = 0x0402
	clColorControl   = 0x0300
	dtTempSensor     = 0x0302
	dtOnOffLightSwch = 0x0103
)

// nodeTree is a fake node whose root serves what RootNode mandates, so it
// conforms as matter.js's MockServerNode root does (createNode,
// validation-helpers.ts:71-77).
func nodeTree() (*fakeTree, *fakeEP) {
	f, root := standardTree()
	root.server(clDescriptor).server(clAccessControl, "AUX").server(clBasicInfo).server(clGeneralComm).
		server(clGeneralDiag).server(clAdminComm).server(clOpCreds).server(clGroupKeyMgmt).server(clGroupcast, "LN")
	return f, root
}

// lightWithoutIdentify is an OnOffLight without its Identify server
// (validation-helpers.ts:59 lightWithoutIdentify).
func lightWithoutIdentify(f *fakeTree, parent *fakeEP, name string) *fakeEP {
	return f.add(name, parent, dtOnOffLight).
		server(clDescriptor).server(clGroups).server(clOnOff, featureOnOffLT).server(clScenes).
		implements(clScenes, schema.RequirementCommand, cmdCopyScene)
}

// tempSensor is a TemperatureSensor that conforms.
func tempSensor(f *fakeTree, parent *fakeEP, name string) *fakeEP {
	return f.add(name, parent, dtTempSensor).server(clDescriptor).server(clIdentify).server(clTempMeasure).
		implements(clIdentify, schema.RequirementCommand, cmdTriggerEffect)
}

// fridgeOf is a Refrigerator with cabinets TemperatureControlledCabinets
// (validation-helpers.ts:191-213 addRefrigerator).
func fridgeOf(f *fakeTree, parent *fakeEP, name string, n int) (fridge *fakeEP, cabinets []*fakeEP) {
	fridge = f.add(name, parent, dtRefrigerator).server(clDescriptor)
	cabinets = make([]*fakeEP, 0, n)
	for i := range n {
		cabinets = append(cabinets, cabinetOf(f, fridge, "cabinet"+strconv.Itoa(i)))
	}
	return fridge, cabinets
}

// cabinetOf is a TemperatureControlledCabinet (validation-helpers.ts:215-228
// CabinetDevice, addCabinet).
func cabinetOf(f *fakeTree, parent *fakeEP, name string) *fakeEP {
	return f.add(name, parent, dtCabinet).server(clDescriptor).server(clTempControl, "TN")
}

// aggregatorOf is an Aggregator.
func aggregatorOf(f *fakeTree, parent *fakeEP, name string) *fakeEP {
	return f.add(name, parent, dtAggregator).server(clDescriptor)
}

// standIn is an endpoint whose Descriptor lists only deviceTypes
// (RuntimeValidationTest.ts:237 addStandIn): a DescribedLight's servers.
func standIn(f *fakeTree, parent *fakeEP, name string, deviceTypes ...uint32) *fakeEP {
	ep := light(f, parent, name)
	ep.deviceTypes = deviceTypes
	return ep
}

// remove takes ep and its descendants out of the tree — an endpoint's
// close() or delete(), which a snapshot without it is in Go.
func (f *fakeTree) remove(ep *fakeEP) {
	for _, p := range slices.Clone(ep.parts) {
		f.remove(p)
	}
	if ep.parent != nil {
		ep.parent.parts = slices.DeleteFunc(ep.parent.parts, func(p *fakeEP) bool { return p == ep })
	}
	delete(f.eps, ep.id)
}

// order is the tree's endpoints in tree order: each root, then its parts
// depth first.
func (f *fakeTree) order() []*fakeEP {
	var out []*fakeEP
	var walk func(ep *fakeEP)
	walk = func(ep *fakeEP) {
		out = append(out, ep)
		for _, p := range ep.parts {
			walk(p)
		}
	}
	ids := make([]uint16, 0, len(f.eps))
	for id := range f.eps {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	for _, id := range ids {
		if ep := f.eps[id]; ep.parent == nil {
			walk(ep)
		}
	}
	return out
}

// service is matter.js's DeviceTypeConformanceService over a fake tree:
// a [DeviceTypeValidator] that judges each state of the tree as one
// reassembly, in a model.
type service struct {
	t *testing.T
	v *DeviceTypeValidator
	l *dtLookups
	f *fakeTree
}

func newService(t *testing.T, mode DeviceTypeValidationMode, l *dtLookups, f *fakeTree) *service {
	t.Helper()
	return &service{t: t, v: NewDeviceTypeValidator(mode), l: l, f: f}
}

// verdictOf judges the tree as it is now, without recording: what the
// pass finds, endpoint by endpoint in tree order.
func (s *service) verdictOf() []DeviceTypeViolation {
	pass := newDTPass(s.f, s.l)
	all := make([]DeviceTypeViolation, 0, len(s.f.eps))
	for _, ep := range s.f.order() {
		all = append(all, pass.judged(ep.id)...)
	}
	return all
}

// pass judges the tree as one reassembly and returns what is logged — one
// line per endpoint with new violations, as matter.js logs "Endpoint …
// violates device type requirements" once per endpoint
// (DeviceTypeConformanceService.ts:433-436) — and the refusal.
func (s *service) pass() (logged []string, err error) {
	shape := treeShape{parent: map[uint16]uint16{}}
	for _, ep := range s.f.order() {
		shape.order = append(shape.order, ep.id)
		if ep.parent != nil {
			shape.parent[ep.id] = ep.parent.id
		}
	}
	verdict, err := s.v.judge(s.verdictOf(), shape)
	if err != nil {
		return nil, err
	}
	return linesOf(s.f, verdict.Fresh), nil
}

// mustPass is pass, failing the test on a refusal.
func (s *service) mustPass() []string {
	s.t.Helper()
	logged, err := s.pass()
	if err != nil {
		s.t.Fatalf("refused: %v", err)
	}
	return logged
}

// linesOf groups violations into one log line per endpoint, in order.
func linesOf(f *fakeTree, vs []DeviceTypeViolation) []string {
	var out []string
	last := -1
	for i := range vs {
		v := &vs[i]
		text := string(v.Kind) + " " + v.DeviceType + " " + v.Requirement
		if int(v.Endpoint) == last {
			out[len(out)-1] += "; " + text
			continue
		}
		last = int(v.Endpoint)
		name := ""
		if ep := f.eps[v.Endpoint]; ep != nil {
			name = ep.name
		}
		out = append(out, "Endpoint "+name+" violates device type requirements: "+text)
	}
	return out
}

// requirementsOf is what the last recorded pass found on ep, as
// `${kind} ${requirement}` (RuntimeValidationTest.ts:283 requirementsOf).
func (s *service) requirementsOf(ep *fakeEP) []string {
	out := []string{}
	vs := s.v.Violations()
	for i := range vs {
		if vs[i].Endpoint == ep.id {
			out = append(out, vs[i].Key())
		}
	}
	return out
}

func wantRequirements(t *testing.T, got []string, want ...string) {
	t.Helper()
	if want == nil {
		want = []string{}
	}
	if !slices.Equal(got, want) {
		t.Errorf("requirements = %v, want %v", got, want)
	}
}

// refusalNames reports the endpoints a refusal names, by fake name.
func refusalNames(f *fakeTree, err error) []string {
	var out []string
	refusal, ok := err.(*DeviceTypeConformanceError) //nolint:errorlint // the validator returns it unwrapped
	if !ok {
		return nil
	}
	for i := range refusal.Violations {
		name := f.eps[refusal.Violations[i].Endpoint].name
		if !slices.Contains(out, name) {
			out = append(out, name)
		}
	}
	return out
}

func joined(lines []string) string { return strings.Join(lines, "\n") }

// condReq is a condition requirement asserting name on the node endpoint
// (location Root) under conformance.
func condReq(declarer, name string, c schema.Conformance) schema.DeviceTypeRequirement {
	return schema.DeviceTypeRequirement{
		Element: schema.RequirementCondition, Name: name, Type: declarer + "." + name, Location: schema.LocationRoot,
		Conformance: c, Referent: schema.Referent{Resolved: true, Name: name, Declarer: declarer},
	}
}

// singletonReq is a server cluster requirement of quality I.
func singletonReq(name string, id uint32, c schema.Conformance) schema.DeviceTypeRequirement {
	r := clusterReq(name, id, c)
	r.Singleton = true
	return r
}

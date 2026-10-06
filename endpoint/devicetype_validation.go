// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package endpoint

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/SukramJ/go-fabric/cluster"
	mattercore "github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/schema"
)

// ViolationKind is the kind of a device type violation: matter.js's
// DeviceTypeViolation.Kind verbatim
// (packages/model/src/logic/device-types/DeviceTypeViolation.ts).
type ViolationKind string

// Violation kinds.
const (
	// ViolationMissing: a mandatory cluster or element is absent.
	ViolationMissing ViolationKind = "missing"
	// ViolationDisallowed: a cluster, element or component device type is
	// present that its conformance forbids whatever conditions hold (an X,
	// a D or its feature terms; a false condition never disallows).
	ViolationDisallowed ViolationKind = "disallowed"
	// ViolationInstanceCount: a component requirement, a choice of them or
	// a Descendant condition matches too few or too many endpoints, or an
	// instance of a component requirement is filled by none.
	ViolationInstanceCount ViolationKind = "instanceCount"
	// ViolationSingletonMisplaced: a server cluster a device type in the
	// node scope declares a singleton is on an endpoint that neither
	// declares it nor lists it.
	ViolationSingletonMisplaced ViolationKind = "singletonMisplaced"
	// ViolationUnknownCondition: a condition the host states for the
	// endpoint names no condition in its scope.
	ViolationUnknownCondition ViolationKind = "unknownCondition"
)

// DeviceTypeViolation is one departure of an endpoint from a requirement of
// a device type it lists (or of the Base device type). The first five
// fields are matter.js's DeviceTypeViolation; the rest locate the
// requirement for a host that acts on it.
type DeviceTypeViolation struct {
	// Endpoint is the endpoint that departs.
	Endpoint uint16
	// DeviceType names the device type whose requirement is violated: one
	// the endpoint lists, "Base", the device type declaring a misplaced
	// singleton, the composing device type of a component, or the
	// asserting device type of a Descendant condition count.
	DeviceType string
	// DeviceTypeID is that device type's id; zero for Base and where the
	// violation names none.
	DeviceTypeID uint32
	// Requirement is matter.js's requirement path: "Identify", "OnOff.LT",
	// "client:OnOff", "Identify.TriggerEffect", "device:PowerSource",
	// "device:ElectricalSensor#2", "condition:Cooler", or the stated
	// condition name of an unknown condition.
	Requirement string
	Kind        ViolationKind
	// Detail describes the departure for a developer, as matter.js words it.
	Detail string

	// Cluster is the cluster the requirement concerns, where it concerns
	// one (HasCluster).
	Cluster    uint32
	HasCluster bool
	// Element is the kind of an element requirement (feature, attribute,
	// command, event); empty for a cluster, component or condition
	// requirement. ElementName is the feature code or element name,
	// ElementID the attribute, command or event id.
	Element     schema.RequirementElement
	ElementName string
	ElementID   uint32
	// Conformance is the requirement's conformance as matter.js prints it;
	// empty when it states none.
	Conformance string
	// Conditions are the names in that conformance that held for the
	// endpoint — the conditions (and features) that made it apply.
	Conditions []string
}

// Key identifies the violation on its endpoint: its kind and requirement
// path (matter.js DeviceTypeViolation.keyOf). Two violations with one key
// report one departure.
func (v DeviceTypeViolation) Key() string { return string(v.Kind) + " " + v.Requirement }

// String renders the violation as matter.js logs it, prefixed with the
// endpoint.
func (v DeviceTypeViolation) String() string {
	return fmt.Sprintf("endpoint %d: %s %s %s: %s", v.Endpoint, v.Kind, v.DeviceType, v.Requirement, v.Detail)
}

// ValidateDeviceTypes judges every endpoint of t against the device types
// it lists in its Descriptor DeviceTypeList, and the Base device type, in
// one pass — matter.js's DeviceTypeConformanceService.validateNodeScope
// over DeviceTypeConformance.check (packages/node/src/node/server/
// DeviceTypeConformanceService.ts, packages/model/src/logic/device-types/
// DeviceTypeConformance.ts). It returns every violation, endpoint by
// endpoint in topology order, each endpoint's in the order matter.js finds
// them. It neither logs nor refuses; [DeviceTypeValidator] adds what
// matter.js's service does with a verdict.
//
// What the endpoint offers is read as a controller reads it: the server
// clusters [ClusterServers] mounts (the root's and the Aggregator's as
// published), each one's FeatureMap, AttributeList and AcceptedCommandList
// (synthesised as the dispatcher synthesises them), its event list, and
// the Descriptor's DeviceTypeList and ClientList.
//
// Conditions are decided as matter.js decides them: structurally from the
// tree (Node, App, Simple, Dynamic, Composed, Client, Server, Duplicate),
// from condition requirements the device types assert (Root, Self,
// Descendant), from the node's configuration — CustomNetworkConfig always,
// because this module never commissions over BLE, and Ethernet / WiFi /
// Thread from the features of a NetworkCommissioning server in the node
// scope — and from [Endpoint.DeviceConditions], what the host states. Every
// other condition is not true, and a condition that is not true never
// makes a requirement disallowed: what only a condition could require is
// left unjudged, as the certification harness leaves it.
func ValidateDeviceTypes(t *Topology) []DeviceTypeViolation {
	if t == nil {
		return nil
	}
	facts := newTopologyFacts(t)
	pass := newDTPass(facts, deviceTypeLookups())
	var out []DeviceTypeViolation
	for _, ep := range t.Endpoints {
		if ep == nil {
			continue
		}
		out = append(out, pass.check(ep.ID)...)
	}
	return out
}

// topologyFacts answers dtFacts for an assembled topology.
type topologyFacts struct {
	t         *Topology
	byID      map[uint16]*Endpoint
	children  map[uint16][]uint16
	servers   map[uint16][]contract.ClusterServer
	descripts map[uint16]*mattercore.Descriptor
}

func newTopologyFacts(t *Topology) *topologyFacts {
	f := &topologyFacts{
		t: t, byID: map[uint16]*Endpoint{}, children: map[uint16][]uint16{},
		servers: map[uint16][]contract.ClusterServer{}, descripts: map[uint16]*mattercore.Descriptor{},
	}
	for _, ep := range t.Endpoints {
		if ep != nil {
			f.byID[ep.ID] = ep
		}
	}
	for _, ep := range t.Endpoints {
		if ep == nil {
			continue
		}
		if parent, ok := f.parentOf(ep.ID); ok {
			f.children[parent] = append(f.children[parent], ep.ID)
		}
		servers := ClusterServers(ep)
		f.servers[ep.ID] = servers
		for _, srv := range servers {
			if d, ok := srv.(*mattercore.Descriptor); ok {
				f.descripts[ep.ID] = d
			}
		}
	}
	return f
}

// parentOf: a bridged endpoint's ParentEndpointID; the root for every other
// endpoint but the root itself, which owns the tree.
func (f *topologyFacts) parentOf(ep uint16) (uint16, bool) {
	e := f.byID[ep]
	if e == nil || e.IsRoot() {
		return 0, false
	}
	parent := uint16(0)
	if e.HasParentEndpointID {
		parent = e.ParentEndpointID
	}
	if _, ok := f.byID[parent]; !ok || parent == ep {
		return 0, false
	}
	return parent, true
}

func (f *topologyFacts) partsOf(ep uint16) []uint16 { return f.children[ep] }

// deviceTypeIDs: the Descriptor's DeviceTypeList, as a controller reads it;
// the endpoint's DeviceType when it serves no Descriptor.
func (f *topologyFacts) deviceTypeIDs(ep uint16) []uint32 {
	if d := f.descripts[ep]; d != nil {
		if v, ok := d.MatterRead(0x0000); ok {
			if list, ok := v.([]mattercore.DeviceTypeStruct); ok {
				ids := make([]uint32, 0, len(list))
				for _, dt := range list {
					ids = append(ids, dt.DeviceType)
				}
				return ids
			}
		}
	}
	if e := f.byID[ep]; e != nil && e.DeviceType != 0 {
		return []uint32{uint32(e.DeviceType)}
	}
	return nil
}

func (f *topologyFacts) serverClusters(ep uint16) []uint32 {
	var ids []uint32
	for _, srv := range f.servers[ep] {
		if srv != nil && !slices.Contains(ids, srv.MatterClusterID()) {
			ids = append(ids, srv.MatterClusterID())
		}
	}
	return ids
}

// clientClusters: the Descriptor's ClientList.
func (f *topologyFacts) clientClusters(ep uint16) []uint32 {
	if d := f.descripts[ep]; d != nil {
		if v, ok := d.MatterRead(0x0002); ok {
			if list, ok := v.([]uint32); ok {
				return list
			}
		}
	}
	return nil
}

func (f *topologyFacts) server(ep uint16, clusterID uint32) contract.ClusterServer {
	for _, srv := range f.servers[ep] {
		if srv != nil && srv.MatterClusterID() == clusterID {
			return srv
		}
	}
	return nil
}

// features decodes the server's FeatureMap.
func (f *topologyFacts) features(ep uint16, clusterID uint32) []string {
	srv := f.server(ep, clusterID)
	if srv == nil {
		return nil
	}
	v, ok := srv.MatterRead(cluster.AttrGlobalFeatureMap)
	if !ok {
		return nil
	}
	var fm uint32
	switch x := v.(type) {
	case uint32:
		fm = x
	case uint16:
		fm = uint32(x)
	case uint8:
		fm = uint32(x)
	case uint64:
		fm = uint32(x) //nolint:gosec // a FeatureMap is a map32; higher bits name no feature
	case int:
		if x < 0 {
			return nil
		}
		fm = uint32(x) //nolint:gosec // bounded above by the map32 FeatureMap type
	default:
		return nil
	}
	return schema.ClusterFeatureNames(clusterID, fm)
}

// supports reads the server's AttributeList, AcceptedCommandList or event
// list, as a controller reads them.
func (f *topologyFacts) supports(ep uint16, clusterID uint32, element schema.RequirementElement, id uint32) bool {
	srv := f.server(ep, clusterID)
	if srv == nil {
		return false
	}
	switch element {
	case schema.RequirementAttribute:
		return slices.Contains(globalList(srv, cluster.AttrGlobalAttributeList), id)
	case schema.RequirementCommand:
		return slices.Contains(globalList(srv, cluster.AttrGlobalAcceptedCommandList), id)
	case schema.RequirementEvent:
		if lister, ok := srv.(contract.ClusterEventLister); ok {
			return slices.Contains(lister.MatterEvents(), id)
		}
		return false
	default:
		return false
	}
}

// globalList reads a list-valued global attribute: the server's own answer,
// the dispatcher's synthesis otherwise.
func globalList(srv contract.ClusterServer, attrID uint32) []uint32 {
	v, ok := srv.MatterRead(attrID)
	if !ok {
		v, ok = synthesizeGlobalRead(srv, attrID)
	}
	ids, isList := v.([]uint32)
	if !ok || !isList {
		return nil
	}
	return ids
}

func (f *topologyFacts) statedConditions(ep uint16) []string {
	if e := f.byID[ep]; e != nil {
		return e.DeviceConditions
	}
	return nil
}

// nodeConditions: CustomNetworkConfig, always — matter.js answers it for a
// node that does not commission over BLE (ServerEndpointFacts
// nodeConditionsOf), and this module has no BLE.
func (f *topologyFacts) nodeConditions(uint16) []string {
	return []string{condCustomNetworkConfig}
}

func (f *topologyFacts) describe(ep uint16) string { return fmt.Sprintf("endpoint %d", ep) }

// DeviceTypeValidationMode selects what [DeviceTypeValidator] does with a
// violation: matter.js's `endpoint.validation` modes.
type DeviceTypeValidationMode int

// Validation modes.
const (
	// DeviceTypeValidationWarn, the default: a new violation is reported
	// for logging; only a misplaced singleton is refused.
	DeviceTypeValidationWarn DeviceTypeValidationMode = iota
	// DeviceTypeValidationStrict: any new violation is refused — for tests
	// and for a host that must not run a non-conforming topology.
	DeviceTypeValidationStrict
	// DeviceTypeValidationOff: nothing is judged or recorded but singleton
	// placement, which is still refused.
	DeviceTypeValidationOff
)

// String names the mode as matter.js spells it.
func (m DeviceTypeValidationMode) String() string {
	switch m {
	case DeviceTypeValidationStrict:
		return "strict"
	case DeviceTypeValidationOff:
		return "off"
	default:
		return "warn"
	}
}

// DeviceTypeConformanceError refuses a topology: matter.js's
// DeviceTypeConformanceError, carrying the new violations of every refused
// endpoint, the first refused first.
type DeviceTypeConformanceError struct {
	Violations []DeviceTypeViolation
}

// Error names the refused endpoints and the violations.
func (e *DeviceTypeConformanceError) Error() string {
	var eps []string
	parts := make([]string, 0, len(e.Violations))
	for i := range e.Violations {
		v := &e.Violations[i]
		if ep := strconv.FormatUint(uint64(v.Endpoint), 10); !slices.Contains(eps, ep) {
			eps = append(eps, ep)
		}
		parts = append(parts, v.String())
	}
	return fmt.Sprintf("endpoint: device type conformance refuses endpoint(s) %s: %s",
		strings.Join(eps, ", "), strings.Join(parts, "; "))
}

// DeviceTypeVerdict is one validation by a [DeviceTypeValidator].
type DeviceTypeVerdict struct {
	// Violations are every current violation, by endpoint in topology
	// order.
	Violations []DeviceTypeViolation
	// Fresh are those not reported by an earlier validation — what
	// matter.js logs, once, as a warning.
	Fresh []DeviceTypeViolation
}

// DeviceTypeValidator holds a node's topologies to their device types
// across reassemblies, as matter.js's DeviceTypeConformanceService holds a
// server node's endpoints: each violation is reported once while it
// persists, and is reported again after it went away and came back. Safe
// for concurrent use.
type DeviceTypeValidator struct {
	mu       sync.Mutex
	mode     DeviceTypeValidationMode
	reported map[uint16]map[string]bool
	recorded []DeviceTypeViolation
}

// NewDeviceTypeValidator returns a validator in mode.
func NewDeviceTypeValidator(mode DeviceTypeValidationMode) *DeviceTypeValidator {
	return &DeviceTypeValidator{mode: mode, reported: map[uint16]map[string]bool{}}
}

// Mode returns the validator's mode.
func (v *DeviceTypeValidator) Mode() DeviceTypeValidationMode { return v.mode }

// Validate judges t (see [ValidateDeviceTypes]). In mode warn an endpoint
// with a new misplaced singleton, in mode strict an endpoint with any new
// violation, is refused: Validate returns a *DeviceTypeConformanceError
// with the refused endpoints' new violations and records nothing, so the
// next validation reports them again. Otherwise the verdict is recorded
// and returned; its Fresh violations are the ones to log. In mode off only
// singleton placement is judged, and nothing is recorded.
//
// Mirrors DeviceTypeConformanceService #validate (packages/node/src/node/
// server/DeviceTypeConformanceService.ts) for a construction pass.
func (v *DeviceTypeValidator) Validate(t *Topology) (DeviceTypeVerdict, error) {
	all := ValidateDeviceTypes(t)
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.mode == DeviceTypeValidationOff {
		var misplaced []DeviceTypeViolation
		for i := range all {
			if all[i].Kind == ViolationSingletonMisplaced {
				misplaced = append(misplaced, all[i])
			}
		}
		if len(misplaced) > 0 {
			return DeviceTypeVerdict{}, &DeviceTypeConformanceError{Violations: misplaced}
		}
		return DeviceTypeVerdict{}, nil
	}
	verdict := DeviceTypeVerdict{Violations: all}
	byEndpoint := map[uint16]map[string]bool{}
	refused := map[uint16]bool{}
	for i := range all {
		viol := &all[i]
		if byEndpoint[viol.Endpoint] == nil {
			byEndpoint[viol.Endpoint] = map[string]bool{}
		}
		byEndpoint[viol.Endpoint][viol.Key()] = true
		if v.reported[viol.Endpoint][viol.Key()] {
			continue
		}
		verdict.Fresh = append(verdict.Fresh, *viol)
		if v.mode == DeviceTypeValidationStrict || viol.Kind == ViolationSingletonMisplaced {
			refused[viol.Endpoint] = true
		}
	}
	if len(refused) > 0 {
		var fresh []DeviceTypeViolation
		for i := range verdict.Fresh {
			if refused[verdict.Fresh[i].Endpoint] {
				fresh = append(fresh, verdict.Fresh[i])
			}
		}
		return DeviceTypeVerdict{}, &DeviceTypeConformanceError{Violations: fresh}
	}
	v.reported = byEndpoint
	v.recorded = all
	return verdict, nil
}

// Violations returns what the last recorded validation found (matter.js
// violationsOf, for every endpoint at once). Empty in mode off.
func (v *DeviceTypeValidator) Violations() []DeviceTypeViolation {
	v.mu.Lock()
	defer v.mu.Unlock()
	return append([]DeviceTypeViolation(nil), v.recorded...)
}

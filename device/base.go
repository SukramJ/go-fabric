// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package device

import (
	"slices"
	"strings"
	"sync"

	"github.com/SukramJ/go-fabric/cluster/spec"
	fixedlabeldef "github.com/SukramJ/go-fabric/cluster/spec/fixedlabel"
	powersourcedef "github.com/SukramJ/go-fabric/cluster/spec/powersource"
	userlabeldef "github.com/SukramJ/go-fabric/cluster/spec/userlabel"
	"github.com/SukramJ/go-fabric/schema"
)

// baseOptionalServers are the server clusters the Base device type allows
// on every endpoint with plain optional conformance ("O"): FixedLabel
// (0x0040) and UserLabel (0x0041) at the pin. A device of any type may
// declare them; the type's own list does not name them.
//
// Mirrors matter.js packages/model/src/standard/elements/base.element.ts:46-47
// at 862ad75f (`Requirement({ name: "FixedLabel", id: 0x40, conformance:
// "O", element: "serverCluster" })`, UserLabel likewise): the Base
// requirements apply to every endpoint that lists a device type
// (ModelLookups.baseDeviceTypes; schema.BaseDeviceTypes). Read from the
// snapshot, not hand-coded, so a regeneration that moves them moves the
// rule. Descriptor (mandatory, the assembler's) and Binding (`Simple &
// Client`, :45 — a condition the kit does not evaluate here) are not
// plain optional and stay out.
var baseOptionalServers = sync.OnceValue(func() []ClusterRequirement {
	var out []ClusterRequirement
	for _, b := range schema.BaseDeviceTypes() {
		for _, r := range b.Requirements {
			if r.Element != schema.RequirementServerCluster || !r.HasID || r.Conformance.Text != "O" {
				continue
			}
			def := spec.Lookup(r.ID)
			if def == nil {
				continue // no generated definition committed: not declarable
			}
			out = append(out, ClusterRequirement{ID: r.ID, Name: r.Name, Conformance: r.Conformance.Text, Definition: def})
		}
	}
	return out
})

// withBaseOptional returns t with the Base device type's optional server
// clusters the declarations name and t does not list appended, as optional
// requirements; t itself when there are none. The copy only serves the
// device's construction: the device keeps t as its type.
func withBaseOptional(t *Type, s *settings) *Type {
	var extra []ClusterRequirement
	for _, r := range baseOptionalServers() {
		if _, listed := t.Server(r.ID); listed || !s.names(r.ID) {
			continue
		}
		extra = append(extra, r)
	}
	if len(extra) == 0 {
		return t
	}
	eff := *t
	eff.Servers = append(slices.Clone(t.Servers), extra...)
	return &eff
}

// ConditionBridgedPowerSourceInfo is the Base device type's condition a
// bridged device states when it carries power-source information
// (matter.js packages/model/src/standard/elements/base.element.ts:40 at
// 862ad75f, `Condition({ name: "BridgedPowerSourceInfo" })`). The kit
// states it for a device whose declarations mount PowerSource through the
// BridgedNode rule ([withBridgedPowerSource]).
const ConditionBridgedPowerSourceInfo = "BridgedPowerSourceInfo"

// bridgedPowerSourceServers are the server clusters the BridgedNode device
// type lists under the condition BridgedPowerSourceInfo: PowerSource
// (0x002F, "BridgedPowerSourceInfo") and PowerSourceConfiguration
// (0x002E, "BridgedPowerSourceInfo, D") at the pin. Every endpoint the
// node's Aggregator serves lists BridgedNode beside its own device type
// (endpoint listedDeviceTypes), so a bridged device of any type may carry
// them; the type's own list does not name them. Under the condition both
// are mandatory: a trailing "D" announces a future deprecation and the
// terms before it decide (matter.js packages/model/src/aspects/Conformance.ts:937-940
// at 862ad75f), so the device-type validator demands
// PowerSourceConfiguration wherever the condition holds.
//
// Mirrors matter.js packages/model/src/standard/elements/bridged-node.element.ts:22-23
// at 862ad75f. Read from the generated BridgedNode type, not hand-coded.
var bridgedPowerSourceServers = sync.OnceValue(func() []ClusterRequirement {
	var out []ClusterRequirement
	for _, r := range typeBridgedNode.Servers {
		if !strings.HasPrefix(r.Conformance, ConditionBridgedPowerSourceInfo) || r.Definition == nil {
			continue
		}
		out = append(out, ClusterRequirement{ID: r.ID, Name: r.Name, Conformance: r.Conformance, Definition: r.Definition})
	}
	return out
})

// withBridgedPowerSource returns t with the BridgedNode clusters under
// BridgedPowerSourceInfo appended, and whether it appended any: a declared
// PowerSource on a device is the device stating the condition for the
// bridged node it becomes, which [New] records in the device's conditions
// so the device-type validator judges the BridgedNode requirements as
// stated (endpoint StatedConditions). PowerSource is appended as the
// declared optional requirement; PowerSourceConfiguration, which the
// condition makes mandatory, as a mandatory one — mounted with its
// default server unless declared — so the stated condition leaves no
// violation. t itself, false when PowerSource is not declared or t lists
// it.
func withBridgedPowerSource(t *Type, s *settings) (*Type, bool) {
	var stated bool
	for _, r := range bridgedPowerSourceServers() {
		if _, listed := t.Server(r.ID); !listed && r.Conformance == ConditionBridgedPowerSourceInfo && s.names(r.ID) {
			stated = true
		}
	}
	if !stated {
		return t, false
	}
	var extra []ClusterRequirement
	for _, r := range bridgedPowerSourceServers() {
		if _, listed := t.Server(r.ID); listed {
			continue
		}
		if r.Conformance != ConditionBridgedPowerSourceInfo {
			r.Mandatory = true
		} else if !s.names(r.ID) {
			continue
		}
		extra = append(extra, r)
	}
	eff := *t
	eff.Servers = append(slices.Clone(t.Servers), extra...)
	return &eff, true
}

// names reports whether a declaration states anything about cluster id: a
// typed declaration, an opt-in, a server, initial values or elements.
func (s *settings) names(id uint32) bool {
	_, server := s.servers[id]
	_, initial := s.initial[id]
	_, elements := s.elements[id]
	return s.typed[id] || s.optional[id] || server || initial || elements
}

// FixedLabel is the device's typed FixedLabel server — a cluster the Base
// device type allows on every endpoint, mounted when a declaration names
// it (fixedlabeldef.Declare); nil when the device does not mount it. Its
// …Async callbacks run on the device's goroutine.
func (d *Device) FixedLabel() *fixedlabeldef.Server {
	return fixedlabeldef.WrapOn(d.Server(fixedlabeldef.ClusterID), d.queue)
}

// PowerSource is the device's typed PowerSource server — a cluster the
// BridgedNode device type allows on every bridged device under the
// condition BridgedPowerSourceInfo, mounted when a declaration names it
// (powersourcedef.Declare) and then stating that condition; nil when the
// device does not mount it. Its EndpointList names the device's own
// endpoint unless the declaration's Initial names others. Its …Async
// callbacks run on the device's goroutine.
func (d *Device) PowerSource() *powersourcedef.Server {
	return powersourcedef.WrapOn(d.Server(powersourcedef.ClusterID), d.queue)
}

// UserLabel is the device's typed UserLabel server — a cluster the Base
// device type allows on every endpoint, mounted when a declaration names
// it (userlabeldef.Declare); nil when the device does not mount it. A
// controller's write reaches its OnChanged; a node keeps the list in its
// store under the device's stable key.
func (d *Device) UserLabel() *userlabeldef.Server {
	return userlabeldef.WrapOn(d.Server(userlabeldef.ClusterID), d.queue)
}

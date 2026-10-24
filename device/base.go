// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package device

import (
	"slices"
	"sync"

	"github.com/SukramJ/go-fabric/cluster/spec"
	fixedlabeldef "github.com/SukramJ/go-fabric/cluster/spec/fixedlabel"
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

// UserLabel is the device's typed UserLabel server — a cluster the Base
// device type allows on every endpoint, mounted when a declaration names
// it (userlabeldef.Declare); nil when the device does not mount it. A
// controller's write reaches its OnChanged; a node keeps the list in its
// store under the device's stable key.
func (d *Device) UserLabel() *userlabeldef.Server {
	return userlabeldef.WrapOn(d.Server(userlabeldef.ClusterID), d.queue)
}

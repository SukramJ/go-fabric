// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package device

import (
	"cmp"
	"errors"
	"fmt"
	"math/bits"
	"slices"

	"github.com/SukramJ/go-fabric/cluster/spec"
	thermostatdef "github.com/SukramJ/go-fabric/cluster/spec/thermostat"
	"github.com/SukramJ/go-fabric/cluster/thermo"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/schema"
	"github.com/SukramJ/go-fabric/surface"
)

// Compile-time assertion.
var _ surface.Reporter = (*Device)(nil)

// Surface reports the device's surface: the digest of the policies it was
// composed under ([Options].Surface), the effective surface read from its
// built servers — one endpoint, the root empty — and every decision of
// the policy chain, at least one per candidate element (package surface).
// The report is computed once, at [New]; the endpoint's number is filled
// in once the node assembled the device. The clusters the endpoint
// assembler mounts and those a [Measurement] source projects are not part
// of it. The report is a fresh copy.
func (d *Device) Surface() surface.Report {
	r := d.report.Clone()
	if n, ok := d.EndpointNumber(); ok && len(r.Effective.Endpoints) == 1 {
		r.Effective.Endpoints[0].Number = n
	}
	return r
}

// effective is the device's effective surface, read from its servers as
// the dispatcher serves them ([endpoint.SurfaceEndpoint]): one endpoint,
// listing the device's own type.
func (d *Device) effective() surface.Surface {
	ep := endpoint.SurfaceEndpoint(endpoint.RenderSourceKey(d.opts.StableKey), d.servers)
	ep.DeviceTypes = []surface.DeviceTypeRef{{ID: uint32(d.typ.ID), Revision: d.typ.Revision}}
	return surface.Surface{Endpoints: []surface.Endpoint{ep}}
}

// compose decides and builds the device's servers from the type t (the
// effective type [New] derived) and the declarations in s: the candidate
// elements with their origin ([candidatesOf]), the policy chain over them
// (surface.Evaluate), then the servers from what the chain kept — the
// defaults in [buildOrder], mounted in the type's order, then the host's
// servers for clusters the type does not list. The report carries the
// policies' digest and the decisions; [New] reads the effective surface
// once the hooks are installed. With the zero-value policies every
// candidate is kept and the servers are the ones the device layer has
// always built.
func compose(t *Type, s *settings, opts Options) ([]contract.ClusterServer, surface.Report, error) {
	var policies surface.Policies
	if opts.Surface != nil {
		policies = *opts.Surface
	}
	rep := surface.Report{Policies: policies.Digest(), Level: policies.EffectiveLevel()}
	candidates := candidatesOf(t, s, endpoint.RenderSourceKey(opts.StableKey))
	for i := range candidates {
		candidates[i].Levels = surface.ElementLevels(candidates[i])
	}
	s.candidates = candidates
	kept, decisions, err := surface.Evaluate(candidates, policies)
	rep.Decisions = decisions
	if w := (*surface.WithheldError)(nil); errors.As(err, &w) {
		rep.Withheld = w.Elements
		w.Report = rep.Clone()
		return nil, rep, fmt.Errorf("%s: %w", t.Name, w)
	}
	if err != nil {
		return nil, rep, fmt.Errorf("%s: %w", t.Name, err)
	}
	s.cut = cutOf(candidates, kept)
	// A removed cluster takes its declaration's hooks with it.
	for id := range s.cut.clusters {
		delete(s.attach, id)
	}
	built := make(map[uint32]contract.ClusterServer, len(t.Servers))
	sibling := func(id uint32) contract.ClusterServer { return built[id] }
	for _, i := range buildOrder(t.Servers) {
		r := &t.Servers[i]
		if !s.composes(r) || s.cut.clusters[r.ID] {
			continue
		}
		srv, ok := s.servers[r.ID]
		if !ok {
			if srv, err = defaultServer(t, r, s, sibling); err != nil {
				return nil, rep, err
			}
		}
		built[r.ID] = srv
	}
	servers := make([]contract.ClusterServer, 0, len(built)+len(s.serverOrder))
	for i := range t.Servers {
		if srv, ok := built[t.Servers[i].ID]; ok {
			servers = append(servers, srv)
		}
	}
	// Host servers for clusters the type does not list, after the type's
	// own (MutableEndpoint.ts:95-110 extends the type's behaviors).
	for _, id := range s.serverOrder {
		if _, listed := t.Server(id); !listed && !s.cut.clusters[id] {
			servers = append(servers, s.servers[id])
		}
	}
	return servers, rep, nil
}

// composes reports whether the device layer builds a server for the
// requirement r: not one the endpoint assembler mounts or a measurement
// source projects, and one the type mandates, the host opted in or the
// host supplies.
func (s *settings) composes(r *ClusterRequirement) bool {
	if _, ok := assemblerMounted[r.ID]; ok || s.measured[r.ID] {
		return false
	}
	_, host := s.servers[r.ID]
	return host || r.Mandatory || s.optional[r.ID]
}

// candidatesOf lists the elements the device layer decides for a device of
// type t under key: the device type; each server cluster it composes; for
// each cluster whose default server it builds, each FeatureMap bit of the
// selection and each attribute, command and event the device type mandates
// or the declarations add.
func candidatesOf(t *Type, s *settings, key string) []surface.Element {
	out := []surface.Element{{
		Endpoint: key, Kind: surface.KindDeviceType, ID: uint32(t.ID),
		Origin: surface.OriginDeviceType, Mandatory: true,
	}}
	for i := range t.Servers {
		r := &t.Servers[i]
		if !s.composes(r) {
			continue
		}
		origin := surface.OriginHostDeclared
		if r.Mandatory {
			origin = surface.OriginDeviceType
		}
		out = append(out, surface.Element{
			Endpoint: key, Cluster: r.ID, Kind: surface.KindCluster, ID: r.ID,
			Origin: origin, Mandatory: r.Mandatory,
		})
		if _, host := s.servers[r.ID]; host || r.Definition == nil {
			// A host's server decides its own features and elements; a
			// cluster without a definition fails in defaultServer.
			continue
		}
		out = append(out, defaultCandidates(r, s, key)...)
	}
	for _, id := range s.serverOrder {
		if _, listed := t.Server(id); !listed {
			out = append(out, surface.Element{
				Endpoint: key, Cluster: id, Kind: surface.KindCluster, ID: id,
				Origin: surface.OriginHostDeclared,
			})
		}
	}
	return out
}

// aheadByCluster are the servers' declarations of their ahead-of-matter.js
// elements (surface.AheadSet), by cluster id: the device layer tags each
// candidate a declaration names with its register row, which the
// reference policy refuses unless surface.Policies.Extensions enables it.
var aheadByCluster = map[uint32]surface.AheadSet{
	thermostatdef.ClusterID: thermo.Ahead,
}

// AheadRows returns the "Ahead of matter.js" register IDs the device
// layer's server declarations name, sorted: the rows a host enables in
// surface.Policies.Extensions to select their elements on a device.
func AheadRows() []string { return aheadRows(aheadByCluster) }

// aheadRows is the sorted union of the register IDs of sets.
func aheadRows(sets map[uint32]surface.AheadSet) []string {
	var out []string
	for _, s := range sets {
		for _, id := range s.IDs() {
			if !slices.Contains(out, id) {
				out = append(out, id)
			}
		}
	}
	slices.Sort(out)
	return out
}

// defaultCandidates are the feature and element candidates of the default
// server [defaultServer] builds for r: the bits of its feature selection —
// the device type's and the cluster's mandated features, or the host's
// selection — and the elements the device type mandates and the
// declarations add. A feature is mandatory when the device type mandates
// it or the selection without it fails spec.CheckFeatures; an element when
// the device type mandates it or its conformance is mandatory for the
// selection. A candidate the cluster's [aheadByCluster] declaration names
// carries its register row in Reference.Ahead.
func defaultCandidates(r *ClusterRequirement, s *settings, key string) []surface.Element {
	def := r.Definition
	mandated := clusterMandatedFeatures(def)
	features := r.Features | mandated
	if sel, ok := s.features[r.ID]; ok {
		features = sel
	}
	ahead := aheadByCluster[r.ID]
	var out []surface.Element
	for x := features; x != 0; x &= x - 1 {
		bit := uint32(1) << bits.TrailingZeros32(x)
		el := surface.Element{
			Endpoint: key, Cluster: r.ID, Kind: surface.KindFeature, ID: bit, Origin: surface.OriginHostDeclared,
			Reference: surface.Reference{Ahead: ahead.Row(surface.KindFeature, bit)},
		}
		switch {
		case r.Features&bit != 0:
			el.Origin, el.Mandatory = surface.OriginDeviceType, true
		case mandated&bit != 0:
			el.Origin, el.Mandatory = surface.OriginClusterMandatory, true
		default:
			el.Mandatory = spec.CheckFeatures(def, features&^bit) != nil
		}
		out = append(out, el)
	}
	ctx := spec.Context(def, features)
	type elementID struct {
		kind surface.Kind
		id   uint32
	}
	elements := map[elementID]surface.Element{}
	for _, e := range r.Elements {
		kind, ok := requirementKind[e.Element]
		if !ok || e.Presence != PresenceMandatory {
			continue
		}
		elements[elementID{kind, e.ID}] = surface.Element{
			Endpoint: key, Cluster: r.ID, Kind: kind, ID: e.ID, Origin: surface.OriginDeviceType, Mandatory: true,
		}
	}
	declared := func(kind surface.Kind, ids []uint32) {
		for _, id := range ids {
			if _, ok := elements[elementID{kind, id}]; ok {
				continue
			}
			el := surface.Element{Endpoint: key, Cluster: r.ID, Kind: kind, ID: id, Origin: surface.OriginHostDeclared}
			if c, ok := conformanceOf(def, kind, id); ok && c.Applicability(ctx) == spec.ApplicabilityMandatory {
				el.Origin, el.Mandatory = surface.OriginClusterMandatory, true
			}
			elements[elementID{kind, id}] = el
		}
	}
	for _, extra := range []spec.Options{s.elements[r.ID], s.implied[r.ID]} {
		declared(surface.KindAttribute, extra.Attributes)
		declared(surface.KindCommand, extra.Commands)
		declared(surface.KindEvent, extra.Events)
	}
	rest := make([]surface.Element, 0, len(elements))
	for _, el := range elements {
		el.Reference.Ahead = ahead.Row(el.Kind, el.ID)
		rest = append(rest, el)
	}
	slices.SortFunc(rest, func(a, b surface.Element) int {
		return cmp.Or(cmp.Compare(a.Kind, b.Kind), cmp.Compare(a.ID, b.ID))
	})
	return append(out, rest...)
}

// requirementKind maps a device type's element requirement to its kind.
var requirementKind = map[schema.RequirementElement]surface.Kind{
	schema.RequirementAttribute: surface.KindAttribute,
	schema.RequirementCommand:   surface.KindCommand,
	schema.RequirementEvent:     surface.KindEvent,
}

// conformanceOf returns the conformance of an element of def; false for
// an element def does not define.
func conformanceOf(def *spec.Cluster, kind surface.Kind, id uint32) (spec.Conformance, bool) {
	switch kind {
	case surface.KindAttribute:
		if a := def.Attribute(id); a != nil {
			return a.Conformance, true
		}
	case surface.KindCommand:
		if c := def.Command(id, spec.Request); c != nil {
			return c.Conformance, true
		}
	case surface.KindEvent:
		if e := def.Event(id); e != nil {
			return e.Conformance, true
		}
	default:
	}
	return spec.Conformance{}, false
}

// surfaceCut is what the policy chain removed, by cluster: whole
// clusters, FeatureMap bits, and attributes, commands and events. The zero
// value removes nothing.
type surfaceCut struct {
	clusters map[uint32]bool
	features map[uint32]uint32
	elements map[uint32]map[surface.Kind]map[uint32]bool
}

// cutOf is the cut of the candidates the chain did not keep.
func cutOf(candidates, kept []surface.Element) surfaceCut {
	keep := make(map[surface.Element]bool, len(kept))
	for _, e := range kept {
		keep[e] = true
	}
	var c surfaceCut
	for _, e := range candidates {
		if keep[e] {
			continue
		}
		switch e.Kind {
		case surface.KindCluster:
			if c.clusters == nil {
				c.clusters = map[uint32]bool{}
			}
			c.clusters[e.ID] = true
		case surface.KindFeature:
			if c.features == nil {
				c.features = map[uint32]uint32{}
			}
			c.features[e.Cluster] |= e.ID
		case surface.KindAttribute, surface.KindCommand, surface.KindEvent:
			if c.elements == nil {
				c.elements = map[uint32]map[surface.Kind]map[uint32]bool{}
			}
			if c.elements[e.Cluster] == nil {
				c.elements[e.Cluster] = map[surface.Kind]map[uint32]bool{}
			}
			if c.elements[e.Cluster][e.Kind] == nil {
				c.elements[e.Cluster][e.Kind] = map[uint32]bool{}
			}
			c.elements[e.Cluster][e.Kind][e.ID] = true
		default:
			// A device type is mandatory: the chain keeps it.
		}
	}
	return c
}

// trim returns opts without the attributes, commands and events the cut
// removes from cluster.
func (c surfaceCut) trim(cluster uint32, opts spec.Options) spec.Options {
	opts.Attributes = c.keep(cluster, surface.KindAttribute, opts.Attributes)
	opts.Commands = c.keep(cluster, surface.KindCommand, opts.Commands)
	opts.Events = c.keep(cluster, surface.KindEvent, opts.Events)
	return opts
}

// initial returns the initial values of cluster without the attributes
// the cut removes; values itself when it removes none.
func (c surfaceCut) initial(cluster uint32, values map[uint32]any) map[uint32]any {
	cut := c.elements[cluster][surface.KindAttribute]
	if len(cut) == 0 {
		return values
	}
	out := make(map[uint32]any, len(values))
	for id, v := range values {
		if !cut[id] {
			out[id] = v
		}
	}
	return out
}

// keep returns ids without the ones the cut removes from cluster; ids
// itself when it removes none.
func (c surfaceCut) keep(cluster uint32, kind surface.Kind, ids []uint32) []uint32 {
	cut := c.elements[cluster][kind]
	if len(cut) == 0 {
		return ids
	}
	return slices.DeleteFunc(slices.Clone(ids), func(id uint32) bool { return cut[id] })
}

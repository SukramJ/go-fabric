// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

import (
	"cmp"
	"errors"
	"fmt"
	"slices"

	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/schema"
	"github.com/SukramJ/go-fabric/surface"
)

// ErrPolicyMismatch: [Aggregator.Add] (or Replace) of an endpoint composed
// under surface policies other than the node's [Config].Surface — a
// device built with another device.Options.Surface. Policies hold for the
// whole node; a rule for one device is a policy that judges by
// [surface.Element].Endpoint.
var ErrPolicyMismatch = errors.New("node: the endpoint was composed under other surface policies than the node's")

// AggregatorSurfaceKey is the key the Aggregator endpoint has in
// [Node.Surface].
const AggregatorSurfaceKey = "aggregator"

// surfaceEntry is one endpoint an added [Endpoint] brings: the endpoint
// itself or one of its parts, with its rendered key and its reporter when
// it has one.
type surfaceEntry struct {
	key      string
	spec     endpoint.Spec
	reporter surface.Reporter
}

// surfaceEntries are ep and its parts, depth first: ep reports through
// itself or its Spec's source, a part through its Spec's source.
func surfaceEntries(ep Endpoint) []surfaceEntry {
	spec := ep.Spec()
	top := surfaceEntry{key: endpoint.RenderSourceKey(spec.StableKey), spec: spec}
	if r, ok := ep.(surface.Reporter); ok {
		top.reporter = r
	} else if r, ok := spec.Source.(surface.Reporter); ok {
		top.reporter = r
	}
	out := []surfaceEntry{top}
	var parts func([]endpoint.Spec)
	parts = func(specs []endpoint.Spec) {
		for _, p := range specs {
			e := surfaceEntry{key: endpoint.RenderSourceKey(p.StableKey), spec: p}
			if r, ok := p.Source.(surface.Reporter); ok {
				e.reporter = r
			}
			out = append(out, e)
			parts(p.Parts)
		}
	}
	parts(spec.Parts)
	return out
}

// checkSurface refuses ep when it, or one of its parts, reports a surface
// composed under policies other than the node's ([ErrPolicyMismatch]). An
// endpoint without a reporter — a host's own Spec type — passes.
func (n *Node) checkSurface(ep Endpoint) error {
	for _, e := range surfaceEntries(ep) {
		if e.reporter == nil {
			continue
		}
		if got := e.reporter.Surface().Policies; got != n.surfaceDigest {
			return fmt.Errorf("%w: endpoint %q (policies %x…, the node's %x…)", ErrPolicyMismatch, e.key, got[:4], n.surfaceDigest[:4])
		}
	}
	return nil
}

// Surface reports the node's surface: the digest of [Config].Surface, the
// effective surface — the root endpoint (key "") from the root's servers,
// the Aggregator endpoint ([AggregatorSurfaceKey]) from its servers, and
// every endpoint added to the [Aggregator] and each of its parts, ordered
// by key — and the decisions and withheld elements of every endpoint
// that reports its own surface (a device.Device), in the same order. An
// endpoint without a reporter is read from its source's servers, with no
// decisions. As for a device, the clusters the endpoint assembler mounts
// on a bridged endpoint are not part of it. An endpoint's number is filled
// in once it is assembled.
func (n *Node) Surface() surface.Report {
	rep := surface.Report{Policies: n.surfaceDigest}
	if n.root != nil {
		rep.Effective.Root = endpoint.SurfaceEndpoint("", n.root.Servers())
	}
	type entry struct {
		ep        surface.Endpoint
		decisions []surface.Decision
		withheld  []surface.Element
	}
	var entries []entry
	if n.aggServers != nil {
		agg := endpoint.SurfaceEndpoint(AggregatorSurfaceKey, n.aggServers)
		if topo := n.br.Topology(); topo != nil {
			if ep := topo.FindByID(1); ep != nil && ep.IsAggregator() {
				agg.Number = ep.ID
			}
		}
		entries = append(entries, entry{ep: agg})
	}
	if n.agg != nil {
		for _, ep := range n.agg.all() {
			for _, e := range surfaceEntries(ep) {
				if e.reporter == nil {
					entries = append(entries, entry{ep: plainSurface(e.key, e.spec)})
					continue
				}
				r := e.reporter.Surface()
				for i, x := range r.Effective.Endpoints {
					en := entry{ep: x}
					if i == 0 {
						en.decisions, en.withheld = r.Decisions, r.Withheld
					}
					entries = append(entries, en)
				}
			}
		}
	}
	slices.SortStableFunc(entries, func(a, b entry) int { return cmp.Compare(a.ep.Key, b.ep.Key) })
	for _, e := range entries {
		rep.Effective.Endpoints = append(rep.Effective.Endpoints, e.ep)
		rep.Decisions = append(rep.Decisions, e.decisions...)
		rep.Withheld = append(rep.Withheld, e.withheld...)
	}
	return rep
}

// plainSurface is the surface of an endpoint that does not report its
// own: its source's servers and its device type.
func plainSurface(key string, spec endpoint.Spec) surface.Endpoint {
	var servers []contract.ClusterServer
	if spec.Source != nil {
		servers = spec.Source.MatterClusterServers()
	}
	ep := endpoint.SurfaceEndpoint(key, servers)
	ep.DeviceTypes = nil
	if spec.DeviceType != 0 {
		id := schema.DeviceTypeID(spec.DeviceType)
		ep.DeviceTypes = []surface.DeviceTypeRef{{ID: uint32(id), Revision: deviceTypeRevision(id)}}
	}
	return ep
}

// all returns every partition's endpoints, partitions in order, each in
// insertion order.
func (a *Aggregator) all() []Endpoint {
	a.mu.Lock()
	defer a.mu.Unlock()
	var out []Endpoint
	for _, p := range a.parts {
		out = append(out, p.eps...)
	}
	return out
}

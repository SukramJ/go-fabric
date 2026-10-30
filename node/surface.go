// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

import (
	"cmp"
	"errors"
	"fmt"
	"maps"
	"math/bits"
	"slices"

	mattercore "github.com/SukramJ/go-fabric/cluster/core"
	netcommdef "github.com/SukramJ/go-fabric/cluster/spec/networkcommissioning"
	tsdef "github.com/SukramJ/go-fabric/cluster/spec/timesynchronization"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/schema"
	"github.com/SukramJ/go-fabric/surface"
)

// ErrPolicyMismatch is the refusal of [Aggregator.Add] (or Replace) of an
// endpoint composed under surface policies other than the node's
// [Config].Surface — a
// device built with another device.Options.Surface. Policies hold for the
// whole node; a rule for one device is a policy that judges by
// [surface.Element].Endpoint.
var ErrPolicyMismatch = errors.New("node: the endpoint was composed under other surface policies than the node's")

// AggregatorSurfaceKey is the key the Aggregator endpoint has in
// [Node.Surface]. It is reserved: [Aggregator.Add] refuses an endpoint, or
// a part, whose rendered StableKey equals it ([ErrReservedKey]).
const AggregatorSurfaceKey = "aggregator"

// ErrReservedKey is the refusal of [Aggregator.Add] (or Replace) of an
// endpoint, or a part, whose rendered StableKey is [AggregatorSurfaceKey].
var ErrReservedKey = errors.New("node: the endpoint's key is reserved")

// aheadByCluster are the root servers' declarations of their
// ahead-of-matter.js elements (surface.AheadSet), by cluster id: the node
// tags each root feature its configuration selects and a declaration
// names with its register row, which the reference policy refuses unless
// [Config].Surface enables it.
var aheadByCluster = map[uint32]surface.AheadSet{
	tsdef.ClusterID:      mattercore.TimeSynchronizationAhead,
	netcommdef.ClusterID: mattercore.NetworkCommissioningAhead,
}

// AheadRows returns the "Ahead of matter.js" register IDs the root
// servers' declarations name, sorted: the rows a host enables in
// [Config].Surface's Extensions to select their elements on the root.
func AheadRows() []string {
	var out []string
	for _, s := range aheadByCluster {
		for _, id := range s.IDs() {
			if !slices.Contains(out, id) {
				out = append(out, id)
			}
		}
	}
	slices.Sort(out)
	return out
}

// rootFeatures is the FeatureMap selection cfg makes for each root server
// [aheadByCluster] names: TimeSynchronization's features
// ([Config].TimeSynchronization; none without it, no entry when it is
// disabled) and NetworkCommissioning's network interface — Wi-Fi with
// [Network].WiFiCommissioning, Ethernet otherwise.
func rootFeatures(cfg *Config) map[uint32]uint32 {
	out := map[uint32]uint32{}
	if !cfg.DisableTimeSynchronization {
		var f uint32
		if cfg.TimeSynchronization != nil {
			f = uint32(cfg.TimeSynchronization.Features)
		}
		out[tsdef.ClusterID] = f
	}
	if cfg.Network.WiFiCommissioning.Driver != nil {
		out[netcommdef.ClusterID] = mattercore.NetworkCommFeatureWiFi
	} else {
		out[netcommdef.ClusterID] = mattercore.NetworkCommFeatureEthernet
	}
	return out
}

// rootCandidates are the root's candidate elements: one feature per bit
// cfg selects that a root server's declaration names, tagged with its
// register row, host-declared and not mandatory, by cluster then bit.
// A selected element no declaration names is no candidate.
func rootCandidates(cfg *Config) []surface.Element {
	selected := rootFeatures(cfg)
	ids := slices.Sorted(maps.Keys(aheadByCluster))
	var out []surface.Element
	for _, cid := range ids {
		set := aheadByCluster[cid]
		for x := selected[cid]; x != 0; x &= x - 1 {
			bit := uint32(1) << bits.TrailingZeros32(x)
			row := set.Row(surface.KindFeature, bit)
			if row == "" {
				continue
			}
			out = append(out, surface.Element{
				Cluster: cid, Kind: surface.KindFeature, ID: bit,
				Origin: surface.OriginHostDeclared, Reference: surface.Reference{Ahead: row},
			})
		}
	}
	return out
}

// validateAndComposeRoot validates cfg ([Config.Validate]) and then
// decides the root's surface ([composeRoot]).
func validateAndComposeRoot(cfg *Config) ([]surface.Decision, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return composeRoot(cfg)
}

// composeRoot judges the root's candidates by cfg.Surface before the root
// servers are built: a refusal fails [New]; a feature a custom policy
// removes leaves cfg's selection (TimeSynchronization's features, copied
// first). It returns the decisions, which [Node.Surface] reports.
func composeRoot(cfg *Config) ([]surface.Decision, error) {
	cands := rootCandidates(cfg)
	kept, decisions, err := surface.Evaluate(cands, cfg.Surface)
	if err != nil {
		return decisions, fmt.Errorf("node: the root endpoint: %w", err)
	}
	keep := make(map[surface.Element]bool, len(kept))
	for _, e := range kept {
		keep[e] = true
	}
	for _, e := range cands {
		if keep[e] || e.Cluster != tsdef.ClusterID {
			// The Thread bit is never selected (rootFeatures): only
			// TimeSynchronization can lose a feature here.
			continue
		}
		ts := *cfg.TimeSynchronization
		ts.Features &^= tsdef.Feature(e.ID)
		cfg.TimeSynchronization = &ts
	}
	return decisions, nil
}

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
		for i := range specs {
			p := &specs[i]
			e := surfaceEntry{key: endpoint.RenderSourceKey(p.StableKey), spec: *p}
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

// checkSurface refuses ep when it, or one of its parts, has the reserved
// key [AggregatorSurfaceKey] ([ErrReservedKey]) or reports a surface
// composed under policies other than the node's ([ErrPolicyMismatch]). An
// endpoint without a reporter — a host's own Spec type — passes the
// second check.
func (n *Node) checkSurface(ep Endpoint) error {
	entries := surfaceEntries(ep)
	for i := range entries {
		e := &entries[i]
		if e.key == AggregatorSurfaceKey {
			return fmt.Errorf("%w: %q is the Aggregator's key in Node.Surface (node.AggregatorSurfaceKey)", ErrReservedKey, e.key)
		}
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
// root's decisions (its ahead-of-matter.js features, [AheadRows]), the
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
	rep := surface.Report{Policies: n.surfaceDigest, Decisions: slices.Clone(n.rootDecisions)}
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
			own := surfaceEntries(ep)
			for i := range own {
				e := &own[i]
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

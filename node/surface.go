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

// validateAndComposeRoot validates cfg ([Config.Validate]) and its
// surface policies' level (surface.Policies.Validate), then decides the
// root's feature surface ([composeRoot]).
func validateAndComposeRoot(cfg *Config) ([]surface.Decision, []surface.Element, error) {
	if err := cfg.Validate(); err != nil {
		return nil, nil, err
	}
	if err := cfg.Surface.Validate(); err != nil {
		return nil, nil, fmt.Errorf("node: %w", err)
	}
	return composeRoot(cfg)
}

// composeRoot judges the root's feature candidates by cfg.Surface before
// the root servers are built: a refusal fails [New]; a feature a policy
// removes — a custom one, or the level — leaves cfg's selection
// (TimeSynchronization's features, copied first). It returns the
// decisions, which [Node.Surface] reports, and the candidates.
func composeRoot(cfg *Config) ([]surface.Decision, []surface.Element, error) {
	cands := rootCandidates(cfg)
	for i := range cands {
		cands[i].Levels = surface.ElementLevels(cands[i])
	}
	kept, decisions, err := surface.Evaluate(cands, cfg.Surface)
	if err != nil {
		return decisions, cands, fmt.Errorf("node: the root endpoint: %w", err)
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
	return decisions, cands, nil
}

// rootNodeType is the Matter RootNode device type (0x0016).
const rootNodeType schema.DeviceTypeID = 0x0016

// rootMandatory reports whether the RootNode device type may mandate the
// server cluster id at the pin: its requirement's conformance
// (schema.DeviceTypeClusterRequirement) is "M", or mandatory under a
// condition — a name, a negated name, "|" or "&" of those, or an otherwise
// list with such a term. Groupcast's "GroupcastListenerCond,
// GroupcastSenderCond, O" is one: the device types a host adds later
// assert GroupcastListenerCond on the root (schema/devicetype_requirements_gen.go),
// which the root's clusters are judged before. An element the pin may
// mandate is never removed by an operator-side policy (ADR 0030
// decision 3); the level keeps it.
func rootMandatory(id uint32) bool {
	r, ok := schema.DeviceTypeClusterRequirement(rootNodeType, id, schema.RequirementServerCluster)
	return ok && mayBeMandatory(r.Conformance)
}

// mayBeMandatory reports whether a device-type conformance makes its
// element mandatory under some set of conditions.
func mayBeMandatory(c schema.Conformance) bool {
	switch c.Op {
	case "M", "name", "!", "|", "&":
		return true
	case "otherwise":
		return slices.ContainsFunc(c.Args, mayBeMandatory)
	default:
		return false
	}
}

// levelRootClusters judges the root's clusters once they are built (owner
// decision E.3, concept v11 §D.11): one KindCluster candidate per mounted
// root server — mandatory when the RootNode device type may mandate it at
// the pin ([rootMandatory]),
// host-declared for the servers the configuration adds
// ([RootClusters.configured]), a kit default otherwise. A cluster a policy
// removes is not mounted: the level removes an optional root cluster it
// does not know. The root is never withheld: a cluster the pin may mandate
// is kept, "kept: mandatory at pin (absent at level <L>)" — Groupcast below
// 1.6.0 (parity/levels, measured).
// The root servers' own optional elements are not level-controlled
// ([Node.Surface] records them, surface.Policies.ServerInternal).
func (n *Node) levelRootClusters() error {
	configured := map[uint32]bool{}
	for _, s := range n.root.configured() {
		configured[s.MatterClusterID()] = true
	}
	var cands []surface.Element
	for _, s := range n.root.servers {
		id := s.MatterClusterID()
		el := surface.Element{Cluster: id, Kind: surface.KindCluster, ID: id, Origin: surface.OriginKitDefault}
		switch {
		case rootMandatory(id):
			el.Origin, el.Mandatory = surface.OriginDeviceType, true
		case configured[id]:
			el.Origin = surface.OriginHostDeclared
		default:
		}
		el.Levels = surface.ElementLevels(el)
		cands = append(cands, el)
	}
	kept, decisions, err := surface.Evaluate(cands, n.cfg.Surface)
	if err != nil {
		return fmt.Errorf("node: the root endpoint: %w", err)
	}
	keep := map[uint32]bool{}
	for _, e := range kept {
		keep[e.ID] = true
	}
	servers := slices.DeleteFunc(slices.Clone(n.root.servers), func(s contract.ClusterServer) bool { return !keep[s.MatterClusterID()] })
	if len(servers) != len(n.root.servers) {
		n.root.servers = servers
		n.root.Descriptor.SetServerListProvider(clusterIDsOf(servers))
	}
	n.rootDecisions = append(n.rootDecisions, decisions...)
	n.rootCandidates = append(n.rootCandidates, cands...)
	return nil
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
// second check. An endpoint the specification level withheld is recorded
// and refused with its *surface.WithheldError ([Node.Surface]).
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
	return n.recordWithheld(entries)
}

// recordWithheld records each entry the specification level withheld
// (its report lists withheld elements; device.New returned the device
// with a surface.WithheldError) for [Node.Surface] and returns a
// *surface.WithheldError naming the first; an entry that is not withheld
// clears an earlier record under its key.
func (n *Node) recordWithheld(entries []surfaceEntry) error {
	n.withheldMu.Lock()
	defer n.withheldMu.Unlock()
	var first error
	for i := range entries {
		e := &entries[i]
		if e.reporter == nil {
			continue
		}
		r := e.reporter.Surface()
		if len(r.Withheld) == 0 {
			delete(n.withheld, e.key)
			continue
		}
		if n.withheld == nil {
			n.withheld = map[string]surface.Report{}
		}
		n.withheld[e.key] = r
		if first == nil {
			first = &surface.WithheldError{Endpoint: e.key, Elements: slices.Clone(r.Withheld), Report: r}
		}
	}
	return first
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
//
// Under a specification level ([Config].Surface's Level) the report names
// the level; the root's decisions include its clusters (levelRootClusters)
// and, for the root and the Aggregator endpoint, a keep decision on every
// served element the level does not know that no candidate decided
// (surface.Policies.ServerInternal); and an endpoint the level withheld at
// Add or Replace appears with its decisions and withheld elements, not in
// the effective surface.
func (n *Node) Surface() surface.Report {
	policies := n.cfg.Surface
	rep := surface.Report{Policies: n.surfaceDigest, Level: policies.EffectiveLevel(), Decisions: slices.Clone(n.rootDecisions)}
	if n.root != nil {
		rep.Effective.Root = endpoint.SurfaceEndpoint("", n.root.Servers())
		rep.Decisions = append(rep.Decisions, policies.ServerInternal(rep.Effective.Root, n.rootCandidates)...)
	}
	type entry struct {
		ep        surface.Endpoint
		decisions []surface.Decision
		withheld  []surface.Element
		// noEndpoint: a withheld endpoint, which serves nothing.
		noEndpoint bool
	}
	var entries []entry
	if n.aggServers != nil {
		agg := endpoint.SurfaceEndpoint(AggregatorSurfaceKey, n.aggServers)
		if topo := n.br.Topology(); topo != nil {
			if ep := topo.FindByID(1); ep != nil && ep.IsAggregator() {
				agg.Number = ep.ID
			}
		}
		entries = append(entries, entry{ep: agg, decisions: policies.ServerInternal(agg, nil)})
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
	n.withheldMu.Lock()
	for key, r := range n.withheld {
		entries = append(entries, entry{ep: surface.Endpoint{Key: key}, decisions: r.Decisions, withheld: r.Withheld, noEndpoint: true})
	}
	n.withheldMu.Unlock()
	slices.SortStableFunc(entries, func(a, b entry) int { return cmp.Compare(a.ep.Key, b.ep.Key) })
	for _, e := range entries {
		if !e.noEndpoint {
			rep.Effective.Endpoints = append(rep.Effective.Endpoints, e.ep)
		}
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

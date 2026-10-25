// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package client

import (
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"sync"

	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/cluster/spec/descriptor"
	"github.com/SukramJ/go-fabric/schema"
)

// errCorruptCache: loading the cache scheduled something other than an
// installation (ClientStructure.ts:217-229).
var errCorruptCache = errors.New("client: corrupt cache")

// emissionKind is what a structure emission announces.
type emissionKind uint8

const (
	emitEndpointInstalled emissionKind = iota
	emitClusterInstalled
	emitClusterReplaced
	emitClusterDeleted
	emitChanged
	emitEvent
)

// emission is one callback the structure produced, delivered by the node
// after its lock is released.
type emission struct {
	kind  emissionKind
	ep    *Endpoint
	c     *Cluster
	event EventData
}

// pendingChange is a structural change queued until a report is applied
// (ClientStructure.ts:1634-1649).
type pendingChange struct{ erase, install, rebuild bool }

// structure is matter.js ClientStructure (node/client/ClientStructure.ts:115-1564
// at 0ceb202d): the endpoints and clusters the peer's reports describe. Its
// state is guarded by the node's lock; every method expects it held.
type structure struct {
	// mu is the owning Node's lock (Node.mu); it guards every field of the
	// structure, and every method expects it held.
	mu  *sync.RWMutex
	log *slog.Logger
	p   *persister

	endpoints map[uint16]*Endpoint
	root      *Endpoint

	// pendingOrder and pending are #pendingChanges, a Map in insertion order.
	pendingOrder []*Endpoint
	pending      map[*Endpoint]*pendingChange

	// claimOrder and claims are #partClaims, part → claimants, both in
	// insertion order (:127-135).
	claimOrder  []*Endpoint
	claims      map[*Endpoint][]*Endpoint
	partsListOf map[*Endpoint]map[uint16]bool
	composition map[*Endpoint]schema.Composition

	// subscribedFabricFiltered is the default subscription's fabric filter
	// (:437-460): the cache holds one fabric view.
	subscribedFabricFiltered bool

	events  []emission
	delayed []EventData
}

// newStructure is a structure with its root endpoint (ClientStructure.ts:155-164):
// the node itself, installed, of the RootNode device type with an unknown
// revision (ClientNode.ts:401-404).
func newStructure(mu *sync.RWMutex, log *slog.Logger, p *persister) *structure {
	s := &structure{
		mu: mu, log: log, p: p,
		endpoints:                map[uint16]*Endpoint{},
		pending:                  map[*Endpoint]*pendingChange{},
		claims:                   map[*Endpoint][]*Endpoint{},
		partsListOf:              map[*Endpoint]map[uint16]bool{},
		composition:              map[*Endpoint]schema.Composition{},
		subscribedFabricFiltered: true,
	}
	s.root = newEndpoint(mu, 0)
	s.root.deviceType, s.root.classification, s.root.name, s.root.installed = rootNodeDeviceType, "node", "RootNode", true
	s.endpoints[0] = s.root
	return s
}

// takeEmissions returns what the structure emitted and forgets it.
func (s *structure) takeEmissions() []emission {
	out := s.events
	s.events = nil
	return out
}

// loadCache builds the structure from the store — ClientStructure.loadCache
// (:183-232): per endpoint the Descriptor first (:199-205), each cluster's
// seed checked (:210) and synchronized (:211); the part claims resolved
// (:215); only installations may be pending (:217-229); the structure
// events emitted (:231).
func (s *structure) loadCache(stored []StoredEndpoint) error {
	for _, se := range stored {
		ep := s.endpointFor(se.Number)
		clusters := slices.Clone(se.Clusters)
		if i := slices.IndexFunc(clusters, func(c StoredCluster) bool { return c.ID == descriptor.ClusterID }); i > 0 {
			d := clusters[i]
			clusters = append([]StoredCluster{d}, slices.Delete(clusters, i, i+1)...)
		}
		for _, sc := range clusters {
			if se.Number == 0 && (sc.ID == settingsCluster || sc.ID == icdCluster) {
				continue
			}
			values := make(map[uint32]spec.Node, len(sc.Values))
			for id, b := range sc.Values {
				n, err := decodeValue(b)
				if err != nil {
					s.log.Warn("matter.controller.client.cache_value_unreadable", slog.Int("endpoint", int(se.Number)),
						slog.Uint64("cluster", uint64(sc.ID)), slog.Uint64("attribute", uint64(id)), slog.Any("err", err))
					continue
				}
				values[id] = n
			}
			c := s.clusterFor(ep, sc.ID)
			c.cache = newCache(s.p, se.Number, sc.ID, values, sc.Version)
			s.checkUnlistedSeedValues(c)
			s.synchronizeCluster(ep, c, nil)
		}
	}
	s.resolvePartClaims()
	order, changes := s.pendingOrder, s.pending
	s.pendingOrder, s.pending = nil, map[*Endpoint]*pendingChange{}
	for _, ep := range order {
		if ch := changes[ep]; !ch.install || ch.erase || ch.rebuild {
			return fmt.Errorf("%w: unexpected erase or rebuild of endpoint %d during initialization", errCorruptCache, ep.number)
		}
		s.install(ep)
	}
	s.emitPendingStructureEvents()
	return nil
}

// injectVersionFilters is the version filters of a read or subscription —
// ClientStructure.injectVersionFilters (:252-290): one per cluster the
// read covers (isRelevant) whose version is known, none with
// refreshChangesOmitted for a cluster in which the peer has a
// changes-omitted attribute (:269-271, :1674-1692).
func (s *structure) injectVersionFilters(sc scope, refreshChangesOmitted bool) []DataVersionFilter {
	var out []DataVersionFilter
	for _, epNo := range slices.Sorted(maps.Keys(s.endpoints)) {
		ep := s.endpoints[epNo]
		for _, id := range slices.Sorted(maps.Keys(ep.clusters)) {
			c := ep.clusters[id]
			if !sc.isRelevant(epNo, id) {
				continue
			}
			if refreshChangesOmitted && hasChangesOmittedAttribute(c) {
				continue
			}
			if !c.cache.hasVersion {
				continue
			}
			out = append(out, DataVersionFilter{Endpoint: epNo, Cluster: id, DataVersion: c.cache.version})
		}
	}
	return out
}

// withKnownVersions is the version filters a read or subscription sends —
// the caller's own, kept as given and first, then the injected ones.
// Mirrors matter.js packages/node/src/node/client/ClientStructure.ts:286-323
// (injectVersionFilters at 862ad75f): a request with the other fabric
// filter than the cache's gets none injected (:288), and a cluster the
// caller filters gets no injected filter (:293-295, :312), so the peer
// sees exactly the caller's filter for it. caller is not modified.
func (s *structure) withKnownVersions(sc scope, caller []DataVersionFilter, refreshChangesOmitted bool) []DataVersionFilter {
	out := slices.Clone(caller)
	if sc.fabricFiltered != s.subscribedFabricFiltered {
		return out
	}
	type key struct {
		ep      uint16
		cluster uint32
	}
	filtered := make(map[key]bool, len(caller))
	for _, f := range caller {
		filtered[key{f.Endpoint, f.Cluster}] = true
	}
	for _, f := range s.injectVersionFilters(sc, refreshChangesOmitted) {
		if !filtered[key{f.Endpoint, f.Cluster}] {
			out = append(out, f)
		}
	}
	return out
}

// hasChangesOmittedAttribute: the peer has an attribute with the
// changes-omitted quality in the cluster — every such attribute of the
// model without the peer's AttributeList, the listed ones with it
// (ClientStructure.ts:1674-1692). A cluster without a built shape has none
// (:1675-1678). The schema's tables stand for the ClusterModel: they know
// every snapshot cluster, so a named cluster (no generated definition) is
// judged exactly, as matter.js judges it.
func hasChangesOmittedAttribute(c *Cluster) bool {
	if c.current == nil {
		return false
	}
	if !c.in.attributeListReported {
		return schema.ClusterHasChangesOmittedAttribute(c.id)
	}
	return slices.ContainsFunc(c.in.attributes, func(id uint32) bool { return schema.AttributeChangesOmitted(c.id, id) })
}

// batch is an AttributeUpdates (ClientStructure.ts:1601-1605): the values
// of one consecutive endpoint and cluster.
type batch struct {
	ep      uint16
	cluster uint32
	values  map[uint32]*spec.Node
	version *uint32
}

// mutateContext is the per-interaction state of mutate (:59-67).
type mutateContext struct {
	endpointsWithData map[uint16]bool
	clustersWithData  map[*Cluster]bool
}

// mutate applies a report — ClientStructure.mutate (:295-380) over a merged
// report: the attribute values in order, batched per consecutive endpoint
// and cluster (:462-504), the events (:506-528), the statuses logged
// (:338-346), the last batch (:357-361), the structural changes (:375),
// the structure events (:378) and the delayed events (:379).
func (s *structure) mutate(sc scope, r *Report) {
	q := &mutateContext{endpointsWithData: map[uint16]bool{}, clustersWithData: map[*Cluster]bool{}}
	var cur *batch
	for i := range r.Attributes {
		cur = s.mutateAttribute(&r.Attributes[i], sc, cur, q)
	}
	for i := range r.Events {
		s.emitEvent(&r.Events[i], q)
	}
	for _, st := range r.Statuses {
		s.log.Debug("matter.controller.client.attribute_status", slog.Int("endpoint", int(st.Path.Endpoint)),
			slog.Uint64("cluster", uint64(st.Path.Cluster)), slog.Uint64("attribute", uint64(st.Path.Attribute)), slog.String("status", st.Status.String()))
	}
	for _, st := range r.EventStatuses {
		s.log.Debug("matter.controller.client.event_status", slog.Int("endpoint", int(st.Path.Endpoint)),
			slog.Uint64("cluster", uint64(st.Path.Cluster)), slog.Uint64("event", uint64(st.Path.Event)), slog.String("status", st.Status.String()))
	}
	if cur != nil {
		s.updateCluster(cur, q)
	}
	s.applyPendingChanges()
	s.emitPendingStructureEvents()
	s.emitPendingEvents()
}

// mutateAttribute is #mutateAttribute (:462-504): nothing is stored from an
// interaction whose fabric filter differs from the subscription's
// (:468-471); the endpoint is recorded (:477); a change of endpoint or
// cluster flushes the batch (:479-484); a new batch carries the report's
// version only when the read covered the whole cluster (:494-497).
func (s *structure) mutateAttribute(a *AttributeData, sc scope, cur *batch, q *mutateContext) *batch {
	if s.subscribedFabricFiltered != sc.fabricFiltered {
		return cur
	}
	ep, cl := a.Path.Endpoint, a.Path.Cluster
	q.endpointsWithData[ep] = true
	if cur != nil && (cur.ep != ep || cur.cluster != cl) {
		s.updateCluster(cur, q)
		cur = nil
	}
	v := a.Value
	if cur == nil {
		cur = &batch{ep: ep, cluster: cl, values: map[uint32]*spec.Node{a.Path.Attribute: &v}}
		if sc.isWildcard(ep, cl) {
			version := a.DataVersion
			cur.version = &version
		}
		return cur
	}
	cur.values[a.Path.Attribute] = &v
	return cur
}

// emitEvent is #emitEvent (:506-528): delayed to the end of the report when
// the endpoint is unknown or not installed, received data in this report or
// has a structural change pending (:518-524), else delivered now (:526).
func (s *structure) emitEvent(e *EventData, q *mutateContext) {
	ep := s.endpoints[e.Path.Endpoint]
	if ep == nil || !ep.installed || q.endpointsWithData[e.Path.Endpoint] || s.pending[ep] != nil {
		s.delayed = append(s.delayed, *e)
		return
	}
	s.events = append(s.events, emission{kind: emitEvent, event: *e})
}

// preserveAbsentCluster cancels a deletion of a cluster the peer still
// sends data for — "Schrödinger's cluster" (:555-565).
func (s *structure) preserveAbsentCluster(ep *Endpoint, c *Cluster) {
	if !c.pendingDelete {
		return
	}
	s.log.Info("matter.controller.client.cluster_kept", slog.Int("endpoint", int(ep.number)), slog.Uint64("cluster", uint64(c.id)),
		slog.String("reason", "absent from the descriptor's server list but the peer sent attribute data for it"))
	c.pendingDelete = false
}

// updateCluster applies one batch — #updateCluster (:572-587).
func (s *structure) updateCluster(b *batch, q *mutateContext) {
	ep := s.endpointFor(b.ep)
	c := s.clusterFor(ep, b.cluster)
	q.clustersWithData[c] = true
	s.preserveAbsentCluster(ep, c)
	s.pruneUnlistedAttributes(c, b.values)
	s.invalidateOnDefinitionChange(c, b.values)
	c.cache.externalSet(b.values, b.version)
	s.synchronizeCluster(ep, c, q.clustersWithData)
}

// authoritativeAttributeList is the AttributeList a value states, nil
// unless a non-empty list (:101-108).
func authoritativeAttributeList(v *spec.Node) []uint32 {
	if v == nil {
		return nil
	}
	ids := idList(*v)
	if len(ids) == 0 {
		return nil
	}
	return ids
}

// invalidateOnDefinitionChange discards a shape the peer no longer matches:
// a changed AttributeList, ClusterRevision, FeatureMap or
// AcceptedCommandList (:632-659).
func (s *structure) invalidateOnDefinitionChange(c *Cluster, values map[uint32]*spec.Node) {
	if c.current == nil {
		return
	}
	changed := false
	if attrs := authoritativeAttributeList(values[spec.AttrAttributeList]); attrs != nil && !slices.Equal(c.in.attributes, attrs) {
		changed = true
	}
	if v, ok := values[spec.AttrClusterRevision]; ok {
		same := v != nil && isUint(*v) && c.in.hasRevision && c.in.revision == v.El.Uint
		if v == nil && !c.in.hasRevision {
			same = true
		}
		changed = changed || !same
	}
	if v, ok := values[spec.AttrFeatureMap]; ok {
		same := v != nil && isUint(*v) && c.in.hasFeatures && uint64(c.in.featureMap) == v.El.Uint
		if v == nil && !c.in.hasFeatures {
			same = true
		}
		changed = changed || !same
	}
	if v := values[spec.AttrAcceptedCommandList]; v != nil && isList(*v) && !slices.Equal(c.in.commands, idList(*v)) {
		changed = true
	}
	if changed {
		c.current = nil
	}
}

// pruneUnlistedAttributes deletes the stored values of attributes a
// reported AttributeList omits; the batch's own values and global
// attributes stay (:668-681, :690-707).
func (s *structure) pruneUnlistedAttributes(c *Cluster, values map[uint32]*spec.Node) {
	list := authoritativeAttributeList(values[spec.AttrAttributeList])
	if list == nil {
		return
	}
	for id := range c.cache.values {
		if _, ok := values[id]; ok {
			continue
		}
		if !slices.Contains(list, id) && !isGlobal(id) {
			values[id] = nil
		}
	}
}

// checkUnlistedSeedValues makes the peer resend a cached cluster holding
// values its cached AttributeList omits: its version is forgotten, so the
// next wildcard read fetches it (:721-744; values are keyed by id only, so
// the name-keyed branch :734-737 does not apply).
func (s *structure) checkUnlistedSeedValues(c *Cluster) {
	v, ok := c.cache.values[spec.AttrAttributeList]
	if !ok {
		return
	}
	list := authoritativeAttributeList(&v)
	if list == nil {
		return
	}
	for id := range c.cache.values {
		if !slices.Contains(list, id) && !isGlobal(id) {
			c.cache.invalidateVersion()
			return
		}
	}
}

// synchronizeCluster builds the cluster's shape once something useful
// exists (:754-837) — queued for an installed endpoint, as a rebuild when
// the endpoint already exposes the cluster and an installation otherwise
// (:825-831), set directly on one that is not installed (:832-835) — and
// for the Descriptor synchronizes the endpoint (:839-848).
func (s *structure) synchronizeCluster(ep *Endpoint, c *Cluster, clustersWithData map[*Cluster]bool) {
	if c.current == nil {
		c.readInput()
		if c.buildable() {
			sh := c.analyse()
			if ep.installed {
				c.pending = sh
				s.preserveAbsentCluster(ep, c)
				if c.injected != nil {
					s.schedule(ep, "rebuild")
				} else {
					s.schedule(ep, "install")
				}
			} else {
				c.current, c.injected = sh, sh
			}
		}
	}
	if c.id == descriptor.ClusterID {
		s.synchronizeDescriptor(ep, c.cache.values, clustersWithData)
	}
}

// synchronizeDescriptor reads the Descriptor's lists (:851-1009).
func (s *structure) synchronizeDescriptor(ep *Endpoint, values map[uint32]spec.Node, clustersWithData map[*Cluster]bool) {
	if v, ok := values[descriptor.AttrDeviceTypeList]; ok && isList(v) {
		s.synchronizeDeviceTypes(ep, deviceTypeEntries(v))
	}
	if v, ok := values[descriptor.AttrServerList]; ok && isList(v) {
		s.synchronizeServerList(ep, v, clustersWithData)
	}
	if v, ok := values[descriptor.AttrPartsList]; ok && isList(v) {
		s.synchronizePartsList(ep, v)
	}
}

// deviceTypeEntries are a DeviceTypeList's entries; an entry without a
// numeric device type is skipped (:866-868).
func deviceTypeEntries(v spec.Node) []descriptor.DeviceTypeStruct {
	var out []descriptor.DeviceTypeStruct
	for _, e := range v.Children {
		dt, err := spec.DecodeStruct[descriptor.DeviceTypeStruct](e)
		if err != nil {
			continue
		}
		out = append(out, dt)
	}
	return out
}

// isApplication is DeviceClassification.isApplication: not a utility —
// "utility" or "node" (model/src/common/DeviceClassification.ts:52-65).
func isApplication(classification string) bool {
	return classification != "utility" && classification != "node"
}

// synchronizeDeviceTypes is the DeviceTypeList half of
// deviceTypeDefinition is the snapshot's definition of a Descriptor
// entry's 32-bit devtype-id; none for a manufacturer-specific one.
func deviceTypeDefinition(devtype uint32) (*schema.DeviceTypeDefinition, bool) {
	id, ok := schema.DeviceTypeIDOf(devtype)
	if !ok {
		return nil, false
	}
	return schema.DeviceTypeDefinitionOf(id)
}

// #synchronizeDescriptor (:858-905).
func (s *structure) synchronizeDeviceTypes(ep *Endpoint, entries []descriptor.DeviceTypeStruct) {
	s.noteComposition(ep, entries)
	for _, dt := range entries {
		def, known := deviceTypeDefinition(dt.DeviceType)
		isApp := known && isApplication(def.Classification)
		// The root of a node whose type is not RootNode takes the revision
		// only (:876-880).
		if ep.number == 0 && ep.deviceType != rootNodeDeviceType {
			ep.deviceRevision, ep.hasRevision = dt.Revision, true
			break
		}
		// A device type once found is replaced only by an application one
		// (:882-892).
		if ep.deviceType != UnknownDeviceType && !isApp {
			continue
		}
		ep.deviceType, ep.deviceRevision, ep.hasRevision = dt.DeviceType, dt.Revision, true
		ep.classification, ep.name = "simple", fmt.Sprintf("Unknown#%04x", dt.DeviceType)
		if known {
			ep.classification, ep.name = def.Classification, def.Name
		}
		if isApp {
			break
		}
	}
}

// noteComposition records how the endpoint composes its PartsList: known
// once any entry names a device type, full-family when any of them is
// (:1457-1478); a device type the schema does not know counts as a tree.
func (s *structure) noteComposition(ep *Endpoint, entries []descriptor.DeviceTypeStruct) {
	if len(entries) == 0 {
		return
	}
	comp := schema.CompositionTree
	for _, dt := range entries {
		if def, ok := deviceTypeDefinition(dt.DeviceType); ok && def.Composition == schema.CompositionFullFamily {
			comp = schema.CompositionFullFamily
		}
	}
	s.composition[ep] = comp
}

// synchronizeServerList is the ServerList half (:907-939): each listed
// cluster gets its structure (:915-920); an exposed cluster the list omits
// is deleted unless it has a pending shape or data arrived for it in this
// interaction (:922-934).
func (s *structure) synchronizeServerList(ep *Endpoint, v spec.Node, clustersWithData map[*Cluster]bool) {
	var supported []uint32
	for _, id := range slices.Sorted(maps.Keys(ep.clusters)) {
		if ep.clusters[id].injected != nil {
			supported = append(supported, id)
		}
	}
	for _, e := range v.Children {
		if !isUint(e) || e.El.Uint > 0xFFFF_FFFF {
			continue
		}
		id := uint32(e.El.Uint)
		s.clusterFor(ep, id)
		supported = slices.DeleteFunc(supported, func(x uint32) bool { return x == id })
	}
	anyDelete := false
	for _, id := range supported {
		c := s.clusterFor(ep, id)
		if c.pending == nil && !clustersWithData[c] {
			c.pendingDelete = true
			anyDelete = true
		}
	}
	if anyDelete {
		s.schedule(ep, "rebuild")
	}
}

// synchronizePartsList is the PartsList half (:941-1008): each valid part
// (EndpointNumber.isValid, 0 to 0xFFFE) gets this endpoint as a claimant,
// claims on parts no longer named are withdrawn, and the root's list erases
// every endpoint it no longer names unless it holds one it does (:971-1008).
func (s *structure) synchronizePartsList(ep *Endpoint, v spec.Node) {
	named := map[uint16]bool{}
	used := map[uint64]bool{}
	for _, e := range v.Children {
		if !isUint(e) {
			continue
		}
		used[e.El.Uint] = true
		if e.El.Uint > 0xFFFE {
			continue
		}
		partNo := uint16(e.El.Uint)
		named[partNo] = true
		part := s.endpointFor(partNo)
		s.addClaim(part, ep)
	}
	s.retractClaimsOf(ep, named)
	s.partsListOf[ep] = named
	if ep.number != 0 {
		return
	}
	for _, no := range slices.Sorted(maps.Keys(s.endpoints)) {
		d := s.endpoints[no]
		if no == 0 || !d.installed || used[uint64(no)] {
			continue
		}
		var keep []uint16
		s.visitBelow(d, func(below *Endpoint) {
			if used[uint64(below.number)] {
				keep = append(keep, below.number)
			}
		})
		if len(keep) > 0 {
			s.log.Warn("matter.controller.client.endpoint_kept", slog.Int("endpoint", int(no)), slog.Any("holds", keep),
				slog.String("reason", "the peer's root no longer names it but names endpoints it holds"))
			continue
		}
		s.schedule(d, "erase")
	}
}

// visitBelow calls f for every installed endpoint below ep.
func (s *structure) visitBelow(ep *Endpoint, f func(*Endpoint)) {
	for _, c := range ep.partsLocked() {
		f(c)
		s.visitBelow(c, f)
	}
}

// addClaim adds claimant to part's claimants.
func (s *structure) addClaim(part, claimant *Endpoint) {
	cl, ok := s.claims[part]
	if !ok {
		s.claimOrder = append(s.claimOrder, part)
	}
	if !slices.Contains(cl, claimant) {
		s.claims[part] = append(cl, claimant)
	}
}

// deleteClaims forgets every claim on part.
func (s *structure) deleteClaims(part *Endpoint) {
	if _, ok := s.claims[part]; !ok {
		return
	}
	delete(s.claims, part)
	s.claimOrder = slices.DeleteFunc(s.claimOrder, func(x *Endpoint) bool { return x == part })
}

// removeClaimant withdraws claimant's claim on part.
func (s *structure) removeClaimant(part, claimant *Endpoint) {
	cl := slices.DeleteFunc(slices.Clone(s.claims[part]), func(x *Endpoint) bool { return x == claimant })
	if len(cl) == 0 {
		s.deleteClaims(part)
		return
	}
	s.claims[part] = cl
}

// retractClaimsOf withdraws ep's claims on the parts its previous list
// named and named does not (:1426-1447).
func (s *structure) retractClaimsOf(ep *Endpoint, named map[uint16]bool) {
	for _, partNo := range slices.Sorted(maps.Keys(s.partsListOf[ep])) {
		if named[partNo] {
			continue
		}
		part := s.endpoints[partNo]
		if part == nil {
			continue
		}
		if _, ok := s.claims[part]; ok {
			s.removeClaimant(part, ep)
		}
	}
}

// endpointFor is #endpointFor (:1011-1037).
func (s *structure) endpointFor(number uint16) *Endpoint {
	if ep := s.endpoints[number]; ep != nil {
		return ep
	}
	ep := newEndpoint(s.mu, number)
	s.endpoints[number] = ep
	return ep
}

// clusterFor is #clusterFor (:1039-1057).
func (s *structure) clusterFor(ep *Endpoint, id uint32) *Cluster {
	if c := ep.clusters[id]; c != nil {
		return c
	}
	c := newCluster(s.mu, id, newCache(s.p, ep.number, id, nil, nil))
	ep.clusters[id] = c
	return c
}

// ownerOf is #ownerOf (:1111-1125): the pending owner, else the owner; the
// root has none.
func (s *structure) ownerOf(ep *Endpoint) *Endpoint {
	if ep.pendingOwner != nil {
		return ep.pendingOwner
	}
	if ep.number == 0 {
		return nil
	}
	return ep.owner
}

// erase removes an endpoint that left the peer, and what it holds
// (:1130-1161, Endpoint.delete closing its parts).
func (s *structure) erase(ep *Endpoint) {
	for _, part := range ep.partsLocked() {
		s.erase(part)
	}
	if s.endpoints[ep.number] == ep {
		delete(s.endpoints, ep.number)
	}
	s.deleteClaims(ep)
	delete(s.partsListOf, ep)
	delete(s.composition, ep)
	for _, part := range slices.Clone(s.claimOrder) {
		if slices.Contains(s.claims[part], ep) {
			s.removeClaimant(part, ep)
		}
	}
	if ep.destroyed {
		return
	}
	s.log.Debug("matter.controller.client.endpoint_removed", slog.Int("endpoint", int(ep.number)))
	ep.destroyed, ep.installed = true, false
	if ep.owner != nil {
		ep.owner.children = slices.DeleteFunc(ep.owner.children, func(x *Endpoint) bool { return x == ep })
	}
	for _, c := range ep.clusters {
		c.cache.discard()
	}
	if err := s.p.store.EraseEndpoint(s.p.nodeID, ep.number); err != nil {
		s.log.Warn("matter.controller.client.store_erase_failed", slog.Int("endpoint", int(ep.number)), slog.Any("err", err))
	}
}

// rebuild replaces or deletes clusters of an installed endpoint (:1171-1222):
// a deleted cluster's structure is discarded and its store erased, a pending
// shape replaces or adds.
func (s *structure) rebuild(ep *Endpoint) {
	for _, id := range slices.Sorted(maps.Keys(ep.clusters)) {
		c := ep.clusters[id]
		if c.pendingDelete {
			delete(ep.clusters, id)
			if c.current == nil {
				continue
			}
			c.cache.erase()
			s.events = append(s.events, emission{kind: emitClusterDeleted, ep: ep, c: c})
			continue
		}
		if c.pending == nil {
			continue
		}
		kind := emitClusterInstalled
		if c.injected != nil {
			kind = emitClusterReplaced
		}
		c.injected, c.current, c.pending = c.pending, c.pending, nil
		s.events = append(s.events, emission{kind: kind, ep: ep, c: c})
	}
}

// install installs the endpoint under its pending owner and the clusters
// pending on it (:1227-1263); a cluster is announced on its own only when
// the endpoint was installed before (:1252-1261).
func (s *structure) install(ep *Endpoint) {
	owner := ep.pendingOwner
	if owner != nil {
		ep.owner, ep.pendingOwner, ep.installed = owner, nil, true
		owner.children = append(owner.children, ep)
		s.events = append(s.events, emission{kind: emitEndpointInstalled, ep: ep})
	}
	for _, id := range slices.Sorted(maps.Keys(ep.clusters)) {
		c := ep.clusters[id]
		if c.pending == nil || c.injected != nil {
			continue
		}
		c.injected, c.current, c.pending = c.pending, c.pending, nil
		if owner == nil {
			s.events = append(s.events, emission{kind: emitClusterInstalled, ep: ep, c: c})
		}
	}
}

// applyPendingChanges resolves the part claims, then applies each queued
// change in order: erase, else rebuild and install (:1269-1288).
func (s *structure) applyPendingChanges() {
	s.resolvePartClaims()
	for len(s.pendingOrder) > 0 {
		ep := s.pendingOrder[0]
		s.pendingOrder = s.pendingOrder[1:]
		ch := s.pending[ep]
		delete(s.pending, ep)
		if ch.erase {
			s.erase(ep)
			continue
		}
		if ch.rebuild {
			s.rebuild(ep)
		}
		if ch.install {
			s.install(ep)
		}
	}
}

// resolvePartClaims gives every part whose owner is now known an owner and
// schedules its installation (:1300-1331): nothing is reparented
// (:1305-1310), a part goes only below a placed owner (:1313) and only when
// the root names it (:1319-1324).
func (s *structure) resolvePartClaims() {
	for placed := true; placed; {
		placed = false
		for _, part := range slices.Clone(s.claimOrder) {
			claimants, ok := s.claims[part]
			if !ok {
				continue
			}
			if s.ownerOf(part) != nil {
				s.deleteClaims(part)
				continue
			}
			owner, contenders := s.ownerFor(claimants)
			if owner == nil || !s.isPlaced(owner) {
				continue
			}
			s.deleteClaims(part)
			if !s.isOnTheNode(part) {
				continue
			}
			// The warning for the other claimants comes after the root's
			// check of the part (ClientStructure.ts:1455-1471 at 862ad75f).
			if len(contenders) > 0 {
				others := make([]int, 0, len(contenders))
				for _, o := range contenders {
					others = append(others, int(o.number))
				}
				s.log.Warn("matter.controller.client.several_parents", slog.Int("parent", int(owner.number)), slog.Int("part", int(part.number)),
					slog.Any("also_named_by", others), slog.String("reason", "an endpoint has a single parent"))
			}
			part.pendingOwner = owner
			s.schedule(part, "install")
			placed = true
		}
	}
}

// isPlaced: the root, or an endpoint with an owner, installed or pending
// (:1337-1339).
func (s *structure) isPlaced(ep *Endpoint) bool { return ep.number == 0 || s.ownerOf(ep) != nil }

// isOnTheNode: the root names the endpoint, so far as it has said
// (:1349-1357).
func (s *structure) isOnTheNode(ep *Endpoint) bool {
	if ep.number == 0 {
		return true
	}
	rootParts, ok := s.partsListOf[s.endpoints[0]]
	return !ok || rootParts[ep.number]
}

// ownerFor is the claimant that owns a part, with the claimants that
// conflict with that choice, nil while that cannot be told. Mirrors
// matter.js packages/node/src/node/client/ClientStructure.ts:1509-1551
// (#ownerFor at 862ad75f): a claimant the root does not name owns nothing
// (:1513-1518), one whose composition is unknown makes the part wait
// (:1520-1525); an ordinary claimant is closer to the part than any
// full-family one (:1530-1533); among the candidates the closest wins
// ([structure.closestClaimant]); a full-family owner waits until every
// endpoint it names has reported its own parts (:1539-1541). The
// contenders are the other ordinary claimants, or the full-family ones
// that do not name the owner (:1543-1548).
func (s *structure) ownerFor(claimants []*Endpoint) (owner *Endpoint, contenders []*Endpoint) {
	var ordinary, fullFamily []*Endpoint
	for _, c := range claimants {
		if !s.isOnTheNode(c) {
			continue
		}
		comp, ok := s.composition[c]
		if !ok {
			return nil, nil
		}
		if comp == schema.CompositionFullFamily {
			fullFamily = append(fullFamily, c)
		} else {
			ordinary = append(ordinary, c)
		}
	}
	candidates := ordinary
	if len(candidates) == 0 {
		candidates = fullFamily
	}
	if len(candidates) == 0 {
		return nil, nil
	}
	owner = s.closestClaimant(candidates, append(slices.Clone(ordinary), fullFamily...))
	if len(ordinary) == 0 && !s.partsAccountedFor(owner) {
		return nil, nil
	}
	if len(ordinary) > 0 {
		for _, c := range ordinary {
			if c != owner {
				contenders = append(contenders, c)
			}
		}
		return owner, contenders
	}
	for _, c := range fullFamily {
		if c != owner && !s.partsListOf[c][owner.number] {
			contenders = append(contenders, c)
		}
	}
	return owner, contenders
}

// closestClaimant is the candidate closest to the part the claimants all
// name: the one naming the fewest of the other claimants, then the one
// with the shorter PartsList, then the one with the lower endpoint number
// — the choice depends on the lists, not on the order they arrived in.
// Mirrors matter.js ClientStructure.ts:1553-1578 (#closestClaimant at
// 862ad75f).
func (s *structure) closestClaimant(candidates, claimants []*Endpoint) *Endpoint {
	rank := func(c *Endpoint) [3]int {
		parts := s.partsListOf[c]
		named := 0
		for _, other := range claimants {
			if other != c && parts[other.number] {
				named++
			}
		}
		return [3]int{named, len(parts), int(c.number)}
	}
	closest, closestRank := candidates[0], rank(candidates[0])
	for _, c := range candidates[1:] {
		if r := rank(c); slices.Compare(r[:], closestRank[:]) < 0 {
			closest, closestRank = c, r
		}
	}
	return closest
}

// partsAccountedFor: every endpoint ep names has reported a PartsList of
// its own; an endpoint the root no longer names reports nothing more and
// is not waited for. Mirrors matter.js ClientStructure.ts:1580-1599
// (#partsAccountedFor at 862ad75f).
func (s *structure) partsAccountedFor(ep *Endpoint) bool {
	rootParts, rootReported := s.partsListOf[s.endpoints[0]]
	for partNo := range s.partsListOf[ep] {
		if rootReported && !rootParts[partNo] {
			continue
		}
		part := s.endpoints[partNo]
		if part == nil {
			return false
		}
		if _, ok := s.partsListOf[part]; !ok {
			return false
		}
	}
	return true
}

// schedule queues a structural change (:1488-1495).
func (s *structure) schedule(ep *Endpoint, kind string) {
	ch := s.pending[ep]
	if ch == nil {
		ch = &pendingChange{}
		s.pending[ep] = ch
		s.pendingOrder = append(s.pendingOrder, ep)
	}
	switch kind {
	case "erase":
		ch.erase = true
	case "install":
		ch.install = true
	case "rebuild":
		ch.rebuild = true
	}
}

// emitPendingStructureEvents closes the structure events with Changed
// (:1503-1551); they were queued as the changes applied.
func (s *structure) emitPendingStructureEvents() {
	s.events = append(s.events, emission{kind: emitChanged})
}

// emitPendingEvents delivers the delayed events (:1553-1563).
func (s *structure) emitPendingEvents() {
	for i := range s.delayed {
		s.events = append(s.events, emission{kind: emitEvent, event: s.delayed[i]})
	}
	s.delayed = nil
}

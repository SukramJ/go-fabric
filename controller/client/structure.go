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

// partsList is one endpoint's PartsList as #synchronizeDescriptor reads it
// (ClientStructure.ts:1093-1115 at b25ed960): the endpoint numbers it
// names, in order and each once.
type partsList struct {
	order []uint16
	has   map[uint16]bool
}

// structure is matter.js ClientStructure (node/client/ClientStructure.ts:123-1846
// at b25ed960): the endpoints and clusters the peer's reports describe. Its
// state is guarded by the node's lock; every method expects it held.
type structure struct {
	// mu is the owning Node's lock (Node.mu); it guards every field of the
	// structure, and every method expects it held.
	mu  *sync.RWMutex
	log *slog.Logger
	p   *persister

	endpoints map[uint16]*Endpoint
	root      *Endpoint
	// bytes is the image's charge ([structure.imageBytes]) kept as a
	// running total: each endpoint of endpoints, each of their clusters and
	// each attached cache's values (cache.total points here). admit reads
	// it on every report, which summed every cluster before (review II,
	// N3-finimg-3).
	bytes int

	// pendingOrder and pending are #pendingChanges, a Map in insertion order.
	pendingOrder []*Endpoint
	pending      map[*Endpoint]*pendingChange

	// partsOrder and partsListOf are #partsListOf (ClientStructure.ts
	// :136-144 at b25ed960): the parts each endpoint's current PartsList
	// names, the endpoints in the order their first list arrived. Which
	// endpoint owns a part is derived from them on every apply
	// ([structure.placeParts]); no claim is stored.
	partsOrder  []*Endpoint
	partsListOf map[*Endpoint]*partsList
	// composition is #composition (:157-165), read through
	// [structure.compositionOf].
	composition map[*Endpoint]schema.Composition
	// reportedLists is EndpointStructure.reportedLists (:1893-1902): the
	// Descriptor lists (DeviceTypeList, PartsList) the peer has reported
	// for an endpoint, from the wire or the store.
	reportedLists map[*Endpoint]map[uint32]bool
	// onTheNode is #endpointsOnTheNode (:146-150): the endpoints on the
	// node other than the root as of the last structural apply, nil while
	// the root has not reported its PartsList
	// ([structure.findEndpointsOnTheNode]). keptOffTheNode is
	// #keptOffTheNode (:152-153): endpoints that left the node but stay,
	// because they hold endpoints on the node.
	onTheNode      map[uint16]bool
	keptOffTheNode map[uint16]bool
	// warnedIncompleteRootPartsList and warnedRootWithoutDeviceType keep
	// those warnings to one per node and start (:155, :166).
	warnedIncompleteRootPartsList bool
	warnedRootWithoutDeviceType   bool

	// recordedFabricFiltered is the fabric filter the cache records, nil
	// before one is recorded — ClientNodeStore.cacheFabricFiltered
	// (packages/node/src/storage/client/ClientNodeStore.ts:80-97 at
	// b25ed960); [structure.cacheFabricFiltered] reads it.
	recordedFabricFiltered *bool
	// epoch is the epoch an interaction starting now belongs to
	// (ClientStructure.ts:128, :335-341).
	epoch *epoch

	events  []emission
	delayed []EventData

	// maxImageBytes is the node's image quota, zero or less the default
	// ([structure.quota]); droppedLogged and quotaLogged keep the image
	// bounds' warnings to one each per node ([structure.admit]).
	maxImageBytes int
	droppedLogged bool
	quotaLogged   bool
}

// newStructure is a structure with its root endpoint (ClientStructure.ts:155-164):
// the node itself, installed, of the RootNode device type with an unknown
// revision (ClientNode.ts:401-404).
func newStructure(mu *sync.RWMutex, log *slog.Logger, p *persister) *structure {
	s := &structure{
		mu: mu, log: log, p: p,
		endpoints:      map[uint16]*Endpoint{},
		pending:        map[*Endpoint]*pendingChange{},
		partsListOf:    map[*Endpoint]*partsList{},
		composition:    map[*Endpoint]schema.Composition{},
		reportedLists:  map[*Endpoint]map[uint32]bool{},
		keptOffTheNode: map[uint16]bool{},
		epoch:          &epoch{trusted: true},
	}
	s.root = newEndpoint(mu, 0)
	s.root.deviceType, s.root.classification, s.root.name, s.root.installed = rootNodeDeviceType, "node", "RootNode", true
	s.endpoints[0] = s.root
	s.bytes = imageEndpointBytes
	return s
}

// epoch is ClientStructure.Epoch (ClientStructure.ts:1776-1782 at
// db021da0): an interaction records the data versions it reports only if
// its epoch is still the current one and trusted when its data is
// applied; a change of the cache's fabric filter starts a new one, and
// while the versions are forgotten the epoch is not trusted. Not
// zero-sized, so two epochs never share an address.
type epoch struct{ trusted bool }

// cacheFabricFiltered is the fabric filter of the interactions the cache
// takes data from: only they update cached values and data versions, and
// only they get versions injected. Fabric filtered until a filter is
// recorded. Mirrors matter.js ClientStructure.cacheFabricFiltered
// (packages/node/src/node/client/ClientStructure.ts:326-333 at db021da0).
func (s *structure) cacheFabricFiltered() bool {
	return s.recordedFabricFiltered == nil || *s.recordedFabricFiltered
}

// takeEmissions returns what the structure emitted and forgets it.
func (s *structure) takeEmissions() []emission {
	out := s.events
	s.events = nil
	return out
}

// loadCache builds the structure from the store — ClientStructure.loadCache
// (:183-232): per endpoint the Descriptor first (:199-205), each cluster's
// seed checked (:210) and synchronized (:211); only installations may be
// pending (:217-229); the structure events emitted (:231). Mirrors
// matter.js ClientStructure.ts:251-258 at b25ed960 (loadCache): the
// stored Descriptor lists count as reported (:251), and before the
// installations the endpoints on the node are found, the root's list
// checked and the parts placed (:256-258).
func (s *structure) loadCache(stored []StoredEndpoint) error {
	stored = s.cutBackStored(stored)
	for _, se := range stored {
		ep := s.endpointFor(se.Number)
		clusters := slices.Clone(se.Clusters)
		if i := slices.IndexFunc(clusters, func(c StoredCluster) bool { return c.ID == descriptor.ClusterID }); i > 0 {
			d := clusters[i]
			clusters = append([]StoredCluster{d}, slices.Delete(clusters, i, i+1)...)
		}
		for _, sc := range clusters {
			if se.Number == 0 && (sc.ID == settingsCluster || sc.ID == icdCluster || sc.ID == onboardingCluster) {
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
			s.detachCache(c)
			c.cache = newCache(s.p, se.Number, sc.ID, values, sc.Version)
			s.attachCache(ep, c)
			s.checkUnlistedSeedValues(c)
			s.noteReportedLists(ep, c, func(id uint32) bool { _, ok := c.cache.values[id]; return ok })
			s.synchronizeCluster(ep, c, nil)
		}
	}
	s.onTheNode = s.findEndpointsOnTheNode()
	s.checkRootPartsList()
	s.placeParts()
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
// Mirrors matter.js packages/node/src/node/client/ClientStructure.ts:286-324
// (injectVersionFilters at db021da0): a request with the other fabric
// filter than the cache's, or one sent while the epoch is not trusted,
// gets none injected (:288), and a cluster the caller filters gets no
// injected filter (:293-295, :312), so the peer sees exactly the
// caller's filter for it. caller is not modified.
func (s *structure) withKnownVersions(sc scope, caller []DataVersionFilter, refreshChangesOmitted bool) []DataVersionFilter {
	out := slices.Clone(caller)
	if !s.epoch.trusted || sc.fabricFiltered != s.cacheFabricFiltered() {
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
	// emitsEvents: the interaction's events reach the node's observers
	// and its cursor — options.emitEvents (:412, default false).
	emitsEvents bool
	// epoch is the interaction's (options.epoch, :409, :427).
	epoch *epoch
}

// mutate applies a report — ClientStructure.mutate (:295-380) over a merged
// report: the attribute values in order, batched per consecutive endpoint
// and cluster (:462-504), the events (:506-528), the statuses logged
// (:338-346), the last batch (:357-361), the structural changes (:375),
// the structure events (:378) and the delayed events (:379).
//
// emitEvents is matter.js's options.emitEvents (:412, default false): only
// an interaction that sets it — the default subscription while it is
// current, its bootstrap read and the newly-commissioned read — hands its
// events to the Event hook and raises the event cursor; every other
// returns them in its Report only (#emitOccurrence, :1757-1770).
//
// in is the epoch the interaction started in, nil for the current one
// (options.epoch, :396-397, :427).
func (s *structure) mutate(sc scope, r *Report, emitEvents bool, in *epoch) {
	if in == nil {
		in = s.epoch
	}
	q := &mutateContext{endpointsWithData: map[uint16]bool{}, clustersWithData: map[*Cluster]bool{}, emitsEvents: emitEvents, epoch: in}
	var cur *batch
	attrs := s.admit(sc, r)
	for i := range attrs {
		cur = s.mutateAttribute(&attrs[i], sc, cur, q)
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
// interaction whose fabric filter differs from the cache's (:573-576 at
// 862ad75f); the endpoint is recorded (:477); a change of endpoint or
// cluster flushes the batch (:479-484); a new batch carries the report's
// version only when the read covered the whole cluster (:494-497).
func (s *structure) mutateAttribute(a *AttributeData, sc scope, cur *batch, q *mutateContext) *batch {
	if s.cacheFabricFiltered() != sc.fabricFiltered {
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
	// Mirrors matter.js ClientStructure.ts:1757-1758: no emit unless
	// q.emitsEvents() (W4-ctlim-01).
	if !q.emitsEvents {
		return
	}
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

// updateCluster applies one batch — #applyUpdates and #updateCluster
// (:673-695 at 862ad75f): data of an interaction that began before a
// fabric filter switch, or while the epoch is not trusted, is applied with
// an unknown version (:674-678).
func (s *structure) updateCluster(b *batch, q *mutateContext) {
	ep := s.endpointFor(b.ep)
	c := s.clusterFor(ep, b.cluster)
	q.clustersWithData[c] = true
	s.preserveAbsentCluster(ep, c)
	s.pruneUnlistedAttributes(c, b.values)
	s.invalidateOnDefinitionChange(c, b.values)
	if q.epoch != s.epoch || !q.epoch.trusted {
		c.cache.externalSetUnversioned(b.values)
	} else {
		c.cache.externalSet(b.values, b.version)
	}
	// Mirrors matter.js ClientStructure.ts:713-715 at b25ed960
	// (#updateCluster): the Descriptor lists the batch carries count as
	// reported before the cluster is synchronized.
	s.noteReportedLists(ep, c, func(id uint32) bool { v, ok := b.values[id]; return ok && v != nil })
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

// noteReportedLists marks the DeviceTypeList and the PartsList reported
// for ep when carries says a Descriptor value holds them. Mirrors
// matter.js packages/node/src/node/client/ClientStructure.ts:983-999 at
// b25ed960 (#noteReportedLists): the Descriptor behavior's state reads an
// unreported mandatory list as empty, so whether the peer reported one is
// kept apart.
func (s *structure) noteReportedLists(ep *Endpoint, c *Cluster, carries func(id uint32) bool) {
	if c.id != descriptor.ClusterID {
		return
	}
	for _, id := range []uint32{descriptor.AttrDeviceTypeList, descriptor.AttrPartsList} {
		if !carries(id) {
			continue
		}
		if s.reportedLists[ep] == nil {
			s.reportedLists[ep] = map[uint32]bool{}
		}
		s.reportedLists[ep][id] = true
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
	// Mirrors matter.js ClientStructure.ts:1013-1016 at b25ed960
	// (#synchronizeDescriptor): the root's list is checked only once the
	// peer has reported it.
	if ep.number == 0 && s.reportedLists[ep][descriptor.AttrDeviceTypeList] {
		s.checkRootDeviceTypes(entries)
	}
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
// once any entry names a device type, full-family when any of them is; a
// device type the schema does not know counts as a tree. Mirrors matter.js
// ClientStructure.ts:1686-1707 at b25ed960 (#noteComposition); it is read
// through [structure.compositionOf].
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

// synchronizePartsList is the PartsList half of #synchronizeDescriptor.
// Mirrors matter.js ClientStructure.ts:1093-1115 at b25ed960: the list's
// valid endpoint numbers (EndpointNumber.isValid, 0 to 0xFFFE) other than
// the root's, which is no endpoint's part (:1104-1107), each given its
// endpoint; which endpoint owns a part is decided by
// [structure.placeParts] once every list of the interaction is in.
func (s *structure) synchronizePartsList(ep *Endpoint, v spec.Node) {
	named := &partsList{has: map[uint16]bool{}}
	for _, e := range v.Children {
		if !isUint(e) || e.El.Uint == 0 || e.El.Uint > 0xFFFE {
			continue
		}
		partNo := uint16(e.El.Uint)
		if named.has[partNo] {
			continue
		}
		named.has[partNo] = true
		named.order = append(named.order, partNo)
		s.endpointFor(partNo)
	}
	if _, ok := s.partsListOf[ep]; !ok {
		s.partsOrder = append(s.partsOrder, ep)
	}
	s.partsListOf[ep] = named
}

// forgetPartsList drops ep's PartsList.
func (s *structure) forgetPartsList(ep *Endpoint) {
	if _, ok := s.partsListOf[ep]; !ok {
		return
	}
	delete(s.partsListOf, ep)
	s.partsOrder = slices.DeleteFunc(s.partsOrder, func(x *Endpoint) bool { return x == ep })
}

// visitBelow calls f for every installed endpoint below ep, each once:
// the walk does not rely on the tree being acyclic (review II,
// R2-P2-alloc-01).
func (s *structure) visitBelow(ep *Endpoint, f func(*Endpoint)) {
	s.visitBelowOnce(ep, f, map[*Endpoint]bool{ep: true})
}

func (s *structure) visitBelowOnce(ep *Endpoint, f func(*Endpoint), seen map[*Endpoint]bool) {
	for _, c := range ep.partsLocked() {
		if seen[c] {
			continue
		}
		seen[c] = true
		f(c)
		s.visitBelowOnce(c, f, seen)
	}
}

// endpointFor is #endpointFor (:1011-1037).
func (s *structure) endpointFor(number uint16) *Endpoint {
	if ep := s.endpoints[number]; ep != nil {
		return ep
	}
	ep := newEndpoint(s.mu, number)
	s.endpoints[number] = ep
	s.bytes += imageEndpointBytes
	return ep
}

// clusterFor is #clusterFor (:1039-1057).
func (s *structure) clusterFor(ep *Endpoint, id uint32) *Cluster {
	if c := ep.clusters[id]; c != nil {
		return c
	}
	c := newCluster(s.mu, id, newCache(s.p, ep.number, id, nil, nil))
	ep.clusters[id] = c
	if s.endpoints[ep.number] == ep {
		s.bytes += imageClusterBytes
		s.attachCache(ep, c)
	}
	return c
}

// attachCache counts c's cache in the image's running total while ep is
// the structure's endpoint of its number; its values' changes follow.
func (s *structure) attachCache(ep *Endpoint, c *Cluster) {
	if s.endpoints[ep.number] != ep || c.cache.total != nil {
		return
	}
	c.cache.total = &s.bytes
	s.bytes += c.cache.bytes
}

// detachCache takes c's cache out of the image's running total.
func (s *structure) detachCache(c *Cluster) {
	if c.cache.total != &s.bytes {
		return
	}
	s.bytes -= c.cache.bytes
	c.cache.total = nil
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
// (ClientStructure.ts:1237-1262 at b25ed960, Endpoint.delete closing its
// parts): its PartsList, composition, reported lists and kept mark go with
// it (:1246-1249). Each endpoint is erased once: the walk does not rely on the tree
// being acyclic (review II, R2-P2-alloc-01).
func (s *structure) erase(ep *Endpoint) {
	s.eraseOnce(ep, map[*Endpoint]bool{})
}

func (s *structure) eraseOnce(ep *Endpoint, seen map[*Endpoint]bool) {
	if seen[ep] {
		return
	}
	seen[ep] = true
	for _, part := range ep.partsLocked() {
		s.eraseOnce(part, seen)
	}
	if s.endpoints[ep.number] == ep {
		delete(s.endpoints, ep.number)
		s.bytes -= imageEndpointBytes + len(ep.clusters)*imageClusterBytes
		for _, c := range ep.clusters {
			s.detachCache(c)
		}
	}
	s.forgetPartsList(ep)
	delete(s.composition, ep)
	delete(s.reportedLists, ep)
	delete(s.keptOffTheNode, ep.number)
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
			if s.endpoints[ep.number] == ep {
				s.bytes -= imageClusterBytes
				s.detachCache(c)
			}
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

// applyPendingChanges decides which endpoints are on the node and the owner
// of every part whose owner is now known, then applies each queued change
// in order: erase, else rebuild and install. Mirrors matter.js
// ClientStructure.ts:1370-1396 at b25ed960 (#applyPendingChanges): the
// endpoints on the node found again (:1371-1373), the root's list checked
// (:1374), the endpoints that left the node erased (:1375-1377), the parts
// placed (:1378), then the queued changes (:1380-1395).
func (s *structure) applyPendingChanges() {
	previous := s.onTheNode
	current := s.findEndpointsOnTheNode()
	s.onTheNode = current
	s.checkRootPartsList()
	if previous != nil && current != nil {
		s.eraseEndpointsOffTheNode(previous, current)
	}
	s.placeParts()
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

// placeParts gives every part on the node whose owner is now known an
// owner and schedules its installation. Mirrors matter.js
// ClientStructure.ts:1408-1456 at b25ed960 (#placeParts): the claimants of
// each part are read from the current PartsLists on every call
// (:1409-1424), so a part that cannot be placed yet is considered again on
// the next apply; nothing is reparented and only a part on the node is
// placed (:1430-1434), and only below a placed owner (:1436-1439); the
// other claimants are warned about (:1441-1448). An endpoint naming itself
// claims nothing there (review II, R2-P2-alloc-01): matter.js counts it
// among its own claimants, which leaves it unplaced when it is the
// closest.
func (s *structure) placeParts() {
	var parts []*Endpoint
	claimantsOf := map[*Endpoint][]*Endpoint{}
	for _, claimant := range s.partsOrder {
		for _, partNo := range s.partsListOf[claimant].order {
			part := s.endpoints[partNo]
			if part == nil || part == claimant {
				continue
			}
			if _, ok := claimantsOf[part]; !ok {
				parts = append(parts, part)
			}
			claimantsOf[part] = append(claimantsOf[part], claimant)
		}
	}
	for placed := true; placed; {
		placed = false
		for _, part := range parts {
			if s.isPlaced(part) || !s.isOnTheNode(part) {
				continue
			}
			owner, contenders := s.ownerFor(claimantsOf[part])
			if owner == nil || !s.isPlaced(owner) {
				continue
			}
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
// (ClientStructure.ts:1462-1464 at b25ed960).
func (s *structure) isPlaced(ep *Endpoint) bool { return ep.number == 0 || s.ownerOf(ep) != nil }

// isOnTheNode is #isOnTheNode (ClientStructure.ts:1470-1473 at b25ed960).
func (s *structure) isOnTheNode(ep *Endpoint) bool { return s.isNumberOnTheNode(ep.number) }

// isNumberOnTheNode: the root, or an endpoint on the node as of the last
// structural apply; every endpoint while the root has not reported its
// PartsList. Mirrors matter.js ClientStructure.ts:1475-1481 at b25ed960
// (#isNumberOnTheNode).
func (s *structure) isNumberOnTheNode(number uint16) bool {
	return number == 0 || s.onTheNode == nil || s.onTheNode[number]
}

// findEndpointsOnTheNode is the endpoints on the node other than the root,
// nil while the root has not reported its PartsList: those the root's
// PartsList names, and those the PartsList of an endpoint on the node
// names once the peer reported their DeviceTypeList or PartsList. Mirrors
// matter.js ClientStructure.ts:1491-1511 at b25ed960
// (#findEndpointsOnTheNode).
func (s *structure) findEndpointsOnTheNode() map[uint16]bool {
	rootParts := s.rootPartsList()
	if rootParts == nil {
		return nil
	}
	found := make(map[uint16]bool, len(rootParts.order))
	pending := slices.Clone(rootParts.order)
	for _, no := range pending {
		found[no] = true
	}
	for len(pending) > 0 {
		no := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		ep := s.endpoints[no]
		if ep == nil || s.partsListOf[ep] == nil {
			continue
		}
		for _, part := range s.partsListOf[ep].order {
			if found[part] || !s.hasReportedDescriptorList(part) {
				continue
			}
			found[part] = true
			pending = append(pending, part)
		}
	}
	return found
}

// hasReportedDescriptorList: the peer reported the endpoint's
// DeviceTypeList or PartsList (ClientStructure.ts:1513-1515 at b25ed960).
func (s *structure) hasReportedDescriptorList(number uint16) bool {
	ep := s.endpoints[number]
	return ep != nil && len(s.reportedLists[ep]) > 0
}

// rootPartsList is the root's PartsList, nil while it has not reported one
// (ClientStructure.ts:1517-1520 at b25ed960).
func (s *structure) rootPartsList() *partsList {
	root := s.endpoints[0]
	if root == nil {
		return nil
	}
	return s.partsListOf[root]
}

// eraseEndpointsOffTheNode schedules the erase of every placed endpoint
// that left the node since previous. Mirrors matter.js
// ClientStructure.ts:1529-1562 at b25ed960 (#eraseEndpointsOffTheNode):
// only an endpoint the peer had on the node is erased (:1531-1536); one
// that holds an endpoint still on the node is kept and checked again on
// every apply, warned about the first time and logged at debug after
// (:1538-1560).
func (s *structure) eraseEndpointsOffTheNode(previous, current map[uint16]bool) {
	candidates := maps.Clone(previous)
	for no := range s.keptOffTheNode {
		candidates[no] = true
	}
	for _, no := range slices.Sorted(maps.Keys(candidates)) {
		ep := s.endpoints[no]
		if current[no] || ep == nil || !s.isPlaced(ep) {
			delete(s.keptOffTheNode, no)
			continue
		}
		var held []uint16
		s.visitBelow(ep, func(below *Endpoint) {
			if current[below.number] {
				held = append(held, below.number)
			}
		})
		if len(held) == 0 {
			delete(s.keptOffTheNode, no)
			s.schedule(ep, "erase")
			continue
		}
		attrs := []any{
			slog.Int("endpoint", int(no)), slog.Any("holds", held),
			slog.String("reason", "it is no longer on the node but holds endpoints that are"),
		}
		if s.keptOffTheNode[no] {
			s.log.Debug("matter.controller.client.endpoint_kept", attrs...)
			continue
		}
		s.log.Warn("matter.controller.client.endpoint_kept", attrs...)
		s.keptOffTheNode[no] = true
	}
}

// checkRootPartsList warns once per node and start when the root's
// PartsList does not name every endpoint on the node. Mirrors matter.js
// ClientStructure.ts:1565-1581 at b25ed960 (#checkRootPartsList).
func (s *structure) checkRootPartsList() {
	rootParts := s.rootPartsList()
	if s.warnedIncompleteRootPartsList || rootParts == nil || s.onTheNode == nil {
		return
	}
	var unnamed []uint16
	for no := range s.onTheNode {
		if !rootParts.has[no] {
			unnamed = append(unnamed, no)
		}
	}
	if len(unnamed) == 0 {
		return
	}
	slices.Sort(unnamed)
	s.warnedIncompleteRootPartsList = true
	s.log.Warn("matter.controller.client.root_parts_list_incomplete", slog.Any("endpoints", unnamed),
		slog.String("reason", "the root must list every endpoint; other endpoints name them, so they are on the node"))
}

// ownerFor is the claimant that owns a part, with the claimants that
// conflict with that choice, nil while that cannot be told. Mirrors
// matter.js packages/node/src/node/client/ClientStructure.ts:1587-1627 at
// b25ed960 (#ownerFor): a claimant not on the node owns nothing
// (:1592-1596), one whose composition is unknown makes the part wait
// (:1598-1603); an ordinary claimant is closer to the part than any
// full-family one (:1608-1612); among the candidates the closest wins
// ([structure.closestClaimant]); a full-family owner waits until every
// endpoint it names has reported its own parts (:1614-1617). The
// contenders are the other ordinary claimants, or the full-family ones
// that do not name the owner (:1619-1624).
func (s *structure) ownerFor(claimants []*Endpoint) (owner *Endpoint, contenders []*Endpoint) {
	var ordinary, fullFamily []*Endpoint
	for _, c := range claimants {
		if !s.isOnTheNode(c) {
			continue
		}
		comp, ok := s.compositionOf(c)
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
		if c != owner && !s.partsListOf[c].has[owner.number] {
			contenders = append(contenders, c)
		}
	}
	return owner, contenders
}

// closestClaimant is the candidate closest to the part the claimants all
// name: the one naming the fewest of the other claimants, then the one
// with the shorter PartsList, then the one with the lower endpoint number
// — the choice depends on the lists, not on the order they arrived in.
// Mirrors matter.js ClientStructure.ts:1636-1657 at b25ed960
// (#closestClaimant).
func (s *structure) closestClaimant(candidates, claimants []*Endpoint) *Endpoint {
	rank := func(c *Endpoint) [3]int {
		parts := s.partsListOf[c]
		named := 0
		for _, other := range claimants {
			if other != c && parts.has[other.number] {
				named++
			}
		}
		return [3]int{named, len(parts.order), int(c.number)}
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
// its own; an endpoint not on the node reports nothing more and is not
// waited for. Mirrors matter.js ClientStructure.ts:1664-1676 at b25ed960
// (#partsAccountedFor).
func (s *structure) partsAccountedFor(ep *Endpoint) bool {
	for _, partNo := range s.partsListOf[ep].order {
		if !s.isNumberOnTheNode(partNo) {
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

// compositionOf is how ep composes its PartsList, false while it has not
// said. Mirrors matter.js ClientStructure.ts:1715-1720 at b25ed960
// (#compositionOf): the root's PartsList lists every endpoint on the node,
// so it is full-family whatever device types it reports.
func (s *structure) compositionOf(ep *Endpoint) (schema.Composition, bool) {
	if ep.number == 0 {
		return schema.CompositionFullFamily, true
	}
	c, ok := s.composition[ep]
	return c, ok
}

// checkRootDeviceTypes warns once per node and start when the root reports
// no device type. Mirrors matter.js ClientStructure.ts:1731-1740 at
// b25ed960 (#checkRootDeviceTypes); placing the endpoints does not depend
// on it ([structure.compositionOf]).
func (s *structure) checkRootDeviceTypes(entries []descriptor.DeviceTypeStruct) {
	if s.warnedRootWithoutDeviceType || len(entries) > 0 {
		return
	}
	s.warnedRootWithoutDeviceType = true
	s.log.Warn("matter.controller.client.root_without_device_type",
		slog.String("reason", "the root must report at least one device type; its PartsList is read as listing every endpoint on the node"))
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

// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package client

import (
	"log/slog"

	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/cluster/spec/descriptor"
)

// DefaultMaxImageBytesPerNode is the image quota of a node when none is
// configured ([RegistryConfig].MaxImageBytesPerNode): the bytes of the
// attribute values one node's image holds, in their stored form. Module's
// own (review II, R2-P3-ctl-dev-02, owner decision 54): matter.js bounds
// neither what a peer's reports store nor their sum
// (ClientStructure.ts:568-611, :1147-1199 at db021da0). The value is
// chosen, sixteen times interaction.DefaultMaxReportBytesPerSession; a
// node's own image is far below it.
const DefaultMaxImageBytesPerNode = 32 << 20

// quota is the node's image quota.
func (s *structure) quota() int {
	if s.maxImageBytes <= 0 {
		return DefaultMaxImageBytesPerNode
	}
	return s.maxImageBytes
}

// imageBytes is the bytes of the image's attribute values in their stored
// form, every endpoint and cluster of the image together; the node's own
// settings, record, ICD and onboarding entries are no cluster of the image
// and do not count.
func (s *structure) imageBytes() int {
	total := 0
	for _, ep := range s.endpoints {
		for _, c := range ep.clusters {
			total += c.cache.bytes
		}
	}
	return total
}

// descriptorList reads a Descriptor list of one endpoint.
type descriptorList func(ep uint16, attr uint32) (spec.Node, bool)

// structureNames is what the peer's structure names: endpoint 0 and every
// endpoint reached from it through PartsLists, each visited once, with
// every cluster on it.
func structureNames(list descriptorList) func(ep uint16, cluster uint32) bool {
	named := map[uint16]bool{0: true}
	queue := []uint16{0}
	for len(queue) > 0 {
		ep := queue[0]
		queue = queue[1:]
		parts, ok := list(ep, descriptor.AttrPartsList)
		if !ok {
			continue
		}
		for _, e := range parts.Children {
			// EndpointNumber.isValid, 0 to 0xFFFE, as synchronizePartsList.
			if !isUint(e) || e.El.Uint > 0xFFFE || named[uint16(e.El.Uint)] {
				continue
			}
			named[uint16(e.El.Uint)] = true
			queue = append(queue, uint16(e.El.Uint))
		}
	}
	return func(ep uint16, _ uint32) bool { return named[ep] }
}

// admit is the attribute data of r the image takes (review II,
// R2-P3-ctl-dev-02, owner decision 54; module's own, matter.js stores
// every path a report names, ClientStructure.ts:568-611 at db021da0):
//
//   - a path the peer's structure names ([structureNames], judged on the
//     report's own Descriptor lists where it carries them, else the
//     image's, so a priming report keeps the data that precedes its
//     Descriptor) or the request asked for ([scope].asks); any other is
//     dropped, logged once per node;
//   - a report whose kept values would raise the image above the node's
//     quota, and above its present size, is refused whole: nothing of it
//     is applied, logged once per node.
//
// A report whose fabric filter differs from the cache's stores nothing
// (mutateAttribute) and is returned as it is.
func (s *structure) admit(sc scope, r *Report) []AttributeData {
	if len(r.Attributes) == 0 || s.cacheFabricFiltered() != sc.fabricFiltered {
		return r.Attributes
	}
	reported := map[uint16]map[uint32]spec.Node{}
	for i := range r.Attributes {
		a := &r.Attributes[i]
		p := a.Path
		if p.Cluster != descriptor.ClusterID || p.Attribute != descriptor.AttrPartsList || !isList(a.Value) {
			continue
		}
		if reported[p.Endpoint] == nil {
			reported[p.Endpoint] = map[uint32]spec.Node{}
		}
		reported[p.Endpoint][p.Attribute] = a.Value
	}
	names := structureNames(func(ep uint16, attr uint32) (spec.Node, bool) {
		if v, ok := reported[ep][attr]; ok {
			return v, true
		}
		if e := s.endpoints[ep]; e != nil {
			if d := e.clusters[descriptor.ClusterID]; d != nil {
				if v, ok := d.cache.values[attr]; ok && isList(v) {
					return v, true
				}
			}
		}
		return spec.Node{}, false
	})
	type place struct {
		ep      uint16
		cluster uint32
	}
	type key struct {
		place
		attr uint32
	}
	decided := map[place]bool{}
	sizes := map[key]int{}
	kept := make([]AttributeData, 0, len(r.Attributes))
	for i := range r.Attributes {
		a := &r.Attributes[i]
		at := place{a.Path.Endpoint, a.Path.Cluster}
		keep, ok := decided[at]
		if !ok {
			keep = sc.asks(at.ep, at.cluster) || names(at.ep, at.cluster)
			decided[at] = keep
		}
		if !keep {
			if !s.droppedLogged {
				s.droppedLogged = true
				s.log.Warn("matter.controller.client.report_path_dropped", slog.Int("endpoint", int(at.ep)),
					slog.Uint64("cluster", uint64(at.cluster)), slog.Uint64("attribute", uint64(a.Path.Attribute)),
					slog.String("reason", "the peer's structure does not name the path and the request did not ask for it; further paths of the node are dropped without a log line"))
			}
			continue
		}
		kept = append(kept, *a)
		sizes[key{at, a.Path.Attribute}] = valueSize(a.Value)
	}
	before := s.imageBytes()
	after := before
	for k, size := range sizes {
		if e := s.endpoints[k.ep]; e != nil {
			if c := e.clusters[k.cluster]; c != nil {
				after -= c.cache.sizes[k.attr]
			}
		}
		after += size
	}
	if quota := s.quota(); after > quota && after > before {
		if !s.quotaLogged {
			s.quotaLogged = true
			s.log.Warn("matter.controller.client.image_quota_reached", slog.Int("bytes", before),
				slog.Int("total", after), slog.Int("max_bytes", quota),
				slog.String("reason", "the report's values would raise the node's image above its quota; the report is not applied, and further refusals of the node are not logged"))
		}
		return nil
	}
	return kept
}

// privateCluster is one of the node's own entries the store keeps on
// endpoint 0 beside the image (settings, record, ICD, onboarding).
func privateCluster(ep uint16, cluster uint32) bool {
	return ep == 0 && (cluster == settingsCluster || cluster == recordCluster || cluster == icdCluster || cluster == onboardingCluster)
}

// storedImageBytes is the bytes of the image's values in stored, the
// node's own entries aside.
func storedImageBytes(stored []StoredEndpoint) int {
	total := 0
	for _, se := range stored {
		for _, sc := range se.Clusters {
			if privateCluster(se.Number, sc.ID) {
				continue
			}
			for _, b := range sc.Values {
				total += len(b)
			}
		}
	}
	return total
}

// cutBackStored is stored without what the structure the stored
// Descriptors describe does not name, when the stored image exceeds the
// quota: an image an earlier version stored beyond it loads, cut back by
// the rule a report meets ([structure.admit]; no request asks for
// anything here). What is dropped is erased from the store. The node's own
// entries on endpoint 0 stay. An image still above the quota loads too; a
// report that would grow it is refused.
func (s *structure) cutBackStored(stored []StoredEndpoint) []StoredEndpoint {
	total, quota := storedImageBytes(stored), s.quota()
	if total <= quota {
		return stored
	}
	lists := map[uint16]map[uint32][]byte{}
	for _, se := range stored {
		for _, sc := range se.Clusters {
			if sc.ID == descriptor.ClusterID {
				lists[se.Number] = sc.Values
			}
		}
	}
	names := structureNames(func(ep uint16, attr uint32) (spec.Node, bool) {
		b, ok := lists[ep][attr]
		if !ok {
			return spec.Node{}, false
		}
		v, err := decodeValue(b)
		if err != nil || !isList(v) {
			return spec.Node{}, false
		}
		return v, true
	})
	out := make([]StoredEndpoint, 0, len(stored))
	dropped := 0
	for _, se := range stored {
		keep := StoredEndpoint{Number: se.Number}
		var cut []uint32
		for _, sc := range se.Clusters {
			if privateCluster(se.Number, sc.ID) || names(se.Number, sc.ID) {
				keep.Clusters = append(keep.Clusters, sc)
			} else {
				cut = append(cut, sc.ID)
			}
		}
		dropped += len(cut)
		if len(keep.Clusters) == 0 {
			s.eraseStored(se.Number, nil)
			continue
		}
		s.eraseStored(se.Number, cut)
		out = append(out, keep)
	}
	s.log.Warn("matter.controller.client.image_cut_at_load", slog.Int("bytes", total), slog.Int("trimmed_bytes", total-storedImageBytes(out)),
		slog.Int("max_bytes", quota), slog.Int("dropped", dropped),
		slog.String("reason", "the stored image exceeds the node's quota; what the node's structure does not name is dropped from the image and the store"))
	return out
}

// eraseStored erases endpoint ep from the store, or with clusters only
// those clusters of it; a failure is logged.
func (s *structure) eraseStored(ep uint16, clusters []uint32) {
	if clusters == nil {
		if err := s.p.store.EraseEndpoint(s.p.nodeID, ep); err != nil {
			s.log.Warn("matter.controller.client.store_erase_failed", slog.Int("endpoint", int(ep)), slog.Any("err", err))
		}
		return
	}
	for _, id := range clusters {
		if err := s.p.store.EraseCluster(s.p.nodeID, ep, id); err != nil {
			s.log.Warn("matter.controller.client.store_erase_failed", slog.Int("endpoint", int(ep)),
				slog.Uint64("cluster", uint64(id)), slog.Any("err", err))
		}
	}
}

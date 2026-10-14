// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package client

import (
	"slices"
	"strings"

	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/cluster/spec/descriptor"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/schema"
)

// networkCommissioningClusterID is NetworkCommissioning (0x0031), whose
// presence on the root decides CustomNetworkConfig for a remote node.
const networkCommissioningClusterID uint32 = 0x0031

// ConformanceStatus is the verdict of a [ConformanceReport].
type ConformanceStatus int

// Conformance statuses.
const (
	// ConformanceUnknown: nothing was judged — the endpoint is not on the
	// image, its Descriptor's DeviceTypeList or ServerList has not
	// arrived, or no device type it lists is in the model.
	ConformanceUnknown ConformanceStatus = iota
	// ConformanceIncomplete: what was judged conforms, but a server
	// cluster the ServerList names has no shape yet (or its FeatureMap,
	// AttributeList or AcceptedCommandList has not arrived): the node is
	// still being read.
	ConformanceIncomplete
	// ConformanceClean: every server requirement was judged and none is
	// violated.
	ConformanceClean
	// ConformanceViolated: a judged requirement is violated.
	ConformanceViolated
)

// String names the status.
func (s ConformanceStatus) String() string {
	switch s {
	case ConformanceIncomplete:
		return "incomplete"
	case ConformanceClean:
		return "clean"
	case ConformanceViolated:
		return "violated"
	default:
		return "unknown"
	}
}

// ConformanceReport is the device type validator's report for an endpoint
// of the image ([Endpoint.Conformance]).
type ConformanceReport struct {
	Status ConformanceStatus
	// Violations are the departures of the endpoint from the server-side,
	// composition and condition requirements of the device types it lists
	// and of Base, as endpoint.DeviceTypeTree.Check finds them: a
	// mandatory server cluster missing, a present cluster lacking a
	// mandatory feature, attribute or command (judged on the cluster's
	// reported FeatureMap, AttributeList and AcceptedCommandList, never on
	// values).
	Violations []endpoint.DeviceTypeViolation
	// Clients are the client cluster requirements the Descriptor's
	// ClientList departs from — informational: they never make the status
	// violated.
	Clients []endpoint.DeviceTypeViolation
	// Unjudged are the server clusters the ServerList names that were not
	// judged inside, in id order.
	Unjudged []uint32
}

// Conformance judges the endpoint's image against the device types its
// Descriptor lists, with the one validator the server side runs
// (endpoint.DeviceTypeTree over endpoint.DeviceTypeSubject — matter.js's
// DeviceTypeConformance over DeviceTypeFacts,
// packages/model/src/logic/device-types/DeviceTypeConformance.ts,
// DeviceTypeFacts.ts:16-69, which matter.js answers for a server node
// only). The whole image is the tree, so the endpoint's node scope decides
// its conditions.
//
// What the image cannot tell is not judged rather than failed: a cluster
// without a built shape ([Cluster.Ready] false) or without its FeatureMap,
// AttributeList or AcceptedCommandList is judged present but nothing
// inside it (Unjudged, and the status at best incomplete); an event
// requirement is judged only when the peer reports an EventList; no
// condition is stated for a remote endpoint. CustomNetworkConfig holds
// for a node whose root serves no NetworkCommissioning — the only way a
// commissioned node without one got its network.
func (e *Endpoint) Conformance() ConformanceReport {
	tree, ok := e.conformanceTree()
	if !ok {
		return ConformanceReport{Status: ConformanceUnknown}
	}
	self := tree.subjects[e.number]
	if !self.modelled() {
		return ConformanceReport{Status: ConformanceUnknown}
	}
	report := ConformanceReport{Unjudged: self.unjudged()}
	found := tree.tree.Check(e.number)
	for i := range found {
		if strings.HasPrefix(found[i].Requirement, "client:") {
			report.Clients = append(report.Clients, found[i])
			continue
		}
		report.Violations = append(report.Violations, found[i])
	}
	switch {
	case len(report.Violations) > 0:
		report.Status = ConformanceViolated
	case len(report.Unjudged) > 0:
		report.Status = ConformanceIncomplete
	default:
		report.Status = ConformanceClean
	}
	return report
}

// imageTree is the image as the validator reads it.
type imageTree struct {
	tree     endpoint.DeviceTypeTree
	subjects map[uint16]*imageSubject
}

// conformanceTree snapshots the installed tree e is on, under the image's
// lock; ok false when e is not installed or its Descriptor has not
// delivered its DeviceTypeList and ServerList.
func (e *Endpoint) conformanceTree() (imageTree, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if !e.installed || e.destroyed {
		return imageTree{}, false
	}
	root := e
	for root.owner != nil {
		root = root.owner
	}
	t := imageTree{subjects: map[uint16]*imageSubject{}}
	var visit func(ep *Endpoint)
	visit = func(ep *Endpoint) {
		if _, seen := t.subjects[ep.number]; seen {
			return
		}
		s := snapshotSubject(ep)
		t.subjects[ep.number] = s
		t.tree.Endpoints = append(t.tree.Endpoints, s)
		for _, part := range ep.partsLocked() {
			visit(part)
		}
	}
	visit(root)
	if self := t.subjects[e.number]; self == nil || !self.described {
		return imageTree{}, false
	}
	custom := !slices.Contains(t.subjects[root.number].servers, networkCommissioningClusterID)
	t.tree.NodeConditions = func(uint16) []string {
		if custom {
			return []string{"CustomNetworkConfig"}
		}
		return nil
	}
	return t, true
}

// imageSubject is one endpoint of the image as the validator reads it, a
// copy taken under the image's lock.
type imageSubject struct {
	number    uint16
	described bool
	types     []uint32
	servers   []uint32
	clients   []uint32
	parts     []uint16
	shapes    map[uint32]endpoint.ClusterShape
}

// snapshotSubject copies ep's Descriptor lists and its clusters' shapes;
// the caller holds the lock.
func snapshotSubject(ep *Endpoint) *imageSubject {
	s := &imageSubject{number: ep.number, shapes: map[uint32]endpoint.ClusterShape{}}
	for _, part := range ep.partsLocked() {
		s.parts = append(s.parts, part.number)
	}
	if d := ep.clusters[descriptor.ClusterID]; d != nil {
		values := d.cache.values
		dtl, hasTypes := values[descriptor.AttrDeviceTypeList]
		sl, hasServers := values[descriptor.AttrServerList]
		s.described = hasTypes && isList(dtl) && hasServers && isList(sl)
		if s.described {
			for _, dt := range deviceTypeEntries(dtl) {
				s.types = append(s.types, dt.DeviceType)
			}
			s.servers = rawIDList(sl)
		}
		if cl, ok := values[descriptor.AttrClientList]; ok {
			s.clients = rawIDList(cl)
		}
	}
	for _, id := range s.servers {
		c := ep.clusters[id]
		if c == nil || c.injected == nil {
			continue
		}
		sh := endpoint.ClusterShape{
			FeatureMap: c.injected.featureMap, FeaturesKnown: c.in.hasFeatures,
			Attributes: slices.Clone(c.injected.attributes), AttributesKnown: c.injected.attributeListReported,
			AcceptedCommands: slices.Clone(c.injected.accepted),
		}
		if v, ok := c.cache.values[spec.AttrAcceptedCommandList]; ok && isList(v) {
			sh.CommandsKnown = true
		}
		if v, ok := c.cache.values[spec.AttrEventList]; ok && isList(v) {
			sh.Events, sh.EventsKnown = idList(v), true
		}
		s.shapes[id] = sh
	}
	return s
}

// rawIDList is a list node's numeric entries in the peer's order, without
// duplicates.
func rawIDList(n spec.Node) []uint32 {
	if !isList(n) {
		return nil
	}
	out := make([]uint32, 0, len(n.Children))
	for _, e := range n.Children {
		if isUint(e) && e.El.Uint <= 0xFFFF_FFFF && !slices.Contains(out, uint32(e.El.Uint)) {
			out = append(out, uint32(e.El.Uint))
		}
	}
	return out
}

// modelled reports whether a device type the endpoint lists is in the
// model — what the validator can judge it by.
func (s *imageSubject) modelled() bool {
	return slices.ContainsFunc(s.types, func(id uint32) bool {
		_, ok := schema.DeviceTypeDefinitionOf(id)
		return ok
	})
}

// unjudged are the listed server clusters whose inside is not judged: no
// shape, or no FeatureMap, AttributeList or AcceptedCommandList yet.
func (s *imageSubject) unjudged() []uint32 {
	var out []uint32
	for _, id := range s.servers {
		sh, ok := s.shapes[id]
		if !ok || !sh.FeaturesKnown || !sh.AttributesKnown || !sh.CommandsKnown {
			out = append(out, id)
		}
	}
	slices.Sort(out)
	return out
}

func (s *imageSubject) EndpointNumber() uint16     { return s.number }
func (s *imageSubject) DeviceTypeIDs() []uint32    { return s.types }
func (s *imageSubject) ServerClusterIDs() []uint32 { return s.servers }
func (s *imageSubject) ClientClusterIDs() []uint32 { return s.clients }
func (s *imageSubject) StatedConditions() []string { return nil }
func (s *imageSubject) PartNumbers() []uint16      { return s.parts }

func (s *imageSubject) ServerClusterShape(cluster uint32) (endpoint.ClusterShape, bool) {
	sh, ok := s.shapes[cluster]
	return sh, ok
}

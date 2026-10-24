// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package endpoint

import (
	"fmt"
	"slices"

	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/schema"
)

// DeviceTypeSubject is one endpoint as device type validation reads it:
// matter.js's DeviceTypeFacts asked of one endpoint
// (packages/model/src/logic/device-types/DeviceTypeFacts.ts:16-69). It is
// the validator's one input: an assembled endpoint answers it (the
// topology's adapter, [ValidateDeviceTypes]) and so does a controller's
// image of a remote endpoint (controller/client's Endpoint.Conformance), so
// both are judged by the one implementation.
//
// matter.js answers the facts for a server node only
// (packages/node/src/node/server/ServerEndpointFacts.ts:25); a client
// view's subject is this module's extension, and what it cannot know yet
// it reports as unknown ([ClusterShape]) instead of absent.
type DeviceTypeSubject interface {
	// EndpointNumber is the endpoint number.
	EndpointNumber() uint16
	// DeviceTypeIDs are the device types the endpoint lists (its
	// Descriptor's DeviceTypeList), the ones the model does not define
	// included (DeviceTypeFacts.ts:33-36).
	DeviceTypeIDs() []uint32
	// ServerClusterIDs are the endpoint's server clusters (its ServerList).
	ServerClusterIDs() []uint32
	// ClientClusterIDs are the endpoint's client clusters (its ClientList).
	ClientClusterIDs() []uint32
	// ServerClusterShape is what the endpoint's server cluster reports of
	// itself; ok false while its shape is not known, and then nothing
	// nested in a requirement of the cluster is judged.
	ServerClusterShape(cluster uint32) (shape ClusterShape, ok bool)
	// StatedConditions are the condition names stated for the endpoint, as
	// spelled (DeviceTypeFacts.ts:54-57).
	StatedConditions() []string
	// PartNumbers are the endpoint's direct children (DeviceTypeFacts.ts:
	// 22-25); a number the tree does not hold is ignored.
	PartNumbers() []uint16
}

// ClusterShape is what one server cluster reports of itself: its
// FeatureMap and the ids of its AttributeList, AcceptedCommandList and
// event list — matter.js's DeviceTypeFacts.Elements with the features
// (DeviceTypeFacts.ts:38-52, :71-80). A list the subject cannot know yet
// (a remote cluster whose list has not arrived) is not Known, and the
// requirements of its kind are not judged; an unknown FeatureMap leaves
// every requirement nested in the cluster unjudged, since the features
// decide what the others require.
type ClusterShape struct {
	FeatureMap    uint32
	FeaturesKnown bool

	Attributes      []uint32
	AttributesKnown bool

	AcceptedCommands []uint32
	CommandsKnown    bool

	Events      []uint32
	EventsKnown bool

	// UnimplementedCommands are the request commands the cluster's
	// conformance requires that its server leaves unimplemented
	// (spec.Server.UnimplementedMandatoryCommands, matter.js
	// Behavior.unimplemented): each is a [ViolationUnimplemented].
	UnimplementedCommands []uint32
}

// DeviceTypeTree is a tree of endpoints to judge: every endpoint of a node
// (or of several), its node endpoints at the roots, ordered as the
// violations are to come back.
type DeviceTypeTree struct {
	// Endpoints are the tree's endpoints; a nil entry is skipped.
	Endpoints []DeviceTypeSubject
	// NodeConditions answers the conditions the configuration of the node
	// whose node endpoint is nodeEndpoint makes true, beyond those derived
	// from the tree (DeviceTypeFacts.ts:59-63); nil answers none.
	NodeConditions func(nodeEndpoint uint16) []string
}

// Validate judges every endpoint of the tree against the device types it
// lists and the Base device type — [ValidateDeviceTypes] for any subjects.
// The violations come back endpoint by endpoint in tree order.
func (tree DeviceTypeTree) Validate() []DeviceTypeViolation {
	facts := newSubjectFacts(tree)
	pass := newDTPass(facts, deviceTypeLookups())
	var out []DeviceTypeViolation
	for _, s := range tree.Endpoints {
		if s != nil {
			out = append(out, pass.check(s.EndpointNumber())...)
		}
	}
	return out
}

// unimplementedOf is a [ViolationUnimplemented] for every mandatory
// command a server cluster of s leaves unimplemented, cluster by cluster
// in ServerList order — not a device-type violation, so
// [ValidateDeviceTypes] leaves it out and [DeviceTypeValidator] adds it:
// matter.js's #validateRequiredCommands
// (packages/node/src/behavior/cluster/ValidatedElements.ts:357-372 at
// 862ad75f), which warns "Throws unimplemented exception" and, under
// strict validation, refuses the behavior (ClusterImplementationError,
// :26-37; strict set at ServerBehaviorBacking.ts:100-102).
func unimplementedOf(s DeviceTypeSubject) []DeviceTypeViolation {
	var out []DeviceTypeViolation
	for _, cluster := range s.ServerClusterIDs() {
		shape, ok := s.ServerClusterShape(cluster)
		if !ok || len(shape.UnimplementedCommands) == 0 {
			continue
		}
		clusterName, cmdName := fmt.Sprintf("0x%04X", cluster), func(id uint32) string { return fmt.Sprintf("0x%02X", id) }
		if def := spec.Lookup(cluster); def != nil {
			clusterName = def.Name
			cmdName = func(id uint32) string {
				if c := def.Command(id, spec.Request); c != nil {
					return c.Name
				}
				return fmt.Sprintf("0x%02X", id)
			}
		}
		for _, id := range shape.UnimplementedCommands {
			out = append(out, DeviceTypeViolation{
				Endpoint: s.EndpointNumber(), DeviceType: clusterName,
				Requirement: clusterName + "." + cmdName(id), Kind: ViolationUnimplemented,
				Detail:  "Throws unimplemented exception",
				Cluster: cluster, HasCluster: true,
				Element: schema.RequirementCommand, ElementName: cmdName(id), ElementID: id,
			})
		}
	}
	return out
}

// Check judges one endpoint of the tree, in the context of the whole tree
// (its node scope decides its conditions); nil when the tree has no such
// endpoint.
func (tree DeviceTypeTree) Check(ep uint16) []DeviceTypeViolation {
	facts := newSubjectFacts(tree)
	if _, ok := facts.byID[ep]; !ok {
		return nil
	}
	return newDTPass(facts, deviceTypeLookups()).check(ep)
}

// subjectFacts answers dtFacts over a [DeviceTypeTree]: the one adapter
// between the validation pass and its inputs.
type subjectFacts struct {
	byID     map[uint16]DeviceTypeSubject
	parent   map[uint16]uint16
	children map[uint16][]uint16
	node     func(uint16) []string
	shapes   map[uint16]map[uint32]shapeAnswer
}

// shapeAnswer is a cluster's shape as the subject answered it once.
type shapeAnswer struct {
	shape ClusterShape
	ok    bool
}

func newSubjectFacts(tree DeviceTypeTree) *subjectFacts {
	f := &subjectFacts{
		byID: map[uint16]DeviceTypeSubject{}, parent: map[uint16]uint16{}, children: map[uint16][]uint16{},
		node: tree.NodeConditions, shapes: map[uint16]map[uint32]shapeAnswer{},
	}
	for _, s := range tree.Endpoints {
		if s != nil {
			if _, dup := f.byID[s.EndpointNumber()]; !dup {
				f.byID[s.EndpointNumber()] = s
			}
		}
	}
	for _, s := range tree.Endpoints {
		if s == nil || f.byID[s.EndpointNumber()] != s {
			continue
		}
		ep := s.EndpointNumber()
		for _, part := range s.PartNumbers() {
			// A part the tree does not hold, the endpoint itself, and a part
			// another endpoint claimed first own nothing here.
			if _, ok := f.byID[part]; !ok || part == ep {
				continue
			}
			if _, claimed := f.parent[part]; claimed || slices.Contains(f.children[ep], part) {
				continue
			}
			f.parent[part] = ep
			f.children[ep] = append(f.children[ep], part)
		}
	}
	return f
}

func (f *subjectFacts) parentOf(ep uint16) (uint16, bool) {
	p, ok := f.parent[ep]
	return p, ok
}

func (f *subjectFacts) partsOf(ep uint16) []uint16 { return f.children[ep] }

func (f *subjectFacts) deviceTypeIDs(ep uint16) []uint32 {
	if s := f.byID[ep]; s != nil {
		return s.DeviceTypeIDs()
	}
	return nil
}

func (f *subjectFacts) serverClusters(ep uint16) []uint32 {
	if s := f.byID[ep]; s != nil {
		return s.ServerClusterIDs()
	}
	return nil
}

func (f *subjectFacts) clientClusters(ep uint16) []uint32 {
	if s := f.byID[ep]; s != nil {
		return s.ClientClusterIDs()
	}
	return nil
}

// shape is the server cluster's shape, read once per pass; ok false for a
// cluster the endpoint does not serve or whose shape is unknown.
func (f *subjectFacts) shape(ep uint16, cluster uint32) (ClusterShape, bool) {
	s := f.byID[ep]
	if s == nil || !slices.Contains(s.ServerClusterIDs(), cluster) {
		return ClusterShape{}, false
	}
	byCluster := f.shapes[ep]
	if byCluster == nil {
		byCluster = map[uint32]shapeAnswer{}
		f.shapes[ep] = byCluster
	}
	a, done := byCluster[cluster]
	if !done {
		a.shape, a.ok = s.ServerClusterShape(cluster)
		byCluster[cluster] = a
	}
	return a.shape, a.ok
}

// features decodes the server's FeatureMap.
func (f *subjectFacts) features(ep uint16, cluster uint32) []string {
	sh, ok := f.shape(ep, cluster)
	if !ok || !sh.FeaturesKnown {
		return nil
	}
	return schema.ClusterFeatureNames(cluster, sh.FeatureMap)
}

// supports reads the server's AttributeList, AcceptedCommandList or event
// list.
func (f *subjectFacts) supports(ep uint16, cluster uint32, element schema.RequirementElement, id uint32) bool {
	sh, ok := f.shape(ep, cluster)
	if !ok {
		return false
	}
	switch element {
	case schema.RequirementAttribute:
		return slices.Contains(sh.Attributes, id)
	case schema.RequirementCommand:
		return slices.Contains(sh.AcceptedCommands, id)
	case schema.RequirementEvent:
		return slices.Contains(sh.Events, id)
	default:
		return false
	}
}

// judgeable reports whether a requirement of element nested in the
// endpoint's server cluster can be judged: the cluster's shape and its
// FeatureMap are known, and so is the list the element is read from.
func (f *subjectFacts) judgeable(ep uint16, cluster uint32, element schema.RequirementElement) bool {
	sh, ok := f.shape(ep, cluster)
	if !ok || !sh.FeaturesKnown {
		return false
	}
	switch element {
	case schema.RequirementAttribute:
		return sh.AttributesKnown
	case schema.RequirementCommand:
		return sh.CommandsKnown
	case schema.RequirementEvent:
		return sh.EventsKnown
	default:
		return true
	}
}

func (f *subjectFacts) statedConditions(ep uint16) []string {
	if s := f.byID[ep]; s != nil {
		return s.StatedConditions()
	}
	return nil
}

func (f *subjectFacts) nodeConditions(nodeEp uint16) []string {
	if f.node == nil {
		return nil
	}
	return f.node(nodeEp)
}

func (f *subjectFacts) describe(ep uint16) string { return fmt.Sprintf("endpoint %d", ep) }

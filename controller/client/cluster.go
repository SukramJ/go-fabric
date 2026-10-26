// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package client

import (
	"errors"
	"slices"
	"strconv"
	"sync"

	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/schema"
	"github.com/SukramJ/go-fabric/tlv"
)

// Errors of [Cluster.Decoded].
var (
	// ErrUntyped: the value has no type to decode it by — an attribute of a
	// named or unknown cluster, or one the definition does not know. The
	// node comes with it.
	ErrUntyped = spec.ErrUntyped
	// ErrNoValue: the image holds no value of the attribute.
	ErrNoValue = errors.New("client: no value")
)

// Cluster is one cluster of an endpoint of the image: its values, its
// DataVersion and its shape — the analysis matter.js's PeerBehavior makes
// of a discovered cluster (node/client/PeerBehavior.ts:403-467 at 0ceb202d)
// from the cluster's global attributes (ClientStructure.ts:754-837). Its
// methods are safe for concurrent use.
type Cluster struct {
	// mu is the owning Node's lock (Node.mu), shared by the structure and
	// every Endpoint and Cluster of the image; it guards this cluster's
	// fields.
	mu    *sync.RWMutex
	id    uint32
	name  string
	def   *spec.Cluster
	cache *cache

	// in are the shape's inputs as the structure last read them
	// (ClientStructure.ts:762-808).
	in shapeInput
	// current is the shape the structure built and keeps (the cluster's
	// behavior), nil until there is one or after a definition change reset
	// it (:632-659); pending is one built for an installed endpoint, which
	// the next structural change applies (pendingBehavior); injected is the
	// shape the endpoint exposes (endpoint.behaviors.supported).
	current, pending, injected *shape
	// pendingDelete: the peer's ServerList dropped the cluster (:922-934).
	pendingDelete bool
}

// shapeInput are a cluster's shape inputs: the fields matter.js keeps on a
// ClusterStructure (ClientStructure.ts:1613-1629).
type shapeInput struct {
	revision    uint64
	hasRevision bool
	featureMap  uint32
	hasFeatures bool
	// attributes is the sorted AttributeList, or the ids received; nil
	// while there is neither.
	attributes            []uint32
	attributeListReported bool
	commands, generated   []uint32
}

// shape is a cluster's analysed shape — DiscoveredShapeAnalysis
// (PeerBehavior.ts:403-467).
type shape struct {
	revision              uint16
	featureMap            uint32
	features              []string
	attributes            []uint32
	attributeListReported bool
	accepted, generated   []uint32
	extraAttrs, extraCmds []uint32
}

// newCluster is the structure of cluster id over its cache — #clusterFor
// (ClientStructure.ts:1039-1057) — named as D3 of the brief: the
// definition's name, schema's, or "Cluster$<hex>" (PeerBehavior.ts:399-401,
// :426-427).
func newCluster(mu *sync.RWMutex, id uint32, c *cache) *Cluster {
	cl := &Cluster{mu: mu, id: id, cache: c, def: spec.Lookup(id)}
	switch name, ok := schema.ClusterName(id); {
	case cl.def != nil:
		cl.name = cl.def.Name
	case ok:
		cl.name = name
	default:
		cl.name = unknownName("Cluster", id)
	}
	return cl
}

// unknownName is createUnknownName (PeerBehavior.ts:399-401).
func unknownName(prefix string, id uint32) string {
	return prefix + "$" + strconv.FormatUint(uint64(id), 16)
}

// readInput updates the shape's inputs from the cache's global attributes —
// #synchronizeCluster's reading (ClientStructure.ts:762-808): the revision
// only when a number (:771-773), the FeatureMap when a number (:775-777;
// the TypeScript value is the decoded bitmap), a non-empty AttributeList
// authoritative and else the ids received (:779-795), the command lists
// when lists (:797-807). The caller holds the lock.
func (c *Cluster) readInput() {
	values := c.cache.values
	if v, ok := values[spec.AttrClusterRevision]; ok && isUint(v) {
		c.in.revision, c.in.hasRevision = v.El.Uint, true
	}
	if v, ok := values[spec.AttrFeatureMap]; ok && isUint(v) && v.El.Uint <= 0xFFFF_FFFF {
		c.in.featureMap, c.in.hasFeatures = uint32(v.El.Uint), true
	}
	reported := idList(values[spec.AttrAttributeList])
	c.in.attributeListReported = len(reported) > 0
	if c.in.attributeListReported {
		c.in.attributes = reported
	} else if len(values) > 0 {
		received := make([]uint32, 0, len(values))
		for id := range values {
			received = append(received, id)
		}
		slices.Sort(received)
		c.in.attributes = received
	}
	if v, ok := values[spec.AttrAcceptedCommandList]; ok && isList(v) {
		c.in.commands = idList(v)
	}
	if v, ok := values[spec.AttrGeneratedCommandList]; ok && isList(v) {
		c.in.generated = idList(v)
	}
}

// buildable: something useful exists — any attributes, commands or
// generated commands (ClientStructure.ts:810-817).
func (c *Cluster) buildable() bool {
	return len(c.in.attributes) > 0 || len(c.in.commands) > 0 || len(c.in.generated) > 0
}

// analyse is DiscoveredShapeAnalysis (PeerBehavior.ts:422-467): the
// revision (a reported one below 1 is none, :429-434), the features the
// FeatureMap names, and the ids the definition does not know (:443-455).
// The caller holds the lock.
func (c *Cluster) analyse() *shape {
	s := &shape{
		featureMap:            c.in.featureMap,
		attributes:            slices.Clone(c.in.attributes),
		attributeListReported: c.in.attributeListReported,
		accepted:              slices.Clone(c.in.commands),
		generated:             slices.Clone(c.in.generated),
	}
	if c.in.hasRevision && c.in.revision >= 1 && c.in.revision <= 0xFFFF {
		s.revision = uint16(c.in.revision)
	}
	// The features are named by the schema, which knows every snapshot
	// cluster, generated definition or not (matter.clusters(shape.id) at
	// PeerBehavior.ts:425-426 resolves a named cluster's model).
	s.features = schema.ClusterFeatureNames(c.id, c.in.featureMap)
	for _, id := range s.attributes {
		if isGlobal(id) || (c.def != nil && c.def.Attribute(id) != nil) {
			continue
		}
		s.extraAttrs = append(s.extraAttrs, id)
	}
	for _, id := range s.accepted {
		if c.def != nil && (c.def.Command(id, spec.Request) != nil || c.def.Command(id, spec.Response) != nil) {
			continue
		}
		s.extraCmds = append(s.extraCmds, id)
	}
	return s
}

// isGlobal is AttributeModel.isGlobal / AttributeId.isGlobal: the global
// attribute ids (Matter Core §7.13).
func isGlobal(id uint32) bool {
	return id >= spec.AttrGeneratedCommandList && id <= spec.AttrClusterRevision
}

func isUint(n spec.Node) bool {
	return n.El.Type >= tlv.TypeUnsignedInt1 && n.El.Type <= tlv.TypeUnsignedInt8
}

func isList(n spec.Node) bool { return n.El.Type == tlv.TypeArray || n.El.Type == tlv.TypeList }

// idList is the sorted numeric entries of a list node — the
// filter(typeof === "number") and sort of ClientStructure.ts:782-806; nil
// for a node that is not a list.
func idList(n spec.Node) []uint32 {
	if !isList(n) {
		return nil
	}
	out := make([]uint32, 0, len(n.Children))
	for _, e := range n.Children {
		if isUint(e) && e.El.Uint <= 0xFFFF_FFFF {
			out = append(out, uint32(e.El.Uint))
		}
	}
	slices.Sort(out)
	return out
}

// ID is the cluster id.
func (c *Cluster) ID() uint32 { return c.id }

// Name is the cluster's name: its definition's, the schema's for a named
// cluster, "Cluster$<hex>" for an unknown one.
func (c *Cluster) Name() string { return c.name }

// Definition is the cluster's generated definition, nil for a named or
// unknown cluster.
func (c *Cluster) Definition() *spec.Cluster { return c.def }

// exposed is the shape the endpoint exposes, nil when not ready.
func (c *Cluster) exposed() *shape {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.injected
}

// Ready reports whether the cluster has a shape: something useful arrived
// (ClientStructure.ts:810-817). A cluster that is not ready is not
// announced.
func (c *Cluster) Ready() bool { return c.exposed() != nil }

// Revision is the peer's ClusterRevision; when it reported none or one
// below 1 the model's — the snapshot's revision of the cluster, 1 for a
// cluster the snapshot does not know (PeerBehavior.ts:426-434 at
// 862ad75f: the revision override only for a valid, differing one;
// PeerBehaviorTest "tolerates a peer that does not report a revision").
// 0 while the cluster has no shape.
func (c *Cluster) Revision() uint16 {
	s := c.exposed()
	if s == nil {
		return 0
	}
	if s.revision != 0 {
		return s.revision
	}
	if rev, ok := schema.ClusterRevision(c.id); ok {
		return rev
	}
	return 1
}

// FeatureMap is the peer's FeatureMap.
func (c *Cluster) FeatureMap() uint32 {
	if s := c.exposed(); s != nil {
		return s.featureMap
	}
	return 0
}

// Features are the codes of the features the FeatureMap sets
// (schema.ClusterFeatureNames), nil for a cluster the snapshot does not
// know or whose FeatureMap sets none.
func (c *Cluster) Features() []string {
	if s := c.exposed(); s != nil {
		return slices.Clone(s.features)
	}
	return nil
}

// AttributeList is the peer's AttributeList, sorted — or, when
// AttributeListReported is false, the ids of the values received
// (ClientStructure.ts:779-795).
func (c *Cluster) AttributeList() []uint32 {
	if s := c.exposed(); s != nil {
		return slices.Clone(s.attributes)
	}
	return nil
}

// AttributeListReported reports whether AttributeList is the peer's list.
func (c *Cluster) AttributeListReported() bool {
	if s := c.exposed(); s != nil {
		return s.attributeListReported
	}
	return false
}

// AcceptedCommands is the peer's AcceptedCommandList, sorted.
func (c *Cluster) AcceptedCommands() []uint32 {
	if s := c.exposed(); s != nil {
		return slices.Clone(s.accepted)
	}
	return nil
}

// GeneratedCommands is the peer's GeneratedCommandList, sorted.
func (c *Cluster) GeneratedCommands() []uint32 {
	if s := c.exposed(); s != nil {
		return slices.Clone(s.generated)
	}
	return nil
}

// Supports reports whether the peer supports the attribute: a global
// attribute always (PeerBehavior.ts:488-492); any other when the peer
// lists it — the support override of maybeOverrideSupport (:478-507),
// which for a named or unknown cluster the lists alone decide (:484-486)
// — and, for a cluster of the model, its conformance under the peer's
// features and revision allows it: an element the peer implements
// although its conformance deprecates it counts as optional
// (supportOverrideOf, :381-397), a disallowed one or one its features
// exclude before a trailing "D" stays out (PeerBehaviorTest "omits a
// disallowed attribute the peer reports", "omits an attribute the peer
// reports whose conformance before a trailing deprecation excludes it").
func (c *Cluster) Supports(attr uint32) bool {
	if isGlobal(attr) {
		return true
	}
	s := c.exposed()
	if s == nil || !slices.Contains(s.attributes, attr) {
		return false
	}
	if c.def == nil {
		return true
	}
	a := c.def.Attribute(attr)
	if a == nil {
		return true
	}
	ctx := peerContext{FeatureContext: spec.Context(c.def, s.featureMap), revision: c.Revision()}
	return a.Conformance.Applicability(ctx) != spec.ApplicabilityNone ||
		a.Conformance.ApplicabilityDeprecatedOptional(ctx) != spec.ApplicabilityNone
}

// peerContext is a cluster definition's conformance context under the
// peer's features and the revision it reported (a "Rev >= vN" element is
// judged against it, PeerBehaviorTest "judges a revision-conformant
// element against the revision the peer sent").
type peerContext struct {
	spec.FeatureContext
	revision uint16
}

func (p peerContext) Revision() uint16 { return p.revision }

// SupportsCommand reports whether the peer accepts the command: it lists
// it in its AcceptedCommandList (PeerBehavior.ts:450-455,
// ClientBehaviorBacking.ts:37-43).
func (c *Cluster) SupportsCommand(cmd uint32) bool {
	s := c.exposed()
	return s != nil && slices.Contains(s.accepted, cmd)
}

// Extra returns the attribute and command ids the peer lists and the
// definition does not know — every id of a named or unknown cluster
// (PeerBehavior.ts:444-455), global attributes aside. matter.js names them
// "attr$<hex>" (:252-255).
func (c *Cluster) Extra() (attrs, cmds []uint32) {
	if s := c.exposed(); s != nil {
		return slices.Clone(s.extraAttrs), slices.Clone(s.extraCmds)
	}
	return nil, nil
}

// Version is the cluster's DataVersion; ok false is an unknown version
// (DatasourceCache.ts:47-48).
func (c *Cluster) Version() (version uint32, ok bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.cache.version, c.cache.hasVersion
}

// Value returns the attribute's value as the peer reported it; false for
// a nil Cluster — an [Endpoint.Cluster] the image does not hold — so a
// generated <cluster>def.StateFrom over it reports every attribute absent.
// It implements [spec.Values].
func (c *Cluster) Value(attr uint32) (spec.Node, bool) {
	if c == nil {
		return spec.Node{}, false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	v, ok := c.cache.values[attr]
	return v, ok
}

// Decoded returns the attribute's value decoded by its type
// ([spec.DecodeAttribute]): a global attribute by its own type, whatever
// the cluster (InputChunk.ts:105-110); a struct, or a list of structs,
// through the definition as the generated type; every other value at its
// widest Go type — an unsigned integer, enum or bitmap as uint64, a
// signed integer as int64, a list as []any, whatever the attribute's
// width (a uint8 attribute of 20 is uint64(20)). A value without a type —
// of a named or unknown cluster, or an attribute the definition does not
// know — is the node with [ErrUntyped]; TLV null is nil.
//
// It is the untyped form, for a cluster without a definition and for one
// attribute at a time. The typed form is the generated package's
// StateFrom over the cluster (onoffdef.StateFrom(ep.Cluster(onoffdef.ClusterID))):
// every attribute in its field's type, and the ones the image lacks.
func (c *Cluster) Decoded(attr uint32) (any, error) {
	n, ok := c.Value(attr)
	if !ok {
		return nil, ErrNoValue
	}
	return spec.DecodeAttribute(c.def, attr, n)
}

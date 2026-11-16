// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package endpoint

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"sync/atomic"

	"github.com/SukramJ/go-fabric/cluster"
	mattercore "github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/cluster/spec"
	descdef "github.com/SukramJ/go-fabric/cluster/spec/descriptor"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/schema"
	"github.com/SukramJ/go-fabric/store"
	"github.com/SukramJ/go-fabric/tlv"
)

// clusterDataVersion returns the current DataVersion for srv by
// type-asserting to [contract.ClusterDataVersion]. Falls back
// to 0 when the cluster does not implement the capability.
func clusterDataVersion(srv contract.ClusterServer) uint32 {
	if dv, ok := srv.(contract.ClusterDataVersion); ok {
		return dv.MatterDataVersion()
	}
	return 0
}

// ownDataVersion returns srv's own version tracker when it is the one the
// dispatcher reports for srv on ep: a root or Aggregator server is a
// persistent instance, so a tracker it keeps itself
// ([contract.ClusterDataVersion]) is authoritative. Bridged endpoints
// materialise their cluster servers fresh on every dispatch
// ([ClusterServers]) — an instance-embedded tracker would report a new
// random initial version on every read — so their servers' own trackers
// are never consulted.
func ownDataVersion(ep *Endpoint, srv contract.ClusterServer) (contract.ClusterDataVersion, bool) {
	if ep != nil && !ep.IsRoot() && !ep.IsAggregator() {
		return nil, false
	}
	dv, ok := srv.(contract.ClusterDataVersion)
	return dv, ok
}

// clusterDataVersionFor resolves the DataVersion for srv on ep: the
// server's own tracker where [ownDataVersion] names one, the tracker the
// persistent [Endpoint] keeps for the cluster otherwise — every cluster
// instance has a version, the root's and the Aggregator's included
// (review II, decision 46). Mirrors matter.js
// packages/node/src/behavior/state/managed/Datasource.ts:597 (every
// Datasource samples a version once per lifetime). ok is false only
// without an endpoint to keep one.
func clusterDataVersionFor(ep *Endpoint, srv contract.ClusterServer) (version uint32, ok bool) {
	if own, has := ownDataVersion(ep, srv); has {
		return own.MatterDataVersion(), true
	}
	if ep == nil {
		return 0, false
	}
	return ep.ClusterDataVersion(srv.MatterClusterID()), true
}

// TopologyDispatcher bridges the assembled [Topology] to the
// Interaction-Model [im.Dispatcher] surface. Each Read / Write /
// Invoke call resolves the addressed endpoint via the topology, then
// the cluster server via [ClusterServers], then the attribute /
// command on the server.
//
// Concurrency: TopologyDispatcher is safe for concurrent calls from
// multiple IM workers. The underlying Topology is treated as
// immutable for the dispatcher's lifetime — when the model changes,
// the bridge constructs a fresh Topology via [Assembler.Assemble]
// and swaps the dispatcher's reference.
type TopologyDispatcher struct {
	topology *Topology
	// acl and aux are the access-control sources [TopologyDispatcher.CheckACL]
	// reads, nil for none. A running bridge replaces them on the live
	// dispatcher (Bridge.AttachACLLister, Bridge.AttachAuxiliaryACL) while
	// requests are checked, hence the atomics; the boxes keep an
	// interface value behind one pointer.
	acl atomic.Pointer[ACLLister]
	aux atomic.Pointer[AuxiliaryACLLister]
	// timedWrite reports an attribute with the timed access quality; nil
	// is [schema.IsTimedWrite]. A test sets it: no attribute of the
	// snapshot at the pin carries the quality. A pointer keeps the
	// dispatcher comparable.
	timedWrite *timedWriteTable
}

// timedWriteTable is a stand-in for the schema's timed-write table.
type timedWriteTable struct {
	isTimed func(clusterID, attributeID uint32) bool
}

// isTimedWrite is [TopologyDispatcher.timedWrite] or the schema's table.
func (d *TopologyDispatcher) isTimedWrite(clusterID, attributeID uint32) bool {
	if d.timedWrite != nil {
		return d.timedWrite.isTimed(clusterID, attributeID)
	}
	return schema.IsTimedWrite(clusterID, attributeID)
}

// AuxiliaryACLLister supplies the auxiliary access control entries of the
// AccessControl cluster's Auxiliary (AUX) feature: entries a cluster
// synthesises rather than a controller writes — today the Groupcast
// cluster's Operate grants for the listener endpoints of a group with
// HasAuxiliaryAcl. *groups.Manager satisfies it.
type AuxiliaryACLLister interface {
	AuxiliaryACL(ctx context.Context, fabricIndex uint8) ([]store.ACLEntry, error)
}

// ACLLister is the subset of the Matter ACL store [TopologyDispatcher.CheckACL]
// consults. Production daemons wire the SQLite-backed `matter/store.Store`.
// Leaving it nil does NOT disable enforcement — [TopologyDispatcher.CheckACL]
// then denies every operational request; a setup that deliberately runs
// without stored entries wires [UnenforcedACL] instead.
type ACLLister interface {
	ListACL(ctx context.Context, fabricIndex uint8) ([]store.ACLEntry, error)
}

// UnenforcedACL is an [ACLLister] that grants every operational request on
// every fabric.
//
// It exists so that running without stored AccessControl entries is a
// decision written down in one greppable place. The alternative — treating a
// missing lister as "enforcement off" — made the whole gate depend on a
// single wiring line whose removal no test could observe: the capability kept
// passing its own tests, the pin that watched the wiring only looked for the
// method name in the source, and every ACL entry a controller had written was
// silently unenforced on operational reads, writes and invokes.
//
// Production never wires this. Tests and local development do, where the
// alternative is that no request at all is answered.
type UnenforcedACL struct{}

// ListACL returns one wildcard CASE entry at Administer privilege: no
// subject restriction, no target restriction, every fabric.
func (UnenforcedACL) ListACL(context.Context, uint8) ([]store.ACLEntry, error) {
	return []store.ACLEntry{{Privilege: store.PrivilegeAdminister, AuthMode: store.AuthModeCASE}}, nil
}

// NewTopologyDispatcher wraps t. Returns nil when t is nil so the
// caller can short-circuit before dispatching.
func NewTopologyDispatcher(t *Topology) *TopologyDispatcher {
	if t == nil {
		return nil
	}
	return &TopologyDispatcher{topology: t}
}

// SetACLLister wires the ACL source that [TopologyDispatcher.CheckACL]
// enforces against (see Bridge.AttachACLLister).
//
// Passing nil leaves the gate closed rather than open: every operational
// (CASE) request is denied until a source is wired. Pass [UnenforcedACL] to
// run without stored entries on purpose. Safe while requests are being
// checked: the next CheckACL reads l.
func (d *TopologyDispatcher) SetACLLister(l ACLLister) {
	if d == nil {
		return
	}
	if l == nil {
		d.acl.Store(nil)
		return
	}
	d.acl.Store(&l)
}

// aclLister is the ACL source, nil when none is wired.
func (d *TopologyDispatcher) aclLister() ACLLister {
	if d == nil {
		return nil
	}
	if p := d.acl.Load(); p != nil {
		return *p
	}
	return nil
}

// auxLister is the auxiliary ACL source, nil when none is wired.
func (d *TopologyDispatcher) auxLister() AuxiliaryACLLister {
	if d == nil {
		return nil
	}
	if p := d.aux.Load(); p != nil {
		return *p
	}
	return nil
}

// SetAuxiliaryACL wires the auxiliary access control entries
// [TopologyDispatcher.CheckACL] evaluates next to the stored ones (see
// Bridge.AttachAuxiliaryACL), and with them the Auxiliary feature's rule
// that a Group entry without targets no longer reaches endpoint 0. nil
// turns the feature off. Mirrors matter.js AccessControlServer
// #applyFabricAcl, which installs `[...realAcl, ...#auxiliaryAclFor(fabric)]`
// and sets FabricAccessControl.auxiliaryFeatureEnabled. Safe while
// requests are being checked, as [TopologyDispatcher.SetACLLister].
func (d *TopologyDispatcher) SetAuxiliaryACL(l AuxiliaryACLLister) {
	if d == nil {
		return
	}
	if l == nil {
		d.aux.Store(nil)
		return
	}
	d.aux.Store(&l)
}

// Compile-time assertion: TopologyDispatcher satisfies im.Dispatcher
// and the optional im.DataVersionReader + im.AttributeReadPrivilegeProvider
// + im.ACLChecker + im.AuthorizingWriter interfaces.
var (
	_ im.Dispatcher                     = (*TopologyDispatcher)(nil)
	_ im.DataVersionReader              = (*TopologyDispatcher)(nil)
	_ im.AttributeReadPrivilegeProvider = (*TopologyDispatcher)(nil)
	_ im.ACLChecker                     = (*TopologyDispatcher)(nil)
	_ im.EndpointExpander               = (*TopologyDispatcher)(nil)
	_ im.AuthorizingWriter              = (*TopologyDispatcher)(nil)
	_ im.AuthorizingInvoker             = (*TopologyDispatcher)(nil)
)

// Read implements [im.Dispatcher]. Wildcards expand as follows:
//
//   - HasEndpoint=false  → iterate every endpoint in the topology.
//   - HasCluster=false   → iterate every cluster server on the endpoint.
//   - HasAttribute=false → the global attributes GeneratedCommandList,
//     AcceptedCommandList, AttributeList, FeatureMap and
//     ClusterRevision (EventList left out, see attributesFor) PLUS
//     every attribute the cluster server advertises via the
//     [contract.ClusterAttributeLister] optional interface. A cluster
//     server that does not implement that interface contributes its
//     reportable attributes ([contract.ClusterServer.MatterReportable])
//     instead.
//
// Concrete (non-wildcard) reads return exactly one ReadResult.
//
// A status answers a fully concrete request path only (review II,
// R2-P4-imread-02, -03): a path naming no endpoint returns what exists
// and nothing else — an endpoint lacking the cluster or the attribute is
// skipped, an empty node answers nothing. A path naming an endpoint keeps
// its statuses here (UnsupportedEndpoint, UnsupportedCluster, an
// attribute whose read fails): node.Node.ReadCluster reports
// them to the host. On the wire [im.AttributeExpansion] hands out a
// status for a fully concrete path alone, as matter.js
// packages/protocol/src/action/server/AttributeReadResponse.ts at
// db021da0 does (addWildcard :88-94 answers no status).
//
// ctx carries the FabricFiltered flag + FabricIndex via
// [im.WithFabricFilter] / [im.FabricFilterFromContext]. Cluster servers
// that implement [contract.FabricScopedReader] receive MatterReadFiltered
// instead of MatterRead so they can project fabric-sensitive list
// attributes (OperationalCredentials.Fabrics, AccessControl.ACL) down
// to the requesting fabric. Mirrors matter.js InteractionServer.ts
// #executeReadInteraction → #prepareOnlineContext(…, isFabricFiltered)
// (:306-326 at 862ad75f).
func (d *TopologyDispatcher) Read(ctx context.Context, path im.ConcreteAttributePath) []im.ReadResult {
	endpoints := d.resolveEndpoints(path)
	wildcardEndpoint := !path.HasEndpoint
	if len(endpoints) == 0 {
		if wildcardEndpoint {
			return nil
		}
		return []im.ReadResult{{Path: path, Status: im.StatusUnsupportedEndpoint}}
	}
	var results []im.ReadResult
	for _, ep := range endpoints {
		results = d.readAt(ctx, ep, path, wildcardEndpoint, results)
	}
	return results
}

// AttributePathStatus implements [im.AttributePathChecker]: the status
// [TopologyDispatcher.Read] answers path, a fully concrete attribute path,
// with, decided from the topology and the server's attribute set — an
// endpoint the topology lacks is UnsupportedEndpoint, a cluster the
// endpoint does not serve UnsupportedCluster — and Success for an
// attribute the server lists ([contract.ClusterAttributeLister]) or a
// global list the dispatcher synthesises, whose value is read at its
// place. Any other attribute is read here, and its status is the read's:
// the server's attribute set does not decide it.
func (d *TopologyDispatcher) AttributePathStatus(ctx context.Context, path im.ConcreteAttributePath) im.StatusCode {
	if !path.HasEndpoint || !path.HasCluster || !path.HasAttribute {
		return im.StatusSuccess
	}
	ep := d.topology.FindByID(path.Endpoint)
	if ep == nil {
		return im.StatusUnsupportedEndpoint
	}
	servers := d.serversFor(ep, path)
	if len(servers) == 0 {
		return im.StatusUnsupportedCluster
	}
	srv := servers[0]
	switch path.Attribute {
	case cluster.AttrGlobalAttributeList, cluster.AttrGlobalAcceptedCommandList, cluster.AttrGlobalGeneratedCommandList:
		return im.StatusSuccess
	}
	// The existence statuses come before the access check (matter.js
	// AttributeReadResponse.ts:255-270 before :272); an attribute the
	// server does not list is known only by reading it, which a refused
	// request does not get to do.
	listed := false
	if lister, ok := srv.(contract.ClusterAttributeLister); ok {
		listed = slices.Contains(lister.MatterAttributes(), path.Attribute)
	}
	if !readAllowed(ctx, srv, path.Attribute) {
		return im.StatusUnsupportedAccess
	}
	if listed {
		return im.StatusSuccess
	}
	return readOne(ctx, ep, srv, path).Status
}

// readAllowed asks a [contract.ReadAccessDeclarer] whether the request
// behind ctx may read attrID, before the server's read runs (decision
// 152, R2-P6-sec-1103's read half). A read without a request behind it
// — a local caller ([im.LookupFabricFilter]) — and a server that
// declares nothing are allowed. The refusal is answered as the access
// check's: UNSUPPORTED_ACCESS on a fully concrete path, the attribute
// skipped under a wildcard — matter.js AttributeReadResponse.ts:272 with
// :473-475 (concrete path: UNSUPPORTED_ACCESS) and :406-413 (wildcard:
// the attribute is skipped), and chip src/app/reporting/Engine.cpp:81-88.
func readAllowed(ctx context.Context, srv contract.ClusterServer, attrID uint32) bool {
	decl, ok := srv.(contract.ReadAccessDeclarer)
	if !ok {
		return true
	}
	if _, _, request := im.LookupFabricFilter(ctx); !request {
		return true
	}
	return decl.MatterReadAllowed(ctx, attrID)
}

// ExpansionEndpoints implements [im.EndpointExpander]: the ids of the
// endpoints [TopologyDispatcher.Read] visits for path, in its order.
func (d *TopologyDispatcher) ExpansionEndpoints(path im.ConcreteAttributePath) []uint16 {
	endpoints := d.resolveEndpoints(path)
	ids := make([]uint16, len(endpoints))
	for i, ep := range endpoints {
		ids[i] = ep.ID
	}
	return ids
}

// ReadAtEndpoint implements [im.EndpointExpander]: what
// [TopologyDispatcher.Read] returns for path, a path naming no endpoint,
// at endpointID alone — nothing when that endpoint is not in the
// topology.
func (d *TopologyDispatcher) ReadAtEndpoint(ctx context.Context, path im.ConcreteAttributePath, endpointID uint16) []im.ReadResult {
	ep := d.topology.FindByID(endpointID)
	if ep == nil {
		return nil
	}
	return d.readAt(ctx, ep, path, true, nil)
}

// readAt appends to results what path reads at ep: one result per
// attribute of each matching cluster server. A concrete endpoint lacking
// the cluster answers UnsupportedCluster; under a wildcard endpoint it is
// skipped, and so is an attribute whose read does not succeed
// (AttributeReadResponse.ts:259-261, :341-344, :386-387, :407-414 and
// :419-424 at db021da0).
func (d *TopologyDispatcher) readAt(ctx context.Context, ep *Endpoint, path im.ConcreteAttributePath, wildcardEndpoint bool, results []im.ReadResult) []im.ReadResult {
	ePath := path
	ePath.Endpoint = ep.ID
	ePath.HasEndpoint = true

	servers := d.serversFor(ep, path)
	if len(servers) == 0 {
		// Wildcard endpoint expansion (Matter §4.5.4 "Path
		// Generation"): paths that match no concrete cluster on
		// this endpoint are SILENTLY SKIPPED, not surfaced as
		// UnsupportedCluster — the spec says the generator only
		// emits paths where the cluster/attribute actually
		// exists. Concrete endpoints (operator named the EP
		// explicitly) keep the error surface so callers see why
		// their read returned nothing.
		if wildcardEndpoint {
			return results
		}
		return append(results, im.ReadResult{Path: ePath, Status: im.StatusUnsupportedCluster})
	}
	concrete := path.HasEndpoint && path.HasCluster && path.HasAttribute
	for _, srv := range servers {
		cPath := ePath
		cPath.Cluster = srv.MatterClusterID()
		cPath.HasCluster = true

		attrs := d.attributesFor(srv, path)
		for _, attrID := range attrs {
			aPath := cPath
			aPath.Attribute = attrID
			aPath.HasAttribute = true
			if !readAllowed(ctx, srv, attrID) {
				if concrete {
					results = append(results, im.ReadResult{Path: aPath, Status: im.StatusUnsupportedAccess})
				}
				continue
			}
			res := readOne(ctx, ep, srv, aPath)
			if wildcardEndpoint && res.Status != im.StatusSuccess {
				continue
			}
			results = append(results, res)
		}
	}
	return results
}

// Write implements [im.Dispatcher]. Wildcards expand the same way as
// Read, but every match is dispatched separately and one
// [im.WriteResult] is returned per match. Unauthorized callers must go
// through [TopologyDispatcher.WriteAuthorized] — Write itself applies no
// access control.
func (d *TopologyDispatcher) Write(ctx context.Context, path im.ConcreteAttributePath, value im.AttributeValue) []im.WriteResult {
	return d.WriteAuthorized(ctx, path, value, nil)
}

// WritePersister stores a controller's write of an attribute whose value
// outlives a restart — a nonvolatile attribute the module holds — before
// the cluster server takes it, so a store that refuses the value refuses
// the write: the value is never in effect without being stored, the rule
// BasicInformation's NodeLabel follows (mattercore.BasicInformation
// SetOnPersistentWrite).
type WritePersister interface {
	// PersistWrite runs for a write every gate of the dispatcher passed,
	// before srv sees it. A nil done means the write is not stored — an
	// attribute it does not keep, or a server no longer in its key's
	// place — and the dispatcher applies the write without settlement.
	// Otherwise it has stored value and returns done, which the
	// dispatcher calls with whether srv applied the write: false puts the
	// stored value back as it stood. An error refuses the write with
	// FAILURE; srv never sees it. Between PersistWrite and done no other
	// write of the same attribute is stored.
	PersistWrite(ctx context.Context, ep *Endpoint, srv contract.ClusterServer, attrID uint32, value im.AttributeValue) (done func(applied bool), err error)
}

// WriteAuthorized implements [im.AuthorizingWriter]. It is
// [TopologyDispatcher.Write] with an access-control gate evaluated at
// every RESOLVED (endpoint, cluster, attribute) — the only place a
// wildcard-endpoint write can be authorized, since the requested path
// names no endpoint. authorize may be nil, which dispatches without a
// gate.
func (d *TopologyDispatcher) WriteAuthorized(ctx context.Context, path im.ConcreteAttributePath, value im.AttributeValue, authorize im.WriteAuthorizer) []im.WriteResult { //nolint:gocyclo,gocognit,funlen // the write path's ordered checks (matter.js AttributeWriteResponse), one branch per status
	endpoints := d.resolveEndpoints(path)
	if len(endpoints) == 0 {
		return []im.WriteResult{{Path: path, Status: im.StatusUnsupportedEndpoint}}
	}
	wildcardEndpoint := !path.HasEndpoint
	// Whether the write runs inside a valid timed interaction; known is
	// false for a caller that stamped nothing (im.WithTimedInteraction),
	// whose per-attribute timed check is its own.
	timed, timedKnown := im.TimedInteractionFromContext(ctx)
	var results []im.WriteResult
	for _, ep := range endpoints {
		ePath := path
		ePath.Endpoint = ep.ID
		ePath.HasEndpoint = true

		servers := d.serversFor(ep, path)
		if len(servers) == 0 {
			// Wildcard endpoint expansion: skip endpoints that don't
			// host the requested cluster (Matter §4.5.4). Concrete
			// endpoint addressing keeps the explicit error surface.
			if wildcardEndpoint {
				continue
			}
			results = append(results, im.WriteResult{Path: ePath, Status: im.StatusUnsupportedCluster})
			continue
		}
		for _, srv := range servers {
			cPath := ePath
			cPath.Cluster = srv.MatterClusterID()
			cPath.HasCluster = true

			attrs := d.attributesFor(srv, path)
			for _, attrID := range attrs {
				aPath := cPath
				aPath.Attribute = attrID
				aPath.HasAttribute = true

				// Read-only-attribute write gate (Matter §8.6). matter.js
				// rejects a WriteRequest to a non-writable attribute BEFORE
				// any behavior runs, so the write never reaches the cluster
				// server (and, downstream, the host). The writability verdict
				// is schema.AttributeWritable's, generated from the effective
				// access of every attribute of every snapshot cluster. A
				// concrete path is answered with UNSUPPORTED_WRITE (mirrors
				// packages/protocol/src/action/server/AttributeWriteResponse.ts:231-234
				// at db021da0, `if (!limits.writable) … return
				// this.#asStatus(path, Status.UnsupportedWrite)`; chip
				// src/app/WriteHandler.cpp:790-791 at 6170af84); a wildcard
				// path silently skips the attribute (mirrors
				// AttributeWriteResponse.ts:331-333 at db021da0,
				// `if (!attribute.limits.writable) return;`). An attribute
				// without a read-only record — a writable one, one of a
				// cluster the snapshot does not have (known == false) — falls
				// through unchanged.
				// An attribute the cluster does not implement answers
				// UNSUPPORTED_ATTRIBUTE before its writability is
				// considered — matter.js AttributeWriteResponse.ts:227-229
				// (`if (!attribute) … Status.UnsupportedAttribute`), ahead of
				// the UnsupportedWrite check at :231-234. Found by the CHIP
				// Python harness (TC-IDM-3.2 step 3 writes a spec attribute
				// the DUT does not list).
				// The global attributes (GeneratedCommandList … ClusterRevision)
				// exist on every cluster and are never writable: UNSUPPORTED_WRITE
				// on a concrete path, skipped on a wildcard one — matter.js
				// models them read-only like any other attribute
				// (AttributeWriteResponse.ts:231-234). They used to reach the
				// cluster server, which answered Failure or
				// UNSUPPORTED_ATTRIBUTE (TC-ACE-2.2).
				if isGlobalAttribute(attrID) {
					if !wildcardEndpoint && path.HasAttribute {
						results = append(results, im.WriteResult{Path: aPath, Status: im.StatusUnsupportedWrite})
					}
					continue
				}
				if path.HasAttribute && !attributeListed(srv, attrID) {
					if !wildcardEndpoint {
						results = append(results, im.WriteResult{Path: aPath, Status: im.StatusUnsupportedAttribute})
					}
					continue
				}
				if writable, known := schema.AttributeWritable(cPath.Cluster, attrID); known && !writable {
					// "Concrete" means endpoint AND cluster AND attribute are
					// all named — the split matter.js makes before it picks a
					// write path (AttributeWriteResponse.ts:68). A wildcard
					// endpoint is a wildcard path even with a concrete
					// attribute, and reporting a status per resolved endpoint
					// would both diverge from the spec and enumerate which
					// endpoints host the cluster to a peer whose authorization
					// the gate below has not evaluated yet.
					if !wildcardEndpoint && path.HasAttribute {
						results = append(results, im.WriteResult{Path: aPath, Status: im.StatusUnsupportedWrite})
					}
					continue
				}

				// An attribute with the timed access quality outside a
				// timed interaction (the check below the access gate).
				timedRefused := timedKnown && !timed && d.isTimedWrite(aPath.Cluster, aPath.Attribute)
				// A path with a numeric ListIndex meets the timed check
				// before authorize: im's authorizer for it ends the
				// interaction where the value would be written
				// (im.listIndexGate), which matter.js does in writeValue,
				// after the timed check
				// (packages/protocol/src/action/server/AttributeWriteResponse.ts:243-247,
				// :343-345 before :419-424 at db021da0). The order against
				// the access check is kept: on a concrete path im decided
				// access before dispatch and the authorizer is the gate
				// alone, on a wildcard path both refusals skip the
				// attribute without a status.
				if timedRefused && path.HasListIndex && !path.ListAppend {
					if !wildcardEndpoint && path.HasAttribute {
						results = append(results, im.WriteResult{Path: aPath, Status: im.StatusNeedsTimedInteraction})
					}
					continue
				}

				// Access-control gate at the RESOLVED location, evaluated
				// after the writability verdict and before the value is
				// applied — the order matter.js uses in
				// ../matter.js/packages/protocol/src/action/server/AttributeWriteResponse.ts:324-343
				// (`if (!attribute.limits.writable) return;` then
				// `session.authorityAt(attribute.limits.writeLevel, location)`).
				// A denied wildcard-endpoint location is skipped silently so
				// the response discloses only authorized endpoints; a denied
				// concrete path keeps the explicit status.
				if authorize != nil {
					if status := authorize(aPath.Endpoint, aPath.Cluster, aPath.Attribute); !status.IsSuccess() {
						if path.HasEndpoint {
							results = append(results, im.WriteResult{Path: aPath, Status: status})
						}
						continue
					}
				}

				// An attribute with the timed access quality outside a
				// timed interaction, after the access gate: a concrete path
				// answers NEEDS_TIMED_INTERACTION, a wildcard one skips the
				// attribute. Mirrors matter.js
				// packages/protocol/src/action/server/AttributeWriteResponse.ts:243-247
				// (`if (limits.timed && !this.session.timed) … Status.NeedsTimedInteraction`)
				// and :343-345 for the wildcard path; chip
				// src/app/WriteHandler.cpp:798-802.
				if timedRefused {
					if !wildcardEndpoint && path.HasAttribute {
						results = append(results, im.WriteResult{Path: aPath, Status: im.StatusNeedsTimedInteraction})
					}
					continue
				}

				writeCtx := ctx
				// The value written at this location: the request's own
				// value, or for an ADD this location's list with the
				// request's element appended. Every resolved location
				// starts from the request's value — a group ADD appends
				// the same element on every member endpoint, as
				// connectedhomeip gives every mapped endpoint a fresh
				// reader over the request's data
				// (src/app/WriteHandler.cpp:555-559 at 6170af84).
				v := value
				if path.ListAppend {
					merged, status := appendListValue(ctx, srv, attrID, value)
					if status != im.StatusSuccess {
						results = append(results, im.WriteResult{Path: aPath, Status: status})
						continue
					}
					v = merged
					// The server sees the whole list; the mark tells it
					// only the last element is new (AccessControl reports
					// just that one as Added).
					writeCtx = im.WithListAppendWrite(ctx)
				}
				// A fabric-scoped attribute is written by the accessing
				// fabric alone: a request without one is refused here for
				// every server, a declarer's attribute included — im's gate
				// knows only the snapshot's (RW-i3) — and every entry of
				// the written value carries the accessing fabric, whatever
				// the request wrote (R2-P6-acl-0402). matter.js injects
				// the accessing fabric into every decoded write value
				// ("We always inject the current fabricIndex for writes",
				// packages/protocol/src/action/server/AttributeWriteResponse.ts:466-473
				// at db021da0) before the behaviour sees it.
				if scope := fabricScopeFor(ctx, srv, attrID); scope != nil {
					if !im.HasAccessingFabric(scope.Fabric) {
						if !wildcardEndpoint && path.HasAttribute {
							results = append(results, im.WriteResult{Path: aPath, Status: im.StatusUnsupportedAccess})
						}
						continue
					}
					v.Value = stampAccessingFabric(v.Value, scope.Fabric)
				}
				// The written value against the cluster's definition,
				// before any server runs (owner decision 64): what
				// matter.js validates as the value enters the behavior's
				// state (packages/node/src/behavior/state/managed/Datasource.ts:667-676
				// at db021da0), for every server with a generated
				// definition, the hand-written ones included.
				if status := writtenHeld(srv, attrID, v); !status.IsSuccess() {
					results = append(results, im.WriteResult{Path: aPath, Status: status})
					continue
				}
				res := d.writeStored(writeCtx, ep, srv, aPath, v)
				// A successful write that changed the cluster advances the
				// endpoint-hosted DataVersion once, so DataVersionFilters
				// miss and subscribers see the change; one that changed
				// nothing leaves it, as matter.js advances a datasource's
				// version once per commit, only when a value changed
				// (packages/node/src/behavior/state/managed/Datasource.ts:1456-1457
				// at db021da0, an equal value no change at :1476-1481). The
				// verdict is the server's ([writeOne], res.Commit); a server
				// without one changed the written attribute. A root or
				// Aggregator server with a tracker of its own advances it
				// inside the same write, on the same verdict.
				if res.Status == im.StatusSuccess {
					changed := res.Commit == nil || len(res.Commit.Changed) > 0
					if _, own := ownDataVersion(ep, srv); changed && !own {
						ep.BumpClusterDataVersion(cPath.Cluster)
					}
					writeChangesFrom(ctx).record(aPath, changed)
				}
				results = append(results, res)
			}
		}
	}
	return results
}

// Invoke implements [im.Dispatcher]. It dispatches a concrete command
// path and returns exactly one [im.InvokeResult]; a path without an
// endpoint is answered UnsupportedEndpoint here. A wildcard-endpoint
// command — a group command's only shape — goes through
// [TopologyDispatcher.InvokeAuthorized] instead, which dispatches it to
// every endpoint that hosts the cluster (R2-P6-doc-0407).
func (d *TopologyDispatcher) Invoke(ctx context.Context, path im.ConcreteCommandPath, fields any) im.InvokeResult {
	if !path.HasEndpoint {
		return im.InvokeResult{Path: path, Status: im.StatusUnsupportedEndpoint}
	}
	ep := d.topology.FindByID(path.Endpoint)
	if ep == nil {
		return im.InvokeResult{Path: path, Status: im.StatusUnsupportedEndpoint}
	}
	for _, srv := range ClusterServers(ep) {
		if srv.MatterClusterID() != path.Cluster {
			continue
		}
		resp, err := srv.MatterInvoke(contract.WithOrigin(im.WithCommandPath(ctx, path), contract.OriginCommand), path.Command, fields)
		if err != nil {
			status, cs, hasCS := classifyError(err)
			return im.InvokeResult{Path: path, Response: resp, Status: status, ClusterStatus: cs, HasClusterStatus: hasCS}
		}
		ep.invalidateSceneOnCommand(srv, path.Cluster)
		return im.InvokeResult{Path: path, Response: resp, Status: im.StatusSuccess}
	}
	return im.InvokeResult{Path: path, Status: im.StatusUnsupportedCluster}
}

// CommandPathStatus implements [im.CommandPathChecker]: UNSUPPORTED_ENDPOINT
// for an endpoint the topology lacks, UNSUPPORTED_CLUSTER for a cluster the
// endpoint does not serve, UNSUPPORTED_COMMAND for a command its server
// lists as not accepted (a server without a command lister answers for
// itself), Success otherwise — the existence checks of matter.js
// CommandInvokeResponse.ts #processConcrete, in their order.
func (d *TopologyDispatcher) CommandPathStatus(path im.ConcreteCommandPath) im.StatusCode {
	ep := d.topology.FindByID(path.Endpoint)
	if ep == nil {
		return im.StatusUnsupportedEndpoint
	}
	for _, srv := range ClusterServers(ep) {
		if srv.MatterClusterID() != path.Cluster {
			continue
		}
		if lister, ok := srv.(contract.ClusterCommandLister); ok && !slices.Contains(lister.MatterAcceptedCommands(), path.Command) {
			return im.StatusUnsupportedCommand
		}
		return im.StatusSuccess
	}
	return im.StatusUnsupportedCluster
}

// EventPathStatus implements [im.EventPathChecker]: UNSUPPORTED_ENDPOINT for
// an endpoint the topology lacks, UNSUPPORTED_CLUSTER for a cluster the
// endpoint does not serve, UNSUPPORTED_EVENT for an event its server does
// not list, Success otherwise — the existence checks of matter.js
// packages/protocol/src/action/server/EventReadResponse.ts:158-221 at
// 862ad75f (#addConcrete), in their order. A path that names no event is
// UNSUPPORTED_EVENT when the server lists none: matter.js registers a
// wildcard event path per event of the cluster (EventReadResponse.ts:277-284
// at db021da0), so such a cluster selects nothing (review II,
// R2-P4-imread-08). A server without a [contract.ClusterEventLister] lists
// no event, as the contract states, so neither shape of path exists on it
// (EventReadResponse.ts:216-218 for a named one; review II, R2-P4-nw-05).
func (d *TopologyDispatcher) EventPathStatus(path im.ConcreteEventPath) im.StatusCode {
	ep := d.topology.FindByID(path.Endpoint)
	if ep == nil {
		return im.StatusUnsupportedEndpoint
	}
	for _, srv := range ClusterServers(ep) {
		if srv.MatterClusterID() != path.Cluster {
			continue
		}
		var events []uint32
		if lister, ok := srv.(contract.ClusterEventLister); ok {
			events = lister.MatterEvents()
		}
		switch {
		case !path.HasEvent && len(events) == 0:
			return im.StatusUnsupportedEvent
		case path.HasEvent && !slices.Contains(events, path.Event):
			return im.StatusUnsupportedEvent
		}
		return im.StatusSuccess
	}
	return im.StatusUnsupportedCluster
}

// InvokeAuthorized implements [im.AuthorizingInvoker]: a command whose
// endpoint is a wildcard runs on every endpoint that hosts the cluster and
// accepts the command, in ascending endpoint order, each location gated by
// authorize. A server that lists no accepted commands is taken to accept
// the command and answers for itself. Mirrors matter.js
// CommandInvokeResponse #processWildcard / #wildcardTargetOf, which skip an
// endpoint lacking the cluster or command and one the session may not
// invoke there.
func (d *TopologyDispatcher) InvokeAuthorized(ctx context.Context, path im.ConcreteCommandPath, fields any, authorize im.CommandAuthorizer) []im.InvokeResult {
	if path.HasEndpoint {
		return []im.InvokeResult{d.Invoke(ctx, path, fields)}
	}
	var results []im.InvokeResult
	for _, t := range d.commandTargets(path, authorize) {
		ePath := path
		ePath.Endpoint, ePath.HasEndpoint = t.ep.ID, true
		resp, err := t.srv.MatterInvoke(contract.WithOrigin(im.WithCommandPath(ctx, ePath), contract.OriginCommand), path.Command, fields)
		res := im.InvokeResult{Path: ePath, Response: resp, Status: im.StatusSuccess}
		if err != nil {
			res.Status, res.ClusterStatus, res.HasClusterStatus = classifyError(err)
		} else {
			// As the concrete path does ([TopologyDispatcher.Invoke]):
			// matter.js runs a wildcard-endpoint or group invoke per
			// endpoint exactly as a concrete one
			// (CommandInvokeResponse.ts:330-358), and the scene
			// invalidation is the command's (R2-P6-par-0404).
			t.ep.invalidateSceneOnCommand(t.srv, path.Cluster)
		}
		results = append(results, res)
	}
	return results
}

// CommandTargets implements [im.CommandTargeter]: the endpoints
// [TopologyDispatcher.InvokeAuthorized] would run the wildcard-endpoint
// command path on, in ascending order, without running it.
func (d *TopologyDispatcher) CommandTargets(path im.ConcreteCommandPath, authorize im.CommandAuthorizer) []uint16 {
	targets := d.commandTargets(path, authorize)
	out := make([]uint16, 0, len(targets))
	for _, t := range targets {
		out = append(out, t.ep.ID)
	}
	return out
}

// commandTarget is an endpoint a wildcard command runs on and its server.
type commandTarget struct {
	ep  *Endpoint
	srv contract.ClusterServer
}

// commandTargets selects the endpoints of a wildcard-endpoint command:
// every endpoint hosting the cluster whose server accepts the command (a
// server that lists no accepted commands answers for itself) and that
// authorize admits — matter.js CommandInvokeResponse #wildcardTargetOf.
func (d *TopologyDispatcher) commandTargets(path im.ConcreteCommandPath, authorize im.CommandAuthorizer) []commandTarget {
	var out []commandTarget
	for _, ep := range d.resolveEndpoints(im.ConcreteAttributePath{}) {
		for _, srv := range ClusterServers(ep) {
			if srv.MatterClusterID() != path.Cluster {
				continue
			}
			if lister, ok := srv.(contract.ClusterCommandLister); ok && !slices.Contains(lister.MatterAcceptedCommands(), path.Command) {
				break
			}
			if authorize != nil && !authorize(ep.ID, path.Cluster, path.Command).IsSuccess() {
				break
			}
			out = append(out, commandTarget{ep: ep, srv: srv})
			break
		}
	}
	return out
}

// resolveEndpoints expands the wildcard-aware endpoint selector. A
// concrete endpoint that does not exist returns an empty slice so
// the caller can synthesise an UnsupportedEndpoint status. Wildcard
// endpoint includes the root (endpoint 0) — Apple Home's
// post-CommissioningComplete wildcard subscribe needs Endpoint 0
// reports (BasicInformation.VendorID/ProductID/NodeLabel, …) to
// build its HAP service map. Without them Apple shows
// `<MTRDevice ... VID: Unknown, PID: Unknown>` and aborts pairing
// at HMMTRAccessoryPairingStep_BuildingHAPServicesAndCharacteristicsFromCHIP
// with HAPErrorDomain Code 24.
func (d *TopologyDispatcher) resolveEndpoints(path im.ConcreteAttributePath) []*Endpoint {
	if path.HasEndpoint {
		ep := d.topology.FindByID(path.Endpoint)
		if ep == nil {
			return nil
		}
		return []*Endpoint{ep}
	}
	// All endpoints, root first so wildcard reports surface
	// device-identity attributes (VendorID/ProductID/UniqueID on the
	// root) before the bridged endpoints. The aggregator endpoint
	// (EP 1) MUST be included between root and the bridged endpoints:
	// Apple Home reads Aggregator.Descriptor.{DeviceTypeList, PartsList,
	// ServerList} from the Subscribe-Initial cache during HAP service
	// rebuild — a missing aggregator report makes
	// `_attributeValueDictionaryForAttributePath` log "PartsList absent
	// from cache" and Apple aborts the pair with HAPErrorDomain Code 14
	// ("Could not construct any of the services of node ...").
	// Every other endpoint follows in topology order — the parts of a
	// bridged endpoint ([Spec.Parts]) included, which `Topology.Bridged()`
	// leaves out.
	out := make([]*Endpoint, 0, len(d.topology.Endpoints))
	if root := d.topology.FindByID(0); root != nil {
		out = append(out, root)
	}
	if agg := d.topology.FindByID(1); agg != nil {
		out = append(out, agg)
	}
	for _, ep := range d.topology.Endpoints {
		if ep != nil && !ep.IsRoot() && !ep.IsAggregator() {
			out = append(out, ep)
		}
	}
	return out
}

// serversFor narrows the cluster-server set for ep based on the
// path's cluster selector. Returns a slice (possibly empty) of cluster
// servers; an empty slice on a concrete cluster means the endpoint
// exists but the cluster is absent.
func (d *TopologyDispatcher) serversFor(ep *Endpoint, path im.ConcreteAttributePath) []contract.ClusterServer {
	all := ClusterServers(ep)
	if !path.HasCluster {
		return all
	}
	for _, srv := range all {
		if srv.MatterClusterID() == path.Cluster {
			return []contract.ClusterServer{srv}
		}
	}
	return nil
}

// attributesFor selects the attribute IDs for a Read / Write across
// the cluster server. Concrete attribute → single-element slice.
// Wildcard attribute → the universal globals merged with the
// cluster's full attribute surface.
//
// Cluster servers expose their attribute set via the optional
// [contract.ClusterAttributeLister] interface. When a cluster
// does not implement it, we fall back to [contract.ClusterServer.MatterReportable]
// — that is at least the subscribable attribute surface, which gives
// Apple Home / matter.js something to bind to. The strict spec
// behaviour (every defined attribute) is achieved by implementing
// MatterAttributes on every cluster; the fallback is the safety net
// for cluster servers that have not been migrated yet.
func (d *TopologyDispatcher) attributesFor(srv contract.ClusterServer, path im.ConcreteAttributePath) []uint32 {
	if path.HasAttribute {
		return []uint32{path.Attribute}
	}
	// All six global attributes per Matter Core Spec §7.13.2 are
	// mandatory on every cluster server. matter.js's behaviour layer
	// auto-generates them at the cluster level; we synthesize them in
	// the dispatcher (see [synthesizeGlobalRead]) when the cluster
	// itself does not return a value. Apple Home's HAP service rebuild
	// reads `AttributeList` (0xFFFB) on every cluster — without it the
	// build fails with HAPErrorDomain Code=24 even when every other
	// cluster surface is correct.
	// EventList (0xFFFA) is a Matter 1.4 global attribute. Apple Home's
	// iOS Matter SDK (still on Matter 1.3 baseline as of iOS 26.4)
	// rejects it with `MTRErrorDomain Code=12 "No known schema for
	// decoding attribute value."` on every cluster that exposes it,
	// then drops the entire ReportData stream — the post-Subscribe-
	// Initial Descriptor.PartsList read returns empty, HAP-Service-
	// Build aborts with HAPErrorDomain Code=14 "No Endpoints In Use",
	// and the controller fires RemoveFabric ~5 s later. Spec §7.13.2
	// allows omitting unsupported global attributes; we drop EventList
	// from wildcard expansion (chip-tool / matter.js controllers do
	// not require it). Re-enable when Apple advances to Matter 1.4.
	out := []uint32{
		cluster.AttrGlobalGeneratedCommandList,
		cluster.AttrGlobalAcceptedCommandList,
		cluster.AttrGlobalAttributeList,
		cluster.AttrGlobalFeatureMap,
		cluster.AttrGlobalClusterRevision,
	}
	var extra []uint32
	if lister, ok := srv.(contract.ClusterAttributeLister); ok {
		extra = lister.MatterAttributes()
	} else {
		// Fallback: subscribable attribute surface. Better than
		// globals-only for wildcard reads — Apple Home builds its
		// HAP service map from these reports.
		extra = srv.MatterReportable()
	}
	seen := make(map[uint32]struct{}, len(out)+len(extra))
	for _, id := range out {
		seen[id] = struct{}{}
	}
	for _, id := range extra {
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	// The cluster's attribute order is the one every report uses
	// (im.CompareAttributeIDs) — a cluster without attributes of its own
	// included, whose globals matter.js lists in its model's order too
	// (packages/node/src/node/integration/ProtocolService.ts:449-522 at
	// db021da0).
	clusterID := srv.MatterClusterID()
	slices.SortFunc(out, func(a, b uint32) int { return im.CompareAttributeIDs(clusterID, a, b) })
	return out
}

// readOne dispatches a single Read against srv and packages the
// outcome. Global attributes (Matter §7.13.2) that the cluster server
// does not implement directly fall through to [synthesizeGlobalRead],
// so every cluster surfaces a consistent global-attribute set without
// each cluster server having to repeat the boilerplate.
//
// ctx carries the FabricFiltered flag + FabricIndex. When srv
// implements [contract.FabricScopedReader], MatterReadFiltered is
// called in preference to MatterRead so the server can project
// fabric-sensitive lists (e.g. OperationalCredentials.Fabrics) to
// the requesting fabric. Mirrors matter.js InteractionServer.ts
// #executeReadInteraction → #prepareOnlineContext(…, isFabricFiltered)
// (:306-326 at 862ad75f). A reader's (nil, false) hands the attribute to
// MatterRead, except on a request for a fabric-scoped attribute
// ([fabricScopedReadFailed]).
//
// On a request, the value of a fabric-scoped attribute carries its scope
// ([fabricScopeFor]): the dispatcher scopes every fabric-scoped attribute
// of the model, whatever the server returns — from MatterReadFiltered or
// MatterRead alike — and the scope is applied where the value is encoded
// ([im.MaskFabricScoped]). A read with no request behind it carries none.
//
// DataVersion: the cluster's current DataVersion ([clusterDataVersionFor])
// is stamped on the ReadResult with HasDataVersion. The IM layer uses
// this in HandleReadRequest for DataVersionFilter evaluation.
// Mirrors matter.js InteractionServer.ts attributeReportPayload building.
func readOne(ctx context.Context, ep *Endpoint, srv contract.ClusterServer, path im.ConcreteAttributePath) im.ReadResult {
	dv, hasDV := clusterDataVersionFor(ep, srv)
	scope := fabricScopeFor(ctx, srv, path.Attribute)
	// Fabric-scoped attributes: prefer MatterReadFiltered when the
	// cluster server opts in by implementing FabricScopedReader.
	if fsr, ok := srv.(contract.FabricScopedReader); ok {
		v, ok := fsr.MatterReadFiltered(ctx, path.Attribute)
		if ok {
			if v == nil {
				return im.ReadResult{Path: path, Value: im.AttributeValue{IsNull: true}, Status: im.StatusSuccess, DataVersion: dv, HasDataVersion: hasDV, FabricScope: scope}
			}
			return im.ReadResult{Path: path, Value: im.AttributeValue{Value: v}, Status: im.StatusSuccess, DataVersion: dv, HasDataVersion: hasDV, FabricScope: scope}
		}
		if status, failed := fabricScopedReadFailed(ctx, srv, path.Attribute); failed {
			return im.ReadResult{Path: path, Status: status}
		}
		// FabricScopedReader returned (nil, false) for an attribute it
		// does not scope — fall through to the universal MatterRead path
		// so non-fabric attributes on the same server are still served.
	}
	v, ok := srv.MatterRead(path.Attribute)
	if ok {
		if v == nil {
			return im.ReadResult{Path: path, Value: im.AttributeValue{IsNull: true}, Status: im.StatusSuccess, DataVersion: dv, HasDataVersion: hasDV, FabricScope: scope}
		}
		return im.ReadResult{Path: path, Value: im.AttributeValue{Value: v}, Status: im.StatusSuccess, DataVersion: dv, HasDataVersion: hasDV, FabricScope: scope}
	}
	// Cluster did not handle the read. Try synthesising the value if
	// it is a global-attribute ID — this lets every cluster expose
	// AttributeList / AcceptedCommandList / GeneratedCommandList
	// automatically (EventList is not synthesised, see
	// [synthesizeGlobalRead]). The cluster-internal MatterRead still
	// wins when it returns a value (so clusters that want a richer
	// AcceptedCommandList can override).
	if synth, synthOK := synthesizeGlobalRead(srv, path.Attribute); synthOK {
		return im.ReadResult{Path: path, Value: im.AttributeValue{Value: synth}, Status: im.StatusSuccess, DataVersion: dv, HasDataVersion: hasDV}
	}
	return im.ReadResult{Path: path, Status: im.StatusUnsupportedAttribute}
}

// fabricScopeFor is the scope a request's read of srv's attrID carries,
// nil for a read with no request behind it ([im.LookupFabricFilter]) and
// for an attribute that is not fabric-scoped. For a cluster of the schema
// snapshot the generated table is the whole answer
// ([schema.AttributeFabricScope]) — whatever the server is, a host's own
// or the module's, and whether or not it is a
// [contract.FabricScopedReader]. A cluster outside the snapshot is scoped
// as its server declares ([contract.FabricScopeDeclarer]; spec.Instance
// declares its definition's), and not at all without a declaration.
func fabricScopeFor(ctx context.Context, srv contract.ClusterServer, attrID uint32) *im.FabricScope {
	filtered, fabric, request := im.LookupFabricFilter(ctx)
	if !request {
		return nil
	}
	clusterID := srv.MatterClusterID()
	var sensitive bool
	var fields []uint32
	if _, standard := schema.ClusterRevision(clusterID); standard {
		s, ok := schema.AttributeFabricScope(clusterID, attrID)
		if !ok {
			return nil
		}
		sensitive, fields = s.Sensitive, s.Fields
	} else {
		d, ok := srv.(contract.FabricScopeDeclarer)
		if !ok {
			return nil
		}
		s, ok := d.MatterFabricScope(attrID)
		if !ok {
			return nil
		}
		sensitive, fields = s.Sensitive, slices.Clone(s.Fields)
	}
	return &im.FabricScope{Fabric: fabric, Filtered: filtered, Sensitive: sensitive, Fields: fields}
}

// fabricScopedReadFailed decides a [contract.FabricScopedReader]'s
// (nil, false) on a request — a context [im.LookupFabricFilter] finds a
// filter in — for an attribute whose access is fabric-scoped
// ([schema.IsFabricScopedRead]). The reader is the only read such a request
// may get: MatterRead has no request behind it and answers what a local
// actor sees, every fabric's entries whole. So the request answers FAILURE
// when the server lists the attribute — its scoped read failed — and
// UNSUPPORTED_ATTRIBUTE when the server's [contract.ClusterAttributeLister]
// leaves it out; a server without a lister answers FAILURE. Every other
// (nil, false) — an attribute that is not fabric-scoped, or a read with no
// request behind it — reports failed=false and is MatterRead's to answer.
//
// Mirrors matter.js
// packages/protocol/src/interaction/InteractionMessenger.ts:425-459 at
// db021da0 (InteractionServerMessenger.handleRequest: a read that throws
// is answered with Status.Failure, no other read stands in for it). There
// the whole interaction answers it; here the attribute's path does.
func fabricScopedReadFailed(ctx context.Context, srv contract.ClusterServer, attrID uint32) (im.StatusCode, bool) {
	if _, _, request := im.LookupFabricFilter(ctx); !request || !schema.IsFabricScopedRead(srv.MatterClusterID(), attrID) {
		return im.StatusSuccess, false
	}
	if lister, ok := srv.(contract.ClusterAttributeLister); ok && !slices.Contains(lister.MatterAttributes(), attrID) {
		return im.StatusUnsupportedAttribute, true
	}
	return im.StatusFailure, true
}

// synthesizeGlobalRead returns the default value for a Matter global
// attribute (Spec §7.13.2) when the cluster server does not handle it
// itself. Returns (value, true) for handled IDs; (nil, false) for any
// non-global attribute. The synthesized values are computed from the
// optional [contract.ClusterAttributeLister] /
// [contract.ClusterCommandLister] /
// [contract.ClusterEventLister] surfaces — clusters that don't
// implement those lister interfaces get an empty list, which is the
// spec-compliant minimum (a cluster with no commands legitimately
// reports `AcceptedCommandList = []`).
//
// AttributeList includes both the cluster's own attributes (via
// MatterAttributes / MatterReportable) and the six global IDs, per
// Matter §7.13.2.4 ("AttributeList SHALL include the IDs of every
// attribute the cluster server implements, including the global
// attributes").
func synthesizeGlobalRead(srv contract.ClusterServer, attrID uint32) (any, bool) {
	switch attrID {
	case cluster.AttrGlobalAttributeList:
		// EventList (0xFFFA) intentionally omitted — see the EventList
		// arm below and [TopologyDispatcher.attributesFor], which leaves
		// it out of a wildcard read's expansion. Apple's iOS Matter SDK rejects the
		// whole ReportData stream when AttributeList advertises
		// EventList but the SDK lacks the Matter 1.4 schema for it.
		seen := map[uint32]struct{}{
			cluster.AttrGlobalGeneratedCommandList: {},
			cluster.AttrGlobalAcceptedCommandList:  {},
			cluster.AttrGlobalAttributeList:        {},
			cluster.AttrGlobalFeatureMap:           {},
			cluster.AttrGlobalClusterRevision:      {},
		}
		out := []uint32{
			cluster.AttrGlobalGeneratedCommandList,
			cluster.AttrGlobalAcceptedCommandList,
			cluster.AttrGlobalAttributeList,
			cluster.AttrGlobalFeatureMap,
			cluster.AttrGlobalClusterRevision,
		}
		var clusterAttrs []uint32
		if lister, ok := srv.(contract.ClusterAttributeLister); ok {
			clusterAttrs = lister.MatterAttributes()
		} else {
			clusterAttrs = srv.MatterReportable()
		}
		for _, id := range clusterAttrs {
			if _, dup := seen[id]; dup {
				continue
			}
			seen[id] = struct{}{}
			out = append(out, id)
		}
		slices.Sort(out)
		return out, true
	// The two command lists read in ascending id order, whatever order the
	// server lists them in; the server's own slice stays as it is. Mirrors
	// matter.js packages/node/src/behavior/internal/ServerBehaviorBacking.ts:135-136
	// at db021da0 (acceptedIds.sort, [...generatedIds].sort, ascending).
	case cluster.AttrGlobalAcceptedCommandList:
		if lister, ok := srv.(contract.ClusterCommandLister); ok {
			return ascending(lister.MatterAcceptedCommands()), true
		}
		return []uint32{}, true
	case cluster.AttrGlobalGeneratedCommandList:
		if lister, ok := srv.(contract.ClusterCommandLister); ok {
			return ascending(lister.MatterGeneratedCommands()), true
		}
		return []uint32{}, true
	case cluster.AttrGlobalEventList:
		// Apple's iOS 26 Matter SDK does not yet ship a schema for
		// the Matter 1.4 EventList attribute. Returning [] (or any
		// other value) makes Apple drop the whole ReportData stream
		// with `MTRErrorDomain Code=12 "No known schema for decoding
		// attribute value."` and abort the pair via HAPErrorDomain
		// Code=14. Treat the attribute as unsupported (return false):
		// a concrete read of it is answered StatusUnsupportedAttribute,
		// which Apple's IM-Decoder handles cleanly; a wildcard read never
		// names it (attributesFor leaves it out), and an attribute a
		// wildcard endpoint cannot serve is skipped, not reported
		// (N4-read-1). matter.js / chip-tool
		// tolerate the absence; re-enable once Apple advances to
		// Matter 1.4 SDK schema.
		return nil, false
	}
	return nil, false
}

// ascending returns a copy of ids in ascending order — an empty list stays
// an empty list, not nil.
func ascending(ids []uint32) []uint32 {
	out := append(make([]uint32, 0, len(ids)), ids...)
	slices.Sort(out)
	return out
}

// attributeListed reports whether attrID is one of the attributes srv
// implements, by the same AttributeList a controller reads: the server's
// own when it answers one, the dispatcher's synthesis otherwise.
//
// A server that neither answers AttributeList nor implements
// [contract.ClusterAttributeLister] declares no attribute set to check
// against (the synthesis would fall back to its reportable list), so its
// writes reach it as before.
func attributeListed(srv contract.ClusterServer, attrID uint32) bool {
	list, ok := srv.MatterRead(cluster.AttrGlobalAttributeList)
	if !ok {
		if _, lister := srv.(contract.ClusterAttributeLister); !lister {
			return true
		}
		list, ok = synthesizeGlobalRead(srv, cluster.AttrGlobalAttributeList)
	}
	ids, isList := list.([]uint32)
	if !ok || !isList {
		// A server whose AttributeList is not a plain id list keeps the
		// previous behaviour: the write reaches it.
		return true
	}
	return slices.Contains(ids, attrID)
}

// writeStored is [writeOne] behind the topology's [WritePersister]: a
// nonvolatile attribute the module holds is stored before the server
// takes it; a refused store write answers FAILURE and the server never
// sees the value, a write the server refuses puts the store back. The
// result is [writeOne]'s.
func (d *TopologyDispatcher) writeStored(ctx context.Context, ep *Endpoint, srv contract.ClusterServer, path im.ConcreteAttributePath, value im.AttributeValue) im.WriteResult {
	p := d.topology.Persister
	if p == nil {
		return writeOne(ctx, srv, path, value)
	}
	done, err := p.PersistWrite(ctx, ep, srv, path.Attribute, value)
	if err != nil {
		return im.WriteResult{Path: path, Status: im.StatusFailure}
	}
	return persistedWrite(ctx, srv, path, value, done)
}

// persistedWrite is [writeOne] followed by done, the [WritePersister]'s
// settlement, with whether srv applied the write — also when srv panics,
// so the persister is never left waiting. A nil done is writeOne alone.
func persistedWrite(ctx context.Context, srv contract.ClusterServer, path im.ConcreteAttributePath, value im.AttributeValue, done func(applied bool)) im.WriteResult {
	if done == nil {
		return writeOne(ctx, srv, path, value)
	}
	applied := false
	defer func() { done(applied) }()
	res := writeOne(ctx, srv, path, value)
	applied = res.Status == im.StatusSuccess
	return res
}

// writtenHeld is the status the definition of srv's cluster answers a
// controller's written value of attrID with ([spec.CheckWritten]): the
// server's FeatureMap selects the enum values and conformance, its
// attributes resolve the ones a constraint names. Success for a cluster
// without a registered definition.
func writtenHeld(srv contract.ClusterServer, attrID uint32, value im.AttributeValue) im.StatusCode {
	def := spec.Lookup(srv.MatterClusterID())
	if def == nil {
		return im.StatusSuccess
	}
	var features uint32
	if v, ok := srv.MatterRead(cluster.AttrGlobalFeatureMap); ok {
		if fm, ok := v.(uint32); ok {
			features = fm
		}
	}
	v := value.Value
	if value.IsNull {
		v = nil
	}
	if err := spec.CheckWritten(def, features, attrID, v, srv.MatterRead); err != nil {
		return spec.StatusOf(err)
	}
	return im.StatusSuccess
}

// writeOne dispatches a single Write against srv. Errors are mapped
// to spec-coded status values where the cluster surfaces them
// distinctly; the catch-all is StatusFailure. A successful write carries
// its verdict in Commit, the server's own when it gives one — decided
// inside the write, so a change another writer makes around it never
// stands in for it: the attributes a [contract.ChangeNamingWriter] names
// (sorted, each once), the written attribute or none for a
// [contract.ChangeReportingWriter]'s true or false, the written attribute
// for a server that gives none. A refused write carries none.
func writeOne(ctx context.Context, srv contract.ClusterServer, path im.ConcreteAttributePath, value im.AttributeValue) im.WriteResult {
	v := value.Value
	if value.IsNull {
		v = nil
	}
	ctx = contract.WithOrigin(ctx, contract.OriginControllerWrite)
	var (
		changed []uint32
		err     error
	)
	switch w := srv.(type) {
	case contract.ChangeNamingWriter:
		changed, err = w.MatterWriteCommit(ctx, path.Attribute, v)
		if err == nil {
			changed = slices.Compact(slices.Sorted(slices.Values(changed)))
		}
	case contract.ChangeReportingWriter:
		var moved bool
		moved, err = w.MatterWriteChanged(ctx, path.Attribute, v)
		if moved {
			changed = []uint32{path.Attribute}
		}
	default:
		err = srv.MatterWrite(ctx, path.Attribute, v)
		changed = []uint32{path.Attribute}
	}
	if err != nil {
		status, cs, hasCS := classifyError(err)
		return im.WriteResult{Path: path, Status: status, ClusterStatus: cs, HasClusterStatus: hasCS}
	}
	if changed == nil {
		changed = []uint32{}
	}
	return im.WriteResult{Path: path, Status: im.StatusSuccess, Commit: &im.WriteCommit{Changed: changed}}
}

// classifyError maps a cluster server's write or invoke error to the
// status it answers: a host's [contract.StatusError] its Status (and
// ClusterStatus), an [im.MatterClusterStatusError] carries the
// cluster-specific code into StatusIB.ClusterStatus as well (Matter
// §10.6.2.2), an [im.StatusCodeError] its IM status, and any other error
// is FAILURE — a server names its status by type, never by the wording of
// its message (ADR 0026 principle 8). A status error naming Success
// answers FAILURE. Mirrors matter.js's answer to a StatusResponseError a
// behaviour throws, its code and clusterCode
// (packages/types/src/common/StatusResponseError.ts:18-25 at 862ad75f).
// Returns (status, clusterStatus, hasClusterStatus).
func classifyError(err error) (im.StatusCode, uint8, bool) {
	if err == nil {
		return im.StatusSuccess, 0, false
	}
	if se, ok := hostStatusError(err); ok {
		if se.Status == uint8(im.StatusSuccess) {
			return im.StatusFailure, 0, false
		}
		if se.ClusterStatus != nil {
			return im.StatusCode(se.Status), *se.ClusterStatus, true
		}
		return im.StatusCode(se.Status), 0, false
	}
	if cse, ok := errors.AsType[im.MatterClusterStatusError](err); ok {
		return cse.MatterStatusCode(), cse.MatterClusterStatus(), true
	}
	if sce, ok := errors.AsType[im.StatusCodeError](err); ok {
		return sce.MatterStatusCode(), 0, false
	}
	return im.StatusFailure, 0, false
}

// hostStatusError is the [contract.StatusError] in err's chain, by value or
// by pointer.
func hostStatusError(err error) (contract.StatusError, bool) {
	if se, ok := errors.AsType[contract.StatusError](err); ok {
		return se, true
	}
	if se, ok := errors.AsType[*contract.StatusError](err); ok && se != nil {
		return *se, true
	}
	return contract.StatusError{}, false
}

// MinReadPrivilege implements [im.AttributeReadPrivilegeProvider]: the
// read privilege of the data model ([schema.AttributeReadPrivilege],
// generated from the effective access strings of parity/schema.json, as
// the write and invoke privileges are), raised further when the cluster
// server's [contract.ClusterAttributeReadPrivilege] asks for more. A
// server's answer cannot lower it: a hand-written server that does not
// forward the optional interface (ICDManagement, GeneralCommissioning)
// used to fall back to View, so a View subject read their Administer
// attributes (review II, R2-P3-admin-02). matter.js authorizes every read
// against the attribute's limits.readLevel
// (packages/protocol/src/action/server/AttributeReadResponse.ts:272, and
// :408-411 on the wildcard path, at db021da0).
//
// A path that does not resolve — no such endpoint, no such cluster on it,
// an attribute the cluster does not list — takes no floor from the data
// model: matter.js authorizes a concrete read at View first, answers the
// existence statuses next and checks limits.readLevel only for an
// attribute that exists (AttributeReadResponse.ts:250-274 at db021da0), as
// [MinWritePrivilege] does for a write. The attribute list is asked only
// for an attribute the data model reads above View.
func (d *TopologyDispatcher) MinReadPrivilege(endpoint uint16, clusterID, attrID uint32) uint8 {
	const privilegeView uint8 = 1
	ep := d.topology.FindByID(endpoint)
	if ep == nil {
		return privilegeView
	}
	for _, srv := range ClusterServers(ep) {
		if srv.MatterClusterID() != clusterID {
			continue
		}
		want := schema.AttributeReadPrivilege(clusterID, attrID)
		if want > privilegeView && !isGlobalAttribute(attrID) && !attributeListed(srv, attrID) {
			want = privilegeView
		}
		if priv, ok := srv.(contract.ClusterAttributeReadPrivilege); ok {
			want = max(want, priv.MinReadPrivilege(attrID))
		}
		return want
	}
	return privilegeView
}

// MinWritePrivilege implements [im.AttributeWritePrivilegeProvider]: the
// write privilege matter.js gives the attribute ([schema.AttributeWritePrivilege],
// generated from the access strings of parity/schema.json), raised further
// when the cluster server's [contract.ClusterAttributeWritePrivilege] asks for
// more. Servers without the optional interface used to fall back to Operate,
// so an Operate subject could write BridgedDeviceBasicInformation.NodeLabel,
// OnOff.StartUpOnOff or the Thermostat setpoint limits ("RW VM") —
// TC-ACE-2.2.
//
// A path that does not resolve — no such endpoint, no such cluster on it,
// an attribute the cluster does not list — needs View only: matter.js
// authorizes a concrete write at View first, answers the existence
// statuses next, and checks the write privilege only for an attribute that
// exists (packages/protocol/src/action/server/AttributeWriteResponse.ts:200-241
// at 862ad75f, core spec §8.7.3.2), so a View subject learns that an
// attribute is absent, as it does on a read. The dispatcher's Write then
// answers UNSUPPORTED_ENDPOINT, _CLUSTER or _ATTRIBUTE.
//
// An attribute that is not writable needs View only as well: both
// references answer UNSUPPORTED_WRITE between the View pass and the
// write-privilege pass (AttributeWriteResponse.ts:215, :231-234, :236-241 at
// db021da0; chip src/app/WriteHandler.cpp:777, :790-791, :794-795 at
// 6170af84 — a read-only attribute has no write privilege there at all), so
// a subject that may view the cluster learns that the attribute is
// read-only whatever else it may do. Before, such a subject was answered
// UNSUPPORTED_ACCESS, at the Operate default (review II, W3-clugen-05).
func (d *TopologyDispatcher) MinWritePrivilege(endpoint uint16, clusterID, attrID uint32) uint8 {
	const privilegeView uint8 = 1
	if writable, known := schema.AttributeWritable(clusterID, attrID); known && !writable {
		return privilegeView
	}
	want := schema.AttributeWritePrivilege(clusterID, attrID)
	ep := d.topology.FindByID(endpoint)
	if ep == nil {
		return privilegeView
	}
	for _, srv := range ClusterServers(ep) {
		if srv.MatterClusterID() != clusterID {
			continue
		}
		if !isGlobalAttribute(attrID) && !attributeListed(srv, attrID) {
			return privilegeView
		}
		if priv, ok := srv.(contract.ClusterAttributeWritePrivilege); ok {
			want = max(want, priv.MinWritePrivilege(attrID))
		}
		return want
	}
	return privilegeView
}

// MinInvokePrivilege implements [im.CommandInvokePrivilegeProvider]: the
// privilege matter.js gives the command ([schema.InvokePrivilege], Operate
// unless the element file says Manage or Administer), raised further when the
// cluster server's [contract.ClusterCommandInvokePrivilege] asks for more.
// Mirrors the command access bits in matter.js
// packages/model/src/standard/elements/*.element.ts; a server without the
// optional interface (Identify) used to fall back to Operate and let an
// Operate subject identify a device (TC-ACE-2.3).
//
// A path that does not resolve — no such endpoint, no such cluster on it,
// a command its server does not accept — needs Operate only: matter.js
// authorizes a concrete invoke at Operate first, answers the existence
// statuses next, and checks the command's privilege only for a command
// that exists (packages/protocol/src/action/server/CommandInvokeResponse.ts:241-278
// at 862ad75f, core spec §8.8.3.2), so an Operate subject learns that a
// Manage command's cluster is absent. The existence check then answers
// UNSUPPORTED_ENDPOINT, _CLUSTER or _COMMAND.
func (d *TopologyDispatcher) MinInvokePrivilege(endpoint uint16, clusterID, cmdID uint32) uint8 {
	const privilegeOperate uint8 = 3
	want := schema.InvokePrivilege(clusterID, cmdID)
	ep := d.topology.FindByID(endpoint)
	if ep == nil {
		return min(want, privilegeOperate)
	}
	for _, srv := range ClusterServers(ep) {
		if srv.MatterClusterID() != clusterID {
			continue
		}
		if lister, ok := srv.(contract.ClusterCommandLister); ok && !slices.Contains(lister.MatterAcceptedCommands(), cmdID) {
			return min(want, privilegeOperate)
		}
		if priv, ok := srv.(contract.ClusterCommandInvokePrivilege); ok {
			want = max(want, priv.MinInvokePrivilege(cmdID))
		}
		return want
	}
	return min(want, privilegeOperate)
}

// IsTimedCommand implements [im.TimedCommandProvider]: the cluster server
// the path resolves to answers from its own model when it implements
// [contract.TimedCommander] (every server built on a generated definition);
// a hand-written server, or a path no server resolves, falls back to
// [schema.IsTimedInvoke], the table generated from the same snapshot.
// Mirrors matter.js packages/protocol/src/action/server/CommandInvokeResponse.ts:292-295
// (`limits.timed` read from the command's model).
func (d *TopologyDispatcher) IsTimedCommand(endpoint uint16, clusterID, cmdID uint32) bool {
	if ep := d.topology.FindByID(endpoint); ep != nil {
		for _, srv := range ClusterServers(ep) {
			if srv.MatterClusterID() != clusterID {
				continue
			}
			if tc, ok := srv.(contract.TimedCommander); ok {
				return tc.IsTimedCommand(cmdID)
			}
			break
		}
	}
	return schema.IsTimedInvoke(clusterID, cmdID)
}

// FabricIndexUnresolvable marks a session whose fabric the bridge could not
// determine. Matter constrains a fabric index to "1 to 254" (matter.js
// packages/model/src/standard/elements/operational-credentials.element.ts:129,
// and three further declarations), so 255 can never collide with a real one,
// and 0 is already spoken for as "no fabric" — a session before AddNOC.
//
// Neither is a grant. 0 means the session has no fabric, 255 that the
// bridge does not know which fabric asked; [TopologyDispatcher.CheckACL]
// denies both. What it grants without a fabric it grants to the PASE auth
// mode ([im.IsPASEFromContext]), never to the index: a request with
// fabric 0 that is not PASE is refused (review 2026-11 W2-sec-03).
const FabricIndexUnresolvable uint8 = 255

// CheckACL implements [im.ACLChecker] (Matter §9.10). It grants the request
// when the requesting fabric holds a CASE ACL entry whose subject covers
// (subjectNodeID, subjectCATs), whose target covers (endpoint, clusterID),
// and whose privilege is at least requiredPrivilege; otherwise it returns
// UnsupportedAccess (0x7e). PASE sessions are already bypassed by the IM
// gate before this is called; the same grant is repeated here so a direct
// caller cannot lose it. It is keyed on the session's AUTH MODE alone —
// fabricIndex 0 without PASE is no grant and is denied, like an index the
// bridge could not resolve — because AddNOC adopts the commissioner's PASE
// session onto the new fabric (matter.js OperationalCredentialsServer.ts:279-283
// `session.fabric = fabric`) while the implicit Administer grant stays
// (FabricAccessControl.ts:189-191 `authMode === Pase && isCommissioning`).
// Without the auth-mode term every post-AddNOC ACL write, GroupKeySetWrite
// or other Administer command over that channel is evaluated as fabric N
// with subject node-id 0, which the default `[CaseAdminSubject]` ACE never
// matches — UnsupportedAccess.
//
// Every path that is not an explicit grant denies, including the one where
// no ACL source is wired at all — see [TopologyDispatcher.SetACLLister].
//
// Mirrors connectedhomeip/src/access/AccessControl.cpp:441-559 — the
// chip iterator walks every ACE on the fabric and applies the AuthMode →
// Privilege → Subjects → Targets filter chain in that order. Subjects with
// an empty list match any subject on the fabric (wildcard); otherwise each
// entry-subject is interpreted per chip src/lib/core/NodeId.h:
// operational node IDs match equality, CAT subjects (range
// 0xFFFF'FFFD'0000'0000..0xFFFF'FFFD'FFFF'FFFF) match via
// [matchesCATSubject] against the requester's CAT set.
func (d *TopologyDispatcher) CheckACL(ctx context.Context, fabricIndex uint8, subjectNodeID uint64, subjectCATs []uint32, endpoint uint16, clusterID uint32, requiredPrivilege uint8) im.StatusCode {
	if im.IsPASEFromContext(ctx) {
		return im.StatusSuccess // PASE — commissioning channel, implicit Administer
	}
	if fabricIndex == 0 || fabricIndex == FabricIndexUnresolvable {
		// No accessing fabric, or one the bridge could not name, and not
		// PASE: fail closed — also before an all-granting [UnenforcedACL],
		// which grants on a fabric, never on a missing one. The implicit
		// Administer is keyed on the PASE auth mode alone — matter.js
		// packages/protocol/src/interaction/FabricAccessControl.ts:189-191
		// (`authMode === Pase && isCommissioning`); review 2026-11
		// W2-sec-03.
		return im.StatusUnsupportedAccess
	}
	acl := d.aclLister()
	if acl == nil {
		// Fail closed. A dispatcher without an ACL source cannot tell an
		// authorised controller from any other node that completed CASE, so
		// answering the request would serve exactly what the AccessControl
		// entries exist to gate. The commissioning path is unaffected: it
		// runs over PASE and returned above. A deployment that means to run
		// without stored entries wires [UnenforcedACL].
		return im.StatusUnsupportedAccess
	}
	entries, err := acl.ListACL(ctx, fabricIndex)
	if err != nil {
		// Fail closed: an ACL that cannot be evaluated must not grant access.
		return im.StatusUnsupportedAccess
	}
	// Resolve the endpoint once for device-type-restricted targets;
	// nil (no topology / unknown endpoint) conservatively fails those.
	var ep *Endpoint
	if d.topology != nil {
		ep = d.topology.FindByID(endpoint)
	}
	if group, isGroup := im.GroupSubjectFromContext(ctx); isGroup {
		// The Auxiliary feature is on when the root's AccessControl
		// advertises it, whether or not a lister is attached: matter.js
		// AccessControlServer.#applyFabricAcl derives
		// auxiliaryFeatureEnabled from the cluster's own feature, so the
		// advertised feature and the endpoint 0 rule cannot disagree. A
		// host that mounts Groupcast (which turns AUX on) without
		// AttachAuxiliaryACL now gets the endpoint 0 restriction and no
		// auxiliary grants — closed, not open.
		auxSource := d.auxLister()
		auxEnabled := auxSource != nil || d.auxiliaryAdvertised()
		if auxSource != nil {
			// Auxiliary entries are all Group entries, so only a group
			// subject can match one. An auxiliary source that cannot be
			// read fails closed like the stored ACL.
			aux, err := auxSource.AuxiliaryACL(ctx, fabricIndex)
			if err != nil {
				return im.StatusUnsupportedAccess
			}
			entries = append(slices.Clone(entries), aux...)
		}
		return checkGroupACL(entries, group, ep, endpoint, clusterID, requiredPrivilege, auxEnabled)
	}
	var granted privilegeSet
	for _, e := range entries {
		// Operational unicast sessions are CASE-authenticated; only CASE
		// entries apply (Group = multicast, PASE = commissioning).
		if e.AuthMode != store.AuthModeCASE {
			continue
		}
		if !aclSubjectMatches(e.Subjects, subjectNodeID, subjectCATs) {
			continue
		}
		if !aclTargetMatches(e.Targets, ep, endpoint, clusterID) {
			continue
		}
		granted |= grantedBy(e.Privilege)
	}
	if granted.grants(requiredPrivilege) {
		return im.StatusSuccess
	}
	return im.StatusUnsupportedAccess
}

// accessControlFeatureAuxiliary is AccessControl's AUX feature bit
// (access-control.element.ts: Auxiliary, constraint 2).
const accessControlFeatureAuxiliary = 1 << 2

// auxiliaryAdvertised reports whether the root endpoint's AccessControl
// server advertises the Auxiliary feature.
func (d *TopologyDispatcher) auxiliaryAdvertised() bool {
	if d.topology == nil {
		return false
	}
	root := d.topology.FindByID(0)
	if root == nil {
		return false
	}
	for _, srv := range ClusterServers(root) {
		if srv.MatterClusterID() != 0x001F {
			continue
		}
		v, ok := srv.MatterRead(0xFFFC)
		if !ok {
			return false
		}
		fm, ok := v.(uint32)
		return ok && fm&accessControlFeatureAuxiliary != 0
	}
	return false
}

// checkGroupACL is the access check for a group message (Matter §9.10.5.6
// with the Group auth mode). Mirrors matter.js FabricAccessControl:
// #getIsdFromMessage gives a group message the Group auth mode and the
// group id as its only subject — but only while the authenticating key is
// the one GroupKeyMap maps the group to (hasValidMapping); otherwise the
// subject descriptor keeps no auth mode and no entry can match. Only Group
// entries apply; an empty Subjects list matches any group, otherwise the
// group id must be listed. A Group entry may never grant Administer:
// matter.js throws on that grant, which denies the request.
//
// With the Auxiliary feature (auxEnabled) entries holds the auxiliary
// entries too, and a Group entry without targets does not reach endpoint 0
// (FabricAccessControl #getGrantedPrivileges: "Group subjects cannot grant
// access to Endpoint 0 when Auxiliary feature is enabled").
func checkGroupACL(entries []store.ACLEntry, group im.GroupSubject, ep *Endpoint, endpoint uint16, clusterID uint32, requiredPrivilege uint8, auxEnabled bool) im.StatusCode {
	// The module's one producer of a GroupSubject always sets the mapping
	// (bridge/group_messaging.go from groups.Message), so this branch has
	// no case in its own wiring; it is the fail-closed default of a
	// public type whose zero value must deny (N4-w3gc-1).
	if !group.HasValidMapping {
		return im.StatusUnsupportedAccess
	}
	var granted privilegeSet
	sawAdminister := false
	for _, e := range entries {
		if e.AuthMode != store.AuthModeGroup {
			continue
		}
		if len(e.Subjects) > 0 && !slices.Contains(e.Subjects, uint64(group.GroupID)) {
			continue
		}
		if auxEnabled && len(e.Targets) == 0 && endpoint == 0 {
			continue
		}
		if !aclTargetMatches(e.Targets, ep, endpoint, clusterID) {
			continue
		}
		if e.Privilege == store.PrivilegeAdminister {
			sawAdminister = true
		}
		granted |= grantedBy(e.Privilege)
	}
	if sawAdminister {
		return im.StatusUnsupportedAccess
	}
	if granted.grants(requiredPrivilege) {
		return im.StatusSuccess
	}
	return im.StatusUnsupportedAccess
}

// aclSubjectMatches reports whether an ACL entry's Subjects list covers
// the requesting (subjectNodeID, subjectCATs). An empty list means
// "any subject on this fabric" (Matter §9.10.5.6).
//
// Mirrors connectedhomeip/src/access/AccessControl.cpp:463-509 — for each
// listed subject:
//   - operational node id (0x0000'0000'0000'0001..0xFFFF'FFEF'FFFF'FFFF):
//     match when the requester's node id equals it exactly.
//   - CAT (0xFFFF'FFFD'0000'0000..0xFFFF'FFFD'FFFF'FFFF): unpack to
//     identifier+version per CASEAuthTag.h:46-49 and match via
//     [matchesCATSubject] against the requester's CATs.
//
// A non-empty Subjects list that contains no covering entry denies the
// request even when the target+privilege would otherwise grant.
func aclSubjectMatches(subjects []uint64, subjectNodeID uint64, subjectCATs []uint32) bool {
	if len(subjects) == 0 {
		return true
	}
	for _, s := range subjects {
		if isCASEAuthTagSubject(s) {
			if matchesCATSubject(uint32(s&kMaskCASEAuthTag), subjectCATs) {
				return true
			}
			continue
		}
		// Operational node id: exact match. A subject of another kind
		// under AuthMode=CASE (a group id, say) matches no one here and the
		// check goes on to the entry's other subjects and the other
		// entries. chip ends the whole check instead — its
		// VerifyOrReturnError(..., CHIP_ERROR_INCORRECT_STATE) at
		// src/access/AccessControl.cpp:475, :484, :493 (and :498-502 for
		// any other subject) at 6170af84 returns an error out of CheckACL,
		// so no later entry is consulted; the subject grants nothing in
		// either (R2-P6-doc-0445). The ACL write validation should never
		// store such an entry.
		if subjectNodeID != 0 && s == subjectNodeID {
			return true
		}
	}
	return false
}

// Matter CASEAuthTag NodeID encoding (chip src/lib/core/NodeId.h:47-49):
// node IDs 0xFFFF'FFFD'0000'0000..0xFFFF'FFFD'FFFF'FFFF are reserved as
// CASE Authenticated Tag subjects; the low 32 bits split into a 16-bit
// identifier (upper) and a 16-bit version (lower) — see
// src/lib/core/CASEAuthTag.h:32-34.
const (
	kMinCASEAuthTag    uint64 = 0xFFFFFFFD00000000
	kMaxCASEAuthTag    uint64 = 0xFFFFFFFDFFFFFFFF
	kMaskCASEAuthTag   uint64 = 0x00000000FFFFFFFF
	kCATIdentifierMask uint32 = 0xFFFF0000
	kCATVersionMask    uint32 = 0x0000FFFF
)

func isCASEAuthTagSubject(s uint64) bool {
	return s >= kMinCASEAuthTag && s <= kMaxCASEAuthTag
}

// matchesCATSubject reports whether any CAT in subjectCATs is granted by
// the entry's CAT subject. Mirrors
// connectedhomeip/src/lib/core/CASEAuthTag.h:174-190
// (CATValues::CheckSubjectAgainstCATs): identifiers must match exactly,
// and the requester's CAT version must be present (>0) and at least the
// entry's version (so a v1 grant covers a v1-NOC holder but a v2 grant
// does not cover a v1-NOC holder).
func matchesCATSubject(entryCAT uint32, subjectCATs []uint32) bool {
	entryID := uint16(((entryCAT & kCATIdentifierMask) >> 16) & 0xFFFF)
	entryVer := uint16((entryCAT & kCATVersionMask) & 0xFFFF)
	for _, sub := range subjectCATs {
		if sub == 0 {
			continue
		}
		subID := uint16(((sub & kCATIdentifierMask) >> 16) & 0xFFFF)
		subVer := uint16((sub & kCATVersionMask) & 0xFFFF)
		if subID != entryID {
			continue
		}
		if subVer == 0 {
			continue // chip CASEAuthTag.h:184 — only present versions match
		}
		if subVer >= entryVer {
			return true
		}
	}
	return false
}

// endpointHasDeviceType reports whether ep advertises deviceType in
// its Descriptor DeviceTypeList — the resolver behind device-type ACL
// targets (chip AccessControl.h:53 DeviceTypeResolver /
// ProviderDeviceTypeResolver.h:34, backed by the data model's
// per-endpoint device-type list). It answers from the endpoint's whole
// list, not its primary device type alone (R2-P6-acl-0443): a bridged
// endpoint or a part from [listedDeviceTypes], the one function its
// Descriptor is built from, so the two cannot disagree; the root, the
// Aggregator and a bare endpoint from the DeviceTypeList of the
// Descriptor published on them, and — when none is published — RootNode,
// Aggregator or the endpoint's own device type.
func endpointHasDeviceType(ep *Endpoint, deviceType uint32) bool {
	if ep == nil {
		return false
	}
	if ep.IsRoot() || ep.IsAggregator() || ep.IsBare() {
		if listed, ok := attachedDescriptorDeviceTypes(ep); ok {
			for _, dt := range listed {
				if dt.DeviceType == deviceType {
					return true
				}
			}
			return false
		}
		switch {
		case ep.IsRoot():
			return deviceType == deviceTypeRootNode
		case ep.IsAggregator():
			return deviceType == deviceTypeAggregator
		default:
			return deviceType == uint32(ep.DeviceType)
		}
	}
	return deviceType <= 0xFFFF && slices.Contains(listedDeviceTypes(ep), uint16(deviceType))
}

// attachedDescriptorDeviceTypes returns the DeviceTypeList of the
// Descriptor published on ep, false when none is published.
func attachedDescriptorDeviceTypes(ep *Endpoint) ([]mattercore.DeviceTypeStruct, bool) {
	for _, srv := range ep.AttachedClusterServers() {
		desc, ok := srv.(*mattercore.Descriptor)
		if !ok {
			continue
		}
		v, ok := desc.MatterRead(descdef.AttrDeviceTypeList)
		if !ok {
			return nil, false
		}
		list, ok := v.([]mattercore.DeviceTypeStruct)
		return list, ok
	}
	return nil, false
}

// aclTargetMatches reports whether an ACL entry's target list covers
// (endpoint, cluster). An empty list means "all targets" (Matter §9.10.4.5).
// A DeviceType field restricts the target to endpoints hosting that
// device type — chip AccessControl.cpp:529-530
// `IsDeviceTypeOnEndpoint(target.deviceType, requestPath.endpoint)`;
// ep may be nil (unknown endpoint), which conservatively fails any
// device-type-restricted target.
func aclTargetMatches(targets []store.ACLTarget, ep *Endpoint, endpoint uint16, clusterID uint32) bool {
	if len(targets) == 0 {
		return true
	}
	for _, t := range targets {
		if t.Cluster != nil && *t.Cluster != clusterID {
			continue
		}
		if t.Endpoint != nil && *t.Endpoint != endpoint {
			continue
		}
		if t.DeviceType != nil && !endpointHasDeviceType(ep, *t.DeviceType) {
			continue
		}
		return true
	}
	return false
}

// privilegeSet is the set of privileges an access check granted, one bit
// per [store.Privilege] value.
type privilegeSet uint8

// grantedBy returns the privileges an entry of privilege p grants: each
// one the privileges it subsumes, as a set rather than a rank because
// ProxyView sits beside the hierarchy — it brings View, and Operate,
// Manage and Administer do not bring it (R2-P6-acl-0408; matter.js
// packages/protocol/src/interaction/FabricAccessControl.ts:148-173,
// #addGrantedPrivilege). An unknown value grants nothing.
func grantedBy(p store.Privilege) privilegeSet {
	bit := func(q store.Privilege) privilegeSet { return 1 << q }
	switch p {
	case store.PrivilegeView:
		return bit(store.PrivilegeView)
	case store.PrivilegeProxyView:
		return bit(store.PrivilegeProxyView) | bit(store.PrivilegeView)
	case store.PrivilegeOperate:
		return bit(store.PrivilegeOperate) | bit(store.PrivilegeView)
	case store.PrivilegeManage:
		return bit(store.PrivilegeManage) | bit(store.PrivilegeOperate) | bit(store.PrivilegeView)
	case store.PrivilegeAdminister:
		return bit(store.PrivilegeAdminister) | bit(store.PrivilegeManage) | bit(store.PrivilegeOperate) | bit(store.PrivilegeView)
	default:
		return 0
	}
}

// grants reports whether the set holds the required privilege. A
// requirement outside the five privileges is satisfied by nothing.
func (s privilegeSet) grants(required uint8) bool {
	return required >= uint8(store.PrivilegeView) && required <= uint8(store.PrivilegeAdminister) && s&(1<<required) != 0
}

// CurrentDataVersion implements [im.DataVersionReader]. It looks up the
// cluster server for (endpoint, clusterID) and returns its current
// DataVersion ([clusterDataVersionFor]): every cluster that exists has
// one, and 0 is an ordinary version.
//
// Mirrors matter.js packages/node/src/node/server/InteractionServer.ts
// DataVersion check before write dispatch. Returns (0, false) only when
// the cluster is not found.
func (d *TopologyDispatcher) CurrentDataVersion(_ context.Context, endpoint uint16, clusterID uint32) (uint32, bool) {
	ep := d.topology.FindByID(endpoint)
	if ep == nil {
		return 0, false
	}
	for _, srv := range ClusterServers(ep) {
		if srv.MatterClusterID() != clusterID {
			continue
		}
		return clusterDataVersionFor(ep, srv)
	}
	return 0, false
}

// isGlobalAttribute reports whether id is one of the global attributes every
// cluster carries: GeneratedCommandList (0xFFF8), AcceptedCommandList
// (0xFFF9), EventList (0xFFFA), AttributeList (0xFFFB), FeatureMap (0xFFFC),
// ClusterRevision (0xFFFD).
func isGlobalAttribute(id uint32) bool { return id >= 0xFFF8 && id <= 0xFFFD }

// appendListValue turns a list-append write (null ListIndex) into the write
// of the whole list: the attribute's current value as the writer sees it —
// fabric-filtered for a fabric-scoped list — with the written element
// appended. matter.js AttributeWriteResponse applies a ListIndex-null write
// as an append to the current list the same way. A value that is not a
// one-element list of the attribute's element type is a CONSTRAINT_ERROR.
//
// A list-append write of a fabric-scoped list appends to the accessing
// fabric's entries: on a request the dispatcher hands the server that
// fabric's entries with the new element, whatever the server's read
// returns ([writersEntries]). Mirrors matter.js, whose write context is
// always fabric-filtered (InteractionServer.ts:484-489, ListManager.ts:609-620,
// AttributeWriteResponse.ts:437-449 at db021da0).
//
// An attribute whose value is not a list — a slice other than an octet
// string — answers INVALID_ACTION. Mirrors matter.js
// packages/protocol/src/action/server/AttributeWriteResponse.ts:436-443 at
// db021da0 (`if (!(tlv instanceof ArraySchema)) throw … Status.InvalidAction`,
// inside the try of writeValue, so the path's status). The attribute's type
// is read from its current value: a nil one (a null list, or none) stays
// CONSTRAINT_ERROR.
func appendListValue(ctx context.Context, srv contract.ClusterServer, attrID uint32, value im.AttributeValue) (im.AttributeValue, im.StatusCode) {
	var current any
	var ok bool
	fr, isFR := srv.(contract.FabricScopedReader)
	if isFR {
		_, fabric := im.FabricFilterFromContext(ctx)
		current, ok = fr.MatterReadFiltered(im.WithFabricFilter(ctx, true, fabric), attrID)
	} else {
		current, ok = srv.MatterRead(attrID)
	}
	if ok && current != nil {
		if _, octets := current.([]byte); octets || reflect.ValueOf(current).Kind() != reflect.Slice {
			return value, im.StatusInvalidAction
		}
	}
	item := reflect.ValueOf(value.Value)
	if !ok || current == nil || item.Kind() != reflect.Slice || item.Len() != 1 {
		return value, im.StatusConstraintError
	}
	list := reflect.ValueOf(current)
	if list.Kind() != reflect.Slice || list.Type() != item.Type() {
		return value, im.StatusConstraintError
	}
	if scope := fabricScopeFor(ctx, srv, attrID); scope != nil {
		var status im.StatusCode
		if list, status = writersEntries(list, scope.Fabric, isFR); status != im.StatusSuccess {
			return value, status
		}
	}
	merged := reflect.MakeSlice(list.Type(), 0, list.Len()+1)
	merged = reflect.AppendSlice(merged, list)
	merged = reflect.Append(merged, item.Index(0))
	return im.AttributeValue{Value: merged.Interface()}, im.StatusSuccess
}

// writersEntries is the entries of list, the current value of a
// fabric-scoped list, that fabric owns: the list a list-append write of
// that fabric appends to. An entry's owner is the FabricIndex (field 254)
// of its own encoding — an entry that encodes itself ([spec.Encodable], as
// every generated struct and a [spec.Instance]'s value do) — judged as a
// fabric-filtered read judges it ([im.MaskFabricScoped]): an entry another
// fabric owns is left out, and one without a valid owner answers FAILURE,
// as the read of the list does. A list whose entries do not encode
// themselves is the server's fabric-filtered read when the server is a
// [contract.FabricScopedReader] (filtered, which returns the accessing
// fabric's entries alone), and answers FAILURE otherwise: the dispatcher
// cannot tell whose entry it is, and hands the server no entry it does not
// know to be the writer's. An empty list has no entry to judge.
func writersEntries(list reflect.Value, fabric uint8, filteredRead bool) (reflect.Value, im.StatusCode) {
	if list.Len() == 0 {
		return list, im.StatusSuccess
	}
	entries := make([]spec.Encodable, list.Len())
	for i := range list.Len() {
		el := list.Index(i)
		if (el.Kind() == reflect.Pointer || el.Kind() == reflect.Interface) && el.IsNil() {
			return list, im.StatusFailure
		}
		e, ok := el.Interface().(spec.Encodable)
		if !ok {
			if filteredRead {
				return list, im.StatusSuccess
			}
			return list, im.StatusFailure
		}
		entries[i] = e
	}
	scope := im.FabricScope{Fabric: fabric, Filtered: true}
	out := reflect.MakeSlice(list.Type(), 0, list.Len())
	for i, e := range entries {
		enc := tlv.NewEncoder()
		enc.StartArray(tlv.AnonymousTag())
		e.EncodeTLV(enc, tlv.AnonymousTag())
		_ = enc.EndContainer()
		encoded, err := enc.Bytes()
		if err != nil {
			return list, im.StatusFailure
		}
		masked, ok := im.MaskFabricScoped(encoded, scope)
		if !ok {
			return list, im.StatusFailure
		}
		// The array's opener and end alone: the entry is another fabric's.
		if len(masked) > 2 {
			out = reflect.Append(out, list.Index(i))
		}
	}
	return out, im.StatusSuccess
}

// stampAccessingFabric returns value with the FabricIndex of every entry
// set to fabric: a list (or a single struct) whose element is a struct, or
// a pointer to one, with a field FabricIndex of type uint8 or *uint8 —
// the name every generated struct gives field 254 (cluster/spec/*/
// definition_gen.go), and the module's hand-decoded entry types. The
// result is a copy: the request's value, which a group write hands every
// resolved location, is not changed. A value of any other shape is
// returned as it is.
func stampAccessingFabric(value any, fabric uint8) any {
	rv := reflect.ValueOf(value)
	if !rv.IsValid() {
		return value
	}
	switch rv.Kind() {
	case reflect.Slice:
		if _, octets := value.([]byte); octets || !stampable(rv.Type().Elem()) {
			return value
		}
		out := reflect.MakeSlice(rv.Type(), rv.Len(), rv.Len())
		for i := range rv.Len() {
			out.Index(i).Set(stampedCopy(rv.Index(i), fabric))
		}
		return out.Interface()
	case reflect.Struct, reflect.Pointer:
		if !stampable(rv.Type()) {
			return value
		}
		return stampedCopy(rv, fabric).Interface()
	default:
		return value
	}
}

// stampable reports whether t is a struct, or a pointer to one, with a
// settable FabricIndex field of type uint8 or *uint8.
func stampable(t reflect.Type) bool {
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return false
	}
	f, ok := t.FieldByName("FabricIndex")
	if !ok || !f.IsExported() || len(f.Index) != 1 {
		return false
	}
	u8 := reflect.TypeFor[uint8]()
	return f.Type == u8 || (f.Type.Kind() == reflect.Pointer && f.Type.Elem() == u8)
}

// stampedCopy returns a copy of the struct (or of the struct behind the
// pointer) el with FabricIndex set to fabric; a nil pointer stays nil.
func stampedCopy(el reflect.Value, fabric uint8) reflect.Value {
	if el.Kind() == reflect.Pointer {
		if el.IsNil() {
			return el
		}
		cp := reflect.New(el.Type().Elem())
		cp.Elem().Set(el.Elem())
		setFabricIndex(cp.Elem(), fabric)
		return cp
	}
	cp := reflect.New(el.Type()).Elem()
	cp.Set(el)
	setFabricIndex(cp, fabric)
	return cp
}

func setFabricIndex(st reflect.Value, fabric uint8) {
	f := st.FieldByName("FabricIndex")
	if f.Kind() == reflect.Pointer {
		v := fabric
		f.Set(reflect.ValueOf(&v))
		return
	}
	f.SetUint(uint64(fabric))
}

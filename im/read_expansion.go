// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package im

import (
	"cmp"
	"context"
	"slices"
)

// EndpointExpander is the optional [Dispatcher] interface that lets an
// [AttributeExpansion] expand an attribute path naming no endpoint one
// endpoint at a time instead of in one Read: a read or a priming report
// then holds one endpoint's results while it waits for the peer, not the
// whole node's. Mirrors the unit matter.js generates a wildcard read in —
// packages/protocol/src/action/server/AttributeReadResponse.ts:312-332 at
// db021da0 (readEndpointForWildcard: "our chunk size is one endpoint worth
// of data").
type EndpointExpander interface {
	// ExpansionEndpoints returns the endpoints Read visits for path, a
	// path whose endpoint is a wildcard, in the order Read visits them.
	ExpansionEndpoints(path ConcreteAttributePath) []uint16
	// ReadAtEndpoint returns the results Read returns for path at
	// endpoint alone, with path's wildcard semantics kept: a cluster or
	// attribute the endpoint lacks is skipped, not reported. An endpoint
	// that is gone returns nothing.
	ReadAtEndpoint(ctx context.Context, path ConcreteAttributePath, endpoint uint16) []ReadResult
}

// AttributeExpansion is the cursor over the attribute paths of one read
// or subscribe request: it expands path N+1 only once every result of
// path N was taken, and a path naming no endpoint one endpoint at a time
// when the dispatcher is an [EndpointExpander]. What it holds between two
// calls of [AttributeExpansion.Next] is the deduplicated path list, where
// it is in it, and the results of the current endpoint (or, without an
// [EndpointExpander], of the current path) until the last of them was
// taken — the slice the dispatcher returned, which it reads and never
// writes, or its sorted copy under [AttributeExpansion.Ascending]. A path's
// WildcardPathFlags are applied as [ApplyWildcardPathFlags] does. When ctx
// carries this node's id ([WithLocalNodeID]), a path naming another node
// selects nothing, or, a concrete one, yields one UnsupportedNode result.
//
// Mirrors matter.js packages/protocol/src/action/server/
// AttributeReadResponse.ts at db021da0: *process (:64) registers one
// producer per request path (:88-94) and runs them in request order
// (:96-100) as a generator; a wildcard path's producer walks the node's
// endpoints (:181-185) and a chunk is yielded whenever the endpoint
// changes (:284-290, :326-332), so nothing is generated before the
// consumer pulls it.
type AttributeExpansion struct {
	ctx       context.Context
	d         Dispatcher
	expander  EndpointExpander
	paths     []ConcreteAttributePath
	nextPath  int
	ascending bool

	localNodeID    uint64
	hasLocalNodeID bool

	path         ConcreteAttributePath
	flags        uint32
	qualities    ElementQualities
	endpoints    []uint16
	nextEndpoint int
	batch        []ReadResult
	pos          int
}

// NewAttributeExpansion returns the expansion of paths against d under
// ctx. A repeat of an identical path is read once ([DedupAttributePaths]).
func NewAttributeExpansion(ctx context.Context, d Dispatcher, paths []ConcreteAttributePath) *AttributeExpansion {
	e := &AttributeExpansion{
		ctx:       ctx,
		d:         d,
		paths:     DedupAttributePaths(paths),
		qualities: elementQualitiesFrom(ctx),
	}
	e.expander, _ = d.(EndpointExpander)
	e.localNodeID, e.hasLocalNodeID = LocalNodeIDFromContext(ctx)
	return e
}

// Ascending makes the expansion visit each path's results in ascending
// (endpoint, cluster, attribute) order — the endpoints of a wildcard path
// in ascending id order, each endpoint's results sorted — instead of the
// dispatcher's order. The paths themselves keep the request's order. Call
// it before the first [AttributeExpansion.Next].
func (e *AttributeExpansion) Ascending() *AttributeExpansion {
	e.ascending = true
	return e
}

// Next returns the next result and the request path it was expanded
// from; ok is false once every path is expanded.
func (e *AttributeExpansion) Next() (requested ConcreteAttributePath, result ReadResult, ok bool) {
	for {
		for e.pos < len(e.batch) {
			// The batch stays the dispatcher's: nothing is written into
			// it (review II, R2-P2-dr-req-04).
			r := e.batch[e.pos]
			e.pos++
			if e.flags != 0 && wildcardPathSkipped(r.Path, e.flags, e.qualities) {
				continue
			}
			return e.path, r, true
		}
		e.batch, e.pos = nil, 0
		if !e.fill() {
			return ConcreteAttributePath{}, ReadResult{}, false
		}
	}
}

// fill loads the next batch: the current path's next endpoint, else the
// next path. It returns false once the paths are exhausted.
func (e *AttributeExpansion) fill() bool {
	for {
		if e.nextEndpoint < len(e.endpoints) {
			ep := e.endpoints[e.nextEndpoint]
			e.nextEndpoint++
			e.setBatch(e.expander.ReadAtEndpoint(e.ctx, e.path, ep))
			return true
		}
		e.endpoints, e.nextEndpoint = nil, 0
		if e.nextPath >= len(e.paths) {
			return false
		}
		e.path = e.paths[e.nextPath]
		e.nextPath++
		if e.path.HasNode && e.hasLocalNodeID && e.path.Node != e.localNodeID {
			// A path naming another node: a concrete one is answered
			// UnsupportedNode before any access check, a wildcard one
			// selects nothing here. Mirrors matter.js
			// AttributeReadResponse.ts:205-208 (addConcrete) and
			// :160-162 (addWildcard) at db021da0.
			if e.path.HasEndpoint && e.path.HasCluster && e.path.HasAttribute {
				e.flags = 0
				e.setBatch([]ReadResult{{Path: e.path, Status: StatusUnsupportedNode}})
				return true
			}
			continue
		}
		e.flags = effectiveWildcardPathFlags(e.ctx, e.d, e.path)
		if !e.path.HasEndpoint && e.expander != nil {
			// No endpoint to visit: Read answers the path whole (an
			// empty node's UnsupportedEndpoint).
			if eps := e.expander.ExpansionEndpoints(e.path); len(eps) > 0 {
				if e.ascending {
					eps = slices.Clone(eps)
					slices.Sort(eps)
				}
				e.endpoints = eps
				continue
			}
		}
		e.setBatch(e.d.Read(e.ctx, e.path))
		return true
	}
}

// setBatch makes results the batch Next hands out. Under Ascending an
// unsorted batch is sorted on a copy: the slice a [Dispatcher] or
// [EndpointExpander] returned is not the expansion's to write (review II,
// R2-P2-dr-req-04).
func (e *AttributeExpansion) setBatch(results []ReadResult) {
	if e.ascending && !slices.IsSortedFunc(results, compareReadResultPaths) {
		results = slices.Clone(results)
		slices.SortStableFunc(results, compareReadResultPaths)
	}
	e.batch, e.pos = results, 0
}

// compareReadResultPaths orders results by (endpoint, cluster, attribute).
func compareReadResultPaths(a, b ReadResult) int {
	if c := cmp.Compare(a.Path.Endpoint, b.Path.Endpoint); c != 0 {
		return c
	}
	if c := cmp.Compare(a.Path.Cluster, b.Path.Cluster); c != 0 {
		return c
	}
	return cmp.Compare(a.Path.Attribute, b.Path.Attribute)
}

// ReadReports is the attribute half of a Read's response, generated one
// report at a time: an [AttributeExpansion] of the request's paths with
// the per-result gates [HandleReadRequest] applies — the ACL check at
// each resolved location (a denied concrete path answers
// UnsupportedAccess, a denied wildcard result is left out) and the
// DataVersion filter. [HandleReadRequest] is its drained form.
type ReadReports struct {
	exp *AttributeExpansion
	ctx context.Context

	filters       []DataVersionFilter
	fabricIndex   uint8
	subjectNodeID uint64
	subjectCATs   []uint32
	pase          bool
	aclChecker    ACLChecker
	hasACL        bool
	privProvider  AttributeReadPrivilegeProvider
	hasPriv       bool
}

// NewReadReports returns the reports of req's attribute paths against d
// under ctx, which carries the fabric filter, the subject and the auth
// mode as for [HandleReadRequest].
func NewReadReports(ctx context.Context, d Dispatcher, req ReadRequest) *ReadReports {
	r := &ReadReports{
		exp:     NewAttributeExpansion(ctx, d, req.AttributeRequests),
		ctx:     ctx,
		filters: dataVersionFiltersForContext(ctx, req.DataVersionFilters),
		// A PASE session keeps its implicit Administer grant after AddNOC
		// adopted it onto the new fabric, so the gate is keyed on the auth
		// mode, not on the fabric: fabric 0 without PASE is checked, and
		// refused.
		pase: IsPASEFromContext(ctx),
	}
	_, r.fabricIndex = FabricFilterFromContext(ctx)
	r.subjectNodeID, r.subjectCATs = SubjectFromContext(ctx)
	r.aclChecker, r.hasACL = d.(ACLChecker)
	r.privProvider, r.hasPriv = d.(AttributeReadPrivilegeProvider)
	return r
}

// readPrivilege returns the minimum privilege needed to read the given
// (endpoint, cluster, attribute) triplet. Falls back to View (1) when no
// AttributeReadPrivilegeProvider is wired or the attribute has no
// elevated requirement.
func (r *ReadReports) readPrivilege(endpoint uint16, clusterID, attrID uint32) uint8 {
	if r.hasPriv {
		return r.privProvider.MinReadPrivilege(endpoint, clusterID, attrID)
	}
	return 1
}

// Next returns the next attribute report; ok is false once the request's
// paths are exhausted.
func (r *ReadReports) Next() (report AttributeReport, ok bool) {
	for {
		path, res, more := r.exp.Next()
		if !more {
			return AttributeReport{}, false
		}
		if rep, keep := r.report(path, res); keep {
			return rep, true
		}
	}
}

// report applies the per-result gates to res, expanded from path; keep is
// false when the result is left out.
func (r *ReadReports) report(path ConcreteAttributePath, res ReadResult) (rep AttributeReport, keep bool) {
	// A fully-concrete path (endpoint + cluster + attribute all named)
	// returns an explicit AttributeStatusIB(UnsupportedAccess) on ACL
	// denial; a wildcard path silently omits the results the subject may
	// not read (Matter §8.4.3.2 — a wildcard read discloses only
	// authorized paths). Mirrors matter.js AttributeReadResponse.ts
	// addConcrete (error status) vs readAttributeForWildcard (omit).
	concretePath := path.HasEndpoint && path.HasCluster && path.HasAttribute

	// A path naming another node is refused before any access check
	// (AttributeReadResponse.ts:205-208); only the expansion answers
	// UnsupportedNode, no dispatcher does.
	if res.Status == StatusUnsupportedNode {
		return AttributeReport{Path: res.Path, IsStatus: true, Status: StatusIB{Status: res.Status}}, true
	}

	// Per-RESULT ACL check at the RESOLVED concrete (endpoint, cluster)
	// using the resolved attribute's read privilege — run for EVERY
	// expanded result, including a wildcard-endpoint + concrete-cluster
	// read (previously authorized only against endpoint 0, then expanded
	// to every endpoint with no per-endpoint recheck). Mirrors matter.js
	// packages/protocol/src/action/server/AttributeReadResponse.ts:
	// 238-274 — each resolved location is authorized with the resolved
	// attribute's readLevel AFTER path resolution. PASE bypasses —
	// commissioning reads must succeed pre-AddNOC, and after it while the
	// commissioner still drives the same PASE channel.
	// No fabric and not PASE fails closed (checkSubjectACL).
	if r.hasACL && !r.pase {
		priv := r.readPrivilege(res.Path.Endpoint, res.Path.Cluster, res.Path.Attribute)
		if status := checkSubjectACL(r.ctx, r.aclChecker, r.fabricIndex, r.subjectNodeID, r.subjectCATs, res.Path.Endpoint, res.Path.Cluster, priv); !status.IsSuccess() {
			if concretePath {
				return AttributeReport{
					Path:     res.Path,
					IsStatus: true,
					Status:   StatusIB{Status: status},
				}, true
			}
			// Wildcard-expanded denial: omit, never disclose.
			return AttributeReport{}, false
		}
	}

	// Check if the controller's cached DataVersion matches the cluster's
	// current DataVersion — if so, skip this cluster. Only a result that
	// has a version is compared (ReadResult.HasDataVersion); every value,
	// 0 and 1 included, is an ordinary version (review II, decision 46).
	if len(r.filters) > 0 && res.Status == StatusSuccess && res.HasDataVersion {
		if cached, ok := matchDataVersionFilter(r.filters, res.Path.Endpoint, res.Path.Cluster); ok {
			if cached == res.DataVersion {
				// Controller cache is fresh — omit this cluster.
				return AttributeReport{}, false
			}
		}
	}
	rep = AttributeReport{Path: res.Path, DataVersion: res.DataVersion, HasDataVersion: res.HasDataVersion}
	if res.Status != StatusSuccess {
		rep.IsStatus = true
		rep.Status = StatusIB{Status: res.Status}
	} else {
		rep.Value = res.Value
	}
	return rep, true
}

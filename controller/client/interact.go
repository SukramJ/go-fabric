// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package client

import (
	"context"
	"slices"
	"sync"

	"github.com/SukramJ/go-fabric/client/interaction"
	"github.com/SukramJ/go-fabric/client/peer"
	"github.com/SukramJ/go-fabric/cluster/spec"
)

// ReadRequest is a read through the node — matter.js ClientRead
// (packages/protocol/src/action/client/ClientRead.ts:12 at 0ceb202d): the
// paths and filters it sends ([ReadPaths], embedded as ReadRequest, its
// fields promoted) and IncludeKnownVersions.
type ReadRequest struct {
	interaction.ReadRequest
	// IncludeKnownVersions skips the version filters, so the peer sends
	// every value (ClientRead.ts:12).
	IncludeKnownVersions bool
}

// SubscribeRequest is a subscription through the node — matter.js
// ClientSubscribe with ClientNodeInteraction's includeKnownVersions
// (packages/node/src/node/client/ClientNodeInteraction.ts:111-124 at
// 862ad75f): what it sends ([SubscribePaths], embedded, its fields
// promoted) and IncludeKnownVersions.
type SubscribeRequest struct {
	interaction.SubscribeRequest
	// IncludeKnownVersions skips the version filters — in the first
	// request and, for a sustained subscription, in every later one it
	// sends — so the peer sends every value (:123-124).
	IncludeKnownVersions bool
}

// Read reads from the peer and applies the report to the image —
// ClientNodeInteraction.read (ClientNodeInteraction.ts:91-112): unless
// IncludeKnownVersions, and when the read has the default subscription's
// fabric filter (:103-106), the cached versions go along as filters, none
// for a cluster with a changes-omitted attribute (:107) and none for a
// cluster the caller filters ([structure.withKnownVersions]). The caller
// gets the report as the peer sent it, its chunks merged.
//
// A host's read returns its events in the Report only: they reach neither
// Hooks.Event nor the persisted event cursor — matter.js
// ClientNodeInteraction.ts:97-98 passes the request's own emitEvents,
// which a plain read leaves unset (ClientStructure.ts:412).
func (n *Node) Read(ctx context.Context, req ReadRequest) (*Report, error) {
	return n.read(ctx, req, false)
}

// read is Read with mutate's emitEvents (ClientStructure.ts:412).
func (n *Node) read(ctx context.Context, req ReadRequest, emitEvents bool) (*Report, error) {
	r := req.ReadRequest
	n.routeICD(&r.ClientRequest)
	sc := newScope(r.Attributes, r.NoFabricFilter)
	n.mu.RLock()
	if !req.IncludeKnownVersions {
		r.DataVersionFilters = n.s.withKnownVersions(sc, r.DataVersionFilters, true)
	}
	n.mu.RUnlock()
	ia, err := n.interaction()
	if err != nil {
		return nil, err
	}
	if err := n.holdICD(ctx); err != nil {
		return nil, err
	}
	rep, err := ia.ReadWith(ctx, r)
	if err != nil {
		return nil, err
	}
	n.noteICDActive()
	n.apply(sc, rep, emitEvents)
	return rep, nil
}

// routeICD routes a one-shot interaction with an await-mode LIT ICD peer
// through the unthrottled "icdLit" network profile, so an interaction a
// wake releases is not queued behind bulk traffic; a non-LIT peer, a node
// without an ICD client, and a caller that named a network keep theirs.
// The routing is applied to the caller's copy, so a reused request never
// caches it across a SIT⇄LIT flip. Mirrors matter.js
// ClientNodeInteraction.#awaitModeIcdRouting
// (packages/node/src/node/client/ClientNodeInteraction.ts:300-318 at
// 862ad75f), applied at :78-81 (read) and :188-191 (write) — and to invoke
// the same way.
func (n *Node) routeICD(r *interaction.ClientRequest) {
	if n.icd == nil || r.Network != "" {
		return
	}
	if w := n.icd.Wakefulness(); w != nil && w.RequiresAwait() {
		r.Network = peer.NetworkICDLit
	}
}

// holdICD holds an interaction with a sleeping LIT ICD peer until it wakes
// — matter.js ClientNodeInteraction.ts:87, :194, :219 (#holdUntilAwake,
// :333-346); a non-LIT or awake peer, and a node without an ICD client,
// pass at once.
func (n *Node) holdICD(ctx context.Context) error {
	if n.icd == nil {
		return nil
	}
	return n.icd.hold(ctx)
}

// noteICDActive records the peer's answer (NodeSession.ts:278-283).
func (n *Node) noteICDActive() {
	if n.icd != nil {
		n.icd.noteActive()
	}
}

// Subscribe subscribes through the node — ClientNodeInteraction.subscribe
// (ClientNodeInteraction.ts:125-169 at 0ceb202d): the cached versions go
// along as filters (:128, no changes-omitted refresh), as for a read none
// for a cluster the caller filters and none with the other fabric filter
// than the cache's ([structure.withKnownVersions]); the interval bounds
// are the node's for the request's (:129-133); each report is applied to
// the image before req.Updated sees it (:139-146); a sustained
// subscription's re-subscriptions send the versions then current and the
// event minimum past the highest event seen (:148-155).
//
// A sustained subscription's re-subscription starts from the caller's own
// filters, runs the caller's RefreshRequest on them, then injects the
// versions, and its reports are applied by the scope of the request that
// produced them. Mirrors matter.js
// packages/node/src/node/client/ClientNodeInteraction.ts:119-181 at
// 862ad75f (callerRefresh :125, sent :129-132, refreshRequest :162-168).
// With IncludeKnownVersions no version goes along, in the first request
// nor in a re-subscription's (withKnownVersions, :123-124).
//
// A host's subscription returns its events in its reports only (see Read);
// only the default subscription emits them.
func (n *Node) Subscribe(ctx context.Context, sr SubscribeRequest) (*Subscription, error) {
	return n.subscribe(ctx, sr, nil)
}

// subscribe is Subscribe with mutate's emitEvents, evaluated per report
// (NetworkClient.ts:171, emitEvents: isCurrent); nil emits nothing.
func (n *Node) subscribe(ctx context.Context, sr SubscribeRequest, emitEvents func() bool) (*Subscription, error) {
	req := sr.SubscribeRequest
	withKnownVersions := func(sc scope, filters []DataVersionFilter) []DataVersionFilter {
		if sr.IncludeKnownVersions {
			return filters
		}
		return n.s.withKnownVersions(sc, filters, false)
	}
	callerFilters := slices.Clone(req.DataVersionFilters)
	var callerRefresh func(SubscribePaths) SubscribePaths
	if req.Sustain {
		callerRefresh = req.RefreshRequest
	}
	// sentMu guards sent, the scope the next report is applied by, which a
	// re-subscription's RefreshRequest replaces.
	var sentMu sync.Mutex
	sent := newScope(req.Attributes, req.NoFabricFilter)
	r := req
	n.mu.RLock()
	r.DataVersionFilters = withKnownVersions(sent, callerFilters)
	n.mu.RUnlock()
	floor, ceiling := n.IntervalBounds(req.MinIntervalFloor, req.MaxIntervalCeiling)
	r.MinIntervalFloor, r.MaxIntervalCeiling = &floor, &ceiling
	r.Updated = func(rep *Report) {
		sentMu.Lock()
		sc := sent
		sentMu.Unlock()
		n.apply(sc, rep, emitEvents != nil && emitEvents())
		if req.Updated != nil {
			req.Updated(rep)
		}
	}
	r.RefreshRequest = func(x SubscribePaths) SubscribePaths {
		x.DataVersionFilters = slices.Clone(callerFilters)
		if callerRefresh != nil {
			x = callerRefresh(x)
		}
		sc := newScope(x.Attributes, x.NoFabricFilter)
		n.mu.RLock()
		x.EventFilters = []EventMinimumNumber{{EventMin: n.settings.maxEventNumber + 1}}
		x.DataVersionFilters = withKnownVersions(sc, x.DataVersionFilters)
		n.mu.RUnlock()
		sentMu.Lock()
		sent = sc
		sentMu.Unlock()
		n.observeRequest(x)
		return x
	}
	ia, err := n.interaction()
	if err != nil {
		return nil, err
	}
	n.observeRequest(r)
	return ia.Subscribe(ctx, r)
}

// observeRequest hands a subscription request to the test seam.
func (n *Node) observeRequest(r SubscribePaths) {
	n.mu.RLock()
	f := n.requestObserver
	n.mu.RUnlock()
	if f != nil {
		f(r)
	}
}

// Read, Subscribe, Write and Invoke fail with [ErrNotCommissioned] on a
// node a [Registry] holds before it is commissioned: it has no peer yet.

// Write writes to the peer — ClientNodeInteraction.write
// (ClientNodeInteraction.ts:175-186). The image changes when the peer
// reports the new value, not through the write (matter.js's RemoteWriter
// sets the cache and compensates a refusal).
func (n *Node) Write(ctx context.Context, writes []WriteRequest, opts WriteOptions) (WriteStatuses, error) {
	ia, err := n.interaction()
	if err != nil {
		return nil, err
	}
	if err := n.holdICD(ctx); err != nil {
		return nil, err
	}
	n.routeICD(&opts.ClientRequest)
	st, err := ia.Write(ctx, writes, opts)
	if err == nil {
		n.noteICDActive()
	}
	return st, err
}

// Invoke invokes a command on the peer — ClientNodeInteraction.invoke
// (ClientNodeInteraction.ts:197-210).
func (n *Node) Invoke(ctx context.Context, path CommandPath, fields spec.Encodable, opts InvokeOptions) (Result, error) {
	ia, err := n.interaction()
	if err != nil {
		return Result{}, err
	}
	if err := n.holdICD(ctx); err != nil {
		return Result{}, err
	}
	n.routeICD(&opts.ClientRequest)
	res, err := ia.Invoke(ctx, path, fields, opts)
	if err == nil {
		n.noteICDActive()
	}
	return res, err
}

// InvokeBatch invokes several commands on the peer and returns one
// [Result] per command, in their order, a command's own status in its
// Result (NoCommandResponse for one the peer did not answer). The commands
// go in batches of the peer's [Node.MaxPathsPerInvoke], each batch on an
// exchange of its own, in parallel — matter.js ClientInteraction.invoke
// with #invokeWithSplitting (packages/protocol/src/action/client/ClientInteraction.ts:654-737
// at 862ad75f) behind ClientNodeInteraction.invoke
// (packages/node/src/node/client/ClientNodeInteraction.ts). A batch's
// exchange failure is the error; the Results of the other batches stand.
func (n *Node) InvokeBatch(ctx context.Context, cmds []CommandRequest, opts InvokeOptions) ([]Result, error) {
	ia, err := n.interaction()
	if err != nil {
		return nil, err
	}
	if err := n.holdICD(ctx); err != nil {
		return nil, err
	}
	n.routeICD(&opts.ClientRequest)
	res, err := ia.InvokeBatch(ctx, cmds, n.MaxPathsPerInvoke(), opts)
	if err == nil {
		n.noteICDActive()
	}
	return res, err
}

// MaxPathsPerInvoke is the number of commands the peer takes in one
// InvokeRequest: the MaxPathsPerInvoke of its session parameters, 1 when
// it reported none or the node has no peer — matter.js
// PeerExchangeProvider.maxPathsPerInvoke (packages/protocol/src/peer/PeerExchangeProvider.ts:30-32
// at 862ad75f) and ClientInteraction's fallback of 1 (ClientInteraction.ts:721).
func (n *Node) MaxPathsPerInvoke() int {
	p := n.peer()
	if p == nil {
		return 1
	}
	return max(int(p.SessionParameters().MaxPathsPerInvoke), 1)
}

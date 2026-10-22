// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package client

import (
	"context"
	"slices"
	"sync"

	"github.com/SukramJ/go-fabric/client/interaction"
	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/im"
)

// ReadRequest is a read through the node — matter.js ClientRead
// (packages/protocol/src/action/client/ClientRead.ts:12 at 0ceb202d).
type ReadRequest struct {
	interaction.ReadRequest
	// IncludeKnownVersions skips the version filters, so the peer sends
	// every value (ClientRead.ts:12).
	IncludeKnownVersions bool
}

// Read reads from the peer and applies the report to the image —
// ClientNodeInteraction.read (ClientNodeInteraction.ts:91-112): unless
// IncludeKnownVersions, and when the read has the default subscription's
// fabric filter (:103-106), the cached versions go along as filters, none
// for a cluster with a changes-omitted attribute (:107) and none for a
// cluster the caller filters ([structure.withKnownVersions]). The caller
// gets the report as the peer sent it, its chunks merged.
func (n *Node) Read(ctx context.Context, req ReadRequest) (*interaction.Report, error) {
	r := req.ReadRequest
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
	rep, err := ia.ReadWith(ctx, r)
	if err != nil {
		return nil, err
	}
	n.apply(sc, rep)
	return rep, nil
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
func (n *Node) Subscribe(ctx context.Context, req interaction.SubscribeRequest) (*interaction.Subscription, error) {
	callerFilters := slices.Clone(req.DataVersionFilters)
	var callerRefresh func(interaction.SubscribeRequest) interaction.SubscribeRequest
	if req.Sustain {
		callerRefresh = req.RefreshRequest
	}
	// sentMu guards sent, the scope the next report is applied by, which a
	// re-subscription's RefreshRequest replaces.
	var sentMu sync.Mutex
	sent := newScope(req.Attributes, req.NoFabricFilter)
	r := req
	n.mu.RLock()
	r.DataVersionFilters = n.s.withKnownVersions(sent, callerFilters, false)
	n.mu.RUnlock()
	floor, ceiling := n.IntervalBounds(req.MinIntervalFloor, req.MaxIntervalCeiling)
	r.MinIntervalFloor, r.MaxIntervalCeiling = &floor, &ceiling
	r.Updated = func(rep *interaction.Report) {
		sentMu.Lock()
		sc := sent
		sentMu.Unlock()
		n.apply(sc, rep)
		if req.Updated != nil {
			req.Updated(rep)
		}
	}
	r.RefreshRequest = func(x interaction.SubscribeRequest) interaction.SubscribeRequest {
		x.DataVersionFilters = slices.Clone(callerFilters)
		if callerRefresh != nil {
			x = callerRefresh(x)
		}
		sc := newScope(x.Attributes, x.NoFabricFilter)
		n.mu.RLock()
		x.EventFilters = []im.EventMinimumNumber{{EventMin: n.settings.maxEventNumber + 1}}
		x.DataVersionFilters = n.s.withKnownVersions(sc, x.DataVersionFilters, false)
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
func (n *Node) observeRequest(r interaction.SubscribeRequest) {
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
func (n *Node) Write(ctx context.Context, writes []interaction.WriteRequest, opts interaction.WriteOptions) (interaction.WriteStatuses, error) {
	ia, err := n.interaction()
	if err != nil {
		return nil, err
	}
	return ia.Write(ctx, writes, opts)
}

// Invoke invokes a command on the peer — ClientNodeInteraction.invoke
// (ClientNodeInteraction.ts:197-210).
func (n *Node) Invoke(ctx context.Context, path im.ConcreteCommandPath, fields spec.Encodable, opts interaction.InvokeOptions) (interaction.Result, error) {
	ia, err := n.interaction()
	if err != nil {
		return interaction.Result{}, err
	}
	return ia.Invoke(ctx, path, fields, opts)
}

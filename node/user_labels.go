// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

import (
	"context"
	"encoding/json"
	"log/slog"
	"slices"
	"sync"

	"github.com/SukramJ/go-fabric/cluster/spec"
	userlabeldef "github.com/SukramJ/go-fabric/cluster/spec/userlabel"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/endpoint"
)

// bridgedUserLabelKeyRoot prefixes the settings key a bridged endpoint's
// UserLabel list is kept under, after its stable key — as the bridged
// NodeLabel's (endpointLabelKeyRoot). The root's and the Aggregator's lists
// keep their own keys (rootUserLabelsKey, aggregatorUserLabelsKey).
const bridgedUserLabelKeyRoot = "bridged_user_label."

// userLabelSync tracks the UserLabel servers of bridged endpoints whose
// list the store keeps (persistUserLabels).
type userLabelSync struct {
	// mu guards persisted: each server's change unsubscribe.
	mu        sync.Mutex
	persisted map[contract.ClusterServer]func()
}

// persistUserLabels restores each UserLabel server of a bridged endpoint
// the node has not met before from the store and saves its later changes,
// under the endpoint's stable key — before the bridge serves the
// topology. LabelList is nonvolatile (quality N, matter.js
// packages/model/src/standard/elements/user-label.element.ts:21-22 at 862ad75f),
// as the root's list is (buildLabels). A server met again in a reassembly
// keeps its subscription; one that left the topology is released. An
// endpoint without a stable key keeps its list in memory only.
func (n *Node) persistUserLabels(ctx context.Context, t *endpoint.Topology) {
	n.userLabels.mu.Lock()
	defer n.userLabels.mu.Unlock()
	current := map[contract.ClusterServer]func(){}
	for _, ep := range t.Endpoints {
		if ep == nil {
			continue
		}
		k, ok := keyString(ep.SourceKey)
		if !ok {
			continue
		}
		for _, srv := range endpoint.ClusterServers(ep) {
			if srv.MatterClusterID() != userlabeldef.ClusterID {
				continue
			}
			if unsub, met := n.userLabels.persisted[srv]; met {
				current[srv] = unsub
				continue
			}
			current[srv] = n.persisted.userLabel(context.WithoutCancel(ctx), bridgedUserLabelKeyRoot+k, srv)
		}
	}
	for srv, unsub := range n.userLabels.persisted {
		if _, kept := current[srv]; !kept {
			unsub()
		}
	}
	n.userLabels.persisted = current
}

// userLabel restores srv's LabelList from key and saves each later change
// of it there; it returns the function that stops the saving.
func (p persisted) userLabel(ctx context.Context, key string, srv contract.ClusterServer) (unsubscribe func()) {
	if v, ok, err := p.st.GetSetting(ctx, key); err == nil && ok {
		var labels []userlabeldef.LabelStruct
		if json.Unmarshal([]byte(v), &labels) == nil {
			if setter, isSetter := srv.(spec.AttributeSetter); isSetter {
				if err := setter.SetAttributes(map[uint32]any{userlabeldef.AttrLabelList: spec.List[userlabeldef.LabelStruct](labels)}); err != nil {
					p.logger.Warn("label.restore", slog.String("key", key), slog.Any("err", err))
				}
			}
		}
	}
	return spec.ChangesOf(srv, func(c contract.Change) {
		if !slices.Contains(c.Attributes, userlabeldef.AttrLabelList) {
			return
		}
		b, err := json.Marshal(userlabeldef.Wrap(srv).Get().LabelList)
		if err == nil {
			err = p.st.SetSetting(ctx, key, string(b))
		}
		if err != nil {
			p.logger.Warn("label.persist", slog.String("key", key), slog.Any("err", err))
		}
	})
}

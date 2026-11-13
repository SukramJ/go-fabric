// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package client

import (
	"context"

	"github.com/SukramJ/go-fabric/client/interaction"
)

// The seams of the cache's fabric filter ports (ClientSubscribeTest.ts
// "cache fabric filter" at 862ad75f).

// SettingsClusterID and SettingCacheFabricFiltered locate the recorded
// fabric filter in a [Store]: matter.js's `nodes.<id>.cache` record.
const (
	SettingsClusterID          = settingsCluster
	SettingCacheFabricFiltered = settingCacheFabricFiltered
)

// RecordedCacheFabricFiltered is the recorded fabric filter of the cache,
// nil before one is recorded — ClientNodeStore.cacheFabricFiltered.
func RecordedCacheFabricFiltered(n *Node) *bool {
	n.mu.RLock()
	defer n.mu.RUnlock()
	if f := n.s.recordedFabricFiltered; f != nil {
		v := *f
		return &v
	}
	return nil
}

// RecordCacheFabricFiltered records the cache's fabric filter as it is,
// forgetting nothing — ClientNodeStore.recordCacheFabricFiltered.
func RecordCacheFabricFiltered(n *Node, filtered bool) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.recordCacheFabricFilteredLocked(filtered)
}

// AdoptFabricFilter is ClientStructure.adoptFabricFilter.
func AdoptFabricFilter(n *Node, filtered bool) error {
	_, err := n.adoptFabricFilter(context.Background(), filtered)
	return err
}

// InvalidateVersion forgets one cluster's version in memory only, the
// stored one kept — DatasourceCache.invalidateVersion.
func InvalidateVersion(n *Node, ep uint16, cluster uint32) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if e := n.s.endpoints[ep]; e != nil {
		if c := e.clusters[cluster]; c != nil && c.cache != nil {
			c.cache.invalidateVersion()
		}
	}
}

// InterceptSubscribe runs each subscription request of the node through f
// in place of the interaction's Subscribe, which subscribe performs —
// ClientSubscribeTest's interceptSubscribe; nil ends it.
func InterceptSubscribe(n *Node, f func(r SubscribePaths, subscribe func() (*Subscription, error)) (*Subscription, error)) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.subscribeInterceptor = f
}

// DefaultSubscriptionOf is the node's default subscription, nil without
// one — internalsOf(NetworkClient).activeSubscription.
func DefaultSubscriptionOf(n *Node) *Subscription {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.sub
}

// DefaultSubscriptionOwned reports whether a default subscription is held
// or being established — internalsOf(NetworkClient).subscriptionOwner.
func DefaultSubscriptionOwned(n *Node) bool {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.subOwner != nil
}

// apply is applyIn in the current epoch: the internal tests feed the image
// a report as matter.js's clientStructureOf(peer).mutate does.
func (n *Node) apply(sc scope, r *Report, emitEvents bool) {
	n.applyIn(sc, r, emitEvents, nil)
}

// KnownReadVersions are the version filters a read with req's paths and
// fabric filter sends: KnownVersions refreshing the clusters with a
// changes-omitted attribute (injectVersionFilters' refreshChangesOmitted).
func KnownReadVersions(n *Node, req interaction.ReadRequest) []DataVersionFilter {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.s.withKnownVersions(newScope(req.Attributes, req.NoFabricFilter), req.DataVersionFilters, true)
}

// ICDClusterID locates the ICD client's stored registration in a [Store].
const ICDClusterID = icdCluster

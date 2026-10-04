// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/im/subscription"
)

// SubscriptionStore is the durable home of the subscriptions of the
// bridge's CASE sessions, so they can be re-established under their old
// SubscriptionId after a restart (docs/adr/0008). It mirrors the persisted
// `subscriptions` state of matter.js SubscriptionsServer
// (packages/node/src/behavior/system/subscriptions/SubscriptionsServer.ts).
//
// The payload is the bridge's encoding of one subscription
// ([subscription.MarshalPeerSubscription]) and is opaque to the store; the
// key columns are there for deletion. *store.Store implements it.
// Implementations must be safe for concurrent use.
type SubscriptionStore interface {
	// SaveServerSubscription inserts or replaces the row of subscriptionID.
	SaveServerSubscription(ctx context.Context, subscriptionID uint32, fabricIndex uint8, peerNodeID uint64, payload []byte) error
	// DeleteServerSubscription removes one row; a missing row is no error.
	DeleteServerSubscription(ctx context.Context, subscriptionID uint32) error
	// DeleteServerSubscriptionsByFabric removes every row of a fabric.
	DeleteServerSubscriptionsByFabric(ctx context.Context, fabricIndex uint8) error
	// LoadServerSubscriptions returns every row's payload.
	LoadServerSubscriptions(ctx context.Context) ([][]byte, error)
	// ClearServerSubscriptions removes every row.
	ClearServerSubscriptions(ctx context.Context) error
}

// noopSubscriptionStore is the default: nothing is persisted, nothing is
// loaded.
type noopSubscriptionStore struct{}

func (noopSubscriptionStore) SaveServerSubscription(context.Context, uint32, uint8, uint64, []byte) error {
	return nil
}
func (noopSubscriptionStore) DeleteServerSubscription(context.Context, uint32) error { return nil }
func (noopSubscriptionStore) DeleteServerSubscriptionsByFabric(context.Context, uint8) error {
	return nil
}

func (noopSubscriptionStore) LoadServerSubscriptions(context.Context) ([][]byte, error) {
	return nil, nil
}
func (noopSubscriptionStore) ClearServerSubscriptions(context.Context) error { return nil }

// subscriptionStoreTimeout bounds each store call. The calls run on
// subscribe / close paths that must not stall behind a wedged database.
const subscriptionStoreTimeout = 5 * time.Second

// resumptionState is the bridge's share of subscription persistence and
// re-establishment. Its own mutex keeps it off b.mu: the manager's close
// hooks reach in here from inside bridge calls that may hold b.mu.
type resumptionState struct {
	// mu guards every field below and serialises store writes, so a
	// subscription's save and its termination's delete cannot cross.
	mu       sync.Mutex
	store    SubscriptionStore
	disabled bool
	// former holds the subscriptions of the previous run, loaded at
	// Start, until ReestablishFormerSubscriptions takes them.
	former []subscription.PeerSubscription

	caseInitiator CaseInitiatorProvider
	resolver      OperationalResolver
	// block is non-nil while ReestablishFormerSubscriptions runs.
	block *peerBlockList

	// initiated routes Secure-Channel replies on exchanges this node
	// opened as CASE initiator: exchange id → *initiatedExchange.
	initiated sync.Map
}

func (r *resumptionState) storeLocked() SubscriptionStore {
	if r.store == nil {
		return noopSubscriptionStore{}
	}
	return r.store
}

// AttachSubscriptionStore wires where the subscriptions of CASE sessions
// are persisted. *store.Store satisfies [SubscriptionStore]. Attach before
// [Bridge.Start]: Start loads the previous run's subscriptions from it.
// Pass nil to revert to the noop store, which persists nothing — silently:
// a restart then drops every subscription and each controller waits out
// its own liveness timeout before re-subscribing.
func (b *Bridge) AttachSubscriptionStore(s SubscriptionStore) {
	b.resumption.mu.Lock()
	b.resumption.store = s
	b.resumption.mu.Unlock()
}

// SetSubscriptionPersistence switches subscription persistence and the
// re-establishment of former subscriptions on or off. On by default, as
// matter.js SubscriptionsServer `persistenceEnabled`. Off, nothing is
// recorded, nothing is loaded at Start and
// [Bridge.ReestablishFormerSubscriptions] does nothing.
func (b *Bridge) SetSubscriptionPersistence(enabled bool) {
	b.resumption.mu.Lock()
	b.resumption.disabled = !enabled
	if !enabled {
		b.resumption.former = nil
	}
	b.resumption.mu.Unlock()
}

// SubscriptionPersistenceEnabled reports the switch
// [Bridge.SetSubscriptionPersistence] sets.
func (b *Bridge) SubscriptionPersistenceEnabled() bool {
	b.resumption.mu.Lock()
	defer b.resumption.mu.Unlock()
	return !b.resumption.disabled
}

// beginSubscriptionRun makes the stored subscriptions the former ones and
// starts this run's record from empty. Mirrors matter.js
// SubscriptionsServer.beginRun, which ServerNetworkRuntime calls each time
// the network starts, before any subscription can be established. A former
// subscription is therefore written back only once it is active again —
// one that fails to re-establish is dropped, exactly as in matter.js.
func (b *Bridge) beginSubscriptionRun() {
	b.resumption.mu.Lock()
	defer b.resumption.mu.Unlock()
	b.resumption.former = nil
	if b.resumption.disabled {
		return
	}
	st := b.resumption.storeLocked()
	ctx, cancel := context.WithTimeout(context.Background(), subscriptionStoreTimeout)
	defer cancel()
	rows, err := st.LoadServerSubscriptions(ctx)
	if err != nil {
		b.logger.Warn("matter.subscription.persist.load", slog.String("err", err.Error()))
		return
	}
	for _, row := range rows {
		rec, err := subscription.UnmarshalPeerSubscription(row)
		if err != nil {
			b.logger.Warn("matter.subscription.persist.decode", slog.String("err", err.Error()))
			continue
		}
		b.resumption.former = append(b.resumption.former, rec)
	}
	if len(rows) > 0 {
		if err := st.ClearServerSubscriptions(ctx); err != nil {
			b.logger.Warn("matter.subscription.persist.clear", slog.String("err", err.Error()))
		}
	}
	b.logger.Debug("matter.subscription.persist.loaded", slog.Int("former", len(b.resumption.former)))
}

// FormerSubscriptionCount reports how many subscriptions of the previous
// run are waiting for [Bridge.ReestablishFormerSubscriptions].
func (b *Bridge) FormerSubscriptionCount() int {
	b.resumption.mu.Lock()
	defer b.resumption.mu.Unlock()
	return len(b.resumption.former)
}

// persistSubscription records an active subscription. Mirrors matter.js
// SubscriptionsServer.#addSubscription, which fires when a subscription of
// a CASE session becomes active (SessionsBehavior skips PASE sessions).
// The identity is the session's — fabric and peer node — not the request
// header's, which carries no source node id on a secure session.
func (b *Bridge) persistSubscription(subID uint32) {
	m := b.subscriptionManagerLocked()
	if m == nil || subID == 0 {
		return
	}
	raw, ok := b.routing.subTargets.Load(subID)
	if !ok {
		return
	}
	target, ok := raw.(subTarget)
	if !ok || target.pase || target.fabricIndex == 0 || target.fabricIndex == endpoint.FabricIndexUnresolvable || target.subjectNodeID == 0 {
		return
	}

	b.resumption.mu.Lock()
	defer b.resumption.mu.Unlock()
	if b.resumption.disabled {
		return
	}
	// Checked under the lock the terminated hook also takes: a
	// subscription terminated before this point is gone from the manager
	// and is not written; one terminated after it is deleted again.
	sub, err := m.Get(subID)
	if err != nil {
		return
	}
	rec := sub.PeerSubscription(target.fabricFiltered)
	rec.FabricIndex = target.fabricIndex
	rec.PeerNodeID = target.subjectNodeID
	payload, err := subscription.MarshalPeerSubscription(rec)
	if err != nil {
		b.logger.Warn("matter.subscription.persist.encode", slog.String("err", err.Error()))
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), subscriptionStoreTimeout)
	defer cancel()
	if err := b.resumption.storeLocked().SaveServerSubscription(ctx, rec.SubscriptionID, rec.FabricIndex, rec.PeerNodeID, payload); err != nil {
		b.logger.Warn("matter.subscription.persist.save",
			slog.Int("subscription_id", int(subID)),
			slog.String("err", err.Error()))
	}
}

// forgetPersistedSubscription drops the record of a terminated
// subscription. Installed as the manager's terminated hook; mirrors
// matter.js SubscriptionsServer.#subscriptionCancelled, which removes the
// entry only when `subscription.isTerminated`.
func (b *Bridge) forgetPersistedSubscription(subID uint32) {
	b.resumption.mu.Lock()
	defer b.resumption.mu.Unlock()
	if b.resumption.disabled {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), subscriptionStoreTimeout)
	defer cancel()
	if err := b.resumption.storeLocked().DeleteServerSubscription(ctx, subID); err != nil {
		b.logger.Warn("matter.subscription.persist.delete",
			slog.Int("subscription_id", int(subID)),
			slog.String("err", err.Error()))
	}
}

// forgetFabricSubscriptions drops every record of a removed fabric, and
// any former subscription of it still waiting to be re-established.
func (b *Bridge) forgetFabricSubscriptions(fabricIndex uint8) {
	b.resumption.mu.Lock()
	defer b.resumption.mu.Unlock()
	kept := b.resumption.former[:0]
	for _, rec := range b.resumption.former {
		if rec.FabricIndex != fabricIndex {
			kept = append(kept, rec)
		}
	}
	b.resumption.former = kept
	ctx, cancel := context.WithTimeout(context.Background(), subscriptionStoreTimeout)
	defer cancel()
	if err := b.resumption.storeLocked().DeleteServerSubscriptionsByFabric(ctx, fabricIndex); err != nil {
		b.logger.Warn("matter.subscription.persist.delete_fabric",
			slog.Int("fabric_index", int(fabricIndex)),
			slog.String("err", err.Error()))
	}
}

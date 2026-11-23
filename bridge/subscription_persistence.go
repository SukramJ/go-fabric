// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"cmp"
	"context"
	"log/slog"
	"slices"
	"sync"
	"time"

	"thingfabric.dev/go-fabric/clock"
	"thingfabric.dev/go-fabric/endpoint"
	"thingfabric.dev/go-fabric/im/subscription"
)

// SubscriptionStore is the durable home of the subscriptions of the
// bridge's CASE sessions, so they can be re-established under their old
// SubscriptionId after a restart (docs/adr/0008). It mirrors the persisted
// `subscriptions` state of matter.js SubscriptionsServer
// (packages/node/src/behavior/system/subscriptions/SubscriptionsServer.ts).
//
// The payload is the bridge's encoding of one subscription
// ([subscription.MarshalPeerSubscription]) and is opaque to the store; the
// key columns are there for deletion. It is store.ServerSubscriptions;
// every store.Port implements it.
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
	// rows are the keys of the store's rows in the store's order, the
	// least recently saved first, while rowsKnown: from a run that began
	// on an emptied store every row is one this bridge saved, so the
	// per-save bound and the re-establishment's order check read these
	// keys instead of loading and decoding every row (review II,
	// R2-C2-bound-0501). A store write that fails, or another store, makes
	// them unknown until the next run; the load-based path serves then.
	rows      []persistedRow
	rowsKnown bool

	// orderMu guards activated alone. It is a leaf: taken alone or under
	// mu, never the other way round; under it only a subscription's own
	// lock (EndPriming).
	orderMu sync.Mutex
	// activated are the subscriptions activated ([Bridge.activateSubscription])
	// and not yet taken by a [Bridge.persistSubscription], in activation
	// order.
	activated []uint32

	caseInitiator CaseInitiatorProvider
	resolver      OperationalResolver
	// block is non-nil while ReestablishFormerSubscriptions runs.
	block *peerBlockList

	// initiated routes Secure-Channel replies on exchanges this node
	// opened as CASE initiator: [HandshakeKey] (this node's ephemeral
	// node id and the exchange id) → *initiatedExchange; the OTA link's
	// secure exchanges share it under a secureExchangeKey.
	initiated sync.Map

	// initiating are the CASE initiations this node runs ([Bridge.connectPeer]),
	// each with its fabric, so a fabric update aborts the ones on its
	// fabric ([Bridge.abortFabricInitiations]). Guarded by mu.
	initiating map[*caseInitiating]struct{}
}

// persistedRow is the key of one persisted subscription row.
type persistedRow struct {
	subscriptionID uint32
	fabricIndex    uint8
	peerNodeID     uint64
}

// noteSavedLocked records a successful save of row: it becomes the most
// recent ([SubscriptionStore.SaveServerSubscription]).
func (r *resumptionState) noteSavedLocked(row persistedRow) {
	if !r.rowsKnown {
		return
	}
	r.rows = slices.DeleteFunc(r.rows, func(x persistedRow) bool { return x.subscriptionID == row.subscriptionID })
	r.rows = append(r.rows, row)
}

// noteDeletedLocked records a successful delete of the rows del matches.
func (r *resumptionState) noteDeletedLocked(del func(persistedRow) bool) {
	if r.rowsKnown {
		r.rows = slices.DeleteFunc(r.rows, del)
	}
}

// forgetRowsLocked makes the keys unknown: a store write failed, so the
// store may hold what they do not say.
func (r *resumptionState) forgetRowsLocked() {
	r.rows, r.rowsKnown = nil, false
}

// caseInitiating is one CASE initiation of [Bridge.connectPeer]: its fabric
// and the cancel of its context.
type caseInitiating struct {
	fabricIndex uint8
	cancel      context.CancelCauseFunc
}

func (r *resumptionState) storeLocked() SubscriptionStore {
	if r.store == nil {
		return noopSubscriptionStore{}
	}
	return r.store
}

// AttachSubscriptionStore wires where the subscriptions of CASE sessions
// are persisted. Every store.Port satisfies [SubscriptionStore]. Attach before
// [Bridge.Start]: Start loads the previous run's subscriptions from it.
// Pass nil to revert to the noop store, which persists nothing — silently:
// a restart then drops every subscription and each controller waits out
// its own liveness timeout before re-subscribing.
func (b *Bridge) AttachSubscriptionStore(s SubscriptionStore) {
	b.resumption.mu.Lock()
	b.resumption.store = s
	b.resumption.forgetRowsLocked()
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
//
// Deprecated: no host reads it; a test reads it with
// bridgetest.SubscriptionPersistence. Removable in v0.25.0.
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
	b.resumption.forgetRowsLocked()
	if b.resumption.disabled {
		return
	}
	st := b.resumption.storeLocked()
	ctx, cancel := clock.WithTimeout(context.Background(), b.clk(), subscriptionStoreTimeout)
	defer cancel()
	rows, err := st.LoadServerSubscriptions(ctx)
	if err != nil {
		b.logger.Warn("matter.subscription.persist.load", slog.Any("err", err))
		return
	}
	for _, row := range rows {
		rec, err := subscription.UnmarshalPeerSubscription(row)
		if err != nil {
			b.logger.Warn("matter.subscription.persist.decode", slog.Any("err", err))
			continue
		}
		b.resumption.former = append(b.resumption.former, rec)
	}
	if len(rows) > 0 {
		if err := st.ClearServerSubscriptions(ctx); err != nil {
			b.logger.Warn("matter.subscription.persist.clear", slog.Any("err", err))
			return
		}
	}
	// The noop store keeps nothing, so there is nothing to keep keys of.
	b.resumption.rowsKnown = b.resumption.store != nil
	b.logger.Debug("matter.subscription.persist.loaded", slog.Int("former", len(b.resumption.former)))
}

// FormerSubscriptionCount reports how many subscriptions of the previous
// run are waiting for [Bridge.ReestablishFormerSubscriptions].
//
// Deprecated: no host reads it; a test reads it with
// bridgetest.SubscriptionPersistence. Removable in v0.25.0.
func (b *Bridge) FormerSubscriptionCount() int {
	b.resumption.mu.Lock()
	defer b.resumption.mu.Unlock()
	return len(b.resumption.former)
}

// activateSubscription ends sub's priming — the engine reports from now
// on — and takes sub's place in the order [Bridge.persistSubscription]
// records in, in one step: the persisted order is the activation order.
// matter.js records a subscription when ServerSubscription.activate adds
// it to its session (ServerSubscription.ts:425), its reactions running
// in emission order (Reactors.ts:173-175, :276-289, :359-371), appended
// to the peer's list (SubscriptionsServer.ts:55, :134-166); the
// newest-first re-establishment rests on that order (:190-191, :323-325),
// all at b25ed960. Two Subscribe handlers of one session run on
// goroutines of their own once the first left the session's order, so
// the queue gives them what matter.js's single thread gives it.
func (b *Bridge) activateSubscription(sub *subscription.Subscription, now time.Time) {
	b.resumption.orderMu.Lock()
	defer b.resumption.orderMu.Unlock()
	sub.EndPriming(now)
	b.resumption.activated = append(b.resumption.activated, sub.ID)
}

// nextActivated takes the oldest activated subscription not yet
// recorded; false when there is none.
func (r *resumptionState) nextActivated() (uint32, bool) {
	r.orderMu.Lock()
	defer r.orderMu.Unlock()
	if len(r.activated) == 0 {
		return 0, false
	}
	id := r.activated[0]
	r.activated = r.activated[1:]
	if len(r.activated) == 0 {
		r.activated = nil
	}
	return id, true
}

// persistSubscription records the subscriptions activated up to subID
// ([Bridge.activateSubscription]), subID included, in activation order:
// a handler that reaches this point after a later-activated one was
// already recorded by that one's call. Mirrors matter.js
// SubscriptionsServer.#addSubscription, which fires when a subscription of
// a CASE session becomes active (SessionsBehavior skips PASE sessions).
//
// ctx is the caller's; the store write outlives its cancellation (an
// established subscription is recorded even if the request that made it
// is done) but is bounded by subscriptionStoreTimeout.
func (b *Bridge) persistSubscription(ctx context.Context, subID uint32) {
	m := b.subscriptionManagerLocked()
	if m == nil || subID == 0 {
		return
	}
	// Taken and recorded under mu: the store sees the activation order.
	b.resumption.mu.Lock()
	defer b.resumption.mu.Unlock()
	for {
		id, ok := b.resumption.nextActivated()
		if !ok {
			return
		}
		b.persistOneLocked(ctx, m, id)
		if id == subID {
			return
		}
	}
}

// persistOneLocked records the active subscription subID; b.resumption.mu
// is held. The identity is the session's — fabric and peer node — not the
// request header's, which carries no source node id on a secure session.
// A subscription of a PASE session, or one whose fabric or subject is
// unknown, is not recorded.
func (b *Bridge) persistOneLocked(ctx context.Context, m *subscription.Manager, subID uint32) {
	raw, ok := b.routing.subTargets.Load(subID)
	if !ok {
		return
	}
	target, ok := raw.(subTarget)
	if !ok || target.pase || target.fabricIndex == 0 || target.fabricIndex == endpoint.FabricIndexUnresolvable || target.subjectNodeID == 0 {
		return
	}
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
		b.logger.Warn("matter.subscription.persist.encode", slog.Any("err", err))
		return
	}
	saveCtx, cancel := clock.WithTimeout(context.WithoutCancel(ctx), b.clk(), subscriptionStoreTimeout)
	defer cancel()
	if err := b.resumption.storeLocked().SaveServerSubscription(saveCtx, rec.SubscriptionID, rec.FabricIndex, rec.PeerNodeID, payload); err != nil {
		b.logger.Warn("matter.subscription.persist.save",
			slog.Int("subscription_id", int(subID)),
			slog.Any("err", err))
		b.resumption.forgetRowsLocked()
		return
	}
	b.resumption.noteSavedLocked(persistedRow{subscriptionID: rec.SubscriptionID, fabricIndex: rec.FabricIndex, peerNodeID: rec.PeerNodeID})
	b.boundPersistedSubscriptionsLocked(saveCtx, m, rec, sub.KeepSubscriptions, persistedRowQuota(m))
}

// persistedRowQuota is the number of rows a fabric may keep: the
// manager's subscription quota (owner decision 175).
func persistedRowQuota(m *subscription.Manager) int {
	return m.MaxSubscriptionsPerFabric()
}

// boundPersistedSubscriptionsLocked keeps the persisted rows of rec's
// fabric bounded once rec was saved (review II, R2-P6-bound-0502; owner
// decision 134). A row outlives its subscription when the subscription
// closes with its session, so without a bound every subscribe and
// session loss adds one for the rest of the run, and every row is loaded
// and dialled at the next start. Two rules, ahead of matter.js, whose
// SubscriptionsServer keeps such an entry as long (verify A of review II):
//
//   - a peer that subscribes with KeepSubscriptions=false gives up its
//     former subscriptions: its rows whose subscription is not live go;
//   - a fabric keeps at most quota rows: beyond it the oldest whose
//     subscription is not live go first, in the store's order, the least
//     recently saved first (store.ServerSubscriptions). A live
//     subscription's row is never dropped.
//
// Runs under resumption.mu, as the save before it.
func (b *Bridge) boundPersistedSubscriptionsLocked(ctx context.Context, m *subscription.Manager, rec subscription.PeerSubscription, keepSubscriptions bool, quota int) {
	if keepSubscriptions && quota <= 0 {
		return
	}
	st := b.resumption.storeLocked()
	rows, ok := b.persistedRowsLocked(ctx)
	if !ok {
		return
	}
	live := func(id uint32) bool {
		_, err := m.Get(id)
		return err == nil
	}
	drop := func(id uint32) bool {
		if err := st.DeleteServerSubscription(ctx, id); err != nil {
			b.logger.Warn("matter.subscription.persist.delete",
				slog.Int("subscription_id", int(id)),
				slog.Any("err", err))
			b.resumption.forgetRowsLocked()
			return false
		}
		b.resumption.noteDeletedLocked(func(x persistedRow) bool { return x.subscriptionID == id })
		return true
	}
	var fabricRows []persistedRow
	for _, other := range rows {
		if other.fabricIndex != rec.FabricIndex {
			continue
		}
		if !keepSubscriptions && other.peerNodeID == rec.PeerNodeID && other.subscriptionID != rec.SubscriptionID && !live(other.subscriptionID) && drop(other.subscriptionID) {
			continue
		}
		fabricRows = append(fabricRows, other)
	}
	excess := len(fabricRows) - quota
	if quota <= 0 || excess <= 0 {
		return
	}
	for _, other := range fabricRows {
		if excess == 0 {
			return
		}
		if other.subscriptionID != rec.SubscriptionID && !live(other.subscriptionID) && drop(other.subscriptionID) {
			excess--
		}
	}
}

// persistedRowsLocked are the keys of the store's rows, the least
// recently saved first: the bridge's own record while it holds one
// (resumptionState.rows), otherwise every row loaded and decoded, an
// undecodable one left out. False when the load failed.
func (b *Bridge) persistedRowsLocked(ctx context.Context) ([]persistedRow, bool) {
	if b.resumption.rowsKnown {
		return slices.Clone(b.resumption.rows), true
	}
	raw, err := b.resumption.storeLocked().LoadServerSubscriptions(ctx)
	if err != nil {
		b.logger.Warn("matter.subscription.persist.load", slog.Any("err", err))
		return nil, false
	}
	rows := make([]persistedRow, 0, len(raw))
	for _, payload := range raw {
		rec, err := subscription.UnmarshalPeerSubscription(payload)
		if err != nil {
			continue
		}
		rows = append(rows, persistedRow{subscriptionID: rec.SubscriptionID, fabricIndex: rec.FabricIndex, peerNodeID: rec.PeerNodeID})
	}
	return rows, true
}

// restoreCreationOrder puts the store's rows back into the order of
// former — the creation order the previous run's store held — with the
// subscriptions saved since this run began after them, in the order they
// were saved. Re-establishment re-saves a peer's subscriptions newest
// first, and the store orders by save time, so without it every restart
// would reverse a peer's subscriptions and the next one's newest-first
// walk would try the oldest first. Nothing is written when the order
// already holds. Mirrors matter.js SubscriptionsServer.#restoreCreationOrder
// (SubscriptionsServer.ts:193-204, called at :371, at db021da0); review
// II, R2-P2-sess-01.
//
// ctx is the caller's; the store calls outlive its cancellation, as
// persistSubscription's do, bounded by subscriptionStoreTimeout. It runs
// under resumption.mu, which persistSubscription and the
// terminated hook take too: no row is saved or deleted between the load
// and the re-saves, so a subscription terminated meanwhile is deleted
// after them, not written back.
func (b *Bridge) restoreCreationOrder(ctx context.Context, former []subscription.PeerSubscription) {
	formerIndex := make(map[uint32]int, len(former))
	for i, rec := range former {
		formerIndex[rec.SubscriptionID] = i
	}
	orderOf := func(rec subscription.PeerSubscription) int {
		if i, ok := formerIndex[rec.SubscriptionID]; ok {
			return i
		}
		return len(former)
	}

	b.resumption.mu.Lock()
	defer b.resumption.mu.Unlock()
	if b.resumption.disabled {
		return
	}
	st := b.resumption.storeLocked()
	ctx, cancel := clock.WithTimeout(context.WithoutCancel(ctx), b.clk(), subscriptionStoreTimeout)
	defer cancel()
	if b.resumption.rowsKnown {
		// The order check needs only the keys; the payloads are loaded
		// when the order does not hold (review II, R2-C2-bound-0501).
		known := b.resumption.rows
		sorted := slices.IsSortedFunc(known, func(x, y persistedRow) int {
			return cmp.Compare(orderOf(subscription.PeerSubscription{SubscriptionID: x.subscriptionID}), orderOf(subscription.PeerSubscription{SubscriptionID: y.subscriptionID}))
		})
		if sorted {
			return
		}
	}
	rows, err := st.LoadServerSubscriptions(ctx)
	if err != nil {
		b.logger.Warn("matter.subscription.persist.load", slog.Any("err", err))
		return
	}
	type savedRow struct {
		rec     subscription.PeerSubscription
		payload []byte
	}
	saved := make([]savedRow, 0, len(rows))
	for _, row := range rows {
		rec, err := subscription.UnmarshalPeerSubscription(row)
		if err != nil {
			b.logger.Warn("matter.subscription.persist.decode", slog.Any("err", err))
			continue
		}
		saved = append(saved, savedRow{rec: rec, payload: row})
	}
	ordered := slices.Clone(saved)
	slices.SortStableFunc(ordered, func(x, y savedRow) int { return cmp.Compare(orderOf(x.rec), orderOf(y.rec)) })
	if slices.EqualFunc(ordered, saved, func(x, y savedRow) bool { return x.rec.SubscriptionID == y.rec.SubscriptionID }) {
		return
	}
	for _, row := range ordered {
		if err := st.SaveServerSubscription(ctx, row.rec.SubscriptionID, row.rec.FabricIndex, row.rec.PeerNodeID, row.payload); err != nil {
			b.logger.Warn("matter.subscription.persist.save",
				slog.Int("subscription_id", int(row.rec.SubscriptionID)),
				slog.Any("err", err))
			b.resumption.forgetRowsLocked()
			continue
		}
		b.resumption.noteSavedLocked(persistedRow{subscriptionID: row.rec.SubscriptionID, fabricIndex: row.rec.FabricIndex, peerNodeID: row.rec.PeerNodeID})
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
	ctx, cancel := clock.WithTimeout(context.Background(), b.clk(), subscriptionStoreTimeout)
	defer cancel()
	if err := b.resumption.storeLocked().DeleteServerSubscription(ctx, subID); err != nil {
		b.logger.Warn("matter.subscription.persist.delete",
			slog.Int("subscription_id", int(subID)),
			slog.Any("err", err))
		b.resumption.forgetRowsLocked()
		return
	}
	b.resumption.noteDeletedLocked(func(x persistedRow) bool { return x.subscriptionID == subID })
}

// forgetFabricSubscriptions drops every record of a removed fabric, and
// any former subscription of it still waiting to be re-established.
func (b *Bridge) forgetFabricSubscriptions(ctx context.Context, fabricIndex uint8) {
	b.resumption.mu.Lock()
	defer b.resumption.mu.Unlock()
	kept := b.resumption.former[:0]
	for _, rec := range b.resumption.former {
		if rec.FabricIndex != fabricIndex {
			kept = append(kept, rec)
		}
	}
	b.resumption.former = kept
	ctx, cancel := clock.WithTimeout(context.WithoutCancel(ctx), b.clk(), subscriptionStoreTimeout)
	defer cancel()
	if err := b.resumption.storeLocked().DeleteServerSubscriptionsByFabric(ctx, fabricIndex); err != nil {
		b.logger.Warn("matter.subscription.persist.delete_fabric",
			slog.Int("fabric_index", int(fabricIndex)),
			slog.Any("err", err))
		b.resumption.forgetRowsLocked()
		return
	}
	b.resumption.noteDeletedLocked(func(x persistedRow) bool { return x.fabricIndex == fabricIndex })
}

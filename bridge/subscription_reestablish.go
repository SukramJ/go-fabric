// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/im/subscription"
)

// peerKey is matter.js's PeerAddress: (fabric index, node id).
type peerKey struct {
	fabricIndex uint8
	nodeID      uint64
}

// peerBlockList is matter.js reestablishFormerSubscriptions' `peerStopList`:
// peers whose former subscriptions must not be (further) re-established —
// because the peer already began a normal subscribe, or because reaching
// it failed.
type peerBlockList struct {
	mu    sync.Mutex
	peers map[peerKey]struct{}
}

func (l *peerBlockList) add(k peerKey) {
	l.mu.Lock()
	l.peers[k] = struct{}{}
	l.mu.Unlock()
}

func (l *peerBlockList) has(k peerKey) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	_, ok := l.peers[k]
	return ok
}

// noteSubscriptionEstablishmentStarted is the Go form of matter.js
// InteractionServer.subscriptionEstablishmentStarted: a normal subscribe
// from the peer has begun, so a re-establishment still running leaves the
// peer alone.
func (b *Bridge) noteSubscriptionEstablishmentStarted(fabricIndex uint8, nodeID uint64) {
	b.resumption.mu.Lock()
	block := b.resumption.block
	b.resumption.mu.Unlock()
	if block != nil && nodeID != 0 {
		block.add(peerKey{fabricIndex: fabricIndex, nodeID: nodeID})
	}
}

// ReestablishResult reports what [Bridge.ReestablishFormerSubscriptions]
// did.
type ReestablishResult struct {
	// Former is the number of former subscriptions considered.
	Former int
	// Reestablished lists the ids that run again.
	Reestablished []uint32
}

// errPeerGone marks a re-establishment failure that says the peer cannot
// be reached; the remaining subscriptions of the peer are not attempted.
var errPeerGone = errors.New("bridge: peer unreachable while re-establishing")

// ReestablishFormerSubscriptions re-establishes the subscriptions of the
// previous run, loaded at [Bridge.Start], towards the controllers that
// held them. For each peer it obtains a CASE session — opening one as the
// initiator, within [subscription.ReestablishTimeout] — then re-creates
// each subscription under its old id and sends its priming report on a
// fresh exchange, without a SubscribeResponse: the controller still
// believes the subscription exists.
//
// Mirrors matter.js SubscriptionsServer.reestablishFormerSubscriptions
// (packages/node/src/behavior/system/subscriptions/SubscriptionsServer.ts):
// peers are handled in parallel, a peer that begins a normal subscribe
// meanwhile is skipped, a peer that cannot be reached is skipped
// entirely, and a peer's loop stops at a transport failure or an
// InvalidSubscription answer. A subscription that does not come back is
// not recorded again — it is dropped. The former set is consumed: a
// second call does nothing.
//
// A host calls it once its CASE identities are loaded and before it
// announces its operational records; matter.js runs it in
// CommissioningServer before enterOperationalMode. Nothing here fails the
// bridge; every failure costs only that peer's re-establishment.
func (b *Bridge) ReestablishFormerSubscriptions(ctx context.Context) ReestablishResult {
	b.resumption.mu.Lock()
	if b.resumption.disabled {
		b.resumption.mu.Unlock()
		return ReestablishResult{}
	}
	former := b.resumption.former
	b.resumption.former = nil
	if len(former) == 0 {
		b.resumption.mu.Unlock()
		b.logger.Debug("matter.subscription.reestablish.none")
		return ReestablishResult{}
	}
	block := &peerBlockList{peers: make(map[peerKey]struct{})}
	b.resumption.block = block
	b.resumption.mu.Unlock()
	defer func() {
		b.resumption.mu.Lock()
		if b.resumption.block == block {
			b.resumption.block = nil
		}
		b.resumption.mu.Unlock()
	}()

	// Group by peer, keeping each peer's persisted order.
	var order []peerKey
	byPeer := make(map[peerKey][]subscription.PeerSubscription)
	for _, rec := range former {
		k := peerKey{fabricIndex: rec.FabricIndex, nodeID: rec.PeerNodeID}
		if _, seen := byPeer[k]; !seen {
			order = append(order, k)
		}
		byPeer[k] = append(byPeer[k], rec)
	}

	var (
		mu   sync.Mutex
		done []uint32
		wg   sync.WaitGroup
	)
	for _, k := range order {
		wg.Go(func() {
			ids := b.reestablishPeer(ctx, k, byPeer[k], block)
			mu.Lock()
			done = append(done, ids...)
			mu.Unlock()
		})
	}
	wg.Wait()

	idStrs := make([]string, 0, len(done))
	for _, id := range done {
		idStrs = append(idStrs, strconv.FormatUint(uint64(id), 10))
	}
	b.logger.Info("matter.subscription.reestablished",
		slog.Int("reestablished", len(done)),
		slog.Int("former", len(former)),
		slog.String("ids", strings.Join(idStrs, ",")))
	return ReestablishResult{Former: len(former), Reestablished: done}
}

// reestablishPeer runs one peer's share of ReestablishFormerSubscriptions.
func (b *Bridge) reestablishPeer(ctx context.Context, k peerKey, recs []subscription.PeerSubscription, block *peerBlockList) []uint32 {
	if block.has(k) {
		b.logger.Debug("matter.subscription.reestablish.skip_peer",
			slog.Int("fabric_index", int(k.fabricIndex)), slog.Uint64("peer_node", k.nodeID))
		return nil
	}
	connCtx, cancel := context.WithTimeout(ctx, subscription.ReestablishTimeout)
	link, err := b.connectPeer(connCtx, k.fabricIndex, k.nodeID)
	cancel()
	if err != nil {
		block.add(k)
		b.logger.Debug("matter.subscription.reestablish.connect_failed",
			slog.Int("fabric_index", int(k.fabricIndex)), slog.Uint64("peer_node", k.nodeID),
			slog.String("err", err.Error()))
		return nil
	}

	var ok []uint32
	for _, rec := range recs {
		if block.has(k) {
			b.logger.Debug("matter.subscription.reestablish.skip",
				slog.Int("subscription_id", int(rec.SubscriptionID)))
			continue
		}
		err := b.establishFormerSubscription(ctx, rec, link)
		if err == nil {
			ok = append(ok, rec.SubscriptionID)
			continue
		}
		b.logger.Debug("matter.subscription.reestablish.failed",
			slog.Int("subscription_id", int(rec.SubscriptionID)),
			slog.Uint64("peer_node", k.nodeID),
			slog.String("err", err.Error()))
		var rejected *chunkStatusError
		switch {
		case errors.Is(err, errPeerGone):
			// Report sends do not declare peer loss, so nothing else would
			// stop this loop from spending a full MRP window on each of the
			// peer's remaining subscriptions.
			return ok
		case errors.As(err, &rejected) && rejected.status == im.StatusInvalidSubscription:
			// The peer dropped its state for us; another of its
			// subscriptions surviving is too unlikely to spend a priming
			// report finding out.
			return ok
		}
	}
	return ok
}

// chunkStatusError is a priming-report chunk the peer answered with an
// error status.
type chunkStatusError struct{ status im.StatusCode }

func (e *chunkStatusError) Error() string {
	return "bridge: peer answered priming report with " + e.status.String()
}

// establishFormerSubscription re-creates rec on the peer's session and
// sends its priming report. Mirrors matter.js
// InteractionServer.establishFormerSubscription: a ServerSubscription with
// the old id and the stored intervals, `sendInitialReport(..., true)` on a
// device-initiated exchange — status reports suppressed, no
// SubscribeResponse — then `activate()`, which is when SubscriptionsServer
// records it again.
func (b *Bridge) establishFormerSubscription(ctx context.Context, rec subscription.PeerSubscription, link peerLink) error {
	m := b.subscriptionManagerLocked()
	dispatcher := b.Dispatcher()
	if m == nil || dispatcher == nil {
		return errors.New("bridge: no subscription manager or dispatcher")
	}
	subjectNodeID, subjectCATs := b.resolveSessionSubject(link.sessionID)
	if subjectNodeID == 0 {
		subjectNodeID = rec.PeerNodeID
	}
	fabricIndex := b.resolveSessionFabric(link.sessionID)
	if fabricIndex == 0 {
		fabricIndex = rec.FabricIndex
	}

	sub, err := m.Restore(rec, link.sessionID)
	if err != nil {
		return err
	}
	target := subTarget{
		src:            link.addr,
		sessionID:      link.sessionID,
		exchangeID:     b.nextOutboundExchangeID(),
		peerInitiator:  false, // this node opens the exchange
		fabricIndex:    fabricIndex,
		subjectNodeID:  subjectNodeID,
		subjectCATs:    subjectCATs,
		fabricFiltered: rec.IsFabricFiltered,
	}
	b.routing.subTargets.Store(sub.ID, target)

	readCtx := im.WithFabricFilter(ctx, rec.IsFabricFiltered, fabricIndex)
	readCtx = im.WithSubject(readCtx, subjectNodeID, subjectCATs)
	report, matched := b.buildInitialReport(readCtx, dispatcher, im.SubscribeRequest{
		MinIntervalFloor:   rec.MinIntervalFloor,
		MaxIntervalCeiling: rec.MaxIntervalCeiling,
		AttributeRequests:  rec.AttributeRequests,
		EventRequests:      rec.EventRequests,
		FabricFiltered:     rec.IsFabricFiltered,
	})
	if matched == 0 {
		// matter.js #processAttributesAndEventsReport: "Subscription
		// failed because no attributes or events are matching the query".
		_ = m.Release(sub.ID)
		return &chunkStatusError{status: im.StatusInvalidAction}
	}
	// `suppressStatusReports`: re-establishment simulates a subscription
	// that never went away, so per-path errors are not reported again.
	kept := report.Reports[:0]
	for _, r := range report.Reports {
		if !r.IsStatus {
			kept = append(kept, r)
		}
	}
	report.Reports = kept
	report.HasSubscription = true
	report.SubscriptionID = sub.ID
	report.SuppressResponse = false

	if err := b.sendPrimingReport(target, report); err != nil {
		_ = m.Release(sub.ID)
		return err
	}
	sub.TouchLastReport(time.Now())
	b.logger.Info("matter.subscription.reestablished_one",
		slog.Int("subscription_id", int(sub.ID)),
		slog.Int("session_id", int(link.sessionID)),
		slog.Int("max_interval", int(sub.MaxIntervalCeiling)),
		slog.Duration("send_interval", sub.SendInterval()))
	b.persistSubscription(sub.ID)
	return nil
}

// sendPrimingReport streams report on target's exchange, one chunk at a
// time, waiting for the peer's IM StatusResponse to each — matter.js
// InteractionMessenger.sendDataReport with its default waitForAck. Each
// chunk after the first piggybacks the acknowledgement of the peer's
// previous StatusResponse.
func (b *Bridge) sendPrimingReport(target subTarget, report im.ReportData) error {
	chunks, err := chunkReportData(report, reportChunkPayloadBudget)
	if err != nil {
		return err
	}
	b.mu.RLock()
	ackTracker := b.ackTracker
	b.mu.RUnlock()
	for i, chunk := range chunks {
		body, err := EncodeReportData(chunk)
		if err != nil {
			return err
		}
		chunkTarget := target
		if ackTracker != nil {
			if counter, ok := ackTracker.LookupAndDischarge(target.sessionID, target.exchangeID, true); ok {
				chunkTarget.hasAck, chunkTarget.ackCounter = true, counter
			}
		}
		waitCh := b.armStatusResponseWait(target.sessionID, target.exchangeID, true)
		if _, err := b.sendUnsolicitedIM(chunkTarget, im.OpcodeReportData, body); err != nil {
			b.disarmStatusResponseWait(target.sessionID, target.exchangeID, true)
			return fmt.Errorf("%w: %w", errPeerGone, err)
		}
		timeout := b.chunkStatusResponseTimeout(target.sessionID)
		timer := time.NewTimer(timeout)
		select {
		case status := <-waitCh:
			timer.Stop()
			b.disarmStatusResponseWait(target.sessionID, target.exchangeID, true)
			if !status.IsSuccess() {
				return &chunkStatusError{status: status}
			}
		case <-timer.C:
			b.disarmStatusResponseWait(target.sessionID, target.exchangeID, true)
			return fmt.Errorf("%w: chunk %d unanswered after %s", errPeerGone, i, timeout)
		}
	}
	return nil
}

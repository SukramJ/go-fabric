// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package softwareupdate

import (
	"context"
	"errors"
	"log/slog"
	"math"
	"slices"
	"sync"
	"time"

	"github.com/SukramJ/go-fabric/client/exchange"
	"github.com/SukramJ/go-fabric/client/peer"
	"github.com/SukramJ/go-fabric/clock"
	"github.com/SukramJ/go-fabric/cluster/spec"
	otareq "github.com/SukramJ/go-fabric/cluster/spec/otasoftwareupdaterequestor"
	"github.com/SukramJ/go-fabric/controller/client"
)

// The announcements: matter.js OtaAnnouncements
// (packages/node/src/behavior/system/software-update/OtaAnnouncements.ts at
// 862ad75f) — the daily write of our provider as each node's default
// provider, one node at a time, and the AnnounceOTAProvider command of an
// update.

// announcementDelay is the pause between two nodes' announcements: at
// least 1 s, matter.js takes 10 s (OtaAnnouncements.ts:54-57).
const announcementDelay = 10 * time.Second

const (
	announceSimple          = otareq.AnnouncementReasonSimpleAnnouncement
	announceUpdateAvailable = otareq.AnnouncementReasonUpdateAvailable
)

type announcements struct {
	m     *Manager
	clock clock.Clock
	log   *slog.Logger

	mu       sync.Mutex
	queue    []Peer
	interval time.Duration
	timer    clock.Timer // the all-nodes timer; nil while announcements are off
	delay    clock.Timer // the per-node delay; nil while not running
	running  bool        // an announcement is in flight
	closed   bool
	work     sync.WaitGroup
}

func newAnnouncements(m *Manager) *announcements {
	return &announcements{m: m, clock: m.clock, log: m.log}
}

// setInterval turns the daily default-provider write on with interval (at
// least 24 h, with 60–180 s of jitter), first after 10–15 minutes, or off
// with 0 — the interval setter (OtaAnnouncements.ts:60-88).
func (a *announcements) setInterval(interval time.Duration) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if interval <= 0 {
		a.interval = 0
		if a.timer != nil {
			a.timer.Stop()
			a.timer = nil
		}
		return
	}
	if interval < 24*time.Hour {
		a.log.Warn("ota.announcements.interval_too_short", slog.Duration("interval", interval))
		interval = 24 * time.Hour
	}
	a.interval = interval + time.Duration(math.Floor(a.m.rand()*120)+60)*time.Second
	if a.timer != nil {
		a.timer.Stop()
	}
	initial := 10*time.Minute + time.Duration(math.Floor(a.m.rand()*300))*time.Second
	a.timer = a.clock.AfterFunc(initial, a.initialize)
}

// initialize is #initializeAnnouncements (:90-99).
func (a *announcements) initialize() {
	a.mu.Lock()
	if a.interval == 0 || a.closed {
		a.mu.Unlock()
		return
	}
	a.mu.Unlock()
	a.queueAllPeers()
}

// queueAllPeers is #queueAllPeers (:101-118): every commissioned node
// online goes into the queue once, and the all-nodes timer restarts.
func (a *announcements) queueAllPeers() {
	a.mu.Lock()
	if a.interval == 0 || a.closed {
		a.mu.Unlock()
		return
	}
	if a.timer != nil {
		a.timer.Stop()
	}
	a.mu.Unlock()
	for _, n := range a.m.cfg.Registry.Nodes() {
		p, ok := n.PeerAddress()
		if !ok || !n.SubscriptionActive() {
			continue
		}
		a.mu.Lock()
		if !slices.Contains(a.queue, p) {
			a.queuePeerLocked(p)
		}
		a.mu.Unlock()
	}
	a.mu.Lock()
	if a.interval != 0 && !a.closed {
		a.timer = a.clock.AfterFunc(a.interval, a.queueAllPeers)
	}
	a.mu.Unlock()
}

// queuePeerLocked is #queuePeer (:120-129).
func (a *announcements) queuePeerLocked(p Peer) {
	a.queue = append(a.queue, p)
	a.log.Debug("ota.announcements.queued", slog.String("node", peerString(p)), slog.Int("queued", len(a.queue)))
	if a.delay == nil {
		a.delay = a.clock.AfterFunc(announcementDelay, a.processQueueEntry)
	}
}

// processQueueEntry is #processQueueEntry (:131-157).
func (a *announcements) processQueueEntry() {
	a.mu.Lock()
	a.delay = nil
	if a.running || a.closed {
		a.mu.Unlock()
		return
	}
	if len(a.queue) == 0 {
		a.mu.Unlock()
		return
	}
	p := a.queue[0]
	a.running = true
	a.work.Add(1)
	a.mu.Unlock()
	defer a.work.Done()
	if err := a.announceDefaultProvider(context.Background(), p); err != nil {
		a.log.Warn("ota.announcements.error", slog.String("node", peerString(p)), slog.Any("err", err))
	}
	a.mu.Lock()
	a.running = false
	if len(a.queue) > 0 && a.queue[0] == p {
		a.queue = a.queue[1:]
	}
	if len(a.queue) > 0 && !a.closed {
		a.delay = a.clock.AfterFunc(announcementDelay, a.processQueueEntry)
	}
	a.mu.Unlock()
}

// announceDefaultProvider is #announceOtaProvider (:159-215): an
// applicable node gets our DefaultOTAProviders record unless it holds the
// same one; a failed write waits for the next day. matter.js deliberately
// sends no Simple Announcement after it (:200-203).
func (a *announcements) announceDefaultProvider(ctx context.Context, p Peer) error {
	n := a.m.nodeAt(p)
	if n == nil || !n.IsCommissioned() {
		return nil
	}
	ota, ok := peerApplicableForUpdate(n, a.log)
	if !ok {
		return nil
	}
	cfg := a.m.cfg
	record := otareq.ProviderLocation{ProviderNodeId: cfg.ProviderNodeID, Endpoint: cfg.ProviderEndpoint, FabricIndex: cfg.FabricIndex}
	var existing *otareq.ProviderLocation
	if ep := n.Endpoint(ota.endpoint); ep != nil {
		if c := ep.Cluster(otareq.ClusterID); c != nil {
			if v, err := c.Decoded(otareq.AttrDefaultOtaProviders); err == nil {
				if list, ok := v.(spec.List[otareq.ProviderLocation]); ok {
					for _, l := range list {
						if l.FabricIndex == cfg.FabricIndex {
							l := l
							existing = &l
							break
						}
					}
				}
			}
		}
	}
	if existing != nil && *existing == record {
		return nil
	}
	// Fabric-scoped: the write replaces our entry (:186-197).
	value := spec.List[otareq.ProviderLocation]{record}
	statuses, err := n.Write(ctx, []client.WriteRequest{{Path: client.AttributeAt(ota.endpoint, otareq.ClusterID, otareq.AttrDefaultOtaProviders), Value: value}}, client.WriteOptions{})
	if err == nil {
		err = statuses.Err()
	}
	if err != nil {
		a.log.Info("ota.announcements.default_provider_failed", slog.String("node", peerString(p)), slog.Any("err", err))
		return nil
	}
	a.log.Debug("ota.announcements.default_provider_written", slog.String("node", peerString(p)), slog.Int("endpoint", int(ota.endpoint)))
	return nil
}

// announceOtaProvider sends AnnounceOTAProvider to the node's requestor
// endpoint (:266-292). A closed connection is logged and retried later;
// any other failure is only logged, as matter.js's.
func (a *announcements) announceOtaProvider(ctx context.Context, n *client.Node, endpoint uint16, reason otareq.AnnouncementReasonEnum) error {
	cfg := a.m.cfg
	req := otareq.AnnounceOtaProviderRequest{
		ProviderNodeId: cfg.ProviderNodeID, VendorId: cfg.VendorID, AnnouncementReason: reason, Endpoint: cfg.ProviderEndpoint,
	}
	_, err := n.Invoke(ctx, client.CommandAt(endpoint, otareq.ClusterID, otareq.CmdAnnounceOtaProvider), req, client.InvokeOptions{})
	if err == nil {
		return nil
	}
	if errors.Is(err, exchange.ErrSessionClosed) || errors.Is(err, peer.ErrClosed) {
		a.log.Debug("ota.announcements.closed_retry_later", slog.Int("endpoint", int(endpoint)), slog.Any("err", err))
		return nil
	}
	a.log.Info("ota.announcements.notify_failed", slog.Int("endpoint", int(endpoint)), slog.Any("err", err))
	return nil
}

func (a *announcements) close() {
	a.mu.Lock()
	a.closed = true
	for _, t := range []clock.Timer{a.timer, a.delay} {
		if t != nil {
			t.Stop()
		}
	}
	a.mu.Unlock()
	a.work.Wait()
}

// otaTarget is peerApplicableForUpdate's result.
type otaTarget struct {
	endpoint uint16
	peer     Peer
}

// peerApplicableForUpdate is peerApplicableForUpdate (:217-253): a node
// with BasicInformation, commissioned, enabled and subscribed — so the
// manager works on current data — with an OTA requestor endpoint
// (#findOtaRequestorEndpointOn, :255-262).
func peerApplicableForUpdate(n *client.Node, log *slog.Logger) (otaTarget, bool) {
	if _, ok := basicInformation(n); !ok {
		return otaTarget{}, false
	}
	p, ok := n.PeerAddress()
	if !ok || n.Disabled() || !n.SubscriptionActive() {
		log.Info("ota.manager.not_applicable", slog.String("node", n.ID()))
		return otaTarget{}, false
	}
	for _, ep := range n.Endpoints() {
		if c := ep.Cluster(otareq.ClusterID); c != nil && c.Ready() {
			return otaTarget{endpoint: ep.Number(), peer: p}, true
		}
	}
	log.Debug("ota.manager.no_requestor", slog.String("node", n.ID()))
	return otaTarget{}, false
}

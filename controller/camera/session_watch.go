// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package camera

import (
	"context"
	"log/slog"
	"slices"
	"sync"
	"time"

	"github.com/SukramJ/go-fabric/clock"
)

// restartMarkTimeout is how long after a StartUp event an endpoint's first
// CurrentSessions change still counts as the restart. Mirrors
// matterjs-server packages/ws-controller/src/controller/WebRtcProviderSessionWatch.ts:13-18
// at 0bd06f2.
const restartMarkTimeout = 30 * time.Second

// leftoverSweepRetryDelay: a sweep with a failed EndSession runs again at
// the first batch end this long after the failure. Mirrors matterjs-server
// packages/ws-controller/src/controller/WebRtcProviderSessionWatch.ts:20-21
// at 0bd06f2.
const leftoverSweepRetryDelay = 30 * time.Second

// maxFailedLeftoverSweeps bounds the EndSession traffic toward a camera
// that keeps refusing it. Mirrors matterjs-server
// packages/ws-controller/src/controller/WebRtcProviderSessionWatch.ts:23-27
// at 0bd06f2.
const maxFailedLeftoverSweeps = 3

// sessionPeer is one CurrentSessions entry as the watch reads it.
type sessionPeer struct {
	id         uint16
	peerNodeID uint64
}

// watchIO is what the watch reads and does. Mirrors matterjs-server
// packages/ws-controller/src/controller/WebRtcProviderSessionWatch.ts:29-43
// at 0bd06f2.
type watchIO interface {
	// localNodeID is the PeerNodeID a camera stores for this controller.
	localNodeID() uint64
	providerEndpoints(nodeID uint64) []uint16
	// currentSessions is the provider's CurrentSessions from the image;
	// ok false without the cluster.
	currentSessions(nodeID uint64, endpointID uint16) (sessions []sessionPeer, ok bool)
	// requestorSessionIDs are the ids of the provider's sessions the local
	// requestor holds, from every route of this run.
	requestorSessionIDs(nodeID uint64, endpointID uint16) map[uint16]bool
	// endSession is EndSession for a session no local record names.
	endSession(ctx context.Context, nodeID uint64, endpointID, sessionID uint16) error
	// reported is one report of an endpoint: present holds the ids it lists
	// for this controller.
	reported(nodeID uint64, endpointID uint16, present map[uint16]bool, restarted bool)
}

type watchKey struct {
	nodeID     uint64
	endpointID uint16
}

type failedSweeps struct {
	count   int
	retryAt time.Time
}

type restartMark struct {
	until time.Time
	used  map[uint16]bool
}

// sessionWatch watches each camera's CurrentSessions (§11.5.5.1) and
// reports every change at the end of the report batch, so a StartUp event
// in the same batch marks it as a restart whatever order the batch had. At
// the first batch end per endpoint and run it ends the sessions the camera
// lists for this controller that the local requestor does not hold — a
// controller that stopped without EndSession leaves them pinning streams.
// The sweep waits while an offer to the endpoint is in flight; one whose
// EndSession failed runs again after leftoverSweepRetryDelay, at most
// maxFailedLeftoverSweeps times. Mirrors matterjs-server
// packages/ws-controller/src/controller/WebRtcProviderSessionWatch.ts:45-252
// at 0bd06f2.
type sessionWatch struct {
	io  watchIO
	clk clock.Clock
	log *slog.Logger
	ctx context.Context //nolint:containedctx // the watch's lifetime: a sweep's EndSession outlives the batch end that started it

	mu           sync.Mutex
	changed      map[uint64]map[uint16]bool
	restarts     map[uint64]*restartMark
	swept        map[watchKey]bool
	nodesSwept   map[uint64]bool
	establishing map[watchKey]int
	failed       map[watchKey]*failedSweeps
	work         sync.WaitGroup
}

func newSessionWatch(ctx context.Context, io watchIO, clk clock.Clock, log *slog.Logger) *sessionWatch {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &sessionWatch{
		io: io, clk: clock.Or(clk), log: log, ctx: ctx,
		changed: map[uint64]map[uint16]bool{}, restarts: map[uint64]*restartMark{},
		swept: map[watchKey]bool{}, nodesSwept: map[uint64]bool{},
		establishing: map[watchKey]int{}, failed: map[watchKey]*failedSweeps{},
	}
}

// currentSessionsChanged records a CurrentSessions change of the endpoint
// for the batch end. Mirrors matterjs-server
// packages/ws-controller/src/controller/WebRtcProviderSessionWatch.ts:91-98
// at 0bd06f2.
func (w *sessionWatch) currentSessionsChanged(nodeID uint64, endpointID uint16) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.changed[nodeID] == nil {
		w.changed[nodeID] = map[uint16]bool{}
	}
	w.changed[nodeID][endpointID] = true
}

// startedUp records the node's StartUp event. Mirrors matterjs-server
// packages/ws-controller/src/controller/WebRtcProviderSessionWatch.ts:100-103
// at 0bd06f2.
func (w *sessionWatch) startedUp(nodeID uint64) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.restarts[nodeID] = &restartMark{until: w.clk.Now().Add(restartMarkTimeout), used: map[uint16]bool{}}
}

// establishingDo wraps every call that can make the camera create a
// session, until the local requestor holds it. Mirrors matterjs-server
// packages/ws-controller/src/controller/WebRtcProviderSessionWatch.ts:105-116
// at 0bd06f2.
func establishingDo[T any](w *sessionWatch, t target, work func() (T, error)) (T, error) {
	k := watchKey(t)
	w.mu.Lock()
	w.establishing[k]++
	w.mu.Unlock()
	defer func() {
		w.mu.Lock()
		w.establishing[k]--
		if w.establishing[k] <= 0 {
			delete(w.establishing, k)
		}
		w.mu.Unlock()
	}()
	return work()
}

// reportCompleted is a report batch of the node applied: its state is
// current. Mirrors matterjs-server
// packages/ws-controller/src/controller/WebRtcProviderSessionWatch.ts:118-132
// at 0bd06f2.
func (w *sessionWatch) reportCompleted(nodeID uint64) {
	w.mu.Lock()
	changed := w.changed[nodeID]
	delete(w.changed, nodeID)
	sweepPending := !w.nodesSwept[nodeID]
	w.mu.Unlock()
	var providers []uint16
	if sweepPending {
		providers = w.io.providerEndpoints(nodeID)
	}
	endpoints := []uint16{}
	for ep := range changed {
		endpoints = append(endpoints, ep)
	}
	slices.Sort(endpoints)
	for _, ep := range providers {
		if !slices.Contains(endpoints, ep) {
			endpoints = append(endpoints, ep)
		}
	}
	if len(endpoints) > 0 {
		w.report(nodeID, endpoints, changed)
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if r := w.restarts[nodeID]; r != nil && !w.clk.Now().Before(r.until) {
		delete(w.restarts, nodeID)
	}
	if sweepPending && !slices.ContainsFunc(providers, func(ep uint16) bool { return !w.swept[watchKey{nodeID, ep}] }) {
		w.nodesSwept[nodeID] = true
	}
}

// report hands each endpoint's report on and sweeps it; a failing handler
// does not stop the others. Mirrors matterjs-server
// packages/ws-controller/src/controller/WebRtcProviderSessionWatch.ts:134-155
// at 0bd06f2.
func (w *sessionWatch) report(nodeID uint64, endpoints []uint16, changed map[uint16]bool) {
	local := w.io.localNodeID()
	for _, ep := range endpoints {
		sessions, ok := w.io.currentSessions(nodeID, ep)
		if !ok {
			continue
		}
		ours := map[uint16]bool{}
		for _, s := range sessions {
			if s.peerNodeID == local {
				ours[s.id] = true
			}
		}
		restarted := changed[ep] && w.useRestartMark(nodeID, ep)
		func() {
			defer func() {
				if r := recover(); r != nil {
					w.log.Warn("camera.watch.report_failed", slog.Uint64("node", nodeID), slog.Int("endpoint", int(ep)), slog.Any("panic", r))
				}
			}()
			w.io.reported(nodeID, ep, ours, restarted)
		}()
		w.sweep(nodeID, ep, ours)
	}
}

// useRestartMark consumes the node's StartUp mark for the endpoint.
// Mirrors matterjs-server packages/ws-controller/src/controller/WebRtcProviderSessionWatch.ts:157-167
// at 0bd06f2.
func (w *sessionWatch) useRestartMark(nodeID uint64, ep uint16) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	r := w.restarts[nodeID]
	if r == nil {
		return false
	}
	if !w.clk.Now().Before(r.until) {
		delete(w.restarts, nodeID)
		return false
	}
	if r.used[ep] {
		return false
	}
	r.used[ep] = true
	return true
}

// endpointAdded makes a later batch end look for provider endpoints
// again. Mirrors matterjs-server
// packages/ws-controller/src/controller/WebRtcProviderSessionWatch.ts:169-172
// at 0bd06f2.
func (w *sessionWatch) endpointAdded(nodeID uint64) {
	w.mu.Lock()
	defer w.mu.Unlock()
	delete(w.nodesSwept, nodeID)
}

// sweepAgain makes the next batch end sweep the endpoint again, for a
// session this controller gave up and could not end. Mirrors
// matterjs-server packages/ws-controller/src/controller/WebRtcProviderSessionWatch.ts:174-180
// at 0bd06f2.
func (w *sessionWatch) sweepAgain(nodeID uint64, ep uint16) {
	w.mu.Lock()
	defer w.mu.Unlock()
	k := watchKey{nodeID, ep}
	delete(w.swept, k)
	delete(w.failed, k)
	delete(w.nodesSwept, nodeID)
}

// nodeRemoved forgets the node. Mirrors matterjs-server
// packages/ws-controller/src/controller/WebRtcProviderSessionWatch.ts:182-194
// at 0bd06f2.
func (w *sessionWatch) nodeRemoved(nodeID uint64) {
	w.mu.Lock()
	defer w.mu.Unlock()
	delete(w.changed, nodeID)
	delete(w.restarts, nodeID)
	delete(w.nodesSwept, nodeID)
	for k := range w.swept {
		if k.nodeID == nodeID {
			delete(w.swept, k)
		}
	}
	for k := range w.failed {
		if k.nodeID == nodeID {
			delete(w.failed, k)
		}
	}
}

// sweep ends, once per endpoint and run, the sessions the camera holds for
// this controller that no local record names. Mirrors matterjs-server
// packages/ws-controller/src/controller/WebRtcProviderSessionWatch.ts:196-222
// at 0bd06f2.
func (w *sessionWatch) sweep(nodeID uint64, ep uint16, ours map[uint16]bool) {
	k := watchKey{nodeID, ep}
	w.mu.Lock()
	if w.swept[k] || w.establishing[k] > 0 {
		w.mu.Unlock()
		return
	}
	if f := w.failed[k]; f != nil && w.clk.Now().Before(f.retryAt) {
		w.mu.Unlock()
		return
	}
	w.swept[k] = true
	w.mu.Unlock()
	tracked := w.io.requestorSessionIDs(nodeID, ep)
	var leftovers []uint16
	for id := range ours {
		if !tracked[id] {
			leftovers = append(leftovers, id)
		}
	}
	if len(leftovers) == 0 {
		return
	}
	slices.Sort(leftovers)
	w.work.Add(1)
	go func() {
		defer w.work.Done()
		failed := false
		var wg sync.WaitGroup
		var fmu sync.Mutex
		for _, id := range leftovers {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if err := w.endLeftover(nodeID, ep, id); err != nil {
					w.log.Warn("camera.sweep.end_failed", slog.Uint64("node", nodeID), slog.Int("session", int(id)), slog.Any("err", err))
					fmu.Lock()
					failed = true
					fmu.Unlock()
				}
			}()
		}
		wg.Wait()
		if failed {
			w.sweepFailed(nodeID, ep)
		}
	}()
}

// sweepFailed schedules the next sweep of the endpoint, or leaves it after
// maxFailedLeftoverSweeps. Mirrors matterjs-server
// packages/ws-controller/src/controller/WebRtcProviderSessionWatch.ts:224-240
// at 0bd06f2.
func (w *sessionWatch) sweepFailed(nodeID uint64, ep uint16) {
	w.mu.Lock()
	defer w.mu.Unlock()
	k := watchKey{nodeID, ep}
	// Node removal or sweepAgain reset the endpoint while the EndSession
	// ran.
	if !w.swept[k] {
		return
	}
	count := 1
	if f := w.failed[k]; f != nil {
		count = f.count + 1
	}
	if count >= maxFailedLeftoverSweeps {
		delete(w.failed, k)
		w.log.Warn("camera.sweep.given_up", slog.Uint64("node", nodeID), slog.Int("endpoint", int(ep)), slog.Int("sweeps", count))
		return
	}
	w.failed[k] = &failedSweeps{count: count, retryAt: w.clk.Now().Add(leftoverSweepRetryDelay)}
	delete(w.swept, k)
	delete(w.nodesSwept, nodeID)
}

// endLeftover ends one leftover; NOT_FOUND is a session already gone.
// Mirrors matterjs-server packages/ws-controller/src/controller/WebRtcProviderSessionWatch.ts:242-251
// at 0bd06f2.
func (w *sessionWatch) endLeftover(nodeID uint64, ep, id uint16) error {
	if err := w.io.endSession(w.ctx, nodeID, ep, id); err != nil {
		if deviceForgotSession(err) {
			return nil
		}
		return err
	}
	w.log.Info("camera.sweep.ended", slog.Uint64("node", nodeID), slog.Int("endpoint", int(ep)), slog.Int("session", int(id)))
	return nil
}

// wait waits for the sweeps in flight.
func (w *sessionWatch) wait() { w.work.Wait() }

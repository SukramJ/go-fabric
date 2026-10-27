// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package softwareupdate

import (
	"errors"
	"sync"
	"sync/atomic"
)

// UpdateEventKind is what an [UpdateEvent] reports.
type UpdateEventKind uint8

// The kinds of an [UpdateEvent]. Available, Applying, Done and Failed are
// matter.js's SoftwareUpdateManager events updateAvailable, updateApplying,
// updateDone and updateFailed (packages/node/src/behavior/system/
// software-update/SoftwareUpdateManager.ts:1420-1438 at 862ad75f), emitted
// where matter.js emits them (:886; :1317; :829, :1279; :878, :1123).
// ConsentChanged, CheckFinished, Queued and TransferStarted are this
// module's own: matter.js emits nothing when a consent is added or
// removed, when a check ends, when #queueUpdate queues an entry (:892-920)
// or when onOtaStatusChange first sees Downloading (:1253-1321). One
// update reports Queued, TransferStarted, Applying, then Done or Failed.
const (
	// UpdateAvailable: a check found an update for a node the manager
	// holds no consent for (requestConsentForUpdate, :885-887); Info
	// carries its details. AddUpdateConsent grants it.
	UpdateAvailable UpdateEventKind = iota + 1
	// UpdateConsentChanged: a consent was added or replaced
	// (AddUpdateConsent) or removed (RemoveConsent); Consented says which.
	// Go-only.
	UpdateConsentChanged
	// UpdateCheckFinished: a check for updates ended (QueryUpdates, and so
	// CheckForUpdates and the periodic check); Updates are the nodes it
	// found an update for, Err the errors of the products whose check
	// failed and was passed over (QueryUpdates itself goes on, as
	// queryUpdates does, :559-623). Peer is the node of a check of one
	// node, zero otherwise. Go-only.
	UpdateCheckFinished
	// UpdateApplying: the node was allowed to apply the image it
	// downloaded (onOtaStatusChange, :1312-1318).
	UpdateApplying
	// UpdateDone: the node runs the target version (:829, :1279).
	UpdateDone
	// UpdateFailed: the update failed or was cancelled (:878, :1123); Err
	// is [ErrNotApplied] or [ErrUpdateCancelled].
	UpdateFailed
	// UpdateQueued: an update entered the queue (#queueUpdate, :892-920)
	// — a consent that queues, ForceUpdate, or a check that found an
	// update with a consent held — or a queued entry not yet in progress
	// took a different target; not when a periodic check queues the same
	// target again. Go-only.
	UpdateQueued
	// UpdateTransferStarted: the node began downloading the image — the
	// entry's first Downloading status, which the provider reports when
	// the node's BDX session starts; a retry's new session does not
	// report it again. Go-only.
	UpdateTransferStarted
)

// String is the kind's name: available, consent_changed, check_finished,
// applying, done, failed, queued, transfer_started.
func (k UpdateEventKind) String() string {
	switch k {
	case UpdateAvailable:
		return "available"
	case UpdateConsentChanged:
		return "consent_changed"
	case UpdateCheckFinished:
		return "check_finished"
	case UpdateApplying:
		return "applying"
	case UpdateDone:
		return "done"
	case UpdateFailed:
		return "failed"
	case UpdateQueued:
		return "queued"
	case UpdateTransferStarted:
		return "transfer_started"
	}
	return "unknown"
}

// ErrNotApplied is the Err of an [UpdateFailed] whose node came back from
// its restart below the target version (#failAppliedUpdate,
// SoftwareUpdateManager.ts:875-880 at 862ad75f).
var ErrNotApplied = errors.New("softwareupdate: the node did not apply the update")

// ErrUpdateCancelled is the Err of an [UpdateFailed] whose update was
// cancelled before it applied — its consent removed (#cancelUpdate,
// :1091-1123).
var ErrUpdateCancelled = errors.New("softwareupdate: the update was cancelled")

// UpdateEvent is one report of [Manager.Observe].
type UpdateEvent struct {
	Kind UpdateEventKind
	// Peer is the node; zero for an [UpdateCheckFinished] of every node.
	Peer Peer
	// Target is the update: the found version of UpdateAvailable, the
	// consent's of UpdateConsentChanged, the queued entry's otherwise.
	Target Target
	// Info is the found update of UpdateAvailable.
	Info SoftwareUpdateInfo
	// Consented, for UpdateConsentChanged, is whether the manager holds
	// the consent now.
	Consented bool
	// Updates are the nodes an UpdateCheckFinished found an update for.
	Updates []PeerUpdate
	// Err is the error of UpdateCheckFinished and UpdateFailed.
	Err error
}

type observer struct {
	fn     func(UpdateEvent)
	active atomic.Bool
}

// observers deliver the manager's events in order on a goroutine of their
// own, never under the manager's lock.
type observers struct {
	mu      sync.Mutex
	list    []*observer
	queue   []UpdateEvent
	running bool
	closed  bool
	wg      sync.WaitGroup
}

// Observe calls fn with every [UpdateEvent] from now on, until the
// returned unsubscribe is called; unsubscribe is idempotent, and no event
// is delivered to fn after it returned except the one a running call is
// delivering. The events are delivered in the order the manager's state
// moved, one at a time, on a goroutine of the manager's that holds none
// of its locks — fn may call back into the manager (QueuedUpdates,
// HasConsent), but must not call Close, which waits for the delivery of
// the events already emitted. After Close nothing is delivered.
func (m *Manager) Observe(fn func(UpdateEvent)) (unsubscribe func()) {
	o := &observer{fn: fn}
	o.active.Store(true)
	m.obs.mu.Lock()
	m.obs.list = append(m.obs.list, o)
	m.obs.mu.Unlock()
	return func() {
		if !o.active.Swap(false) {
			return
		}
		m.obs.mu.Lock()
		for i, x := range m.obs.list {
			if x == o {
				m.obs.list = append(m.obs.list[:i:i], m.obs.list[i+1:]...)
				break
			}
		}
		m.obs.mu.Unlock()
	}
}

// emit queues ev for the observers; with none, or once closed, it is
// dropped.
func (m *Manager) emit(ev UpdateEvent) {
	o := &m.obs
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.closed || len(o.list) == 0 {
		return
	}
	o.queue = append(o.queue, ev)
	if !o.running {
		o.running = true
		o.wg.Add(1)
		go o.deliver()
	}
}

// deliver hands the queued events to the observers subscribed when each
// is taken, until the queue is empty.
func (o *observers) deliver() {
	defer o.wg.Done()
	for {
		o.mu.Lock()
		if len(o.queue) == 0 {
			o.running = false
			o.mu.Unlock()
			return
		}
		ev := o.queue[0]
		o.queue[0] = UpdateEvent{}
		o.queue = o.queue[1:]
		list := o.list
		o.mu.Unlock()
		for _, x := range list {
			if x.active.Load() {
				x.fn(ev)
			}
		}
	}
}

// close drops later events and waits for the delivery of the queued ones.
func (o *observers) close() {
	o.mu.Lock()
	o.closed = true
	o.mu.Unlock()
	o.wg.Wait()
}

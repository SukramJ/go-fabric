// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package icd

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"sync"
	"time"

	"github.com/SukramJ/go-fabric/clock"
	"github.com/SukramJ/go-fabric/icd/checkin"
)

// ErrUnknownPeer reports a peer the table holds no registration of —
// matter.js's ImplementationError of FabricIcd.ts:138-144.
var ErrUnknownPeer = errors.New("icd: ICD peer is not registered with this fabric")

// Peer is a registered ICD peer of the controller with its key and the
// counter state Check-Ins are validated against. Mirrors matter.js
// FabricIcd.ts:289-297 (FabricIcd.Peer).
type Peer struct {
	PeerNodeID   uint64
	Key          []byte
	CounterStart uint32
	LastOffset   uint32
	// PendingKey is the key a refresh offered the peer and nobody
	// confirmed yet; nil for none.
	PendingKey []byte
}

func (p Peer) clone() Peer {
	p.Key = bytes.Clone(p.Key)
	p.PendingKey = bytes.Clone(p.PendingKey)
	return p
}

// ReceivedCheckIn is what a peer's handler receives for a Check-In that
// decrypted and was fresh (FabricIcd.ts:300-304).
type ReceivedCheckIn struct {
	PeerNodeID          uint64
	Counter             uint32
	ActiveModeThreshold uint16
}

// CheckInHandler is a registered peer's Check-In handler (FabricIcd.ts:306).
type CheckInHandler func(ReceivedCheckIn)

type peerEntry struct {
	peer    Peer
	handler CheckInHandler
}

// PeerTable is the controller role of one fabric's ICD state: the
// registered peers with their keys and counter state, one
// [PeerWakefulness] per peer for its lifetime on the fabric, and the
// trial decryption of a received Check-In. It is runtime state; the ICD
// client persists the registrations and feeds them back on start
// (FabricIcd.ts:16-23). Safe for concurrent use.
//
// Mirrors matter.js packages/protocol/src/icd/FabricIcd.ts:24-273, the
// members of the controller role (:62-272).
type PeerTable struct {
	clock  clock.Clock
	logger *slog.Logger

	// mu guards peers, order, wakefulness and fed. The handler, the
	// wakefulness and the fed observers run outside it.
	mu          sync.Mutex
	peers       map[uint64]*peerEntry
	order       []uint64 // registration order, as matter.js's Map iterates
	wakefulness map[uint64]*PeerWakefulness
	fed         map[uint64]func(peerNodeID uint64)
	nextFed     uint64
}

// NewPeerTable returns an empty table whose wakefulness timers run on c
// (nil: the system clock); nil logger is slog.Default.
func NewPeerTable(c clock.Clock, logger *slog.Logger) *PeerTable {
	if logger == nil {
		logger = slog.Default()
	}
	return &PeerTable{
		clock: clock.Or(c), logger: logger,
		peers: map[uint64]*peerEntry{}, wakefulness: map[uint64]*PeerWakefulness{}, fed: map[uint64]func(uint64){},
	}
}

// OnPeerFed registers fn for the start of a peer's registration
// ([PeerTable.AddPeer] of an unregistered peer), the moment
// WakefulnessFor starts returning the peer's wakefulness: a sustained
// subscription running without one races this signal
// (FabricIcd.ts:35-42, peerFed). It returns the unsubscribe function.
func (t *PeerTable) OnPeerFed(fn func(peerNodeID uint64)) (unsubscribe func()) {
	t.mu.Lock()
	id := t.nextFed
	t.nextFed++
	t.fed[id] = fn
	t.mu.Unlock()
	return func() {
		t.mu.Lock()
		delete(t.fed, id)
		t.mu.Unlock()
	}
}

// AddPeer registers a peer's key and handler. A peer keeps one
// wakefulness for its lifetime on the fabric, so a repeated registration
// updates the key and handler and resumes the wakefulness with its
// history; a new registration resumes it and announces the peer
// (FabricIcd.ts:66-78).
func (t *PeerTable) AddPeer(p Peer, handler CheckInHandler) {
	t.mu.Lock()
	_, was := t.peers[p.PeerNodeID]
	t.peers[p.PeerNodeID] = &peerEntry{peer: p.clone(), handler: handler}
	if !was {
		t.order = append(t.order, p.PeerNodeID)
	}
	w := t.wakefulness[p.PeerNodeID]
	if w == nil {
		w = NewPeerWakefulness(t.clock)
		t.wakefulness[p.PeerNodeID] = w
	}
	var fed []func(uint64)
	if !was {
		fed = slices.Collect(maps.Values(t.fed))
	}
	t.mu.Unlock()
	if !was {
		w.Resume()
		for _, fn := range fed {
			fn(p.PeerNodeID)
		}
	}
}

// SetPendingKey sets the key a refresh offers the peer: until confirmed,
// Check-Ins decrypt under the current key or this one, the peer may have
// taken it without the confirmation reaching the controller
// (FabricIcd.ts:86-88, Core §4.22.3.4, §9.16.7.1).
func (t *PeerTable) SetPendingKey(peerNodeID uint64, key []byte) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	e, ok := t.peers[peerNodeID]
	if !ok {
		return fmt.Errorf("%w: %#x", ErrUnknownPeer, peerNodeID)
	}
	e.peer.PendingKey = bytes.Clone(key)
	return nil
}

// RefreshNeeded reports whether the last Check-In accepted under the
// peer's current key requires a key refresh (FabricIcd.ts:96-99).
func (t *PeerTable) RefreshNeeded(peerNodeID uint64) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	e, ok := t.peers[peerNodeID]
	return ok && checkin.RefreshNeededAt(e.peer.LastOffset)
}

// ConfirmKey applies the starting counter a refresh response reported for
// key: a pending key replaces the current one starting at counterStart; a
// key a Check-In promoted already keeps its last counter, measured from
// counterStart, unless that counter lies before counterStart; any other
// key changes nothing and reports false (FabricIcd.ts:113-136).
func (t *PeerTable) ConfirmKey(peerNodeID uint64, key []byte, counterStart uint32) (bool, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	e, ok := t.peers[peerNodeID]
	if !ok {
		return false, fmt.Errorf("%w: %#x", ErrUnknownPeer, peerNodeID)
	}
	p := &e.peer
	if p.PendingKey != nil && bytes.Equal(p.PendingKey, key) {
		p.Key, p.PendingKey, p.CounterStart, p.LastOffset = bytes.Clone(key), nil, counterStart, 0
		return true, nil
	}
	if !bytes.Equal(p.Key, key) {
		return false, nil
	}
	last := p.CounterStart + p.LastOffset
	v := checkin.ValidateCounter(last, counterStart, 0)
	if v.RefreshNeeded {
		t.logger.Info("icd.peer.keep_checkin_baseline", slog.Uint64("node_id", peerNodeID),
			slog.Uint64("counter_start", uint64(counterStart)), slog.Uint64("last_counter", uint64(last)))
		return true, nil
	}
	p.CounterStart, p.LastOffset = counterStart, v.Offset
	return true, nil
}

// PeerFor returns a copy of the peer's registration (FabricIcd.ts:146-148).
func (t *PeerTable) PeerFor(peerNodeID uint64) (Peer, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	e, ok := t.peers[peerNodeID]
	if !ok {
		return Peer{}, false
	}
	return e.peer.clone(), true
}

// WakefulnessFor returns a registered peer's wakefulness, nil for a peer
// not registered (FabricIcd.ts:151-153).
func (t *PeerTable) WakefulnessFor(peerNodeID uint64) *PeerWakefulness {
	t.mu.Lock()
	defer t.mu.Unlock()
	if _, ok := t.peers[peerNodeID]; !ok {
		return nil
	}
	return t.wakefulness[peerNodeID]
}

// DeletePeer removes a peer's registration; its wakefulness is suspended
// until the peer registers again (FabricIcd.ts:159-163).
func (t *PeerTable) DeletePeer(peerNodeID uint64) {
	t.mu.Lock()
	_, ok := t.peers[peerNodeID]
	w := t.wakefulness[peerNodeID]
	t.deleteLocked(peerNodeID)
	t.mu.Unlock()
	if ok && w != nil {
		w.Suspend()
	}
}

// RemovePeer removes a peer that left the fabric, with its wakefulness; a
// later peer of the same node id starts afresh (FabricIcd.ts:166-170).
func (t *PeerTable) RemovePeer(peerNodeID uint64) {
	t.mu.Lock()
	w := t.wakefulness[peerNodeID]
	t.deleteLocked(peerNodeID)
	delete(t.wakefulness, peerNodeID)
	t.mu.Unlock()
	if w != nil {
		w.Close()
	}
}

func (t *PeerTable) deleteLocked(peerNodeID uint64) {
	if _, ok := t.peers[peerNodeID]; !ok {
		return
	}
	delete(t.peers, peerNodeID)
	t.order = slices.DeleteFunc(t.order, func(n uint64) bool { return n == peerNodeID })
}

// NotePeerActive records a message received from the peer
// (FabricIcd.ts:173-175).
func (t *PeerTable) NotePeerActive(peerNodeID uint64) {
	t.mu.Lock()
	w := t.wakefulness[peerNodeID]
	t.mu.Unlock()
	if w != nil {
		w.NoteActive()
	}
}

// HasPeers reports a registered peer (FabricIcd.ts:177-179).
func (t *PeerTable) HasPeers() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.peers) > 0
}

// Close tears the table down for shutdown: every wakefulness's timers stop
// and the peers go; nothing is unpersisted (FabricIcd.ts:185-192).
func (t *PeerTable) Close() {
	t.mu.Lock()
	ws := slices.Collect(maps.Values(t.wakefulness))
	clear(t.wakefulness)
	clear(t.peers)
	t.order = nil
	t.mu.Unlock()
	for _, w := range ws {
		w.Close()
	}
}

// ProcessCheckIn trial-decrypts a Check-In against every registered
// peer's key and pending key. Under the current key a counter that is not
// fresh is a replay and dropped; the first Check-In under a pending key
// proves the peer took it, so the pending key replaces the current one,
// counting from the last counter accepted under the old key (modulo 2^32),
// a counter at or past 2^31 from it being an old one. A fresh counter is
// consumed before the handler runs: lastOffset advances, the peer's
// wakefulness notes the Check-In with the threshold it carried, and the
// handler is called. It reports whether a key matched, a dropped replay
// included; a decryption failure under every key is false.
//
// Mirrors matter.js FabricIcd.ts:207-272 (processCheckIn, #decrypt; Core
// §4.22.4.2). An error that is not a failed decryption — a key that is not
// 16 bytes — is returned, as matter.js rethrows it (:266-268).
func (t *PeerTable) ProcessCheckIn(payload []byte) (bool, error) {
	t.mu.Lock()
	order := slices.Clone(t.order)
	t.mu.Unlock()
	for _, id := range order {
		t.mu.Lock()
		e, ok := t.peers[id]
		var p Peer
		if ok {
			p = e.peer.clone()
		}
		t.mu.Unlock()
		if !ok {
			continue
		}
		counter, threshold, key, decoded, err := t.decrypt(p, payload)
		if err != nil {
			return false, err
		}
		if !decoded {
			continue
		}
		t.mu.Lock()
		e, ok = t.peers[id]
		if !ok {
			t.mu.Unlock()
			continue
		}
		cur := &e.peer
		var v checkin.Validation
		switch {
		case bytes.Equal(key, cur.Key):
			v = checkin.ValidateCounter(counter, cur.CounterStart, cur.LastOffset)
			if !v.Valid {
				t.mu.Unlock()
				t.logger.Info("icd.peer.replayed_checkin", slog.Uint64("node_id", id))
				return true, nil
			}
		case cur.PendingKey != nil && bytes.Equal(key, cur.PendingKey):
			// The ICD counter spans keys: the last counter accepted under
			// the old key bounds the peer's counter when it took the new one.
			start := cur.CounterStart + cur.LastOffset
			v = checkin.ValidateCounter(counter, start, 0)
			if !v.Valid || v.RefreshNeeded {
				t.mu.Unlock()
				t.logger.Info("icd.peer.replayed_checkin", slog.Uint64("node_id", id))
				return true, nil
			}
			t.logger.Info("icd.peer.refreshed_key_checkin", slog.Uint64("node_id", id))
			cur.Key, cur.PendingKey, cur.CounterStart = bytes.Clone(key), nil, start
		default:
			// The peer was re-keyed while this payload decrypted.
			t.mu.Unlock()
			continue
		}
		cur.LastOffset = v.Offset
		w := t.wakefulness[id]
		handler := e.handler
		t.mu.Unlock()
		if w != nil {
			d := time.Duration(threshold) * time.Millisecond
			w.NoteCheckIn(&d)
		}
		if handler != nil {
			t.runHandler(handler, ReceivedCheckIn{PeerNodeID: id, Counter: counter, ActiveModeThreshold: threshold})
		}
		return true, nil
	}
	return false, nil
}

// runHandler keeps a handler's panic from escaping ProcessCheckIn
// (FabricIcd.ts:244-252, a handler error logged).
func (t *PeerTable) runHandler(h CheckInHandler, c ReceivedCheckIn) {
	defer func() {
		if r := recover(); r != nil {
			t.logger.Warn("icd.peer.checkin_handler_failed", slog.Uint64("node_id", c.PeerNodeID), slog.Any("panic", r))
		}
	}()
	h(c)
}

// decrypt tries the peer's key, then its pending key (FabricIcd.ts:259-272).
func (*PeerTable) decrypt(p Peer, payload []byte) (counter uint32, threshold uint16, key []byte, ok bool, err error) {
	for _, k := range [][]byte{p.Key, p.PendingKey} {
		if k == nil {
			continue
		}
		c, th, derr := checkin.DecodeICD(k, payload)
		if derr == nil {
			return c, th, k, true, nil
		}
		if !errors.Is(derr, checkin.ErrMalformed) {
			return 0, 0, nil, false, derr
		}
	}
	return 0, 0, nil, false, nil
}

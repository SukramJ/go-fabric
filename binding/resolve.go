// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package binding

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"github.com/SukramJ/go-fabric/client/exchange"
	"github.com/SukramJ/go-fabric/client/handshake"
	"github.com/SukramJ/go-fabric/client/interaction"
	"github.com/SukramJ/go-fabric/client/peer"
	"github.com/SukramJ/go-fabric/schema"
)

// Everything in this file runs on the worker goroutine.

// record returns (creating) the record of a source endpoint — matter.js
// #record (BindingManager.ts:125-133).
func (m *Manager) record(source uint16) *record {
	m.stateMu.Lock()
	defer m.stateMu.Unlock()
	rec := m.records[source]
	if rec == nil {
		rec = &record{
			pending: map[string]*pendingEntry{}, established: map[string]Resolution{},
			dormant: map[string]*item{},
		}
		m.records[source] = rec
	}
	return rec
}

// forgetIfEmptyLocked drops an empty record (#forgetIfEmpty, :159-163).
// Called with stateMu held.
func (m *Manager) forgetIfEmptyLocked(source uint16) {
	if rec := m.records[source]; rec != nil && rec.empty() {
		delete(m.records, source)
	}
}

func (m *Manager) entryAttrs(it *item) []any {
	e := it.entry
	return []any{
		slog.Int("source_endpoint", int(it.source)), slog.Int("fabric_index", int(e.FabricIndex)),
		slog.Uint64("node_id", e.Node), slog.Int("group_id", int(e.Group)),
		slog.Int("endpoint", int(e.Endpoint)), slog.Uint64("cluster", uint64(e.Cluster)),
	}
}

// drop warns that an entry is ignored, with matter.js's reason.
func (m *Manager) drop(reason string, it *item, extra ...any) {
	m.log.Warn("binding.entry.ignored", append(append([]any{slog.String("reason", reason)}, m.entryAttrs(it)...), extra...)...)
}

func (m *Manager) info(msg, reason string, it *item, extra ...any) {
	m.log.Info(msg, append(append([]any{slog.String("reason", reason)}, m.entryAttrs(it)...), extra...)...)
}

// resolveAndEmit resolves one entry and emits established for it, or
// drops it with a warning, or leaves it waiting — matter.js
// #resolveAndEmit (BindingManager.ts:255-353), in its order.
func (m *Manager) resolveAndEmit(it *item) {
	clients, isOurNode, ok := m.admit(it)
	if !ok || m.parkUnlessListened(it) { // :303-305
		return
	}
	e := it.entry
	topo := m.cfg.Topology
	res := Resolution{Entry: e, SourceEndpoint: it.source, Clients: clients}
	switch {
	case e.HasGroup(): // :308-321
		if !topo.HasGroupKey(e.FabricIndex, e.Group) {
			m.info("binding.group.waiting", reasonGroupWaitsForKey, it)
			return
		}
		target, ok := m.resolveGroup(it)
		if !ok {
			return
		}
		res.Kind, res.Group, res.Endpoint = KindGroup, target, it.source
		// An unregister or a withdrawn key may have come in meanwhile
		// (:348-351).
		if !m.isRegisteredGroup(it) || !topo.HasGroupKey(e.FabricIndex, e.Group) {
			return
		}
		m.emitEstablished(it, res)
	case isOurNode: // :322-328
		res.Kind, res.Endpoint = KindServer, e.Endpoint
		m.emitEstablished(it, res)
	default: // :329-345
		if m.cfg.Peers == nil {
			m.drop(reasonClientStackDisabled, it)
			return
		}
		addr := handshake.PeerAddress{FabricIndex: e.FabricIndex, NodeID: e.Node}
		// Has then For are two calls — client/peer.Set offers no atomic
		// "for, and did you create it": a host that adds the node's peer
		// between them has it counted as the manager's, whose release then
		// stops a connection process the host may have started. Narrow,
		// and the peer and its sessions stay (decision 221).
		set, tracks := m.cfg.Peers.(peerSet)
		creates := tracks && !set.Has(addr)
		p, err := m.cfg.Peers.For(addr)
		if err != nil || p == nil {
			if err == nil {
				err = fmt.Errorf("binding: no peer for %d:%d", e.FabricIndex, e.Node)
			}
			m.drop(reasonClientPeerFailed, it, slog.Any("err", err))
			return
		}
		m.trackCreated(addr, p, creates)
		res.Kind, res.Peer, res.Endpoint = KindClient, p, e.Endpoint
		res.Interaction = interaction.ForPeer(p, m.cfg.Interaction)
		m.establishClientKind(it, res)
	}
}

// admit holds an entry to the checks before its resolution and drops it
// with matter.js's reason where one fails (BindingManager.ts:258-301): it
// returns the clients the entry directs and whether it names our own
// node.
func (m *Manager) admit(it *item) (clients []uint32, isOurNode, ok bool) {
	e := it.entry
	topo := m.cfg.Topology
	hasNode, hasGroup := e.HasNode(), e.HasGroup()
	if hasNode == hasGroup { // :258-263
		m.drop(reasonNodeXorGroup, it)
		return nil, false, false
	}
	clients, unbindable := selectClientClusters(topo.ClientClusters(it.source), e) // :265-272
	if len(clients) == 0 {
		m.drop(ignoredEntryReason(it.source, e.Cluster, e.HasCluster(), unbindable), it)
		return nil, false, false
	}
	ourID, fabricKnown := topo.NodeID(e.FabricIndex)
	isOurNode = hasNode && fabricKnown && ourID == e.Node // :274, :645-650
	switch {
	case hasGroup: // :275-282
		if !fabricKnown {
			m.drop(reasonGroupFabricUnknown, it)
			return nil, false, false
		}
		if !m.isRegisteredGroup(it) {
			return nil, false, false
		}
	case isOurNode: // :283-297
		if !e.HasEndpoint() || !topo.HasEndpoint(e.Endpoint) {
			m.drop(reasonSelfNoEndpoint, it)
			return nil, false, false
		}
		if e.HasCluster() && !topo.HasServer(e.Endpoint, e.Cluster) {
			m.drop(reasonSelfNoServer, it)
			return nil, false, false
		}
	case !e.HasEndpoint(): // :298-301
		m.drop(reasonClientNoEndpoint, it)
		return nil, false, false
	}
	return clients, isOurNode, true
}

// selectClientClusters are the declared clients an entry directs: those
// of the cluster the entry names, or all without one, split into the
// bindable ones and those left out as not bindable — matter.js
// #selectClientClusters (BindingManager.ts:556-577).
func selectClientClusters(declared []uint32, e Entry) (clients, unbindable []uint32) {
	for _, c := range declared {
		if e.HasCluster() && c != e.Cluster {
			continue
		}
		if schema.ClusterBindable(c) {
			clients = append(clients, c)
		} else {
			unbindable = append(unbindable, c)
		}
	}
	return clients, unbindable
}

// observed reports whether something observes established.
func (m *Manager) observed() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.established) > 0
}

// parkUnlessListened keeps an entry dormant while nothing observes
// established, so no peer is registered and no CASE session opened; it
// reports whether it parked the entry — matter.js #parkUnlessListened
// (BindingManager.ts:385-424), the info logged once per entry.
func (m *Manager) parkUnlessListened(it *item) bool {
	if m.observed() {
		return false
	}
	rec := m.record(it.source)
	m.stateMu.Lock()
	_, known := rec.dormant[it.key]
	rec.dormant[it.key] = it
	m.stateMu.Unlock()
	if !known {
		m.info("binding.entry.dormant", reasonDormant, it)
	}
	return true
}

// wakeAll resolves every dormant entry once something observes
// established — matter.js #wake (BindingManager.ts:426-438), for every
// record.
func (m *Manager) wakeAll() {
	m.stateMu.Lock()
	var items []*item
	for ep, rec := range m.records {
		for _, it := range rec.dormant {
			items = append(items, it)
		}
		clear(rec.dormant)
		m.forgetIfEmptyLocked(ep)
	}
	m.stateMu.Unlock()
	slices.SortFunc(items, func(a, b *item) int {
		switch {
		case a.seq < b.seq:
			return -1
		case a.seq > b.seq:
			return 1
		default:
			return 0
		}
	})
	for _, it := range items {
		m.resolveAndEmit(it)
	}
}

// clearDormant forgets a dormant entry (#clearDormant, :440-450).
func (m *Manager) clearDormant(source uint16, key string) bool {
	m.stateMu.Lock()
	defer m.stateMu.Unlock()
	rec := m.records[source]
	if rec == nil {
		return false
	}
	if _, ok := rec.dormant[key]; !ok {
		return false
	}
	delete(rec.dormant, key)
	m.forgetIfEmptyLocked(source)
	return true
}

// errNoGroupSender and errNoGroupTarget are the reasons a group entry
// is dropped without an error of the sender's: the node has none, or it
// answered no target.
var (
	errNoGroupSender = errors.New("binding: the node has no group sender")
	errNoGroupTarget = errors.New("binding: no group sender")
)

// resolveGroup is a group entry's sender — matter.js #resolveGroup
// (BindingManager.ts:452-474).
func (m *Manager) resolveGroup(it *item) (GroupTarget, bool) {
	if m.cfg.Groups == nil {
		m.drop(reasonGroupPeerFailed, it, slog.Any("err", errNoGroupSender))
		return nil, false
	}
	target, err := m.cfg.Groups.Sender(m.life, it.entry.FabricIndex, it.entry.Group)
	if err != nil || target == nil {
		if err == nil {
			err = errNoGroupTarget
		}
		m.drop(reasonGroupPeerFailed, it, slog.Any("err", err))
		return nil, false
	}
	return target, true
}

// isRegisteredGroup reports whether it is its server's registered group
// entry and not yet established — matter.js #isRegisteredGroup
// (BindingManager.ts:528-532). The registration is the one of the time of
// the call, so an unregister or a disposal that came in while the entry
// resolved makes it false.
func (m *Manager) isRegisteredGroup(it *item) bool {
	m.mu.Lock()
	registered := m.registered[regKey{source: it.source, key: it.key}] == it
	m.mu.Unlock()
	if !registered {
		return false
	}
	m.stateMu.Lock()
	defer m.stateMu.Unlock()
	if rec := m.records[it.source]; rec != nil {
		if _, est := rec.established[it.key]; est {
			return false
		}
	}
	return true
}

// recheckGroups resolves the registered group entries of a fabric whose
// group now holds a key; an established one stays — matter.js
// #recheckGroups (BindingManager.ts:522-541).
func (m *Manager) recheckGroups(fabricIndex uint8) {
	m.mu.Lock()
	var items []*item
	for _, it := range m.registered {
		if it.entry.FabricIndex == fabricIndex {
			items = append(items, it)
		}
	}
	m.mu.Unlock()
	slices.SortFunc(items, func(a, b *item) int {
		if a.source != b.source {
			return int(a.source) - int(b.source)
		}
		return strings.Compare(a.key, b.key)
	})
	for _, it := range items {
		if m.isRegisteredGroup(it) && m.cfg.Topology.HasGroupKey(fabricIndex, it.entry.Group) {
			m.resolveAndEmit(it)
		}
	}
}

// establishClientKind connects to the bound peer in the background and
// emits established once it has a session — matter.js
// #establishClientKind (BindingManager.ts:585-627), the peer's session
// standing in for the client node's lifecycle.online: immediately when it
// has one, else on the next SessionAdded.
func (m *Manager) establishClientKind(it *item, res Resolution) {
	p := res.Peer
	m.info("binding.peer.connecting", reasonInitiatingCASE, it)
	ctx, period := m.life, (*bindingPeriod)(nil)
	m.stateMu.Lock()
	addr := handshake.PeerAddress{FabricIndex: it.entry.FabricIndex, NodeID: it.entry.Node}
	if c := m.created[addr]; c != nil && c.peer == p && c.period != nil {
		period, ctx = c.period, c.period.ctx
		// Counted on the worker, before the release can look: the release
		// waits for this call (releaseUnbound).
		period.calls.Add(1)
	}
	m.stateMu.Unlock()
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		if period != nil {
			defer period.calls.Done()
		}
		if ctx.Err() != nil {
			return // released before the call began
		}
		if err := m.link.connect(ctx, p); err != nil && ctx.Err() == nil {
			m.log.Debug("binding.peer.connect_failed", append([]any{slog.Any("err", err)}, m.entryAttrs(it)...)...)
		}
	}()

	pe := &pendingEntry{peer: p}
	online := func() { m.post(func() { m.onClientOnline(it, res, pe) }) }
	rec := m.record(it.source)
	m.stateMu.Lock()
	prev := rec.pending[it.key]
	rec.pending[it.key] = pe
	m.stateMu.Unlock()
	if prev != nil && prev.cancel != nil {
		prev.cancel()
	}
	pe.cancel = m.link.observe(p, online)
	if m.link.hasSession(p) {
		online()
	}
}

// onClientOnline emits established for a pending client entry whose peer
// has a session, unless the entry was unregistered or replaced meanwhile.
func (m *Manager) onClientOnline(it *item, res Resolution, pe *pendingEntry) {
	if !m.clearPending(it.source, it.key, pe) {
		return
	}
	m.emitEstablished(it, res)
}

// clearPending cancels a pending client entry — matter.js #clearPending
// (BindingManager.ts:629-643); with only set, only that one.
func (m *Manager) clearPending(source uint16, key string, only *pendingEntry) bool {
	m.stateMu.Lock()
	rec := m.records[source]
	if rec == nil {
		m.stateMu.Unlock()
		return false
	}
	pe := rec.pending[key]
	if pe == nil || (only != nil && pe != only) {
		m.stateMu.Unlock()
		return false
	}
	delete(rec.pending, key)
	m.forgetIfEmptyLocked(source)
	m.stateMu.Unlock()
	if pe.cancel != nil {
		pe.cancel()
	}
	return true
}

// emitEstablished records a resolution and emits established for it, or
// parks the entry again when nothing observes established any more —
// matter.js #emitEstablished (BindingManager.ts:355-383).
func (m *Manager) emitEstablished(it *item, res Resolution) {
	if m.parkUnlessListened(it) {
		return
	}
	rec := m.record(it.source)
	m.stateMu.Lock()
	rec.established[it.key] = res
	m.stateMu.Unlock()
	m.log.Debug("binding.established", append([]any{slog.String("reason", reasonEstablished), slog.String("kind", res.Kind.String())}, m.entryAttrs(it)...)...)
	m.mu.Lock()
	hs := slices.Clone(m.established)
	m.mu.Unlock()
	for _, h := range hs {
		m.call(h, res, reasonEstablishedFailed, it, slog.LevelWarn)
	}
}

// dropEstablished removes an established entry and emits removed for it
// (#dropEstablished, :165-205).
func (m *Manager) dropEstablished(source uint16, key string) {
	m.stateMu.Lock()
	rec := m.records[source]
	if rec == nil {
		m.stateMu.Unlock()
		return
	}
	res, ok := rec.established[key]
	if !ok {
		m.stateMu.Unlock()
		return
	}
	delete(rec.established, key)
	m.forgetIfEmptyLocked(source)
	m.stateMu.Unlock()
	m.emitRemoved(res, slog.LevelWarn)
}

// emitRemoved emits removed for one resolution.
func (m *Manager) emitRemoved(res Resolution, failLevel slog.Level) {
	it := &item{source: res.SourceEndpoint, entry: res.Entry}
	m.log.Debug("binding.removed", append([]any{slog.String("reason", reasonRemoved), slog.String("kind", res.Kind.String())}, m.entryAttrs(it)...)...)
	m.mu.Lock()
	hs := slices.Clone(m.removed)
	m.mu.Unlock()
	for _, h := range hs {
		m.call(h, res, reasonRemovedFailed, it, failLevel)
	}
}

// call runs one handler; a panic is logged with reason, not raised —
// matter.js catches a handler's error and logs it (:374-382, :196-204).
func (m *Manager) call(h *handler, res Resolution, reason string, it *item, level slog.Level) {
	defer func() {
		if r := recover(); r != nil {
			m.log.Log(m.life, level, "binding.handler.failed",
				append([]any{slog.String("reason", reason), slog.String("kind", res.Kind.String()), slog.Any("panic", r)}, m.entryAttrs(it)...)...)
		}
	}()
	h.fn(res)
}

// disposeServer forgets a source endpoint's entries and emits removed for
// each established one — matter.js disposeServer (BindingManager.ts:
// 207-244), whose handler failures log as errors.
func (m *Manager) disposeServer(source uint16) {
	m.stateMu.Lock()
	rec := m.records[source]
	if rec == nil {
		m.stateMu.Unlock()
		return
	}
	delete(m.records, source)
	var snapshot []Resolution
	for _, k := range sortedKeys(rec.established) {
		snapshot = append(snapshot, rec.established[k])
	}
	pendings := make([]*pendingEntry, 0, len(rec.pending))
	for _, pe := range rec.pending {
		pendings = append(pendings, pe)
	}
	m.stateMu.Unlock()
	for _, pe := range pendings {
		if pe.cancel != nil {
			pe.cancel()
		}
	}
	for _, res := range snapshot {
		m.emitRemoved(res, slog.LevelError)
	}
}

// trackCreated records p as the manager's when the set held no peer for
// addr before (creates), and opens a binding period for a recorded peer
// that has none. A record whose peer is no longer the set's (another
// peer at addr) is dropped.
func (m *Manager) trackCreated(addr handshake.PeerAddress, p *peer.Peer, creates bool) {
	m.stateMu.Lock()
	defer m.stateMu.Unlock()
	rec := m.created[addr]
	switch {
	case creates:
		rec = &createdPeer{peer: p}
		m.created[addr] = rec
	case rec != nil && rec.peer != p:
		delete(m.created, addr)
		return
	case rec == nil:
		return
	}
	if rec.period == nil {
		ctx, cancel := context.WithCancel(m.life)
		rec.period = &bindingPeriod{ctx: ctx, cancel: cancel}
	}
}

// releaseUnbound ends the binding period of every peer the manager
// created whose node no pending or established entry names any more:
// the entries' Connect calls are ended and waited for, then the peer's
// connection process is stopped. The peer stays in the set and is never
// closed, its sessions with it — another holder may use the same peer
// (owner decision 221) — and its record stays, so a later entry for the
// node opens a new period and is released again. It runs on the worker
// after every task, so an entry that went — unregistered, its server
// detached, parked again — stops the connecting it started. Ahead of
// matter.js, whose BindingManager keeps the peer it made for an entry
// (BindingManager.ts:584-585, #clearPending :613-627 at db021da0) with
// the connection process and the resolution that have no end of their
// own; the rule is this module's (review II, R2-P6-bound-2104, TM R3.4).
func (m *Manager) releaseUnbound() {
	m.stateMu.Lock()
	if len(m.created) == 0 {
		m.stateMu.Unlock()
		return
	}
	inUse := make(map[*peer.Peer]bool)
	for _, rec := range m.records {
		for _, pe := range rec.pending {
			inUse[pe.peer] = true
		}
		for _, res := range rec.established {
			if res.Peer != nil {
				inUse[res.Peer] = true
			}
		}
	}
	type end struct {
		peer   *peer.Peer
		period *bindingPeriod
	}
	ended := make(map[handshake.PeerAddress]end)
	for addr, rec := range m.created {
		if rec.period != nil && !inUse[rec.peer] {
			ended[addr] = end{rec.peer, rec.period}
			rec.period = nil
		}
	}
	m.stateMu.Unlock()
	for addr, e := range ended {
		p, period := e.peer, e.period
		m.log.Debug("binding.peer.released", slog.Uint64("node_id", addr.NodeID), slog.Int("fabric_index", int(addr.FabricIndex)))
		period.cancel()
		m.wg.Add(1)
		go func() {
			defer m.wg.Done()
			// The period's Connect calls return on the cancel; one still
			// running would start the stopped process again.
			period.calls.Wait()
			m.link.release(context.WithoutCancel(m.life), p)
		}()
	}
}

// peerLink is how the manager drives a peer: the peer's own methods,
// replaced in the package's tests.
type peerLink interface {
	connect(ctx context.Context, p *peer.Peer) error
	hasSession(p *peer.Peer) bool
	observe(p *peer.Peer, sessionAdded func()) (stop func())
	// release stops p's connection process; p stays in the set, its
	// sessions open (releaseUnbound).
	release(ctx context.Context, p *peer.Peer)
}

// realLink is [peerLink] over client/peer: PeerSet.for(addr).connect()
// (BindingManager.ts:600-601) and the peer's session set.
type realLink struct{}

func (realLink) connect(ctx context.Context, p *peer.Peer) error {
	_, err := p.Connect(ctx, peer.ConnectOptions{})
	return err
}

func (realLink) hasSession(p *peer.Peer) bool { return p.HasSession() }

func (realLink) release(ctx context.Context, p *peer.Peer) {
	p.Disconnect(ctx, nil)
}

func (realLink) observe(p *peer.Peer, sessionAdded func()) func() {
	return p.Observe(peer.Observer{SessionAdded: func(*exchange.Session) { sessionAdded() }})
}

// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"time"

	"thingfabric.dev/go-fabric/clock"
	"thingfabric.dev/go-fabric/cluster"
	"thingfabric.dev/go-fabric/cluster/spec"
	gkmdef "thingfabric.dev/go-fabric/cluster/spec/groupkeymanagement"
	"thingfabric.dev/go-fabric/contract"
	"thingfabric.dev/go-fabric/groups"
	"thingfabric.dev/go-fabric/im"
	"thingfabric.dev/go-fabric/store"
)

// GroupKeyManagement implements the Matter GroupKeyManagement cluster
// (0x003F) per Matter Core Specification 1.6.1 §11.2.10 (cluster revision 4). Mandatory on
// the Root endpoint; fabric-scoped attributes (GroupKeyMap,
// GroupTable) are filtered to the requesting fabric by the IM layer
// before the cluster sees them.
//
// The cluster is a thin facade over [store.Store] — every read goes
// through the store, every write commits to it.
type GroupKeyManagement struct {
	store GroupStoreFacade

	// groups is the operational group state this cluster keeps current —
	// matter.js's GroupKeyManagementServer drives `fabric.groups` the same
	// way (setFromGroupKeySet, removeGroupKeySet, groupKeyIdMap). nil
	// leaves the cluster a pure store facade with an empty GroupTable.
	groups *groups.Manager

	// writeMu serialises a controller's GroupKeyMap writes: one write's
	// read of the fabric's bindings, its comparison and its store are one
	// step, so a write that answers unchanged has stored nothing, a
	// concurrent write's change is never undone under it, and two writes
	// never leave a mixture of both lists (matter.js holds the datasource
	// across a write's change and the commit's comparison,
	// packages/node/src/behavior/state/managed/Datasource.ts:1345-1350 and
	// :1446-1457 at db021da0). Groupcast's binding changes take it too
	// (setGroupKeyMapping, removeGroupKeyMapping; review II,
	// F-db021da0-42). It is taken before mu, never under it, and the store
	// is called under it.
	writeMu sync.Mutex
	// keySetMu makes a key set's budget check and its store one step —
	// KeySetWrite's and Groupcast's key creation alike — so concurrent
	// creations never exceed MaxGroupKeysPerFabric (review II,
	// R2-P6-bound-1015). It is taken before mu, never under it, and the
	// store is called under it.
	keySetMu sync.Mutex
	// mu guards the fields below: the request's fabric, the limits and
	// the cached key-set state.
	mu                    sync.RWMutex
	currentFabric         uint8
	maxGroupsPerFabric    uint16
	maxGroupKeysPerFabric uint16
	// staleGroups are the fabrics whose operational group state a reload
	// could not refresh after a stored change (syncGroups); the retry on
	// the clock and each later command or write of the server reload them
	// again. retryTimer is the armed retry (nil while none), retryDelay the
	// wait of the next one.
	staleGroups map[uint8]bool
	retryTimer  clock.Timer
	retryDelay  time.Duration
	clk         clock.Clock
	// retrySeq names the armed retry: a callback whose number is not the
	// armed one (stopped or superseded) does nothing. quiesceEpoch counts
	// MatterQuiesce calls: a retry that was running when the server was
	// quiesced arms no next one (review II, R2-P6-err-1701 R3-a).
	retrySeq     uint64
	quiesceEpoch uint64
	// mapHeld and tableHeld name each fabric whose GroupKeyMap rows, and
	// whose GroupTable entries, the server last saw non-empty in the store:
	// at construction, at a read, at a write or a group table change. The
	// store's fabric cascade removes a fabric's rows before the drop runs,
	// so the drop asks the store only for a fabric named here, and an empty
	// answer is the attribute shrinking (review II, R4-par-4051a).
	mapHeld, tableHeld map[uint8]bool
	// tableMarkMu makes a GroupTable mark's store read and its mark one
	// step (markTableHeld, and the drop's read and clear): of two group
	// table changes at once, the mark is the store's answer to the later
	// read, so a removal's stale empty answer never clears the mark of an
	// addition after it (review II, R5-conc-5002). It is taken after
	// writeMu and before mu; the store is called under it.
	tableMarkMu sync.Mutex
	// reloadMu serialises reloadGroups: a failed reload's forget never
	// lands after a concurrent reload that succeeded and found nothing
	// stale to re-announce (review II, R2-P6-err-1701 R2-a). It is taken
	// before mu, never under it.
	reloadMu sync.Mutex

	// logger receives the server's diagnostics (GroupKeyMgmtConfig.Logger).
	logger *slog.Logger

	// dataVersion tracks the per-cluster monotonic counter per Matter
	// §10.6.5. Bumped at construction (non-zero sentinel) and after every
	// successful GroupKeyMap / GroupKeySet mutation so DataVersionFilter
	// evaluation works correctly. Satisfies
	// [contract.ClusterDataVersion]. Mirrors matter.js behavior
	// layer auto-tracking and chip's ember dirty-marking in
	// src/app/clusters/group-key-mgmt-server/.
	dataVersion cluster.DataVersionTracker
	// changes reports what moves outside a write of this cluster: a
	// fabric drop's removal of GroupKeyMap rows and GroupTable entries
	// ([GroupKeyManagement.OnAttributeChange]).
	changes cluster.AttributeChanges
}

// groupTableLister is the optional store capability the fabric drop reads
// a fabric's GroupTable entries with ([store.Groups] has it).
type groupTableLister interface {
	ListGroupTable(ctx context.Context, fabricIndex uint8) ([]store.GroupTableEntry, error)
}

// GroupStoreFacade is the subset of [store.Store] this cluster uses.
type GroupStoreFacade interface {
	UpsertGroupKeySet(ctx context.Context, rec store.GroupKeySet) error
	GetGroupKeySet(ctx context.Context, fabricIndex uint8, groupKeySetID uint16) (store.GroupKeySet, error)
	ListGroupKeySets(ctx context.Context, fabricIndex uint8) ([]store.GroupKeySet, error)
	RemoveGroupKeySet(ctx context.Context, fabricIndex uint8, groupKeySetID uint16) error
	SetGroupKeyMapping(ctx context.Context, m store.GroupKeyMapping) error
	RemoveGroupKeyMapping(ctx context.Context, fabricIndex uint8, groupID uint16) error
	// ReplaceGroupKeyMappings is a GroupKeyMap write's one store write: the
	// fabric's whole map, atomic.
	ReplaceGroupKeyMappings(ctx context.Context, fabricIndex uint8, mappings []store.GroupKeyMapping) error
	ListGroupKeyMappings(ctx context.Context, fabricIndex uint8) ([]store.GroupKeyMapping, error)
}

// Cluster ID + revision per Matter §11.2.10.
const (
	groupKeyMgmtClusterID       uint32 = 0x003F
	groupKeyMgmtClusterRevision uint16 = 4 // matter.js HEAD group-key-management.element.ts:20 default=4 (Matter 1.6.1)

	groupKeyMgmtAttrGroupKeyMap           uint32 = 0x0000
	groupKeyMgmtAttrGroupTable            uint32 = 0x0001
	groupKeyMgmtAttrMaxGroupsPerFabric    uint32 = 0x0002
	groupKeyMgmtAttrMaxGroupKeysPerFabric uint32 = 0x0003

	groupKeyMgmtCmdKeySetWrite              uint32 = 0x00
	groupKeyMgmtCmdKeySetRead               uint32 = 0x01
	groupKeyMgmtCmdKeySetReadResponse       uint32 = 0x02
	groupKeyMgmtCmdKeySetRemove             uint32 = 0x03
	groupKeyMgmtCmdKeySetReadAllIndices     uint32 = 0x04
	groupKeyMgmtCmdKeySetReadAllIndicesResp uint32 = 0x05
)

// Errors.
// errGroupKeyMgmtInvalidArg is returned for a payload of the wrong Go type
// and answers INVALID_COMMAND ([invalidArgErr]; review II, W6-test-17,
// R2-P6-err-3112).
var errGroupKeyMgmtInvalidArg error = invalidArgErr{msg: "core: GroupKeyManagement invalid argument"}

// errGroupKeyMgmtInvalidValue is returned for an attribute write's value of
// the wrong Go type: INVALID_DATA_TYPE ([invalidValueErr]; owner decision
// 234).
var errGroupKeyMgmtInvalidValue error = invalidValueErr{msg: "core: GroupKeyManagement invalid value"}

// groupKeyNotFoundErr is the typed [im.StatusCodeError] returned when a
// KeySetRead / KeySetRemove targets a group key set that does not exist on
// the fabric. Maps to IM NotFound (0x8b), matching matter.js
// GroupKeyManagementServer.ts (Status.NotFound) rather than a generic
// failure.
type groupKeyNotFoundErr struct{ id uint16 }

func (e groupKeyNotFoundErr) Error() string {
	return fmt.Sprintf("core: GroupKeyManagement key set not found: id=%d", e.id)
}

func (groupKeyNotFoundErr) MatterStatusCode() im.StatusCode { return im.StatusNotFound }

var _ im.StatusCodeError = groupKeyNotFoundErr{}

// groupKeyExhaustedErr is the typed error a KeySetWrite raises when
// adding a new key set would exceed the fabric's MaxGroupKeysPerFabric
// budget. Maps to IM ResourceExhausted (0x89), matching matter.js
// GroupKeyManagementServer.ts:386-394.
type groupKeyExhaustedErr struct{ maxKeys uint16 }

func (e groupKeyExhaustedErr) Error() string {
	return fmt.Sprintf("core: GroupKeyManagement key sets exhausted: max=%d per fabric", e.maxKeys)
}

func (groupKeyExhaustedErr) MatterStatusCode() im.StatusCode { return im.StatusResourceExhausted }

var _ im.StatusCodeError = groupKeyExhaustedErr{}

// GroupKeyMgmtConfig drives [NewGroupKeyManagement]. Defaults mirror
// matter.js GroupKeyManagementServer.ts:613-614 at db91886c
// (maxGroupKeysPerFabric = 20, maxGroupsPerFabric = 22). Spec floors
// per Matter §11.2.4 are maxGroupKeysPerFabric ≥ 3 and
// maxGroupsPerFabric ≥ 4.
type GroupKeyMgmtConfig struct {
	MaxGroupsPerFabric    uint16
	MaxGroupKeysPerFabric uint16
	// Groups is the node's operational group state. When set, every key
	// set and GroupKeyMap change is applied to it, its MaxGroupsPerFabric
	// cap follows this cluster's attribute, and GroupTable is read from
	// it. A node with a Groups server must set it — without it the group
	// table stays empty and no group message can be authenticated.
	Groups *groups.Manager
	// Logger receives the server's diagnostics; nil uses slog.Default().
	Logger *slog.Logger
	// Clock runs the retry of a group state whose reload failed
	// (staleRetryFirst, doubling up to staleRetryMax); nil is
	// [clock.System].
	Clock clock.Clock
}

// The retry of a fabric's group state after a reload failed: the first
// after staleRetryFirst, each next one doubled up to staleRetryMax, while
// any fabric is marked stale (review II, R2-P6-err-1701 R2-1).
const (
	staleRetryFirst = time.Second
	staleRetryMax   = time.Minute
)

// matter.js defaults — see GroupKeyManagementServer.ts:613-614 at db91886c (the
// server overrides the schema minimums: maxGroupKeysPerFabric = 20,
// maxGroupsPerFabric = 22, "aligned with Groupcast quota=floor(44/2)").
const (
	defaultMaxGroupsPerFabric    uint16 = 22
	defaultMaxGroupKeysPerFabric uint16 = 20
)

// NewGroupKeyManagement constructs the cluster.
func NewGroupKeyManagement(s GroupStoreFacade, cfg GroupKeyMgmtConfig) (*GroupKeyManagement, error) {
	if s == nil {
		return nil, errors.New("core: GroupKeyManagement store is required")
	}
	if cfg.MaxGroupsPerFabric == 0 {
		cfg.MaxGroupsPerFabric = defaultMaxGroupsPerFabric
	}
	if cfg.MaxGroupKeysPerFabric == 0 {
		cfg.MaxGroupKeysPerFabric = defaultMaxGroupKeysPerFabric
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	g := &GroupKeyManagement{
		store:                 s,
		groups:                cfg.Groups,
		maxGroupsPerFabric:    cfg.MaxGroupsPerFabric,
		maxGroupKeysPerFabric: cfg.MaxGroupKeysPerFabric,
		logger:                logger,
		clk:                   clock.Or(cfg.Clock),
	}
	// Seed DataVersion at a non-zero sentinel on construction so
	// DataVersionFilter=0 does not produce a false-positive cache hit on
	// the first read.
	g.dataVersion.Bump()
	if g.groups != nil {
		// The cap AddGroup enforces is this cluster's attribute
		// (matter.js addEndpointForGroup reads state.maxGroupsPerFabric),
		// and a membership change is a GroupTable change.
		g.groups.SetMaxGroupsPerFabric(int(cfg.MaxGroupsPerFabric))
		g.groups.OnGroupTableChanged(func(f uint8) {
			g.dataVersion.Bump()
			g.markTableHeld(context.Background(), f)
		})
	}
	g.dropMappingsToKeySet0(context.Background())
	g.loadHeld(context.Background())
	return g, nil
}

// loadHeld marks every fabric the store lists whose GroupKeyMap rows or
// GroupTable entries are not empty (mapHeld, tableHeld). A store that
// cannot list fabrics, or a list that fails, marks nothing: such a fabric
// is marked at its next read, write or group table change.
func (g *GroupKeyManagement) loadHeld(ctx context.Context) {
	lister, ok := g.store.(fabricLister)
	if !ok {
		return
	}
	recs, err := lister.ListFabrics(ctx)
	if err != nil {
		return
	}
	for i := range recs {
		f := recs[i].FabricIndex
		if rows, err := g.store.ListGroupKeyMappings(ctx, f); err == nil {
			g.markHeld(false, f, len(rows) > 0)
		}
		g.markTableHeld(ctx, f)
	}
}

// markTableHeld records whether the store holds GroupTable entries of
// fabric; a store without the list, or a list that fails, leaves the mark.
// A server without a group state serves an empty GroupTable and marks
// nothing.
func (g *GroupKeyManagement) markTableHeld(ctx context.Context, fabric uint8) {
	if g.groups == nil {
		return
	}
	g.tableMarkMu.Lock()
	defer g.tableMarkMu.Unlock()
	n, ok := g.tableRows(ctx, fabric)
	if ok {
		g.markHeld(true, fabric, n > 0)
	}
}

// tableRows is the number of fabric's GroupTable entries the store holds;
// false when the store cannot say.
func (g *GroupKeyManagement) tableRows(ctx context.Context, fabric uint8) (int, bool) {
	lister, ok := g.store.(groupTableLister)
	if !ok {
		return 0, false
	}
	rows, err := lister.ListGroupTable(ctx, fabric)
	if err != nil {
		return 0, false
	}
	return len(rows), true
}

// markHeld sets or clears fabric's mark in tableHeld (table) or mapHeld,
// under mu.
func (g *GroupKeyManagement) markHeld(table bool, fabric uint8, on bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	held := &g.mapHeld
	if table {
		held = &g.tableHeld
	}
	if !on {
		delete(*held, fabric)
		return
	}
	if *held == nil {
		*held = map[uint8]bool{}
	}
	(*held)[fabric] = true
}

// OnAttributeChange implements [contract.AttributeChangeNotifier]: a
// fabric's removal ([GroupKeyManagement.MatterDropFabric]) that takes the
// fabric's GroupKeyMap rows or GroupTable entries out of the attributes
// advances the DataVersion once and tells the listeners of the attributes
// that shrank, after its locks are released. A controller's write is
// reported by the bridge itself (review II, R4-par-4051a, decision 614).
func (g *GroupKeyManagement) OnAttributeChange(cb func(change contract.Change)) (unsubscribe func()) {
	return g.changes.OnAttributeChange(cb)
}

// MatterDropFabric implements [contract.FabricScopedState]. The store's
// fabric cascade removes the fabric's key sets, GroupKeyMap rows and
// GroupTable entries, and the group state forgets the fabric
// ([groups.Manager.ForgetFabric]); the server keeps nothing of its own. A
// drop that takes GroupKeyMap rows or GroupTable entries out of the
// attributes — a fabric the server saw rows of, whose rows the store no
// longer holds — advances the DataVersion once and reports the attributes
// that shrank; a drop that removes nothing — the second drop of a fabric,
// the drop at an index whose rows the store holds — moves and reports
// nothing (review II, R4-par-4051a, decision 614).
func (g *GroupKeyManagement) MatterDropFabric(ctx context.Context, fabricIndex uint8) error {
	g.writeMu.Lock()
	g.mu.RLock()
	mapHad, tableHad := g.mapHeld[fabricIndex], g.tableHeld[fabricIndex]
	g.mu.RUnlock()
	var moved []uint32
	if mapHad {
		// A list that fails keeps the mark, so a later drop asks again.
		if rows, err := g.store.ListGroupKeyMappings(ctx, fabricIndex); err == nil && len(rows) == 0 {
			g.markHeld(false, fabricIndex, false)
			moved = append(moved, groupKeyMgmtAttrGroupKeyMap)
		}
	}
	if tableHad {
		g.tableMarkMu.Lock()
		if n, ok := g.tableRows(ctx, fabricIndex); ok && n == 0 {
			g.markHeld(true, fabricIndex, false)
			moved = append(moved, groupKeyMgmtAttrGroupTable)
		}
		g.tableMarkMu.Unlock()
	}
	if len(moved) > 0 {
		g.dataVersion.Bump()
	}
	g.writeMu.Unlock()
	if len(moved) > 0 {
		g.changes.Notify(moved...)
	}
	return nil
}

// MatterHeldFabrics implements [contract.FabricScopedState]: the fabrics
// whose GroupKeyMap rows or GroupTable entries the server saw held.
func (g *GroupKeyManagement) MatterHeldFabrics() []uint8 {
	g.mu.RLock()
	defer g.mu.RUnlock()
	out := make([]uint8, 0, len(g.mapHeld)+len(g.tableHeld))
	for f := range g.mapHeld {
		out = append(out, f)
	}
	for f := range g.tableHeld {
		if !g.mapHeld[f] {
			out = append(out, f)
		}
	}
	slices.Sort(out)
	return out
}

// dropMappingsToKeySet0 removes every stored GroupKeyMap entry that maps a
// group to key set 0, the fabric's IPK, and logs each one. The entry's
// GroupKeySetId is "1 to 65535" (checkMappedKeySet), and an earlier
// version of this module stored such an entry from a write it accepted. It
// is inert — key set 0 never authenticates a group message (groups/
// manager.go loadFabric keeps the IPK out of the key sets a group message
// is decoded with) — but it would
// be served, and every list append of its fabric, judged with the
// fabric's whole list, would fail on it. matter.js holds a behavior's
// stored state to the model as it initialises (packages/node/src/behavior/
// internal/ServerBehaviorBacking.ts:60-63 at db021da0) and so never serves
// such an entry. The node removes a removed fabric's state at start the
// same way (node/fabric_state.go, reconcileFabricState). Best-effort: a
// store error is logged, and the entry stays for the next start.
//
// The removal covers the fabrics a request-free read covers (readFabrics):
// every fabric the store lists, or else every fabric the Groups manager
// holds. A store that cannot list fabrics, with no Groups manager set,
// covers none — nothing is scanned, removed or logged, and such an entry
// is served as before. Every [store.Port] lists fabrics.
func (g *GroupKeyManagement) dropMappingsToKeySet0(ctx context.Context) {
	fabrics, err := g.readFabrics(ctx)
	if err != nil {
		g.logger.Warn("matter.group_key_management.keyset0_mapping_scan_failed", slog.Any("err", err))
		return
	}
	for _, fabric := range fabrics {
		mappings, err := g.store.ListGroupKeyMappings(ctx, fabric)
		if err != nil {
			g.logger.Warn("matter.group_key_management.keyset0_mapping_scan_failed", slog.Int("fabric_index", int(fabric)), slog.Any("err", err))
			continue
		}
		dropped := false
		for _, m := range mappings {
			if checkMappedKeySet(m.GroupKeySetID) == nil {
				continue
			}
			if err := g.store.RemoveGroupKeyMapping(ctx, fabric, m.GroupID); err != nil {
				g.logger.Warn("matter.group_key_management.keyset0_mapping_drop_failed",
					slog.Int("fabric_index", int(fabric)), slog.Int("group_id", int(m.GroupID)), slog.Any("err", err))
				continue
			}
			dropped = true
			g.logger.Warn("matter.group_key_management.keyset0_mapping_dropped",
				slog.Int("fabric_index", int(fabric)), slog.Int("group_id", int(m.GroupID)),
				slog.String("hint", "a GroupKeyMap entry mapped the group to key set 0, the IPK; the group has no key set mapped now"))
		}
		if dropped {
			g.syncGroups(ctx, fabric)
		}
	}
}

// syncGroups applies a committed key set / GroupKeyMap change to the
// operational group state. Mirrors the fabric.groups calls of matter.js
// GroupKeyManagementServer (keySetWrite → setFromGroupKeySet,
// keySetRemove → removeGroupKeySet, #updateGroupKeyMap →
// groupKeyIdMap).
//
// matter.js derives the group state inside the command, after the state
// assignment and before the transaction commits
// (GroupKeyManagementServer.ts:368-385, FabricGroups.ts:140-170 at
// db91886c), so it has no "stored but not derived" outcome; here the store
// has already taken the write, so the command succeeds and the stale group
// state is reloaded.
//
// A reload that fails is logged, the fabric marked stale and its group
// state dropped, failing closed (reloadGroups); the change answers success
// for what is stored, and the next command or write of this server
// reloads the fabric again (reloadStale) (review II, R2-P6-err-1701,
// R2-P6-err-1014).
func (g *GroupKeyManagement) syncGroups(ctx context.Context, fabric uint8) {
	if g.groups == nil {
		return
	}
	g.reloadStale(ctx)
	g.reloadGroups(ctx, fabric)
}

// reloadGroups reloads fabric's operational group state. A reload that
// fails leaves the fabric marked stale and fails closed: the fabric's
// group state leaves the manager at once (groups.Manager.ForgetFabric) —
// its keys, so a key set the store no longer holds stops authenticating
// group messages whether or not the next reload succeeds, and the
// reception state of every key of the fabric, the surviving key sets'
// too, so their senders re-synchronise on the first counter seen, as
// after a restart. The manager loads the fabric from the store again on
// its next use, and the retry on the clock (armRetryLocked) reloads it
// until a reload succeeds (review II, R2-P6-err-1701 R-1, R2-1). The
// reload that succeeds after a stale mark re-announces the memberships
// the forget withdrew (groups.Manager.Load). A fabric the store no longer
// holds is unmarked and forgotten as well. Serialised by reloadMu.
func (g *GroupKeyManagement) reloadGroups(ctx context.Context, fabric uint8) {
	g.reloadMu.Lock()
	defer g.reloadMu.Unlock()
	err := g.groups.Reload(ctx, fabric)
	g.mu.Lock()
	was := g.staleGroups[fabric]
	switch {
	case err == nil && was:
		// The memberships the forget withdrew are announced again; the
		// fabric is unmarked — and the retry stopped and its backoff reset
		// — only once that succeeded. A failed announcement keeps the mark
		// and the retry owed, its backoff going on (review II,
		// R2-P6-err-1701 R3-2, R4-1).
		g.mu.Unlock()
		lerr := g.groups.Load(ctx)
		g.mu.Lock()
		defer g.mu.Unlock()
		if lerr != nil {
			g.logger.Warn("matter.group_key_management.group_state_reannounce_failed",
				slog.Int("fabric_index", int(fabric)), slog.Any("err", lerr))
			g.markStaleLocked(ctx, fabric)
			return
		}
		g.unmarkLocked(fabric)
		return
	case err == nil, errors.Is(err, groups.ErrNoFabric):
		g.unmarkLocked(fabric)
		g.mu.Unlock()
		if err != nil {
			g.groups.ForgetFabric(fabric) //nolint:contextcheck // ForgetFabric takes no context; it drops in-memory state only
		}
		return
	}
	g.markStaleLocked(ctx, fabric)
	g.mu.Unlock()
	g.logger.Warn("matter.group_key_management.group_state_stale",
		slog.Int("fabric_index", int(fabric)), slog.Any("err", err))
	g.groups.ForgetFabric(fabric) //nolint:contextcheck // ForgetFabric takes no context; it drops in-memory state only
}

// unmarkLocked unmarks fabric and, with no fabric stale any more, stops
// the retry and resets its backoff. g.mu is held.
func (g *GroupKeyManagement) unmarkLocked(fabric uint8) {
	delete(g.staleGroups, fabric)
	if len(g.staleGroups) == 0 {
		g.stopRetryLocked()
	}
}

// armRetryLocked arms the retry of the stale fabrics unless one is armed:
// after staleRetryFirst, then doubled per failed retry up to staleRetryMax.
// g.mu is held.
func (g *GroupKeyManagement) armRetryLocked() {
	if g.retryTimer != nil {
		return
	}
	if g.retryDelay == 0 {
		g.retryDelay = staleRetryFirst
	}
	g.retrySeq++
	seq := g.retrySeq
	g.retryTimer = g.clk.AfterFunc(g.retryDelay, func() { g.retryStale(seq) })
}

// stopRetryLocked disarms the retry and resets its backoff; a callback of
// the disarmed retry that already fired finds its number superseded. g.mu
// is held.
func (g *GroupKeyManagement) stopRetryLocked() {
	if g.retryTimer != nil {
		g.retryTimer.Stop()
		g.retryTimer = nil
	}
	g.retrySeq++
	g.retryDelay = 0
}

// retryEpochKey marks the context of a retry with the quiesce epoch it
// started in (reloadGroups arms no next retry once the epoch moved).
type retryEpochKey struct{}

// retryStale is the retry seq on the clock: it reloads every stale fabric
// and, while one stays stale, the failed reload arms the next retry with
// the doubled wait. A callback that is not the armed retry — stopped,
// quiesced or superseded by a newer arm — does nothing, so it neither
// clears a newer timer nor starts a chain of its own.
func (g *GroupKeyManagement) retryStale(seq uint64) {
	g.mu.Lock()
	if seq != g.retrySeq || g.retryTimer == nil {
		g.mu.Unlock()
		return
	}
	g.retryTimer = nil
	// A reload that fails again arms the next retry with the doubled wait.
	g.retryDelay = min(2*g.retryDelay, staleRetryMax)
	epoch := g.quiesceEpoch
	g.mu.Unlock()
	g.reloadStale(context.WithValue(context.Background(), retryEpochKey{}, epoch))
}

// MatterQuiesce implements [contract.ClusterQuiescer]: the retry of a
// stale group state is disarmed, and a retry already running when it is
// called arms no next one. The stale marks stay; the server's next command
// or write reloads them and arms the retry again when that fails.
func (g *GroupKeyManagement) MatterQuiesce() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.quiesceEpoch++
	g.stopRetryLocked()
}

// markStaleLocked marks fabric stale and arms the retry — unless ctx is a
// retry's that MatterQuiesce overtook. g.mu is held.
func (g *GroupKeyManagement) markStaleLocked(ctx context.Context, fabric uint8) {
	if g.staleGroups == nil {
		g.staleGroups = map[uint8]bool{}
	}
	g.staleGroups[fabric] = true
	if epoch, retry := ctx.Value(retryEpochKey{}).(uint64); retry && epoch != g.quiesceEpoch {
		return
	}
	g.armRetryLocked() //nolint:contextcheck // the retry runs on the clock, outside any request; its context is its own (retryStale)
}

// reloadStale reloads every fabric syncGroups marked stale.
func (g *GroupKeyManagement) reloadStale(ctx context.Context) {
	if g.groups == nil {
		return
	}
	g.mu.RLock()
	stale := make([]uint8, 0, len(g.staleGroups))
	for f := range g.staleGroups {
		stale = append(stale, f)
	}
	g.mu.RUnlock()
	for _, f := range stale {
		g.reloadGroups(ctx, f)
	}
}

// Compile-time assertions.
var (
	_ contract.ClusterServer                  = (*GroupKeyManagement)(nil)
	_ contract.ClusterCommandLister           = (*GroupKeyManagement)(nil)
	_ contract.ClusterDataVersion             = (*GroupKeyManagement)(nil)
	_ contract.ClusterCommandInvokePrivilege  = (*GroupKeyManagement)(nil)
	_ contract.ClusterAttributeWritePrivilege = (*GroupKeyManagement)(nil)
	_ contract.ClusterQuiescer                = (*GroupKeyManagement)(nil)
	_ contract.FabricScopedState              = (*GroupKeyManagement)(nil)
	_ contract.AttributeChangeNotifier        = (*GroupKeyManagement)(nil)
)

// MatterDataVersion implements [contract.ClusterDataVersion].
// Returns the per-cluster monotonic counter seeded at construction and
// bumped on every GroupKeyMap / GroupKeySet mutation. Mirrors matter.js
// behavior layer auto-tracking and chip's ember dirty-marking in
// src/app/clusters/group-key-mgmt-server/.
func (g *GroupKeyManagement) MatterDataVersion() uint32 {
	return g.dataVersion.Current()
}

// MatterClusterID implements [contract.ClusterServer].
func (g *GroupKeyManagement) MatterClusterID() uint32 { return groupKeyMgmtClusterID }

// MinInvokePrivilege implements [contract.ClusterCommandInvokePrivilege].
// Every GroupKeyManagement command requires Administer (5) per Matter
// §11.2.10 (access "F A"). Mirrors matter.js
// packages/model/src/standard/elements/group-key-management.element.ts:48,54,65,71.
func (g *GroupKeyManagement) MinInvokePrivilege(cmdID uint32) uint8 {
	switch cmdID {
	case groupKeyMgmtCmdKeySetWrite, groupKeyMgmtCmdKeySetRead, groupKeyMgmtCmdKeySetRemove, groupKeyMgmtCmdKeySetReadAllIndices:
		return 5 // Administer
	default:
		return 3 // Operate — standard default
	}
}

// MinWritePrivilege implements [contract.ClusterAttributeWritePrivilege].
// GroupKeyMap (0x0000) requires Manage (4) per Matter §11.2.10 (access
// "RW F VM"). Mirrors matter.js packages/model/src/standard/elements/
// group-key-management.element.ts:28.
func (g *GroupKeyManagement) MinWritePrivilege(attrID uint32) uint8 {
	switch attrID {
	case groupKeyMgmtAttrGroupKeyMap:
		return 4 // Manage
	default:
		return 3 // Operate — standard default
	}
}

// GroupKeyMapStruct mirrors Matter §11.2.10.4.1.
type GroupKeyMapStruct struct {
	GroupID       uint16
	GroupKeySetID uint16
	FabricIndex   uint8
}

// The Go fields of a GroupKeyMap write's entries (the bridge decodes the
// write into GroupKeyMapStruct), so the dispatcher's check of a written
// value against the definition reads them (spec.MapFields; owner decision
// 64).
func init() {
	spec.MapFields[GroupKeyMapStruct](gkmdef.GroupKeyMapStructDef, spec.FieldMap{
		"GroupId": "GroupID", "GroupKeySetId": "GroupKeySetID", "FabricIndex": "FabricIndex",
	})
}

// GroupInfoMapStruct mirrors Matter §11.2.10.4.2.
type GroupInfoMapStruct struct {
	GroupID     uint16
	Endpoints   []uint16
	GroupName   string
	FabricIndex uint8
}

// GroupKeySetStruct mirrors Matter §11.2.10.4.3.
//
// A zero EpochStartTime1 / EpochStartTime2 stands for the null the wire
// carries for an unused slot: a used slot's start time is always greater
// than EpochStartTime0, which is itself at least 1 for every key set but
// the IPK (key set 0, start time 0).
type GroupKeySetStruct struct {
	GroupKeySetID          uint16
	GroupKeySecurityPolicy uint8
	EpochKey0              []byte // nullable
	EpochStartTime0        uint64
	EpochKey1              []byte
	EpochStartTime1        uint64
	EpochKey2              []byte
	EpochStartTime2        uint64
	// GroupKeyMulticastPolicy is field 8 (GroupKeyMulticastPolicyEnum).
	// It has no effect: KeySetWrite accepts any value and does not store
	// it, and KeySetRead reports PerGroupID while the model still defines
	// the field. Mirrors matter.js 452d6f5c,
	// packages/node/src/behaviors/group-key-management/
	// GroupKeyManagementServer.ts:withoutMulticastPolicy / keySetRead
	// (reportsMulticastPolicy).
	GroupKeyMulticastPolicy uint8
}

// GroupKeyMulticastPolicyPerGroupID is GroupKeyMulticastPolicyEnum
// PerGroupId (0), the value KeySetRead reports for every key set
// (group-key-management.element.ts GroupKeyMulticastPolicyEnum).
const GroupKeyMulticastPolicyPerGroupID uint8 = 0

// MatterRead implements [contract.ClusterServer].
//
// A read without an IM request behind it is a local one: GroupKeyMap and
// GroupTable cover every fabric (see readFabrics).
func (g *GroupKeyManagement) MatterRead(attrID uint32) (any, bool) {
	return g.matterReadWithCtx(context.Background(), attrID)
}

// MatterReadFiltered is the read path for the fabric-scoped GroupKeyMap
// and GroupTable: a fabric-filtered read sees the accessing fabric's
// entries, an unfiltered one every fabric's (see readFabrics).
func (g *GroupKeyManagement) MatterReadFiltered(ctx context.Context, attrID uint32) (any, bool) {
	return g.matterReadWithCtx(ctx, attrID)
}

func (g *GroupKeyManagement) matterReadWithCtx(ctx context.Context, attrID uint32) (any, bool) {
	switch attrID {
	case groupKeyMgmtAttrGroupKeyMap:
		fabrics, err := g.readFabrics(ctx)
		if err != nil {
			return nil, false
		}
		out := []GroupKeyMapStruct{}
		for _, fabric := range fabrics {
			mappings, err := g.store.ListGroupKeyMappings(ctx, fabric)
			if err != nil {
				return nil, false
			}
			if len(mappings) > 0 {
				g.markHeld(false, fabric, true)
			}
			for _, m := range mappings {
				out = append(out, GroupKeyMapStruct{
					GroupID:       m.GroupID,
					GroupKeySetID: m.GroupKeySetID,
					FabricIndex:   m.FabricIndex,
				})
			}
		}
		return out, true
	case groupKeyMgmtAttrGroupTable:
		// The group table the Groups servers maintain. Mirrors matter.js
		// GroupKeyManagementServer state `groupTable` (addEndpointForGroup
		// / removeEndpoint).
		if g.groups == nil {
			return []GroupInfoMapStruct{}, true
		}
		fabrics, err := g.readFabrics(ctx)
		if err != nil {
			return nil, false
		}
		out := []GroupInfoMapStruct{}
		for _, fabric := range fabrics {
			table, err := g.groups.GroupTable(ctx, fabric)
			if err != nil {
				return nil, false
			}
			for _, e := range table {
				out = append(out, GroupInfoMapStruct{GroupID: e.GroupID, Endpoints: e.Endpoints, GroupName: e.GroupName, FabricIndex: fabric})
			}
		}
		return out, true
	case groupKeyMgmtAttrMaxGroupsPerFabric:
		g.mu.RLock()
		v := g.maxGroupsPerFabric
		g.mu.RUnlock()
		return v, true
	case groupKeyMgmtAttrMaxGroupKeysPerFabric:
		g.mu.RLock()
		v := g.maxGroupKeysPerFabric
		g.mu.RUnlock()
		return v, true
	case cluster.AttrGlobalFeatureMap:
		return uint32(0), true
	case cluster.AttrGlobalClusterRevision:
		return groupKeyMgmtClusterRevision, true
	// Global attributes 0xFFF8–0xFFFB: mirrors matter.js ClusterServer
	// auto-populated globalAttributes
	// (packages/node/src/behavior/cluster/ClusterBehavior.ts) and chip
	// endpoint_config.h cluster metadata tables.
	case cluster.AttrGlobalGeneratedCommandList:
		// GKM generated commands: KeySetReadResponse (0x02),
		// KeySetReadAllIndicesResp (0x05).
		return []uint32{
			groupKeyMgmtCmdKeySetReadResponse,       // 0x02
			groupKeyMgmtCmdKeySetReadAllIndicesResp, // 0x05
		}, true
	case cluster.AttrGlobalAcceptedCommandList:
		// GKM accepted commands per Matter §11.2.10.
		return []uint32{
			groupKeyMgmtCmdKeySetWrite,          // 0x00
			groupKeyMgmtCmdKeySetRead,           // 0x01
			groupKeyMgmtCmdKeySetRemove,         // 0x03
			groupKeyMgmtCmdKeySetReadAllIndices, // 0x04
		}, true
	case cluster.AttrGlobalAttributeList:
		// Full attribute list per Matter §11.2.10 + global attrs.
		return []uint32{
			groupKeyMgmtAttrGroupKeyMap,            // 0x0000
			groupKeyMgmtAttrGroupTable,             // 0x0001
			groupKeyMgmtAttrMaxGroupsPerFabric,     // 0x0002
			groupKeyMgmtAttrMaxGroupKeysPerFabric,  // 0x0003
			cluster.AttrGlobalFeatureMap,           // 0xFFFC
			cluster.AttrGlobalClusterRevision,      // 0xFFFD
			cluster.AttrGlobalGeneratedCommandList, // 0xFFF8
			cluster.AttrGlobalAcceptedCommandList,  // 0xFFF9
			cluster.AttrGlobalAttributeList,        // 0xFFFB
		}, true
	}
	return nil, false
}

// fabricLister is the optional store capability an unfiltered read of a
// fabric-scoped list needs: every fabric's index. Every store.Port has it.
type fabricLister interface {
	ListFabrics(ctx context.Context) ([]store.FabricRecord, error)
}

// readFabrics returns the fabrics whose entries a GroupKeyMap / GroupTable
// read covers. A fabric-filtered read covers the accessing fabric; an
// unfiltered read and a read with no request behind it cover every
// fabric. Neither list has a fabric-sensitive
// field (group-key-management.element.ts: GroupKeyMapStruct and
// GroupInfoMapStruct carry access "F" only), so the other fabrics' entries
// go out whole. Mirrors matter.js ListManager createProxy, which filters a
// fabric-scoped list only for `session.fabricFiltered ||
// config.fabricSensitive`, the way OperationalCredentials.Fabrics and NOCs
// already read here.
//
// A read with no request behind it — [GroupKeyManagement.MatterRead], or
// the node facade's read through MatterReadFiltered — is a local actor's
// and covers every fabric; the fabric last set with
// [GroupKeyManagement.SetCurrentFabric] scopes nothing. Mirrors matter.js
// packages/node/src/behavior/state/managed/values/ListManager.ts:609-616
// at db021da0 (createProxy filters a fabric-scoped list only for a remote
// actor). A fabric-filtered request from a session that has no fabric yet
// (PASE before AddNOC, FabricIndex 0) covers no fabric at all: matter.js
// ListManager's FabricFilteredListProxyHandler shows a fabric-filtered
// session only the entries of `session.fabric`, and such a session has
// none.
func (g *GroupKeyManagement) readFabrics(ctx context.Context) ([]uint8, error) {
	filtered, fabric := im.FabricFilterFromContext(ctx)
	if filtered {
		if fabric == 0 {
			return nil, nil
		}
		return []uint8{fabric}, nil
	}
	if lister, ok := g.store.(fabricLister); ok {
		recs, err := lister.ListFabrics(ctx)
		if err != nil {
			return nil, fmt.Errorf("core: GroupKeyManagement: list fabrics: %w", err)
		}
		out := make([]uint8, 0, len(recs))
		for i := range recs {
			out = append(out, recs[i].FabricIndex)
		}
		slices.Sort(out)
		return out, nil
	}
	if g.groups != nil {
		idxs, err := g.groups.Fabrics(ctx)
		if err != nil {
			return nil, fmt.Errorf("core: GroupKeyManagement: list fabrics: %w", err)
		}
		return idxs, nil
	}
	// A store that cannot enumerate fabrics leaves the accessing one.
	if fabric == 0 {
		return nil, nil
	}
	return []uint8{fabric}, nil
}

// writeFabric is the fabric a GroupKeyMap write lands on: the accessing
// fabric of the request. An Interaction Model write without one — a PASE
// session before AddNOC — is refused with UnsupportedAccess, as
// [AccessControl] refuses its lists. A local write — MatterWrite without a
// request context — uses the [GroupKeyManagement.SetCurrentFabric] value
// and is refused without one; the entries' own FabricIndex is raw input
// and never a scope. Mirrors chip src/app/WriteHandler.cpp:805-811 at
// 6170af84 ("a fabric-scoped list and there is no accessing fabric →
// UNSUPPORTED_ACCESS") and matter.js
// packages/protocol/src/action/server/AccessControl.ts:358-365 at db91886c
// (authorizeWrite: "Permission denied: No accessing fabric",
// Status.UnsupportedAccess).
func (g *GroupKeyManagement) writeFabric(ctx context.Context) (uint8, error) {
	_, fabric, request := im.LookupFabricFilter(ctx)
	if !request {
		g.mu.RLock()
		fabric = g.currentFabric
		g.mu.RUnlock()
	}
	if fabric == 0 {
		return 0, gkmStatusError{im.StatusUnsupportedAccess, "GroupKeyMap is fabric-scoped; the write has no accessing fabric"}
	}
	return fabric, nil
}

// MatterWrite handles GroupKeyMap as a writable attribute (Matter
// §11.2.10.4.1). The IM layer pre-filters the list to the requesting
// fabric.
func (g *GroupKeyManagement) MatterWrite(ctx context.Context, attrID uint32, value any) error {
	_, err := g.MatterWriteChanged(ctx, attrID, value)
	return err
}

// MatterWriteChanged implements [contract.ChangeReportingWriter]: the
// write of [GroupKeyManagement.MatterWrite], changed when it stored the
// list — the fabric's bindings differed from the written ones. Bindings
// equal to the held ones are not stored and leave the server's own
// version (matter.js Datasource.ts:1456-1457, :1476-1492 at db021da0: an
// equal value is no change); the read, the comparison and the store hold
// writeMu. The written order counts: the same bindings in another order
// are a change (sameGroupKeyMap).
func (g *GroupKeyManagement) MatterWriteChanged(ctx context.Context, attrID uint32, value any) (bool, error) {
	g.reloadStale(ctx)
	if attrID != groupKeyMgmtAttrGroupKeyMap {
		return false, spec.Errorf(im.StatusUnsupportedWrite, "core: GroupKeyManagement attribute 0x%04X is read-only", attrID)
	}
	list, ok := value.([]GroupKeyMapStruct)
	if !ok {
		return false, fmt.Errorf("%w: GroupKeyMap write expected []GroupKeyMapStruct, got %T", errGroupKeyMgmtInvalidValue, value)
	}
	fabric, err := g.writeFabric(ctx)
	if err != nil {
		return false, err
	}

	// Matter §11.2.10.4.1 semantics: writing GroupKeyMap REPLACES the
	// fabric's list. Entries the controller left out are unbound, so the
	// write has to delete them — keeping them would make a read
	// contradict the controller's own write it just acknowledged.
	//
	// The FabricIndex a client puts on an entry is raw input, not a
	// scope: every entry is stamped with the accessing fabric below, the
	// way AccessControl.MatterWrite stamps ACL entries. matter.js injects
	// the session's fabric into every fabric-scoped struct before the
	// behaviour sees it (packages/protocol/src/action/server/
	// AttributeWriteResponse.ts:466-472, overwriting via
	// packages/types/src/tlv/TlvObject.ts:309-313), and
	// GroupKeyManagementServer.ts:170-226 at db021da0 #validateGroupKeyMap never
	// compares an entry's fabricIndex with the session — so a controller
	// that omits the field (decoded as 0) or echoes a stale value gets
	// its write applied, not refused.
	if err := g.validateGroupKeyMap(list); err != nil {
		return false, err
	}
	g.writeMu.Lock()
	defer g.writeMu.Unlock()
	existing, err := g.store.ListGroupKeyMappings(ctx, fabric)
	if err != nil {
		return false, fmt.Errorf("core: GroupKeyMap write: list current bindings: %w", err)
	}
	if sameGroupKeyMap(existing, list) {
		return false, nil
	}
	// The whole map in one write (store.GroupKeys ReplaceGroupKeyMappings):
	// a refused write keeps the map held before it, under its DataVersion.
	mappings := make([]store.GroupKeyMapping, 0, len(list))
	for _, m := range list {
		mappings = append(mappings, store.GroupKeyMapping{FabricIndex: fabric, GroupID: m.GroupID, GroupKeySetID: m.GroupKeySetID})
	}
	if err := g.store.ReplaceGroupKeyMappings(ctx, fabric, mappings); err != nil {
		return false, fmt.Errorf("core: GroupKeyMap write: %w", err)
	}
	g.markHeld(false, fabric, len(mappings) > 0)
	// A GroupKeyMap that changed advances DataVersion so DataVersionFilter
	// evaluation detects the cluster changed; one written as it was held
	// returned above.
	g.dataVersion.Bump()
	g.syncGroups(ctx, fabric)
	return true, nil
}

// sameGroupKeyMap reports whether the written list is the held one entry
// for entry, in its order: the GroupKeyMap keeps its written order (owner
// decision 148, F-38; the store keeps the order it was given), so a write
// that only reorders the entries is a change and is stored.
func sameGroupKeyMap(held []store.GroupKeyMapping, list []GroupKeyMapStruct) bool {
	if len(held) != len(list) {
		return false
	}
	for i, m := range list {
		if held[i].GroupID != m.GroupID || held[i].GroupKeySetID != m.GroupKeySetID {
			return false
		}
	}
	return true
}

// gkmStatusError is a GroupKeyManagement rejection that carries the exact
// IM status matter.js answers with (a StatusResponseError there).
type gkmStatusError struct {
	code im.StatusCode
	msg  string
}

func (e gkmStatusError) Error() string { return "core: GroupKeyManagement: " + e.msg }

// MatterStatusCode implements [im.StatusCodeError].
func (e gkmStatusError) MatterStatusCode() im.StatusCode { return e.code }

var _ im.StatusCodeError = gkmStatusError{}

// isApplicationGroupID reports whether id lies in the Application Group ID
// range 0x0001..0xFEFF (Matter §2.5.4 Table 2). Mirrors matter.js
// packages/types/src/datatype/GroupId.ts:isApplicationGroupId.
func isApplicationGroupID(id uint16) bool { return id >= 0x0001 && id <= 0xFEFF }

// checkMappedKeySet holds a GroupKeyMap entry's GroupKeySetId to its model
// constraint "1 to 65535": key set 0 is the fabric's IPK and is never
// mapped to a group.
func checkMappedKeySet(id uint16) error {
	if id == 0 {
		return gkmStatusError{im.StatusConstraintError, "GroupKeyMap: GroupKeySetId 0 (the IPK) cannot be mapped to a group"}
	}
	return nil
}

// validateGroupKeyMap applies the checks matter.js runs before a
// GroupKeyMap write is accepted: every GroupId must be an Application
// Group ID (InvalidAction), a GroupId may appear once per fabric
// (ConstraintError), and no fabric may map more than MaxGroupsPerFabric
// groups (ResourceExhausted). Every entry belongs to the accessing fabric
// (see MatterWrite), so the per-fabric count is the list length. The
// mapped key set is deliberately not required to exist — matter.js leaves
// that check commented out because certification tests write it first.
// Mirrors GroupKeyManagementServer.ts:#validateGroupKeyMap.
//
// Before those rules every entry meets its field constraint: GroupKeySetId
// is "1 to 65535", so key set 0 (the IPK) cannot be mapped
// (ConstraintError). Mirrors matter.js
// packages/model/src/standard/elements/group-key-management.element.ts:109
// at db021da0, enforced per entry as the list accepts it
// (packages/node/src/behavior/state/managed/values/ListManager.ts:379-386,
// #acceptEntry), before the groupKeyMap$Changing reactor
// (GroupKeyManagementServer.ts:96); connectedhomeip 6170af84 refuses it
// the same way, src/app/clusters/group-key-mgmt-server/GroupKeyManagementCluster.cpp:199-200.
func (g *GroupKeyManagement) validateGroupKeyMap(list []GroupKeyMapStruct) error {
	for _, m := range list {
		if err := checkMappedKeySet(m.GroupKeySetID); err != nil {
			return err
		}
	}
	seen := make(map[uint16]struct{}, len(list))
	for _, m := range list {
		if !isApplicationGroupID(m.GroupID) {
			return gkmStatusError{im.StatusInvalidAction, fmt.Sprintf("GroupKeyMap: GroupId 0x%04X is not an operational group id", m.GroupID)}
		}
		if _, dup := seen[m.GroupID]; dup {
			return gkmStatusError{im.StatusConstraintError, fmt.Sprintf("GroupKeyMap: duplicate GroupId 0x%04X", m.GroupID)}
		}
		seen[m.GroupID] = struct{}{}
	}
	g.mu.RLock()
	maxGroups := g.maxGroupsPerFabric
	g.mu.RUnlock()
	if len(list) > int(maxGroups) {
		return gkmStatusError{im.StatusResourceExhausted, fmt.Sprintf("GroupKeyMap: too many groups per fabric, maximum is %d", maxGroups)}
	}
	return nil
}

// KeySetWriteRequest mirrors Matter §11.2.10.6.1.
type KeySetWriteRequest struct {
	GroupKeySet GroupKeySetStruct
}

// keySetWriteFields is the key set of a KeySetWrite request — the module's
// [KeySetWriteRequest], or the generated one, whose nullable start times
// say whether EpochStartTime1 and EpochStartTime2 are set (a 0 that is
// not null is set).
func keySetWriteFields(fields any) (gks GroupKeySetStruct, start1Set, start2Set, ok bool) {
	switch req := fields.(type) {
	case KeySetWriteRequest:
		return req.GroupKeySet, false, false, true
	case gkmdef.KeySetWriteRequest:
		in := req.GroupKeySet
		gks = GroupKeySetStruct{
			GroupKeySetID:          in.GroupKeySetId,
			GroupKeySecurityPolicy: uint8(in.GroupKeySecurityPolicy),
			EpochKey0:              nullableBytes(in.EpochKey0),
			EpochStartTime0:        in.EpochStartTime0.Value,
			EpochKey1:              nullableBytes(in.EpochKey1),
			EpochStartTime1:        in.EpochStartTime1.Value,
			EpochKey2:              nullableBytes(in.EpochKey2),
			EpochStartTime2:        in.EpochStartTime2.Value,
		}
		if in.EpochStartTime0.Null {
			gks.EpochStartTime0 = 0
		}
		if in.EpochStartTime1.Null {
			gks.EpochStartTime1 = 0
		}
		if in.EpochStartTime2.Null {
			gks.EpochStartTime2 = 0
		}
		if in.GroupKeyMulticastPolicy != nil {
			gks.GroupKeyMulticastPolicy = uint8(*in.GroupKeyMulticastPolicy)
		}
		return gks, !in.EpochStartTime1.Null, !in.EpochStartTime2.Null, true
	}
	return GroupKeySetStruct{}, false, false, false
}

// validateKeySet is KeySetWrite's key and start-time validation of gks,
// whose disabled slots (MAX_64BIT_TIME) are already cleared; start1Set and
// start2Set say that a start time is not null.
func validateKeySet(gks GroupKeySetStruct, start1Set, start2Set bool) error {
	// EpochKey length constraint: each non-nil key must be exactly 16 bytes.
	// Mirrors chip src/app/clusters/group-key-mgmt-server/
	// GroupKeyManagementCluster.cpp: VerifyOrExit(epochKey.size() == 16,
	// err = CHIP_ERROR_INVALID_ARGUMENT), answered as
	// Status::ConstraintError: the error below carries that status.
	const epochKeyLen = 16
	if len(gks.EpochKey0) > 0 && len(gks.EpochKey0) != epochKeyLen {
		return spec.Errorf(im.StatusConstraintError, "core: KeySetWrite: constraint error: EpochKey0 must be exactly %d bytes, got %d", epochKeyLen, len(gks.EpochKey0))
	}
	if len(gks.EpochKey1) > 0 && len(gks.EpochKey1) != epochKeyLen {
		return spec.Errorf(im.StatusConstraintError, "core: KeySetWrite: constraint error: EpochKey1 must be exactly %d bytes, got %d", epochKeyLen, len(gks.EpochKey1))
	}
	if len(gks.EpochKey2) > 0 && len(gks.EpochKey2) != epochKeyLen {
		return spec.Errorf(im.StatusConstraintError, "core: KeySetWrite: constraint error: EpochKey2 must be exactly %d bytes, got %d", epochKeyLen, len(gks.EpochKey2))
	}

	if len(gks.EpochKey0) == 0 || gks.EpochStartTime0 == 0 {
		return spec.Errorf(im.StatusInvalidCommand, "core: KeySetWrite: invalid command argument: EpochKey0 and EpochStartTime0 must be set")
	}
	if gks.EpochStartTime0 <= ipkDefaultEpochStartTime {
		return spec.Errorf(im.StatusInvalidCommand, "core: KeySetWrite: invalid command argument: EpochStartTime0 must be > IPK default")
	}

	if len(gks.EpochKey1) > 0 && (gks.EpochStartTime1 == 0 || gks.EpochStartTime1 <= gks.EpochStartTime0) {
		return spec.Errorf(im.StatusInvalidCommand, "core: KeySetWrite: invalid command argument: EpochStartTime1 must be set and greater than EpochStartTime0")
	}
	// A null key with a start time that is not null — 0 included — is
	// refused, as matter.js refuses it (GroupKeyManagementServer.ts:335-337,
	// :351-353 at db021da0); a request in the module's own struct carries
	// no null, and there a start time of 0 reads as absent (review II,
	// R2-P6-par-1013).
	if len(gks.EpochKey1) == 0 && (start1Set || gks.EpochStartTime1 != 0) {
		return spec.Errorf(im.StatusInvalidCommand, "core: KeySetWrite: invalid command argument: EpochKey1 must be set if EpochStartTime1 is set")
	}

	if len(gks.EpochKey2) > 0 && len(gks.EpochKey1) == 0 {
		return spec.Errorf(im.StatusInvalidCommand, "core: KeySetWrite: invalid command argument: EpochKey1 must be set if EpochKey2 is set")
	}
	if len(gks.EpochKey2) > 0 && (gks.EpochStartTime2 == 0 || gks.EpochStartTime1 == 0 || gks.EpochStartTime2 <= gks.EpochStartTime1) {
		return spec.Errorf(im.StatusInvalidCommand, "core: KeySetWrite: invalid command argument: EpochStartTime2 must be set and greater than EpochStartTime1")
	}
	if len(gks.EpochKey2) == 0 && (start2Set || gks.EpochStartTime2 != 0) {
		return spec.Errorf(im.StatusInvalidCommand, "core: KeySetWrite: invalid command argument: EpochKey2 must be set if EpochStartTime2 is set")
	}

	if store.SecurityPolicy(gks.GroupKeySecurityPolicy) != store.SecurityPolicyTrustFirst {
		return spec.Errorf(im.StatusInvalidCommand, "core: KeySetWrite: invalid command argument: GroupKeySecurityPolicy must be TrustFirst")
	}
	return nil
}

// nullableBytes is v's bytes, nil for null.
func nullableBytes(v spec.Nullable[[]byte]) []byte {
	if v.Null {
		return nil
	}
	return v.Value
}

// KeySetReadRequest mirrors Matter §11.2.10.6.2.
type KeySetReadRequest struct {
	GroupKeySetID uint16
}

// KeySetReadResponse mirrors Matter §11.2.10.6.3.
type KeySetReadResponse struct {
	GroupKeySet GroupKeySetStruct
}

// KeySetRemoveRequest mirrors Matter §11.2.10.6.4.
type KeySetRemoveRequest struct {
	GroupKeySetID uint16
}

// KeySetReadAllIndicesResponse mirrors Matter §11.2.10.6.5.
type KeySetReadAllIndicesResponse struct {
	GroupKeySetIDs []uint16
}

// MatterInvoke implements [contract.ClusterServer].
func (g *GroupKeyManagement) MatterInvoke(ctx context.Context, cmdID uint32, fields any) (any, error) {
	g.reloadStale(ctx)
	// The fabric is the request's accessing fabric
	// (FabricFilterFromContext), never a server-wide value: a command
	// without one is refused (review II, R2-P6-sec-1006).
	_, fabric := im.FabricFilterFromContext(ctx)
	if fabric == 0 {
		// A fabric-scoped operation without an accessing fabric is
		// UNSUPPORTED_ACCESS, matter.js's AccessDeniedError
		// (packages/protocol/src/interaction/FabricAccessControl.ts:59-63 at
		// db91886c); the dispatcher's ACL gate refuses such a request
		// before the server sees it (endpoint/dispatcher.go CheckACL), so
		// this is the server's own answer on a direct call.
		return nil, spec.Errorf(im.StatusUnsupportedAccess, "core: GroupKeyManagement command without active fabric")
	}

	switch cmdID {
	case groupKeyMgmtCmdKeySetWrite:
		return g.handleKeySetWrite(ctx, fabric, fields)
	case groupKeyMgmtCmdKeySetRead:
		return g.handleKeySetRead(ctx, fabric, fields)
	case groupKeyMgmtCmdKeySetRemove:
		return g.handleKeySetRemove(ctx, fabric, fields)
	case groupKeyMgmtCmdKeySetReadAllIndices:
		return g.handleKeySetReadAllIndices(ctx, fabric)
	}
	return nil, im.UnsupportedCommandf("core: GroupKeyManagement command 0x%02X not supported", cmdID)
}

// MatterReportable lists subscribe-able attributes.
//
// GroupKeyMap is absent: since cluster revision 4 (Matter 1.6.1) it
// carries quality "N C" (group-key-management.element.ts GroupKeyMap),
// and "C" (changesOmitted) means a change to it is never reported to a
// subscriber. Mirrors matter.js packages/node/src/behavior/internal/
// ServerBehaviorBacking.ts:#configureEventSuppression, which suppresses
// change broadcasts for every changesOmitted attribute, and
// packages/node/src/node/integration/ProtocolService.ts:addCluster, which
// leaves such attributes out of the attrsChanged set. The attribute stays
// readable; a controller sees a new value on its next read.
func (g *GroupKeyManagement) MatterReportable() []uint32 {
	return []uint32{groupKeyMgmtAttrGroupTable}
}

// MatterAttributes implements [contract.ClusterAttributeLister]
// so wildcard subscribe enumerates the full cluster surface.
//
// The globals are included so Apple's initial subscribe sweep can cache
// GeneratedCommandList, AcceptedCommandList and AttributeList for cluster
// 0x3F. EventList (0xFFFA) is not: matter.js marks it deprecated
// (event-list.element.ts conformance "D") and the dispatcher leaves it out
// of every synthesised AttributeList (endpoint/dispatcher.go
// synthesizeGlobalRead), so listing it here made this one cluster disagree
// with every other — the chip-tool data-model sweep is what noticed.
func (g *GroupKeyManagement) MatterAttributes() []uint32 {
	return []uint32{
		groupKeyMgmtAttrGroupKeyMap,
		groupKeyMgmtAttrGroupTable,
		groupKeyMgmtAttrMaxGroupsPerFabric,
		groupKeyMgmtAttrMaxGroupKeysPerFabric,
		cluster.AttrGlobalFeatureMap,
		cluster.AttrGlobalClusterRevision,
		cluster.AttrGlobalGeneratedCommandList,
		cluster.AttrGlobalAcceptedCommandList,
		cluster.AttrGlobalAttributeList,
	}
}

// MatterAcceptedCommands implements [contract.ClusterCommandLister].
// Lists the command IDs the server handles via MatterInvoke.
// Mirrors matter.js packages/model/src/standard/elements/
// group-key-management.element.ts accepted commands.
func (g *GroupKeyManagement) MatterAcceptedCommands() []uint32 {
	return []uint32{
		groupKeyMgmtCmdKeySetWrite,          // 0x00
		groupKeyMgmtCmdKeySetRead,           // 0x01
		groupKeyMgmtCmdKeySetRemove,         // 0x03
		groupKeyMgmtCmdKeySetReadAllIndices, // 0x04
	}
}

// MatterGeneratedCommands implements [contract.ClusterCommandLister].
// Lists the response command IDs this server may emit.
// Mirrors matter.js packages/model/src/standard/elements/
// group-key-management.element.ts generated commands.
func (g *GroupKeyManagement) MatterGeneratedCommands() []uint32 {
	return []uint32{
		groupKeyMgmtCmdKeySetReadResponse,       // 0x02
		groupKeyMgmtCmdKeySetReadAllIndicesResp, // 0x05
	}
}

// SetCurrentFabric sets the fabric a local write —
// [GroupKeyManagement.MatterWrite] with no request in its context — lands
// on. It scopes no command — [GroupKeyManagement.MatterInvoke] without an
// accessing fabric answers UnsupportedAccess — and no read: a read with
// no request behind it covers every fabric, and a request's read is
// scoped by the request's own fabric. Nothing in this module calls it.
//
// Deprecated: a fabric-scoped write takes its fabric from the request
// ([im.WithFabricFilter]); there is no replacement. Removable in
// v0.25.0.
func (g *GroupKeyManagement) SetCurrentFabric(idx uint8) {
	g.mu.Lock()
	g.currentFabric = idx
	g.mu.Unlock()
}

// max64BitTime mirrors matter.js MAX_64BIT_TIME — when an
// EpochStartTime field carries this sentinel the corresponding slot
// is disabled (see GroupKeyManagementServer.ts:24,298-309).
const max64BitTime uint64 = 0xFFFFFFFFFFFFFFFF

// ipkDefaultEpochStartTime mirrors matter.js
// `IPK_DEFAULT_EPOCH_START_TIME` (the sentinel reserved for the IPK).
// EpochStartTime0 must be strictly greater than this value
// (GroupKeyManagementServer.ts:314-317).
const ipkDefaultEpochStartTime uint64 = 0

func (g *GroupKeyManagement) handleKeySetWrite(ctx context.Context, fabric uint8, fields any) (any, error) {
	gks, start1Set, start2Set, ok := keySetWriteFields(fields)
	if !ok {
		return nil, fmt.Errorf("%w: KeySetWriteRequest expected, got %T", errGroupKeyMgmtInvalidArg, fields)
	}

	// Mirrors matter.js packages/node/src/behaviors/group-key-management/
	// GroupKeyManagementServer.ts:291-357 at db91886c. Five validation classes,
	// every failure is reported as InvalidCommand (a typed
	// spec.Errorf(im.StatusInvalidCommand, …)):
	//
	//   1. MAX_64BIT_TIME on EpochStartTimeN disables the slot —
	//      treat (start, key) as null.
	//   2. EpochKey0 + EpochStartTime0 are mandatory and EpochStartTime0
	//      must exceed IPK_DEFAULT_EPOCH_START_TIME.
	//   3. EpochKey1 ↔ EpochStartTime1 must agree (both set or both
	//      absent), EpochStartTime1 > EpochStartTime0.
	//   4. EpochKey2 ↔ EpochStartTime2 must agree, EpochStartTime2 >
	//      EpochStartTime1, and EpochKey1 must be present whenever
	//      EpochKey2 is present.
	//   5. GroupKeySecurityPolicy must be TrustFirst (the only value
	//      matter.js accepts as of HEAD).
	if gks.EpochStartTime0 == max64BitTime {
		gks.EpochStartTime0 = 0
		gks.EpochKey0 = nil
	}
	if gks.EpochStartTime1 == max64BitTime {
		gks.EpochStartTime1 = 0
		gks.EpochKey1 = nil
		start1Set = false
	}
	if gks.EpochStartTime2 == max64BitTime {
		gks.EpochStartTime2 = 0
		gks.EpochKey2 = nil
		start2Set = false
	}

	if err := validateKeySet(gks, start1Set, start2Set); err != nil {
		return nil, err
	}
	// GroupKeyMulticastPolicy (field 8) is accepted whatever its value and
	// never stored — matter.js 452d6f5c dropped the PerGroupID-only check
	// (GroupKeyManagementServer.ts:keySetWrite → withoutMulticastPolicy).

	g.keySetMu.Lock()
	if err := g.enforceKeySetBudget(ctx, fabric, gks.GroupKeySetID); err != nil {
		g.keySetMu.Unlock()
		return nil, err
	}

	rec := store.GroupKeySet{
		FabricIndex:    fabric,
		GroupKeySetID:  gks.GroupKeySetID,
		SecurityPolicy: store.SecurityPolicy(gks.GroupKeySecurityPolicy),
		EpochKey0:      gks.EpochKey0,
		EpochStart0:    gks.EpochStartTime0,
		EpochKey1:      gks.EpochKey1,
		EpochStart1:    gks.EpochStartTime1,
		EpochKey2:      gks.EpochKey2,
		EpochStart2:    gks.EpochStartTime2,
	}
	err := g.store.UpsertGroupKeySet(ctx, rec)
	g.keySetMu.Unlock()
	if err != nil {
		return nil, fmt.Errorf("core: KeySetWrite: %w", err)
	}
	// Bump DataVersion after a successful KeySetWrite so DataVersionFilter
	// evaluation correctly detects the cluster changed.
	g.dataVersion.Bump()
	g.syncGroups(ctx, fabric)
	return nil, nil
}

// enforceKeySetBudget rejects ADDING a new key-set id once the
// fabric's MaxGroupKeysPerFabric budget is reached — updating an
// existing key set is always allowed. Mirrors matter.js
// GroupKeyManagementServer.ts:keySetWrite, which counts the fabric's
// written key sets plus one for the implicit IPK key set 0 and rejects
// with ResourceExhausted when that count reaches the cap. Key set 0 is
// counted once whether or not the store holds a row for it (AddNOC
// installs one; a store that predates that does not).
func (g *GroupKeyManagement) enforceKeySetBudget(ctx context.Context, fabric uint8, id uint16) error {
	existing, err := g.store.ListGroupKeySets(ctx, fabric)
	if err != nil {
		return fmt.Errorf("core: KeySetWrite: %w", err)
	}
	written := 0
	for _, ks := range existing {
		if ks.GroupKeySetID == id {
			return nil
		}
		if ks.GroupKeySetID != 0 {
			written++
		}
	}
	g.mu.RLock()
	limit := g.maxGroupKeysPerFabric
	g.mu.RUnlock()
	if written+1 >= int(limit) {
		return groupKeyExhaustedErr{maxKeys: limit}
	}
	return nil
}

func (g *GroupKeyManagement) handleKeySetRead(ctx context.Context, fabric uint8, fields any) (any, error) {
	req, ok := fields.(KeySetReadRequest)
	if !ok {
		return nil, fmt.Errorf("%w: KeySetReadRequest expected, got %T", errGroupKeyMgmtInvalidArg, fields)
	}
	rec, err := g.store.GetGroupKeySet(ctx, fabric, req.GroupKeySetID)
	if errors.Is(err, store.ErrGroupKeySetNotFound) {
		return nil, groupKeyNotFoundErr{req.GroupKeySetID}
	}
	if err != nil {
		return nil, fmt.Errorf("core: KeySetRead: %w", err)
	}
	return KeySetReadResponse{
		GroupKeySet: GroupKeySetStruct{
			GroupKeySetID:          rec.GroupKeySetID,
			GroupKeySecurityPolicy: uint8(rec.SecurityPolicy),
			// Per Matter §11.2.10.6.3, EpochKey* fields are nulled out
			// in the response (the keys never leave the bridge after
			// initial write). We intentionally omit them.
			EpochStartTime0: rec.EpochStart0,
			EpochStartTime1: rec.EpochStart1,
			EpochStartTime2: rec.EpochStart2,
			// matter.js 452d6f5c: KeySetRead reports PerGroupID while the
			// model defines the field (GroupKeyManagementServer.ts:keySetRead,
			// reportsMulticastPolicy).
			GroupKeyMulticastPolicy: GroupKeyMulticastPolicyPerGroupID,
		},
	}, nil
}

func (g *GroupKeyManagement) handleKeySetRemove(ctx context.Context, fabric uint8, fields any) (any, error) {
	req, ok := fields.(KeySetRemoveRequest)
	if !ok {
		return nil, fmt.Errorf("%w: KeySetRemoveRequest expected, got %T", errGroupKeyMgmtInvalidArg, fields)
	}
	// Mirrors matter.js packages/node/src/behaviors/group-key-management/
	// GroupKeyManagementServer.ts:414-417 at db91886c — GroupKeySet 0 is the IPK
	// (Identity Protection Key) that the bridge needs for CASE; the
	// spec forbids deleting it via KeySetRemove. matter.js throws a
	// StatusResponseError with InvalidCommand; we surface the same
	// status as a typed spec.Errorf.
	if req.GroupKeySetID == 0 {
		return nil, spec.Errorf(im.StatusInvalidCommand, "core: KeySetRemove: invalid command argument: GroupKeySet 0 (IPK) cannot be removed")
	}
	// Removing a non-existent key set must return NotFound, not silent
	// success. matter.js GroupKeyManagementServer.ts throws Status.NotFound
	// (the bridge's store RemoveGroupKeySet is idempotent, so an existence
	// check is needed to surface the correct status).
	if _, err := g.store.GetGroupKeySet(ctx, fabric, req.GroupKeySetID); errors.Is(err, store.ErrGroupKeySetNotFound) {
		return nil, groupKeyNotFoundErr{req.GroupKeySetID}
	} else if err != nil {
		return nil, fmt.Errorf("core: KeySetRemove: %w", err)
	}
	if err := g.store.RemoveGroupKeySet(ctx, fabric, req.GroupKeySetID); err != nil {
		return nil, fmt.Errorf("core: KeySetRemove: %w", err)
	}
	// Bump DataVersion after a successful KeySetRemove. The store has
	// dropped the fabric's GroupKeyMap entries that referred to the key
	// set in the same transaction (core§11.2.7.4.1; matter.js keySetRemove
	// filters them out of groupKeyMap).
	g.dataVersion.Bump()
	g.syncGroups(ctx, fabric)
	return nil, nil
}

func (g *GroupKeyManagement) handleKeySetReadAllIndices(ctx context.Context, fabric uint8) (any, error) {
	sets, err := g.store.ListGroupKeySets(ctx, fabric)
	if err != nil {
		return nil, fmt.Errorf("core: KeySetReadAllIndices: %w", err)
	}
	// Key set 0 (the IPK) is always listed, first, whether or not the
	// store holds a row for it. Mirrors matter.js
	// GroupKeyManagementServer.ts:keySetReadAllIndices
	// (`groupKeySetIds.unshift(0)` over the fabric's written key sets).
	ids := make([]uint16, 0, len(sets)+1)
	ids = append(ids, 0)
	for _, s := range sets {
		if s.GroupKeySetID == 0 {
			continue
		}
		ids = append(ids, s.GroupKeySetID)
	}
	return KeySetReadAllIndicesResponse{GroupKeySetIDs: ids}, nil
}

// groupcastKeyEpochStartTime is EpochStartTime0 of a key set Groupcast
// creates: 1 µs after the Matter epoch, as core§11.27.7.1.4 requires.
// Mirrors matter.js GroupKeyManagementServer GROUPCAST_KEY_EPOCH_START_TIME
// (MATTER_EPOCH_OFFSET_US + 1 internally, 1 on the wire).
const groupcastKeyEpochStartTime uint64 = 1

// validateKeySetID reports whether the fabric holds key set id; key set 0,
// the IPK, always exists. Mirrors matter.js
// GroupKeyManagementServer.validateKeySetId.
func (g *GroupKeyManagement) validateKeySetID(ctx context.Context, fabric uint8, id uint16) (bool, error) {
	if id == 0 {
		return true, nil
	}
	_, err := g.store.GetGroupKeySet(ctx, fabric, id)
	if errors.Is(err, store.ErrGroupKeySetNotFound) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("core: GroupKeyManagement: %w", err)
	}
	return true, nil
}

// createKeySetForGroupcast writes a key set for a Groupcast JoinGroup or
// UpdateGroupKey that carried a key, bypassing KeySetWrite's validation:
// TrustFirst, the key as EpochKey0 with EpochStartTime0 1, and the
// MaxGroupKeysPerFabric budget. Mirrors matter.js
// GroupKeyManagementServer.createKeySetForGroupcast.
func (g *GroupKeyManagement) createKeySetForGroupcast(ctx context.Context, fabric uint8, id uint16, key []byte) error {
	g.keySetMu.Lock()
	if err := g.enforceKeySetBudget(ctx, fabric, id); err != nil {
		g.keySetMu.Unlock()
		return err
	}
	err := g.store.UpsertGroupKeySet(ctx, store.GroupKeySet{
		FabricIndex: fabric, GroupKeySetID: id, SecurityPolicy: store.SecurityPolicyTrustFirst,
		EpochKey0: slices.Clone(key), EpochStart0: groupcastKeyEpochStartTime,
	})
	g.keySetMu.Unlock()
	if err != nil {
		return fmt.Errorf("core: GroupKeyManagement: create Groupcast key set: %w", err)
	}
	g.dataVersion.Bump()
	g.syncGroups(ctx, fabric)
	return nil
}

// checkGroupKeyMapping applies the GroupKeyMap validation a Groupcast
// binding of groupID to keySetID passes through before it is written: a
// key set other than 0 (ConstraintError, the entry's field constraint), an
// application group id (InvalidAction) and no more than MaxGroupsPerFabric
// bindings on the fabric (ResourceExhausted). Mirrors matter.js
// GroupcastServer #setGroupKeyMapping, whose groupKeyMap write validates
// the entry and runs GroupKeyManagementServer #validateGroupKeyMap.
func (g *GroupKeyManagement) checkGroupKeyMapping(ctx context.Context, fabric uint8, groupID, keySetID uint16) error {
	if err := checkMappedKeySet(keySetID); err != nil {
		return err
	}
	if !isApplicationGroupID(groupID) {
		return gkmStatusError{im.StatusInvalidAction, fmt.Sprintf("GroupKeyMap: GroupId 0x%04X is not an operational group id", groupID)}
	}
	existing, err := g.store.ListGroupKeyMappings(ctx, fabric)
	if err != nil {
		return fmt.Errorf("core: GroupKeyManagement: %w", err)
	}
	n := len(existing)
	if !slices.ContainsFunc(existing, func(m store.GroupKeyMapping) bool { return m.GroupID == groupID }) {
		n++
	}
	g.mu.RLock()
	maxGroups := g.maxGroupsPerFabric
	g.mu.RUnlock()
	if n > int(maxGroups) {
		return gkmStatusError{im.StatusResourceExhausted, fmt.Sprintf("GroupKeyMap: too many groups per fabric, maximum is %d", maxGroups)}
	}
	return nil
}

// setGroupKeyMapping binds groupID to key set keySetID on the fabric,
// replacing an earlier binding, and brings the operational key map along.
// Mirrors matter.js GroupcastServer #setGroupKeyMapping /
// #syncOperationalKeyMap.
//
// It writes the GroupKeyMap under writeMu, as a controller's write of the
// attribute does: matter.js's Groupcast helpers assign the GroupKeyMap
// state (packages/node/src/behaviors/groupcast/GroupcastServer.ts:777-793
// at db021da0), and a managed write adds that state's datasource to its
// transaction as a resource
// (packages/node/src/behavior/state/managed/Datasource.ts:1345-1350), the
// same resource a controller's write of the attribute takes: one writer
// of the map at a time, whichever cluster the write comes through. The
// check is repeated under the lock, where it binds; the caller's earlier
// one only answers before anything moved (review II, F-db021da0-42).
// Lock order: Groupcast.cmdMu, then writeMu.
func (g *GroupKeyManagement) setGroupKeyMapping(ctx context.Context, fabric uint8, groupID, keySetID uint16) error {
	g.writeMu.Lock()
	defer g.writeMu.Unlock()
	if err := g.checkGroupKeyMapping(ctx, fabric, groupID, keySetID); err != nil {
		return err
	}
	if err := g.store.SetGroupKeyMapping(ctx, store.GroupKeyMapping{FabricIndex: fabric, GroupID: groupID, GroupKeySetID: keySetID}); err != nil {
		return fmt.Errorf("core: GroupKeyManagement: %w", err)
	}
	g.markHeld(false, fabric, true)
	g.dataVersion.Bump()
	g.syncGroups(ctx, fabric)
	return nil
}

// removeGroupKeyMapping drops the fabric's binding of groupID. Mirrors
// matter.js GroupcastServer #removeGroupKeyMappings. Under writeMu, as
// [GroupKeyManagement.setGroupKeyMapping] (review II, F-db021da0-42).
func (g *GroupKeyManagement) removeGroupKeyMapping(ctx context.Context, fabric uint8, groupID uint16) error {
	g.writeMu.Lock()
	defer g.writeMu.Unlock()
	if err := g.store.RemoveGroupKeyMapping(ctx, fabric, groupID); err != nil {
		return fmt.Errorf("core: GroupKeyManagement: %w", err)
	}
	// The removal may have emptied the fabric's GroupKeyMap: the mark
	// follows the store, so the fabric's drop reports no shrinking the
	// removal already made (review II, R5-par-5005). A list that fails
	// keeps the mark, and the drop asks the store again.
	if rows, err := g.store.ListGroupKeyMappings(ctx, fabric); err == nil {
		g.markHeld(false, fabric, len(rows) > 0)
	}
	g.dataVersion.Bump()
	g.syncGroups(ctx, fabric)
	return nil
}

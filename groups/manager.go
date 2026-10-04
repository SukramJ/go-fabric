// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package groups

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"slices"
	"sync"

	"github.com/SukramJ/go-fabric/secure/aesccm"
	"github.com/SukramJ/go-fabric/store"
	"github.com/SukramJ/go-fabric/transport/mrp"
)

// Errors.
var (
	// ErrResourceExhausted is returned by [Manager.AddEndpointForGroup]
	// when the fabric already holds MaxGroupsPerFabric groups. Callers
	// answer it as IM ResourceExhausted.
	ErrResourceExhausted = errors.New("groups: too many groups for fabric")
	// ErrNoFabric is returned for a fabric index the store does not know.
	ErrNoFabric = errors.New("groups: fabric not found")
)

// DefaultMaxGroupsPerFabric mirrors matter.js
// GroupKeyManagementServer.State.maxGroupsPerFabric (22).
const DefaultMaxGroupsPerFabric = 22

// Store is the persistence the manager reads its state from and writes
// group membership to. *store.Store satisfies it.
type Store interface {
	GetFabric(ctx context.Context, fabricIndex uint8) (store.FabricRecord, error)
	ListFabrics(ctx context.Context) ([]store.FabricRecord, error)
	ListGroupKeySets(ctx context.Context, fabricIndex uint8) ([]store.GroupKeySet, error)
	ListGroupKeyMappings(ctx context.Context, fabricIndex uint8) ([]store.GroupKeyMapping, error)
	ListGroupTable(ctx context.Context, fabricIndex uint8) ([]store.GroupTableEntry, error)
	UpsertGroupTableEntry(ctx context.Context, e store.GroupTableEntry) error
	RemoveGroupTableEntry(ctx context.Context, fabricIndex uint8, groupID uint16) error
}

// TableEntry is one group of a fabric with the local endpoints that are
// members of it — one GroupInfoMapStruct of GroupKeyManagement.GroupTable.
type TableEntry struct {
	GroupID   uint16
	GroupName string
	Endpoints []uint16
}

// epochKey is one epoch of an operational key set: the operational group
// key, its privacy key, its group session id and the AES-CCM instance
// that opens messages under it.
type epochKey struct {
	key       []byte
	privacy   []byte
	sessionID uint16
	ccm       *aesccm.CCM
}

// keySet is the operational form of one group key set — matter.js
// OperationalKeySet (packages/protocol/src/groups/KeySets.ts).
type keySet struct {
	id     uint16
	epochs []epochKey
}

// fabricGroups is the operational view of one fabric's groups — matter.js
// FabricGroups (packages/protocol/src/groups/FabricGroups.ts): the key
// sets with their derived keys, the GroupKeyMap (group → key set), the
// group table (group → endpoints) and the message reception state.
type fabricGroups struct {
	index      uint8
	fabricID   uint64
	compressed [8]byte

	keySets map[uint16]*keySet
	idMap   map[uint16]uint16
	table   map[uint16]*TableEntry
}

// Manager holds the group state of every fabric of this node: what the
// GroupKeyManagement and Groups clusters configure, in the form the
// receive path needs to authenticate a group message and route it. Group
// membership is stack state, as in matter.js — the host supplies no part
// of it.
//
// The persisted state lives in [Store]; the manager keeps the derived
// keys and the group table in memory, loading a fabric from the store the
// first time it is needed, and is told about every change by the cluster
// servers that make it. Safe for concurrent use.
type Manager struct {
	st     Store
	logger *slog.Logger

	mu        sync.RWMutex
	maxGroups int
	fabrics   map[uint8]*fabricGroups
	allLoaded bool

	// reception is matter.js MessagingState: the replay-protection
	// window per operational key and source node. In memory only, as
	// in matter.js.
	receptionMu sync.Mutex
	reception   map[string]map[uint64]*mrp.Window

	observersMu sync.Mutex
	observers   []func()
}

// NewManager returns a manager over st. logger may be nil.
func NewManager(st Store, logger *slog.Logger) (*Manager, error) {
	if st == nil {
		return nil, errors.New("groups: store is required")
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Manager{
		st:        st,
		logger:    logger,
		maxGroups: DefaultMaxGroupsPerFabric,
		fabrics:   make(map[uint8]*fabricGroups),
		reception: make(map[string]map[uint64]*mrp.Window),
	}, nil
}

// SetMaxGroupsPerFabric sets the cap AddEndpointForGroup enforces. The
// GroupKeyManagement cluster owns the value (its MaxGroupsPerFabric
// attribute) and pushes it here. Values below 1 are ignored.
func (m *Manager) SetMaxGroupsPerFabric(n int) {
	if n < 1 {
		return
	}
	m.mu.Lock()
	m.maxGroups = n
	m.mu.Unlock()
}

// MaxGroupsPerFabric returns the cap AddEndpointForGroup enforces.
func (m *Manager) MaxGroupsPerFabric() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.maxGroups
}

// OnMembershipChanged registers fn to run after the set of groups with
// member endpoints changed on any fabric — the trigger for re-joining the
// multicast addresses. fn runs synchronously, outside the manager's locks.
// Mirrors the endpointMap observers matter.js ServerGroupNetworking
// registers (packages/node/src/behavior/system/network/
// ServerGroupNetworking.ts:#registerFabricGroupObserver).
func (m *Manager) OnMembershipChanged(fn func()) {
	if fn == nil {
		return
	}
	m.observersMu.Lock()
	m.observers = append(m.observers, fn)
	m.observersMu.Unlock()
}

func (m *Manager) notify() {
	m.observersMu.Lock()
	obs := slices.Clone(m.observers)
	m.observersMu.Unlock()
	for _, fn := range obs {
		fn()
	}
}

// loadFabric reads one fabric's group state from the store. Called with
// m.mu held for writing.
func (m *Manager) loadFabric(ctx context.Context, idx uint8) (*fabricGroups, error) {
	rec, err := m.st.GetFabric(ctx, idx)
	if errors.Is(err, store.ErrFabricNotFound) {
		return nil, fmt.Errorf("%w: %d", ErrNoFabric, idx)
	}
	if err != nil {
		return nil, fmt.Errorf("groups: load fabric %d: %w", idx, err)
	}
	f := &fabricGroups{
		index: idx, fabricID: rec.FabricID, compressed: rec.CompressedID,
		keySets: make(map[uint16]*keySet), idMap: make(map[uint16]uint16), table: make(map[uint16]*TableEntry),
	}
	sets, err := m.st.ListGroupKeySets(ctx, idx)
	if err != nil {
		return nil, fmt.Errorf("groups: load key sets of fabric %d: %w", idx, err)
	}
	for _, s := range sets {
		// Key set 0 is the IPK. matter.js keeps it in KeySets but never
		// in the session index (KeySets.ts:#updateSessions skips id 0),
		// so it cannot authenticate a group message.
		if s.GroupKeySetID == 0 {
			continue
		}
		ks, err := deriveKeySet(s, rec.CompressedID)
		if err != nil {
			m.logger.Warn("groups.keyset.derive_failed",
				slog.Int("fabric_index", int(idx)), slog.Int("key_set", int(s.GroupKeySetID)), slog.String("err", err.Error()))
			continue
		}
		f.keySets[s.GroupKeySetID] = ks
	}
	maps, err := m.st.ListGroupKeyMappings(ctx, idx)
	if err != nil {
		return nil, fmt.Errorf("groups: load key map of fabric %d: %w", idx, err)
	}
	for _, mp := range maps {
		f.idMap[mp.GroupID] = mp.GroupKeySetID
	}
	table, err := m.st.ListGroupTable(ctx, idx)
	if err != nil {
		return nil, fmt.Errorf("groups: load group table of fabric %d: %w", idx, err)
	}
	for _, e := range table {
		f.table[e.GroupID] = &TableEntry{GroupID: e.GroupID, GroupName: e.GroupName, Endpoints: slices.Clone(e.Endpoints)}
	}
	return f, nil
}

// deriveKeySet computes the operational form of a stored key set.
// Mirrors matter.js FabricGroups.setFromGroupKeySet.
func deriveKeySet(s store.GroupKeySet, compressed [8]byte) (*keySet, error) {
	ks := &keySet{id: s.GroupKeySetID}
	for _, ek := range [][]byte{s.EpochKey0, s.EpochKey1, s.EpochKey2} {
		if len(ek) == 0 {
			continue
		}
		op, err := OperationalKey(ek, compressed)
		if err != nil {
			return nil, err
		}
		sid, err := SessionID(op)
		if err != nil {
			return nil, err
		}
		priv, err := PrivacyKey(op)
		if err != nil {
			return nil, err
		}
		ccm, err := aesccm.New(op)
		if err != nil {
			return nil, err
		}
		ks.epochs = append(ks.epochs, epochKey{key: op, privacy: priv, sessionID: sid, ccm: ccm})
	}
	if len(ks.epochs) == 0 {
		return nil, fmt.Errorf("groups: key set %d has no epoch key", s.GroupKeySetID)
	}
	return ks, nil
}

// fabricLocked returns the loaded fabric, loading it on first use. Called
// with m.mu held for writing.
func (m *Manager) fabricLocked(ctx context.Context, idx uint8) (*fabricGroups, error) {
	if f, ok := m.fabrics[idx]; ok {
		return f, nil
	}
	f, err := m.loadFabric(ctx, idx)
	if err != nil {
		return nil, err
	}
	m.fabrics[idx] = f
	return f, nil
}

// ensureAllLoaded loads every fabric the store knows the first time the
// receive path needs them all. Mirrors GroupKeyManagementServer #online,
// which restores the key sets, the key map and the group endpoint map of
// every fabric at startup ("fd65e989: restore group endpoint mappings").
func (m *Manager) ensureAllLoaded(ctx context.Context) error {
	m.mu.RLock()
	done := m.allLoaded
	m.mu.RUnlock()
	if done {
		return nil
	}
	recs, err := m.st.ListFabrics(ctx)
	if err != nil {
		return fmt.Errorf("groups: list fabrics: %w", err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.allLoaded {
		return nil
	}
	for _, r := range recs {
		if _, err := m.fabricLocked(ctx, r.FabricIndex); err != nil {
			return err
		}
	}
	m.allLoaded = true
	return nil
}

// Load restores every fabric's group state from the store. Optional: the
// manager loads on demand; a host calls Load at start-up so the multicast
// memberships of persisted groups are joined before the first group
// message is due.
func (m *Manager) Load(ctx context.Context) error {
	if err := m.ensureAllLoaded(ctx); err != nil {
		return err
	}
	m.notify()
	return nil
}

// Reload re-reads one fabric's key sets and GroupKeyMap from the store
// after GroupKeyManagement changed them (KeySetWrite, KeySetRemove, a
// GroupKeyMap write). The reception state of an operational key that no
// key set derives any more is forgotten, so a later key set reusing that
// epoch key re-synchronises on its first message instead of being taken
// for a replay. Mirrors matter.js FabricGroups.setFromGroupKeySet /
// removeGroupKeySet with #forgetUnreferencedReceptionState, and
// Groups.idMap's setter.
func (m *Manager) Reload(ctx context.Context, fabricIndex uint8) error {
	m.mu.Lock()
	prev := m.fabrics[fabricIndex]
	f, err := m.loadFabric(ctx, fabricIndex)
	if err != nil {
		m.mu.Unlock()
		return err
	}
	if prev != nil {
		// The group table is owned by this manager; keep the in-memory
		// copy rather than racing a concurrent membership change.
		f.table = prev.table
	}
	m.fabrics[fabricIndex] = f
	m.mu.Unlock()
	m.forgetUnreferenced()
	return nil
}

// ForgetFabric drops a removed fabric's group state, its reception state
// and its multicast memberships. The store's cascade has already removed
// the rows. Mirrors the FabricManager `deleting` handling of matter.js
// ServerGroupNetworking and the fabric's own disposal.
func (m *Manager) ForgetFabric(fabricIndex uint8) {
	m.mu.Lock()
	_, had := m.fabrics[fabricIndex]
	delete(m.fabrics, fabricIndex)
	m.mu.Unlock()
	m.forgetUnreferenced()
	if had {
		m.notify()
	}
}

// forgetUnreferenced drops reception state for operational keys no
// loaded key set derives.
func (m *Manager) forgetUnreferenced() {
	live := make(map[string]struct{})
	m.mu.RLock()
	for _, f := range m.fabrics {
		for _, ks := range f.keySets {
			for _, e := range ks.epochs {
				live[hex.EncodeToString(e.key)] = struct{}{}
			}
		}
	}
	m.mu.RUnlock()
	m.receptionMu.Lock()
	for k := range m.reception {
		if _, ok := live[k]; !ok {
			delete(m.reception, k)
		}
	}
	m.receptionMu.Unlock()
}

// HasKeyMapping reports whether the fabric's GroupKeyMap maps groupID to
// a key set. matter.js GroupsServer.addGroup answers UnsupportedAccess for
// a group without one (`fabric.groups.groupKeyIdMap.has(groupId)`).
func (m *Manager) HasKeyMapping(ctx context.Context, fabricIndex uint8, groupID uint16) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	f, err := m.fabricLocked(ctx, fabricIndex)
	if err != nil {
		return false, err
	}
	_, ok := f.idMap[groupID]
	return ok, nil
}

// GroupTable returns the fabric's group table ordered by group id.
func (m *Manager) GroupTable(ctx context.Context, fabricIndex uint8) ([]TableEntry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	f, err := m.fabricLocked(ctx, fabricIndex)
	if err != nil {
		return nil, err
	}
	ids := make([]uint16, 0, len(f.table))
	for id := range f.table {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	out := make([]TableEntry, 0, len(ids))
	for _, id := range ids {
		e := f.table[id]
		out = append(out, TableEntry{GroupID: e.GroupID, GroupName: e.GroupName, Endpoints: slices.Clone(e.Endpoints)})
	}
	return out, nil
}

// AddEndpointForGroup makes endpoint a member of groupID on the fabric and
// sets the group's name. A new group beyond MaxGroupsPerFabric is refused
// with [ErrResourceExhausted]. Mirrors matter.js
// GroupKeyManagementServer.addEndpointForGroup: an existing group gains
// the endpoint (once) and always takes the new name.
func (m *Manager) AddEndpointForGroup(ctx context.Context, fabricIndex uint8, groupID, endpoint uint16, name string) error {
	m.mu.Lock()
	f, err := m.fabricLocked(ctx, fabricIndex)
	if err != nil {
		m.mu.Unlock()
		return err
	}
	e, exists := f.table[groupID]
	var next TableEntry
	switch {
	case exists:
		next = TableEntry{GroupID: groupID, GroupName: name, Endpoints: slices.Clone(e.Endpoints)}
		if !slices.Contains(next.Endpoints, endpoint) {
			next.Endpoints = append(next.Endpoints, endpoint)
		}
	case len(f.table) >= m.maxGroups:
		m.mu.Unlock()
		return fmt.Errorf("%w: maximum is %d", ErrResourceExhausted, m.maxGroups)
	default:
		next = TableEntry{GroupID: groupID, GroupName: name, Endpoints: []uint16{endpoint}}
	}
	if err := m.st.UpsertGroupTableEntry(ctx, store.GroupTableEntry{
		FabricIndex: fabricIndex, GroupID: groupID, GroupName: name, Endpoints: next.Endpoints,
	}); err != nil {
		m.mu.Unlock()
		return fmt.Errorf("groups: persist group %d: %w", groupID, err)
	}
	f.table[groupID] = &next
	m.mu.Unlock()
	m.logger.Info("groups.endpoint_added",
		slog.Int("fabric_index", int(fabricIndex)), slog.Int("group_id", int(groupID)),
		slog.Int("endpoint", int(endpoint)), slog.String("name", name))
	if !exists {
		m.notify()
	}
	return nil
}

// RemoveEndpoint removes endpoint from groupID — or, with all set, from
// every group of the fabric — and reports whether it was a member of any.
// A group whose last endpoint leaves is removed. Mirrors matter.js
// GroupKeyManagementServer.removeEndpoint.
func (m *Manager) RemoveEndpoint(ctx context.Context, fabricIndex uint8, endpoint, groupID uint16, all bool) (bool, error) {
	m.mu.Lock()
	f, err := m.fabricLocked(ctx, fabricIndex)
	if err != nil {
		m.mu.Unlock()
		return false, err
	}
	var (
		existing bool
		removed  bool
	)
	ids := make([]uint16, 0, len(f.table))
	for id := range f.table {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	for _, id := range ids {
		if !all && id != groupID {
			continue
		}
		e := f.table[id]
		if !slices.Contains(e.Endpoints, endpoint) {
			continue
		}
		existing = true
		rest := slices.DeleteFunc(slices.Clone(e.Endpoints), func(ep uint16) bool { return ep == endpoint })
		if len(rest) == 0 {
			if err := m.st.RemoveGroupTableEntry(ctx, fabricIndex, id); err != nil {
				m.mu.Unlock()
				return existing, fmt.Errorf("groups: remove group %d: %w", id, err)
			}
			delete(f.table, id)
			removed = true
			continue
		}
		if err := m.st.UpsertGroupTableEntry(ctx, store.GroupTableEntry{
			FabricIndex: fabricIndex, GroupID: id, GroupName: e.GroupName, Endpoints: rest,
		}); err != nil {
			m.mu.Unlock()
			return existing, fmt.Errorf("groups: persist group %d: %w", id, err)
		}
		f.table[id] = &TableEntry{GroupID: id, GroupName: e.GroupName, Endpoints: rest}
	}
	m.mu.Unlock()
	if removed {
		m.notify()
	}
	return existing, nil
}

// Membership is one joined group of a fabric: the multicast address the
// node must listen on for it.
type Membership struct {
	FabricIndex uint8
	GroupID     uint16
	Address     net.IP
}

// Memberships returns, for every loaded fabric, each group that has a
// member endpoint, with its multicast address — the addresses the node's
// socket must have joined. Membership follows group existence, not key
// availability, as in matter.js ServerGroupNetworking. Loads every fabric
// first.
func (m *Manager) Memberships(ctx context.Context) ([]Membership, error) {
	if err := m.ensureAllLoaded(ctx); err != nil {
		return nil, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []Membership
	idxs := make([]uint8, 0, len(m.fabrics))
	for idx := range m.fabrics {
		idxs = append(idxs, idx)
	}
	slices.Sort(idxs)
	for _, idx := range idxs {
		f := m.fabrics[idx]
		ids := make([]uint16, 0, len(f.table))
		for id, e := range f.table {
			if len(e.Endpoints) > 0 {
				ids = append(ids, id)
			}
		}
		slices.Sort(ids)
		for _, id := range ids {
			out = append(out, Membership{FabricIndex: idx, GroupID: id, Address: MulticastAddress(f.fabricID, id)})
		}
	}
	return out, nil
}

// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package memstore is the in-memory implementation of the persistence port
// [store.Port]: the second implementation beside SQLite's [store.Store],
// held to the same contract by store/storetest.
//
// It is for tests and for small daemons that accept losing their state on
// restart: a process that exits forgets every fabric, so a controller that
// commissioned the node has to commission it again. The state lives in maps
// under one mutex; every method is atomic, and every slice it returns or
// takes is copied, so a caller never aliases the store.
//
// It is the port of matter.js's MemoryStorageDriver
// (packages/general/src/storage/MemoryStorageDriver.ts:13), the backend its
// tests and MockStorageService use; like SQLite it follows the schema's
// rules rather than a key-value layout: removing a fabric removes the rows
// the SQLite schema cascades (store/schema.sql, ON DELETE CASCADE), a row
// naming an unknown fabric is refused as the foreign key refuses it, and
// the fabric index comes from the same forward-only counter
// (matter.js FabricManager.ts #nextFabricIndex), kept as the metadata
// counter "next_fabric_index" exactly where SQLite keeps it.
//
// Wire it as the node's credentials store with the in-memory endpoint store:
//
//	cfg.Store = node.Store{Credentials: memstore.New(), Endpoints: epmem.New()}
//
// where epmem is go-fabric/endpoint/memstore.
package memstore

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"

	"github.com/SukramJ/go-fabric/store"
)

// Keys shared with SQLite's [store.Store], so the two hold the same settings
// and counters for the same state (store/metadata.go, store/ota.go,
// store/doorlock.go, store/icd.go).
const (
	metadataKeyNextFabricIndex = "next_fabric_index"
	otaRequestorStateKey       = "ota_requestor.state"
	settingDoorLockSaved       = "door_lock.saved:"
	metadataKeyICDCounter      = "icd_management.counter"
)

// maxMetadataCounter is SQLite's bound: the counter rides in an INTEGER.
const maxMetadataCounter = 1 << 62

// errUnknownFabric is the refusal of a fabric-owned row whose fabric the
// store does not hold — SQLite's FOREIGN KEY failure.
var errUnknownFabric = errors.New("memstore: no such fabric")

// Store is the in-memory [store.Port]. The zero value is not usable; call
// [New].
type Store struct {
	mu sync.Mutex

	fabrics    map[uint8]store.FabricRecord
	identities map[uint8]store.IdentityRecord
	acl        map[uint8][]store.ACLEntry
	aclNextID  int64
	keySets    map[uint8]map[uint16]store.GroupKeySet
	keyMap     map[uint8]map[uint16]store.GroupKeyMapping
	groupTable map[uint8]map[uint16]store.GroupTableEntry
	groupcast  map[uint8]map[uint16]store.GroupcastGroup
	resumption map[resumptionKey]store.ResumptionRecord
	otaDefault map[uint8][]store.OTAProviderLocation
	icd        map[uint8][]store.ICDRegistration
	subs       map[uint32]subscription
	subsSeq    uint64
	settings   map[string]string
	metadata   map[string]uint64
	presets    map[string]presetRow
	locks      map[string]store.DoorLockAuth
}

type resumptionKey struct {
	fabric uint8
	peer   uint64
}

type subscription struct {
	fabric  uint8
	payload []byte
	seq     uint64
}

type presetRow struct {
	presets, active []byte
}

var _ store.Port = (*Store)(nil)

// New returns an empty store. Like a freshly applied SQLite schema it holds
// the next-fabric-index counter at 1 (store/schema.sql, matter_metadata).
func New() *Store {
	return &Store{
		fabrics:    map[uint8]store.FabricRecord{},
		identities: map[uint8]store.IdentityRecord{},
		acl:        map[uint8][]store.ACLEntry{},
		aclNextID:  1,
		keySets:    map[uint8]map[uint16]store.GroupKeySet{},
		keyMap:     map[uint8]map[uint16]store.GroupKeyMapping{},
		groupTable: map[uint8]map[uint16]store.GroupTableEntry{},
		groupcast:  map[uint8]map[uint16]store.GroupcastGroup{},
		resumption: map[resumptionKey]store.ResumptionRecord{},
		otaDefault: map[uint8][]store.OTAProviderLocation{},
		icd:        map[uint8][]store.ICDRegistration{},
		subs:       map[uint32]subscription{},
		settings:   map[string]string{},
		metadata:   map[string]uint64{metadataKeyNextFabricIndex: 1},
		presets:    map[string]presetRow{},
		locks:      map[string]store.DoorLockAuth{},
	}
}

// hasFabric reports whether the fabric exists; the caller holds mu.
func (s *Store) hasFabric(fabricIndex uint8) bool {
	_, ok := s.fabrics[fabricIndex]
	return ok
}

// --- Fabrics ---

// AddFabric implements [store.Fabrics]. A record without an index takes
// the next free index from the counter, which then moves past it, wrapping
// after 254 and skipping occupied indexes (store/fabric.go AddFabric,
// matter.js FabricManager.ts:178-179).
func (s *Store) AddFabric(_ context.Context, rec store.FabricRecord) (uint8, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	idx := rec.FabricIndex
	auto := idx == 0
	if auto {
		if len(s.fabrics) >= 254 {
			return 0, store.ErrFabricExhausted
		}
		idx = s.nextFreeFrom(s.counter())
	}
	if idx < 1 || idx > 254 {
		return 0, fmt.Errorf("memstore: add fabric: index %d is outside [1, 254]", idx)
	}
	if s.hasFabric(idx) {
		return 0, fmt.Errorf("memstore: add fabric: index %d is taken", idx)
	}
	for _, f := range s.fabrics {
		if f.FabricID == rec.FabricID && bytes.Equal(f.RootPublicKey, rec.RootPublicKey) {
			return 0, fmt.Errorf("memstore: add fabric: fabric %#x under this root exists as index %d", rec.FabricID, f.FabricIndex)
		}
	}
	rec.FabricIndex = idx
	s.fabrics[idx] = cloneFabric(rec)
	if auto {
		s.bumpCounter()
	}
	return idx, nil
}

// counter is the persisted next fabric index; the caller holds mu.
func (s *Store) counter() uint8 {
	v := s.metadata[metadataKeyNextFabricIndex]
	if v < 1 || v > 254 {
		return 1
	}
	return uint8(v)
}

// nextFreeFrom walks forward from candidate to the first free index,
// wrapping 254 → 1; the caller checked that one is free.
func (s *Store) nextFreeFrom(candidate uint8) uint8 {
	for range 254 {
		if !s.hasFabric(candidate) {
			return candidate
		}
		candidate = nextIndex(candidate)
	}
	return candidate
}

// bumpCounter advances the counter past its value to the next free index,
// as store/metadata.go bumpNextFabricIndex does; a full table leaves it.
func (s *Store) bumpCounter() {
	next := s.counter()
	for range 254 {
		next = nextIndex(next)
		if !s.hasFabric(next) {
			s.metadata[metadataKeyNextFabricIndex] = uint64(next)
			return
		}
	}
}

func nextIndex(i uint8) uint8 {
	if i >= 254 {
		return 1
	}
	return i + 1
}

// GetFabric implements [store.Fabrics].
func (s *Store) GetFabric(_ context.Context, fabricIndex uint8) (store.FabricRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, ok := s.fabrics[fabricIndex]
	if !ok {
		return store.FabricRecord{}, store.ErrFabricNotFound
	}
	return cloneFabric(f), nil
}

// ListFabrics implements [store.Fabrics].
func (s *Store) ListFabrics(_ context.Context) ([]store.FabricRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.fabrics) == 0 {
		return nil, nil
	}
	out := make([]store.FabricRecord, 0, len(s.fabrics))
	for _, idx := range sortedKeys(s.fabrics) {
		out = append(out, cloneFabric(s.fabrics[idx]))
	}
	return out, nil
}

// UpdateFabricLabel implements [store.Fabrics].
func (s *Store) UpdateFabricLabel(_ context.Context, fabricIndex uint8, label string) error {
	return s.updateFabric(fabricIndex, func(f *store.FabricRecord) { f.Label = label })
}

// UpdateFabricVendorID implements [store.Fabrics].
func (s *Store) UpdateFabricVendorID(_ context.Context, fabricIndex uint8, vendorID uint16) error {
	return s.updateFabric(fabricIndex, func(f *store.FabricRecord) { f.VendorID = vendorID })
}

// UpdateFabricNodeID implements [store.Fabrics].
func (s *Store) UpdateFabricNodeID(_ context.Context, fabricIndex uint8, nodeID uint64) error {
	return s.updateFabric(fabricIndex, func(f *store.FabricRecord) { f.NodeID = nodeID })
}

func (s *Store) updateFabric(fabricIndex uint8, change func(*store.FabricRecord)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, ok := s.fabrics[fabricIndex]
	if !ok {
		return store.ErrFabricNotFound
	}
	change(&f)
	s.fabrics[fabricIndex] = f
	return nil
}

// RemoveFabric implements [store.Fabrics]: the fabric and every row the
// SQLite schema's ON DELETE CASCADE removes with it — identity, group key
// sets, key map, group table, groupcast groups, ACL, resumption records,
// default OTA providers and ICD registrations. Server subscriptions, presets, lock records,
// settings and counters stay, as they do in SQLite.
func (s *Store) RemoveFabric(_ context.Context, fabricIndex uint8) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.hasFabric(fabricIndex) {
		return store.ErrFabricNotFound
	}
	delete(s.fabrics, fabricIndex)
	delete(s.identities, fabricIndex)
	delete(s.keySets, fabricIndex)
	delete(s.keyMap, fabricIndex)
	delete(s.groupTable, fabricIndex)
	delete(s.groupcast, fabricIndex)
	delete(s.acl, fabricIndex)
	delete(s.otaDefault, fabricIndex)
	delete(s.icd, fabricIndex)
	for k := range s.resumption {
		if k.fabric == fabricIndex {
			delete(s.resumption, k)
		}
	}
	return nil
}

// --- Identity ---

// UpsertIdentity implements [store.Identity].
func (s *Store) UpsertIdentity(_ context.Context, rec store.IdentityRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.hasFabric(rec.FabricIndex) {
		return fmt.Errorf("memstore: upsert identity: %w %d", errUnknownFabric, rec.FabricIndex)
	}
	s.identities[rec.FabricIndex] = cloneIdentity(rec)
	return nil
}

// GetIdentity implements [store.Identity].
func (s *Store) GetIdentity(_ context.Context, fabricIndex uint8) (store.IdentityRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.identities[fabricIndex]
	if !ok {
		return store.IdentityRecord{}, store.ErrIdentityNotFound
	}
	return cloneIdentity(rec), nil
}

// --- ACL ---

// ListACL implements [store.ACL].
func (s *Store) ListACL(_ context.Context, fabricIndex uint8) ([]store.ACLEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.acl[fabricIndex]) == 0 {
		return nil, nil
	}
	out := make([]store.ACLEntry, 0, len(s.acl[fabricIndex]))
	for _, e := range s.acl[fabricIndex] {
		out = append(out, cloneACLEntry(e))
	}
	return out, nil
}

// ReplaceACL implements [store.ACL]. Each entry takes a fresh ID, as
// SQLite's AUTOINCREMENT gives one, and Position its place in entries.
func (s *Store) ReplaceACL(_ context.Context, fabricIndex uint8, entries []store.ACLEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(entries) > 0 && !s.hasFabric(fabricIndex) {
		return fmt.Errorf("memstore: replace acl: %w %d", errUnknownFabric, fabricIndex)
	}
	out := make([]store.ACLEntry, 0, len(entries))
	for i, e := range entries {
		if e.Privilege < store.PrivilegeView || e.Privilege > store.PrivilegeAdminister {
			return fmt.Errorf("memstore: replace acl: entry %d: privilege %d", i, e.Privilege)
		}
		if e.AuthMode < store.AuthModePASE || e.AuthMode > store.AuthModeGroup {
			return fmt.Errorf("memstore: replace acl: entry %d: auth mode %d", i, e.AuthMode)
		}
		c := cloneACLEntry(e)
		c.ID = s.aclNextID + int64(i)
		c.FabricIndex = fabricIndex
		c.Position = uint16(i) //nolint:gosec // an ACL is far below 65536 entries
		out = append(out, c)
	}
	s.aclNextID += int64(len(entries))
	if len(out) == 0 {
		delete(s.acl, fabricIndex)
		return nil
	}
	s.acl[fabricIndex] = out
	return nil
}

// --- GroupKeys ---

// UpsertGroupKeySet implements [store.GroupKeys]. An epoch start without
// its key is not kept, as SQLite stores NULL for both.
func (s *Store) UpsertGroupKeySet(_ context.Context, rec store.GroupKeySet) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.hasFabric(rec.FabricIndex) {
		return fmt.Errorf("memstore: upsert group key set: %w %d", errUnknownFabric, rec.FabricIndex)
	}
	rec = cloneKeySet(rec)
	if rec.EpochKey1 == nil {
		rec.EpochStart1 = 0
	}
	if rec.EpochKey2 == nil {
		rec.EpochStart2 = 0
	}
	inner(s.keySets, rec.FabricIndex)[rec.GroupKeySetID] = rec
	return nil
}

// GetGroupKeySet implements [store.GroupKeys].
func (s *Store) GetGroupKeySet(_ context.Context, fabricIndex uint8, groupKeySetID uint16) (store.GroupKeySet, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.keySets[fabricIndex][groupKeySetID]
	if !ok {
		return store.GroupKeySet{}, store.ErrGroupKeySetNotFound
	}
	return cloneKeySet(rec), nil
}

// ListGroupKeySets implements [store.GroupKeys].
func (s *Store) ListGroupKeySets(_ context.Context, fabricIndex uint8) ([]store.GroupKeySet, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return sortedValues(s.keySets[fabricIndex], cloneKeySet), nil
}

// RemoveGroupKeySet implements [store.GroupKeys].
func (s *Store) RemoveGroupKeySet(_ context.Context, fabricIndex uint8, groupKeySetID uint16) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for g, m := range s.keyMap[fabricIndex] {
		if m.GroupKeySetID == groupKeySetID {
			delete(s.keyMap[fabricIndex], g)
		}
	}
	delete(s.keySets[fabricIndex], groupKeySetID)
	return nil
}

// RemoveGroupKeysByFabric implements [store.GroupKeys].
func (s *Store) RemoveGroupKeysByFabric(_ context.Context, fabricIndex uint8) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.keyMap, fabricIndex)
	delete(s.keySets, fabricIndex)
	return nil
}

// SetGroupKeyMapping implements [store.GroupKeys]. The key set need not
// exist (store/schema.go Upgrade).
func (s *Store) SetGroupKeyMapping(_ context.Context, m store.GroupKeyMapping) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.hasFabric(m.FabricIndex) {
		return fmt.Errorf("memstore: set group key mapping: %w %d", errUnknownFabric, m.FabricIndex)
	}
	inner(s.keyMap, m.FabricIndex)[m.GroupID] = m
	return nil
}

// RemoveGroupKeyMapping implements [store.GroupKeys].
func (s *Store) RemoveGroupKeyMapping(_ context.Context, fabricIndex uint8, groupID uint16) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.keyMap[fabricIndex], groupID)
	return nil
}

// ListGroupKeyMappings implements [store.GroupKeys].
func (s *Store) ListGroupKeyMappings(_ context.Context, fabricIndex uint8) ([]store.GroupKeyMapping, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return sortedValues(s.keyMap[fabricIndex], identity[store.GroupKeyMapping]), nil
}

// --- Groups ---

// UpsertGroupTableEntry implements [store.Groups].
func (s *Store) UpsertGroupTableEntry(_ context.Context, e store.GroupTableEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(e.Endpoints) == 0 {
		return fmt.Errorf("memstore: group table entry %d/%d has no endpoints", e.FabricIndex, e.GroupID)
	}
	if !s.hasFabric(e.FabricIndex) {
		return fmt.Errorf("memstore: upsert group table entry: %w %d", errUnknownFabric, e.FabricIndex)
	}
	inner(s.groupTable, e.FabricIndex)[e.GroupID] = cloneGroupEntry(e)
	return nil
}

// RemoveGroupTableEntry implements [store.Groups].
func (s *Store) RemoveGroupTableEntry(_ context.Context, fabricIndex uint8, groupID uint16) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.groupTable[fabricIndex], groupID)
	return nil
}

// ListGroupTable implements [store.Groups].
func (s *Store) ListGroupTable(_ context.Context, fabricIndex uint8) ([]store.GroupTableEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return sortedValues(s.groupTable[fabricIndex], cloneGroupEntry), nil
}

// UpsertGroupcastGroup implements [store.Groups].
func (s *Store) UpsertGroupcastGroup(_ context.Context, g store.GroupcastGroup) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.hasFabric(g.FabricIndex) {
		return fmt.Errorf("memstore: upsert groupcast group: %w %d", errUnknownFabric, g.FabricIndex)
	}
	inner(s.groupcast, g.FabricIndex)[g.GroupID] = g
	return nil
}

// RemoveGroupcastGroup implements [store.Groups].
func (s *Store) RemoveGroupcastGroup(_ context.Context, fabricIndex uint8, groupID uint16) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.groupcast[fabricIndex], groupID)
	return nil
}

// ListGroupcastGroups implements [store.Groups].
func (s *Store) ListGroupcastGroups(_ context.Context, fabricIndex uint8) ([]store.GroupcastGroup, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return sortedValues(s.groupcast[fabricIndex], identity[store.GroupcastGroup]), nil
}

// --- Resumption ---

// UpsertResumption implements [store.Resumption]. A resumption id is unique
// across the table, as SQLite's index on it makes it.
func (s *Store) UpsertResumption(_ context.Context, rec store.ResumptionRecord) error {
	if len(rec.ResumptionID) != 16 {
		return fmt.Errorf("memstore: resumption_id length=%d (want 16)", len(rec.ResumptionID))
	}
	if len(rec.SharedSecret) != 32 {
		return fmt.Errorf("memstore: shared_secret length=%d (want 32)", len(rec.SharedSecret))
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.hasFabric(rec.FabricIndex) {
		return fmt.Errorf("memstore: upsert resumption: %w %d", errUnknownFabric, rec.FabricIndex)
	}
	key := resumptionKey{rec.FabricIndex, rec.PeerNodeID}
	for k, r := range s.resumption {
		if k != key && bytes.Equal(r.ResumptionID, rec.ResumptionID) {
			return errors.New("memstore: upsert resumption: resumption id held by another peer")
		}
	}
	rec = cloneResumption(rec)
	if rec.CASEAuthTags == nil {
		rec.CASEAuthTags = []uint32{}
	}
	s.resumption[key] = rec
	return nil
}

// GetResumptionByID implements [store.Resumption].
func (s *Store) GetResumptionByID(_ context.Context, resumptionID []byte) (store.ResumptionRecord, error) {
	if len(resumptionID) != 16 {
		return store.ResumptionRecord{}, fmt.Errorf("memstore: resumption_id length=%d (want 16)", len(resumptionID))
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range s.resumption {
		if bytes.Equal(r.ResumptionID, resumptionID) {
			return cloneResumption(r), nil
		}
	}
	return store.ResumptionRecord{}, store.ErrResumptionNotFound
}

// GetResumptionByPeer implements [store.Resumption].
func (s *Store) GetResumptionByPeer(_ context.Context, fabricIndex uint8, peerNodeID uint64) (store.ResumptionRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.resumption[resumptionKey{fabricIndex, peerNodeID}]
	if !ok {
		return store.ResumptionRecord{}, store.ErrResumptionNotFound
	}
	return cloneResumption(r), nil
}

// RemoveResumption implements [store.Resumption].
func (s *Store) RemoveResumption(_ context.Context, fabricIndex uint8, peerNodeID uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.resumption, resumptionKey{fabricIndex, peerNodeID})
	return nil
}

// RemoveResumptionsByFabric implements [store.Resumption].
func (s *Store) RemoveResumptionsByFabric(_ context.Context, fabricIndex uint8) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for k := range s.resumption {
		if k.fabric == fabricIndex {
			delete(s.resumption, k)
		}
	}
	return nil
}

// --- ServerSubscriptions ---

// SaveServerSubscription implements [store.ServerSubscriptions]. A save
// makes the row the most recent.
func (s *Store) SaveServerSubscription(_ context.Context, subscriptionID uint32, fabricIndex uint8, _ uint64, payload []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.subsSeq++
	s.subs[subscriptionID] = subscription{fabric: fabricIndex, payload: bytes.Clone(payload), seq: s.subsSeq}
	return nil
}

// DeleteServerSubscription implements [store.ServerSubscriptions].
func (s *Store) DeleteServerSubscription(_ context.Context, subscriptionID uint32) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.subs, subscriptionID)
	return nil
}

// DeleteServerSubscriptionsByFabric implements [store.ServerSubscriptions].
func (s *Store) DeleteServerSubscriptionsByFabric(_ context.Context, fabricIndex uint8) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, sub := range s.subs {
		if sub.fabric == fabricIndex {
			delete(s.subs, id)
		}
	}
	return nil
}

// LoadServerSubscriptions implements [store.ServerSubscriptions]: the
// least recently saved first.
func (s *Store) LoadServerSubscriptions(_ context.Context) ([][]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows := make([]subscription, 0, len(s.subs))
	for _, sub := range s.subs {
		rows = append(rows, sub)
	}
	slices.SortFunc(rows, func(a, b subscription) int { return cmp.Compare(a.seq, b.seq) })
	if len(rows) == 0 {
		return nil, nil
	}
	out := make([][]byte, 0, len(rows))
	for _, sub := range rows {
		out = append(out, bytes.Clone(sub.payload))
	}
	return out, nil
}

// ClearServerSubscriptions implements [store.ServerSubscriptions].
func (s *Store) ClearServerSubscriptions(_ context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	clear(s.subs)
	return nil
}

// --- Settings ---

// GetSetting implements [store.Settings].
func (s *Store) GetSetting(_ context.Context, key string) (value string, ok bool, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.settings[key]
	return v, ok, nil
}

// SetSetting implements [store.Settings].
func (s *Store) SetSetting(_ context.Context, key, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.settings[key] = value
	return nil
}

// GetMetadataCounter implements [store.Settings].
func (s *Store) GetMetadataCounter(_ context.Context, key string) (value uint64, ok bool, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.metadata[key]
	return v, ok, nil
}

// SetMetadataCounter implements [store.Settings].
func (s *Store) SetMetadataCounter(_ context.Context, key string, value uint64) error {
	if value > maxMetadataCounter {
		return fmt.Errorf("memstore: metadata %q overflows int64: %d", key, value)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.metadata[key] = value
	return nil
}

// --- OTA ---

// ListDefaultOTAProviders implements [store.OTA].
func (s *Store) ListDefaultOTAProviders(_ context.Context) ([]store.OTAProviderLocation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []store.OTAProviderLocation
	for _, f := range sortedKeys(s.otaDefault) {
		out = append(out, s.otaDefault[f]...)
	}
	return out, nil
}

// ReplaceDefaultOTAProviders implements [store.OTA].
func (s *Store) ReplaceDefaultOTAProviders(_ context.Context, fabricIndex uint8, entries []store.OTAProviderLocation) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(entries) == 0 {
		delete(s.otaDefault, fabricIndex)
		return nil
	}
	if !s.hasFabric(fabricIndex) {
		return fmt.Errorf("memstore: replace default ota providers: %w %d", errUnknownFabric, fabricIndex)
	}
	out := slices.Clone(entries)
	for i := range out {
		out[i].FabricIndex = fabricIndex
	}
	s.otaDefault[fabricIndex] = out
	return nil
}

// LoadOTARequestorState implements [store.OTA]; the state is a setting, as
// in SQLite.
func (s *Store) LoadOTARequestorState(ctx context.Context) (state []byte, ok bool, err error) {
	v, ok, err := s.GetSetting(ctx, otaRequestorStateKey)
	if err != nil || !ok {
		return nil, ok, err
	}
	return []byte(v), true, nil
}

// SaveOTARequestorState implements [store.OTA].
func (s *Store) SaveOTARequestorState(ctx context.Context, state []byte) error {
	return s.SetSetting(ctx, otaRequestorStateKey, string(state))
}

// --- ICD ---

// ListICDRegistrations implements [store.ICD].
func (s *Store) ListICDRegistrations(_ context.Context) ([]store.ICDRegistration, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []store.ICDRegistration
	for _, f := range sortedKeys(s.icd) {
		for _, r := range s.icd[f] {
			r.Key = slices.Clone(r.Key)
			out = append(out, r)
		}
	}
	return out, nil
}

// ReplaceICDRegistrations implements [store.ICD].
func (s *Store) ReplaceICDRegistrations(_ context.Context, fabricIndex uint8, regs []store.ICDRegistration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(regs) == 0 {
		delete(s.icd, fabricIndex)
		return nil
	}
	if !s.hasFabric(fabricIndex) {
		return fmt.Errorf("memstore: replace icd registrations: %w %d", errUnknownFabric, fabricIndex)
	}
	out := slices.Clone(regs)
	for i := range out {
		out[i].FabricIndex = fabricIndex
		out[i].Key = slices.Clone(out[i].Key)
	}
	s.icd[fabricIndex] = out
	return nil
}

// LoadICDCounter implements [store.ICD]; the counter is a metadata counter,
// as in SQLite.
func (s *Store) LoadICDCounter(ctx context.Context) (counter uint32, ok bool, err error) {
	v, ok, err := s.GetMetadataCounter(ctx, metadataKeyICDCounter)
	if err != nil || !ok {
		return 0, ok, err
	}
	return uint32(v), true, nil //nolint:gosec // G115: SaveICDCounter stores a uint32
}

// SaveICDCounter implements [store.ICD].
func (s *Store) SaveICDCounter(ctx context.Context, counter uint32) error {
	return s.SetMetadataCounter(ctx, metadataKeyICDCounter, uint64(counter))
}

// --- ThermostatPresets ---

// LoadThermostatPresets implements [store.ThermostatPresets].
func (s *Store) LoadThermostatPresets(_ context.Context, endpointKey string) (presets, activeHandle []byte, ok bool, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	row, ok := s.presets[endpointKey]
	if !ok {
		return nil, nil, false, nil
	}
	return bytes.Clone(row.presets), bytes.Clone(row.active), true, nil
}

// SaveThermostatPresets implements [store.ThermostatPresets]. Nil presets
// are kept as empty, a nil handle as none.
func (s *Store) SaveThermostatPresets(_ context.Context, endpointKey string, presets, activeHandle []byte) error {
	if presets == nil {
		presets = []byte{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.presets[endpointKey] = presetRow{presets: bytes.Clone(presets), active: bytes.Clone(activeHandle)}
	return nil
}

// --- DoorLocks ---

// LoadDoorLockAuth implements [store.DoorLocks].
func (s *Store) LoadDoorLockAuth(_ context.Context, endpointKey string) (store.DoorLockAuth, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, saved := s.settings[settingDoorLockSaved+endpointKey]; !saved {
		return store.DoorLockAuth{}, false, nil
	}
	return cloneLockAuth(s.locks[endpointKey]), true, nil
}

// SaveDoorLockAuth implements [store.DoorLocks]: the records are kept in
// the order a load returns them (SQLite's ORDER BY in store/doorlock.go),
// and an index used twice is refused as the tables' primary keys refuse it.
func (s *Store) SaveDoorLockAuth(_ context.Context, endpointKey string, auth store.DoorLockAuth) error {
	a := cloneLockAuth(auth)
	for i := range a.Users {
		if a.Users[i].Credentials == nil {
			a.Users[i].Credentials = []store.DoorLockCredentialRef{}
		}
	}
	slices.SortStableFunc(a.Users, func(x, y store.DoorLockUser) int { return cmp.Compare(x.UserIndex, y.UserIndex) })
	slices.SortStableFunc(a.Credentials, func(x, y store.DoorLockCredential) int {
		return cmp.Or(cmp.Compare(x.Type, y.Type), cmp.Compare(x.Index, y.Index))
	})
	slices.SortStableFunc(a.WeekDaySchedules, func(x, y store.DoorLockWeekDaySchedule) int {
		return cmp.Or(cmp.Compare(x.UserIndex, y.UserIndex), cmp.Compare(x.WeekDayIndex, y.WeekDayIndex))
	})
	slices.SortStableFunc(a.YearDaySchedules, func(x, y store.DoorLockYearDaySchedule) int {
		return cmp.Or(cmp.Compare(x.UserIndex, y.UserIndex), cmp.Compare(x.YearDayIndex, y.YearDayIndex))
	})
	slices.SortStableFunc(a.HolidaySchedules, func(x, y store.DoorLockHolidaySchedule) int {
		return cmp.Compare(x.HolidayIndex, y.HolidayIndex)
	})
	if err := lockDuplicates(a); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.locks[endpointKey] = a
	s.settings[settingDoorLockSaved+endpointKey] = "1"
	return nil
}

// lockDuplicates reports the first index a sorted [store.DoorLockAuth] uses
// twice.
func lockDuplicates(a store.DoorLockAuth) error {
	for i := 1; i < len(a.Users); i++ {
		if a.Users[i].UserIndex == a.Users[i-1].UserIndex {
			return fmt.Errorf("memstore: save door lock: user %d twice", a.Users[i].UserIndex)
		}
	}
	for i := 1; i < len(a.Credentials); i++ {
		if a.Credentials[i].Type == a.Credentials[i-1].Type && a.Credentials[i].Index == a.Credentials[i-1].Index {
			return fmt.Errorf("memstore: save door lock: credential %d/%d twice", a.Credentials[i].Type, a.Credentials[i].Index)
		}
	}
	for i := 1; i < len(a.WeekDaySchedules); i++ {
		x, y := a.WeekDaySchedules[i], a.WeekDaySchedules[i-1]
		if x.UserIndex == y.UserIndex && x.WeekDayIndex == y.WeekDayIndex {
			return fmt.Errorf("memstore: save door lock: week day schedule %d of user %d twice", x.WeekDayIndex, x.UserIndex)
		}
	}
	for i := 1; i < len(a.YearDaySchedules); i++ {
		x, y := a.YearDaySchedules[i], a.YearDaySchedules[i-1]
		if x.UserIndex == y.UserIndex && x.YearDayIndex == y.YearDayIndex {
			return fmt.Errorf("memstore: save door lock: year day schedule %d of user %d twice", x.YearDayIndex, x.UserIndex)
		}
	}
	for i := 1; i < len(a.HolidaySchedules); i++ {
		if a.HolidaySchedules[i].HolidayIndex == a.HolidaySchedules[i-1].HolidayIndex {
			return fmt.Errorf("memstore: save door lock: holiday schedule %d twice", a.HolidaySchedules[i].HolidayIndex)
		}
	}
	return nil
}

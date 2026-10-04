// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package groups

import (
	"context"
	"encoding/binary"
	"slices"
	"sync"
	"testing"

	"github.com/SukramJ/go-fabric/secure/aesccm"
	"github.com/SukramJ/go-fabric/secure/channel"
	"github.com/SukramJ/go-fabric/store"
	"github.com/SukramJ/go-fabric/transport/message"
)

// memStore is an in-memory [Store].
type memStore struct {
	mu      sync.Mutex
	fabrics map[uint8]store.FabricRecord
	sets    map[uint8][]store.GroupKeySet
	maps    map[uint8][]store.GroupKeyMapping
	table   map[uint8]map[uint16]store.GroupTableEntry
	failPut error
}

func newMemStore() *memStore {
	return &memStore{
		fabrics: map[uint8]store.FabricRecord{}, sets: map[uint8][]store.GroupKeySet{},
		maps: map[uint8][]store.GroupKeyMapping{}, table: map[uint8]map[uint16]store.GroupTableEntry{},
	}
}

func (s *memStore) GetFabric(_ context.Context, idx uint8) (store.FabricRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.fabrics[idx]
	if !ok {
		return store.FabricRecord{}, store.ErrFabricNotFound
	}
	return r, nil
}

func (s *memStore) ListFabrics(context.Context) ([]store.FabricRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []store.FabricRecord
	for _, r := range s.fabrics {
		out = append(out, r)
	}
	return out, nil
}

func (s *memStore) ListGroupKeySets(_ context.Context, idx uint8) ([]store.GroupKeySet, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.sets[idx]), nil
}

func (s *memStore) ListGroupKeyMappings(_ context.Context, idx uint8) ([]store.GroupKeyMapping, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.maps[idx]), nil
}

func (s *memStore) ListGroupTable(_ context.Context, idx uint8) ([]store.GroupTableEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []store.GroupTableEntry
	for _, e := range s.table[idx] {
		out = append(out, e)
	}
	return out, nil
}

func (s *memStore) UpsertGroupTableEntry(_ context.Context, e store.GroupTableEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failPut != nil {
		return s.failPut
	}
	if s.table[e.FabricIndex] == nil {
		s.table[e.FabricIndex] = map[uint16]store.GroupTableEntry{}
	}
	e.Endpoints = slices.Clone(e.Endpoints)
	s.table[e.FabricIndex][e.GroupID] = e
	return nil
}

func (s *memStore) RemoveGroupTableEntry(_ context.Context, idx uint8, gid uint16) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failPut != nil {
		return s.failPut
	}
	delete(s.table[idx], gid)
	return nil
}

func (s *memStore) addFabric(idx uint8, fabricID uint64, compressed [8]byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.fabrics[idx] = store.FabricRecord{FabricIndex: idx, FabricID: fabricID, CompressedID: compressed}
}

func (s *memStore) putKeySet(idx uint8, ks store.GroupKeySet) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ks.FabricIndex = idx
	s.sets[idx] = slices.DeleteFunc(s.sets[idx], func(o store.GroupKeySet) bool { return o.GroupKeySetID == ks.GroupKeySetID })
	s.sets[idx] = append(s.sets[idx], ks)
}

func (s *memStore) removeKeySet(idx uint8, id uint16) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sets[idx] = slices.DeleteFunc(s.sets[idx], func(o store.GroupKeySet) bool { return o.GroupKeySetID == id })
	s.maps[idx] = slices.DeleteFunc(s.maps[idx], func(o store.GroupKeyMapping) bool { return o.GroupKeySetID == id })
}

func (s *memStore) mapGroup(idx uint8, gid, ksID uint16) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.maps[idx] = slices.DeleteFunc(s.maps[idx], func(o store.GroupKeyMapping) bool { return o.GroupID == gid })
	s.maps[idx] = append(s.maps[idx], store.GroupKeyMapping{FabricIndex: idx, GroupID: gid, GroupKeySetID: ksID})
}

// sealGroup builds a group message the way a controller sends one —
// matter.js GroupSession.encode: S flag and a 16-bit destination group,
// the Security Flags naming a group session (with P when privacy is on),
// the AEAD nonce from the Security Flags, the counter and the source node
// id, and the privacy obfuscation of the header past the Security Flags.
func sealGroup(t *testing.T, opKey []byte, sessionID, groupID uint16, source uint64, counter uint32, privacy bool, plain []byte) []byte {
	t.Helper()
	hdr := message.Header{
		SessionID: sessionID, MessageCounter: counter,
		HasSourceNodeID: true, SourceNodeID: source,
		DestSize: message.DestGroup, DestGroupID: groupID,
		SessionType: message.SessionGroup, Privacy: privacy,
	}
	raw := hdr.Marshal()
	nonce := make([]byte, aesccm.NonceSize)
	nonce[0] = raw[3]
	binary.LittleEndian.PutUint32(nonce[1:5], counter)
	binary.LittleEndian.PutUint64(nonce[5:13], source)
	ccm, err := aesccm.New(opKey)
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := ccm.Seal(nil, nonce, plain, raw)
	if err != nil {
		t.Fatal(err)
	}
	out := append(slices.Clone(raw), sealed...)
	if privacy {
		pk, err := PrivacyKey(opKey)
		if err != nil {
			t.Fatal(err)
		}
		region := out[4:len(raw)]
		mask, err := channel.PrivacyKeystream(pk, sessionID, out[len(out)-aesccm.TagSize:], len(region))
		if err != nil {
			t.Fatal(err)
		}
		if err := channel.ApplyPrivacyMask(mask, region); err != nil {
			t.Fatal(err)
		}
	}
	return out
}

// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package memstore is an in-memory [endpoint.Store]: endpoint/sqlitestore's
// allocation rules, held in a map under a mutex.
//
// Endpoint numbers are the one piece of bridge state a controller never
// re-reads (endpoint/sqlitestore's package doc), so this store is right only
// for as long as the process lives: within it, a number is never reissued
// while the counter has not wrapped, exactly as in SQLite; across a restart
// every number is forgotten. Use it for tests and for a node whose
// controllers are commissioned again after each start — the pairing of
// store/memstore for the credentials.
//
//	cfg.Store = node.Store{Credentials: memstore.New(), Endpoints: epmem.New()}
//
// with this package imported as epmem.
//
// The allocation mirrors matter.js
// packages/node/src/storage/server/ServerEndpointStores.ts assignNumber
// (:101-103, :156-158, :170-183), as sqlitestore does.
package memstore

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"

	"github.com/SukramJ/go-fabric/endpoint"
)

// The bounds of sqlitestore: a stored number lies in [1, 65534]; fresh
// allocation starts at 2, after the Aggregator.
const (
	endpointIDMin uint16 = 1
	allocMin      uint16 = 2
	endpointIDMax uint16 = 65534
)

// ErrEndpointsExhausted is returned when every number from 2 to 65534 is
// held, as sqlitestore.ErrEndpointsExhausted is.
var ErrEndpointsExhausted = errors.New("memstore: no endpoint numbers left below 65535")

// Store is the in-memory [endpoint.Store]. The zero value is not usable;
// call [New].
type Store struct {
	mu   sync.Mutex
	rows map[string]endpoint.Record // by rendered key, as sqlitestore keys them
	byID map[uint16]string
	// next is the allocation counter in [2, 65535], never rewound.
	next uint32
	// highest is the highest number a row holds, 0 for none.
	highest uint16
}

var _ endpoint.Store = (*Store)(nil)

// New returns an empty store whose first allocated number is 2.
func New() *Store {
	return &Store{rows: map[string]endpoint.Record{}, byID: map[uint16]string{}, next: uint32(allocMin)}
}

// GetEndpoint implements [endpoint.Store].
func (s *Store) GetEndpoint(_ context.Context, key endpoint.SourceKey) (endpoint.Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.rows[key.String()]
	if !ok {
		return endpoint.Record{}, endpoint.ErrNotFound
	}
	rec.Key = key
	return rec, nil
}

// UpsertEndpointAssigning implements [endpoint.Store]: a known key keeps
// its number and takes rec's scope and device type; a new key takes
// rec.EndpointID (lifting the counter past it) or, when that is 0, the
// next free number from the counter, wrapping to 2 after 65534.
func (s *Store) UpsertEndpointAssigning(_ context.Context, rec endpoint.Record) (uint16, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := rec.Key.String()
	if old, ok := s.rows[k]; ok {
		rec.EndpointID = old.EndpointID
		s.rows[k] = rec
		return rec.EndpointID, nil
	}
	if rec.EndpointID != 0 {
		if rec.EndpointID < endpointIDMin || rec.EndpointID > endpointIDMax {
			return 0, fmt.Errorf("memstore: endpoint id %d is outside [%d, %d]", rec.EndpointID, endpointIDMin, endpointIDMax)
		}
		if holder, taken := s.byID[rec.EndpointID]; taken {
			return 0, fmt.Errorf("memstore: endpoint id %d is held by %q", rec.EndpointID, holder)
		}
		s.next = max(s.next, uint32(rec.EndpointID)+1)
	} else {
		id, err := s.allocate()
		if err != nil {
			return 0, err
		}
		rec.EndpointID = id
	}
	s.rows[k] = rec
	s.byID[rec.EndpointID] = k
	s.highest = max(s.highest, rec.EndpointID)
	return rec.EndpointID, nil
}

// allocate is sqlitestore's allocate: from max(counter, highest+1) up, then
// from 2 up to the counter; the counter moves past the number taken.
func (s *Store) allocate() (uint16, error) {
	next := max(s.next, uint32(s.highest)+1)
	id := s.firstFree(next, uint32(endpointIDMax))
	if id == 0 {
		id = s.firstFree(uint32(allocMin), min(next-1, uint32(endpointIDMax)))
	}
	if id == 0 {
		return 0, ErrEndpointsExhausted
	}
	s.next = uint32(id) + 1
	return id, nil
}

// firstFree is the lowest number in [lo, hi] no row holds, or 0.
func (s *Store) firstFree(lo, hi uint32) uint16 {
	for n := lo; n <= hi; n++ {
		if _, taken := s.byID[uint16(n)]; !taken { //nolint:gosec // hi <= 65534
			return uint16(n) //nolint:gosec // as above
		}
	}
	return 0
}

// ListEndpoints implements [endpoint.Store]: by endpoint number ascending;
// an empty scope lists every scope.
func (s *Store) ListEndpoints(_ context.Context, scope string) ([]endpoint.Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []endpoint.Record
	for _, rec := range s.rows {
		if scope == "" || rec.Scope == scope {
			out = append(out, rec)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].EndpointID < out[j].EndpointID })
	return out, nil
}

// RemoveEndpoint implements [endpoint.Store]; a missing key is no error.
// The number is not handed out again until the counter wraps.
func (s *Store) RemoveEndpoint(_ context.Context, key endpoint.SourceKey) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := key.String()
	if rec, ok := s.rows[k]; ok {
		delete(s.byID, rec.EndpointID)
		delete(s.rows, k)
		if rec.EndpointID == s.highest {
			s.highest = 0
			for id := range s.byID {
				s.highest = max(s.highest, id)
			}
		}
	}
	return nil
}

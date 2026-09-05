// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package endpoint_test

import (
	"context"

	"github.com/SukramJ/go-fabric/endpoint"
)

// fakeStore is an in-memory, non-thread-safe implementation of the
// [endpoint.Store] interface for use in tests only.
type fakeStore struct {
	rows   map[endpoint.SourceKey]endpoint.Record
	nextID uint16
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		rows:   make(map[endpoint.SourceKey]endpoint.Record),
		nextID: 2, // bridged endpoints start at ID 2 (root=0, aggregator=1)
	}
}

func (s *fakeStore) GetEndpoint(_ context.Context, key endpoint.SourceKey) (endpoint.Record, error) {
	rec, ok := s.rows[key]
	if !ok {
		return endpoint.Record{}, endpoint.ErrNotFound
	}
	return rec, nil
}

func (s *fakeStore) UpsertEndpointAssigning(_ context.Context, rec endpoint.Record) (uint16, error) {
	if rec.EndpointID == 0 {
		rec.EndpointID = s.nextID
		s.nextID++
	}
	s.rows[rec.Key] = rec
	return rec.EndpointID, nil
}

func (s *fakeStore) ListEndpoints(_ context.Context, scope string) ([]endpoint.Record, error) {
	var out []endpoint.Record
	for _, rec := range s.rows {
		if scope == "" || rec.Scope == scope {
			out = append(out, rec)
		}
	}
	return out, nil
}

func (s *fakeStore) RemoveEndpoint(_ context.Context, key endpoint.SourceKey) error {
	delete(s.rows, key)
	return nil
}

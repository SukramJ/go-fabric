// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"thingfabric.dev/go-fabric/store"
)

// loadFailingOtaStore fails ListDefaultOTAProviders while fail is set.
type loadFailingOtaStore struct {
	OtaRequestorStore
	fail atomic.Bool
}

func (s *loadFailingOtaStore) ListDefaultOTAProviders(ctx context.Context) ([]store.OTAProviderLocation, error) {
	if s.fail.Load() {
		return nil, errors.New("store down")
	}
	return s.OtaRequestorStore.ListDefaultOTAProviders(ctx)
}

// TestOTARequestor_StartAfterLoadFailureRetries (R3-err-3152): a Start
// whose load fails leaves the requestor not started, so a later Start —
// the store recovered — loads, adds the default providers and schedules
// the first query, rather than answering "already started" for good.
func TestOTARequestor_StartAfterLoadFailureRetries(t *testing.T) {
	t.Parallel()
	var failing *loadFailingOtaStore
	h := newOtaHarness(t, func(c *OTARequestorConfig) {
		failing = &loadFailingOtaStore{OtaRequestorStore: c.Store}
		c.Store = failing
		c.Rand = func(int) int { return 5 }
	})
	_ = h.store.ReplaceDefaultOTAProviders(context.Background(), 1, []store.OTAProviderLocation{{ProviderNodeID: testProvider}})
	failing.fail.Store(true)
	if err := h.r.Start(context.Background()); err == nil {
		t.Fatal("Start over a failing store answered nil")
	}
	failing.fail.Store(false)
	if err := h.r.Start(context.Background()); err != nil {
		t.Fatalf("Start after the store recovered: %v", err)
	}
	if len(h.r.active) != 1 || h.r.active[0].Location.ProviderNodeId != testProvider {
		t.Errorf("active %+v, want the default provider", h.r.active)
	}
	if got := h.clock.pending(); len(got) != 1 || got[0] != 125*time.Second {
		t.Errorf("pending %v, want the first query at 125s", got)
	}
}

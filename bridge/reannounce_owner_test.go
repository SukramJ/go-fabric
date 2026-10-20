// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/mdns"
)

// slowReannouncer is an advertiser whose re-announce blocks until its
// context ends — an mDNS round that does not finish before Stop.
type slowReannouncer struct {
	*mdns.Noop
	entered  chan struct{}
	returned atomic.Bool
}

func (a *slowReannouncer) TriggerReannounce(ctx context.Context) {
	close(a.entered)
	<-ctx.Done()
	a.returned.Store(true)
}

// TestStopWaitsForTheSessionReannounce pins the bridge as the owner of
// the session re-announce goroutine (G1-14, ADR 0026 P5): Stop cancels
// it with the serve context and returns only after it returned, before
// the advertiser closes.
func TestStopWaitsForTheSessionReannounce(t *testing.T) {
	t.Parallel()
	adv := &slowReannouncer{Noop: mdns.NewNoop(), entered: make(chan struct{})}
	b, err := New(wbEmptySnapshotter, adv, Config{
		Listen: ":0", VendorID: 0x1234, ProductID: 0x5678, NodeLabel: "wb-test",
		AdvertiseTimeout: time.Hour, // only Stop's cancel ends the round
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := b.Start(ctx); err != nil {
		t.Fatal(err)
	}
	b.triggerSessionReannounce()
	<-adv.entered
	if err := b.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	if !adv.returned.Load() {
		t.Fatal("Stop returned while the session re-announce still ran")
	}
	b.triggerSessionReannounce() // stopped: starts nothing
}

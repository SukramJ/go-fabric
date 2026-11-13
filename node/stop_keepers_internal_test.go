// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

import (
	"context"
	"runtime"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock/clocktest"
	"github.com/SukramJ/go-fabric/mdns"
	"github.com/SukramJ/go-fabric/store"
)

// lateCloseAdvertiser runs onLateClose on its second Close: the node's own
// (stopLocked), after the bridge's Stop — which closed it first and
// stopped the commissioning window's timer — and before the node's
// lifetime ends.
type lateCloseAdvertiser struct {
	*mdns.Noop
	closes      int
	onLateClose func()
}

func (a *lateCloseAdvertiser) Close() error {
	a.closes++
	if a.closes == 2 && a.onLateClose != nil {
		a.onLateClose()
	}
	return a.Noop.Close()
}

// commissionedKeeperNode is a started node on a fake clock whose store
// holds one fabric (no CASE identity: nothing goes on the network), so it
// starts commissioned and without a window; it returns the fabric's index.
func commissionedKeeperNode(t *testing.T, adv Advertiser) (*Node, *clocktest.Fake, uint8) {
	t.Helper()
	ctx := t.Context()
	cfg := mdnsTestConfig(t)
	cfg.Network.MDNS.Off = true
	cfg.Advertiser = adv
	fake := clocktest.New(time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC))
	cfg.Clock = fake
	db, err := store.OpenSQLite(cfg.Store.Path)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Apply(ctx, db); err != nil {
		t.Fatal(err)
	}
	idx, err := store.New(db).AddFabric(ctx, store.FabricRecord{FabricID: 1, NodeID: 2, VendorID: 0xFFF1, RootPublicKey: make([]byte, 65)})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	n := newMDNSTestNode(t, cfg)
	if err := n.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if n.Window().Open {
		t.Fatal("a node with a stored fabric opened its own window at Start")
	}
	return n, fake, idx
}

// TestReopenPendingAtStopArmsNoWindow: a reopen after the last fabric's
// removal that waits (a fail-safe armed) when Stop begins, and whose wait
// ends after the bridge stopped — between br.Stop, which stopped the
// window's timer, and the end of the node's lifetime — opens no window and
// leaves no timer armed on the stopped node's clock (ADR 0026 P5). That is
// FactoryReset's order: RemoveFabric starts the reopen, then the node
// stops.
func TestReopenPendingAtStopArmsNoWindow(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	adv := &lateCloseAdvertiser{Noop: mdns.NewNoop()}
	n, fake, idx := commissionedKeeperNode(t, adv)
	gc := n.root.GeneralCommissioning
	if err := gc.ArmFailSafeFor(ctx, 60, 0); err != nil {
		t.Fatal(err)
	}
	if err := n.RemoveFabric(ctx, idx); err != nil {
		t.Fatal(err)
	}
	if !n.reopenPending.Load() {
		t.Fatal("the last fabric's removal left no reopen pending")
	}
	ended := false
	adv.onLateClose = func() {
		// The fail-safe ends and the reopen's next poll comes due while
		// the node is between br.Stop and the end of its lifetime.
		if err := gc.ArmFailSafeFor(ctx, 0, 0); err != nil {
			t.Error(err)
		}
		fake.Advance(reopenPollInterval)
		for n.reopenPending.Load() {
			if ctx.Err() != nil {
				t.Error("the pending reopen did not end")
				return
			}
			runtime.Gosched()
		}
		ended = true
	}
	if err := n.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	if !ended {
		t.Fatal("the advertiser was not closed twice: the reopen did not end during Stop")
	}
	if armed := fake.Armed(); len(armed) != 0 {
		t.Errorf("armed after Stop: %v", armed)
	}
	if w := n.Window(); w.Open {
		t.Errorf("window open after Stop: %+v", w)
	}
}

// TestLateHandlerKeeperDoesNotRaceStop: a handler that still runs when
// Stop's wait for the bridge's handlers gave up (its ctx ended) and that
// starts a keeper goroutine — reopenWhenIdle after a RemoveFabric,
// leaveRadioWhenCommissioned after a CommissioningComplete — while Stop
// waits for keeperWG must not add to keeperWG concurrently with that Wait
// (sync.WaitGroup: an Add from zero must happen before Wait), and starts
// nothing once Stop began. Run with -race: the detector reports the
// unordered Add and Wait.
func TestLateHandlerKeeperDoesNotRaceStop(t *testing.T) {
	t.Parallel()
	n, _, _ := commissionedKeeperNode(t, nil)
	stopped := make(chan struct{})
	hammered := make(chan struct{})
	go func() {
		defer close(hammered)
		for {
			select {
			case <-stopped:
				return
			default:
			}
			n.reopenWhenIdle()
			runtime.Gosched()
		}
	}()
	err := n.Stop(t.Context())
	close(stopped)
	<-hammered
	if err != nil {
		t.Fatal(err)
	}
}

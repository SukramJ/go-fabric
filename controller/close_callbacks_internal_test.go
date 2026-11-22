// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controller

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"thingfabric.dev/go-fabric/client/exchange"
)

// TestBackgroundCallbacksOutliveClose (review II, R2-P6-conc-2305): the
// reboot armer's callbacks run on timer goroutines Close does not wait
// for. Run while Close tears the stack down, and after it, they do
// nothing instead of reading the fields Close clears — before, the armer's
// PeerLost dereferenced the cleared exchange manager (a nil-pointer panic)
// and every callback raced Close's writes (-race).
func TestBackgroundCallbacksOutliveClose(t *testing.T) {
	t.Parallel()
	db := openSealerDB(t, filepath.Join(t.TempDir(), "controller.db"))
	cfg := sealerConfig(db, nil)
	cfg.OTA = &OTAConfig{}
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	c, err := New(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	ac := c.armerContext()
	p := exchange.PeerAddress{FabricIndex: c.fabricIndex(), NodeID: 0x1234}
	fire := func() {
		ac.PeerLost(p)
		ac.CloseSubscriptions(p)
		ac.ClosePeerSessionsRegisteredBefore(p, armerSession{c: c})
		ac.SessionsRegisteredSince(armerSession{c: c})
	}
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Go(func() {
		for {
			select {
			case <-stop:
				return
			default:
				fire()
			}
		}
	})
	if err := c.Close(ctx); err != nil {
		t.Fatalf("Close: %v", err)
	}
	close(stop)
	wg.Wait()
	fire()
	if c.initiated(nil) {
		t.Fatal("initiated after Close = true, want false")
	}
}

// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package energy_test

import (
	"context"
	"sync"
	"testing"
	"testing/synctest"

	"thingfabric.dev/go-fabric/cluster/energy"
	whm "thingfabric.dev/go-fabric/cluster/spec/waterheatermanagement"
)

// gatedBooster blocks CancelBoost until release closes and signals each
// Boost it completed.
type gatedBooster struct {
	booster
	release  chan struct{}
	inCancel chan struct{}
	boosted  chan struct{}
	once     sync.Once
}

func (g *gatedBooster) Boost(ctx context.Context, info energy.BoostInfo) error {
	err := g.booster.Boost(ctx, info)
	select {
	case g.boosted <- struct{}{}:
	default:
	}
	return err
}

func (g *gatedBooster) CancelBoost(ctx context.Context) error {
	g.once.Do(func() { close(g.inCancel) })
	<-g.release
	return g.booster.CancelBoost(ctx)
}

// TestWaterHeaterBoostAndCancelAreSerialised (R2-C2-conc-1604): a Boost
// from one session that arrives while another session's CancelBoost is at
// the host waits for it, so the cancel's BoostState Inactive does not
// overwrite the newer boost the host runs. Before, the Boost completed
// in between and the cancel then left BoostState Inactive.
func TestWaterHeaterBoostAndCancelAreSerialised(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		g := &gatedBooster{release: make(chan struct{}), inCancel: make(chan struct{}), boosted: make(chan struct{}, 1)}
		srv2, err := energy.NewWaterHeaterManagement(energy.WaterHeaterConfig{
			HeaterTypes: energy.HeatSourceImmersionElement1,
			Initial:     energy.WaterHeaterState{TankVolume: 100},
			Booster:     g,
		})
		if err != nil {
			t.Fatal(err)
		}
		ctx := context.Background()
		if _, err := srv2.MatterInvoke(ctx, whm.CmdBoost, whm.BoostRequest{BoostInfo: energy.BoostInfo{Duration: 5}}); err != nil {
			t.Fatal(err)
		}
		<-g.boosted
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			if _, err := srv2.MatterInvoke(ctx, whm.CmdCancelBoost, nil); err != nil {
				t.Error(err)
			}
		}()
		<-g.inCancel
		go func() {
			defer wg.Done()
			if _, err := srv2.MatterInvoke(ctx, whm.CmdBoost, whm.BoostRequest{BoostInfo: energy.BoostInfo{Duration: 600}}); err != nil {
				t.Error(err)
			}
		}()
		// Release the cancel once the second Boost completed at the host —
		// which the serialisation prevents — or once it waits behind the cancel.
		synctest.Wait() // the second call is through, or durably blocked behind the first
		select {
		case <-g.boosted:
		default:
		}
		close(g.release)
		wg.Wait()
		if got := srv2.BoostState(); got != energy.BoostStateActive {
			t.Errorf("BoostState %d after Cancel then Boost, want Active (the host runs the newer boost)", got)
		}
	})
}

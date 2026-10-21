// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package energy_test

import (
	"context"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/cluster/energy"
	dem "github.com/SukramJ/go-fabric/cluster/spec/deviceenergymanagement"
)

// settingDemHost reports the device's state through the server's setter
// from inside its PowerAdjust hook, as a host whose device answers at
// once would.
type settingDemHost struct {
	demHost
	srv *energy.DeviceEnergyManagementServer
}

func (h *settingDemHost) PowerAdjust(ctx context.Context, p int64, d uint32, c energy.AdjustmentCause) error {
	if err := h.srv.SetEsaState(energy.EsaStateOnline); err != nil {
		return err
	}
	return h.demHost.PowerAdjust(ctx, p, d, c)
}

// TestDEMHookMayCallTheSetters pins G1-16 (ADR 0026 P6): the manager hooks
// run with the state lock released, so a hook that calls a state setter
// does not deadlock, and the command's own state stands after it.
func TestDEMHookMayCallTheSetters(t *testing.T) {
	t.Parallel()
	host := &settingDemHost{}
	srv, err := energy.NewDeviceEnergyManagement(energy.DeviceEnergyConfig{
		Features: demPFRSet, EsaType: dem.ESATypeEvse, EsaState: energy.EsaStateOnline,
		AbsMinPower: 1200000, AbsMaxPower: 7600000, Manager: host,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Close)
	host.srv = srv
	if err := srv.SetPowerAdjustmentCapability(triggerCapability()); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		done <- demInvoke(srv, dem.CmdPowerAdjustRequest, dem.PowerAdjustRequestRequest{Power: 10000000, Duration: 20, Cause: dem.AdjustmentCauseLocalOptimization})
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second): // a held lock never releases: this bound only names the failure
		t.Fatal("the server holds its state lock across the PowerAdjust hook")
	}
	if got := srv.EsaState(); got != energy.EsaStatePowerAdjustActive {
		t.Fatalf("ESAState %d after the command, want PowerAdjustActive (the command wins)", got)
	}
}

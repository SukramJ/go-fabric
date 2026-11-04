// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package fabrictest_test

import (
	"testing"

	"github.com/SukramJ/go-fabric/fabrictest"
)

// TestDeviceFactoryReset: a running device's factory reset brings a new,
// uncommissioned node back on the erased store; a stopped device's erases
// the store and stays stopped.
func TestDeviceFactoryReset(t *testing.T) {
	t.Parallel()
	site := fabrictest.NewSite(t, fabrictest.Options{})
	ctx := testContext(t)
	p := site.CommissionedPair(fabrictest.PairOptions{})
	if !p.Device.Running() {
		t.Fatal("the commissioned device does not run")
	}
	if err := p.Device.FactoryReset(ctx); err != nil {
		t.Fatal(err)
	}
	if !p.Device.Running() {
		t.Fatal("the device does not run after the reset of a running device")
	}
	if infos, err := p.Device.Node().FabricInfos(ctx); err != nil || len(infos) != 0 {
		t.Fatalf("fabrics after the reset %v (%v), want none", infos, err)
	}
	if err := p.Device.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	if err := p.Device.FactoryReset(ctx); err != nil {
		t.Fatal(err)
	}
	if p.Device.Running() {
		t.Fatal("the device runs after the reset of a stopped device")
	}
}

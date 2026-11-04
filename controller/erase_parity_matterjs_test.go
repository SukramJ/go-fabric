// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controller_test

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/controller"
	"github.com/SukramJ/go-fabric/controller/attestation"
	"github.com/SukramJ/go-fabric/controller/client/sqlstore"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/store"
)

// TestControllerEraseParityMatterJS ports the controller factory-reset
// cases of matter.js packages/node/test/node/ServerNodeTest.ts at 862ad75f.
// A Go controller is single-use: Erase closes it and erases its database,
// and the host builds the controller that comes back on the same database
// (BD-Matter-FactoryResetEndsTheNode); "commissions again on the same
// controller instance" (:1488-1498) is therefore the same case as the one
// after a reboot (:1500-1516).
func TestControllerEraseParityMatterJS(t *testing.T) { //nolint:tparallel // the subtests are ordered: the erase, then the reboot on its database
	t.Parallel()
	site := fabrictest.NewSite(t, fabrictest.Options{})
	dev := site.AddDevice(fabrictest.Topology{})
	cfg := site.ControllerConfig(fabrictest.ControllerOptions{})
	run := func(fn func(ctx context.Context) error) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		if err := site.Run(ctx, fn); err != nil {
			t.Fatal(err)
		}
	}
	var c *controller.Controller
	run(func(ctx context.Context) (err error) { c, err = controller.New(ctx, cfg); return err })
	run(func(ctx context.Context) error {
		_, err := c.Pair(ctx, manualCode(t, dev), controller.PairOptions{OnAttestationFailure: attestation.Accept()})
		return err
	})
	ctx := t.Context()
	settings := store.New(cfg.DB)
	nodes := sqlstore.New(cfg.DB)
	oldRoot := bytes.Clone(c.Fabric().CA.RootCert())

	// ServerNodeTest.ts:1317-1338: the peers and the CA key material are
	// in the storage before and gone after.
	t.Run("factory reset of a controller erases peers and CA key material", func(t *testing.T) {
		if ids, err := nodes.NodeIDs(); err != nil || len(ids) == 0 {
			t.Fatalf("stored nodes before the erase %v (%v), want the paired one", ids, err)
		}
		if _, ok, err := settings.GetSetting(ctx, "controller.ca"); err != nil || !ok {
			t.Fatalf("CA material before the erase: ok=%v err=%v", ok, err)
		}
		run(c.Erase)
		if ids, err := nodes.NodeIDs(); err != nil || len(ids) != 0 {
			t.Fatalf("stored nodes after the erase %v (%v), want none", ids, err)
		}
		for _, key := range []string{"controller.ca", "controller.ca.fabric"} {
			if _, ok, err := settings.GetSetting(ctx, key); err != nil || ok {
				t.Fatalf("%s after the erase: ok=%v err=%v", key, ok, err)
			}
		}
	})

	// ServerNodeTest.ts:1500-1516 (and :1488-1498): a controller booted on
	// the storage the erased one left holds no peer, has a new root and
	// commissions a device.
	t.Run("commissions again after factory reset of a controller with commissioned peers", func(t *testing.T) {
		again := site.ControllerConfig(fabrictest.ControllerOptions{})
		again.DB = cfg.DB
		var rebooted *controller.Controller
		run(func(ctx context.Context) (err error) { rebooted, err = controller.New(ctx, again); return err })
		t.Cleanup(func() { run(rebooted.Close) })
		if n := len(rebooted.Nodes()); n != 0 {
			t.Fatalf("%d nodes after the erase, want none", n)
		}
		if bytes.Equal(rebooted.Fabric().CA.RootCert(), oldRoot) {
			t.Fatal("the controller kept its root certificate across the erase")
		}
		fresh := site.AddDevice(fabrictest.Topology{})
		run(func(ctx context.Context) error {
			_, err := rebooted.Pair(ctx, manualCode(t, fresh), controller.PairOptions{OnAttestationFailure: attestation.Accept()})
			return err
		})
		if n := len(rebooted.Nodes()); n != 1 {
			t.Fatalf("%d nodes after commissioning again, want 1", n)
		}
	})
}

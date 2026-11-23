// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controller_test

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"thingfabric.dev/go-fabric/controller"
	"thingfabric.dev/go-fabric/controller/attestation"
	"thingfabric.dev/go-fabric/controller/softwareupdate"
	"thingfabric.dev/go-fabric/fabrictest"
)

// TestOTAForgetsADeletedNodeItNeverObserved holds R2-C2-fsm-2302: a node
// restored from the store and never started — no hook saw it with an
// address — is still forgotten by the update manager when the registry
// deletes it — the manager's NodeDeleted runs for its address (logged
// ota.manager.node_deleted), not only the next check's presence purge —
// and the consent the host gave for its address goes.
func TestOTAForgetsADeletedNodeItNeverObserved(t *testing.T) {
	t.Parallel()
	site := fabrictest.NewSite(t, fabrictest.Options{})
	dev := site.AddDevice(fabrictest.Topology{})
	otaCfg := func() *controller.OTAConfig {
		return &controller.OTAConfig{Images: softwareupdate.NewImages(softwareupdate.ImagesConfig{}), AllowTestImages: true}
	}
	cfg := site.ControllerConfig(fabrictest.ControllerOptions{OTA: otaCfg()})
	run := func(fn func(ctx context.Context) error) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		if err := site.Run(ctx, fn); err != nil {
			t.Fatal(err)
		}
	}
	var first *controller.Controller
	run(func(ctx context.Context) (err error) { first, err = controller.New(ctx, cfg); return err })
	run(func(ctx context.Context) error {
		_, err := first.Pair(ctx, manualCode(t, dev), controller.PairOptions{OnAttestationFailure: attestation.Accept()})
		return err
	})
	run(first.Close)

	again := site.ControllerConfig(fabrictest.ControllerOptions{OTA: otaCfg()})
	again.DB = cfg.DB
	again.ManualStart = true
	logs := &otaRig{site: site}
	again.Logger = slog.New(&logCapture{next: slog.DiscardHandler, r: logs})
	var c *controller.Controller
	run(func(ctx context.Context) (err error) { c, err = controller.New(ctx, again); return err })
	t.Cleanup(func() { run(c.Close) })
	nodes := c.Nodes()
	if len(nodes) != 1 {
		t.Fatalf("%d nodes restored, want 1", len(nodes))
	}
	n := nodes[0]
	p, ok := n.PeerAddress()
	if !ok {
		t.Fatal("the restored node has no address")
	}
	run(func(ctx context.Context) error {
		_, err := c.SoftwareUpdates().AddUpdateConsent(ctx, p, softwareupdate.Target{VendorID: 0xFFF1, ProductID: 0x8001, TargetSoftwareVersion: 2})
		return err
	})
	if !c.SoftwareUpdates().HasConsent(p, 0) {
		t.Fatal("no consent before the delete: the test shows nothing")
	}
	run(func(ctx context.Context) error { return c.Registry().Forget(ctx, n) })
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if err := site.Eventually(ctx, "the manager forgot the deleted node", func() bool { return logs.logged("ota.manager.node_deleted") }); err != nil {
		t.Fatal(err)
	}
	if c.SoftwareUpdates().HasConsent(p, 0) {
		t.Fatal("the deleted node keeps its consent")
	}
}

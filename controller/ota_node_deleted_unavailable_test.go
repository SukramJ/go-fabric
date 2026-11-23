// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controller_test

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"thingfabric.dev/go-fabric/controller"
	"thingfabric.dev/go-fabric/controller/attestation"
	"thingfabric.dev/go-fabric/controller/client"
	"thingfabric.dev/go-fabric/controller/client/sqlstore"
	"thingfabric.dev/go-fabric/controller/softwareupdate"
	"thingfabric.dev/go-fabric/fabrictest"
	"thingfabric.dev/go-fabric/tlv"
)

// TestOTAForgetsADeletedNodeThatFailedToLoad holds R3-fsm-3801, the
// unavailable-node half of R2-C2-fsm-2302: a node that failed to load —
// its stored record has a commissionedAt that is not a timestamp — keeps
// its stored address, and when the registry forgets it the update manager
// forgets that address (logged ota.manager.node_deleted) and the consent
// the host gave for it goes. Before the fix the unavailable node did not
// carry the controller's HooksFor chain, so startOTA's seed found no
// address cell and NodeDeleted never ran for it.
func TestOTAForgetsADeletedNodeThatFailedToLoad(t *testing.T) {
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
	var id string
	run(func(ctx context.Context) error {
		n, err := first.Pair(ctx, manualCode(t, dev), controller.PairOptions{OnAttestationFailure: attestation.Accept()})
		if err == nil {
			id = n.ID()
		}
		return err
	})
	run(first.Close)

	// commissionedAt (field 5 of the record, cluster 0xFFFF_FFFE on
	// endpoint 0) that is not a timestamp: the node loads unavailable.
	e := tlv.NewEncoder()
	e.PutUTF8(tlv.AnonymousTag(), "not a timestamp")
	b, err := e.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	if err := sqlstore.New(cfg.DB).Save(id, 0, 0xFFFF_FFFE, map[uint32][]byte{5: b}, nil); err != nil {
		t.Fatal(err)
	}

	again := site.ControllerConfig(fabrictest.ControllerOptions{OTA: otaCfg()})
	again.DB = cfg.DB
	again.ManualStart = true
	logs := &otaRig{site: site}
	again.Logger = slog.New(&logCapture{next: slog.DiscardHandler, r: logs})
	var c *controller.Controller
	run(func(ctx context.Context) (err error) { c, err = controller.New(ctx, again); return err })
	t.Cleanup(func() { run(c.Close) })
	n := c.Registry().Get(id)
	if n == nil || !errors.Is(n.LoadError(), client.ErrLoadFailed) {
		t.Fatalf("node %q %v, want one that failed to load", id, n)
	}
	p, ok := n.PeerAddress()
	if !ok {
		t.Fatal("the unavailable node has no stored address")
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

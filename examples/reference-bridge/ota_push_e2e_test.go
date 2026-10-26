// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"bytes"
	"context"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/bdx/otaimage"
	mattercore "github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/controller"
	"github.com/SukramJ/go-fabric/controller/attestation"
	"github.com/SukramJ/go-fabric/controller/softwareupdate"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/node"
	"github.com/SukramJ/go-fabric/secure/setup"
)

// TestOTAPushToTheReferenceDaemon is the proof of brief CM §1.4: the
// controller's OTA push (controller.Config.OTA — what the reference
// controller's "ota serve --announce" runs, examples/reference-controller/cli)
// updates the reference daemon's node — its composition (nodeConfig) with
// its OTA requestor and applier in harness mode, which applies in place
// and reports the new version — over the air on a fabrictest site.
func TestOTAPushToTheReferenceDaemon(t *testing.T) {
	t.Parallel()
	site := fabrictest.NewSite(t, fabrictest.Options{})
	logger := slog.New(slog.DiscardHandler)
	dir := t.TempDir()
	applier := &otaApplier{vendorID: testVendorID, productID: testProductID, dir: filepath.Join(dir, "ota"), harness: true, logger: logger}
	actions, err := mattercore.NewActions(mattercore.ActionsConfig{Host: newFleet().actions})
	if err != nil {
		t.Fatal(err)
	}
	const passcode, discriminator = 20202021, 3840
	cfg := nodeConfig(flags{dbPath: filepath.Join(dir, "daemon.db"), listen: "127.0.0.1:0", label: "reference-daemon", salt: "go-fabric-ref-salt", iterations: 1000},
		passcode, discriminator, node.TCPConfig{}, applier, actions, logger)
	daemon := site.AddNode(cfg)
	n := daemon.Node()
	applier.basicInfo = n.Root().BasicInformation
	applier.requestor = func() *node.OTARequestorServer { return n.Root().OTARequestor }

	images := softwareupdate.NewImages(softwareupdate.ImagesConfig{})
	done := make(chan struct{}, 1)
	ccfg := site.ControllerConfig(fabrictest.ControllerOptions{OTA: &controller.OTAConfig{
		Images: images, AllowTestImages: true,
		OnUpdateDone: func(softwareupdate.Peer) {
			select {
			case done <- struct{}{}:
			default:
			}
		},
	}})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	var c *controller.Controller
	if err := site.Run(ctx, func(ctx context.Context) (err error) { c, err = controller.New(ctx, ccfg); return err }); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = site.Run(context.Background(), c.Close) })
	code, err := setup.ManualCode(discriminator, passcode)
	if err != nil {
		t.Fatal(err)
	}
	var paired interface{ SubscriptionActive() bool }
	var peer softwareupdate.Peer
	if err := site.Run(ctx, func(ctx context.Context) error {
		cn, err := c.Pair(ctx, code, controller.PairOptions{OnAttestationFailure: attestation.Accept()})
		if err != nil {
			return err
		}
		paired = cn
		peer, _ = cn.PeerAddress()
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := site.Eventually(ctx, "the default subscription", paired.SubscriptionActive); err != nil {
		t.Fatal(err)
	}
	image, err := otaimage.Create(otaimage.CreateOptions{
		VendorID: testVendorID, ProductID: testProductID, SoftwareVersion: 2, SoftwareVersionString: "2.0",
		Payload: bytes.Repeat([]byte{0x5A, 0xA5}, 8*1024),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := images.Store(ctx, image, softwareupdate.ModeLocal, nil); err != nil {
		t.Fatal(err)
	}
	if err := site.Run(ctx, func(ctx context.Context) error {
		_, err := c.SoftwareUpdates().AddUpdateConsent(ctx, peer, softwareupdate.Target{VendorID: testVendorID, ProductID: testProductID, TargetSoftwareVersion: 2})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := site.Eventually(ctx, "the update done", func() bool { return len(done) > 0 }); err != nil {
		t.Fatal(err)
	}
	if v, _ := n.Root().BasicInformation.MatterRead(0x0009); v != uint32(2) {
		t.Errorf("the daemon's SoftwareVersion %v, want 2", v)
	}
}

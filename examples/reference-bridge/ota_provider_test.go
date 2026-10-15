// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"bytes"
	"context"
	"io"
	"path/filepath"
	"testing"

	"github.com/SukramJ/go-fabric/bdx/otaimage"
	mattercore "github.com/SukramJ/go-fabric/cluster/core"
	provdef "github.com/SukramJ/go-fabric/cluster/spec/otasoftwareupdateprovider"
	"github.com/SukramJ/go-fabric/node"
)

// TestOTAProviderFlag: with --ota-provider the node serves an OtaProvider
// endpoint whose test image is a valid OTA image one version above the
// daemon's, offered to the test vendor/product below it without a consent
// to ask for; the app pipe withholds and restores the consent. Without the
// flag there is no provider and the command is refused.
func TestOTAProviderFlag(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	plain := newFleet()
	n := testNode(t, plain, filepath.Join(t.TempDir(), "plain.db"))
	if n.OTAProvider() != nil {
		t.Fatal("an OTA provider without --ota-provider")
	}
	if err := plain.applyPipeCommand(pipeCommand{Name: "OtaProviderConsent", Action: "withhold"}); err == nil {
		t.Error("OtaProviderConsent accepted without --ota-provider")
	}

	images, err := newTestImages()
	if err != nil {
		t.Fatal(err)
	}
	f := newFleet()
	f.otaImages = images
	n = testNodeWith(t, f, filepath.Join(t.TempDir(), "provider.db"), func(cfg *node.Config) {
		cfg.OTAProvider = &node.OTAProviderConfig{ImageSource: images, Consent: images}
	})
	if n.OTAProvider() == nil {
		t.Fatal("no OTA provider with --ota-provider")
	}
	req := provdef.QueryImageRequest{VendorId: testVendorID, ProductId: testProductID, SoftwareVersion: 1}
	img, ok, err := images.QueryImage(ctx, mattercore.OTAPeer{}, req)
	if err != nil || !ok || img.SoftwareVersion != testProviderImageVersion || img.ConsentRequired {
		t.Fatalf("QueryImage %+v %v %v", img, ok, err)
	}
	blob, err := images.Open(ctx, string(img.FileDesignator))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(io.NewSectionReader(blob, 0, blob.Size()))
	if err != nil {
		t.Fatal(err)
	}
	size := uint64(len(raw))
	h, err := otaimage.ExtractPayload(bytes.NewReader(raw), io.Discard, &size)
	if err != nil || h.SoftwareVersion != testProviderImageVersion || h.VendorID != testVendorID || h.ProductID != testProductID || h.PayloadSize != testProviderPayloadSize {
		t.Fatalf("image header %+v %v", h, err)
	}
	if _, ok, _ := images.QueryImage(ctx, mattercore.OTAPeer{}, provdef.QueryImageRequest{VendorId: testVendorID, ProductId: testProductID, SoftwareVersion: 2}); ok {
		t.Error("an image offered to a node already at its version")
	}

	if err := f.applyPipeCommand(pipeCommand{Name: "OtaProviderConsent", Action: "withhold"}); err != nil {
		t.Fatal(err)
	}
	img, _, _ = images.QueryImage(ctx, mattercore.OTAPeer{}, req)
	if state, _, _ := images.RequestConsent(ctx, mattercore.OTAPeer{}, req, img); !img.ConsentRequired || state != mattercore.OTAConsentDenied {
		t.Fatalf("withheld: consent required %v, state %d", img.ConsentRequired, state)
	}
	if err := f.applyPipeCommand(pipeCommand{Name: "OtaProviderConsent", Action: "grant"}); err != nil {
		t.Fatal(err)
	}
	if img, _, _ = images.QueryImage(ctx, mattercore.OTAPeer{}, req); img.ConsentRequired {
		t.Fatal("granted: consent still required")
	}
	if err := f.applyPipeCommand(pipeCommand{Name: "OtaProviderConsent", Action: "maybe"}); err == nil {
		t.Error("an unknown action was accepted")
	}
}

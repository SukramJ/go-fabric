// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"strconv"
	"sync"
	"time"

	"github.com/SukramJ/go-fabric/node"

	provdef "github.com/SukramJ/go-fabric/cluster/spec/otasoftwareupdateprovider"
)

// TESTING ONLY: --ota-provider serves one embedded test image through the
// node's OTA Software Update Provider (ADR 0021): an OtaProvider endpoint
// beside the Aggregator, the image over BDX. It exists so an OTA requestor
// — the module's own, chip's ota-requestor-app — can be pointed at this
// daemon in a test; a product's provider serves the images its vendor
// signed, which this one does not have.
//
// The image is version testProviderImageVersion of the CSA test
// vendor/product, a few KiB of pattern bytes in the OTA image format
// (bdx/otaimage). The user's consent is granted unless the app pipe
// withholds it:
//
//	{"Name": "OtaProviderConsent", "Action": "withhold"}   the image needs a consent the provider denies
//	{"Name": "OtaProviderConsent", "Action": "grant"}      the consent exists again (the default)
//
// An allowed apply proceeds at once (no ApplyPolicy).

// The test image: one software version over the daemon's own
// (nodeConfig: SoftwareVersion 1), 4 KiB of payload.
const (
	testProviderImageVersion = 2
	testProviderImageName    = "reference-bridge-v2.ota"
	testProviderPayloadSize  = 4 * 1024
)

// testImages is the daemon's OTA image source and consent.
type testImages struct {
	image []byte

	mu       sync.Mutex
	withhold bool
}

// newTestImages builds the embedded image.
func newTestImages() (*testImages, error) {
	payload := make([]byte, testProviderPayloadSize)
	for i := range payload {
		payload[i] = byte(i * 7)
	}
	image, err := node.CreateOTAImage(node.OTAImageOptions{
		VendorID: testVendorID, ProductID: testProductID,
		SoftwareVersion: testProviderImageVersion, SoftwareVersionString: strconv.Itoa(testProviderImageVersion),
		Payload: payload,
	})
	if err != nil {
		return nil, fmt.Errorf("ota provider test image: %w", err)
	}
	return &testImages{image: image}, nil
}

var (
	_ node.OTAImageSource = (*testImages)(nil)
	_ node.OTAConsent     = (*testImages)(nil)
)

// QueryImage implements node.OTAImageSource: the image for the CSA
// test vendor/product below its version, nothing for anything else.
func (s *testImages) QueryImage(_ context.Context, _ node.OTAPeer, req provdef.QueryImageRequest) (node.OTAImage, bool, error) {
	if req.VendorId != testVendorID || req.ProductId != testProductID || req.SoftwareVersion >= testProviderImageVersion {
		return node.OTAImage{}, false, nil
	}
	s.mu.Lock()
	withhold := s.withhold
	s.mu.Unlock()
	return node.OTAImage{
		FileDesignator:        node.OTAFileDesignator(testProviderImageName),
		SoftwareVersion:       testProviderImageVersion,
		SoftwareVersionString: strconv.Itoa(testProviderImageVersion),
		ConsentRequired:       withhold,
	}, true, nil
}

// Open implements node.OTAImageSource.
func (s *testImages) Open(_ context.Context, file string) (node.OTABlob, error) {
	if file != testProviderImageName {
		return nil, fs.ErrNotExist
	}
	return bytes.NewReader(s.image), nil
}

// RequestConsent implements node.OTAConsent: denied while withheld.
func (s *testImages) RequestConsent(context.Context, node.OTAPeer, provdef.QueryImageRequest, node.OTAImage) (node.OTAConsentState, time.Duration, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.withhold {
		return node.OTAConsentDenied, 0, nil
	}
	return node.OTAConsentGranted, 0, nil
}

// pipeCommand applies OtaProviderConsent.
func (s *testImages) pipeCommand(cmd pipeCommand) error {
	if s == nil {
		return errors.New("OtaProviderConsent: the daemon runs without --ota-provider")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	switch cmd.Action {
	case "withhold":
		s.withhold = true
	case "grant":
		s.withhold = false
	default:
		return fmt.Errorf("OtaProviderConsent: Action %q, want withhold or grant", cmd.Action)
	}
	return nil
}

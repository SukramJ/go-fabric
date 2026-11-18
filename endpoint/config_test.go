// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package endpoint_test

import (
	"strings"
	"testing"

	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/endpoint/endpointtest"
)

// validEndpointConfig is the minimal config [endpoint.New] accepts.
func validEndpointConfig() endpoint.Config {
	return endpoint.Config{
		VendorID:  0x1234,
		ProductID: 0x5678,
	}
}

func TestConfigValidate_ZeroVendorID(t *testing.T) {
	t.Parallel()
	cfg := validEndpointConfig()
	cfg.VendorID = 0
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for zero VendorID, got nil")
	}
}

func TestConfigValidate_ZeroProductID(t *testing.T) {
	t.Parallel()
	cfg := validEndpointConfig()
	cfg.ProductID = 0
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for zero ProductID, got nil")
	}
}

func TestConfigValidate_OK(t *testing.T) {
	t.Parallel()
	if err := validEndpointConfig().Validate(); err != nil {
		t.Errorf("expected nil for valid config, got %v", err)
	}
}

// TestConfigValidate_VendorNameBeyondItsConstraint (R2-P6-wire-2502,
// decision 201): the bridge's VendorName, served uncut as the
// BridgedDeviceBasicInformation VendorName of every bridged endpoint that
// states none, is refused beyond the generated definition's "max 32" — the
// check an endpoint's own VendorName meets (checkBridgedInfo).
func TestConfigValidate_VendorNameBeyondItsConstraint(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, vendor string
		ok           bool
	}{
		{"empty", "", true},
		{"32", strings.Repeat("v", 32), true},
		{"33", strings.Repeat("v", 33), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := validEndpointConfig()
			cfg.VendorName = tc.vendor
			err := cfg.Validate()
			if (err == nil) != tc.ok {
				t.Fatalf("Validate(%q) = %v, want ok=%v", tc.vendor, err, tc.ok)
			}
			if err != nil && !strings.Contains(err.Error(), "VendorName") {
				t.Errorf("the refusal names no attribute: %v", err)
			}
			if _, err := endpoint.New(endpointtest.NewFakeStore(), cfg, nil); (err == nil) != tc.ok {
				t.Errorf("endpoint.New(%q) = %v, want ok=%v", tc.vendor, err, tc.ok)
			}
		})
	}
}

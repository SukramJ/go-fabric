// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package endpoint_test

import (
	"testing"

	"github.com/SukramJ/go-fabric/endpoint"
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

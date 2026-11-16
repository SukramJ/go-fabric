// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core_test

import (
	"testing"

	"github.com/SukramJ/go-fabric/cluster/core"
)

// TestBridgedBasicInfo_SerialEqualsUniqueIDIsValid verifies that a
// BridgedConfig where SerialNumber equals UniqueID still constructs
// successfully (the validation only warns, it never blocks), and that no
// SerialNumber is derived from UniqueID when the host states none
// (decision 143).
func TestBridgedBasicInfo_SerialEqualsUniqueIDIsValid(t *testing.T) {
	t.Parallel()
	// Case 1: explicit SerialNumber == UniqueID — constructor must succeed.
	cfg := core.BridgedConfig{
		UniqueID:     "dev-01:1",
		NodeLabel:    "Test",
		SerialNumber: "dev-01:1",
	}
	if _, err := core.NewBridgedDeviceBasicInformation(cfg); err != nil {
		t.Fatalf("NewBridgedDeviceBasicInformation with Serial==UniqueID: %v", err)
	}

	// Case 2: no SerialNumber stated — none is served.
	cfg2 := core.BridgedConfig{
		UniqueID:  "dev-02:1",
		NodeLabel: "Test2",
	}
	b2, err := core.NewBridgedDeviceBasicInformation(cfg2)
	if err != nil {
		t.Fatalf("NewBridgedDeviceBasicInformation with empty serial: %v", err)
	}
	if v, ok := b2.MatterRead(0x000F); ok {
		t.Errorf("SerialNumber = %v served although the host stated none", v)
	}
}

// TestBridgedBasicInfo_HonorsConfigReachable verifies that the
// constructor now reflects cfg.Reachable verbatim in both directions.
// The bridge passes the underlying CCU device's live availability
// (dev.Available()) through BridgedConfig.Reachable, so a dead device
// must surface Reachable=false at construction time rather than relying
// on a deferred SetReachable(false) that — because cluster servers are
// reconstructed per dispatch — would never persist.
func TestBridgedBasicInfo_HonorsConfigReachable(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		want bool
	}{
		{"reachable", true},
		{"unreachable", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := core.BridgedConfig{
				UniqueID:  "HM-00:1",
				NodeLabel: "Test",
				Reachable: tc.want,
			}
			b, err := core.NewBridgedDeviceBasicInformation(cfg)
			if err != nil {
				t.Fatalf("NewBridgedDeviceBasicInformation: %v", err)
			}
			v, ok := b.MatterRead(0x0011) // Reachable attr
			if !ok {
				t.Fatal("MatterRead(Reachable): ok=false")
			}
			reachable, isBool := v.(bool)
			if !isBool {
				t.Fatalf("Reachable = %T, want bool", v)
			}
			if reachable != tc.want {
				t.Errorf("Reachable = %v, want %v (cfg.Reachable honored)", reachable, tc.want)
			}
		})
	}
}

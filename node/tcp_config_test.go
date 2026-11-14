// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/SukramJ/go-fabric/node"
)

// TestNetworkTCPInboundBoundIsValidated: Network.TCP.MaxInboundConnections
// may be zero (the device default) or positive; a negative value is
// ErrInvalidConfig.
func TestNetworkTCPInboundBoundIsValidated(t *testing.T) {
	t.Parallel()
	for _, bound := range []int{0, 16} {
		cfg := minimalConfig(openDB(t, filepath.Join(t.TempDir(), "n.db")))
		cfg.Network.TCP = node.TCPConfig{Incoming: true, MaxInboundConnections: bound}
		if err := cfg.Validate(); err != nil {
			t.Errorf("MaxInboundConnections %d: %v", bound, err)
		}
	}
	cfg := minimalConfig(openDB(t, filepath.Join(t.TempDir(), "m.db")))
	cfg.Network.TCP = node.TCPConfig{Incoming: true, MaxInboundConnections: -1}
	if err := cfg.Validate(); !errors.Is(err, node.ErrInvalidConfig) {
		t.Fatalf("Validate with MaxInboundConnections -1 returned %v, want ErrInvalidConfig", err)
	}
}

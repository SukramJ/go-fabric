// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controller

import "testing"

// TestTCPInboundBoundDefaultsToTheControllerBound: the zero TCPConfig
// bounds the controller's listener at defaultMaxInboundTCPConnections
// (256); a configured value is kept.
func TestTCPInboundBoundDefaultsToTheControllerBound(t *testing.T) {
	t.Parallel()
	if got := (TCPConfig{Incoming: true}).maxInboundConnections(); got != 256 {
		t.Errorf("default bound %d, want 256", got)
	}
	if got := (TCPConfig{Incoming: true, MaxInboundConnections: 3}).maxInboundConnections(); got != 3 {
		t.Errorf("configured bound 3 resolves to %d", got)
	}
}

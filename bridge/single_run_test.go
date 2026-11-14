// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/bridge"
)

// TestStartAfterStopIsRefused (review II, R2-P2-stop-05): a bridge is
// single-run, as a node is — Start after Stop returns ErrStopped, whether
// the bridge ran or not, and a second Stop stays a no-op. Before, a
// second Start brought the bridge up again over the advertiser its Stop
// had closed.
func TestStartAfterStopIsRefused(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	ran := newTestBridge(t)
	if err := ran.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := ran.Stop(ctx); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if err := ran.Start(ctx); !errors.Is(err, bridge.ErrStopped) {
		_ = ran.Stop(ctx)
		t.Fatalf("Start after Stop = %v, want ErrStopped", err)
	}
	if err := ran.Stop(ctx); err != nil {
		t.Fatalf("a second Stop: %v", err)
	}

	never := newTestBridge(t)
	if err := never.Stop(ctx); err != nil {
		t.Fatalf("Stop without Start: %v", err)
	}
	if err := never.Start(ctx); !errors.Is(err, bridge.ErrStopped) {
		_ = never.Stop(ctx)
		t.Fatalf("Start after a Stop without Start = %v, want ErrStopped", err)
	}
}

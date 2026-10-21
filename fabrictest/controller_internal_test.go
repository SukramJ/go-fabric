// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package fabrictest

import (
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/controller/client"
)

// TestControllerFailedStartReleasesItsAddress pins the second half of the
// restart flake's fix (G1-15): a start that fails after it listened closes
// what it built, the listener first, so the next start on the host's
// address listens again instead of finding it in use.
func TestControllerFailedStartReleasesItsAddress(t *testing.T) {
	t.Parallel()
	site := NewSite(t, Options{Logger: slog.New(slog.DiscardHandler)})
	broken := false
	c := site.AddController(ControllerOptions{Registry: func(rc *client.RegistryConfig) {
		if broken {
			rc.Peers = nil // NewRegistry refuses: the last step of start fails
		}
	}})
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	broken = true
	err := c.Restart(ctx)
	if err == nil || !strings.Contains(err.Error(), "client registry") {
		t.Fatalf("Restart with a broken registry: %v, want the registry's error", err)
	}
	l, err := site.listen(c.index)
	if err != nil {
		t.Fatalf("the failed start still holds the address: %v", err)
	}
	_ = l.Close()
	broken = false
	if err := c.Restart(ctx); err != nil {
		t.Fatalf("Restart after the failed start: %v", err)
	}
	if c.Registry() == nil {
		t.Fatal("no registry after the restart")
	}
}

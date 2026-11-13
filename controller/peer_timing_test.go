// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controller_test

import (
	"context"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/client/peer"
	"github.com/SukramJ/go-fabric/controller"
	"github.com/SukramJ/go-fabric/controller/client"
)

// peerTiming opens a controller with timing as Config.PeerTiming and
// returns its peer set's timing.
func peerTiming(t *testing.T, timing *client.Timing) client.Timing {
	t.Helper()
	cfg := config(openDB(t))
	cfg.PeerTiming = timing
	c, err := controller.New(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close(context.Background()) })
	return c.Peers().Timing()
}

// TestPeerTimingNilIsTheDefaults: no PeerTiming leaves the peer set on
// client/peer's DefaultTiming.
func TestPeerTimingNilIsTheDefaults(t *testing.T) {
	t.Parallel()
	if got, want := peerTiming(t, nil), peer.DefaultTiming(); got != want {
		t.Errorf("timing %+v, want the defaults %+v", got, want)
	}
}

// TestPeerTimingProvenAddressRetention: a positive and a negative
// ProvenAddressRetention reach the peer set, every other field default.
func TestPeerTimingProvenAddressRetention(t *testing.T) {
	t.Parallel()
	for _, d := range []time.Duration{3 * time.Minute, -1} {
		want := peer.DefaultTiming()
		want.ProvenAddressRetention = d
		if got := peerTiming(t, &client.Timing{ProvenAddressRetention: d}); got != want {
			t.Errorf("retention %v: timing %+v, want %+v", d, got, want)
		}
	}
}

// TestPeerTimingOverridesFieldWise: one set field overrides its default,
// the zero fields keep theirs.
func TestPeerTimingOverridesFieldWise(t *testing.T) {
	t.Parallel()
	want := peer.DefaultTiming()
	want.DelayBeforeNextAddress = 7 * time.Second
	if got := peerTiming(t, &client.Timing{DelayBeforeNextAddress: 7 * time.Second}); got != want {
		t.Errorf("timing %+v, want %+v", got, want)
	}
}

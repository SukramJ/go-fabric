// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controller_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/controller"
	"github.com/SukramJ/go-fabric/controller/attestation"
	"github.com/SukramJ/go-fabric/controller/commissioner"
	"github.com/SukramJ/go-fabric/fabrictest"
)

// commissionBound is the CommissionTimeout of the tests below, shorter
// than the 13.5 s the site's exchange takes to give up on a silent peer.
const commissionBound = 5 * time.Second

// silenceAfterPASE is a Progress hook that drops every datagram of site
// once PASE is established, so the commissioning step after it waits for
// a device that no longer answers.
func silenceAfterPASE(site *fabrictest.Site) func(controller.PairingStep) {
	var once sync.Once
	return func(s controller.PairingStep) {
		if s.Name == commissioner.StepPASE && s.Phase == controller.PairingSucceeded {
			once.Do(func() { site.Net().Set(fabrictest.NetOptions{Loss: 1}) })
		}
	}
}

// checkBounded fails t unless the silenced commissioning ended through
// its context (context.Canceled, the step's own error) because the bound
// passed (context.DeadlineExceeded). Without the bound the step ends
// when its exchange gives up, "peer is no longer responding" (measured on
// the site), neither of the two. The end's time on the site's clock is
// no measure: the failed step is reported after the abort's disarm of
// the fail-safe (commissioner flow.go, disarmAfterAbort before the
// report).
func checkBounded(t *testing.T, err error) {
	t.Helper()
	if !errors.Is(err, context.DeadlineExceeded) || !errors.Is(err, context.Canceled) {
		t.Fatalf("pairing: %v, want the step canceled by the bound (context.Canceled and context.DeadlineExceeded)", err)
	}
}

// TestPairDiscoveredCommissionTimeout (go-fabric#554): PairOptions.
// CommissionTimeout bounds the commissioning of a device that is reached,
// though ctx has no deadline: a device that falls silent once PASE is
// established ends PairDiscovered with context.DeadlineExceeded at the
// bound on the controller's clock, and nothing is commissioned.
func TestPairDiscoveredCommissionTimeout(t *testing.T) {
	t.Parallel()
	site, dev, c, _, found := discoveredSite(t)
	hook := silenceAfterPASE(site)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	err := site.Run(ctx, func(ctx context.Context) error {
		_, err := c.PairDiscovered(ctx, found, dev.Passcode(), controller.PairOptions{
			OnAttestationFailure: attestation.Accept(), CommissionTimeout: commissionBound, Progress: hook,
		})
		return err
	})
	checkBounded(t, err)
	if len(c.Nodes()) != 0 {
		t.Fatalf("%d commissioned nodes after the commissioning's bound", len(c.Nodes()))
	}
}

// TestPairCommissionTimeoutBoundsEachAttempt (go-fabric#554): in Pair's
// discovery race the bound applies to the attempt on a found device too,
// though ctx has no deadline: a device that falls silent once PASE is
// established ends Pair with context.DeadlineExceeded at the bound, and
// nothing is commissioned.
func TestPairCommissionTimeoutBoundsEachAttempt(t *testing.T) {
	t.Parallel()
	site, dev, c := siteController(t, fabrictest.Topology{}, nil)
	hook := silenceAfterPASE(site)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	err := site.Run(ctx, func(ctx context.Context) error {
		_, err := c.Pair(ctx, manualCode(t, dev), controller.PairOptions{
			OnAttestationFailure: attestation.Accept(), CommissionTimeout: commissionBound, Progress: hook,
		})
		return err
	})
	checkBounded(t, err)
	if len(c.Nodes()) != 0 {
		t.Fatalf("%d commissioned nodes after the commissioning's bound", len(c.Nodes()))
	}
}

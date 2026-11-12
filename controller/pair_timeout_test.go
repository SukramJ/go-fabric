// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controller_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock"
	"github.com/SukramJ/go-fabric/controller"
	"github.com/SukramJ/go-fabric/controller/attestation"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/secure/setup"
)

// TestPairTimeoutBoundsTheSearchOnly: with a context without a deadline
// DefaultPairTimeout ends the search, not the commissioning of a device
// that was found — matter.js's commissionNode has no overall deadline
// (CommissioningController.ts:465-510 at db021da0), only the discovery's
// timeout (CommissioningDiscovery.ts:40-45). A flow that is still running
// when the timeout passes completes; found on BLE-to-Thread pairings that
// ended exactly one minute after their start, in the step-18 reconnect.
func TestPairTimeoutBoundsTheSearchOnly(t *testing.T) {
	t.Parallel()
	site, dev, c := siteController(t, fabrictest.Topology{}, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	var once sync.Once
	if err := site.Run(ctx, func(ctx context.Context) error {
		_, err := c.Pair(context.WithoutCancel(ctx), manualCode(t, dev), controller.PairOptions{
			OnAttestationFailure: attestation.Accept(),
			Progress: func(s controller.PairingStep) {
				// CommissioningComplete has disarmed the fail-safe and the
				// flow is not over (Subscribe follows): let the search's
				// timeout pass here, where no fail-safe of the device's
				// runs out meanwhile.
				if s.Number == 20 && s.Sub == 1 && s.Phase == controller.PairingSucceeded {
					once.Do(func() { <-clock.After(site.Clock(), controller.DefaultPairTimeout+time.Second) })
				}
			},
		})
		return err
	}); err != nil {
		t.Fatalf("Pair past DefaultPairTimeout: %v", err)
	}
	waited := true
	once.Do(func() { waited = false })
	if !waited {
		t.Fatal("the flow never reported CommissioningComplete: the timeout did not pass during it")
	}
	if len(c.Nodes()) != 1 {
		t.Fatalf("nodes: %d, want the commissioned one", len(c.Nodes()))
	}
}

// TestPairTimeoutEndsASilentSearch: a code no device answers is
// ErrNoDevice once DefaultPairTimeout has passed on the controller's
// clock.
func TestPairTimeoutEndsASilentSearch(t *testing.T) {
	t.Parallel()
	site, dev, c := siteController(t, fabrictest.Topology{}, nil)
	// A short discriminator the site's device does not have.
	absent, err := setup.ManualCode(dev.Discriminator()^0x0800, dev.Passcode())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	start := site.Clock().Now()
	err = site.Run(ctx, func(ctx context.Context) error {
		_, err := c.Pair(context.WithoutCancel(ctx), absent, controller.PairOptions{})
		return err
	})
	if !errors.Is(err, controller.ErrNoDevice) {
		t.Fatalf("silent search: %v, want ErrNoDevice", err)
	}
	if d := site.Clock().Now().Sub(start); d < controller.DefaultPairTimeout || d > controller.DefaultPairTimeout+10*time.Second {
		t.Fatalf("the search ended after %v, want DefaultPairTimeout", d)
	}
}

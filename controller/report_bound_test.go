// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controller_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/client/interaction"
	"github.com/SukramJ/go-fabric/controller"
	"github.com/SukramJ/go-fabric/controller/attestation"
	"github.com/SukramJ/go-fabric/controller/commissioner"
	"github.com/SukramJ/go-fabric/fabrictest"
)

// TestMaxReportBytesPerSessionReachesTheNodes: Config.MaxReportBytesPerSession
// bounds the report chunks a paired node's interactions hold (owner
// decision 48): under a bound below one report's chunk the pairing's read
// of the commissioned node fails with interaction.ErrReportTooLarge.
func TestMaxReportBytesPerSessionReachesTheNodes(t *testing.T) {
	t.Parallel()
	site, dev, c := siteController(t, fabrictest.Topology{}, func(cfg *controller.Config) {
		cfg.ManualStart = true
		cfg.MaxReportBytesPerSession = 8
	})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	err := site.Run(ctx, func(ctx context.Context) error {
		_, err := c.Pair(ctx, manualCode(t, dev), controller.PairOptions{OnAttestationFailure: attestation.Accept()})
		return err
	})
	if !errors.Is(err, interaction.ErrReportTooLarge) {
		t.Fatalf("Pair under an 8-byte report bound returned %v, want ErrReportTooLarge", err)
	}
}

// TestMaxReportBytesPerSessionBoundsTheCommissioningSteps: the commissioning
// steps' interactions over PASE and the first CASE session take the
// configured bound too, so under a bound below one report's chunk the
// pairing fails at the first commissioning read, not at the node's start
// ("Subscribe" 21.1) after it.
func TestMaxReportBytesPerSessionBoundsTheCommissioningSteps(t *testing.T) {
	t.Parallel()
	site, dev, c := siteController(t, fabrictest.Topology{}, func(cfg *controller.Config) {
		cfg.ManualStart = true
		cfg.MaxReportBytesPerSession = 8
	})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	var failed []controller.PairingStep
	err := site.Run(ctx, func(ctx context.Context) error {
		_, err := c.Pair(ctx, manualCode(t, dev), controller.PairOptions{
			OnAttestationFailure: attestation.Accept(),
			Progress: func(s controller.PairingStep) {
				if s.Phase == controller.PairingFailed {
					failed = append(failed, s)
				}
			},
		})
		return err
	})
	if !errors.Is(err, interaction.ErrReportTooLarge) {
		t.Fatalf("Pair under an 8-byte report bound returned %v, want ErrReportTooLarge", err)
	}
	if len(failed) == 0 || failed[0].Name == commissioner.StepSubscribe {
		t.Fatalf("failed steps %+v, want a commissioning step before %q", failed, commissioner.StepSubscribe)
	}
}

// TestMaxReportBytesPerSessionNegativeIsRefused: a negative bound is a
// configuration error.
func TestMaxReportBytesPerSessionNegativeIsRefused(t *testing.T) {
	t.Parallel()
	cfg := config(openDB(t))
	cfg.MaxReportBytesPerSession = -1
	if c, err := controller.New(context.Background(), cfg); !errors.Is(err, controller.ErrConfiguration) {
		if err == nil {
			_ = c.Close(context.Background())
		}
		t.Fatalf("New with a negative MaxReportBytesPerSession returned %v, want ErrConfiguration", err)
	}
}

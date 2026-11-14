// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controller_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/controller"
	"github.com/SukramJ/go-fabric/controller/attestation"
	"github.com/SukramJ/go-fabric/controller/commissioner"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/node"
	"github.com/SukramJ/go-fabric/surface"
)

// devicePASESessions counts the PASE sessions the device's session table
// holds.
func devicePASESessions(t *testing.T, dev *fabrictest.Device) int {
	t.Helper()
	n := 0
	for _, s := range dev.Node().Sessions().Sessions() {
		if s.IsPASE {
			n++
		}
	}
	return n
}

// TestPairThatGivesUpLeavesNoPASESession: a Pair the commissioner gives up
// after its PASE session was established — for a reason of its own: the
// device's terms not acknowledged, the device's attestation refused by the
// policy, the caller's context cancelled — leaves the device without a
// PASE session the moment it returns, with no time passing on the site
// clock, so the next Pair is accepted at once. The device accepts no new
// PASE exchange while a PASE session exists that is not closing (matter.js
// packages/protocol/src/session/pase/PaseServer.ts:80-82 at db021da0);
// the commissioner expires the fail-safe the device armed on the PASE
// session's establishment, and the device ends the session with it.
func TestPairThatGivesUpLeavesNoPASESession(t *testing.T) {
	t.Parallel()
	terms := fabrictest.Topology{Configure: func(cfg *node.Config) {
		cfg.Commissioning.TermsAndConditions = &node.TermsAndConditions{Version: 1, Acknowledgements: 1}
		cfg.Surface = surface.Policies{Extensions: surface.NewExtensions("BD-chip-GeneralCommissioning-TermsAndConditions")}
	}}
	accept := controller.PairOptions{OnAttestationFailure: attestation.Accept()}
	rows := []struct {
		name  string
		topo  fabrictest.Topology
		first func(cancel context.CancelFunc) controller.PairOptions
		want  func(error) bool
		retry controller.PairOptions
	}{
		{
			name:  "terms not acknowledged",
			topo:  terms,
			first: func(context.CancelFunc) controller.PairOptions { return accept },
			want: func(err error) bool {
				var te *commissioner.TermsRequiredError
				return errors.As(err, &te)
			},
			retry: controller.PairOptions{
				OnAttestationFailure: attestation.Accept(),
				TermsAndConditions:   &commissioner.TermsAndConditionsAcknowledgement{Version: 1, Acknowledgements: 1},
			},
		},
		{
			name: "attestation refused by the policy",
			first: func(context.CancelFunc) controller.PairOptions {
				return controller.PairOptions{OnAttestationFailure: attestation.Reject()}
			},
			want:  func(err error) bool { return errors.Is(err, commissioner.ErrAttestationRejected) },
			retry: accept,
		},
		{
			name: "context cancelled after PASE",
			first: func(cancel context.CancelFunc) controller.PairOptions {
				return controller.PairOptions{OnAttestationFailure: attestation.Accept(), Progress: func(s controller.PairingStep) {
					if s.Name == commissioner.StepPASE && s.Phase == controller.PairingSucceeded {
						cancel()
					}
				}}
			},
			want:  func(err error) bool { return errors.Is(err, context.Canceled) },
			retry: accept,
		},
	}
	for _, r := range rows {
		t.Run(r.name, func(t *testing.T) {
			t.Parallel()
			site, dev, c := siteController(t, r.topo, nil)
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			var first error
			if err := site.Run(ctx, func(ctx context.Context) error {
				pctx, stop := context.WithCancel(ctx)
				defer stop()
				_, first = c.Pair(pctx, manualCode(t, dev), r.first(stop))
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			if !r.want(first) {
				t.Fatalf("first Pair: %v", first)
			}
			// The site clock stands still from here: the device's own
			// fail-safe cannot have ended the session.
			if err := site.Settle(ctx); err != nil {
				t.Fatal(err)
			}
			if n := devicePASESessions(t, dev); n != 0 {
				t.Fatalf("after the Pair that gave up (%v) the device holds %d PASE session(s), want 0", first, n)
			}
			if err := site.Run(ctx, func(ctx context.Context) error {
				_, err := c.Pair(ctx, manualCode(t, dev), r.retry)
				return err
			}); err != nil {
				t.Fatalf("the next Pair: %v", err)
			}
		})
	}
}

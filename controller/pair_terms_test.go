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
	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/controller/commissioner"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/node"
	"github.com/SukramJ/go-fabric/surface"
)

// TestPairTermsRequired: a device that requires terms and conditions
// (version 1, acknowledgement bit 1, as the reference daemon's
// --tc-required) fails a Pair without the acknowledgement early, with a
// *commissioner.TermsRequiredError of version 1 and the device left
// uncommissioned, and is commissioned by a Pair whose
// PairOptions.TermsAndConditions acknowledges it
// (BD-Matter-Commissioner-TermsRequirementRead).
func TestPairTermsRequired(t *testing.T) {
	t.Parallel()
	site, dev, c := siteController(t, fabrictest.Topology{Configure: func(cfg *node.Config) {
		cfg.Commissioning.TermsAndConditions = &node.TermsAndConditions{Version: 1, Acknowledgements: 1}
		cfg.Surface = surface.Policies{Extensions: surface.NewExtensions("BD-chip-GeneralCommissioning-TermsAndConditions")}
	}}, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	var refused error
	var n *client.Node
	if err := site.Run(ctx, func(ctx context.Context) (err error) {
		_, refused = c.Pair(ctx, manualCode(t, dev), controller.PairOptions{OnAttestationFailure: attestation.Accept()})
		n, err = c.Pair(ctx, manualCode(t, dev), controller.PairOptions{
			OnAttestationFailure: attestation.Accept(),
			TermsAndConditions:   &commissioner.TermsAndConditionsAcknowledgement{Version: 1, Acknowledgements: 1},
		})
		return err
	}); err != nil {
		t.Fatalf("Pair with the acknowledgement: %v", err)
	}
	var te *commissioner.TermsRequiredError
	if !errors.As(refused, &te) || te.Version != 1 || te.Deadline != nil {
		t.Fatalf("Pair without the acknowledgement: %v, want a *TermsRequiredError of version 1", refused)
	}
	if !errors.Is(refused, commissioner.ErrCommissioning) {
		t.Fatalf("%v does not match ErrCommissioning", refused)
	}
	if !n.IsCommissioned() || len(c.Nodes()) != 1 {
		t.Fatalf("commissioned %v, nodes %d; want the one node of the second Pair", n.IsCommissioned(), len(c.Nodes()))
	}
}

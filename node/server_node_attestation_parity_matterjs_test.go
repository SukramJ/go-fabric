// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/node"
)

// TestServerNodeAttestationProviderParityMatterJS ports matter.js
// packages/node/test/node/ServerNodeTest.ts:409-459 at 862ad75f
// ("commissions with delayed provided certificates"): the certification
// provider is called neither when the node is created nor when it starts,
// only when the commissioner asks for the attestation material — matter.js
// OperationalCredentialsServer.ts:560-572 builds the DeviceCertification on
// first use. matter.js arms the fail-safe and sets the regulatory config
// before the CertificateChainRequest that calls it; the port commissions
// over the wire, whose flow asks the same way, and holds that the provider
// ran exactly once across the CertificateChainRequests, AttestationRequest
// and CSRRequest the flow sends. The provider returns no chain and no CD,
// which is the CSA test material the fabrictest commissioner accepts.
func TestServerNodeAttestationProviderParityMatterJS(t *testing.T) {
	t.Parallel()
	site := quietSite(t)
	var calls atomic.Int32
	u := site.UncommissionedPair(fabrictest.PairOptions{Device: fabrictest.Topology{Configure: func(cfg *node.Config) {
		cfg.Attestation.Provider = func(context.Context) (*node.AttestationChain, []byte, error) {
			calls.Add(1)
			return nil, nil, nil
		}
	}}})
	if err := site.Settle(parityCtx(t)); err != nil {
		t.Fatal(err)
	}
	if n := calls.Load(); n != 0 {
		t.Fatalf("provider called %d times at creation and start, want 0", n)
	}
	p, err := u.Controller.Commission(parityCtx(t), u.Device, fabrictest.CommissionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !p.Commissioned() {
		t.Fatal("not commissioned")
	}
	if n := calls.Load(); n != 1 {
		t.Fatalf("provider called %d times by the commissioning, want 1", n)
	}
}

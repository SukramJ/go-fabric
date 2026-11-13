// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	mattercore "github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/secure/attestation"
)

// productChain is a CSA test chain for VendorID 0xFFF1 and pid.
func productChain(t *testing.T, pid uint16) *attestation.Chain {
	t.Helper()
	chain, err := attestation.BuildTestChainAt(0xFFF1, pid, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return chain
}

// TestAttestationChainMustMatchTheProduct: a chain the host sets in
// Attestation.Chain is checked against the product description as one
// Attestation.Files loads is — a chain of another product fails New with
// attestation.ErrProductMismatch, the product's own builds the node.
func TestAttestationChainMustMatchTheProduct(t *testing.T) {
	t.Parallel()
	cfg := mdnsTestConfig(t) // ProductDescription 0xFFF1/0x8001
	cfg.Network.MDNS.Off = true
	cfg.Attestation.Chain = productChain(t, 0x8000)
	if n, err := New(t.Context(), cfg); !errors.Is(err, attestation.ErrProductMismatch) {
		if n != nil {
			_ = n.Stop(context.Background())
		}
		t.Fatalf("New with another product's chain: %v, want ErrProductMismatch", err)
	}
	cfg = mdnsTestConfig(t)
	cfg.Network.MDNS.Off = true
	cfg.Attestation.Chain = productChain(t, 0x8001)
	newMDNSTestNode(t, cfg)
}

// TestAttestationProviderChainMustMatchTheProduct: a chain
// Attestation.Provider returns is checked against the product description
// when the commissioner asks for it — another product's fails that
// request with attestation.ErrProductMismatch, the product's own is
// served.
func TestAttestationProviderChainMustMatchTheProduct(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		pid     uint16
		wantErr error
	}{
		{"another product", 0x8000, attestation.ErrProductMismatch},
		{"the product", 0x8001, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := mdnsTestConfig(t)
			chain := productChain(t, tc.pid)
			cfg.Attestation.Provider = func(context.Context) (*attestation.Chain, []byte, error) { return chain, nil, nil }
			var opcreds mattercore.OpcredsConfig
			if err := withAttestation(&cfg, &opcreds); err != nil {
				t.Fatal(err)
			}
			m, err := opcreds.AttestationProvider(t.Context())
			if !errors.Is(err, tc.wantErr) || (tc.wantErr == nil && err != nil) {
				t.Fatalf("the provider's chain: %v, want %v", err, tc.wantErr)
			}
			if tc.wantErr == nil && !bytes.Equal(m.DAC, chain.DAC) {
				t.Fatal("the product's own chain is not the one served")
			}
		})
	}
}

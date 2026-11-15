// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package client_test

import (
	"testing"

	"github.com/SukramJ/go-fabric/controller/attestation"
	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/fabrictest"
	secattestation "github.com/SukramJ/go-fabric/secure/attestation"
)

// TestRecordKeepsTheAttestedProduct: a commission whose attestation
// validates keeps the BasicInformation vendor and product id the
// Certification Declaration was checked against (review II decision 105);
// a decommission clears them; a commission whose attestation failed and
// was accepted by policy keeps none.
func TestRecordKeepsTheAttestedProduct(t *testing.T) {
	t.Run("attested, then decommissioned", func(t *testing.T) {
		cp := newClientPair(t, clientPairOptions{
			rootOnly:   true,
			controller: fabrictest.ControllerOptions{Attestation: attestationStore(true, secattestation.TestPAANoVIDCert)},
		})
		rec := cp.node.Record()
		// fabrictest's device is VendorID 0xFFF1, ProductID 0x8001
		// (fabrictest/device.go's ProductDescription).
		if rec.AttestedVendorID == nil || rec.AttestedProductID == nil || *rec.AttestedVendorID != 0xFFF1 || *rec.AttestedProductID != 0x8001 {
			t.Fatalf("attested %v/%v, want 0xFFF1/0x8001", rec.AttestedVendorID, rec.AttestedProductID)
		}
		if err := cp.decommission(t); err != nil {
			t.Fatal(err)
		}
		if rec := cp.node.Record(); rec.AttestedVendorID != nil || rec.AttestedProductID != nil {
			t.Fatalf("after decommission attested %v/%v, want none", rec.AttestedVendorID, rec.AttestedProductID)
		}
	})
	t.Run("a failure the policy accepted is not attested", func(t *testing.T) {
		cp := newClientPair(t, clientPairOptions{
			rootOnly:   true,
			controller: fabrictest.ControllerOptions{Attestation: attestationStore(true, secattestation.TestPAAFFF1Cert)},
			commission: client.CommissionOptions{OnAttestationFailure: attestation.Accept()},
		})
		if rec := cp.node.Record(); rec.AttestedVendorID != nil || rec.AttestedProductID != nil {
			t.Fatalf("attested %v/%v after an accepted failure, want none", rec.AttestedVendorID, rec.AttestedProductID)
		}
	})
}

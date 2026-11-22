// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package commissioning_test

import (
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"math/big"
	"testing"
	"time"

	"thingfabric.dev/go-fabric/commissioning"
)

// TestVerifyChainRefusesTwoCertificateChain pins VerifyChain's chain-length
// guard: a DAC the PAA signed directly verifies under crypto/x509 as the
// two-certificate chain DAC ← PAA, which has no PAI. VerifyChain returns
// the DAC/PAI/PAA trio by index, so without the guard it would index past
// the chain and panic; with it the chain is refused as broken
// (R2-P5-tmut-09).
func TestVerifyChainRefusesTwoCertificateChain(t *testing.T) {
	t.Parallel()
	now := time.Now()
	tc := newTestChain(t, now)
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(0x2D2D),
		Subject:               pkix.Name{CommonName: "Test DAC under the PAA"},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(24 * time.Hour),
		BasicConstraintsValid: true,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageAny},
	}
	dacDer, err := x509.CreateCertificate(rand.Reader, tmpl, tc.PAACert, &tc.DACKey.PublicKey, tc.PAAKey)
	if err != nil {
		t.Fatalf("create DAC: %v", err)
	}

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("VerifyChain panicked on a two-certificate chain: %v", r)
		}
	}()
	dac, pai, paa, err := commissioning.VerifyChain(commissioning.DACChain{DAC: dacDer, PAI: tc.PAIDer}, tc.paaPool(t), now)
	if !errors.Is(err, commissioning.ErrDACChainBroken) {
		t.Fatalf("err = %v, want ErrDACChainBroken", err)
	}
	if dac != nil || pai != nil || paa != nil {
		t.Errorf("a refused chain returned certificates: dac=%v pai=%v paa=%v", dac != nil, pai != nil, paa != nil)
	}
}

// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package attestation_test

import (
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/controller/attestation"
)

// TestAttestationValidityAtDacNotBefore pins
// BD-Matter-Attestation-ValidityAtDacNotBefore (notes/parity/by_design.md):
// the attestation chain is judged at the DAC's notBefore, so a DAC past
// its own notAfter and a PAI that has expired since the DAC was issued
// pass — the current time is not part of the verdict.
//
// matter.js's validator checks "Certificate validity at DAC's notBefore
// timestamp": the DAC's notBefore against the PAI's and the PAA's
// notBefore and notAfter, a notAfter of 0 being none, and no clock
// (packages/protocol/src/certificate/DeviceAttestationValidator.ts:393-419
// at db021da0). connectedhomeip refuses the expired DAC where it has a
// clock (src/credentials/attestation_verifier/
// DefaultDeviceAttestationVerifier.cpp:638-641 at 6170af84, kDacExpired);
// the row records why this module stays with matter.js.
//
// Every date below lies before the day this test was written
// (2026-11-13), so "expired" holds on any clock the test runs under.
func TestAttestationValidityAtDacNotBefore(t *testing.T) {
	t.Parallel()
	day := func(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }

	t.Run("a DAC past its notAfter is accepted", func(t *testing.T) {
		t.Parallel()
		s := defaultSpec() // PAI from 2024-01-01, DAC from 2025-01-01
		s.dacNotAfter = day(2025, 6, 1)
		f := newFixture(t, s, defaultCD())
		if dac := davParse(t, f.data.DAC); !dac.NotAfter.Before(time.Now()) {
			t.Fatalf("the fixture's DAC is not expired: notAfter %v", dac.NotAfter)
		}
		davPass(t, f)
	})
	t.Run("a PAI that expired after the DAC was issued is accepted", func(t *testing.T) {
		t.Parallel()
		s := defaultSpec()
		s.paiNotAfter = day(2025, 3, 1) // the DAC's notBefore, 2025-01-01, is inside
		f := newFixture(t, s, defaultCD())
		if pai := davParse(t, f.data.PAI); !pai.NotAfter.Before(time.Now()) {
			t.Fatalf("the fixture's PAI is not expired: notAfter %v", pai.NotAfter)
		}
		davPass(t, f)
	})
	t.Run("an expired DAC under an expired PAI is accepted", func(t *testing.T) {
		t.Parallel()
		s := defaultSpec()
		s.paiNotAfter, s.dacNotAfter = day(2025, 3, 1), day(2025, 2, 1)
		davPass(t, newFixture(t, s, defaultCD()))
	})
	// The control: the same fixture is refused as soon as the DAC's
	// notBefore — the one instant the rule looks at — leaves the PAI's
	// period (DeviceAttestationValidator.ts:402-407 at db021da0).
	t.Run("control: a DAC issued after the PAI's notAfter is refused", func(t *testing.T) {
		t.Parallel()
		s := defaultSpec()
		s.paiNotAfter = day(2024, 6, 1) // before the DAC's notBefore, 2025-01-01
		davReject(t, newFixture(t, s, defaultCD()), attestation.CertificateExpired, `DAC notBefore \(\d+\) is after PAI notAfter \(\d+\)`)
	})
}

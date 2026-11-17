// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package attestation_test

import (
	"slices"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock/clocktest"
)

// TestStricterDeviceChecksAreErrorFindings: a DAC without a ProductID, a
// PAI whose ProductID is not the DAC's and — with a clock in the Context —
// a DAC past its notAfter are error findings: Validate returns no error,
// the commissioner's policy decides, and an error finding withholds the
// attested pair (owner decision 119). Ahead of matter.js, whose validator
// compares product ids only against the Certification Declaration and
// judges validity at the DAC's notBefore (review II, R2-P6-par-1804,
// R2-P6-par-1805, owner decision 193).
func TestStricterDeviceChecksAreErrorFindings(t *testing.T) {
	t.Parallel()
	pid := func(v uint16) *uint16 { return &v }
	day := func(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }
	twoPIDs := defaultCD()
	twoPIDs.pids = []uint16{0x8000, 0x8001}
	for _, c := range []struct {
		name string
		spec func(*chainSpec)
		cd   cdSpec
		now  time.Time // zero: no clock in the Context
		want []string
	}{
		{
			"a DAC without a ProductID", func(s *chainSpec) { s.dacNoPID = true }, defaultCD(),
			time.Time{},
			[]string{"error:DacFormatInvalid", "info:CertificationTypeTest"},
		},
		{
			"a PAI ProductID that is not the DAC's", func(s *chainSpec) { s.paiPID = pid(0x8001) }, twoPIDs,
			time.Time{},
			[]string{"error:CertificateChainInvalid", "info:CertificationTypeTest"},
		},
		{
			"control: a PAI ProductID equal to the DAC's", func(s *chainSpec) { s.paiPID = pid(0x8000) }, defaultCD(),
			time.Time{},
			[]string{"info:CertificationTypeTest"},
		},
		{
			"a DAC past its notAfter at the clock's time", func(s *chainSpec) { s.dacNotAfter = day(2025, 6, 1) }, defaultCD(), day(2025, 6, 2),
			[]string{"error:CertificateExpired", "info:CertificationTypeTest"},
		},
		{
			"control: the same DAC inside its period", func(s *chainSpec) { s.dacNotAfter = day(2025, 6, 1) }, defaultCD(), day(2025, 5, 31),
			[]string{"info:CertificationTypeTest"},
		},
		{
			"control: the same DAC without a clock (matter.js's rule)", func(s *chainSpec) { s.dacNotAfter = day(2025, 6, 1) }, defaultCD(),
			time.Time{},
			[]string{"info:CertificationTypeTest"},
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			s := defaultSpec()
			c.spec(&s)
			f := newFixture(t, s, c.cd)
			if !c.now.IsZero() {
				f.ctx.Clock = clocktest.New(c.now)
			}
			res := davPass(t, f)
			if got := checks(res.Findings); !slices.Equal(got, c.want) {
				t.Fatalf("findings %v, want %v", got, c.want)
			}
		})
	}
}

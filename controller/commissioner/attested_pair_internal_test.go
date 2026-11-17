// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package commissioner

import (
	"errors"
	"log/slog"
	"net"
	"testing"

	"github.com/SukramJ/go-fabric/client/exchange"
	"github.com/SukramJ/go-fabric/client/interaction"
	"github.com/SukramJ/go-fabric/controller/attestation"
)

// TestAttestedPairWithheld: an attestation that returned no error records
// its vendor/product pair only when the chain was verified and no error
// finding stands — no trust store, a CD authorized_paa_list that could not
// be checked without one, and an accepted LevelError finding each withhold
// it; an informational or other warning finding does not.
func TestAttestedPairWithheld(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name     string
		findings []attestation.Finding
		withheld bool
	}{
		{"no finding", nil, false},
		{"no trust store", []attestation.Finding{{Level: attestation.LevelWarning, Check: attestation.DclServiceUnavailable, Message: "no trust store"}}, true},
		{"authorized_paa_list unchecked", []attestation.Finding{{Level: attestation.LevelInfo, Check: attestation.CertificationTypeTest}, {Level: attestation.LevelWarning, Check: attestation.DclServiceUnavailable, Message: "CD has authorized_paa_list"}}, true},
		{"an error finding", []attestation.Finding{{Level: attestation.LevelError, Check: attestation.TrustedAsTestCertificate, Message: "test PAA"}}, true},
		{"informational and warning findings", []attestation.Finding{{Level: attestation.LevelInfo, Check: attestation.PaaTrustStoreTimeMismatch}, {Level: attestation.LevelWarning, Check: attestation.CertificationTypeProvisional}}, false},
	} {
		why, withheld := attestedPairWithheld(c.findings)
		if withheld != c.withheld || (withheld && why == "") {
			t.Errorf("%s: withheld %v (%q), want %v", c.name, withheld, why, c.withheld)
		}
	}
}

// TestAttestedPairStrictFindingsWithhold: the error findings the validator
// raises beyond matter.js on the device's own certificates withhold the
// attested pair like any other (review II, R2-P6-par-1804,
// R2-P6-par-1805, owner decisions 119 and 193).
func TestAttestedPairStrictFindingsWithhold(t *testing.T) {
	t.Parallel()
	for _, c := range []attestation.Check{attestation.DacFormatInvalid, attestation.CertificateChainInvalid, attestation.CertificateExpired} {
		if _, withheld := attestedPairWithheld([]attestation.Finding{{Level: attestation.LevelError, Check: c, Message: "x"}}); !withheld {
			t.Errorf("%s: the pair is recorded", c)
		}
	}
}

// TestAttestationStepClearsAnEarlierPair: the attestation step clears the
// pair an earlier run of it recorded before it does anything else, so a
// run that fails — or withholds the pair — never leaves the earlier run's
// pair standing (review II, R2-P6-fsm-1811). A step runs once per flow
// today; the test holds the rule for a retry added later.
func TestAttestationStepClearsAnEarlierPair(t *testing.T) {
	t.Parallel()
	m, err := exchange.NewManager(refusingConn{}, exchange.Config{})
	if err != nil {
		t.Fatal(err)
	}
	s, err := m.NewUnsecuredSession(&net.UDPAddr{IP: net.IPv6loopback, Port: 5540}, nil)
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	f := &flow{log: slog.New(slog.DiscardHandler), ia: interaction.New(m, s, nil), attested: &[2]uint16{0xFFF1, 0x8000}}
	if _, err := f.deviceAttestation(t.Context()); err == nil {
		t.Fatal("the step succeeded on a closed session")
	}
	if f.attested != nil {
		t.Fatalf("the earlier pair %v still stands", *f.attested)
	}
}

type refusingConn struct{}

func (refusingConn) Send(*net.UDPAddr, []byte) error { return errors.New("refused") }

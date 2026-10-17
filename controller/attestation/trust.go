// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package attestation

import (
	"crypto/ecdsa"
	"fmt"
)

// TrustMode is which roots a controller's attestation check trusts.
type TrustMode int

// The trust modes.
const (
	// Development trusts the DCL snapshot and the embedded CSA test PAAs
	// ([TestStore]) — devices in development and production devices.
	Development TrustMode = iota
	// Production trusts the DCL snapshot only, with the online ledger on
	// top when one is configured.
	Production
	// None checks no chain: the commissioner runs without a trust store
	// (commissioner.Context.Attestation nil, a DclServiceUnavailable
	// warning finding). Only on an explicit choice.
	None
)

// String is the mode's flag value.
func (m TrustMode) String() string {
	switch m {
	case Development:
		return "development"
	case Production:
		return "production"
	case None:
		return "none"
	}
	return fmt.Sprintf("TrustMode(%d)", int(m))
}

// ParseTrustMode reads a [TrustMode.String] value.
func ParseTrustMode(s string) (TrustMode, error) {
	for _, m := range []TrustMode{Development, Production, None} {
		if s == m.String() {
			return m, nil
		}
	}
	return 0, fmt.Errorf("attestation: trust mode %q, want development, production or none", s)
}

// Trust is the trust store of mode: in Development the snapshot, the
// online store and the CSA test PAAs, in that order; in Production the
// snapshot and the online store; nil in None. snapshot and online may be
// nil (an empty snapshot, no ledger). Revocation is the online store's
// alone: the snapshot holds trust anchors, never revocation data.
func Trust(mode TrustMode, snapshot, online PAAStore) PAAStore {
	var l Layered
	for _, s := range []PAAStore{snapshot, online} {
		if s != nil {
			l = append(l, s)
		}
	}
	switch mode {
	case Development:
		return append(l, TestStore())
	case Production:
		return l
	case None:
	}
	return nil
}

// Layered is a [PAAStore] over several: a root or CD signer is the first
// layer's that has it; test certificates are accepted when a layer
// accepts them. It is [Revocations] through the first layer that is one —
// a layer that is none (a snapshot, a static store) never answers a
// revocation question, so a root found in it is still checked against the
// online store's CRLs.
type Layered []PAAStore

var (
	_ PAAStore    = Layered(nil)
	_ Revocations = Layered(nil)
)

// PAA implements [PAAStore].
func (l Layered) PAA(skid []byte) (PAA, bool) {
	for _, s := range l {
		if p, ok := s.PAA(skid); ok {
			return p, true
		}
	}
	return PAA{}, false
}

// AcceptsTestCertificates implements [PAAStore].
func (l Layered) AcceptsTestCertificates() bool {
	for _, s := range l {
		if s.AcceptsTestCertificates() {
			return true
		}
	}
	return false
}

// CDSigner implements [PAAStore].
func (l Layered) CDSigner(skid []byte) (*ecdsa.PublicKey, bool) {
	for _, s := range l {
		if k, ok := s.CDSigner(skid); ok {
			return k, true
		}
	}
	return nil, false
}

// IsRevoked implements [Revocations]: the first layer that is
// [Revocations] answers; without one nothing is revoked.
func (l Layered) IsRevoked(akid, serial []byte, issuerDN string) (bool, error) {
	for _, s := range l {
		if r, ok := s.(Revocations); ok {
			return r.IsRevoked(akid, serial, issuerDN)
		}
	}
	return false, nil
}

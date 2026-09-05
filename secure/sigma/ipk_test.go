// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package sigma_test

import (
	"encoding/hex"
	"testing"

	"github.com/SukramJ/go-fabric/secure/sigma"
)

// mustHex decodes a fixture constant, failing the test rather than the
// assertion when a literal is mistyped.
func mustHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatalf("decode %q: %v", s, err)
	}
	return b
}

// The group-key derivation vector matter.js asserts on in
// packages/nodejs/test/crypto/NodeJsCryptoTest.ts:144-155 ("validate group
// key derivation"): createHkdfKey(ikm, salt, "GroupKey v1.0") with the
// default 16-byte length.
//
// It is the gold standard's own fixture, so it pins all four inputs at once
// — hash, ikm, salt and info — rather than restating what this package's
// code already says.
const (
	matterJSGroupKeyIKM  = "235bf7e62823d358dca4ba50b1535f4b"
	matterJSGroupKeySalt = "87e1b004e235a130"
	matterJSGroupKeyOut  = "a6f5306baf6d050af23ba4bd6b9dd960"
)

// TestDeriveOperationalIPKMatchesMatterJSVector runs the exported derivation
// against matter.js's own HKDF fixture.
func TestDeriveOperationalIPKMatchesMatterJSVector(t *testing.T) {
	t.Parallel()

	var salt [8]byte
	copy(salt[:], mustHex(t, matterJSGroupKeySalt))

	got, err := sigma.DeriveOperationalIPK(mustHex(t, matterJSGroupKeyIKM), salt)
	if err != nil {
		t.Fatalf("DeriveOperationalIPK: %v", err)
	}
	if want := mustHex(t, matterJSGroupKeyOut); hex.EncodeToString(got[:]) != hex.EncodeToString(want) {
		t.Errorf("operational IPK = %x, matter.js NodeJsCryptoTest.ts says %x", got, want)
	}
}

// TestDeriveOperationalIPKSaltIsLoadBearing is the negative control for the
// vector above: had the derivation ignored the compressed fabric id — the
// one input a hand-written copy is most likely to drop, because the raw IPK
// alone is already 16 bytes and "looks like" the answer — the assertion
// above would still pass on some other constant. Changing one salt byte must
// change the output.
func TestDeriveOperationalIPKSaltIsLoadBearing(t *testing.T) {
	t.Parallel()

	ikm := mustHex(t, matterJSGroupKeyIKM)
	var salt [8]byte
	copy(salt[:], mustHex(t, matterJSGroupKeySalt))

	base, err := sigma.DeriveOperationalIPK(ikm, salt)
	if err != nil {
		t.Fatalf("DeriveOperationalIPK(base): %v", err)
	}
	salt[7] ^= 0x01
	altered, err := sigma.DeriveOperationalIPK(ikm, salt)
	if err != nil {
		t.Fatalf("DeriveOperationalIPK(altered salt): %v", err)
	}
	if base == altered {
		t.Error("one flipped salt bit left the operational IPK unchanged — the compressed fabric id is not reaching the HKDF")
	}
}

// TestDeriveOperationalIPKRejectsWrongLength keeps a truncated or padded
// AddNOC.IPKValue from being silently zero-extended into a valid-looking
// key. A short key that derives without complaint fails much later, as an
// unexplained Sigma2 rejection.
func TestDeriveOperationalIPKRejectsWrongLength(t *testing.T) {
	t.Parallel()

	for _, n := range []int{0, 15, 17, 32} {
		if _, err := sigma.DeriveOperationalIPK(make([]byte, n), [8]byte{}); err == nil {
			t.Errorf("DeriveOperationalIPK with a %d-byte raw IPK returned no error", n)
		}
	}
}

// TestDeriveOperationalIPKFeedsComputeDestinationID pins the two exported
// halves together: the derivation's output is what [sigma.ComputeDestinationID]
// keys on, so a host that passes the RAW ipk there must land on a different
// destination id than one that derives first. Without this, a host could
// wire the raw value into the resolver, match nothing, and see only "no
// fabric matched" at pairing time.
func TestDeriveOperationalIPKFeedsComputeDestinationID(t *testing.T) {
	t.Parallel()

	raw := mustHex(t, matterJSGroupKeyIKM)
	var (
		salt    [8]byte
		random  [sigma.RandomSize]byte
		rawIPK  [16]byte
		rootKey = make([]byte, 65)
	)
	copy(salt[:], mustHex(t, matterJSGroupKeySalt))
	copy(rawIPK[:], raw)
	rootKey[0] = 0x04
	for i := 1; i < len(rootKey); i++ {
		rootKey[i] = byte(i)
	}

	opIPK, err := sigma.DeriveOperationalIPK(raw, salt)
	if err != nil {
		t.Fatalf("DeriveOperationalIPK: %v", err)
	}
	derived := sigma.ComputeDestinationID(opIPK, random, rootKey, 0x0102030405060708, 0x1122334455667788)
	rawBased := sigma.ComputeDestinationID(rawIPK, random, rootKey, 0x0102030405060708, 0x1122334455667788)
	if derived == rawBased {
		t.Error("destination id is the same for the raw and the derived IPK — the derivation is a no-op")
	}
}

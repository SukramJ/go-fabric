// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package mattercert

import (
	"bytes"
	"encoding/asn1"
	"encoding/hex"
	"math/big"
	"testing"
)

// TestEcdsaSignatureDERParityMatterJS mirrors
// packages/general/test/crypto/EcdsaSignatureTest.ts:27-38 at 862ad75f
// ("round-trips through DER encoding", "rejects malformed DER input"): a
// certificate's DER ECDSA-Sig-Value reads back as the 64-byte r ‖ s it
// encodes (derECDSASignature), and bytes that are no DER signature are
// refused.
func TestEcdsaSignatureDERParityMatterJS(t *testing.T) {
	t.Parallel()
	ieee := make([]byte, 64)
	for i := range ieee {
		ieee[i] = byte(i + 1)
	}
	der, err := asn1.Marshal(ecdsaSignature{R: new(big.Int).SetBytes(ieee[:32]), S: new(big.Int).SetBytes(ieee[32:])})
	if err != nil {
		t.Fatal(err)
	}
	wrap := func(b []byte) asn1.RawValue {
		full, err := asn1.Marshal(asn1.BitString{Bytes: b, BitLength: 8 * len(b)})
		if err != nil {
			t.Fatal(err)
		}
		return asn1.RawValue{FullBytes: full}
	}
	got, err := derECDSASignature(wrap(der))
	if err != nil || !bytes.Equal(got, ieee) {
		t.Errorf("DER round trip %x, %v; want %x", got, err, ieee)
	}
	malformed, _ := hex.DecodeString("000102")
	if _, err := derECDSASignature(wrap(malformed)); err == nil {
		t.Error("malformed DER accepted")
	}
}

// TestX509ExtensionsParityMatterJS mirrors
// packages/general/test/crypto/X509Test.ts:11-31 at 862ad75f: extended
// key usage encodes the known values and refuses an unsupported one, and
// basic constraints and key usage encode (the X.509 extensions a Matter
// certificate's DER form carries, tbs_der.go).
func TestX509ExtensionsParityMatterJS(t *testing.T) {
	t.Parallel()
	if oids, err := mapEKU(nil); err != nil || len(oids) != 0 {
		t.Errorf("no extended key usage: %v, %v", oids, err)
	}
	if _, err := buildExtendedKeyUsage(&CertificateExtensions{ExtendedKeyUsage: []uint8{1, 2}}); err != nil {
		t.Errorf("known extended key usages: %v", err)
	}
	if _, err := buildExtendedKeyUsage(&CertificateExtensions{ExtendedKeyUsage: []uint8{99}}); err == nil {
		t.Error("extended key usage 99 accepted")
	}
	for _, ext := range []CertificateExtensions{
		{BasicConstraintsIsCA: true, BasicConstraintsHasPathLen: true, BasicConstraintsPathLen: 1},
		{BasicConstraintsIsCA: false},
	} {
		if _, err := buildBasicConstraints(&ext); err != nil {
			t.Errorf("basic constraints %+v: %v", ext, err)
		}
	}
	if _, err := buildKeyUsage(&CertificateExtensions{KeyUsage: 0x01}); err != nil {
		t.Errorf("key usage: %v", err)
	}
}

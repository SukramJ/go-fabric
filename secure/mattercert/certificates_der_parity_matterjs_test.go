// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package mattercert

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/sha256"
	"encoding/asn1"
	"encoding/base64"
	"encoding/hex"
	"strings"
	"testing"
)

// The X.509 half of matter.js's CertificatesTest.ts at 862ad75f:
// Rcac/Icac/Noc.fromAsn1 ([DecodeDER]), asSignedDer ([EncodeDER]) and
// asUnsignedDer ([TBSToDER]) on the CERTIFICATE_SETS and on the external
// certificates of TestCertificates.ts:215-249.

// certificatesExternal is EXTERNAL_TEST_CERTIFICATES (TestCertificates.ts:215-249
// at 862ad75f): an RCAC, an ICAC and a NOC of an external PKI in DER.
var certificatesExternal = map[string]string{
	"RCAC": "MIIBlzCCATygAwIBAgIBATAKBggqhkjOPQQDAjAiMSAwHgYKKwYBBAGConwBBAwQMDAwMDAwMDAwMDAwMDAwMTAeFw0yMTAxMDEwMDAwMDBaFw0zMDEyMzAwMDAwMDBaMCIxIDAeBgorBgEEAYKifAEEDBAwMDAwMDAwMDAwMDAwMDAxMFkwEwYHKoZIzj0CAQYIKoZIzj0DAQcDQgAE1Z8+sLnCFqRvBAr/39VYF8qKPckhqmJNpRaLqQdm1zvwjkgM0PXMlGU6ktd3GdmwBHR9X32jne22fsVPfLxq3qNjMGEwDwYDVR0TAQH/BAUwAwEB/zAOBgNVHQ8BAf8EBAMCAQYwHQYDVR0OBBYEFBkNojfgeCqcWP7LvV3UifbQAMrRMB8GA1UdIwQYMBaAFBkNojfgeCqcWP7LvV3UifbQAMrRMAoGCCqGSM49BAMCA0kAMEYCIQDDOq80q3nQa9/iT4hK7tBp4QUtlLvHGCDnSwDW7s0aWQIhANwRcs4MBcDq92J61vDDxosEg8m4lZkknpk+pirVnH79",
	"ICAC": "MIIBlTCCATygAwIBAgIBATAKBggqhkjOPQQDAjAiMSAwHgYKKwYBBAGConwBBAwQMDAwMDAwMDAwMDAwMDAwMTAeFw0yMTAxMDEwMDAwMDBaFw0zMDEyMzAwMDAwMDBaMCIxIDAeBgorBgEEAYKifAEDDBAwMDAwMDAwMDAwMDAwMDAyMFkwEwYHKoZIzj0CAQYIKoZIzj0DAQcDQgAEAQkkmAiPq0p7LlkgYMUSDzfWgjIcphuyDuqpETefdMAmXJJbzRc5iplD5fSQegPAKJdRKyn9oVeu71vYorDP16NjMGEwDwYDVR0TAQH/BAUwAwEB/zAOBgNVHQ8BAf8EBAMCAQYwHQYDVR0OBBYEFLcNxoI2fCA+E/TUT5N0AjIU+yLZMB8GA1UdIwQYMBaAFBkNojfgeCqcWP7LvV3UifbQAMrRMAoGCCqGSM49BAMCA0cAMEQCIH5Wh/Ne8O4aaFs8wR+QT4L508z0E8vm0DwqOS9YQzfgAiBN/iku1zBRGmBmUtOzWm/whEgIzCd8tXOkTaeZk463dg==",
	"NOC":  "MIIB2DCCAX+gAwIBAgIBATAKBggqhkjOPQQDAjAiMSAwHgYKKwYBBAGConwBAwwQMDAwMDAwMDAwMDAwMDAwMjAeFw0yMTAxMDEwMDAwMDBaFw0zMDEyMzAwMDAwMDBaMEQxIDAeBgorBgEEAYKifAEFDBAwMDAwMDAwMDAwMDAwMDAxMSAwHgYKKwYBBAGConwBAQwQMDAwMDAwMDAxMjM0NDMyMTBZMBMGByqGSM49AgEGCCqGSM49AwEHA0IABA57wmb5bT+IcT0mAhIV8KTAaiFJxdiAmQbaGQgKbYMECC4PxM2oJ0DrlI7VIi662UtMcu8lU/pUyX/YSzt25LWjgYMwgYAwDAYDVR0TAQH/BAIwADAOBgNVHQ8BAf8EBAMCB4AwIAYDVR0lAQH/BBYwFAYIKwYBBQUHAwIGCCsGAQUFBwMBMB0GA1UdDgQWBBTUxT25i0KFMXNdC2izZ9XyUn92QTAfBgNVHSMEGDAWgBS3DcaCNnwgPhP01E+TdAIyFPsi2TAKBggqhkjOPQQDAgNHADBEAiB8FazXDfKZesqUvL0RAr5qIJfB6F/RQgpwblq3785IVgIgeEnxza8qMpNtGhzgfhWcpsDi/DStQapeQ1ISQRDiOT4=",
}

func certificatesExternalDER(t *testing.T, kind string) []byte {
	t.Helper()
	b, err := base64.StdEncoding.DecodeString(certificatesExternal[kind])
	if err != nil {
		t.Fatalf("%s: %v", kind, err)
	}
	return b
}

func certificatesDecodeDER(t *testing.T, der []byte) *Certificate {
	t.Helper()
	c, err := DecodeDER(der)
	if err != nil {
		t.Fatalf("DecodeDER: %v", err)
	}
	return c
}

// TestCertificatesParseFromASN1ParityMatterJS ports "parse root / intermediate /
// operational certificate from ASN.1" (CertificatesTest.ts:124-168 at
// 862ad75f): fromAsn1(fromTlv(TLV).asSignedDer()) is the same certificate
// (here: encodes to the fixture TLV again), asUnsignedDer is the set's
// ASN1 fixture, and the DER-parsed certificate verifies in its chain.
func TestCertificatesParseFromASN1ParityMatterJS(t *testing.T) {
	t.Parallel()
	for _, set := range certificatesSets {
		t.Run(set.name, func(t *testing.T) {
			t.Parallel()
			roundTrip := func(name, tlvHex, asn1Hex string) *Certificate {
				t.Helper()
				fromTLV := certificatesDecode(t, tlvHex)
				der, err := EncodeDER(fromTLV)
				if err != nil {
					t.Fatalf("%s: EncodeDER: %v", name, err)
				}
				fromDER := certificatesDecodeDER(t, der)
				if !bytes.Equal(fromDER.Raw, certificatesHex(t, tlvHex)) {
					t.Fatalf("%s: the DER-parsed certificate encodes to %x\nwant %x", name, fromDER.Raw, certificatesHex(t, tlvHex))
				}
				if asn1Hex != "" {
					tbs, err := TBSToDER(fromTLV)
					if err != nil || !bytes.Equal(tbs, certificatesHex(t, asn1Hex)) {
						t.Fatalf("%s: asUnsignedDer = %x (%v)\nwant %x", name, tbs, err, certificatesHex(t, asn1Hex))
					}
				}
				return fromDER
			}
			root := roundTrip("root", set.rootTLV, set.rootASN1)
			if err := ValidateRCAC(root); err != nil {
				t.Fatalf("root from ASN.1 does not verify: %v", err)
			}
			var icac []byte
			if set.icacTLV != "" {
				icac = roundTrip("intermediate", set.icacTLV, set.icacASN1).Raw
			}
			noc := roundTrip("operational", set.nocTLV, set.nocASN1)
			v, err := NewVerifierFromCertificate(certificatesDecode(t, set.rootTLV), certificatesNow)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := v.VerifyAndExtractPubKey(noc.Raw, icac); err != nil {
				t.Fatalf("NOC from ASN.1 does not verify: %v", err)
			}
		})
	}
}

// TestCertificatesExternalParityMatterJS ports "parse and regenerate external
// RCAC / ICAC / NOC certificate" and "external certificates form a valid
// chain" (CertificatesTest.ts:178-236 at 862ad75f) on
// EXTERNAL_TEST_CERTIFICATES: the DER of an external PKI parses, carries
// its ids and CA flags, regenerates byte for byte (asSignedDer), and the
// three form a chain whose key identifiers link and whose signatures
// verify.
func TestCertificatesExternalParityMatterJS(t *testing.T) {
	t.Parallel()
	rcac := certificatesDecodeDER(t, certificatesExternalDER(t, "RCAC"))
	icac := certificatesDecodeDER(t, certificatesExternalDER(t, "ICAC"))
	noc := certificatesDecodeDER(t, certificatesExternalDER(t, "NOC"))
	if !rcac.Subject.HasRCACID || !rcac.Extensions.BasicConstraintsIsCA || len(rcac.Signature) != 64 {
		t.Fatalf("RCAC: rcacId %v, isCa %v", rcac.Subject.HasRCACID, rcac.Extensions.BasicConstraintsIsCA)
	}
	if !icac.Subject.HasICACID || !icac.Extensions.BasicConstraintsIsCA {
		t.Fatalf("ICAC: icacId %v, isCa %v", icac.Subject.HasICACID, icac.Extensions.BasicConstraintsIsCA)
	}
	if !noc.Subject.HasNodeID || !noc.Subject.HasFabricID || noc.Extensions.BasicConstraintsIsCA {
		t.Fatalf("NOC: nodeId %v, fabricId %v, isCa %v", noc.Subject.HasNodeID, noc.Subject.HasFabricID, noc.Extensions.BasicConstraintsIsCA)
	}
	for kind, c := range map[string]*Certificate{"RCAC": rcac, "ICAC": icac, "NOC": noc} {
		der, err := EncodeDER(c)
		if err != nil {
			t.Fatalf("%s: EncodeDER: %v", kind, err)
		}
		if want := certificatesExternalDER(t, kind); !bytes.Equal(der, want) {
			t.Errorf("%s regenerated: %s\nwant %s", kind, hex.EncodeToString(der), hex.EncodeToString(want))
		}
	}
	if icac.Issuer.MatterRCACID != rcac.Subject.MatterRCACID || noc.Issuer.MatterICACID != icac.Subject.MatterICACID {
		t.Fatal("the issuer ids do not name the parents")
	}
	if !bytes.Equal(rcac.Extensions.AuthorityKeyID, rcac.Extensions.SubjectKeyID) ||
		!bytes.Equal(icac.Extensions.AuthorityKeyID, rcac.Extensions.SubjectKeyID) ||
		!bytes.Equal(noc.Extensions.AuthorityKeyID, icac.Extensions.SubjectKeyID) {
		t.Fatal("the authority key identifiers do not name the parents' subject key identifiers")
	}
	if err := ValidateRCAC(rcac); err != nil {
		t.Fatalf("RCAC: %v", err)
	}
	v, err := NewVerifierFromCertificate(rcac, certificatesNow)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := v.VerifyAndExtractPubKey(noc.Raw, icac.Raw); err != nil {
		t.Fatalf("chain: %v", err)
	}
}

// certificatesWithExtension is withExtension (CertificatesTest.ts:256-263):
// the DER certificate with one more extension appended to its [3]
// extensions SEQUENCE and the lengths re-encoded — the signature no
// longer verifies, which fromAsn1 does not check.
func certificatesWithExtension(t *testing.T, der, extension []byte) []byte {
	t.Helper()
	var cert asn1.RawValue
	if _, err := asn1.Unmarshal(der, &cert); err != nil {
		t.Fatal(err)
	}
	parts, err := derChildren(cert.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	tbs, err := derChildren(parts[0].Bytes)
	if err != nil {
		t.Fatal(err)
	}
	seq := func(class, tag int, children ...[]byte) []byte {
		out, err := asn1.Marshal(asn1.RawValue{Class: class, Tag: tag, IsCompound: true, Bytes: bytes.Join(children, nil)})
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	tbsOut := make([][]byte, 0, len(tbs))
	for _, f := range tbs {
		if f.Class == asn1.ClassContextSpecific && f.Tag == 3 {
			inner, err := derChildren(f.Bytes)
			if err != nil || len(inner) != 1 {
				t.Fatal("extensions")
			}
			f.FullBytes = seq(asn1.ClassContextSpecific, 3, seq(asn1.ClassUniversal, asn1.TagSequence, inner[0].Bytes, extension))
		}
		tbsOut = append(tbsOut, f.FullBytes)
	}
	return seq(asn1.ClassUniversal, asn1.TagSequence, seq(asn1.ClassUniversal, asn1.TagSequence, tbsOut...), parts[1].FullBytes, parts[2].FullBytes)
}

// TestCertificatesExtensionEdgesParityMatterJS ports "rejects a certificate
// whose recognized extension has no value octet string" and "parses a
// certificate whose unrecognized extension holds bytes that are not DER"
// (CertificatesTest.ts:265-293 at 862ad75f) and "rejects a DER operational
// certificate larger than 600 bytes before parsing" (:691-696).
func TestCertificatesExtensionEdgesParityMatterJS(t *testing.T) {
	t.Parallel()
	rcac := certificatesExternalDER(t, "RCAC")
	// basicConstraints (2.5.29.19) marked critical, no value.
	valueless := certificatesHex(t, "3008"+"0603551d13"+"0101ff")
	if _, err := DecodeDER(certificatesWithExtension(t, rcac, valueless)); err == nil || !strings.Contains(err.Error(), "missing its value octet string") {
		t.Errorf("valueless recognised extension: err = %v", err)
	}
	// OID 2.5.29.99 is not one we read; 0xff is not a DER tag.
	opaque := certificatesHex(t, "300a"+"0603551d63"+"0403ffffff")
	c, err := DecodeDER(certificatesWithExtension(t, rcac, opaque))
	if err != nil || !c.Subject.HasRCACID || !c.Extensions.BasicConstraintsIsCA {
		t.Errorf("opaque unrecognised extension: err = %v", err)
	}
	for _, kind := range []string{"Noc", "Rcac", "Icac"} {
		if _, err := DecodeDER(make([]byte, 601)); err == nil || !strings.Contains(err.Error(), "600") {
			t.Errorf("%s.fromAsn1 of 601 bytes: err = %v, want the 600-byte refusal", kind, err)
		}
	}
}

// TestCertificatesTLVOrderParityMatterJS ports "validates TLV order
// preserve" (CertificatesTest.ts:474-521 at 862ad75f): a NOC whose subject
// lists its CATs before the fabric and node ids round-trips through the
// TLV with that order kept, and its unsigned DER is the same bytes
// whichever way it was built.
func TestCertificatesTLVOrderParityMatterJS(t *testing.T) {
	t.Parallel()
	nocJSON := &Certificate{
		SerialNumber:       []byte{0x01},
		SignatureAlgorithm: SigAlgoECDSAWithSHA256,
		Issuer:             DistinguishedName{MatterRCACID: 0, HasRCACID: true, Order: []uint8{dnTagMatterRCACID}},
		NotBefore:          734738627,
		NotAfter:           1081634627,
		Subject: DistinguishedName{
			CASEAuthTags:   []uint32{305419896, 1450709556},
			MatterFabricID: 1, HasFabricID: true,
			MatterNodeID: 9544138254462263483, HasNodeID: true,
			Order: []uint8{dnTagMatterCASEAuth, dnTagMatterCASEAuth, dnTagMatterFabricID, dnTagMatterNodeID},
		},
		PublicKeyAlgorithm: PubKeyAlgoEC,
		EllipticCurveID:    CurvePrime256v1,
		PublicKey:          certificatesHex(t, "047a13da11a960c91abdbe002c6f969f03b88f7f5c58f36f6755c6dce7e1ddd460ab4b2df33f8ed1da1ff5ebf21c3a293c3251241c445b208a22bea0abec369740"),
		Extensions: CertificateExtensions{
			HasBasicConstraints: true, BasicConstraintsIsCA: false,
			HasKeyUsage: true, KeyUsage: keyUsageDigitalSignature,
			HasExtendedKeyUsage: true, ExtendedKeyUsage: []uint8{2, 1},
			HasSubjectKeyID: true, SubjectKeyID: certificatesHex(t, "50a7b210085872f47492e9442ccb2bfec82097da"),
			HasAuthorityKeyID: true, AuthorityKeyID: certificatesHex(t, "9f833bc968b9c7ce347b1d10f046ebda0aeb0b3e"),
			Order: []uint8{extTagBasicConstraints, extTagKeyUsage, extTagExtendedKeyUsage, extTagSubjectKeyID, extTagAuthorityKeyID},
		},
		Signature: certificatesHex(t, "3fcd56cf81d769100934b32f6a8b07afddfbf6c32f01e97385f251b8c71bc631a876c56fac76dd3c81449b71a0a450d28d9eaf314ccd3a166b61cddd965829f1"),
	}
	nocASN1, err := TBSToDER(nocJSON)
	if err != nil {
		t.Fatal(err)
	}
	nocTLV, err := Encode(nocJSON)
	if err != nil {
		t.Fatal(err)
	}
	unTLV, err := Decode(nocTLV)
	if err != nil {
		t.Fatal(err)
	}
	if got := unTLV.Subject.Order; !bytes.Equal(got, nocJSON.Subject.Order) {
		t.Fatalf("subject order after the TLV = %v, want %v", got, nocJSON.Subject.Order)
	}
	unASN1, err := TBSToDER(unTLV)
	if err != nil || !bytes.Equal(unASN1, nocASN1) {
		t.Fatalf("unsigned DER after the TLV = %x (%v)\nwant %x", unASN1, err, nocASN1)
	}
	if again, _ := Encode(unTLV); !bytes.Equal(again, nocTLV) {
		t.Fatalf("TLV after the round trip = %x\nwant %x", again, nocTLV)
	}
}

// TestCertificatesCSRParityMatterJS ports "generates a valid CSR"
// (CertificatesTest.ts:628-645 at 862ad75f): the request is three DER
// elements, the algorithm ecdsa-with-SHA256, the CertificationRequestInfo
// the fixture TEST_CSR_REQUEST_ASN1 (TestCertificates.ts:207-209) byte for
// byte, and the signature verifies with the key's public half.
func TestCertificatesCSRParityMatterJS(t *testing.T) {
	t.Parallel()
	key, err := ecdsa.ParseRawPrivateKey(elliptic.P256(), certificatesHex(t, "727F1005CBA47ED7822A9D930943621617CFD3B79D9AF528B801ECF9F1992204"))
	if err != nil {
		t.Fatal(err)
	}
	csr, err := CreateCSR(key, nil)
	if err != nil {
		t.Fatal(err)
	}
	var outer asn1.RawValue
	if _, err := asn1.Unmarshal(csr, &outer); err != nil {
		t.Fatal(err)
	}
	parts, err := derChildren(outer.Bytes)
	if err != nil || len(parts) != 3 {
		t.Fatalf("CSR has %d root elements, want 3", len(parts))
	}
	want := certificatesHex(t, "3070020100300e310c300a060355040a0c034353523059301306072a8648ce3d020106082a8648ce3d0301070342000462e2b6e1baff8d74a6fd8216c4cb67a3363a31e691492792e61aee610261481396725ef95e142686ba98f339b0ff65bc338bec7b9e8be0bdf3b2774982476220a000")
	if !bytes.Equal(parts[0].FullBytes, want) {
		t.Fatalf("request = %x\nwant %x", parts[0].FullBytes, want)
	}
	if alg, err := derFirstOID(parts[1]); err != nil || !alg.Equal(oidEcdsaWithSHA256) {
		t.Fatalf("signature algorithm = %v (%v), want ecdsa-with-SHA256", alg, err)
	}
	var sig asn1.BitString
	if _, err := asn1.Unmarshal(parts[2].FullBytes, &sig); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(parts[0].FullBytes)
	if !ecdsa.VerifyASN1(&key.PublicKey, digest[:], sig.Bytes) {
		t.Fatal("the CSR signature does not verify")
	}
}

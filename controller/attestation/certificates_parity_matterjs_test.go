// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package attestation

// A port of matter.js packages/protocol/test/certificate/CertificatesTest.ts at
// 862ad75f: the attestation-certificate cases — Paa/Pai/Dac.fromAsn1 (here
// parseAttCert), the Mvid:/Mpid: commonName fallback (parseMatterFallbackVidPid,
// here fallbackID). The operational-certificate cases are in
// secure/mattercert/certificates_parity_matterjs_test.go.

import (
	"encoding/base64"
	"encoding/hex"
	"strings"
	"testing"
)

func certificatesDER(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatalf("hex: %v", err)
	}
	return b
}

func certificatesParse(t *testing.T, der []byte, authorityKeyID bool) *attCert {
	t.Helper()
	a, err := parseAttCert(der, authorityKeyID)
	if err != nil {
		t.Fatalf("parseAttCert: %v", err)
	}
	return a
}

func certificatesID(t *testing.T, what string, got *uint16, want uint16) {
	t.Helper()
	if got == nil || *got != want {
		t.Fatalf("%s = %v, want %#04x", what, got, want)
	}
}

// TestCertificatesFallbackVendorIDProductID is the describe "parses
// VendorID/ProductID encoded via the fallback method (spec 6.2.2.2)"
// (CertificatesTest.ts:295). Dac.fromAsn1 and Pai.fromAsn1 are
// parseAttCert with the authorityKeyIdentifier required.
func TestCertificatesFallbackVendorIDProductID(t *testing.T) {
	t.Run("parses fallback VendorID and ProductID from a DAC commonName", func(t *testing.T) {
		dac := certificatesParse(t, certificatesDER(t, certificatesNukiDAC), true)
		certificatesID(t, "vendorId", dac.vendorID, 0x135d)
		certificatesID(t, "productId", dac.productID, 0x00a1)
	})
	t.Run("parses fallback VendorID from a PAI commonName", func(t *testing.T) {
		pai := certificatesParse(t, certificatesDER(t, certificatesNukiPAI), true)
		certificatesID(t, "vendorId", pai.vendorID, 0x135d)
	})
}

// TestCertificatesParseMatterFallbackVidPid is the describe
// "parseMatterFallbackVidPid" (CertificatesTest.ts:357); the vectors are
// spec 6.2.2.2.1's as matter.js lists them.
func TestCertificatesParseMatterFallbackVidPid(t *testing.T) {
	t.Run("returns undefined when the prefix is absent", func(t *testing.T) {
		if id, err := fallbackID("Matter Test Cert", "Mvid:"); id != nil || err != nil {
			t.Fatalf("fallbackID = %v, %v; want undefined", id, err)
		}
	})
	t.Run("parses the spec's valid encodings (VID 0xFFF1, PID 0x00B1)", func(t *testing.T) {
		for _, cn := range []string{
			"ACME Matter Devel DAC 5CDA9899 Mvid:FFF1 Mpid:00B1",
			"ACME Matter Devel DAC 5CDA9899 Mpid:00B1 Mvid:FFF1", // order irrelevant
			"Mpid:00B1,ACME Matter Devel DAC 5CDA9899,Mvid:FFF1", // separators irrelevant
			"ACME Matter Devel DAC 5CDA9899 Mvid:FFF1Mpid:00B1",  // adjacent
			"Mvid:FFF1ACME Matter Devel DAC 5CDAMpid:00B19899",   // trailing hex ignored (consume exactly 4)
		} {
			vid, err := fallbackID(cn, "Mvid:")
			if err != nil {
				t.Fatalf("%q Mvid: %v", cn, err)
			}
			certificatesID(t, cn+" Mvid:", vid, 0xfff1)
			pid, err := fallbackID(cn, "Mpid:")
			if err != nil {
				t.Fatalf("%q Mpid: %v", cn, err)
			}
			certificatesID(t, cn+" Mpid:", pid, 0x00b1)
		}
	})
	t.Run("uses the leftmost correctly-encoded match", func(t *testing.T) {
		pid, err := fallbackID("Mpid:Mvid:FFF1 Mpid:12cd Matter Test Mpid:FE67", "Mpid:")
		if err != nil {
			t.Fatalf("fallbackID: %v", err)
		}
		certificatesID(t, "Mpid:", pid, 0xfe67)
	})
	t.Run("throws on the spec's malformed encodings", func(t *testing.T) {
		for _, c := range []struct{ cn, prefix string }{
			{"DAC Mvid:FF1 Mpid:00B1", "Mvid:"},  // 3 digits
			{"DAC Mvid:fff1 Mpid:00B1", "Mvid:"}, // lowercase
			{"DAC Mvid:FFF1 Mpid:B1", "Mpid:"},   // 2 digits
			{"DAC Mpid: Mvid:FFF1", "Mpid:"},     // prefix only
		} {
			if id, err := fallbackID(c.cn, c.prefix); err == nil {
				t.Errorf("fallbackID(%q, %q) = %v, want an error", c.cn, c.prefix, *id)
			}
		}
	})
}

// TestCertificatesPaaFromAsn1 is the describe "Paa.fromAsn1"
// (CertificatesTest.ts:648): parseAttCert without the
// authorityKeyIdentifier requirement (REQUIRED_PAA_EXTENSIONS).
func TestCertificatesPaaFromAsn1(t *testing.T) {
	withoutAKID, err := base64.StdEncoding.DecodeString(certificatesPAAWithoutAuthKeyID)
	if err != nil {
		t.Fatalf("base64: %v", err)
	}
	for _, c := range []struct {
		it, cn, skid string
		der          []byte
		vid          *uint16
	}{
		{"parses PAA certificate without VID from ASN.1", "Matter Test PAA", "785CE705B86B8F4E6FC793AA60CB43EA696882D5", certificatesDER(t, certificatesPAANoVID), nil},
		{"parses PAA certificate with VID from ASN.1", "Matter Test PAA", "6AFD22771F511FECBF1641976710DCDC31A1717E", certificatesDER(t, certificatesPAAFFF1), new(uint16(0xfff1))},
		{"parses PAA certificate without AuthorityKeyIdentifier with VID from ASN.1", "Kwikset Matter Test PAA", "62B177DEFD5A9CC35D9190A0E0F5F4E9ECE84BE1", withoutAKID, new(uint16(5153))},
	} {
		t.Run(c.it, func(t *testing.T) {
			paa := certificatesParse(t, c.der, false)
			if got := paa.x509.Subject.CommonName; got != c.cn {
				t.Errorf("commonName = %q, want %q", got, c.cn)
			}
			if got := strings.ToUpper(hex.EncodeToString(paa.x509.SubjectKeyId)); got != c.skid {
				t.Errorf("subjectKeyIdentifier = %s, want %s", got, c.skid)
			}
			if c.vid != nil {
				certificatesID(t, "vendorId", paa.vendorID, *c.vid)
			}
		})
	}
}

// The fixtures below are matter.js at 862ad75f, verbatim:
// ChipPAAuthorities.ts:20-39 (TestCert_PAA_NoVID_Cert, TestCert_PAA_FFF1_Cert,
// TestCert_WithoutAuthKeyId, base64) and CertificatesTest.ts:298-303
// (NUKI_DAC_ASN1, NUKI_PAI_ASN1).

const (
	certificatesPAANoVID            = "3082019130820137a00302010202070b8fbaa8dd86ee300a06082a8648ce3d040302301a3118301606035504030c0f4d61747465722054657374205041413020170d3231303632383134323334335a180f39393939313233313233353935395a301a3118301606035504030c0f4d61747465722054657374205041413059301306072a8648ce3d020106082a8648ce3d0301070342000410ef02a81a87b68121fba8d31978f807a317e50aa8a828446828914b933de8edd4a5c39c9ff71a4ce3647fd7f62653b7d2495fcba4c0f47f876880039e07204aa366306430120603551d130101ff040830060101ff020101300e0603551d0f0101ff040403020106301d0603551d0e04160414785ce705b86b8f4e6fc793aa60cb43ea696882d5301f0603551d23041830168014785ce705b86b8f4e6fc793aa60cb43ea696882d5300a06082a8648ce3d0403020348003045022100b9efdb3ea06a52ec0bf01e61daed2c2d156ddb6cf014101dab798fac05fa47e5022060061d3e35d60d9d4b0d448dad7612f7e85c582e3fc312dc18794dd373715e5d"
	certificatesPAAFFF1             = "308201bd30820164a00302010202084ea8e83182d41c1c300a06082a8648ce3d04030230303118301606035504030c0f4d617474657220546573742050414131143012060a2b0601040182a27c02010c04464646313020170d3231303632383134323334335a180f39393939313233313233353935395a30303118301606035504030c0f4d617474657220546573742050414131143012060a2b0601040182a27c02010c04464646313059301306072a8648ce3d020106082a8648ce3d03010703420004b6cb6372887f2928f5bac81aa9d93ae2431cada9d79e242f65177ef9ced932a28ecd03baaf6a8fca184a1a503542960d453f303f1f19421d751e8f8f1a9a9b75a366306430120603551d130101ff040830060101ff020101300e0603551d0f0101ff040403020106301d0603551d0e041604146afd22771f511fecbf1641976710dcdc31a1717e301f0603551d230418301680146afd22771f511fecbf1641976710dcdc31a1717e300a06082a8648ce3d0403020347003044022050aa8002f4d932a9a00538f65368ad0fffc8efbbc9beb7da569835cf9aa7510e022023bac8fe0f23e75445b65339081a47994929c72aaf0a1548d40d034d514b25de"
	certificatesPAAWithoutAuthKeyID = "MIIBsjCCAVegAwIBAgIRAJnC0tA+L5QzoIwineuTXvwwCgYIKoZIzj0EAwIwODEgMB4GA1UEAwwXS3dpa3NldCBNYXR0ZXIgVGVzdCBQQUExFDASBgorBgEEAYKifAIBDAQxNDIxMB4XDTIzMDQxOTE3MDIwMFoXDTI0MDQxODE4MDIwMFowODEgMB4GA1UEAwwXS3dpa3NldCBNYXR0ZXIgVGVzdCBQQUExFDASBgorBgEEAYKifAIBDAQxNDIxMFkwEwYHKoZIzj0CAQYIKoZIzj0DAQcDQgAED//Uo3C9Q4xlZEUeAhcKl+HtNDDjgg9IwI4iOx3YYvn93TymtcAtrPzCiIHzBkmB16pWG0fUDiHt2L3zskmfNaNCMEAwDwYDVR0TAQH/BAUwAwEB/zAdBgNVHQ4EFgQUYrF33v1anMNdkZCg4PX06ezoS+EwDgYDVR0PAQH/BAQDAgGGMAoGCCqGSM49BAMCA0kAMEYCIQDOits8OE9aPTbQ1xYHUuycpioMkaptvaGZ2ZDYmLtjSAIhAKes3U/GsZ72mhK7ff5tq6sJAX03eE5cmpCuDTNRLr/6"
	certificatesNukiDAC             = "308201c130820167a003020102020e03ff135d00a1001b000802d19da9300a06082a8648ce3d040302303431323030060355040313294b7564656c736b69204d61747465722050414920666f72204e756b69204d7669643a313335442030313020170d3236303532323039313332365a180f39393939313233313233353935395a302f312d302b060355040313244d7669643a31333544204d7069643a3030413120303031623030303830326431396461393059301306072a8648ce3d020106082a8648ce3d0301070342000494f0ff20d3df40660f59e3838a97b26188d6004db8f670b6ff7b1f1b952cfcb63516bbc379204ec542fdaf5809360b28c76d1b2b30862a6dafaf0454b301921ea360305e300e0603551d0f0101ff040403020780300c0603551d130101ff04023000301d0603551d0e041604140ac17a032b81fd47c00388872acae54850b231c3301f0603551d2304183016801448fd5991116b0a91759346351ea4728e51fb7c39300a06082a8648ce3d0403020348003045022012e4f811b5b2d14feeb86c28ba744895e5bd1925506a4f9bdb130b4366b3f45a022100f4b82b41df0b14aeea9eaa1ef95439ac75d687199c53e6369da8cf9047f36e51"
	certificatesNukiPAI             = "308201b83082015da003020102020e02ff135d000001897333e3210001300a06082a8648ce3d0403023021311f301d060355040313164b7564656c736b69204d617474657220504141203031301e170d3233303732303132303834375a170d3238303732303132303834375a303431323030060355040313294b7564656c736b69204d61747465722050414920666f72204e756b69204d7669643a313335442030313059301306072a8648ce3d020106082a8648ce3d0301070342000455687c57f5c97097b23945b5fbd3578b53038cfae793a098771e7f88337fe694e26b441f75939e574b21126fc4c947c29658fe1cb085c2c4b2b2a92cb728c073a3663064300e0603551d0f0101ff04040302010630120603551d130101ff040830060101ff020100301d0603551d0e0416041448fd5991116b0a91759346351ea4728e51fb7c39301f0603551d23041830168014a607c3607b7150e3622ab0ba889e6cbc3fc552f9300a06082a8648ce3d0403020349003046022100caef1def29bf5069f1ae3928f3cab0ba88171f65bb5ee98fdf549f0e1ec5cb5b022100897030f94de3efcb5ff0b1ceb6a755edc811a9559f3048d129fc26c61462d631"
)

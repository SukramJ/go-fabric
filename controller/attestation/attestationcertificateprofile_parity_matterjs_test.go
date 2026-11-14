// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package attestation

// A port of matter.js packages/protocol/test/certificate/
// AttestationCertificateProfileTest.ts at db021da0: the DAC and PAI
// certificate profile on connectedhomeip's commissioner DUT vectors and on
// edited certificates, the encoding the parser keeps, and the validator's
// DacFormatInvalid / PaiFormatInvalid. The "VendorID and ProductID in the
// distinguished name" group belongs to matter.js #4880 (pin lift wave P1)
// and the acceptProfileViolation cases to the callers matter.js gives the
// option; parseAttCert's flag is exercised by TestProfileAcceptViolation.

import (
	"encoding/hex"
	"errors"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// profileRefusal is a refusal the test expects: a profile violation or a
// parse failure, with the rule its message names.
type profileRefusal struct {
	profile bool
	rule    string
}

func profileRule(rule string) profileRefusal { return profileRefusal{profile: true, rule: rule} }
func parseRule(rule string) profileRefusal   { return profileRefusal{rule: rule} }

// dacRefusals is DAC_REFUSALS (:25-44 at db021da0).
var dacRefusals = map[string]profileRefusal{
	"struct_dac_cert_version_v2":                   profileRule(`DAC version field must be 2`),
	"struct_dac_ext_basic_ca_missing":              profileRule(`DAC basic constraints must not have a pathLen`),
	"struct_dac_ext_basic_ca_wrong":                profileRule(`DAC basic constraints must have cA set to FALSE`),
	"struct_dac_ext_basic_critical_missing":        profileRule(`DAC basic constraints extension must be marked critical`),
	"struct_dac_ext_basic_critical_wrong":          profileRule(`DAC basic constraints extension must be marked critical`),
	"struct_dac_ext_basic_missing":                 profileRule(`DAC must carry the basic constraints extension`),
	"struct_dac_ext_basic_pathlen0":                profileRule(`DAC basic constraints must not have a pathLen, found 0`),
	"struct_dac_ext_basic_pathlen1":                profileRule(`DAC basic constraints must not have a pathLen, found 1`),
	"struct_dac_ext_basic_pathlen2":                profileRule(`DAC basic constraints must not have a pathLen, found 2`),
	"struct_dac_ext_basic_pathlen_presence_wrong":  profileRule(`DAC basic constraints must not have a pathLen`),
	"struct_dac_ext_key_usage_critical_missing":    profileRule(`DAC key usage extension must be marked critical`),
	"struct_dac_ext_key_usage_critical_wrong":      profileRule(`DAC key usage extension must be marked critical`),
	"struct_dac_ext_key_usage_crl_sign_wrong":      profileRule(`DAC key usage must have only digitalSignature set`),
	"struct_dac_ext_key_usage_dig_sig_wrong":       profileRule(`DAC key usage must have only digitalSignature set`),
	"struct_dac_ext_key_usage_key_cert_sign_wrong": profileRule(`DAC key usage must have only digitalSignature set`),
	"struct_dac_ext_key_usage_missing":             parseRule(`(?i)missing required extensions in certificate: keyUsage`),
	"struct_dac_ext_akid_missing":                  parseRule(`(?i)missing required extensions in certificate: authorityKeyIdentifier`),
	"struct_dac_ext_skid_missing":                  parseRule(`(?i)missing required extensions in certificate: subjectKeyIdentifier`),
}

// paiRefusals is PAI_REFUSALS (:46-63 at db021da0).
var paiRefusals = map[string]profileRefusal{
	"struct_pai_cert_version_v2":                   profileRule(`PAI version field must be 2`),
	"struct_pai_ext_basic_ca_missing":              profileRule(`PAI basic constraints must have cA set to TRUE`),
	"struct_pai_ext_basic_ca_wrong":                profileRule(`PAI basic constraints must have cA set to TRUE`),
	"struct_pai_ext_basic_critical_missing":        profileRule(`PAI basic constraints extension must be marked critical`),
	"struct_pai_ext_basic_critical_wrong":          profileRule(`PAI basic constraints extension must be marked critical`),
	"struct_pai_ext_basic_missing":                 profileRule(`PAI must carry the basic constraints extension`),
	"struct_pai_ext_basic_pathlen1":                profileRule(`PAI basic constraints must have pathLen 0, not 1`),
	"struct_pai_ext_basic_pathlen2":                profileRule(`PAI basic constraints must have pathLen 0, not 2`),
	"struct_pai_ext_basic_pathlen_presence_wrong":  profileRule(`PAI basic constraints must have pathLen 0, not undefined`),
	"struct_pai_ext_key_usage_critical_missing":    profileRule(`PAI key usage extension must be marked critical`),
	"struct_pai_ext_key_usage_critical_wrong":      profileRule(`PAI key usage extension must be marked critical`),
	"struct_pai_ext_key_usage_crl_sign_wrong":      profileRule(`PAI key usage must have keyCertSign and cRLSign set`),
	"struct_pai_ext_key_usage_key_cert_sign_wrong": profileRule(`PAI key usage must have keyCertSign and cRLSign set`),
	"struct_pai_ext_key_usage_missing":             parseRule(`(?i)missing required extensions in certificate: keyUsage`),
	"struct_pai_ext_akid_missing":                  parseRule(`(?i)missing required extensions in certificate: authorityKeyIdentifier`),
	"struct_pai_ext_skid_missing":                  parseRule(`(?i)missing required extensions in certificate: subjectKeyIdentifier`),
}

// expectRefusal is expectRefusal (:69-74 at db021da0): a parse refusal is
// never a profile violation.
func expectRefusal(t *testing.T, der []byte, kind certKind, want profileRefusal) {
	t.Helper()
	_, err := parseAttCert(der, kind, false)
	switch {
	case err == nil:
		t.Fatalf("%s parsed, want a refusal /%s/", kind, want.rule)
	case isProfileError(err) != want.profile:
		t.Fatalf("%s refused with %v (profile %v), want profile %v", kind, err, isProfileError(err), want.profile)
	case !regexp.MustCompile(want.rule).MatchString(err.Error()):
		t.Fatalf("%s refused with %q, want /%s/", kind, err, want.rule)
	}
}

const (
	basicConstraintsOID = "551d13"
	keyUsageOID         = "551d0f"
)

// validProfilePair is VALID_DAC and VALID_PAI (:183-184 at db021da0), the
// struct_dac_cert_version_v3 success vector.
func validProfilePair(t *testing.T) (dac, pai []byte) {
	t.Helper()
	v := chipVectorNamed(t, "struct_dac_cert_version_v3")
	return unhex(t, v.DACCert), unhex(t, v.PAICert)
}

// TestAttestationProfileRefusesDAC: describe "refuses a DAC that violates
// the DAC profile" (:207-234 at db021da0).
func TestAttestationProfileRefusesDAC(t *testing.T) {
	t.Parallel()
	for _, name := range chipVectors(t).Groups["refusedDAC"] {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			want, ok := dacRefusals[name]
			if !ok {
				t.Fatalf("no refusal for %s", name)
			}
			expectRefusal(t, unhex(t, chipVectorNamed(t, name).DACCert), kindDAC, want)
		})
	}
	valid, _ := validProfilePair(t)
	t.Run("with a key usage bit beyond decipherOnly", func(t *testing.T) {
		t.Parallel()
		// BIT STRING with 6 unused bits: digitalSignature and bit 9.
		dac := withExtensionValue(t, valid, keyUsageOID, "0303068040")
		expectRefusal(t, dac, kindDAC, profileRule(`DAC key usage must have only digitalSignature set`))
	})
	t.Run("without a version field", func(t *testing.T) {
		t.Parallel()
		v1 := editTBS(t, valid, func(tbs *[]derNode) {
			if (*tbs)[0].tag != 0xa0 {
				t.Fatal("no version field")
			}
			*tbs = (*tbs)[1:]
		})
		// crypto/x509 reads a v1 certificate's extensions too little to
		// matter: the encoding reader states the version.
		expectRefusal(t, v1, kindDAC, profileRule(`DAC version field must be 2 \(v3\), not 0`))
	})
	t.Run("with a basic constraints extension it carries twice", func(t *testing.T) {
		t.Parallel()
		dac := editExtensions(t, valid, func(exts *[]derNode) {
			*exts = append(*exts, (*exts)[extensionNamed(t, *exts, basicConstraintsOID)])
		})
		expectRefusal(t, dac, kindDAC, profileRule(`DAC carries more than one basicConstraints extension`))
	})
}

// TestAttestationProfileRefusesPAI: describe "refuses a PAI that violates
// the PAI profile" (:236-263 at db021da0).
func TestAttestationProfileRefusesPAI(t *testing.T) {
	t.Parallel()
	for _, name := range chipVectors(t).Groups["refusedPAI"] {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			want, ok := paiRefusals[name]
			if !ok {
				t.Fatalf("no refusal for %s", name)
			}
			expectRefusal(t, unhex(t, chipVectorNamed(t, name).PAICert), kindPAI, want)
		})
	}
	_, valid := validProfilePair(t)
	t.Run("with a key usage bit besides keyCertSign, cRLSign and digitalSignature", func(t *testing.T) {
		t.Parallel()
		// BIT STRING with 1 unused bit: keyEncipherment, keyCertSign and cRLSign.
		pai := withExtensionValue(t, valid, keyUsageOID, "03020126")
		expectRefusal(t, pai, kindPAI, profileRule(`PAI key usage must have keyCertSign and cRLSign set`))
	})
	t.Run("with a key usage extension it carries twice", func(t *testing.T) {
		t.Parallel()
		pai := editExtensions(t, valid, func(exts *[]derNode) {
			ku := (*exts)[extensionNamed(t, *exts, keyUsageOID)]
			// A second copy without the critical flag.
			*exts = append(*exts, derNode{tag: ku.tag, elements: []derNode{ku.elements[0], ku.elements[len(ku.elements)-1]}})
		})
		expectRefusal(t, pai, kindPAI, profileRule(`PAI carries more than one keyUsage extension`))
	})
}

// TestAttestationProfileAcceptsSuccessVectors: describe "accepts the DAC
// and PAI of each TC-DA-1.8 success vector" (:265-272 at db021da0).
func TestAttestationProfileAcceptsSuccessVectors(t *testing.T) {
	t.Parallel()
	for _, name := range chipVectors(t).Groups["acceptedProfile"] {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			v := chipVectorNamed(t, name)
			if _, err := parseAttCert(unhex(t, v.DACCert), kindDAC, false); err != nil {
				t.Fatalf("DAC: %v", err)
			}
			if _, err := parseAttCert(unhex(t, v.PAICert), kindPAI, false); err != nil {
				t.Fatalf("PAI: %v", err)
			}
		})
	}
}

// TestProfileAcceptViolation: describe "when accepting a profile
// violation" and "PAA" (:274-323 at db021da0).
func TestProfileAcceptViolation(t *testing.T) {
	t.Parallel()
	t.Run("parses a DAC and a PAI that violate their profile", func(t *testing.T) {
		t.Parallel()
		if _, err := parseAttCert(unhex(t, chipVectorNamed(t, "struct_dac_ext_basic_ca_wrong").DACCert), kindDAC, true); err != nil {
			t.Fatal(err)
		}
		if _, err := parseAttCert(unhex(t, chipVectorNamed(t, "struct_pai_ext_basic_ca_wrong").PAICert), kindPAI, true); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("reads an absent basic constraints extension as not a CA", func(t *testing.T) {
		t.Parallel()
		for _, c := range []struct {
			der  []byte
			kind certKind
		}{
			{unhex(t, chipVectorNamed(t, "struct_dac_ext_basic_missing").DACCert), kindDAC},
			{unhex(t, chipVectorNamed(t, "struct_pai_ext_basic_missing").PAICert), kindPAI},
		} {
			if _, err := parseAttCert(c.der, c.kind, true); err != nil {
				t.Fatalf("%s: %v", c.kind, err)
			}
			if e, err := readEncoding(c.der); err != nil || e.isCA || e.pathLen != nil {
				t.Fatalf("%s: encoding %+v (%v)", c.kind, e, err)
			}
		}
	})
	t.Run("still refuses a certificate without key usage", func(t *testing.T) {
		t.Parallel()
		_, err := parseAttCert(unhex(t, chipVectorNamed(t, "struct_dac_ext_key_usage_missing").DACCert), kindDAC, true)
		if err == nil || !regexp.MustCompile(`(?i)missing required extensions in certificate: keyUsage`).MatchString(err.Error()) {
			t.Fatalf("parse = %v", err)
		}
	})
	// A PAI without basic constraints stands in for a PAA without them; the
	// PAA profile is not checked.
	noBC := unhex(t, chipVectorNamed(t, "struct_pai_ext_basic_missing").PAICert)
	t.Run("PAA requires the basic constraints extension", func(t *testing.T) {
		t.Parallel()
		_, err := parseAttCert(noBC, kindPAA, false)
		if err == nil || isProfileError(err) || !regexp.MustCompile(`(?i)missing required extensions in certificate: basicConstraints`).MatchString(err.Error()) {
			t.Fatalf("parse = %v", err)
		}
	})
	t.Run("PAA reads an absent basic constraints extension as not a CA when accepting a profile violation", func(t *testing.T) {
		t.Parallel()
		if _, err := parseAttCert(noBC, kindPAA, true); err != nil {
			t.Fatal(err)
		}
		enc, err := readEncoding(noBC)
		if err != nil || enc.isCA || enc.pathLen != nil {
			t.Fatalf("encoding %+v (%v)", enc, err)
		}
	})
}

// TestProfileParser: describe "parser" (:325-425 at db021da0), the
// encoding the reader keeps.
func TestProfileParser(t *testing.T) {
	t.Parallel()
	validDAC, validPAI := validProfilePair(t)
	enc := func(t *testing.T, der []byte) certEncoding {
		t.Helper()
		e, err := readEncoding(der)
		if err != nil {
			t.Fatal(err)
		}
		return e
	}
	t.Run("keeps the version field", func(t *testing.T) {
		t.Parallel()
		if v := enc(t, validDAC).version; v.Int64() != 2 {
			t.Fatalf("version %v", v)
		}
		if v := enc(t, unhex(t, chipVectorNamed(t, "struct_dac_cert_version_v2").DACCert)).version; v.Int64() != 1 {
			t.Fatalf("version %v", v)
		}
	})
	t.Run("keeps which interpreted extensions are present and critical", func(t *testing.T) {
		t.Parallel()
		want := []extensionEncoding{
			{extBasicConstraints, true}, {extKeyUsage, true}, {extSubjectKeyID, false}, {extAuthorityKeyID, false},
		}
		if got := enc(t, validDAC).extensions; !slices.Equal(got, want) {
			t.Fatalf("extensions %v, want %v", got, want)
		}
		if e := enc(t, unhex(t, chipVectorNamed(t, "struct_dac_ext_key_usage_critical_wrong").DACCert)); e.critical(extKeyUsage) {
			t.Fatal("keyUsage read as critical")
		}
	})
	t.Run("reads an explicit critical FALSE as not critical", func(t *testing.T) {
		t.Parallel()
		// keyUsage (2.5.29.15) with critical TRUE, rewritten in place to an
		// explicit FALSE.
		h := hex.EncodeToString(validDAC)
		const criticalKeyUsage = "0603551d0f0101ff"
		if c := strings.Count(h, criticalKeyUsage); c != 1 {
			t.Fatalf("%d occurrences", c)
		}
		der := unhex(t, strings.Replace(h, criticalKeyUsage, "0603551d0f010100", 1))
		if e := enc(t, der); e.critical(extKeyUsage) {
			t.Fatal("explicit FALSE read as critical")
		}
	})
	t.Run("refuses an empty critical flag", func(t *testing.T) {
		t.Parallel()
		der := editExtensions(t, validDAC, func(exts *[]derNode) {
			(*exts)[extensionNamed(t, *exts, keyUsageOID)].elements[1].bytes = nil
		})
		if _, err := readEncoding(der); err == nil || !regexp.MustCompile(`BOOLEAN of 0 bytes`).MatchString(err.Error()) {
			t.Fatalf("readEncoding = %v", err)
		}
		if _, err := parseAttCert(der, kindDAC, false); err == nil || isProfileError(err) {
			t.Fatalf("parse = %v", err)
		}
	})
	t.Run("refuses an empty cA flag", func(t *testing.T) {
		t.Parallel()
		der := withExtensionValue(t, validPAI, basicConstraintsOID, "30050100020100")
		if _, err := readEncoding(der); err == nil || !regexp.MustCompile(`Basic constraints cA is a BOOLEAN of 0 bytes`).MatchString(err.Error()) {
			t.Fatalf("readEncoding = %v", err)
		}
	})
	t.Run("refuses a pathLen without content", func(t *testing.T) {
		t.Parallel()
		der := withExtensionValue(t, validPAI, basicConstraintsOID, "30050101ff0200")
		if _, err := readEncoding(der); err == nil || !regexp.MustCompile(`pathLenConstraint is an INTEGER without content`).MatchString(err.Error()) {
			t.Fatalf("readEncoding = %v", err)
		}
	})
	t.Run("reads pathLen when cA is omitted", func(t *testing.T) {
		t.Parallel()
		e := enc(t, unhex(t, chipVectorNamed(t, "struct_dac_ext_basic_ca_missing").DACCert))
		if e.isCA || e.pathLen == nil || e.pathLen.Sign() != 0 {
			t.Fatalf("basic constraints cA %v pathLen %v", e.isCA, e.pathLen)
		}
	})
	t.Run("reads an absent basic constraints extension as not a CA and records it as absent", func(t *testing.T) {
		t.Parallel()
		e := enc(t, unhex(t, chipVectorNamed(t, "struct_dac_ext_basic_missing").DACCert))
		if e.isCA || e.pathLen != nil || slices.Contains(e.names(), extBasicConstraints) {
			t.Fatalf("encoding %+v", e)
		}
	})
	t.Run("reads a pathLen that needs more than one byte", func(t *testing.T) {
		t.Parallel()
		// SEQUENCE { cA TRUE, pathLen 256 }, whose low byte alone would
		// read as 0.
		pai := withExtensionValue(t, validPAI, basicConstraintsOID, "30070101ff02020100")
		if e := enc(t, pai); !e.isCA || e.pathLen.Int64() != 256 {
			t.Fatalf("basic constraints cA %v pathLen %v", e.isCA, e.pathLen)
		}
		expectRefusal(t, pai, kindPAI, profileRule(`pathLen 0, not 256`))
	})
	t.Run("reads all key usage bits", func(t *testing.T) {
		t.Parallel()
		// digitalSignature, decipherOnly (bit 8) and bit 9.
		e := enc(t, withExtensionValue(t, validDAC, keyUsageOID, "03030680c0"))
		if e.keyUsage == nil || *e.keyUsage != 0x0301 {
			t.Fatalf("key usage %v", e.keyUsage)
		}
	})
	t.Run("parses a certificate with an extension it carries twice", func(t *testing.T) {
		t.Parallel()
		der := editExtensions(t, validDAC, func(exts *[]derNode) {
			*exts = append(*exts, (*exts)[extensionNamed(t, *exts, keyUsageOID)])
		})
		if e := enc(t, der); e.count(extKeyUsage) != 2 {
			t.Fatalf("%d keyUsage extensions", e.count(extKeyUsage))
		}
	})
}

// profileValidate is validate (:186-204 at db021da0): the validator with
// the attestation fields empty, so it fails at the first check.
func profileValidate(t *testing.T, dac, pai []byte) *Error {
	t.Helper()
	_, err := Validate(Context{Challenge: make([]byte, 16)}, Data{
		DAC: dac, PAI: pai, Signature: make([]byte, 64), Nonce: make([]byte, 32), VendorID: 0xFFF1, ProductID: 0x8000,
	})
	var ae *Error
	if !errors.As(err, &ae) {
		t.Fatalf("Validate = %v, want a DeviceAttestationError", err)
	}
	return ae
}

// TestProfileDeviceAttestationFindings: describe "device attestation
// findings" (:646-678 at db021da0).
func TestProfileDeviceAttestationFindings(t *testing.T) {
	t.Parallel()
	_, validPAI := validProfilePair(t)
	caWrongDAC := unhex(t, chipVectorNamed(t, "struct_dac_ext_basic_ca_wrong").DACCert)
	t.Run("reports a DAC that violates the DAC profile as DacFormatInvalid", func(t *testing.T) {
		t.Parallel()
		ae := profileValidate(t, caWrongDAC, validPAI)
		if ae.Check != DacFormatInvalid || !regexp.MustCompile(`DAC certificate that violates the DAC profile: DAC basic constraints must have cA set to FALSE`).MatchString(ae.Message) {
			t.Fatalf("%s %q", ae.Check, ae.Message)
		}
	})
	t.Run("reports a PAI that violates the PAI profile as PaiFormatInvalid, before checking the DAC", func(t *testing.T) {
		t.Parallel()
		ae := profileValidate(t, caWrongDAC, unhex(t, chipVectorNamed(t, "struct_pai_ext_basic_ca_wrong").PAICert))
		if ae.Check != PaiFormatInvalid {
			t.Fatalf("%s %q", ae.Check, ae.Message)
		}
	})
	t.Run("reports a DAC without key usage as unparseable", func(t *testing.T) {
		t.Parallel()
		ae := profileValidate(t, unhex(t, chipVectorNamed(t, "struct_dac_ext_key_usage_missing").DACCert), validPAI)
		if ae.Check != CertificateUnparseable {
			t.Fatalf("%s %q", ae.Check, ae.Message)
		}
	})
}

// The DN attribute OIDs and TBSCertificate positions the DN cases edit
// (:126-130 at db021da0).
const (
	vendorIDOID    = "2b0601040182a27c0201"
	productIDOID   = "2b0601040182a27c0202"
	commonNameOID  = "550403"
	issuerElement  = 3
	subjectElement = 5
)

// dnAttributeOf is attributeOf (:132-139 at db021da0): the first attribute
// of name whose type is oidHex.
func dnAttributeOf(t *testing.T, name *derNode, oidHex string) *derNode {
	t.Helper()
	for i := range name.elements {
		for j := range name.elements[i].elements {
			a := &name.elements[i].elements[j]
			if len(a.elements) == 2 && hex.EncodeToString(a.elements[0].bytes) == oidHex {
				return a
			}
		}
	}
	t.Fatalf("name has no attribute %s", oidHex)
	return nil
}

// dnRDNIndexOf is rdnIndexOf (:177-181 at db021da0).
func dnRDNIndexOf(t *testing.T, name *derNode, oidHex string) int {
	t.Helper()
	for i, rdn := range name.elements {
		for _, a := range rdn.elements {
			if len(a.elements) == 2 && hex.EncodeToString(a.elements[0].bytes) == oidHex {
				return i
			}
		}
	}
	t.Fatalf("name has no attribute %s", oidHex)
	return -1
}

// editName is editSubject (:141-144 at db021da0) for the TBSCertificate
// element at (the issuer or the subject).
func editName(t *testing.T, der []byte, at int, edit func(name *derNode)) []byte {
	t.Helper()
	return editTBS(t, der, func(tbs *[]derNode) { edit(&(*tbs)[at]) })
}

// withNameText is withIssuerText / withSubjectText (:146-158 at
// db021da0): the text of the name's attribute oidHex replaced.
func withNameText(t *testing.T, der []byte, at int, oidHex, text string) []byte {
	t.Helper()
	return editName(t, der, at, func(name *derNode) { dnAttributeOf(t, name, oidHex).elements[1].bytes = []byte(text) })
}

// withRepeatedSubjectAttribute is :160-168 at db021da0: a copy of the RDN
// that carries oidHex appended to the subject.
func withRepeatedSubjectAttribute(t *testing.T, der []byte, oidHex string) []byte {
	t.Helper()
	return editName(t, der, subjectElement, func(name *derNode) {
		name.elements = append(name.elements, name.elements[dnRDNIndexOf(t, name, oidHex)])
	})
}

// asCommonName is :170-175 at db021da0: the subject's attribute oidHex
// turned into a commonName with text.
func asCommonName(t *testing.T, name *derNode, oidHex, text string) {
	t.Helper()
	a := dnAttributeOf(t, name, oidHex)
	a.elements[0].bytes = unhex(t, commonNameOID)
	a.elements[1].bytes = []byte(text)
}

// TestProfileVendorProductIDInTheDN: describe "VendorID and ProductID in
// the distinguished name" (:427-644 at db021da0), the cases that parse
// without acceptMalformedVidPid — every caller in this package does. The
// cases with acceptMalformedVidPid are the device's read of its own chain,
// secure/attestation's TestProductIDsAcceptMalformed (DeviceCertification.ts
// :116-120 at db021da0); "cuts a commonName with invalid UTF-8 at byte 64"
// is not ported: crypto/x509 refuses a UTF8String DN value that is not
// UTF-8, so that DAC does not parse here.
func TestProfileVendorProductIDInTheDN(t *testing.T) {
	t.Parallel()
	validDAC, validPAI := validProfilePair(t)
	violations := map[string][]byte{
		"lowercase VendorID":           withNameText(t, validDAC, subjectElement, vendorIDOID, "fff1"),
		"VendorID of the wrong length": withNameText(t, validDAC, subjectElement, vendorIDOID, "FFF"),
		"non-hexadecimal ProductID":    withNameText(t, validDAC, subjectElement, productIDOID, "80G0"),
		"repeated VendorID":            withRepeatedSubjectAttribute(t, validDAC, vendorIDOID),
		"repeated ProductID":           withRepeatedSubjectAttribute(t, validDAC, productIDOID),
	}
	for name, dac := range violations {
		t.Run("reports a DAC with a "+name+" as a profile violation", func(t *testing.T) {
			t.Parallel()
			if _, err := parseAttCert(dac, kindDAC, false); !isProfileError(err) {
				t.Fatalf("parse = %v, want a profile violation", err)
			}
		})
	}
	t.Run("still reports a malformed VendorID when only accepting a profile violation", func(t *testing.T) {
		t.Parallel()
		if _, err := parseAttCert(violations["lowercase VendorID"], kindDAC, true); !isProfileError(err) {
			t.Fatalf("parse = %v, want a profile violation", err)
		}
	})
	t.Run("reports a malformed VendorID in the issuer of a DAC like one in the subject", func(t *testing.T) {
		t.Parallel()
		dac := withNameText(t, validDAC, issuerElement, vendorIDOID, "fff1")
		if _, err := parseAttCert(dac, kindDAC, false); !isProfileError(err) {
			t.Fatalf("parse = %v, want a profile violation", err)
		}
	})
	t.Run("reports a PAI with a malformed VendorID as a profile violation", func(t *testing.T) {
		t.Parallel()
		pai := withNameText(t, validPAI, subjectElement, vendorIDOID, "fff1")
		if _, err := parseAttCert(pai, kindPAI, false); !isProfileError(err) {
			t.Fatalf("parse = %v, want a profile violation", err)
		}
	})

	// describe "in the commonName" (:489-608): the DAC subject is
	// commonName "Matter Test DAC", then VendorID, then ProductID.
	dacWithFallback := func(t *testing.T, edit func(subject *derNode)) []byte {
		t.Helper()
		return editName(t, validDAC, subjectElement, edit)
	}
	dropProductID := func(t *testing.T, subject *derNode) {
		t.Helper()
		i := dnRDNIndexOf(t, subject, productIDOID)
		subject.elements = slices.Delete(subject.elements, i, i+1)
	}
	type ids struct{ vid, pid int } // -1 for absent
	read := func(t *testing.T, dac []byte) ids {
		t.Helper()
		c, err := parseAttCert(dac, kindDAC, false)
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		got := ids{-1, -1}
		if c.vendorID != nil {
			got.vid = int(*c.vendorID)
		}
		if c.productID != nil {
			got.pid = int(*c.productID)
		}
		return got
	}
	refused := func(t *testing.T, dac []byte, rule string) {
		t.Helper()
		_, err := parseAttCert(dac, kindDAC, false)
		if err == nil || isProfileError(err) || !regexp.MustCompile(rule).MatchString(err.Error()) {
			t.Fatalf("parse = %v, want a parse refusal /%s/", err, rule)
		}
	}
	x := func(n int) string { return strings.Repeat("x", n) }
	cases := []struct {
		name string
		edit func(t *testing.T, subject *derNode)
		want ids
	}{
		{"reads only the first commonName that carries a VendorID or ProductID", func(t *testing.T, s *derNode) {
			t.Helper()
			asCommonName(t, s, vendorIDOID, "Mvid:FFF1")
			asCommonName(t, s, productIDOID, "Mvid:FFF2 Mpid:8000")
		}, ids{0xFFF1, -1}},
		{"ignores a malformed prefix in a later commonName", func(t *testing.T, s *derNode) {
			t.Helper()
			asCommonName(t, s, vendorIDOID, "Mvid:FFF1 Mpid:8000")
			asCommonName(t, s, productIDOID, "Mvid:")
		}, ids{0xFFF1, 0x8000}},
		{"does not read a value beyond the first 64 bytes of a commonName", func(t *testing.T, s *derNode) {
			t.Helper()
			asCommonName(t, s, vendorIDOID, x(60)+"Mvid:FFF1")
			dropProductID(t, s)
		}, ids{-1, -1}},
		{"reads VendorID 0xFFF5", func(t *testing.T, s *derNode) {
			t.Helper()
			asCommonName(t, s, vendorIDOID, "Mvid:FFF5 Mpid:8000")
			dropProductID(t, s)
		}, ids{0xFFF5, 0x8000}},
		{"reads VendorID 0xFFFF", func(t *testing.T, s *derNode) {
			t.Helper()
			asCommonName(t, s, vendorIDOID, "Mvid:FFFF Mpid:8000")
			dropProductID(t, s)
		}, ids{0xFFFF, 0x8000}},
		{"reads a value that ends at byte 64 of a commonName", func(t *testing.T, s *derNode) {
			t.Helper()
			asCommonName(t, s, vendorIDOID, x(55)+"Mvid:FFF1")
			dropProductID(t, s)
		}, ids{0xFFF1, -1}},
		{"does not read a prefix that starts after byte 64 of a commonName", func(t *testing.T, s *derNode) {
			t.Helper()
			asCommonName(t, s, vendorIDOID, x(64)+"Mvid:FFF1")
			dropProductID(t, s)
		}, ids{-1, -1}},
		{"reads the values from a later commonName and keeps the first commonName", func(t *testing.T, s *derNode) {
			t.Helper()
			asCommonName(t, s, vendorIDOID, "Mvid:FFF1 Mpid:8000")
			dropProductID(t, s)
		}, ids{0xFFF1, 0x8000}},
		{"reads the values from the first commonName when a later one carries none", func(t *testing.T, s *derNode) {
			t.Helper()
			asCommonName(t, s, vendorIDOID, "Mvid:FFF1 Mpid:8000")
			dropProductID(t, s)
			s.elements = append(s.elements[1:], s.elements[0])
		}, ids{0xFFF1, 0x8000}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := read(t, dacWithFallback(t, func(s *derNode) { tc.edit(t, s) })); got != tc.want {
				t.Fatalf("ids %+v, want %+v", got, tc.want)
			}
		})
	}
	t.Run("is not read when the DN has a VendorID attribute", func(t *testing.T) {
		t.Parallel()
		if got := read(t, withNameText(t, validDAC, subjectElement, commonNameOID, "Matter Test DAC Mvid:FFF2")); got.vid != 0xFFF1 {
			t.Fatalf("ids %+v, want VendorID 0xFFF1", got)
		}
	})
	t.Run("refuses a malformed prefix in an earlier commonName", func(t *testing.T) {
		t.Parallel()
		refused(t, dacWithFallback(t, func(s *derNode) {
			dnAttributeOf(t, s, commonNameOID).elements[1].bytes = []byte("Matter Mpid:")
			asCommonName(t, s, vendorIDOID, "Mvid:FFF1 Mpid:8000")
			dropProductID(t, s)
		}), `Mpid: not followed by 4 uppercase hex digits`)
	})
	t.Run("refuses a value that byte 64 of a commonName cuts", func(t *testing.T) {
		t.Parallel()
		refused(t, dacWithFallback(t, func(s *derNode) {
			asCommonName(t, s, vendorIDOID, x(56)+"Mvid:FFF1")
			dropProductID(t, s)
		}), `Mvid: not followed by 4 uppercase hex digits`)
	})

	t.Run("reports a DAC with a malformed VendorID as DacFormatInvalid", func(t *testing.T) {
		t.Parallel()
		if ae := profileValidate(t, violations["lowercase VendorID"], validPAI); ae.Check != DacFormatInvalid {
			t.Fatalf("%s %q", ae.Check, ae.Message)
		}
	})
	t.Run("reports a PAI with a repeated VendorID as PaiFormatInvalid", func(t *testing.T) {
		t.Parallel()
		if ae := profileValidate(t, validDAC, withRepeatedSubjectAttribute(t, validPAI, vendorIDOID)); ae.Check != PaiFormatInvalid {
			t.Fatalf("%s %q", ae.Check, ae.Message)
		}
	})
}

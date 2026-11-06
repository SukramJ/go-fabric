// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package mattercert

import (
	"bytes"
	"encoding/asn1"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"
)

// derRaw marshals one DER value from its class, tag and content.
func derRaw(t *testing.T, class, tag int, compound bool, content []byte) []byte {
	t.Helper()
	b, err := asn1.Marshal(asn1.RawValue{Class: class, Tag: tag, IsCompound: compound, Bytes: content})
	if err != nil {
		t.Fatalf("asn1.Marshal: %v", err)
	}
	return b
}

func derSeq(t *testing.T, children ...[]byte) []byte {
	t.Helper()
	return derRaw(t, asn1.ClassUniversal, asn1.TagSequence, true, bytes.Join(children, nil))
}

func derSet(t *testing.T, children ...[]byte) []byte {
	t.Helper()
	return derRaw(t, asn1.ClassUniversal, asn1.TagSet, true, bytes.Join(children, nil))
}

func derMust(t *testing.T, v any) []byte {
	t.Helper()
	b, err := asn1.Marshal(v)
	if err != nil {
		t.Fatalf("asn1.Marshal(%T): %v", v, err)
	}
	return b
}

func derChildrenOf(t *testing.T, full []byte) [][]byte {
	t.Helper()
	var v asn1.RawValue
	if _, err := asn1.Unmarshal(full, &v); err != nil {
		t.Fatalf("asn1.Unmarshal: %v", err)
	}
	parts, err := derChildren(v.Bytes)
	if err != nil {
		t.Fatalf("derChildren: %v", err)
	}
	out := make([][]byte, len(parts))
	for i, p := range parts {
		out[i] = p.FullBytes
	}
	return out
}

// derParts is a DER certificate split into its TBS fields (the version
// [0] included when present), its algorithm and its signature, so one
// field can be replaced and the certificate reassembled.
type derParts struct {
	fields   [][]byte
	alg, sig []byte
}

func derPartsOf(t *testing.T, der []byte) derParts {
	t.Helper()
	top := derChildrenOf(t, der)
	if len(top) != 3 {
		t.Fatalf("certificate of %d root elements", len(top))
	}
	return derParts{fields: derChildrenOf(t, top[0]), alg: top[1], sig: top[2]}
}

// idx is the index of the n-th TBS field after the version: 0 serial,
// 1 algorithm, 2 issuer, 3 validity, 4 subject, 5 public key, 6 extensions.
func (p derParts) idx(n int) int {
	if p.fields[0][0] == 0xa0 {
		return n + 1
	}
	return n
}

func (p derParts) with(n int, field []byte) derParts {
	p.fields = slices.Clone(p.fields)
	p.fields[p.idx(n)] = field
	return p
}

func (p derParts) tbs(t *testing.T) []byte { t.Helper(); return derSeq(t, p.fields...) }

func (p derParts) cert(t *testing.T) []byte { t.Helper(); return derSeq(t, p.tbs(t), p.alg, p.sig) }

func (p derParts) certWithAlg(t *testing.T, alg []byte) []byte {
	t.Helper()
	return derSeq(t, p.with(1, alg).tbs(t), alg, p.sig)
}

func derCtx3(t *testing.T, children ...[]byte) []byte {
	t.Helper()
	return derRaw(t, asn1.ClassContextSpecific, 3, true, bytes.Join(children, nil))
}

func derExt(t *testing.T, oid asn1.ObjectIdentifier, value []byte) []byte {
	t.Helper()
	return derSeq(t, derMust(t, oid), derRaw(t, asn1.ClassUniversal, asn1.TagOctetString, false, value))
}

func derUTC(t *testing.T, s string) []byte {
	t.Helper()
	return derRaw(t, asn1.ClassUniversal, asn1.TagUTCTime, false, []byte(s))
}

// extensionsByOID maps the certificate's extension entries by their OID.
func (p derParts) extensionsByOID(t *testing.T) map[string][]byte {
	t.Helper()
	seqs := derChildrenOf(t, p.fields[p.idx(6)])
	if len(seqs) != 1 {
		t.Fatalf("extensions of %d sequences", len(seqs))
	}
	out := map[string][]byte{}
	for _, e := range derChildrenOf(t, seqs[0]) {
		var oid asn1.ObjectIdentifier
		if _, err := asn1.Unmarshal(derChildrenOf(t, e)[0], &oid); err != nil {
			t.Fatalf("extension OID: %v", err)
		}
		out[oid.String()] = e
	}
	return out
}

// Every malformed part of a DER certificate is refused as ErrMalformed
// (or ErrUnsupportedAlgorithm for an algorithm this module does not
// speak) — parseAsn1Certificate (Certificate.ts:612-770 at 862ad75f)
// throws on each.
func TestDecodeDERRefusesEachMalformedPart(t *testing.T) {
	t.Parallel()
	rcac := certificatesExternalDER(t, "RCAC")
	p := derPartsOf(t, rcac)
	if !bytes.Equal(p.cert(t), rcac) {
		t.Fatal("the reassembled RCAC differs from the original")
	}
	spki := derChildrenOf(t, p.fields[p.idx(5)])
	badOID := derRaw(t, asn1.ClassUniversal, asn1.TagOID, false, []byte{0x80})
	garbageSeq := derRaw(t, asn1.ClassUniversal, asn1.TagSequence, true, []byte{0xff})
	sha384 := derSeq(t, derMust(t, asn1.ObjectIdentifier{1, 2, 840, 10045, 4, 3, 3}))
	validity := func(a, b string) []byte { return derSeq(t, derUTC(t, a), derUTC(t, b)) }
	keyAlg := func(key, curve []byte) []byte { return derSeq(t, derSeq(t, key, curve), spki[1]) }
	exts := func(entries ...[]byte) []byte { return derCtx3(t, derSeq(t, entries...)) }

	cases := []struct {
		name string
		der  []byte
		want string
		is   error
	}{
		{"trailing byte", append(slices.Clone(rcac), 0), "not a DER certificate", ErrMalformed},
		{"not a sequence", derRaw(t, asn1.ClassUniversal, asn1.TagOctetString, false, []byte{1}), "not a DER certificate", ErrMalformed},
		{"two root elements", derSeq(t, p.tbs(t), p.alg), "expected 3 root elements", ErrMalformed},
		{"five TBS fields", derSeq(t, derSeq(t, p.fields[:5]...), p.alg, p.sig), "invalid TBSCertificate structure", ErrMalformed},
		{"TBS content unreadable", derSeq(t, garbageSeq, p.alg, p.sig), "invalid TBSCertificate structure", ErrMalformed},
		{"algorithm differs inside and outside", derSeq(t, p.tbs(t), sha384, p.sig), "signature algorithm differs", ErrMalformed},
		{"unsupported algorithm", p.certWithAlg(t, sha384), "signature algorithm", ErrUnsupportedAlgorithm},
		{"algorithm without an OID", p.certWithAlg(t, derSeq(t, derMust(t, 1))), "invalid signature algorithm structure", ErrMalformed},
		{"algorithm OID unreadable", p.certWithAlg(t, derSeq(t, badOID)), "invalid signature algorithm structure", ErrMalformed},
		{"issuer unreadable", p.with(2, garbageSeq).cert(t), "issuer: ", ErrMalformed},
		{"validity of three", p.with(3, derSeq(t, derUTC(t, "260101000000Z"), derUTC(t, "270101000000Z"), derUTC(t, "280101000000Z"))).cert(t), "invalid validity structure", ErrMalformed},
		{"notBefore unreadable", p.with(3, validity("garbage", "270101000000Z")).cert(t), "unsupported date", ErrMalformed},
		{"notBefore before the Matter epoch", p.with(3, validity("700101000000Z", "270101000000Z")).cert(t), "before the Matter epoch", ErrMalformed},
		{"subject unreadable", p.with(4, garbageSeq).cert(t), "subject: ", ErrMalformed},
		{"public key of one element", p.with(5, derSeq(t, spki[0])).cert(t), "invalid public key structure", ErrMalformed},
		{"public key algorithm of one element", p.with(5, derSeq(t, derSeq(t, derMust(t, oidEcPublicKey)), spki[1])).cert(t), "invalid public key algorithm structure", ErrMalformed},
		{"public key algorithm OID unreadable", p.with(5, keyAlg(badOID, derMust(t, oidPrime256v1))).cert(t), "public key algorithm: ", ErrMalformed},
		{"public key curve OID unreadable", p.with(5, keyAlg(derMust(t, oidEcPublicKey), badOID)).cert(t), "public key curve: ", ErrMalformed},
		{"public key on another curve", p.with(5, keyAlg(derMust(t, oidEcPublicKey), derMust(t, asn1.ObjectIdentifier{1, 3, 132, 0, 34}))).cert(t), "public key", ErrUnsupportedAlgorithm},
		{"public key with unused bits", p.with(5, derSeq(t, spki[0], derMust(t, asn1.BitString{Bytes: []byte{1, 2, 3}, BitLength: 23}))).cert(t), "without unused bits", ErrMalformed},
		{"extensions field absent", p.with(6, derSeq(t)).cert(t), "missing required extensions", ErrMalformed},
		{"extensions of two sequences", p.with(6, derCtx3(t, derSeq(t), derSeq(t))).cert(t), "invalid extensions structure", ErrMalformed},
		{"extensions list unreadable", p.with(6, derCtx3(t, garbageSeq)).cert(t), "invalid extensions structure", ErrMalformed},
		{"basic constraints unreadable", p.with(6, exts(derExt(t, oidExtBasicConstraints, []byte{0xff}))).cert(t), "basic-constraints: ", ErrMalformed},
		{"key usage unreadable", p.with(6, exts(derExt(t, oidExtKeyUsage, []byte{0xff}))).cert(t), "key-usage: ", ErrMalformed},
		{"extended key usage unreadable", p.with(6, exts(derExt(t, oidExtExtendedKeyUsage, []byte{0xff}))).cert(t), "extended-key-usage: ", ErrMalformed},
		{"subject key id unreadable", p.with(6, exts(derExt(t, oidExtSubjectKeyID, []byte{0xff}))).cert(t), "subject-key-id: ", ErrMalformed},
		{"authority key id unreadable", p.with(6, exts(derExt(t, oidExtAuthorityKeyID, []byte{0xff}))).cert(t), "authority-key-id: ", ErrMalformed},
		{"a required extension missing", p.with(6, exts(p.extensionsByOID(t)[oidExtBasicConstraints.String()])).cert(t), "missing required extensions", ErrMalformed},
		{"signature not a bit string", derSeq(t, p.tbs(t), p.alg, derRaw(t, asn1.ClassUniversal, asn1.TagOctetString, false, []byte{1})), "signature: ", ErrMalformed},
		{"signature not an ECDSA-Sig-Value", derSeq(t, p.tbs(t), p.alg, derMust(t, asn1.BitString{Bytes: []byte{0xff}, BitLength: 8})), "not an ECDSA-Sig-Value", ErrMalformed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := DecodeDER(tc.der)
			if err == nil || !errors.Is(err, tc.is) || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("DecodeDER = %v, want %v containing %q", err, tc.is, tc.want)
			}
		})
	}
}

// A truncated certificate is refused at every length, never read past
// its end.
func TestDecodeDERRefusesEveryTruncation(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"RCAC", "ICAC", "NOC"} {
		der := certificatesExternalDER(t, kind)
		for n := range der {
			if _, err := DecodeDER(der[:n]); err == nil {
				t.Fatalf("%s[:%d] decoded", kind, n)
			}
		}
	}
}

// Extension entries that are not Matter's are skipped whatever their
// shape — parseExtensions (Certificate.ts:406-547) reads the five by OID
// and leaves the rest — and every extended-key-usage purpose maps to its
// Matter id.
func TestDecodeDERSkipsForeignExtensionsAndReadsEveryKeyPurpose(t *testing.T) {
	t.Parallel()
	p := derPartsOf(t, certificatesExternalDER(t, "RCAC"))
	byOID := p.extensionsByOID(t)
	oneElement := derSeq(t, derMust(t, oidExtKeyUsage))
	badOID := derSeq(t, derRaw(t, asn1.ClassUniversal, asn1.TagOID, false, []byte{0x80}), derRaw(t, asn1.ClassUniversal, asn1.TagOctetString, false, []byte{1}))
	altName := derExt(t, asn1.ObjectIdentifier{2, 5, 29, 17}, []byte{0x30, 0x00})
	eku := derExt(t, oidExtExtendedKeyUsage, derMust(t, []asn1.ObjectIdentifier{
		oidEKUServerAuth, oidEKUClientAuth, oidEKUCodeSigning, oidEKUEmailProtection, oidEKUTimeStamping, oidEKUOCSPSigning,
		{1, 3, 6, 1, 5, 5, 7, 3, 17},
	}))
	list := derCtx3(t, derSeq(t,
		oneElement, badOID, altName,
		byOID[oidExtBasicConstraints.String()], byOID[oidExtKeyUsage.String()], eku,
		byOID[oidExtSubjectKeyID.String()], byOID[oidExtAuthorityKeyID.String()],
	))
	c, err := DecodeDER(p.with(6, list).cert(t))
	if err != nil {
		t.Fatalf("DecodeDER: %v", err)
	}
	if !c.Extensions.HasExtendedKeyUsage || !slices.Equal(c.Extensions.ExtendedKeyUsage, []uint8{1, 2, 3, 4, 5, 6}) {
		t.Fatalf("ExtendedKeyUsage = %v, want the six purposes", c.Extensions.ExtendedKeyUsage)
	}
	if want := []uint8{extTagBasicConstraints, extTagKeyUsage, extTagExtendedKeyUsage, extTagSubjectKeyID, extTagAuthorityKeyID}; !slices.Equal(c.Extensions.Order, want) {
		t.Fatalf("extension order = %v, want %v", c.Extensions.Order, want)
	}
}

// derDN refuses a Matter id that is not hex, skips an attribute it cannot
// read or does not know, and reads a domainComponent and a
// PrintableString under their tags — parseSubjectOrIssuer
// (Certificate.ts:296-394 at 862ad75f).
func TestDERDistinguishedNameEdges(t *testing.T) {
	t.Parallel()
	attr := func(oid asn1.ObjectIdentifier, tag int, value string) []byte {
		return derSet(t, derSeq(t, derMust(t, oid), derRaw(t, asn1.ClassUniversal, tag, false, []byte(value))))
	}
	name := func(rdns ...[]byte) asn1.RawValue {
		var v asn1.RawValue
		if _, err := asn1.Unmarshal(derSeq(t, rdns...), &v); err != nil {
			t.Fatalf("name: %v", err)
		}
		return v
	}
	for _, oid := range []asn1.ObjectIdentifier{oidMatterNodeID, oidMatterICACID, oidMatterRCACID, oidMatterFabricID, oidMatterCASEAuthTag} {
		if _, err := derDN(name(attr(oid, asn1.TagUTF8String, "zz"))); err == nil || !errors.Is(err, ErrMalformed) || !strings.Contains(err.Error(), "not hex") {
			t.Fatalf("derDN(%v = zz) = %v, want the not-hex refusal", oid, err)
		}
	}
	if _, err := derDN(asn1.RawValue{Bytes: []byte{0xff}}); err == nil || !strings.Contains(err.Error(), "invalid subject/issuer structure") {
		t.Fatalf("derDN(unreadable) = %v", err)
	}
	dn, err := derDN(name(
		derRaw(t, asn1.ClassUniversal, asn1.TagSet, true, []byte{0xff}),                                         // RDN unreadable: skipped
		derSet(t, derSeq(t, derMust(t, oidDomainComponent))),                                                    // one element: skipped
		derSet(t, derSeq(t, derRaw(t, asn1.ClassUniversal, asn1.TagOID, false, []byte{0x80}), derMust(t, "x"))), // OID unreadable: skipped
		attr(asn1.ObjectIdentifier{2, 5, 4, 99}, asn1.TagUTF8String, "x"),                                       // unknown X.520 type: skipped
		attr(asn1.ObjectIdentifier{1, 2, 3}, asn1.TagUTF8String, "x"),                                           // not X.520: skipped
		attr(oidDomainComponent, asn1.TagIA5String, "example"),
		attr(asn1.ObjectIdentifier{2, 5, 4, 3}, asn1.TagPrintableString, "cn"),
	))
	if err != nil {
		t.Fatalf("derDN: %v", err)
	}
	if want := []uint8{dnTagStandardLast, dnTagPrintableFirst}; !slices.Equal(dn.Order, want) {
		t.Fatalf("Order = %v, want %v", dn.Order, want)
	}
	if len(dn.Attributes) != 2 || dn.Attributes[0].Value != "example" || dn.Attributes[1].Value != "cn" {
		t.Fatalf("Attributes = %+v", dn.Attributes)
	}
	// A repeated single-valued Matter attribute is refused in DER as in
	// TLV (matter.js Certificate.ts parseSubjectOrIssuer, "more than once"
	// — OperationalCertificateVerifyTest.ts:1014-1019 at db021da0); a
	// repeated standard attribute is kept, every value in order
	// (DistinguishedNameTest.ts:203-237, :1021-1030).
	for _, oid := range []asn1.ObjectIdentifier{oidMatterNodeID, oidMatterICACID, oidMatterRCACID, oidMatterFabricID, oidMatterVVSID} {
		_, err := derDN(name(attr(oid, asn1.TagUTF8String, "0000000000000001"), attr(oidMatterFabricID, asn1.TagUTF8String, "00000000000000AB"), attr(oid, asn1.TagUTF8String, "0000000000000002")))
		if err == nil || !errors.Is(err, ErrMalformed) || !strings.Contains(err.Error(), "repeated") {
			t.Fatalf("derDN(%v twice) = %v, want the repeated refusal", oid, err)
		}
	}
	dn, err = derDN(name(
		attr(asn1.ObjectIdentifier{2, 5, 4, 3}, asn1.TagUTF8String, "A"),
		attr(asn1.ObjectIdentifier{2, 5, 4, 11}, asn1.TagUTF8String, "Unit 1"),
		attr(oidMatterNodeID, asn1.TagUTF8String, "0000000000000001"),
		attr(oidDomainComponent, asn1.TagIA5String, "example"),
		attr(asn1.ObjectIdentifier{2, 5, 4, 11}, asn1.TagUTF8String, "Unit 2"),
		attr(oidMatterFabricID, asn1.TagUTF8String, "00000000000000AB"),
		attr(asn1.ObjectIdentifier{2, 5, 4, 3}, asn1.TagUTF8String, "B"),
		attr(oidDomainComponent, asn1.TagIA5String, "com"),
	))
	if err != nil {
		t.Fatalf("derDN(repeated standard attributes): %v", err)
	}
	if want := []uint8{1, 8, dnTagMatterNodeID, dnTagStandardLast, 8, dnTagMatterFabricID, 1, dnTagStandardLast}; !slices.Equal(dn.Order, want) { // commonName 1, organizationalUnitName 8
		t.Fatalf("Order = %v, want %v", dn.Order, want)
	}
	values := make([]string, 0, len(dn.Attributes))
	for _, a := range dn.Attributes {
		values = append(values, a.Value)
	}
	if want := []string{"A", "Unit 1", "example", "Unit 2", "B", "com"}; !slices.Equal(values, want) {
		t.Fatalf("Attributes = %v, want %v", values, want)
	}
}

func TestEncodeDERRefusals(t *testing.T) {
	t.Parallel()
	base := certificatesDecodeDER(t, certificatesExternalDER(t, "RCAC"))

	short := *base
	short.Signature = base.Signature[:10]
	if _, err := EncodeDER(&short); err == nil || !strings.Contains(err.Error(), "signature length") {
		t.Fatalf("EncodeDER(short signature) = %v", err)
	}

	unknownExt := *base
	unknownExt.TBSDER = nil
	unknownExt.Extensions.Order = []uint8{99}
	if _, err := EncodeDER(&unknownExt); err == nil || !strings.Contains(err.Error(), "unknown extension tag") {
		t.Fatalf("EncodeDER(unknown extension tag) = %v", err)
	}

	huge := *base
	huge.TBSDER = nil
	huge.Subject = DistinguishedName{MatterRCACID: base.Subject.MatterRCACID, HasRCACID: true, Attributes: []DNAttribute{{Tag: dnTagStandardFirst + 2, Value: strings.Repeat("x", 600)}}, Order: []uint8{dnTagMatterRCACID, dnTagStandardFirst + 2}}
	if _, err := EncodeDER(&huge); err == nil || !strings.Contains(err.Error(), "600 byte limit") {
		t.Fatalf("EncodeDER(over 600 bytes) = %v", err)
	}
}

// The TLV encoder refuses an order entry without its value and an
// unknown tag in an order.
func TestEncodeRefusesOrdersWithoutValues(t *testing.T) {
	t.Parallel()
	if _, err := Encode(nil); !errors.Is(err, ErrMalformed) {
		t.Fatalf("Encode(nil) = %v", err)
	}
	base := certificatesDecodeDER(t, certificatesExternalDER(t, "RCAC"))
	cases := []struct {
		name  string
		tweak func(c *Certificate)
		want  string
	}{
		{"issuer CAT without value", func(c *Certificate) { c.Issuer.Order = append(slices.Clone(c.Issuer.Order), dnTagMatterCASEAuth) }, "issuer: "},
		{"subject unknown tag", func(c *Certificate) { c.Subject.Order = []uint8{99} }, "unknown DN tag"},
		{"subject attribute without value", func(c *Certificate) { c.Subject.Order, c.Subject.Attributes = []uint8{dnTagStandardFirst}, nil }, "DN attribute order entry without value"},
		{"future extension without value", func(c *Certificate) { c.Extensions.Order = []uint8{extTagFutureExtension} }, "future-extension order entry without value"},
		{"unknown extension tag", func(c *Certificate) { c.Extensions.Order = []uint8{99} }, "unknown extension tag"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c := *base
			tc.tweak(&c)
			if _, err := Encode(&c); err == nil || !errors.Is(err, ErrMalformed) || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Encode = %v, want ErrMalformed containing %q", err, tc.want)
			}
		})
	}
}

// A DN or extension set built without an Order is written in the spec's
// tag order: the Matter ids, the CATs, the standard attributes; the five
// extensions, then the future ones.
func TestEncodeCanonicalOrderWithoutAnOrder(t *testing.T) {
	t.Parallel()
	c := *certificatesDecodeDER(t, certificatesExternalDER(t, "RCAC"))
	c.TBSDER = nil
	c.Subject = DistinguishedName{
		MatterRCACID: 1, HasRCACID: true, MatterICACID: 2, HasICACID: true,
		MatterNodeID: 3, HasNodeID: true, MatterFabricID: 4, HasFabricID: true,
		CASEAuthTags: []uint32{0xabcd0001},
		Attributes:   []DNAttribute{{Tag: dnTagStandardFirst + 2, Value: "cn"}},
	}
	c.Extensions = CertificateExtensions{
		HasBasicConstraints: true, BasicConstraintsIsCA: true,
		HasKeyUsage: true, KeyUsage: KeyUsageKeyCertSign,
		HasExtendedKeyUsage: true, ExtendedKeyUsage: []uint8{2},
		HasSubjectKeyID: true, SubjectKeyID: make([]byte, 20),
		HasAuthorityKeyID: true, AuthorityKeyID: make([]byte, 20),
		FutureExtensions: [][]byte{{0x30, 0x00}},
	}
	raw, err := Encode(&c)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	back, err := Decode(raw)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if want := []uint8{dnTagMatterRCACID, dnTagMatterICACID, dnTagMatterNodeID, dnTagMatterFabricID, dnTagMatterCASEAuth, dnTagStandardFirst + 2}; !slices.Equal(back.Subject.Order, want) {
		t.Fatalf("subject order = %v, want %v", back.Subject.Order, want)
	}
	if want := []uint8{extTagBasicConstraints, extTagKeyUsage, extTagExtendedKeyUsage, extTagSubjectKeyID, extTagAuthorityKeyID, extTagFutureExtension}; !slices.Equal(back.Extensions.Order, want) {
		t.Fatalf("extension order = %v, want %v", back.Extensions.Order, want)
	}
	if _, err := TBSToDER(&c); err != nil {
		t.Fatalf("TBSToDER without an order: %v", err)
	}
}

func TestVerifierConstructionAndExtractionRefusals(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	ts := FixedTime{T: at}
	if got := ts.Now(); !got.Equal(at) {
		t.Fatalf("FixedTime.Now() = %v", got)
	}
	root := certificatesDecodeDER(t, certificatesExternalDER(t, "RCAC"))
	noc := certificatesDecodeDER(t, certificatesExternalDER(t, "NOC"))

	if _, err := NewVerifierFromRCAC(make([]byte, MaxOperationalCertTLVBytes+1), ts); err == nil || !strings.Contains(err.Error(), "over the") {
		t.Fatalf("NewVerifierFromRCAC(over the cap) = %v", err)
	}
	if _, err := NewVerifierFromCertificate(nil, ts); err == nil || !strings.Contains(err.Error(), "nil root") {
		t.Fatalf("NewVerifierFromCertificate(nil) = %v", err)
	}
	if _, err := NewVerifierFromCertificate(noc, ts); err == nil || !strings.Contains(err.Error(), "not a root CA") {
		t.Fatalf("NewVerifierFromCertificate(NOC) = %v", err)
	}
	shortSKID := *root
	shortSKID.Extensions.SubjectKeyID = root.Extensions.SubjectKeyID[:19]
	if _, err := NewVerifierFromCertificate(&shortSKID, ts); err == nil || !strings.Contains(err.Error(), "160 bit") {
		t.Fatalf("NewVerifierFromCertificate(19-byte SKID) = %v", err)
	}
	badKey := *root
	badKey.PublicKey = []byte{1}
	if _, err := NewVerifierFromCertificate(&badKey, ts); err == nil {
		t.Fatal("NewVerifierFromCertificate(1-byte public key) accepted")
	}

	v, err := NewVerifierFromCertificate(root, ts)
	if err != nil {
		t.Fatalf("NewVerifierFromCertificate: %v", err)
	}
	noFabric := *noc
	noFabric.Subject = DistinguishedName{MatterNodeID: 1, HasNodeID: true}
	rawNoFabric, err := Encode(&noFabric)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if _, err := v.PeerFabricIDFromNOC(rawNoFabric); err == nil || !strings.Contains(err.Error(), "missing FabricID") {
		t.Fatalf("PeerFabricIDFromNOC(no fabric id) = %v", err)
	}
	if _, err := v.PeerCATsFromNOC([]byte{0xff}); err == nil {
		t.Fatal("PeerCATsFromNOC(garbage) accepted")
	}
	withCATs := *noc
	withCATs.Subject = DistinguishedName{MatterNodeID: 1, HasNodeID: true, MatterFabricID: 2, HasFabricID: true, CASEAuthTags: []uint32{0xabcd0001, 0x12340002}}
	rawCATs, err := Encode(&withCATs)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if cats, err := v.PeerCATsFromNOC(rawCATs); err != nil || !slices.Equal(cats, []uint32{0xabcd0001, 0x12340002}) {
		t.Fatalf("PeerCATsFromNOC = %v, %v", cats, err)
	}
	if _, err := v.VerifyAndExtractPubKey(noc.Raw, make([]byte, MaxOperationalCertTLVBytes+1)); err == nil || !strings.Contains(err.Error(), "ICAC is") {
		t.Fatalf("VerifyAndExtractPubKey(ICAC over the cap) = %v", err)
	}
	if _, err := v.VerifyAndExtractPubKey([]byte{0xff}, nil); err == nil || !strings.Contains(err.Error(), "noc: ") {
		t.Fatalf("VerifyAndExtractPubKey(garbage NOC) = %v", err)
	}
}

// ValidateRCAC's refusals, each on the external RCAC with one field
// moved — chip CHIPCert.cpp:1131-1144 (ValidateRCAC's own comments).
func TestValidateRCACRefusals(t *testing.T) {
	t.Parallel()
	root := certificatesDecodeDER(t, certificatesExternalDER(t, "RCAC"))
	noc := certificatesDecodeDER(t, certificatesExternalDER(t, "NOC"))
	if err := ValidateRCAC(root); err != nil {
		t.Fatalf("ValidateRCAC(external RCAC) = %v", err)
	}
	cases := []struct {
		name  string
		tweak func(c *Certificate)
		want  string
	}{
		{"not a root", func(c *Certificate) { *c = *noc }, "not a Root CA"},
		{"issuer id differs", func(c *Certificate) { c.Issuer.MatterRCACID++ }, "root must be self-signed"},
		{"no key usage", func(c *Certificate) { c.Extensions.HasKeyUsage = false }, "KeyUsage extension absent"},
		{"no keyCertSign", func(c *Certificate) { c.Extensions.KeyUsage = 0x0001 }, "missing keyCertSign"},
		{"public key unreadable", func(c *Certificate) { c.PublicKey = []byte{1} }, "cannot decode RCAC public key"},
		{"signature short", func(c *Certificate) { c.Signature = c.Signature[:8] }, "signature length"},
		{"TBS not rebuildable", func(c *Certificate) { c.TBSDER, c.Extensions.Order = nil, []uint8{99} }, "rebuild tbs der"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c := *root
			tc.tweak(&c)
			if err := ValidateRCAC(&c); err == nil || !errors.Is(err, ErrInvalidRCAC) || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("ValidateRCAC = %v, want ErrInvalidRCAC containing %q", err, tc.want)
			}
		})
	}
}

func TestCreateCSRRefusesANilKey(t *testing.T) {
	t.Parallel()
	if _, err := CreateCSR(nil, nil); !errors.Is(err, ErrMalformed) {
		t.Fatalf("CreateCSR(nil) = %v", err)
	}
}

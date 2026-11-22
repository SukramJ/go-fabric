// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package mattercert

import (
	"bytes"
	"errors"
	"testing"

	"thingfabric.dev/go-fabric/tlv"
)

// shapeCert encodes a minimal NOC-shaped certificate. Each member is
// written by a named writer; over replaces a writer (a nil one leaves the
// member out); the four mandatory extensions are "bc", "ku", "skid" and
// "akid", and the writers named "subject+" and "ext+" are appended
// at the end of the subject DN and of the extensions list.
func shapeCert(t *testing.T, over map[string]func(e *tlv.Encoder)) []byte {
	t.Helper()
	pub := make([]byte, 65)
	pub[0] = 0x04
	def := map[string]func(e *tlv.Encoder){
		"serial":   func(e *tlv.Encoder) { e.PutOctets(tlv.ContextTag(tagSerialNumber), []byte{0x01}) },
		"sigAlgo":  func(e *tlv.Encoder) { e.PutUint(tlv.ContextTag(tagSignatureAlgorithm), SigAlgoECDSAWithSHA256) },
		"issuer20": func(e *tlv.Encoder) { e.PutUint(tlv.ContextTag(dnTagMatterRCACID), 1) },
		"notBefore": func(e *tlv.Encoder) {
			e.PutUint(tlv.ContextTag(tagNotBefore), 1)
		},
		"notAfter":   func(e *tlv.Encoder) { e.PutUint(tlv.ContextTag(tagNotAfter), 2) },
		"subject17":  func(e *tlv.Encoder) { e.PutUint(tlv.ContextTag(dnTagMatterNodeID), 0x1234) },
		"subject21":  func(e *tlv.Encoder) { e.PutUint(tlv.ContextTag(dnTagMatterFabricID), 1) },
		"subject+":   func(*tlv.Encoder) {},
		"pubAlgo":    func(e *tlv.Encoder) { e.PutUint(tlv.ContextTag(tagPublicKeyAlgorithm), PubKeyAlgoEC) },
		"curve":      func(e *tlv.Encoder) { e.PutUint(tlv.ContextTag(tagEllipticCurveID), CurvePrime256v1) },
		"pub":        func(e *tlv.Encoder) { e.PutOctets(tlv.ContextTag(tagEllipticCurvePub), pub) },
		"extensions": nil, // the list itself, written below unless replaced
		"bc": func(e *tlv.Encoder) {
			e.StartStruct(tlv.ContextTag(extTagBasicConstraints))
			e.PutBool(tlv.ContextTag(extBCInnerTagIsCa), false)
			_ = e.EndContainer()
		},
		"ku":        func(e *tlv.Encoder) { e.PutUint(tlv.ContextTag(extTagKeyUsage), 0x01) },
		"ext+":      func(*tlv.Encoder) {},
		"skid":      func(e *tlv.Encoder) { e.PutOctets(tlv.ContextTag(extTagSubjectKeyID), make([]byte, keyIDSize)) },
		"akid":      func(e *tlv.Encoder) { e.PutOctets(tlv.ContextTag(extTagAuthorityKeyID), make([]byte, keyIDSize)) },
		"signature": func(e *tlv.Encoder) { e.PutOctets(tlv.ContextTag(tagSignature), make([]byte, 64)) },
	}
	w := func(k string, e *tlv.Encoder) {
		f, ok := over[k]
		if !ok {
			f = def[k]
		}
		if f != nil {
			f(e)
		}
	}
	e := tlv.NewEncoder()
	e.StartStruct(tlv.AnonymousTag())
	w("serial", e)
	w("sigAlgo", e)
	e.StartList(tlv.ContextTag(tagIssuer))
	w("issuer20", e)
	_ = e.EndContainer()
	w("notBefore", e)
	w("notAfter", e)
	e.StartList(tlv.ContextTag(tagSubject))
	w("subject17", e)
	w("subject21", e)
	w("subject+", e)
	_ = e.EndContainer()
	w("pubAlgo", e)
	w("curve", e)
	w("pub", e)
	if f, ok := over["extensions"]; ok {
		if f != nil {
			f(e)
		}
	} else {
		e.StartList(tlv.ContextTag(tagExtensions))
		w("bc", e)
		w("ku", e)
		w("ext+", e)
		w("skid", e)
		w("akid", e)
		_ = e.EndContainer()
	}
	w("signature", e)
	if err := e.EndContainer(); err != nil {
		t.Fatal(err)
	}
	b, err := e.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// TestDecodeRefusesAFieldOfAnotherTypeOrWidth holds R2-P6-wire-0103: every
// member of the certificate's TLV form is read with its type and its
// width. An integer member carried as a string or a container, an
// integer wider than its field, an octet-string member of another type,
// the extensions as anything but a list, a basic-constraints that is not
// a structure, an extended-key-usage that is not an array and an
// extension other than a future extension given twice are refused —
// before, each decoded with its value masked, read as 0 or the last one
// kept. The control is the same certificate with the member well formed.
func TestDecodeRefusesAFieldOfAnotherTypeOrWidth(t *testing.T) {
	t.Parallel()
	if _, err := Decode(shapeCert(t, nil)); err != nil {
		t.Fatalf("control: the well-formed certificate is refused: %v", err)
	}
	put := func(f func(e *tlv.Encoder)) func(e *tlv.Encoder) { return f }
	utf8 := func(tag uint8) func(e *tlv.Encoder) {
		return func(e *tlv.Encoder) { e.PutUTF8(tlv.ContextTag(tag), "1") }
	}
	cases := map[string]map[string]func(e *tlv.Encoder){
		"signature algorithm as UTF-8":        {"sigAlgo": utf8(tagSignatureAlgorithm)},
		"signature algorithm above 8 bits":    {"sigAlgo": put(func(e *tlv.Encoder) { e.PutUint(tlv.ContextTag(tagSignatureAlgorithm), 0x100|SigAlgoECDSAWithSHA256) })},
		"notBefore as UTF-8":                  {"notBefore": utf8(tagNotBefore)},
		"notAfter as a signed integer":        {"notAfter": put(func(e *tlv.Encoder) { e.PutInt(tlv.ContextTag(tagNotAfter), 2) })},
		"public-key algorithm above 8 bits":   {"pubAlgo": put(func(e *tlv.Encoder) { e.PutUint(tlv.ContextTag(tagPublicKeyAlgorithm), 0x100|PubKeyAlgoEC) })},
		"curve as an octet string":            {"curve": put(func(e *tlv.Encoder) { e.PutOctets(tlv.ContextTag(tagEllipticCurveID), []byte{byte(CurvePrime256v1)}) })},
		"public key as UTF-8":                 {"pub": utf8(tagEllipticCurvePub)},
		"issuer RCAC id as UTF-8":             {"issuer20": utf8(dnTagMatterRCACID)},
		"subject node id as UTF-8":            {"subject17": utf8(dnTagMatterNodeID)},
		"subject node id as a signed integer": {"subject17": put(func(e *tlv.Encoder) { e.PutInt(tlv.ContextTag(dnTagMatterNodeID), 0x1234) })},
		"subject fabric id as UTF-8":          {"subject21": utf8(dnTagMatterFabricID)},
		"CAT above 32 bits":                   {"subject+": put(func(e *tlv.Encoder) { e.PutUint(tlv.ContextTag(dnTagMatterCASEAuth), 0x1_0001_0001) })},
		"CAT as UTF-8":                        {"subject+": utf8(dnTagMatterCASEAuth)},
		"extensions as a structure": {"extensions": put(func(e *tlv.Encoder) {
			e.StartStruct(tlv.ContextTag(tagExtensions))
			_ = e.EndContainer()
		})},
		"extensions as an array": {"extensions": put(func(e *tlv.Encoder) {
			e.StartArray(tlv.ContextTag(tagExtensions))
			_ = e.EndContainer()
		})},
		"extensions as an integer": {"extensions": put(func(e *tlv.Encoder) { e.PutUint(tlv.ContextTag(tagExtensions), 0) })},
		"basic constraints as a list": {"bc": put(func(e *tlv.Encoder) {
			e.StartList(tlv.ContextTag(extTagBasicConstraints))
			_ = e.EndContainer()
		})},
		"isCA as an integer": {"bc": put(func(e *tlv.Encoder) {
			e.StartStruct(tlv.ContextTag(extTagBasicConstraints))
			e.PutUint(tlv.ContextTag(extBCInnerTagIsCa), 1)
			_ = e.EndContainer()
		})},
		"pathLen as a signed integer": {"bc": put(func(e *tlv.Encoder) {
			e.StartStruct(tlv.ContextTag(extTagBasicConstraints))
			e.PutBool(tlv.ContextTag(extBCInnerTagIsCa), true)
			e.PutInt(tlv.ContextTag(extBCInnerTagPathLen), 0)
			_ = e.EndContainer()
		})},
		"key usage above 16 bits": {"ku": put(func(e *tlv.Encoder) { e.PutUint(tlv.ContextTag(extTagKeyUsage), 0x1_0001) })},
		"key usage as UTF-8":      {"ku": utf8(extTagKeyUsage)},
		"key usage twice": {"ku": put(func(e *tlv.Encoder) {
			e.PutUint(tlv.ContextTag(extTagKeyUsage), 0x01)
			e.PutUint(tlv.ContextTag(extTagKeyUsage), 0x20)
		})},
		"extended key usage as a list": {"ext+": put(func(e *tlv.Encoder) {
			e.StartList(tlv.ContextTag(extTagExtendedKeyUsage))
			e.PutUint(tlv.AnonymousTag(), 2)
			_ = e.EndContainer()
		})},
		"extended key usage entry above 8 bits": {"ext+": put(func(e *tlv.Encoder) {
			e.StartArray(tlv.ContextTag(extTagExtendedKeyUsage))
			e.PutUint(tlv.AnonymousTag(), 0x102)
			_ = e.EndContainer()
		})},
		"extended key usage entry as UTF-8": {"ext+": put(func(e *tlv.Encoder) {
			e.StartArray(tlv.ContextTag(extTagExtendedKeyUsage))
			e.PutUTF8(tlv.AnonymousTag(), "2")
			_ = e.EndContainer()
		})},
		"extended key usage twice": {"ext+": put(func(e *tlv.Encoder) {
			for range 2 {
				e.StartArray(tlv.ContextTag(extTagExtendedKeyUsage))
				e.PutUint(tlv.AnonymousTag(), 2)
				_ = e.EndContainer()
			}
		})},
		"subject key id as an integer": {"skid": put(func(e *tlv.Encoder) { e.PutUint(tlv.ContextTag(extTagSubjectKeyID), 1) })},
		"subject key id twice": {"skid": put(func(e *tlv.Encoder) {
			e.PutOctets(tlv.ContextTag(extTagSubjectKeyID), []byte{1})
			e.PutOctets(tlv.ContextTag(extTagSubjectKeyID), []byte{2})
		})},
		"authority key id twice": {"akid": put(func(e *tlv.Encoder) {
			e.PutOctets(tlv.ContextTag(extTagAuthorityKeyID), []byte{1})
			e.PutOctets(tlv.ContextTag(extTagAuthorityKeyID), []byte{2})
		})},
		"subject key id of 19 bytes":     {"skid": put(func(e *tlv.Encoder) { e.PutOctets(tlv.ContextTag(extTagSubjectKeyID), make([]byte, 19)) })},
		"subject key id of 1 byte":       {"skid": put(func(e *tlv.Encoder) { e.PutOctets(tlv.ContextTag(extTagSubjectKeyID), []byte{1}) })},
		"authority key id of 21 bytes":   {"akid": put(func(e *tlv.Encoder) { e.PutOctets(tlv.ContextTag(extTagAuthorityKeyID), make([]byte, 21)) })},
		"basic constraints absent":       {"bc": nil},
		"key usage absent":               {"ku": nil},
		"subject key id absent":          {"skid": nil},
		"authority key id absent":        {"akid": nil},
		"extensions absent":              {"extensions": nil},
		"future extension as an integer": {"ext+": put(func(e *tlv.Encoder) { e.PutUint(tlv.ContextTag(extTagFutureExtension), 1) })},
		"basic constraints twice": {"bc": put(func(e *tlv.Encoder) {
			for range 2 {
				e.StartStruct(tlv.ContextTag(extTagBasicConstraints))
				e.PutBool(tlv.ContextTag(extBCInnerTagIsCa), false)
				_ = e.EndContainer()
			}
		})},
	}
	for name, over := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			c, err := Decode(shapeCert(t, over))
			if err == nil {
				t.Fatalf("decoded (subject %+v, extensions %+v), want a refusal", c.Subject, c.Extensions)
			}
			if !errors.Is(err, ErrMalformed) && !errors.Is(err, ErrTruncated) {
				t.Errorf("err = %v, want ErrMalformed", err)
			}
		})
	}
}

// TestDecodeTakesWellFormedOptionalExtensions is the positive half of the
// table above: every extension at its own type, a future extension given
// twice (the one extension that may repeat) and an integer member encoded
// wider than its value needs all decode, so the type and width checks
// refuse only what the field cannot hold.
func TestDecodeTakesWellFormedOptionalExtensions(t *testing.T) {
	t.Parallel()
	raw := shapeCert(t, map[string]func(e *tlv.Encoder){
		"sigAlgo": func(e *tlv.Encoder) { e.PutUintWidth(tlv.ContextTag(tagSignatureAlgorithm), SigAlgoECDSAWithSHA256, 4) },
		"subject+": func(e *tlv.Encoder) {
			e.PutUint(tlv.ContextTag(dnTagMatterCASEAuth), 0xFFFF_0001)
		},
		"ext+": func(e *tlv.Encoder) {
			e.StartArray(tlv.ContextTag(extTagExtendedKeyUsage))
			e.PutUint(tlv.AnonymousTag(), 2)
			e.PutUint(tlv.AnonymousTag(), 1)
			_ = e.EndContainer()
		},
		"akid": func(e *tlv.Encoder) {
			e.PutOctets(tlv.ContextTag(extTagAuthorityKeyID), make([]byte, keyIDSize))
			e.PutOctets(tlv.ContextTag(extTagFutureExtension), []byte{0x30, 0x00})
			e.PutOctets(tlv.ContextTag(extTagFutureExtension), []byte{0x30, 0x01})
		},
	})
	c, err := Decode(raw)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got := c.Subject.CASEAuthTags; len(got) != 1 || got[0] != 0xFFFF_0001 {
		t.Errorf("CASEAuthTags = %#x, want [0xffff0001]", got)
	}
	if got := len(c.Extensions.FutureExtensions); got != 2 {
		t.Errorf("FutureExtensions = %d, want 2", got)
	}
	if got := c.Extensions.ExtendedKeyUsage; len(got) != 2 || got[0] != 2 || got[1] != 1 {
		t.Errorf("ExtendedKeyUsage = %v, want [2 1]", got)
	}
}

// TestDecodeRefusesATaggedExtendedKeyUsageEntry holds the anonymous-element
// rule of the extended key usage array: an entry that carries a context
// tag is refused, as matter.js's array decoder refuses it
// (packages/types/src/tlv/TlvArray.ts:78 at b25ed960, "Array element tags
// should be anonymous"). The encoder cannot write such an entry, so the
// test writes the anonymous one and patches its control byte and tag in;
// the unpatched certificate is the control.
func TestDecodeRefusesATaggedExtendedKeyUsageEntry(t *testing.T) {
	t.Parallel()
	raw := shapeCert(t, map[string]func(e *tlv.Encoder){"ext+": func(e *tlv.Encoder) {
		e.StartArray(tlv.ContextTag(extTagExtendedKeyUsage))
		e.PutUint(tlv.AnonymousTag(), 0x02)
		_ = e.EndContainer()
	}})
	if _, err := Decode(raw); err != nil {
		t.Fatalf("control: the anonymous entry is refused: %v", err)
	}
	anonymous := []byte{0x36, extTagExtendedKeyUsage, 0x04, 0x02, 0x18}
	tagged := []byte{0x36, extTagExtendedKeyUsage, 0x24, 0x01, 0x02, 0x18}
	if bytes.Count(raw, anonymous) != 1 {
		t.Fatalf("the extended key usage array is not encoded as % X", anonymous)
	}
	if _, err := Decode(bytes.Replace(raw, anonymous, tagged, 1)); !errors.Is(err, ErrMalformed) {
		t.Fatalf("a context-tagged entry: %v, want ErrMalformed", err)
	}
}

// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package mattercert_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"errors"
	"testing"

	"github.com/SukramJ/go-fabric/secure/mattercert"
	"github.com/SukramJ/go-fabric/tlv"
)

// The DN rules of the matter.js pin db021da0 at the decoder: a
// single-valued Matter attribute occurs at most once (TlvObject.ts:230,
// DistinguishedName.ts:242-246 at db021da0 — "Tagged list contains field
// … more than once"), and matter-vvs-id (tag 23) is decoded in every
// operational certificate (definitions/operational.ts:64), so the NOC,
// ICAC and RCAC checks can refuse it.
func TestDecodeDNRulesPinDB021da0(t *testing.T) {
	t.Parallel()
	build := func(subject func(e *tlv.Encoder)) []byte {
		enc := tlv.NewEncoder()
		enc.StartStruct(tlv.AnonymousTag())
		enc.PutOctets(tlv.ContextTag(1), []byte{0x01})
		enc.PutUint(tlv.ContextTag(2), mattercert.SigAlgoECDSAWithSHA256)
		enc.StartList(tlv.ContextTag(3))
		enc.PutUint(tlv.ContextTag(20), uint64(0xFABFAB))
		_ = enc.EndContainer()
		enc.PutUint(tlv.ContextTag(4), uint64(0))
		enc.PutUint(tlv.ContextTag(5), uint64(0))
		enc.StartList(tlv.ContextTag(6))
		subject(enc)
		_ = enc.EndContainer()
		enc.PutUint(tlv.ContextTag(7), mattercert.PubKeyAlgoEC)
		enc.PutUint(tlv.ContextTag(8), mattercert.CurvePrime256v1)
		priv, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		enc.PutOctets(tlv.ContextTag(9), marshalPub(priv))
		enc.StartList(tlv.ContextTag(10))
		_ = enc.EndContainer()
		enc.PutOctets(tlv.ContextTag(11), make([]byte, 64))
		_ = enc.EndContainer()
		raw, err := enc.Bytes()
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	t.Run("a matter-vvs-id is decoded", func(t *testing.T) {
		t.Parallel()
		c, err := mattercert.Decode(build(func(e *tlv.Encoder) {
			e.PutUint(tlv.ContextTag(23), uint64(0x77))
		}))
		if err != nil {
			t.Fatal(err)
		}
		if !c.Subject.HasVVSID || c.Subject.MatterVVSID != 0x77 || len(c.Subject.Order) != 1 || c.Subject.Order[0] != 23 {
			t.Fatalf("subject = %+v", c.Subject)
		}
	})
	for _, tc := range []struct {
		name string
		tag  uint8
	}{{"node id", 17}, {"fabric id", 21}, {"icac id", 19}, {"rcac id", 20}, {"vvs id", 23}} {
		t.Run("a repeated "+tc.name+" is refused", func(t *testing.T) {
			t.Parallel()
			_, err := mattercert.Decode(build(func(e *tlv.Encoder) {
				e.PutUint(tlv.ContextTag(tc.tag), uint64(1))
				e.PutUint(tlv.ContextTag(tc.tag), uint64(2))
			}))
			if !errors.Is(err, mattercert.ErrMalformed) {
				t.Fatalf("err = %v, want ErrMalformed", err)
			}
		})
	}
	t.Run("a repeated CAT is not a repeated single-valued field", func(t *testing.T) {
		t.Parallel()
		if _, err := mattercert.Decode(build(func(e *tlv.Encoder) {
			e.PutUint(tlv.ContextTag(17), uint64(1))
			e.PutUint(tlv.ContextTag(22), uint64(0x00010001))
			e.PutUint(tlv.ContextTag(22), uint64(0x00020001))
		})); err != nil {
			t.Fatal(err)
		}
	})
}

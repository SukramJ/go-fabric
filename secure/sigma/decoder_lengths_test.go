// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package sigma

import (
	"bytes"
	"errors"
	"testing"

	"github.com/SukramJ/go-fabric/secure/aesccm"
	"github.com/SukramJ/go-fabric/tlv"
)

// sigmaField is one context-tagged member of a hand-built Sigma message:
// an octet string, or an unsigned integer when octets is nil.
type sigmaField struct {
	tag    uint8
	octets []byte
	u      uint64
}

func encodeSigmaFields(t *testing.T, fields []sigmaField) []byte {
	t.Helper()
	enc := tlv.NewEncoder()
	enc.StartStruct(tlv.AnonymousTag())
	for _, f := range fields {
		if f.octets != nil {
			enc.PutOctets(tlv.ContextTag(f.tag), f.octets)
		} else {
			enc.PutUint(tlv.ContextTag(f.tag), f.u)
		}
	}
	if err := enc.EndContainer(); err != nil {
		t.Fatal(err)
	}
	b, err := enc.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// withField returns fields with the member of tag replaced by f, or with it
// dropped when f is nil.
func withField(fields []sigmaField, tag uint8, f *sigmaField) []sigmaField {
	out := make([]sigmaField, 0, len(fields))
	for _, have := range fields {
		switch {
		case have.tag != tag:
			out = append(out, have)
		case f != nil:
			out = append(out, *f)
		}
	}
	return out
}

// TestSigmaDecodersRefuseWrongFieldLengths holds the length and
// mandatory-field checks of UnmarshalSigma1, UnmarshalSigma2 and
// UnmarshalSigma2Resume: every fixed-size field one byte short and one
// byte long, and every mandatory field of Sigma2 and of Sigma2_Resume
// dropped in turn, is a decode error (ErrSessionState); the unmodified
// message decodes. Sigma1's fields are checked for length only — the
// table drops none of them, so its missing-field returns are not held
// here. Without
// a check the decoders copy a short random into a zero-padded array or
// keep a key of the wrong size, and the failure surfaces — if at all — as
// a MIC or destination mismatch much later (R2-P5-tmut-16).
func TestSigmaDecodersRefuseWrongFieldLengths(t *testing.T) {
	t.Parallel()
	b := func(n int) []byte { return bytes.Repeat([]byte{0x5A}, n) }
	type decoder func([]byte) error
	msgs := []struct {
		name   string
		fields []sigmaField
		decode decoder
		sized  map[uint8]int // tag → required length
		needed []uint8       // mandatory tags checked by dropping them
	}{
		{
			name: "Sigma1",
			fields: []sigmaField{
				{tag: 1, octets: b(RandomSize)},
				{tag: 2, u: 7},
				{tag: 3, octets: b(32)},
				{tag: 4, octets: b(EphPubKeySize)},
				{tag: 6, octets: b(16)},
				{tag: 7, octets: b(16)},
			},
			decode: func(raw []byte) error { _, err := UnmarshalSigma1(raw); return err },
			sized:  map[uint8]int{1: RandomSize, 3: 32, 4: EphPubKeySize, 6: 16, 7: 16},
		},
		{
			name: "Sigma2",
			fields: []sigmaField{
				{tag: 1, octets: b(RandomSize)}, {tag: 2, u: 7}, {tag: 3, octets: b(EphPubKeySize)}, {tag: 4, octets: b(40)},
			},
			decode: func(raw []byte) error { _, err := UnmarshalSigma2(raw); return err },
			sized:  map[uint8]int{1: RandomSize, 3: EphPubKeySize},
			needed: []uint8{1, 2, 3, 4},
		},
		{
			name: "Sigma2_Resume",
			fields: []sigmaField{
				{tag: 1, octets: b(ResumptionIDSize)}, {tag: 2, octets: b(aesccm.TagSize)}, {tag: 3, u: 7},
			},
			decode: func(raw []byte) error { _, err := UnmarshalSigma2Resume(raw); return err },
			sized:  map[uint8]int{1: ResumptionIDSize, 2: aesccm.TagSize},
			needed: []uint8{1, 2, 3},
		},
	}
	for _, m := range msgs {
		if err := m.decode(encodeSigmaFields(t, m.fields)); err != nil {
			t.Fatalf("%s: the well-formed message is refused: %v", m.name, err)
		}
		for tag, size := range m.sized {
			for _, n := range []int{size - 1, size + 1} {
				raw := encodeSigmaFields(t, withField(m.fields, tag, &sigmaField{tag: tag, octets: b(n)}))
				if err := m.decode(raw); !errors.Is(err, ErrSessionState) {
					t.Errorf("%s: tag %d with %d bytes (want %d): err = %v, want ErrSessionState", m.name, tag, n, size, err)
				}
			}
		}
		for _, tag := range m.needed {
			if err := m.decode(encodeSigmaFields(t, withField(m.fields, tag, nil))); !errors.Is(err, ErrSessionState) {
				t.Errorf("%s: without tag %d: err = %v, want ErrSessionState", m.name, tag, err)
			}
		}
	}
}

// TestSigma1RefusesMalformedSessionParameters: Sigma1's
// initiatorSessionParams are decoded by the strict decoder the PASE
// messages use ([sessionparams.DecodeFields]); a wrong-typed or
// out-of-range field refuses the Sigma1 with ErrSessionState, as any
// other malformed Sigma1 member, and a well-formed structure is read.
func TestSigma1RefusesMalformedSessionParameters(t *testing.T) {
	t.Parallel()
	sigma1 := func(params func(e *tlv.Encoder)) []byte {
		e := tlv.NewEncoder()
		e.StartStruct(tlv.AnonymousTag())
		e.PutOctets(tlv.ContextTag(1), make([]byte, RandomSize))
		e.PutUint16(tlv.ContextTag(2), 7)
		e.PutOctets(tlv.ContextTag(3), make([]byte, 32))
		e.PutOctets(tlv.ContextTag(4), make([]byte, EphPubKeySize))
		e.StartStruct(tlv.ContextTag(5))
		params(e)
		_ = e.EndContainer()
		_ = e.EndContainer()
		b, err := e.Bytes()
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	for name, params := range map[string]func(e *tlv.Encoder){
		"signed SessionIdleInterval":       func(e *tlv.Encoder) { e.PutInt32(tlv.ContextTag(1), 500) },
		"SessionActiveThreshold > 16 bits": func(e *tlv.Encoder) { e.PutUint32(tlv.ContextTag(3), 0x10000) },
		"string MaxPathsPerInvoke":         func(e *tlv.Encoder) { e.PutUTF8(tlv.ContextTag(7), "1") },
	} {
		if _, err := UnmarshalSigma1(sigma1(params)); !errors.Is(err, ErrSessionState) {
			t.Errorf("%s: err = %v, want ErrSessionState", name, err)
		}
	}
	s, err := UnmarshalSigma1(sigma1(func(e *tlv.Encoder) { e.PutUint64(tlv.ContextTag(1), 500) }))
	if err != nil {
		t.Fatal(err)
	}
	if s.InitiatorSessionParams == nil || s.InitiatorSessionParams.SessionIdleInterval != 500 {
		t.Fatalf("InitiatorSessionParams = %+v, want SessionIdleInterval 500", s.InitiatorSessionParams)
	}
}

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

// sigmaDecoderCases is every decoder of a CASE message or of an encrypted
// payload, each with one well-formed encoding. It is the table the shape
// tests below iterate: a decoder added to the package belongs here, so the
// top-level rule (a structure, nothing else) holds for all of them.
func sigmaDecoderCases() []struct {
	name   string
	decode func([]byte) error
	good   []byte
} {
	var s1 Sigma1
	s1.InitiatorSessionID = 0x1234
	s1.InitiatorEphPubKey = bytes.Repeat([]byte{0x04}, EphPubKeySize)
	var s2 Sigma2
	s2.ResponderSessionID = 0x2345
	s2.ResponderEphPubKey = bytes.Repeat([]byte{0x04}, EphPubKeySize)
	s2.Encrypted2 = []byte{0x01, 0x02}
	s2r := Sigma2Resume{
		ResumptionID:       make([]byte, ResumptionIDSize),
		Sigma2ResumeMIC:    make([]byte, aesccm.TagSize),
		ResponderSessionID: 0x3456,
	}
	return []struct {
		name   string
		decode func([]byte) error
		good   []byte
	}{
		{"Sigma1", func(b []byte) error { _, err := UnmarshalSigma1(b); return err }, s1.Marshal()},
		{"Sigma2", func(b []byte) error { _, err := UnmarshalSigma2(b); return err }, s2.Marshal()},
		{"Sigma2_Resume", func(b []byte) error { _, err := UnmarshalSigma2Resume(b); return err }, MarshalSigma2Resume(s2r)},
		{"Sigma3", func(b []byte) error { _, err := UnmarshalSigma3(b); return err }, Sigma3{Encrypted3: []byte{0x01}}.Marshal()},
		{"TBE2", func(b []byte) error { _, err := unmarshalTBE2(b); return err }, marshalTBE2(TBE2Plaintext{
			ResponderNOC: []byte{0x01}, Signature: make([]byte, 64), ResumptionID: make([]byte, ResumptionIDSize),
		})},
		{"TBE3", func(b []byte) error { _, err := unmarshalTBE3(b); return err }, marshalTBE3(TBE3Plaintext{
			InitiatorNOC: []byte{0x01}, Signature: make([]byte, 64),
		})},
	}
}

// TestSigmaDecodersRefuseANonStructureTopLevel holds R2-P6-wire-0101: every
// CASE message and both encrypted payloads are an anonymous structure, and
// an Array (0x16) or a List (0x17) at the top level is refused rather than
// read as if it were one. The well-formed encoding of each decoder is the
// control: it decodes, so the refusal comes from the container type alone.
func TestSigmaDecodersRefuseANonStructureTopLevel(t *testing.T) {
	t.Parallel()
	for _, c := range sigmaDecoderCases() {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if c.good[0] != byte(tlv.TypeStructure) {
				t.Fatalf("control: first byte 0x%02X, want an anonymous structure", c.good[0])
			}
			if err := c.decode(c.good); err != nil {
				t.Fatalf("control: the well-formed encoding is refused: %v", err)
			}
			for _, ct := range []tlv.ElementType{tlv.TypeArray, tlv.TypeList} {
				b := append([]byte(nil), c.good...)
				b[0] = byte(ct)
				if err := c.decode(b); err == nil {
					t.Errorf("top-level container 0x%02X decoded, want a refusal", byte(ct))
				}
			}
		})
	}
}

// tbe2Fields encodes a TBE2-shaped structure from the given members; a nil
// member is left out, so a case can drop a mandatory one.
func tbe2Fields(t *testing.T, put func(e *tlv.Encoder)) []byte {
	t.Helper()
	e := tlv.NewEncoder()
	e.StartStruct(tlv.AnonymousTag())
	put(e)
	if err := e.EndContainer(); err != nil {
		t.Fatal(err)
	}
	b, err := e.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// TestTBE2RefusesAMissingOrMalformedMandatoryField holds R2-P6-wire-0102 for
// the Sigma2 payload: the responder's NOC (tag 1), its signature (tag 3, 64
// bytes) and the resumption id (tag 4, 16 bytes) are mandatory octet
// strings. A TBE2 without one, with one of another length, or with one of
// another TLV type is refused; before, each decoded and the initiator kept
// whatever resumption id it found.
func TestTBE2RefusesAMissingOrMalformedMandatoryField(t *testing.T) {
	t.Parallel()
	noc := func(e *tlv.Encoder) { e.PutOctets(tlv.ContextTag(1), []byte{0x01, 0x02}) }
	sig := func(e *tlv.Encoder) { e.PutOctets(tlv.ContextTag(3), make([]byte, 64)) }
	rid := func(n int) func(e *tlv.Encoder) {
		return func(e *tlv.Encoder) { e.PutOctets(tlv.ContextTag(4), make([]byte, n)) }
	}
	all := func(fs ...func(e *tlv.Encoder)) func(e *tlv.Encoder) {
		return func(e *tlv.Encoder) {
			for _, f := range fs {
				f(e)
			}
		}
	}
	good := tbe2Fields(t, all(noc, sig, rid(16)))
	if _, err := unmarshalTBE2(good); err != nil {
		t.Fatalf("control: a complete TBE2 is refused: %v", err)
	}
	cases := map[string][]byte{
		"resumption id absent":          tbe2Fields(t, all(noc, sig)),
		"resumption id of 0 bytes":      tbe2Fields(t, all(noc, sig, rid(0))),
		"resumption id of 5 bytes":      tbe2Fields(t, all(noc, sig, rid(5))),
		"resumption id of 40 bytes":     tbe2Fields(t, all(noc, sig, rid(40))),
		"resumption id as UTF-8 string": tbe2Fields(t, all(noc, sig, func(e *tlv.Encoder) { e.PutUTF8(tlv.ContextTag(4), "0123456789abcdef") })),
		"NOC absent":                    tbe2Fields(t, all(sig, rid(16))),
		"NOC as unsigned integer":       tbe2Fields(t, all(func(e *tlv.Encoder) { e.PutUint(tlv.ContextTag(1), 7) }, sig, rid(16))),
		"signature absent":              tbe2Fields(t, all(noc, rid(16))),
		"signature of 63 bytes":         tbe2Fields(t, all(noc, func(e *tlv.Encoder) { e.PutOctets(tlv.ContextTag(3), make([]byte, 63)) }, rid(16))),
		"ICAC as unsigned integer":      tbe2Fields(t, all(noc, func(e *tlv.Encoder) { e.PutUint(tlv.ContextTag(2), 7) }, sig, rid(16))),
		"NOC as a structure":            tbe2Fields(t, all(func(e *tlv.Encoder) { e.StartStruct(tlv.ContextTag(1)); _ = e.EndContainer() }, sig, rid(16))),
	}
	for name, b := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := unmarshalTBE2(b); err == nil {
				t.Error("decoded, want a refusal")
			}
		})
	}
}

// TestTBE3RefusesAMissingOrMalformedMandatoryField holds R2-P6-wire-0102 for
// the Sigma3 payload: the initiator's NOC (tag 1) and its signature (tag 3,
// 64 bytes) are mandatory octet strings.
func TestTBE3RefusesAMissingOrMalformedMandatoryField(t *testing.T) {
	t.Parallel()
	noc := func(e *tlv.Encoder) { e.PutOctets(tlv.ContextTag(1), []byte{0x01, 0x02}) }
	sig := func(e *tlv.Encoder) { e.PutOctets(tlv.ContextTag(3), make([]byte, 64)) }
	if _, err := unmarshalTBE3(tbe2Fields(t, func(e *tlv.Encoder) { noc(e); sig(e) })); err != nil {
		t.Fatalf("control: a complete TBE3 is refused: %v", err)
	}
	cases := map[string][]byte{
		"NOC absent":               tbe2Fields(t, sig),
		"signature absent":         tbe2Fields(t, noc),
		"signature of 65 bytes":    tbe2Fields(t, func(e *tlv.Encoder) { noc(e); e.PutOctets(tlv.ContextTag(3), make([]byte, 65)) }),
		"signature as UTF-8":       tbe2Fields(t, func(e *tlv.Encoder) { noc(e); e.PutUTF8(tlv.ContextTag(3), string(make([]byte, 64))) }),
		"ICAC as unsigned integer": tbe2Fields(t, func(e *tlv.Encoder) { noc(e); e.PutUint(tlv.ContextTag(2), 1); sig(e) }),
	}
	for name, b := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := unmarshalTBE3(b); err == nil {
				t.Error("decoded, want a refusal")
			}
		})
	}
}

// TestInitiatorRefusesASigma2WithoutAResumptionID is R2-P6-wire-0102's
// consequence: a responder that holds the fabric's keys and signs the
// transcript correctly, but leaves the resumption id out of TBE2, does not
// get a finished handshake — the initiator used to finish and keep an empty
// id the store then refused. The control re-seals the same TBE2 with the id
// and finishes, so only the missing member refuses.
func TestInitiatorRefusesASigma2WithoutAResumptionID(t *testing.T) {
	t.Parallel()
	ipk := fabricIPK()
	run := func(t *testing.T, withID bool) error {
		t.Helper()
		initID := newTestIdentity(t, 0xAAAA, 1, ipk)
		respID := newTestIdentity(t, 0xBBBB, 1, ipk)
		initiator := NewInitiator(initID, testVerifier{}, 0x1001, [32]byte{0x01})
		responder := NewResponder(respID, testVerifier{}, 0x2001)
		s1, err := initiator.GenerateSigma1()
		if err != nil {
			t.Fatal(err)
		}
		sigma2, err := responder.ProcessSigma1(s1)
		if err != nil {
			t.Fatal(err)
		}
		parsed, err := UnmarshalSigma1(s1)
		if err != nil {
			t.Fatal(err)
		}
		sig, err := signTranscript(respID.PrivateKey, respID.NOC, respID.ICAC, sigma2.ResponderEphPubKey, parsed.InitiatorEphPubKey)
		if err != nil {
			t.Fatal(err)
		}
		plain := TBE2Plaintext{ResponderNOC: respID.NOC, ResponderICAC: respID.ICAC, Signature: sig}
		if withID {
			plain.ResumptionID = make([]byte, ResumptionIDSize)
		}
		var body []byte
		if withID {
			body = marshalTBE2(plain)
		} else {
			body = tbe2Fields(t, func(e *tlv.Encoder) {
				e.PutOctets(tlv.ContextTag(1), plain.ResponderNOC)
				if len(plain.ResponderICAC) > 0 {
					e.PutOctets(tlv.ContextTag(2), plain.ResponderICAC)
				}
				e.PutOctets(tlv.ContextTag(3), plain.Signature)
			})
		}
		s2k, err := hkdfDerive(responder.ECDHSharedSecret(), sigma2Salt(ipk[:], sigma2.ResponderRandom[:], sigma2.ResponderEphPubKey, s1), HKDFInfoSigma2, SessionKeySize)
		if err != nil {
			t.Fatal(err)
		}
		c, err := aesccm.New(s2k)
		if err != nil {
			t.Fatal(err)
		}
		sigma2.Encrypted2, err = c.Seal(nil, nonceTBE2, body, nil)
		if err != nil {
			t.Fatal(err)
		}
		s3, err := initiator.ProcessSigma2(sigma2)
		if err == nil && s3 == nil {
			t.Fatal("no error and no Sigma3")
		}
		if err != nil {
			if _, ok := initiator.SessionKeys(); ok {
				t.Error("SessionKeys available after a refused Sigma2")
			}
		}
		return err
	}
	if err := run(t, true); err != nil {
		t.Fatalf("control: a TBE2 with its resumption id is refused: %v", err)
	}
	if err := run(t, false); err == nil {
		t.Fatal("a TBE2 without a resumption id finished the handshake")
	} else if errors.Is(err, ErrSignatureInvalid) {
		t.Fatalf("refused for the signature, not the missing member: %v", err)
	}
}

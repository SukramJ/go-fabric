// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package spake2_test

import (
	"bytes"
	"errors"
	"testing"

	"github.com/SukramJ/go-fabric/secure/spake2"
	"github.com/SukramJ/go-fabric/tlv"
)

// wireMember is one member of a hand-built PASE message.
type wireMember struct {
	tag    tlv.Tag
	octets []byte
	u      uint64
	flag   *bool // a boolean member when set
}

// encodeWire encodes members in a container that is a structure, or a
// list when asList is set — a list may carry context-tagged members too,
// so only the decoder's top-level type check tells the two apart.
func encodeWire(t *testing.T, asList bool, members ...wireMember) []byte {
	t.Helper()
	enc := tlv.NewEncoder()
	if asList {
		enc.StartList(tlv.AnonymousTag())
	} else {
		enc.StartStruct(tlv.AnonymousTag())
	}
	for _, m := range members {
		switch {
		case m.flag != nil:
			enc.PutBool(m.tag, *m.flag)
		case m.octets != nil:
			enc.PutOctets(m.tag, m.octets)
		default:
			enc.PutUint(m.tag, m.u)
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

// TestPASEDecodersAreStrictAboutShape holds the shape checks of the PASE
// decoders a device runs on what a commissioner sends — PBKDFParamRequest,
// Pake1, Pake3 — and of Pake2: the top level must be a structure (a list
// with the same members is refused), and only context-tagged members
// count, so a profile-tagged member, or a context member of another tag,
// that shares a field's number is ignored rather than merged into the
// field (R2-P5-tmut-16). A PBKDFParamRequest needs all four of its
// mandatory members — matter.js's TlvPbkdfParamRequest declares tags 1-4
// TlvFields (packages/protocol/src/session/pase/PaseMessages.ts:70-73 at
// db021da0) — so one without PasscodeID or HasPBKDFParameters is refused.
func TestPASEDecodersAreStrictAboutShape(t *testing.T) {
	t.Parallel()
	b := func(n int) []byte { return bytes.Repeat([]byte{0x5A}, n) }
	ctx := tlv.ContextTag
	profile := tlv.CommonTag(1)
	type decoder func([]byte) error
	pake1 := func(raw []byte) error { _, err := spake2.DecodePake1(raw); return err }
	pake2 := func(raw []byte) error { _, _, err := spake2.DecodePake2(raw); return err }
	pake3 := func(raw []byte) error { _, err := spake2.DecodePake3(raw); return err }
	request := func(raw []byte) error { _, err := spake2.DecodePBKDFParamRequest(raw); return err }
	no := false
	random, session, passcode, hasParams := wireMember{tag: ctx(1), octets: b(spake2.PBKDFRandomSize)}, wireMember{tag: ctx(2), u: 7}, wireMember{tag: ctx(3), u: 0}, wireMember{tag: ctx(4), flag: &no}
	for _, tc := range []struct {
		name    string
		decode  decoder
		asList  bool
		members []wireMember
		ok      bool
	}{
		{"Pake1", pake1, false, []wireMember{{tag: ctx(1), octets: b(spake2.PointSize)}}, true},
		{"Pake1 as a list", pake1, true, []wireMember{{tag: ctx(1), octets: b(spake2.PointSize)}}, false},
		{"Pake1 with a profile-tagged member numbered 1", pake1, false, []wireMember{{tag: ctx(1), octets: b(spake2.PointSize)}, {tag: profile, octets: b(4)}}, true},
		{"Pake1 with a context member of another tag", pake1, false, []wireMember{{tag: ctx(1), octets: b(spake2.PointSize)}, {tag: ctx(2), octets: b(4)}}, true},
		{"Pake3", pake3, false, []wireMember{{tag: ctx(1), octets: b(spake2.ConfirmTagSize)}}, true},
		{"Pake3 as a list", pake3, true, []wireMember{{tag: ctx(1), octets: b(spake2.ConfirmTagSize)}}, false},
		{"Pake2", pake2, false, []wireMember{{tag: ctx(1), octets: b(spake2.PointSize)}, {tag: ctx(2), octets: b(spake2.ConfirmTagSize)}}, true},
		{"Pake2 as a list", pake2, true, []wireMember{{tag: ctx(1), octets: b(spake2.PointSize)}, {tag: ctx(2), octets: b(spake2.ConfirmTagSize)}}, false},
		{"Pake2 with a profile-tagged member numbered 1", pake2, false, []wireMember{{tag: ctx(1), octets: b(spake2.PointSize)}, {tag: ctx(2), octets: b(spake2.ConfirmTagSize)}, {tag: profile, octets: b(4)}}, true},
		{"PBKDFParamRequest", request, false, []wireMember{random, session, passcode, hasParams}, true},
		{"PBKDFParamRequest as a list", request, true, []wireMember{random, session, passcode, hasParams}, false},
		{"PBKDFParamRequest with a profile-tagged member numbered 1", request, false, []wireMember{random, {tag: profile, octets: b(4)}, session, passcode, hasParams}, true},
		{"PBKDFParamRequest without HasPBKDFParameters", request, false, []wireMember{random, session, passcode}, false},
		{"PBKDFParamRequest without PasscodeID and HasPBKDFParameters", request, false, []wireMember{random, session}, false},
	} {
		err := tc.decode(encodeWire(t, tc.asList, tc.members...))
		switch {
		case tc.ok && err != nil:
			t.Errorf("%s: refused: %v", tc.name, err)
		case !tc.ok && !errors.Is(err, spake2.ErrWireMalformed):
			t.Errorf("%s: err = %v, want ErrWireMalformed", tc.name, err)
		}
	}
}

// TestPBKDFParamResponseFieldsAreTyped: the commissioner's decoder of a
// PBKDFParamResponse refuses a response without ResponderSessionID (tag
// 3) or with one above 0xFFFF, and an Iterations above 0xFFFFFFFF that
// would mask into the bounds — matter.js's TlvPbkdfParamResponse makes
// ResponderSessionID a mandatory TlvUInt16 and Iterations a bounded
// TlvUInt32 (packages/protocol/src/session/pase/PaseMessages.ts:83,
// :87-90 at db021da0). The well-formed response decodes (the control).
func TestPBKDFParamResponseFieldsAreTyped(t *testing.T) {
	t.Parallel()
	random := bytes.Repeat([]byte{0x5A}, spake2.PBKDFRandomSize)
	encode := func(sessionID *uint64, iterations uint64) []byte {
		enc := tlv.NewEncoder()
		enc.StartStruct(tlv.AnonymousTag())
		enc.PutOctets(tlv.ContextTag(1), random)
		enc.PutOctets(tlv.ContextTag(2), random)
		if sessionID != nil {
			enc.PutUint(tlv.ContextTag(3), *sessionID)
		}
		enc.StartStruct(tlv.ContextTag(4))
		enc.PutUint(tlv.ContextTag(1), iterations)
		enc.PutOctets(tlv.ContextTag(2), bytes.Repeat([]byte{0x01}, spake2.PBKDFMinSaltSize))
		if err := enc.EndContainer(); err != nil {
			t.Fatal(err)
		}
		if err := enc.EndContainer(); err != nil {
			t.Fatal(err)
		}
		out, err := enc.Bytes()
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	id := func(v uint64) *uint64 { return &v }
	const iterations = uint64(spake2.PBKDFMinIterations)
	if _, err := spake2.DecodePBKDFParamResponse(encode(id(9), iterations)); err != nil {
		t.Fatalf("well-formed response refused: %v", err)
	}
	for _, c := range []struct {
		name string
		raw  []byte
	}{
		{"without ResponderSessionID", encode(nil, iterations)},
		{"a ResponderSessionID above 0xFFFF", encode(id(0x10000), iterations)},
		{"an Iterations above 0xFFFFFFFF", encode(id(9), 1<<32|iterations)},
	} {
		if _, err := spake2.DecodePBKDFParamResponse(c.raw); !errors.Is(err, spake2.ErrWireMalformed) {
			t.Errorf("%s: err = %v, want ErrWireMalformed", c.name, err)
		}
	}
}

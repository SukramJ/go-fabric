// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package spake2_test

import (
	"bytes"
	"errors"
	"fmt"
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
	// structure / list make the member a container holding nested.
	structure, list bool
	nested          []wireMember
}

// putWire encodes one member, recursing into a container member.
func putWire(t *testing.T, enc *tlv.Encoder, m wireMember) {
	t.Helper()
	switch {
	case m.structure || m.list:
		if m.list {
			enc.StartList(m.tag)
		} else {
			enc.StartStruct(m.tag)
		}
		for _, n := range m.nested {
			putWire(t, enc, n)
		}
		if err := enc.EndContainer(); err != nil {
			t.Fatal(err)
		}
	case m.flag != nil:
		enc.PutBool(m.tag, *m.flag)
	case m.octets != nil:
		enc.PutOctets(m.tag, m.octets)
	default:
		enc.PutUint(m.tag, m.u)
	}
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
		putWire(t, enc, m)
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
// Pake1, Pake3 — and of Pake2 and PBKDFParamResponse: the top level must
// be a structure (a list with the same members is refused); a context
// member of a tag the message does not declare is skipped rather than
// merged into a field (R2-P5-tmut-16); and a member whose tag is not
// context-specific — anonymous, common-profile or fully-qualified,
// numbered like a field or not, before or after the declared members, at
// the top level or inside the nested PBKDFParameters — is refused, as
// matter.js's TlvObject decode throws for it
// (packages/types/src/tlv/TlvObject.ts:212-214 at b25ed960; an implicit
// tag matter.js's codec already throws for, TlvCodec.ts:169-171), while inside
// a skipped unknown member the tag forms are not looked at
// (packages/types/src/tlv/TlvAny.ts:87-90; an implicit-profile tag is
// refused by the tlv decoder wherever it stands) (R2-P5 Fable F1). A
// PBKDFParamRequest needs all four of its mandatory members — matter.js's
// TlvPbkdfParamRequest declares tags 1-4 TlvFields
// (packages/protocol/src/session/pase/PaseMessages.ts:70-73 at b25ed960) —
// so one without PasscodeID or HasPBKDFParameters is refused.
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
	response := func(raw []byte) error { _, err := spake2.DecodePBKDFParamResponse(raw); return err }
	no := false
	random, session, passcode, hasParams := wireMember{tag: ctx(1), octets: b(spake2.PBKDFRandomSize)}, wireMember{tag: ctx(2), u: 7}, wireMember{tag: ctx(3), u: 0}, wireMember{tag: ctx(4), flag: &no}
	type tableCase struct {
		name    string
		decode  decoder
		asList  bool
		members []wireMember
		ok      bool
	}
	cases := []tableCase{
		{"Pake1", pake1, false, []wireMember{{tag: ctx(1), octets: b(spake2.PointSize)}}, true},
		{"Pake1 as a list", pake1, true, []wireMember{{tag: ctx(1), octets: b(spake2.PointSize)}}, false},
		{"Pake1 with a profile-tagged member numbered 1", pake1, false, []wireMember{{tag: ctx(1), octets: b(spake2.PointSize)}, {tag: profile, octets: b(4)}}, false},
		{"Pake1 with a context member of another tag", pake1, false, []wireMember{{tag: ctx(1), octets: b(spake2.PointSize)}, {tag: ctx(2), octets: b(4)}}, true},
		{"Pake3", pake3, false, []wireMember{{tag: ctx(1), octets: b(spake2.ConfirmTagSize)}}, true},
		{"Pake3 as a list", pake3, true, []wireMember{{tag: ctx(1), octets: b(spake2.ConfirmTagSize)}}, false},
		{"Pake2", pake2, false, []wireMember{{tag: ctx(1), octets: b(spake2.PointSize)}, {tag: ctx(2), octets: b(spake2.ConfirmTagSize)}}, true},
		{"Pake2 as a list", pake2, true, []wireMember{{tag: ctx(1), octets: b(spake2.PointSize)}, {tag: ctx(2), octets: b(spake2.ConfirmTagSize)}}, false},
		{"Pake2 with a profile-tagged member numbered 1", pake2, false, []wireMember{{tag: ctx(1), octets: b(spake2.PointSize)}, {tag: ctx(2), octets: b(spake2.ConfirmTagSize)}, {tag: profile, octets: b(4)}}, false},
		{"PBKDFParamRequest", request, false, []wireMember{random, session, passcode, hasParams}, true},
		{"PBKDFParamRequest as a list", request, true, []wireMember{random, session, passcode, hasParams}, false},
		{"PBKDFParamRequest with a profile-tagged member numbered 1", request, false, []wireMember{random, {tag: profile, octets: b(4)}, session, passcode, hasParams}, false},
		{"PBKDFParamRequest with a context member numbered 0", request, false, []wireMember{random, session, passcode, hasParams, {tag: ctx(0), u: 1}}, true},
		{"PBKDFParamRequest without HasPBKDFParameters", request, false, []wireMember{random, session, passcode}, false},
		{"PBKDFParamRequest without PasscodeID and HasPBKDFParameters", request, false, []wireMember{random, session}, false},
	}

	// Every tag form other than context-specific, at every declared
	// position (R2-P5 Fable F1).
	pbkdfParams := func(nested ...wireMember) wireMember {
		return wireMember{tag: ctx(4), structure: true, nested: nested}
	}
	iterations := wireMember{tag: ctx(1), u: uint64(spake2.PBKDFMinIterations)}
	salt := wireMember{tag: ctx(2), octets: b(spake2.PBKDFMinSaltSize)}
	respFields := []wireMember{{tag: ctx(1), octets: b(spake2.PBKDFRandomSize)}, {tag: ctx(2), octets: b(spake2.PBKDFRandomSize)}, {tag: ctx(3), u: 9}}
	messages := []struct {
		name   string
		decode decoder
		fields []wireMember
	}{
		{"Pake1", pake1, []wireMember{{tag: ctx(1), octets: b(spake2.PointSize)}}},
		{"Pake2", pake2, []wireMember{{tag: ctx(1), octets: b(spake2.PointSize)}, {tag: ctx(2), octets: b(spake2.ConfirmTagSize)}}},
		{"Pake3", pake3, []wireMember{{tag: ctx(1), octets: b(spake2.ConfirmTagSize)}}},
		{"PBKDFParamRequest", request, []wireMember{random, session, passcode, hasParams}},
		{"PBKDFParamResponse", response, append(append([]wireMember(nil), respFields...), pbkdfParams(iterations, salt))},
	}
	forms := []struct {
		name     string
		tag      tlv.Tag
		implicit bool // refused by the tlv decoder wherever it stands
	}{
		{"anonymous", tlv.AnonymousTag(), false},
		{"common-profile 2-byte", tlv.CommonTag(1), false},
		{"common-profile 4-byte", tlv.CommonTag(0x10001), false},
		{"fully-qualified 6-byte", tlv.FullyQualifiedTag(0xFFF1, 1, 1), false},
		{"fully-qualified 8-byte", tlv.FullyQualifiedTag(0xFFF1, 1, 0x10001), false},
		{"implicit-profile 2-byte", tlv.ImplicitTag(1), true},
		{"implicit-profile 4-byte", tlv.ImplicitTag(0x10001), true},
	}
	join := func(parts ...[]wireMember) []wireMember {
		var out []wireMember
		for _, p := range parts {
			out = append(out, p...)
		}
		return out
	}
	generated := make([]tableCase, 0, (4*len(messages)+2)*len(forms))
	for _, f := range forms {
		scalar := wireMember{tag: f.tag, u: 1}
		container := wireMember{tag: f.tag, structure: true, nested: []wireMember{{tag: ctx(1), u: 1}}}
		unknown := wireMember{tag: ctx(9), structure: true, nested: []wireMember{scalar}}
		for _, m := range messages {
			generated = append(generated,
				tableCase{m.name + " with a " + f.name + " member after the fields", m.decode, false, join(m.fields, []wireMember{scalar}), false},
				tableCase{m.name + " with a " + f.name + " member before the fields", m.decode, false, join([]wireMember{scalar}, m.fields), false},
				tableCase{m.name + " with a " + f.name + " structure after the fields", m.decode, false, join(m.fields, []wireMember{container}), false},
				tableCase{m.name + " with a " + f.name + " member inside a skipped unknown structure", m.decode, false, join(m.fields, []wireMember{unknown}), !f.implicit},
			)
		}
		generated = append(generated,
			tableCase{"PBKDFParamResponse with a " + f.name + " member inside PBKDFParameters", response, false, join(respFields, []wireMember{pbkdfParams(iterations, scalar, salt)}), false},
			tableCase{"PBKDFParamResponse with a " + f.name + " member inside a skipped unknown structure inside PBKDFParameters", response, false, join(respFields, []wireMember{pbkdfParams(iterations, salt, unknown)}), !f.implicit},
		)
	}

	for _, tc := range append(cases, generated...) {
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
// TlvUInt32 (packages/protocol/src/session/pase/PaseMessages.ts:85,
// :89-92 at db021da0). The well-formed response decodes (the control).
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

// TestPASEDecodersSkipUnknownMembersWholeAndKeepTheLast holds the member
// walk of every PASE decoder against matter.js's TlvObject decode
// (packages/types/src/tlv/TlvObject.ts at b25ed960): a context-tagged
// member the message does not declare is skipped whole, containers included
// (:216-219), so the members of an unknown structure never count as the
// message's own fields and its end never ends the message; a declared
// member that repeats keeps its last occurrence (:229-234) instead of
// being merged, and the survivor is validated as one value; a declared
// member is decoded through its type as it is read (:222), so a mistyped
// first occurrence is refused even when a later one is well formed.
// PBKDFParamResponse's PBKDFParameters (tag 4) and ResponderSessionParams
// (tag 5) are structures, as the request's tag 5 (R2-P5 DR5a-code-01,
// DR5a-code-05, Fable D2).
func TestPASEDecodersSkipUnknownMembersWholeAndKeepTheLast(t *testing.T) {
	t.Parallel()
	b := func(n int, v byte) []byte { return bytes.Repeat([]byte{v}, n) }
	short := func(raw []byte) string {
		if len(raw) == 0 {
			return "0"
		}
		return fmt.Sprintf("%d×%02x", len(raw), raw[0])
	}
	ctx := tlv.ContextTag
	profile := tlv.CommonTag(2)
	no := false

	request := func(raw []byte) (string, error) {
		r, err := spake2.DecodePBKDFParamRequest(raw)
		return fmt.Sprintf("random=%s sid=%d pid=%d", short(r.InitiatorRandom), r.InitiatorSessionID, r.PasscodeID), err
	}
	response := func(raw []byte) (string, error) {
		r, err := spake2.DecodePBKDFParamResponse(raw)
		s := fmt.Sprintf("ir=%s rr=%s sid=%d", short(r.InitiatorRandom), short(r.ResponderRandom), r.ResponderSessionID)
		if r.Parameters != nil {
			s += fmt.Sprintf(" it=%d salt=%s", r.Parameters.Iterations, short(r.Parameters.Salt))
		}
		return s, err
	}
	pake1 := func(raw []byte) (string, error) { p, err := spake2.DecodePake1(raw); return short(p), err }
	pake3 := func(raw []byte) (string, error) { p, err := spake2.DecodePake3(raw); return short(p), err }
	pake2 := func(raw []byte) (string, error) {
		pB, cB, err := spake2.DecodePake2(raw)
		return short(pB) + " " + short(cB), err
	}

	random := wireMember{tag: ctx(1), octets: b(spake2.PBKDFRandomSize, 0x5A)}
	session42 := wireMember{tag: ctx(2), u: 42}
	passcode, hasParams := wireMember{tag: ctx(3), u: 0}, wireMember{tag: ctx(4), flag: &no}
	inner := []wireMember{{tag: ctx(2), u: 7}, {tag: ctx(3), u: 0}, {tag: ctx(4), flag: &no}}
	wellFormed := []wireMember{random, session42, passcode, hasParams}
	with := func(base []wireMember, extra ...wireMember) []wireMember {
		return append(append([]wireMember(nil), base...), extra...)
	}
	params := func(nested ...wireMember) wireMember {
		return wireMember{tag: ctx(4), structure: true, nested: nested}
	}
	iterations := wireMember{tag: ctx(1), u: uint64(spake2.PBKDFMinIterations)}
	salt := wireMember{tag: ctx(2), octets: b(spake2.PBKDFMinSaltSize, 0x01)}
	respBase := []wireMember{
		{tag: ctx(1), octets: b(spake2.PBKDFRandomSize, 0x5A)},
		{tag: ctx(2), octets: b(spake2.PBKDFRandomSize, 0x5B)},
		{tag: ctx(3), u: 9},
	}
	pB, cB := wireMember{tag: ctx(1), octets: b(spake2.PointSize, 0x5A)}, wireMember{tag: ctx(2), octets: b(spake2.ConfirmTagSize, 0x5B)}

	for _, tc := range []struct {
		name    string
		decode  func([]byte) (string, error)
		members []wireMember
		want    string // "" when the message is refused
	}{
		// DR5a-code-01 / Fable F1: an unknown container is skipped whole.
		{
			"request: tags 2-4 only inside an unknown tag-9 structure", request,
			[]wireMember{random, {tag: ctx(9), structure: true, nested: inner}},
			"",
		},
		{
			"request: an unknown tag-9 structure holding tag 2 after the fields", request,
			with(wellFormed, wireMember{tag: ctx(9), structure: true, nested: inner[:1]}), "random=32×5a sid=42 pid=0",
		},
		{
			"request: an unknown tag-9 structure holding tag 2 before the fields", request,
			with([]wireMember{{tag: ctx(9), structure: true, nested: inner[:1]}}, wellFormed...), "random=32×5a sid=42 pid=0",
		},
		{
			"request: an unknown tag-9 list holding a structure holding tag 2", request,
			with(wellFormed, wireMember{tag: ctx(9), list: true, nested: []wireMember{{tag: tlv.AnonymousTag(), structure: true, nested: inner[:1]}}}), "random=32×5a sid=42 pid=0",
		},
		{
			"request: a profile-tagged structure holding tag 2", request,
			with(wellFormed, wireMember{tag: profile, structure: true, nested: inner[:1]}), "",
		},
		// Fable D2: a repeated member keeps its last occurrence.
		{
			"request: two 16-byte InitiatorRandom members", request,
			with([]wireMember{{tag: ctx(1), octets: b(16, 0x5A)}, {tag: ctx(1), octets: b(16, 0xA5)}}, session42, passcode, hasParams), "",
		},
		{
			"request: two 32-byte InitiatorRandom members", request,
			with([]wireMember{random, {tag: ctx(1), octets: b(spake2.PBKDFRandomSize, 0xA5)}}, session42, passcode, hasParams), "random=32×a5 sid=42 pid=0",
		},
		{
			"request: an integer InitiatorRandom before a well-formed one", request,
			with([]wireMember{{tag: ctx(1), u: 5}}, wellFormed...), "",
		},
		{
			"request: InitiatorSessionID twice", request,
			with([]wireMember{{tag: ctx(2), u: 7}}, wellFormed...), "random=32×5a sid=42 pid=0",
		},
		// The response, the commissioner's side.
		{
			"response: well formed", response,
			with(respBase, params(iterations, salt)), "ir=32×5a rr=32×5b sid=9 it=1000 salt=16×01",
		},
		{
			"response: PBKDFParameters as a list", response,
			with(respBase, wireMember{tag: ctx(4), list: true, nested: []wireMember{iterations, salt}}), "",
		},
		{
			"response: ResponderSessionParams as a list", response,
			with(respBase, wireMember{tag: ctx(5), list: true, nested: []wireMember{{tag: ctx(1), u: 500}}}), "",
		},
		{
			"response: an unknown tag-9 structure holding tag 3", response,
			with(respBase, wireMember{tag: ctx(9), structure: true, nested: []wireMember{{tag: ctx(3), u: 7}}}), "ir=32×5a rr=32×5b sid=9",
		},
		{
			"response: two 32-byte ResponderRandom members", response,
			with(respBase, wireMember{tag: ctx(2), octets: b(spake2.PBKDFRandomSize, 0xA5)}), "ir=32×5a rr=32×a5 sid=9",
		},
		{
			"response: an unknown structure inside PBKDFParameters holding tag 1", response,
			with(respBase, params(iterations, salt, wireMember{tag: ctx(9), structure: true, nested: []wireMember{{tag: ctx(1), u: 5}}})), "ir=32×5a rr=32×5b sid=9 it=1000 salt=16×01",
		},
		{
			"response: two 16-byte salts", response,
			with(respBase, params(iterations, salt, wireMember{tag: ctx(2), octets: b(spake2.PBKDFMinSaltSize, 0x02)})), "ir=32×5a rr=32×5b sid=9 it=1000 salt=16×02",
		},
		// Pake1, Pake2, Pake3.
		{
			"Pake1: pA only inside an unknown tag-9 structure", pake1,
			[]wireMember{{tag: ctx(9), structure: true, nested: []wireMember{{tag: ctx(1), octets: b(spake2.PointSize, 0x5A)}}}},
			"",
		},
		{
			"Pake1: pA in two halves", pake1,
			[]wireMember{{tag: ctx(1), octets: b(32, 0x5A)}, {tag: ctx(1), octets: b(spake2.PointSize-32, 0xA5)}},
			"",
		},
		{
			"Pake1: pA twice", pake1,
			[]wireMember{{tag: ctx(1), octets: b(spake2.PointSize, 0x5A)}, {tag: ctx(1), octets: b(spake2.PointSize, 0xA5)}},
			fmt.Sprintf("%d×a5", spake2.PointSize),
		},
		{
			"Pake3: cA in two halves", pake3,
			[]wireMember{{tag: ctx(1), octets: b(spake2.ConfirmTagSize/2, 0x5A)}, {tag: ctx(1), octets: b(spake2.ConfirmTagSize/2, 0xA5)}},
			"",
		},
		{
			"Pake3: an unknown tag-9 structure holding tag 1 after cA", pake3,
			[]wireMember{{tag: ctx(1), octets: b(spake2.ConfirmTagSize, 0x5A)}, {tag: ctx(9), structure: true, nested: []wireMember{{tag: ctx(1), octets: b(4, 0xA5)}}}},
			fmt.Sprintf("%d×5a", spake2.ConfirmTagSize),
		},
		{
			"Pake2: an unknown tag-9 structure holding tag 1 after pB and cB", pake2,
			[]wireMember{pB, cB, {tag: ctx(9), structure: true, nested: []wireMember{{tag: ctx(1), octets: b(4, 0xA5)}}}},
			fmt.Sprintf("%d×5a %d×5b", spake2.PointSize, spake2.ConfirmTagSize),
		},
		// A declared member of another type is refused.
		{"Pake2: pB an integer", pake2, []wireMember{{tag: ctx(1), u: 5}, cB}, ""},
		{"Pake2: cB an integer", pake2, []wireMember{pB, {tag: ctx(2), u: 5}}, ""},
		{"Pake3: cA an integer", pake3, []wireMember{{tag: ctx(1), u: 5}}, ""},
		{"response: InitiatorRandom an integer", response, with([]wireMember{{tag: ctx(1), u: 5}}, respBase[1:]...), ""},
		{"response: ResponderRandom an integer", response, with(respBase, wireMember{tag: ctx(2), u: 5}), ""},
		{"response: the salt an integer", response, with(respBase, params(iterations, wireMember{tag: ctx(2), u: 5})), ""},
		{
			"Pake2: cB twice", pake2,
			[]wireMember{pB, cB, {tag: ctx(2), octets: b(spake2.ConfirmTagSize, 0xA5)}},
			fmt.Sprintf("%d×5a %d×a5", spake2.PointSize, spake2.ConfirmTagSize),
		},
	} {
		got, err := tc.decode(encodeWire(t, false, tc.members...))
		switch {
		case tc.want == "" && !errors.Is(err, spake2.ErrWireMalformed):
			t.Errorf("%s: decoded %q, err = %v, want ErrWireMalformed", tc.name, got, err)
		case tc.want != "" && err != nil:
			t.Errorf("%s: refused: %v", tc.name, err)
		case tc.want != "" && got != tc.want:
			t.Errorf("%s: decoded %q, want %q", tc.name, got, tc.want)
		}
	}
	// An unknown structure cut before its end is a truncated message, not
	// a member skipped short.
	unknown := wireMember{tag: ctx(9), structure: true, nested: inner[:1]}
	for _, tc := range []struct {
		name    string
		decode  func([]byte) (string, error)
		members []wireMember
	}{
		{"request", request, with(wellFormed, unknown)},
		{"response", response, with(respBase, unknown)},
		{"PBKDFParameters", response, with(respBase, params(iterations, salt, unknown))},
		{"Pake1", pake1, []wireMember{{tag: ctx(1), octets: b(spake2.PointSize, 0x5A)}, unknown}},
		{"Pake2", pake2, []wireMember{pB, cB, unknown}},
	} {
		raw := encodeWire(t, false, tc.members...)
		// Drop the outer end and the unknown structure's own end (and, for
		// PBKDFParameters, the end of the structure around it).
		cut := 2
		if tc.name == "PBKDFParameters" {
			cut = 3
		}
		if got, err := tc.decode(raw[:len(raw)-cut]); !errors.Is(err, spake2.ErrWireMalformed) {
			t.Errorf("%s with a truncated unknown structure: decoded %q, err = %v, want ErrWireMalformed", tc.name, got, err)
		}
	}
}

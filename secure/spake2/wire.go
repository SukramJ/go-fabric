// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package spake2

import (
	"errors"
	"fmt"

	"thingfabric.dev/go-fabric/secure/sessionparams"
	"thingfabric.dev/go-fabric/tlv"
)

// PASE wire-format helpers per Matter Core Spec §4.13.5.
//
// Pake1, Pake2 and Pake3 each ride as a top-level TLV Structure with
// a small set of context-tagged octet-string fields:
//
//	Pake1 = { [1]: pA }
//	Pake2 = { [1]: pB, [2]: cB }
//	Pake3 = { [1]: cA }
//
// PBKDFParamRequest / PBKDFParamResponse are richer (they carry the
// salt + iterations chosen by the responder, plus an optional
// ResponderSessionParams sub-struct so a sleepy-end-device commissioner
// can size its retransmit timers to the bridge's active-period).

// PASE wire field tags (context-tagged inside the message struct).
const (
	tagPake1PA uint8 = 1
	tagPake2PB uint8 = 1
	tagPake2CB uint8 = 2
	tagPake3CA uint8 = 1
)

// PBKDFParamRequest field tags per Matter §4.13.5.2.
const (
	tagPBKDFReqInitiatorRandom    uint8 = 1
	tagPBKDFReqInitiatorSessionID uint8 = 2
	tagPBKDFReqPasscodeID         uint8 = 3
	tagPBKDFReqHasPBKDFParameters uint8 = 4
	tagPBKDFReqInitiatorMRPParams uint8 = 5 // optional, decoded as raw octets / skipped
)

// PBKDFParamResponse field tags per Matter §4.13.5.2.
const (
	tagPBKDFRespInitiatorRandom    uint8 = 1
	tagPBKDFRespResponderRandom    uint8 = 2
	tagPBKDFRespResponderSessionID uint8 = 3
	tagPBKDFRespPBKDFParameters    uint8 = 4 // nested Structure, only when requested
	tagPBKDFRespResponderMRPParams uint8 = 5 // optional: present when the responder states session parameters (sessionParamsForWire)
)

// PBKDFParameters nested-struct field tags.
const (
	tagPBKDFParamsIterations uint8 = 1
	tagPBKDFParamsSalt       uint8 = 2
)

// PBKDFRandomSize is the byte length of the InitiatorRandom /
// ResponderRandom fields per Matter §4.13.5.2.
const PBKDFRandomSize = 32

// Wire-format errors.
var (
	// ErrWireMalformed is returned when a Pake message does not parse
	// as the expected TLV shape (wrong top-level type, missing field,
	// or wrong field length).
	ErrWireMalformed = errors.New("spake2: malformed wire message")
)

// EncodePake1 builds the on-wire TLV bytes for a Pake1 message
// carrying pA. The bridge consumes Pake1 from the commissioner via
// [DecodePake1]; this encoder is here for symmetry / tests.
func EncodePake1(pA []byte) []byte {
	enc := tlv.NewEncoder()
	enc.StartStruct(tlv.AnonymousTag())
	enc.PutOctets(tlv.ContextTag(tagPake1PA), pA)
	_ = enc.EndContainer()
	out, _ := enc.Bytes()
	return out
}

// Bytes after a message's end. Every decoder of this file reads one
// top-level structure and returns at its end-of-container; what follows
// is not looked at (5a-3 S-5, pinned by
// TestPASEDecodersTakeBytesAfterTheMessageEnd).
// Mirrors matter.js packages/types/src/tlv/TlvSchema.ts:48-50
// (decodeInternal) and TlvObject.ts:205-211 at db91886c: one top-level
// element is decoded and the reader is dropped; bytes after the
// structure's end-of-container are not inspected.

// DecodePake1 extracts the pA octet string from a Pake1 TLV payload.
// Returns [ErrWireMalformed] when the payload is not the expected
// shape.
func DecodePake1(payload []byte) ([]byte, error) {
	pA, err := decodeSingleOctet(payload, tagPake1PA, "Pake1")
	if err != nil {
		return nil, err
	}
	if len(pA) != PointSize {
		return nil, fmt.Errorf("%w: Pake1.pA length %d, want %d", ErrWireMalformed, len(pA), PointSize)
	}
	return pA, nil
}

// EncodePake2 builds the on-wire TLV bytes for a Pake2 message from
// the verifier's [Pake2Output].
func EncodePake2(out *Pake2Output) []byte {
	enc := tlv.NewEncoder()
	enc.StartStruct(tlv.AnonymousTag())
	enc.PutOctets(tlv.ContextTag(tagPake2PB), out.Y)
	enc.PutOctets(tlv.ContextTag(tagPake2CB), out.CB)
	_ = enc.EndContainer()
	buf, _ := enc.Bytes()
	return buf
}

// DecodePake2 extracts pB (Y) + cB from a Pake2 TLV payload.
// Returns [ErrWireMalformed] for shape errors. The decoded fields
// should be lengths [PointSize] and [ConfirmTagSize] respectively;
// the function enforces both.
func DecodePake2(payload []byte) (pB, cB []byte, err error) {
	dec := tlv.NewDecoder(payload)
	open, err := dec.Next()
	if err != nil {
		return nil, nil, fmt.Errorf("%w: Pake2 top: %w", ErrWireMalformed, err)
	}
	if open.Type != tlv.TypeStructure {
		return nil, nil, fmt.Errorf("%w: Pake2 top must be Structure", ErrWireMalformed)
	}
	for {
		el, err := dec.Next()
		if err != nil {
			return nil, nil, fmt.Errorf("%w: %w", ErrWireMalformed, err)
		}
		if el.IsEndContainer {
			break
		}
		tag, err := memberTag(el, "Pake2")
		if err != nil {
			return nil, nil, err
		}
		switch tag {
		case tagPake2PB:
			if pB, err = octetMember(el, "Pake2.pB"); err != nil {
				return nil, nil, err
			}
		case tagPake2CB:
			if cB, err = octetMember(el, "Pake2.cB"); err != nil {
				return nil, nil, err
			}
		default:
			if err := skipMember(dec, el, "Pake2"); err != nil {
				return nil, nil, err
			}
		}
	}
	if len(pB) != PointSize {
		return nil, nil, fmt.Errorf("%w: Pake2.pB length %d, want %d", ErrWireMalformed, len(pB), PointSize)
	}
	if len(cB) != ConfirmTagSize {
		return nil, nil, fmt.Errorf("%w: Pake2.cB length %d, want %d", ErrWireMalformed, len(cB), ConfirmTagSize)
	}
	return pB, cB, nil
}

// EncodePake3 builds the on-wire TLV bytes for a Pake3 message
// carrying the prover confirmation tag cA.
func EncodePake3(cA []byte) []byte {
	enc := tlv.NewEncoder()
	enc.StartStruct(tlv.AnonymousTag())
	enc.PutOctets(tlv.ContextTag(tagPake3CA), cA)
	_ = enc.EndContainer()
	out, _ := enc.Bytes()
	return out
}

// DecodePake3 extracts the cA octet string from a Pake3 TLV payload.
func DecodePake3(payload []byte) ([]byte, error) {
	cA, err := decodeSingleOctet(payload, tagPake3CA, "Pake3")
	if err != nil {
		return nil, err
	}
	if len(cA) != ConfirmTagSize {
		return nil, fmt.Errorf("%w: Pake3.cA length %d, want %d", ErrWireMalformed, len(cA), ConfirmTagSize)
	}
	return cA, nil
}

// decodeSingleOctet is the shared helper behind DecodePake1 +
// DecodePake3 (single-field Structure with one context-tagged octet
// string). Returns the octet bytes; length validation is the
// caller's job.
func decodeSingleOctet(payload []byte, wantTag uint8, msgName string) ([]byte, error) {
	dec := tlv.NewDecoder(payload)
	open, err := dec.Next()
	if err != nil {
		return nil, fmt.Errorf("%w: %s top: %w", ErrWireMalformed, msgName, err)
	}
	if open.Type != tlv.TypeStructure {
		return nil, fmt.Errorf("%w: %s top must be Structure", ErrWireMalformed, msgName)
	}
	var found []byte
	var has bool
	for {
		el, err := dec.Next()
		if err != nil {
			return nil, fmt.Errorf("%w: %s: %w", ErrWireMalformed, msgName, err)
		}
		if el.IsEndContainer {
			break
		}
		tag, err := memberTag(el, msgName)
		if err != nil {
			return nil, err
		}
		if tag != wantTag {
			if err := skipMember(dec, el, msgName); err != nil {
				return nil, err
			}
			continue
		}
		if found, err = octetMember(el, msgName); err != nil {
			return nil, err
		}
		has = true
	}
	if !has {
		return nil, fmt.Errorf("%w: %s missing field tag %d", ErrWireMalformed, msgName, wantTag)
	}
	return found, nil
}

// uint16Member returns el's value when el is an unsigned integer of at
// most 0xFFFF, [ErrWireMalformed] otherwise: matter.js's TlvUInt16
// (packages/types/src/tlv/TlvNumber.ts:277 at db021da0) refuses an element
// of another type (:124-131) and a value above its maximum (:155) instead
// of masking it.
func uint16Member(el tlv.Element, name string) (uint16, error) {
	if !isUnsignedInt(el.Type) || el.Uint > 0xFFFF {
		return 0, fmt.Errorf("%w: %s not a UInt16", ErrWireMalformed, name)
	}
	return uint16(el.Uint), nil
}

// isUnsignedInt reports whether t is a TLV unsigned integer of any width.
func isUnsignedInt(t tlv.ElementType) bool {
	return t >= tlv.TypeUnsignedInt1 && t <= tlv.TypeUnsignedInt8
}

// memberTag returns the context tag number of el, a member of the
// structure named name. A member whose tag is not context-specific —
// anonymous, common-profile or fully-qualified; an implicit-profile tag
// never gets here, the tlv decoder refuses it — is [ErrWireMalformed]:
// matter.js's TlvObject decode throws for it at the member's position,
// before it looks the field up
// (packages/types/src/tlv/TlvObject.ts:212-214 at db91886c). A context tag
// the structure does not declare, 0 included, is the caller's to skip
// ([skipMember]).
func memberTag(el tlv.Element, name string) (uint8, error) {
	if el.Tag.Kind != tlv.TagKindContext {
		return 0, fmt.Errorf("%w: %s member with a tag that is not context-specific (tag control %d)", ErrWireMalformed, name, el.Tag.Kind)
	}
	return uint8(el.Tag.Number), nil //nolint:gosec // G115: a context tag is one byte on the wire (tlv/decode.go readTag)
}

// skipMember consumes a context-tagged member of the structure named
// name that the structure does not declare, whole: when el opens a
// container, every element up to its own end is read and dropped, so
// the members of an unknown container never count as the message's
// fields and its end never ends the message — as matter.js's TlvObject
// decode skips a context-tagged field it does not declare
// (packages/types/src/tlv/TlvObject.ts:216-219 at db021da0) and refuses a
// member whose tag is not context-specific (:212-214, [memberTag]).
// Inside the skipped member the tag forms are not looked at, as
// matter.js's TlvAny reads them (packages/types/src/tlv/TlvAny.ts:87-90);
// an implicit-profile tag is still refused by the tlv decoder.
//
// The walk is the one tlv's unexported skipContainer does (tlv/raw.go);
// reusing it would export a skip from the public tlv package for this one
// caller, so the loop stays here (R2-P5 RR5a-05).
func skipMember(dec *tlv.Decoder, el tlv.Element, name string) error {
	for depth := 0; el.IsContainer || depth > 0; {
		switch {
		case el.IsContainer:
			depth++
		case el.IsEndContainer:
			depth--
			if depth == 0 {
				return nil
			}
		}
		var err error
		if el, err = dec.Next(); err != nil {
			return fmt.Errorf("%w: %s: unknown member: %w", ErrWireMalformed, name, err)
		}
	}
	return nil
}

// octetMember returns a copy of el's octets when el is an octet string,
// [ErrWireMalformed] otherwise. A field that repeats is replaced, never
// merged: matter.js decodes each declared member through its schema as
// it reads it (TlvObject.ts:222) and keeps the last occurrence of a
// repeated one (:229-234), whose length the caller then checks. The copy
// is defensive: the TLV decoder's Octets slice aliases the input buffer,
// which the caller may recycle.
func octetMember(el tlv.Element, name string) ([]byte, error) {
	if el.Type < tlv.TypeOctetStr1 || el.Type > tlv.TypeOctetStr8 {
		return nil, fmt.Errorf("%w: %s not an octet string", ErrWireMalformed, name)
	}
	return append(make([]byte, 0, len(el.Octets)), el.Octets...), nil
}

// PBKDFParamRequest is the in-memory form of the commissioner's
// initial PASE message per Matter §4.13.5.2. The bridge consumes
// it via [DecodePBKDFParamRequest], assembles a matching
// [PBKDFParamResponse], and ships it back via the SecureChannel
// router's reply path.
type PBKDFParamRequest struct {
	// InitiatorRandom is the 32-byte random the commissioner picks
	// per request. The bridge echoes it verbatim in the response.
	InitiatorRandom []byte
	// InitiatorSessionID is the local session ID the commissioner
	// has reserved for the eventual PASE session. Goes into every
	// inbound encrypted Header.SessionID once the exchange completes.
	InitiatorSessionID uint16
	// PasscodeID identifies which passcode on the bridge the
	// commissioner expects to authenticate against. The default
	// passcode lives at PasscodeID=0; multi-passcode bridges use
	// > 0 to address per-fabric onboarding codes.
	PasscodeID uint16
	// HasPBKDFParameters reports whether the commissioner already
	// knows the bridge's PBKDF salt + iterations (e.g. carried in
	// the QR code or Manual Pairing Code). When true, the
	// responder omits the [PBKDFParamResponse.PBKDFParameters]
	// field; when false, the responder MUST include it.
	HasPBKDFParameters bool
	// InitiatorSessionParams is the commissioner's SessionParameters
	// (tag 5, nil when absent): the MRP intervals, the revisions,
	// MaxPathsPerInvoke and, from Matter 1.5, SupportedTransports and
	// MaxTCPMessageSize — the full TlvSessionParameters of matter.js
	// PaseMessages.ts:28-58, :74. The PASE session opener stamps them
	// onto the operational entry (PaseServer.ts:155-157
	// `session.timingParameters = initiatorSessionParams`).
	InitiatorSessionParams *sessionparams.Parameters
}

// DecodePBKDFParamRequest parses a Matter PBKDFParamRequest TLV
// payload, including the optional InitiatorSessionParams field (tag 5)
// so the eventual PASE session can honour the commissioner's
// retransmit intervals (matter.js PaseServer.ts:135-157).
//
// Tags 1-4 are mandatory and typed, as matter.js's TlvPbkdfParamRequest
// declares them (packages/protocol/src/session/pase/PaseMessages.ts:70-73
// at db021da0: ByteString of 32, UInt16, UInt16, Boolean): a request that
// lacks one, or carries one of another type or above its width, is
// [ErrWireMalformed] — matter.js's decode throws (TlvObject.ts:252-261,
// TlvNumber.ts:124-131, :155, TlvBoolean.ts:22-24), and a missing
// PasscodeID or HasPBKDFParameters is never read as 0 or false.
func DecodePBKDFParamRequest(payload []byte) (PBKDFParamRequest, error) {
	var req PBKDFParamRequest
	var hasSessionID, hasPasscodeID, hasHasParams bool
	dec := tlv.NewDecoder(payload)
	open, err := dec.Next()
	if err != nil {
		return req, fmt.Errorf("%w: PBKDFParamRequest top: %w", ErrWireMalformed, err)
	}
	if open.Type != tlv.TypeStructure {
		return req, fmt.Errorf("%w: PBKDFParamRequest top must be Structure", ErrWireMalformed)
	}
	for {
		el, err := dec.Next()
		if err != nil {
			return req, fmt.Errorf("%w: %w", ErrWireMalformed, err)
		}
		if el.IsEndContainer {
			break
		}
		tag, err := memberTag(el, "PBKDFParamRequest")
		if err != nil {
			return req, err
		}
		switch tag {
		case tagPBKDFReqInitiatorRandom:
			if req.InitiatorRandom, err = octetMember(el, "PBKDFParamRequest.InitiatorRandom"); err != nil {
				return req, err
			}
		case tagPBKDFReqInitiatorSessionID:
			if req.InitiatorSessionID, err = uint16Member(el, "PBKDFParamRequest.InitiatorSessionID"); err != nil {
				return req, err
			}
			hasSessionID = true
		case tagPBKDFReqPasscodeID:
			if req.PasscodeID, err = uint16Member(el, "PBKDFParamRequest.PasscodeID"); err != nil {
				return req, err
			}
			hasPasscodeID = true
		case tagPBKDFReqHasPBKDFParameters:
			if el.Type != tlv.TypeBoolFalse && el.Type != tlv.TypeBoolTrue {
				return req, fmt.Errorf("%w: PBKDFParamRequest.HasPBKDFParameters not a boolean", ErrWireMalformed)
			}
			req.HasPBKDFParameters = el.Bool
			hasHasParams = true
		case tagPBKDFReqInitiatorMRPParams:
			// TlvOptionalField(5, TlvSessionParameters), PaseMessages.ts:74:
			// a structure when present.
			if el.Type != tlv.TypeStructure {
				return req, fmt.Errorf("%w: InitiatorSessionParams not a structure", ErrWireMalformed)
			}
			params, perr := sessionparams.DecodeFields(dec)
			if perr != nil {
				return req, fmt.Errorf("%w: InitiatorSessionParams: %w", ErrWireMalformed, perr)
			}
			req.InitiatorSessionParams = &params
		default:
			if err := skipMember(dec, el, "PBKDFParamRequest"); err != nil {
				return req, err
			}
		}
	}
	switch {
	case !hasSessionID:
		return req, fmt.Errorf("%w: PBKDFParamRequest missing field tag %d (InitiatorSessionID)", ErrWireMalformed, tagPBKDFReqInitiatorSessionID)
	case !hasPasscodeID:
		return req, fmt.Errorf("%w: PBKDFParamRequest missing field tag %d (PasscodeID)", ErrWireMalformed, tagPBKDFReqPasscodeID)
	case !hasHasParams:
		return req, fmt.Errorf("%w: PBKDFParamRequest missing field tag %d (HasPBKDFParameters)", ErrWireMalformed, tagPBKDFReqHasPBKDFParameters)
	}
	if len(req.InitiatorRandom) != PBKDFRandomSize {
		return req, fmt.Errorf("%w: PBKDFParamRequest.InitiatorRandom length %d, want %d",
			ErrWireMalformed, len(req.InitiatorRandom), PBKDFRandomSize)
	}
	return req, nil
}

// PBKDFParamResponse is the bridge's reply per Matter §4.13.5.2. The
// caller fills in fields and calls [PBKDFParamResponse.Marshal] to
// get the wire bytes.
type PBKDFParamResponse struct {
	// InitiatorRandom MUST equal the value from the inbound
	// PBKDFParamRequest — Matter §4.13.5.2 ties the request /
	// response into the verifier's transcript hash.
	InitiatorRandom []byte
	// ResponderRandom is a fresh 32-byte random the bridge picks
	// per response; included in the verifier's transcript.
	ResponderRandom []byte
	// ResponderSessionID is the local session ID the bridge has
	// reserved for the eventual PASE session. Goes into every
	// outbound encrypted Header.SessionID once the exchange completes.
	ResponderSessionID uint16
	// Parameters is included only when the request flagged
	// HasPBKDFParameters=false; nil otherwise.
	Parameters *PBKDFParameters
	// ResponderSessionParams is the bridge's SessionParameters for the
	// commissioner (tag 5): the full structure matter.js's PaseServer.ts:151
	// sends (`responderSessionParams = this.sessions.sessionParameters`,
	// the same parameters CASE advertises). Nil omits the field.
	ResponderSessionParams *sessionparams.Parameters
}

// PBKDFParameters carries the bridge's PBKDF salt + iteration count
// per Matter §4.13.5.2. The commissioner uses these to derive
// (w0, w1) on its side identically to the bridge.
type PBKDFParameters struct {
	Iterations uint32
	Salt       []byte
}

// PBKDF salt + iteration count bounds per Matter §3.10.3 (pbkdf2_*
// fields). Decoded responses outside these bounds are rejected so a
// hostile or buggy peer cannot push us into a brute-forceable region
// (low iterations) or DoS us with extreme values (huge iterations).
const (
	// PBKDFMinSaltSize is the lower bound on the PBKDF2 salt length
	// in bytes (Matter §3.10.3 — pbkdf2_iterations_salt MIN).
	PBKDFMinSaltSize = 16
	// PBKDFMaxSaltSize is the upper bound on the PBKDF2 salt length
	// in bytes (Matter §3.10.3 — pbkdf2_iterations_salt MAX).
	PBKDFMaxSaltSize = 32
	// PBKDFMinIterations is the lower bound on the PBKDF2 iteration
	// count (Matter §3.10.3 — pbkdf2_iterations MIN).
	PBKDFMinIterations uint32 = 1000
	// PBKDFMaxIterations is the upper bound on the PBKDF2 iteration
	// count (Matter §3.10.3 — pbkdf2_iterations MAX).
	PBKDFMaxIterations uint32 = 100000
)

// Validate reports whether the parameters are within the bounds the
// Matter spec mandates. Use this before deriving (w0, w1) from
// peer-supplied params; invalid bounds get wrapped in [ErrWireMalformed]
// by the response decoder.
func (p PBKDFParameters) Validate() error {
	if len(p.Salt) < PBKDFMinSaltSize || len(p.Salt) > PBKDFMaxSaltSize {
		return fmt.Errorf("%w: PBKDFParameters.Salt length %d, want %d..%d",
			ErrWireMalformed, len(p.Salt), PBKDFMinSaltSize, PBKDFMaxSaltSize)
	}
	if p.Iterations < PBKDFMinIterations || p.Iterations > PBKDFMaxIterations {
		return fmt.Errorf("%w: PBKDFParameters.Iterations %d, want %d..%d",
			ErrWireMalformed, p.Iterations, PBKDFMinIterations, PBKDFMaxIterations)
	}
	return nil
}

// Marshal encodes resp as a Matter PBKDFParamResponse TLV payload.
// Caller is expected to populate every required field; missing
// InitiatorRandom / ResponderRandom (wrong length) produces a
// well-formed but invalid payload — callers SHOULD validate before
// shipping. (We keep Marshal infallible to match the
// EncodePake1/2/3 pattern that already discards encoder errors.)
func (resp PBKDFParamResponse) Marshal() []byte {
	enc := tlv.NewEncoder()
	enc.StartStruct(tlv.AnonymousTag())
	enc.PutOctets(tlv.ContextTag(tagPBKDFRespInitiatorRandom), resp.InitiatorRandom)
	enc.PutOctets(tlv.ContextTag(tagPBKDFRespResponderRandom), resp.ResponderRandom)
	enc.PutUint(tlv.ContextTag(tagPBKDFRespResponderSessionID), uint64(resp.ResponderSessionID))
	if resp.Parameters != nil {
		enc.StartStruct(tlv.ContextTag(tagPBKDFRespPBKDFParameters))
		enc.PutUint(tlv.ContextTag(tagPBKDFParamsIterations), uint64(resp.Parameters.Iterations))
		enc.PutOctets(tlv.ContextTag(tagPBKDFParamsSalt), resp.Parameters.Salt)
		_ = enc.EndContainer()
	}
	if sp := resp.sessionParamsForWire(); sp != nil {
		sp.Encode(enc, tlv.ContextTag(tagPBKDFRespResponderMRPParams))
	}
	_ = enc.EndContainer()
	out, _ := enc.Bytes()
	return out
}

// DecodePBKDFParamResponse parses a Matter PBKDFParamResponse TLV
// payload. Provided for symmetry with the encoder + tests; the
// bridge itself only encodes responses (it never receives them).
//
// ResponderSessionID (tag 3) is mandatory and a UInt16
// (PaseMessages.ts:85 at db91886c), so a response without it, or with
// one of another type or width, is [ErrWireMalformed], never session 0.
func DecodePBKDFParamResponse(payload []byte) (PBKDFParamResponse, error) {
	var resp PBKDFParamResponse
	var hasSessionID bool
	dec := tlv.NewDecoder(payload)
	open, err := dec.Next()
	if err != nil {
		return resp, fmt.Errorf("%w: PBKDFParamResponse top: %w", ErrWireMalformed, err)
	}
	if open.Type != tlv.TypeStructure {
		return resp, fmt.Errorf("%w: PBKDFParamResponse top must be Structure", ErrWireMalformed)
	}
	for {
		el, err := dec.Next()
		if err != nil {
			return resp, fmt.Errorf("%w: %w", ErrWireMalformed, err)
		}
		if el.IsEndContainer {
			break
		}
		tag, err := memberTag(el, "PBKDFParamResponse")
		if err != nil {
			return resp, err
		}
		switch tag {
		case tagPBKDFRespInitiatorRandom:
			if resp.InitiatorRandom, err = octetMember(el, "PBKDFParamResponse.InitiatorRandom"); err != nil {
				return resp, err
			}
		case tagPBKDFRespResponderRandom:
			if resp.ResponderRandom, err = octetMember(el, "PBKDFParamResponse.ResponderRandom"); err != nil {
				return resp, err
			}
		case tagPBKDFRespResponderSessionID:
			if resp.ResponderSessionID, err = uint16Member(el, "PBKDFParamResponse.ResponderSessionID"); err != nil {
				return resp, err
			}
			hasSessionID = true
		case tagPBKDFRespPBKDFParameters:
			// A structure, as the request's tag 5.
			if el.Type != tlv.TypeStructure {
				return resp, fmt.Errorf("%w: PBKDFParameters not a structure", ErrWireMalformed)
			}
			params, err := decodePBKDFParameters(dec)
			if err != nil {
				return resp, err
			}
			resp.Parameters = &params
		case tagPBKDFRespResponderMRPParams:
			if el.Type != tlv.TypeStructure {
				return resp, fmt.Errorf("%w: ResponderSessionParams not a structure", ErrWireMalformed)
			}
			params, err := sessionparams.DecodeFields(dec)
			if err != nil {
				return resp, fmt.Errorf("%w: ResponderSessionParams: %w", ErrWireMalformed, err)
			}
			resp.ResponderSessionParams = &params
		default:
			if err := skipMember(dec, el, "PBKDFParamResponse"); err != nil {
				return resp, err
			}
		}
	}
	if !hasSessionID {
		return resp, fmt.Errorf("%w: PBKDFParamResponse missing field tag %d (ResponderSessionID)", ErrWireMalformed, tagPBKDFRespResponderSessionID)
	}
	if len(resp.InitiatorRandom) != PBKDFRandomSize {
		return resp, fmt.Errorf("%w: PBKDFParamResponse.InitiatorRandom length %d, want %d",
			ErrWireMalformed, len(resp.InitiatorRandom), PBKDFRandomSize)
	}
	if len(resp.ResponderRandom) != PBKDFRandomSize {
		return resp, fmt.Errorf("%w: PBKDFParamResponse.ResponderRandom length %d, want %d",
			ErrWireMalformed, len(resp.ResponderRandom), PBKDFRandomSize)
	}
	return resp, nil
}

// decodePBKDFParameters consumes the nested PBKDFParameters
// structure off dec. The opening Structure element has already been
// consumed by the caller. Salt + iterations are validated against
// the Matter §3.10.3 bounds; out-of-range values wrap [ErrWireMalformed].
func decodePBKDFParameters(dec *tlv.Decoder) (PBKDFParameters, error) {
	var p PBKDFParameters
	for {
		el, err := dec.Next()
		if err != nil {
			return p, fmt.Errorf("%w: PBKDFParameters: %w", ErrWireMalformed, err)
		}
		if el.IsEndContainer {
			break
		}
		tag, err := memberTag(el, "PBKDFParameters")
		if err != nil {
			return p, err
		}
		switch tag {
		case tagPBKDFParamsIterations:
			// TlvUInt32 (PaseMessages.ts:89-92): never masked into the
			// bounds [PBKDFParameters.Validate] holds.
			if !isUnsignedInt(el.Type) || el.Uint > 0xFFFFFFFF {
				return p, fmt.Errorf("%w: PBKDFParameters.Iterations not a UInt32", ErrWireMalformed)
			}
			p.Iterations = uint32(el.Uint)
		case tagPBKDFParamsSalt:
			if p.Salt, err = octetMember(el, "PBKDFParameters.Salt"); err != nil {
				return p, err
			}
		default:
			if err := skipMember(dec, el, "PBKDFParameters"); err != nil {
				return p, err
			}
		}
	}
	if err := p.Validate(); err != nil {
		return p, err
	}
	return p, nil
}

// sessionParamsForWire is the structure PBKDFParamResponse tag 5 carries:
// ResponderSessionParams when set and not empty, else nothing.
func (resp PBKDFParamResponse) sessionParamsForWire() *sessionparams.Parameters {
	if resp.ResponderSessionParams != nil {
		if resp.ResponderSessionParams.IsEmpty() {
			return nil
		}
		return resp.ResponderSessionParams
	}
	return nil
}

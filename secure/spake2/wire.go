// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package spake2

import (
	"errors"
	"fmt"

	"github.com/SukramJ/go-fabric/secure/sessionparams"
	"github.com/SukramJ/go-fabric/tlv"
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
// ResponderMRPParams sub-struct so a sleepy-end-device commissioner
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
	tagPBKDFRespResponderMRPParams uint8 = 5 // optional, omitted in v1.1
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
		if el.Tag.Kind != tlv.TagKindContext {
			continue
		}
		switch uint8(el.Tag.Number & 0xFF) {
		case tagPake2PB:
			pB = appendOctets(pB, el.Octets)
		case tagPake2CB:
			cB = appendOctets(cB, el.Octets)
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
	for {
		el, err := dec.Next()
		if err != nil {
			return nil, fmt.Errorf("%w: %s: %w", ErrWireMalformed, msgName, err)
		}
		if el.IsEndContainer {
			break
		}
		if el.Tag.Kind == tlv.TagKindContext && el.Tag.Number == uint32(wantTag) {
			found = appendOctets(found, el.Octets)
		}
	}
	if found == nil {
		return nil, fmt.Errorf("%w: %s missing field tag %d", ErrWireMalformed, msgName, wantTag)
	}
	return found, nil
}

// appendOctets is a defensive copy. The TLV decoder's Octets slice
// aliases the input buffer which the caller may recycle; the
// adapter must take ownership before returning to the caller.
func appendOctets(dst, src []byte) []byte {
	if dst == nil {
		dst = make([]byte, 0, len(src))
	}
	return append(dst, src...)
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
	// InitiatorMRPParams is the MRP triplet of InitiatorSessionParams,
	// filled on decode for consumers of the legacy field.
	//
	// Deprecated: read [PBKDFParamRequest.InitiatorSessionParams], which
	// carries the whole structure. Marked in v0.3.0; removal is
	// permissible from v0.5.0, the second minor release after the
	// marking one (README §API stability).
	InitiatorMRPParams *MRPParameters
}

// DecodePBKDFParamRequest parses a Matter PBKDFParamRequest TLV
// payload, including the optional InitiatorMRPParams field (tag 5)
// so the eventual PASE session can honour the commissioner's
// retransmit intervals (matter.js PaseServer.ts:135-157).
func DecodePBKDFParamRequest(payload []byte) (PBKDFParamRequest, error) {
	var req PBKDFParamRequest
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
		if el.Tag.Kind != tlv.TagKindContext {
			continue
		}
		switch uint8(el.Tag.Number & 0xFF) {
		case tagPBKDFReqInitiatorRandom:
			req.InitiatorRandom = appendOctets(req.InitiatorRandom, el.Octets)
		case tagPBKDFReqInitiatorSessionID:
			req.InitiatorSessionID = uint16(el.Uint & 0xFFFF)
		case tagPBKDFReqPasscodeID:
			req.PasscodeID = uint16(el.Uint & 0xFFFF)
		case tagPBKDFReqHasPBKDFParameters:
			req.HasPBKDFParameters = el.Bool
		case tagPBKDFReqInitiatorMRPParams:
			if el.IsContainer {
				params, perr := sessionparams.DecodeFields(dec)
				if perr != nil {
					return req, fmt.Errorf("%w: InitiatorSessionParams: %w", ErrWireMalformed, perr)
				}
				req.InitiatorSessionParams = &params
				req.InitiatorMRPParams = legacyMRPParameters(params)
			}
		}
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
	// ResponderMRPParams is the legacy MRP triplet. When
	// ResponderSessionParams is nil it is encoded as tags 1-3 of the
	// structure; on decode it is filled from ResponderSessionParams.
	//
	// Deprecated: set [PBKDFParamResponse.ResponderSessionParams]. Marked
	// in v0.3.0; removal is permissible from v0.5.0, the second minor
	// release after the marking one (README §API stability).
	ResponderMRPParams *MRPParameters
}

// MRPParameters carries the MRP retransmit-timer triplet, tags 1-3 of
// the SessionParameters structure a PASE peer advertises in
// PBKDFParamRequest / PBKDFParamResponse tag 5.
//
// Deprecated: use [sessionparams.Parameters], which carries the whole
// structure (tags 1-9) on both the PASE and the CASE path. Marked in
// v0.3.0; removal is permissible from v0.5.0, the second minor release
// after the marking one (README §API stability).
//
// It Each pointer field is
// independently optional — a nil leaves the corresponding TLV tag
// absent on the wire so the peer falls back to its own defaults
// (Matter §4.12.8).
//
// The two retransmit timeouts are uint32 on the wire and the active
// threshold is uint16 — matter.js PaseMessages.ts:25-29 declares
// idleInterval / activeInterval as TlvUInt32 and activeThreshold as
// TlvUInt16, the same widths sigma.SessionParameters carries for CASE.
type MRPParameters struct {
	IdleRetransTimeoutMs   *uint32
	ActiveRetransTimeoutMs *uint32
	ActiveThresholdTimeMs  *uint16
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
func DecodePBKDFParamResponse(payload []byte) (PBKDFParamResponse, error) {
	var resp PBKDFParamResponse
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
		if el.Tag.Kind != tlv.TagKindContext {
			continue
		}
		switch uint8(el.Tag.Number & 0xFF) {
		case tagPBKDFRespInitiatorRandom:
			resp.InitiatorRandom = appendOctets(resp.InitiatorRandom, el.Octets)
		case tagPBKDFRespResponderRandom:
			resp.ResponderRandom = appendOctets(resp.ResponderRandom, el.Octets)
		case tagPBKDFRespResponderSessionID:
			resp.ResponderSessionID = uint16(el.Uint & 0xFFFF)
		case tagPBKDFRespPBKDFParameters:
			if !el.IsContainer {
				return resp, fmt.Errorf("%w: PBKDFParameters not a container", ErrWireMalformed)
			}
			params, err := decodePBKDFParameters(dec)
			if err != nil {
				return resp, err
			}
			resp.Parameters = &params
		case tagPBKDFRespResponderMRPParams:
			if !el.IsContainer {
				return resp, fmt.Errorf("%w: ResponderSessionParams not a container", ErrWireMalformed)
			}
			params, err := sessionparams.DecodeFields(dec)
			if err != nil {
				return resp, fmt.Errorf("%w: ResponderSessionParams: %w", ErrWireMalformed, err)
			}
			resp.ResponderSessionParams = &params
			resp.ResponderMRPParams = legacyMRPParameters(params)
		}
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
		if el.Tag.Kind != tlv.TagKindContext {
			continue
		}
		switch uint8(el.Tag.Number & 0xFF) {
		case tagPBKDFParamsIterations:
			p.Iterations = uint32(el.Uint & 0xFFFFFFFF)
		case tagPBKDFParamsSalt:
			p.Salt = appendOctets(p.Salt, el.Octets)
		}
	}
	if err := p.Validate(); err != nil {
		return p, err
	}
	return p, nil
}

// sessionParamsForWire is the structure PBKDFParamResponse tag 5 carries:
// ResponderSessionParams when set, else the legacy triplet lifted into
// the structure (the same tags and widths, PaseMessages.ts:28-33), else
// nothing.
func (resp PBKDFParamResponse) sessionParamsForWire() *sessionparams.Parameters {
	if resp.ResponderSessionParams != nil {
		if resp.ResponderSessionParams.IsEmpty() {
			return nil
		}
		return resp.ResponderSessionParams
	}
	if m := resp.ResponderMRPParams; m != nil {
		p := m.Parameters()
		if p.IsEmpty() {
			return nil
		}
		return &p
	}
	return nil
}

// Parameters lifts the triplet into a SessionParameters structure: a nil
// field stays 0 ("not stated").
func (m MRPParameters) Parameters() sessionparams.Parameters {
	var p sessionparams.Parameters
	if m.IdleRetransTimeoutMs != nil {
		p.SessionIdleInterval = *m.IdleRetransTimeoutMs
	}
	if m.ActiveRetransTimeoutMs != nil {
		p.SessionActiveInterval = *m.ActiveRetransTimeoutMs
	}
	if m.ActiveThresholdTimeMs != nil {
		p.SessionActiveThreshold = *m.ActiveThresholdTimeMs
	}
	return p
}

// legacyMRPParameters is the triplet view of p for the deprecated
// fields: nil when p states none of the three.
func legacyMRPParameters(p sessionparams.Parameters) *MRPParameters {
	if p.SessionIdleInterval == 0 && p.SessionActiveInterval == 0 && p.SessionActiveThreshold == 0 {
		return nil
	}
	m := &MRPParameters{}
	if p.SessionIdleInterval != 0 {
		v := p.SessionIdleInterval
		m.IdleRetransTimeoutMs = &v
	}
	if p.SessionActiveInterval != 0 {
		v := p.SessionActiveInterval
		m.ActiveRetransTimeoutMs = &v
	}
	if p.SessionActiveThreshold != 0 {
		v := p.SessionActiveThreshold
		m.ActiveThresholdTimeMs = &v
	}
	return m
}

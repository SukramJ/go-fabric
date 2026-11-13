// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package spake2

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/binary"
	"errors"
	"fmt"
	"math/big"

	"filippo.io/nistec"
)

// MatterContext is the SPAKE2+ context string Matter mandates for
// PASE (Core Spec §3.10.4 / Table 13).
const MatterContext = "CHIP PAKE V1 Commissioning"

// Sizes per Matter Core Spec §3.10.
const (
	// PointSize is the encoded length of an uncompressed P-256 point
	// (1 prefix + 32 X + 32 Y).
	PointSize = 65
	// ScalarSize is the encoded length of a P-256 scalar.
	ScalarSize = 32
	// WSize is the byte length of the (w0, w1) PBKDF2 outputs as fed
	// into the modular reduction (40 bytes per Matter spec).
	WSize = 40
	// PBKDF2OutputSize is the total PBKDF2 output for both ws values.
	PBKDF2OutputSize = 2 * WSize
	// ConfirmTagSize is the truncated HMAC length used for cA / cB.
	ConfirmTagSize = 32
	// SharedSecretSize is the size of the resulting Ke session key.
	SharedSecretSize = 16
)

// Errors.
var (
	// ErrInvalidPoint surfaces when a peer sends a point that is not
	// on the curve or is the identity element.
	ErrInvalidPoint = errors.New("spake2: peer point is invalid")
	// ErrConfirmationFailed is returned when a peer's confirmation
	// tag does not match the locally-derived expectation. The caller
	// MUST treat this as a fatal session-establishment error and
	// reset the exchange — it is the canonical "wrong passcode" or
	// "tampered transcript" signal.
	ErrConfirmationFailed = errors.New("spake2: peer confirmation tag mismatch")
	// ErrInvalidPasscode is reserved for future use when the passcode
	// is outside the allowed range (1..99999998 per Matter §5.1.1.6).
	ErrInvalidPasscode = errors.New("spake2: passcode out of range")
	// ErrSessionState surfaces when methods are invoked out of order
	// (e.g., calling [Verifier.Finish] before [Verifier.Pake1]).
	ErrSessionState = errors.New("spake2: invalid session state")
)

// curve returns the P-256 curve instance shared across the package.
//
// Its remaining use is the group order n, which the scalar arithmetic
// reduces against. The point arithmetic runs on [nistec.P256Point]:
// crypto/elliptic's ScalarMult / Add / Marshal are deprecated, and the
// standard library offers no replacement for the operations SPAKE2+ needs
// (addition of arbitrary points, negation, an identity test), which is why
// the deprecation notice itself points at filippo.io/nistec.
func curve() elliptic.Curve { return elliptic.P256() }

// Matter-mandated SPAKE2+ generator points M and N. Bytes are taken
// from Matter Core Specification §3.10.1, which references the
// SPAKE2+ test-vector points from RFC 9383.
//
// Encoded as uncompressed (0x04 || X || Y): the compressed form with its
// 0x02/0x03 prefix is harder to verify against the spec by eye, so the
// uncompressed serialisation is used throughout.
var (
	// matterMHex is the M point per Matter §3.10.1 (uncompressed).
	matterMHex = "" +
		"04" +
		"886e2f97ace46e55ba9dd7242579f2993b64e16ef3dcab95afd497333d8fa12f" +
		"5ff355163e43ce224e0b0e65ff02ac8e5c7be09419c785e0ca547d55a12e2d20"

	// matterNHex is the N point per Matter §3.10.1 (uncompressed).
	matterNHex = "" +
		"04" +
		"d8bbd6c639c62937b04d997f38c3770719c629d7014d49a24b4f98baa1292b49" +
		"07d60aa6bfade45008a636337f5168c64d9bd36034808cd564490b1e656edbe7"
)

// pointMN holds the unmarshalled M and N points. Computed once in
// init().
var (
	mPoint *nistec.P256Point
	nPoint *nistec.P256Point
)

func init() {
	// invariant: matterMHex / matterNHex are the Matter spec's fixed
	// PAKE M/N constants, checked-in source literals — never derived
	// from remote input. A decode failure here can only be a typo in
	// this file, caught at process start on every boot, long before
	// any PASE handshake reaches the wire.
	var err error
	if mPoint, err = unmarshalUncompressed(matterMHex); err != nil {
		panic(fmt.Sprintf("spake2: invalid M constant: %v", err))
	}
	if nPoint, err = unmarshalUncompressed(matterNHex); err != nil {
		panic(fmt.Sprintf("spake2: invalid N constant: %v", err))
	}
}

// hexToBytes is a tiny encoder/hex helper used only by the M/N
// constant initialisation. We avoid encoding/hex to keep the
// init-time dependency surface minimal.
func hexToBytes(s string) ([]byte, error) {
	if len(s)%2 != 0 {
		return nil, errors.New("spake2: hex: odd length")
	}
	out := make([]byte, len(s)/2)
	for i := 0; i < len(s); i += 2 {
		hi, err := hexNibble(s[i])
		if err != nil {
			return nil, err
		}
		lo, err := hexNibble(s[i+1])
		if err != nil {
			return nil, err
		}
		out[i/2] = hi<<4 | lo
	}
	return out, nil
}

func hexNibble(b byte) (byte, error) {
	switch {
	case b >= '0' && b <= '9':
		return b - '0', nil
	case b >= 'a' && b <= 'f':
		return b - 'a' + 10, nil
	case b >= 'A' && b <= 'F':
		return b - 'A' + 10, nil
	}
	return 0, fmt.Errorf("hex: bad nibble %q", b)
}

// unmarshalUncompressed decodes a hex-encoded uncompressed P-256
// point (0x04 || X || Y) into a curve point. SetBytes rejects anything
// that is not on the curve.
func unmarshalUncompressed(hexStr string) (*nistec.P256Point, error) {
	raw, err := hexToBytes(hexStr)
	if err != nil {
		return nil, err
	}
	pt, err := nistec.NewP256Point().SetBytes(raw)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidPoint, err)
	}
	return pt, nil
}

// PBKDF derives (w0, w1) from a passcode + salt + iterations. Matter
// Core Spec §3.10.3 mandates SHA-256 and a 40-byte output per scalar
// (the modular reduction below maps the wider value into the curve
// order).
func PBKDF(passcode uint32, salt []byte, iterations int) (w0, w1 *big.Int, err error) {
	if iterations <= 0 {
		return nil, nil, fmt.Errorf("%w: iterations must be > 0", ErrInvalidPasscode)
	}
	pwd := make([]byte, 4)
	binary.LittleEndian.PutUint32(pwd, passcode)
	out, err := pbkdf2.Key(sha256.New, string(pwd), salt, iterations, PBKDF2OutputSize)
	if err != nil {
		return nil, nil, fmt.Errorf("spake2: pbkdf2: %w", err)
	}
	order := curve().Params().N
	// The PBKDF2 output and its two unreduced halves are the passcode's
	// equivalent; only the reduced scalars leave this function.
	wide0, wide1 := new(big.Int).SetBytes(out[:WSize]), new(big.Int).SetBytes(out[WSize:])
	w0 = new(big.Int).Mod(wide0, order)
	w1 = new(big.Int).Mod(wide1, order)
	secretHook("pbkdf2", out)
	zero(out)
	zero(pwd)
	zeroInt(wide0)
	zeroInt(wide1)
	return w0, w1, nil
}

// VerifierContext bundles the constants a Matter device stores after
// commissioning passcode setup. The verifier never sees the passcode
// directly — only w0 and L = w1·G.
type VerifierContext struct {
	W0 *big.Int
	L  *ecdsa.PublicKey // w1·G
}

// Matter Core Spec §3.10.1 PBKDF iteration bounds.
const (
	// IterationsMin is the spec-mandated minimum PBKDF2 iteration count.
	IterationsMin = 1000
	// IterationsMax is the spec-mandated maximum PBKDF2 iteration count.
	IterationsMax = 100000
)

// NewVerifierContext computes a fresh VerifierContext from passcode
// material. Used at commissioning-onboarding time; in production the
// derived (W0, L) pair is persisted and reused, not the passcode.
// Returns an error if iterations is outside [IterationsMin, IterationsMax].
func NewVerifierContext(passcode uint32, salt []byte, iterations int) (*VerifierContext, error) {
	if iterations < IterationsMin || iterations > IterationsMax {
		return nil, fmt.Errorf("%w: iterations=%d not in [%d, %d]", ErrInvalidPasscode, iterations, IterationsMin, IterationsMax)
	}
	w0, w1, err := PBKDF(passcode, salt, iterations)
	if err != nil {
		return nil, err
	}
	// w1 is needed for L only: the verifier keeps (w0, L) and never w1.
	w1Scalar := scalarTo32Bytes(w1)
	lPt, err := nistec.NewP256Point().ScalarBaseMult(w1Scalar)
	secretHook("context.w1", w1Scalar)
	zero(w1Scalar)
	zeroInt(w1)
	if err != nil {
		return nil, fmt.Errorf("spake2: derive L: %w", err)
	}
	l, err := ecdsa.ParseUncompressedPublicKey(curve(), lPt.Bytes())
	if err != nil {
		return nil, fmt.Errorf("spake2: encode L: %w", err)
	}
	return &VerifierContext{W0: w0, L: l}, nil
}

// VerifierW0Size is the byte length of the w0 scalar as it appears in a
// Matter PAKE passcode verifier (Matter §3.10.5): a 32-byte P-256 scalar
// (the reduced value), distinct from the wider [WSize] PBKDF2 output.
const VerifierW0Size = 32

// VerifierLSize is the byte length of the L point in a Matter PAKE
// passcode verifier: a 65-byte uncompressed P-256 point (0x04 || X || Y).
const VerifierLSize = 65

// NewVerifierFromValue builds a VerifierContext directly from a Matter
// PAKE passcode verifier: the w0 scalar (32 bytes) followed by
// L = w1·G (65-byte uncompressed P-256 point), Matter §3.10.5. This is
// the path a commissioner uses for an Enhanced Commissioning Window — it
// computes the verifier from a passcode it chose and hands the device
// only the (w0, L) pair, never the passcode, so the device can run PASE
// against the commissioner-selected passcode. Mirrors matter.js
// PaseServer.fromVerificationValue
// (packages/protocol/src/session/pase/PaseServer.ts:52-61 —
// `w0 = asBigInt(slice(0,32)); L = slice(32, 32+65)`).
//
// L is validated on the curve: ParseUncompressedPublicKey rejects
// off-curve and malformed encodings (and the point at infinity, which has
// no uncompressed encoding), guarding against invalid-curve attacks; a bad
// L returns [ErrInvalidPoint]. w0 is taken verbatim as the wire scalar,
// matching matter.js.
func NewVerifierFromValue(w0Bytes, lBytes []byte) (*VerifierContext, error) {
	if len(w0Bytes) != VerifierW0Size {
		return nil, fmt.Errorf("%w: w0 length=%d, want %d", ErrInvalidPasscode, len(w0Bytes), VerifierW0Size)
	}
	if len(lBytes) != VerifierLSize {
		return nil, fmt.Errorf("%w: L length=%d, want %d", ErrInvalidPoint, len(lBytes), VerifierLSize)
	}
	l, err := ecdsa.ParseUncompressedPublicKey(curve(), lBytes)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidPoint, err)
	}
	w0 := new(big.Int).SetBytes(w0Bytes)
	// A w0 that is zero modulo the group order makes w0·M and w0·N the
	// identity, so X = x·G + w0·M collapses to x·G and the passcode drops
	// out of the transcript entirely — the handshake would still complete,
	// against a verifier that proves nothing. The verifier is
	// commissioner-supplied (OpenCommissioningWindow carries the
	// PAKEPasscodeVerifier verbatim), so the degenerate value has to be
	// rejected here rather than reaching the Pake1 arithmetic. Every
	// verifier a conformant commissioner derives lands in [1, n-1]
	// (chip Spake2p::ComputeW0W1 reduces mod n-1 and adds 1), so this
	// rejects only malformed input.
	if new(big.Int).Mod(w0, curve().Params().N).Sign() == 0 {
		return nil, fmt.Errorf("%w: w0 is zero modulo the group order", ErrInvalidPasscode)
	}
	return &VerifierContext{W0: w0, L: l}, nil
}

// Verifier drives the device-side of PASE. Construct with
// [NewVerifier], call [Verifier.ProcessPake1] with the prover's pA,
// send the returned [Pake2Output.Y] + [Pake2Output.CB] back, then
// call [Verifier.ProcessPake3] with the prover's cA. On success
// [Verifier.SharedSecret] returns Ke.
//
// What a handshake leaves behind is overwritten as soon as it has served
// (see [zero] for what that can and cannot promise): the random scalar y
// and the points Z and V once the keys are derived, KcB once cB is
// computed, KcA once cA is compared; a cA that does not match takes Ke
// with it and ends the Verifier — it answers no second Pake3. Two things
// live on: the [VerifierContext], which is the stored verifier and serves
// every later handshake, and after a success Ke, which
// [Verifier.SharedSecret] hands out for as long as the Verifier is held.
type Verifier struct {
	ctx      *VerifierContext
	idA, idB []byte
	context  []byte
	y        *big.Int
	xMarshal []byte
	yMarshal []byte
	zMarshal []byte
	vMarshal []byte
	kcA, kcB []byte
	ke       []byte
	state    verifierState
}

type verifierState uint8

const (
	verifierStateInit verifierState = iota
	verifierStatePake1Processed
	verifierStateFinished
	// verifierStateFailed is terminal: the confirmation keys are gone, so
	// a second cA could only be compared against an overwritten key.
	verifierStateFailed
)

// NewVerifier returns a Verifier ready to process Pake1.
//
// The optional idA / idB peer-identity strings ride into the
// transcript hash. Matter sets both to empty per Core Spec §3.10.4
// — pass nil for the default. context defaults to [MatterContext].
func NewVerifier(vc *VerifierContext, idA, idB, context []byte) *Verifier {
	if context == nil {
		context = []byte(MatterContext)
	}
	return &Verifier{
		ctx:     vc,
		idA:     idA,
		idB:     idB,
		context: context,
	}
}

// Pake2Output bundles the bytes the verifier sends back to the prover
// after [Verifier.ProcessPake1].
type Pake2Output struct {
	Y  []byte // uncompressed Y point (65 bytes)
	CB []byte // confirmation tag for the prover (32 bytes)
}

// ProcessPake1 consumes the prover's pA = X and produces the
// verifier's Pake2 reply.
func (v *Verifier) ProcessPake1(pA []byte) (*Pake2Output, error) {
	if v.state != verifierStateInit {
		return nil, fmt.Errorf("%w: ProcessPake1 already called", ErrSessionState)
	}
	xPoint, err := unmarshalAndValidate(pA)
	if err != nil {
		return nil, err
	}

	// Pick random y ∈ [1, n-1].
	y, err := randomScalar()
	if err != nil {
		return nil, err
	}
	v.y = y

	yScalar := scalarTo32Bytes(y)
	w0Scalar := scalarTo32Bytes(v.ctx.W0)
	// y, Z and V serve the key derivation below and nothing after it; on
	// an error before it they are of no use either.
	defer func() {
		secretHook("verifier.y", yScalar)
		secretHook("verifier.w0", w0Scalar)
		secretHook("verifier.Z", v.zMarshal)
		secretHook("verifier.V", v.vMarshal)
		zero(yScalar)
		zero(w0Scalar)
		zeroInt(v.y)
		zero(v.zMarshal)
		zero(v.vMarshal)
		v.y, v.zMarshal, v.vMarshal = nil, nil, nil
	}()

	// Y = y·G + w0·N.
	yG, err := nistec.NewP256Point().ScalarBaseMult(yScalar)
	if err != nil {
		return nil, fmt.Errorf("%w: y·G: %w", ErrInvalidPoint, err)
	}
	w0N, err := nistec.NewP256Point().ScalarMult(nPoint, w0Scalar)
	if err != nil {
		return nil, fmt.Errorf("%w: w0·N: %w", ErrInvalidPoint, err)
	}
	v.yMarshal = nistec.NewP256Point().Add(yG, w0N).Bytes()

	// Z = y·(X - w0·M).
	w0M, err := nistec.NewP256Point().ScalarMult(mPoint, w0Scalar)
	if err != nil {
		return nil, fmt.Errorf("%w: w0·M: %w", ErrInvalidPoint, err)
	}
	xMinusW0M := nistec.NewP256Point().Add(xPoint, nistec.NewP256Point().Negate(w0M))
	// X - w0·M is the identity when the prover sends exactly X = w0·M.
	// Carrying on would fix Z and V to a value the peer chose, so the
	// transcript would no longer bind the passcode. RFC 9383 §3.3 requires
	// the abort; chip validates the peer point on the same step
	// (CHIPCryptoPAL.cpp:424 PointIsValid inside ComputeRoundTwo).
	if xMinusW0M.IsInfinity() == 1 {
		return nil, fmt.Errorf("%w: X equals w0*M (X - w0*M is the identity)", ErrInvalidPoint)
	}
	z, err := nistec.NewP256Point().ScalarMult(xMinusW0M, yScalar)
	if err != nil {
		return nil, fmt.Errorf("%w: y·(X-w0·M): %w", ErrInvalidPoint, err)
	}
	v.zMarshal = z.Bytes()

	// V = y·L.
	lBytes, err := v.ctx.L.Bytes()
	if err != nil {
		return nil, fmt.Errorf("%w: encode L: %w", ErrInvalidPoint, err)
	}
	lPt, err := nistec.NewP256Point().SetBytes(lBytes)
	if err != nil {
		return nil, fmt.Errorf("%w: L: %w", ErrInvalidPoint, err)
	}
	vPt, err := nistec.NewP256Point().ScalarMult(lPt, yScalar)
	if err != nil {
		return nil, fmt.Errorf("%w: y·L: %w", ErrInvalidPoint, err)
	}
	v.vMarshal = vPt.Bytes()

	v.xMarshal = pA
	v.deriveKeys()

	cB := hmacSHA256(v.kcB, v.xMarshal)
	zero(v.kcB)
	v.state = verifierStatePake1Processed
	return &Pake2Output{Y: v.yMarshal, CB: cB[:ConfirmTagSize]}, nil
}

// ProcessPake3 verifies the prover's cA tag and finalises the
// session. On success [Verifier.SharedSecret] returns the 16-byte Ke. A
// cA that does not match is final: every later call returns
// [ErrSessionState].
func (v *Verifier) ProcessPake3(cA []byte) error {
	if v.state != verifierStatePake1Processed {
		return fmt.Errorf("%w: ProcessPake1 must run first", ErrSessionState)
	}
	expected := hmacSHA256(v.kcA, v.yMarshal)
	zero(v.kcA)
	if subtle.ConstantTimeCompare(cA, expected[:ConfirmTagSize]) != 1 {
		// Ke goes with the failed handshake, and the Verifier with it:
		// KcA is overwritten, so nothing may be compared against it again.
		zero(v.ke)
		v.state = verifierStateFailed
		return ErrConfirmationFailed
	}
	v.state = verifierStateFinished
	return nil
}

// SharedSecret returns the 16-byte session key (Ke). Valid only
// after a successful [Verifier.ProcessPake3].
func (v *Verifier) SharedSecret() []byte {
	if v.state != verifierStateFinished {
		return nil
	}
	out := make([]byte, len(v.ke))
	copy(out, v.ke)
	return out
}

// Prover drives the commissioner-side of PASE. The flow mirrors
// [Verifier]: derive (w0, w1), call [Prover.GeneratePake1] to produce
// pA (the bytes sent on the wire), then call [Prover.ProcessPake2]
// with the verifier's reply. On success [Prover.SharedSecret] returns
// the same 16-byte Ke as the verifier and [Prover.Pake3] returns the
// final cA bytes.
//
// w0, w1 and the random scalar x are the passcode's equivalent for this
// handshake; [Prover.ProcessPake2] overwrites them on every way out, with
// Z, V and the confirmation keys (see [zero]). After a success only Ke
// lives on, for [Prover.SharedSecret]; after any error past the state
// check nothing does, and the Prover answers no second Pake2.
type Prover struct {
	w0, w1   *big.Int
	idA, idB []byte
	context  []byte
	x        *big.Int
	xMarshal []byte
	yMarshal []byte
	zMarshal []byte
	vMarshal []byte
	kcA, kcB []byte
	ke       []byte
	state    proverState
}

type proverState uint8

const (
	proverStateInit proverState = iota
	proverStatePake1Generated
	proverStateFinished
	// proverStateFailed is terminal: the scalars are gone.
	proverStateFailed
)

// NewProver returns a Prover ready to compute Pake1.
func NewProver(passcode uint32, salt []byte, iterations int, idA, idB, context []byte) (*Prover, error) {
	w0, w1, err := PBKDF(passcode, salt, iterations)
	if err != nil {
		return nil, err
	}
	if context == nil {
		context = []byte(MatterContext)
	}
	return &Prover{
		w0: w0, w1: w1,
		idA: idA, idB: idB,
		context: context,
	}, nil
}

// GeneratePake1 returns pA = X for transmission to the verifier.
func (p *Prover) GeneratePake1() ([]byte, error) {
	if p.state != proverStateInit {
		return nil, fmt.Errorf("%w: GeneratePake1 already called", ErrSessionState)
	}
	x, err := randomScalar()
	if err != nil {
		return nil, err
	}
	p.x = x
	xScalar, w0Scalar := scalarTo32Bytes(x), scalarTo32Bytes(p.w0)
	defer func() {
		secretHook("prover.x", xScalar)
		secretHook("prover.w0", w0Scalar)
		zero(xScalar)
		zero(w0Scalar)
	}()
	// X = x·G + w0·M.
	xG, err := nistec.NewP256Point().ScalarBaseMult(xScalar)
	if err != nil {
		return nil, fmt.Errorf("%w: x·G: %w", ErrInvalidPoint, err)
	}
	w0M, err := nistec.NewP256Point().ScalarMult(mPoint, w0Scalar)
	if err != nil {
		return nil, fmt.Errorf("%w: w0·M: %w", ErrInvalidPoint, err)
	}
	p.xMarshal = nistec.NewP256Point().Add(xG, w0M).Bytes()
	p.state = proverStatePake1Generated
	return p.xMarshal, nil
}

// ProcessPake2 consumes the verifier's Y + cB. Returns the prover's
// cA on success; on cB mismatch returns [ErrConfirmationFailed]
// without exposing Ke.
func (p *Prover) ProcessPake2(yBytes, cB []byte) (cA []byte, err error) {
	if p.state != proverStatePake1Generated {
		return nil, fmt.Errorf("%w: GeneratePake1 must run first", ErrSessionState)
	}
	xScalar, w0Scalar, w1Scalar := scalarTo32Bytes(p.x), scalarTo32Bytes(p.w0), scalarTo32Bytes(p.w1)
	// Every way out of here ends the use of x, w0, w1, Z, V and the
	// confirmation keys; one that is not the success takes Ke as well and
	// leaves the Prover failed.
	defer func() {
		secretHook("prover.x", xScalar)
		secretHook("prover.w0", w0Scalar)
		secretHook("prover.w1", w1Scalar)
		secretHook("prover.Z", p.zMarshal)
		secretHook("prover.V", p.vMarshal)
		zero(xScalar)
		zero(w0Scalar)
		zero(w1Scalar)
		zeroInt(p.x)
		zeroInt(p.w0)
		zeroInt(p.w1)
		zero(p.zMarshal)
		zero(p.vMarshal)
		zero(p.kcA)
		zero(p.kcB)
		p.x, p.w0, p.w1, p.zMarshal, p.vMarshal = nil, nil, nil, nil, nil
		if err != nil {
			zero(p.ke)
			p.state = proverStateFailed
		}
	}()
	yPoint, err := unmarshalAndValidate(yBytes)
	if err != nil {
		return nil, err
	}

	// Z = x·(Y - w0·N).
	w0N, err := nistec.NewP256Point().ScalarMult(nPoint, w0Scalar)
	if err != nil {
		return nil, fmt.Errorf("%w: w0·N: %w", ErrInvalidPoint, err)
	}
	yMinusW0N := nistec.NewP256Point().Add(yPoint, nistec.NewP256Point().Negate(w0N))
	// Mirror image of the identity guard in [Verifier.ProcessPake1]: a peer
	// that answers with exactly Y = w0·N makes Y - w0·N the identity, and Z
	// and V would collapse to a value the peer chose, so the transcript
	// would stop binding the passcode. RFC 9383 §3.3 requires the abort for
	// both roles; chip runs the same peer-point validation on the prover
	// side (CHIPCryptoPAL.cpp:424 PointIsValid inside ComputeRoundTwo,
	// which serves PROVER and VERIFIER alike).
	if yMinusW0N.IsInfinity() == 1 {
		return nil, fmt.Errorf("%w: Y equals w0*N (Y - w0*N is the identity)", ErrInvalidPoint)
	}
	z, err := nistec.NewP256Point().ScalarMult(yMinusW0N, xScalar)
	if err != nil {
		return nil, fmt.Errorf("%w: x·(Y-w0·N): %w", ErrInvalidPoint, err)
	}
	p.zMarshal = z.Bytes()

	// V = w1·(Y - w0·N).
	vPt, err := nistec.NewP256Point().ScalarMult(yMinusW0N, w1Scalar)
	if err != nil {
		return nil, fmt.Errorf("%w: w1·(Y-w0·N): %w", ErrInvalidPoint, err)
	}
	p.vMarshal = vPt.Bytes()

	p.yMarshal = yBytes
	p.deriveKeys()

	expected := hmacSHA256(p.kcB, p.xMarshal)
	if subtle.ConstantTimeCompare(cB, expected[:ConfirmTagSize]) != 1 {
		// The deferred overwrite takes Ke with the rest, so nothing of a
		// handshake whose confirmation failed can be probed.
		return nil, ErrConfirmationFailed
	}

	cA = hmacSHA256(p.kcA, p.yMarshal)[:ConfirmTagSize]
	p.state = proverStateFinished
	return cA, nil
}

// SharedSecret returns the 16-byte session key (Ke). Valid only after
// a successful [Prover.ProcessPake2].
func (p *Prover) SharedSecret() []byte {
	if p.state != proverStateFinished {
		return nil
	}
	out := make([]byte, len(p.ke))
	copy(out, p.ke)
	return out
}

// zero overwrites b. It is how this package ends the life of handshake
// key material — the scalars, the points Z and V, the transcript and what
// the key schedule derives from it — once the value has served or the
// handshake has failed; connectedhomeip does the same in Spake2p's Clear
// and with its SensitiveDataBuffer (src/crypto/CHIPCryptoPALOpenSSL.cpp:
// 1119-1154, src/crypto/CHIPCryptoPAL.h:299-306 at 6170af84), matter.js
// does not (packages/protocol/src/session/pase/PaseServer.ts:174-190 at
// db021da0 hands Ke on and drops the rest to the collector).
//
// It is a best effort, and Go sets its limit: the runtime gives no
// guarantee that the bytes overwritten here were the only copy. Values
// are copied when a goroutine's stack grows and when they are passed by
// value; math/big, crypto/hmac, crypto/hkdf, crypto/pbkdf2 and the nistec
// points keep working state of their own that nothing outside them can
// reach; and a slice the caller was handed ([Verifier.SharedSecret]) is
// the caller's. What is overwritten is every buffer this package holds a
// reference to.
func zero(b []byte) {
	clear(b)
}

// zeroInt overwrites the words of n, capacity included, and leaves it 0.
// The limit stated at [zero] applies: math/big may have left earlier
// copies of the value in buffers it grew out of.
func zeroInt(n *big.Int) {
	if n == nil {
		return
	}
	words := n.Bits()
	clear(words[:cap(words)])
	n.SetInt64(0)
}

// secretHook is the test seam that sees a short-lived secret buffer just
// before it is overwritten, so a test can hold the slice and read it
// afterwards. Production code never replaces it.
var secretHook = func(string, []byte) {}

// --- shared key-derivation pipeline ---

func (v *Verifier) deriveKeys() {
	v.kcA, v.kcB, v.ke = deriveKeys(v.context, v.idA, v.idB, v.xMarshal, v.yMarshal, v.zMarshal, v.vMarshal, v.ctx.W0)
}

func (p *Prover) deriveKeys() {
	p.kcA, p.kcB, p.ke = deriveKeys(p.context, p.idA, p.idB, p.xMarshal, p.yMarshal, p.zMarshal, p.vMarshal, p.w0)
}

// deriveKeys computes the SPAKE2+ key schedule per Matter Core Spec
// §3.10.4 / RFC 9383 §3.4:
//
//	K_main = SHA-256(TT)
//	Ka = K_main[0:16]
//	Ke = K_main[16:32]
//	KcA || KcB = HKDF(salt=nil, IKM=Ka, info="ConfirmationKeys", L=32)
//
// where HKDF here is the full Extract+Expand pipeline per RFC 5869:
// HKDF-Extract maps the 16-byte Ka through HMAC-SHA-256 (with empty
// salt) into a 32-byte PRK before HKDF-Expand produces the
// confirmation key material. Skipping Extract (passing Ka directly
// to HKDF-Expand) silently produces wrong output bytes — chip-tool's
// prover then rejects our Pake2 with CHIP_ERROR_INTERNAL because
// our cB tag is unrelated to its locally-computed expectation.
func deriveKeys(context, idA, idB, x, y, z, v []byte, w0 *big.Int) (kcA, kcB, ke []byte) {
	tt := buildTranscript(context, idA, idB, x, y, z, v, w0)
	// Summed into a slice, not sha256.Sum256's array: that would be a
	// second copy, returned by value, for the overwrite below to miss.
	h := sha256.New()
	h.Write(tt)
	kMain := h.Sum(nil)
	ka := kMain[0:16]
	ke = make([]byte, 16)
	copy(ke, kMain[16:32])
	var confKeys []byte
	defer func() {
		secretHook("transcript", tt)
		secretHook("kMain", kMain)
		secretHook("confirmationKeys", confKeys)
		zero(tt)
		zero(kMain)
		zero(confKeys)
	}()

	// connectedhomeip's Spake2p::GenerateKeys (CHIPCryptoPAL.cpp):
	//   KDF(Ka, hash_size/2=16, nullptr, 0,
	//       "ConfirmationKeys", 16, Kcab, hash_size=32)
	//   then split Kcab into KcA (first 16) || KcB (second 16).
	// hkdf.Key does HKDF-Extract(salt=nil, IKM=Ka) followed by
	// HKDF-Expand(PRK, info, L=32) — equivalent to chip-tool's KDF.
	var err error
	confKeys, err = hkdf.Key(sha256.New, ka, nil, "ConfirmationKeys", 32)
	if err != nil {
		// invariant: hkdf.Key only errors when the requested output
		// length exceeds the hash's expansion limit (255 * 32 bytes for
		// SHA-256) — the L=32 here is a fixed literal, so this can only
		// fail on a code change to that constant, never on peer-
		// supplied PAKE parameters (ka is always a fixed 16-byte slice
		// of our own SHA-256 transcript hash regardless of what the
		// peer sent).
		panic(fmt.Sprintf("spake2: hkdf.Key: %v", err))
	}
	kcA = make([]byte, 16)
	kcB = make([]byte, 16)
	copy(kcA, confKeys[0:16])
	copy(kcB, confKeys[16:32])
	return kcA, kcB, ke
}

// buildTranscript assembles TT per Matter §3.10.4 / RFC 9383 §3.3:
// each field is length-prefixed (8-byte little-endian length).
//
// The w0 scalar is encoded as a fixed-length [ScalarSize]-byte (32)
// big-endian unsigned integer — connectedhomeip's Spake2p uses
// `FEWrite(w0, point_buffer, fe_size=32)` regardless of Matter
// §3.10.4's published "40-byte" wording. The 40-byte width applies
// only to the pre-reduction PBKDF2 output, not to the post-mod-n
// scalar that participates in the transcript hash.
func buildTranscript(context, idA, idB, x, y, z, v []byte, w0 *big.Int) []byte {
	mBytes := mPoint.Bytes()
	nBytes := nPoint.Bytes()

	w0Bytes := scalarTo32Bytes(w0)
	defer zero(w0Bytes)

	parts := [][]byte{context, idA, idB, mBytes, nBytes, x, y, z, v, w0Bytes}
	size := 0
	for _, part := range parts {
		size += 8 + len(part)
	}
	// Sized once: a buffer that grew would leave Z, V and w0 behind in
	// the arrays it outgrew, where the caller's overwrite cannot reach.
	out := make([]byte, 0, size)
	for _, part := range parts {
		out = binary.LittleEndian.AppendUint64(out, uint64(len(part)))
		out = append(out, part...)
	}
	return out
}

// scalarTo32Bytes returns a left-zero-padded 32-byte big-endian
// representation of n.
func scalarTo32Bytes(n *big.Int) []byte {
	out := make([]byte, ScalarSize)
	b := n.Bytes()
	copy(out[ScalarSize-len(b):], b)
	return out
}

// --- helpers ---

// randomScalarFn is the indirection [randomScalar] dispatches through
// so unit tests can substitute a deterministic scalar for golden-
// vector validation. Production code never replaces it; the variable
// is unexported and the test seam lives in spake2_test.go alongside
// the [randomScalar] consumers.
var randomScalarFn = defaultRandomScalar

// randomScalar returns a uniformly-distributed scalar in [1, n-1]
// where n is the P-256 group order. Tests may override
// [randomScalarFn] to inject a known value.
func randomScalar() (*big.Int, error) {
	return randomScalarFn()
}

func defaultRandomScalar() (*big.Int, error) {
	n := curve().Params().N
	for range 16 {
		// Sample 32 random bytes, map mod n; reject zero.
		k, err := rand.Int(rand.Reader, n)
		if err != nil {
			return nil, fmt.Errorf("spake2: rand.Int: %w", err)
		}
		if k.Sign() != 0 {
			return k, nil
		}
	}
	return nil, errors.New("spake2: failed to sample non-zero scalar")
}

// unmarshalAndValidate decodes an uncompressed P-256 point and
// rejects the identity element / off-curve points. SPAKE2+ requires
// the peer's contribution to be a valid non-identity point or the
// session is trivially attackable.
func unmarshalAndValidate(b []byte) (*nistec.P256Point, error) {
	// The length/prefix check stays ahead of SetBytes so the identity —
	// whose encoding is the single byte 0x00 — is rejected as a bad
	// encoding rather than decoded and then caught below.
	if len(b) != PointSize || b[0] != 0x04 {
		return nil, fmt.Errorf("%w: bad encoding", ErrInvalidPoint)
	}
	pt, err := nistec.NewP256Point().SetBytes(b)
	if err != nil {
		return nil, fmt.Errorf("%w: not on curve: %w", ErrInvalidPoint, err)
	}
	if pt.IsInfinity() == 1 {
		return nil, fmt.Errorf("%w: identity element", ErrInvalidPoint)
	}
	return pt, nil
}

// hmacSHA256 returns the full 32-byte HMAC-SHA-256 over data. Callers
// truncate to [ConfirmTagSize] when needed.
func hmacSHA256(key, data []byte) []byte {
	h := hmac.New(sha256.New, key)
	h.Write(data)
	return h.Sum(nil)
}

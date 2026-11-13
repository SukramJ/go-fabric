// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package commissioning

import (
	"crypto/hkdf"
	"crypto/sha256"
	"errors"
	"fmt"

	"github.com/SukramJ/go-fabric/secure/channel"
	"github.com/SukramJ/go-fabric/secure/spake2"
)

// PASEConfig drives a PASE-responder run.
type PASEConfig struct {
	// Passcode is the 27-bit setup code (Matter §5.1.6.4) the
	// operator entered into the commissioner. Always between
	// 00000001 and 99999998 inclusive (per spec); 00000000 and
	// every-bit-on values are reserved.
	Passcode uint32
	// Salt is the PBKDF2 salt persisted alongside the passcode.
	// 16..32 bytes per Matter §3.10.
	Salt []byte
	// Iterations is the PBKDF2 iteration count. Matter requires
	// 1000..100000 (§3.10).
	Iterations int
	// LocalNodeID is the bridge's transient PASE node identifier.
	// Per Matter §4.13.2 the responder uses a random "ephemeral"
	// node id during PASE.
	LocalNodeID uint64
	// PeerNodeID is the commissioner's transient PASE node
	// identifier.
	PeerNodeID uint64
	// IDA / IDB are the PASE identifiers (commonly "CHIP_PAKE_ID_A"
	// and "CHIP_PAKE_ID_B").
	IDA []byte
	IDB []byte
}

// Errors.
var (
	// ErrPASEStateMismatch is returned when methods are invoked out
	// of order (e.g. Pake3 before Pake1).
	ErrPASEStateMismatch = errors.New("commissioning: PASE state mismatch")
	// ErrPASEInvalidPasscode is returned when [PASEConfig.Passcode]
	// is outside the allowed setup-code range.
	ErrPASEInvalidPasscode = errors.New("commissioning: invalid passcode")
	// ErrPASEInvalidIterations is returned when [PASEConfig.Iterations]
	// is outside Matter's 1000..100000 range.
	ErrPASEInvalidIterations = errors.New("commissioning: invalid PBKDF2 iterations")
)

// PASEResponder runs the bridge-side of a Matter PASE handshake. The
// commissioner sends Pake1; the responder produces Pake2; the
// commissioner sends Pake3; the responder validates the confirmation
// and emits the session keys.
//
// What the handshake leaves behind: [PASEResponder.HandlePake3] derives
// the 48-byte key block from the shared secret Ke and overwrites Ke with
// the rest of the SPAKE2+ state, whether the confirmation held or not. The
// block — the two session keys and the attestation challenge, the
// handshake's result — lives on in the responder, because
// [PASEResponder.Session] and [PASEResponder.AttestationChallenge] hand it
// out on every call, until [PASEResponder.Close]. Call Close once the
// session and the challenge are taken, and on a handshake that is given
// up. All of it is a best effort within Go's limits (secure/spake2, zero).
type PASEResponder struct {
	cfg       PASEConfig
	verifier  *spake2.Verifier
	pake1Done bool
	finished  bool
	closed    bool
	// keys is the PASE key block, I2RKey || R2IKey || AttestationChallenge:
	// set by a successful HandlePake3, overwritten by Close.
	keys []byte
}

// NewPASEResponder constructs a responder with cfg validated. The
// PBKDF / SPAKE2+ initialisation runs eagerly so Pake1 processing is
// fast.
func NewPASEResponder(cfg PASEConfig) (*PASEResponder, error) {
	if cfg.Passcode < 1 || cfg.Passcode > 99999998 {
		// The value is not rendered: the text reaches a log, and a
		// passcode that fails the range is most often a real one mistyped.
		return nil, fmt.Errorf("%w: outside 1..99999998", ErrPASEInvalidPasscode)
	}
	if cfg.Iterations < 1000 || cfg.Iterations > 100000 {
		return nil, fmt.Errorf("%w: %d", ErrPASEInvalidIterations, cfg.Iterations)
	}
	if len(cfg.Salt) < 16 || len(cfg.Salt) > 32 {
		return nil, fmt.Errorf("commissioning: salt length=%d (want 16..32)", len(cfg.Salt))
	}

	vc, err := spake2.NewVerifierContext(cfg.Passcode, cfg.Salt, cfg.Iterations)
	if err != nil {
		return nil, fmt.Errorf("commissioning: PASE verifier context: %w", err)
	}
	return &PASEResponder{
		cfg:      cfg,
		verifier: spake2.NewVerifier(vc, cfg.IDA, cfg.IDB, nil),
	}, nil
}

// HandlePake1 consumes the commissioner's Pake1 message and returns
// the bridge's Pake2 (which the caller wires onto the wire). Must be
// called before HandlePake3.
func (r *PASEResponder) HandlePake1(pake1 []byte) (*spake2.Pake2Output, error) {
	if r.pake1Done || r.finished || r.closed {
		return nil, ErrPASEStateMismatch
	}
	out, err := r.verifier.ProcessPake1(pake1)
	if err != nil {
		return nil, fmt.Errorf("commissioning: PASE Pake1: %w", err)
	}
	r.pake1Done = true
	return out, nil
}

// HandlePake3 consumes the commissioner's Pake3 message. Must be
// called after HandlePake1 returned successfully. Returns nil on
// success; on failure the responder is poisoned and must be
// discarded.
func (r *PASEResponder) HandlePake3(pake3 []byte) error {
	if !r.pake1Done || r.finished || r.closed {
		return ErrPASEStateMismatch
	}
	// The verifier has served after this step on every way out: a cA that
	// does not match ends it, and one that does leaves only Ke, which the
	// key block below is derived from once.
	defer r.verifier.Close()
	if err := r.verifier.ProcessPake3(pake3); err != nil {
		return fmt.Errorf("commissioning: PASE Pake3: %w", err)
	}
	keys, err := r.sessionKeys()
	if err != nil {
		return err
	}
	r.keys = keys
	r.finished = true
	return nil
}

// Close ends the responder and overwrites the key material it still
// holds: the key block after a finished handshake, the SPAKE2+ state of
// one that was given up. A [channel.Session] and a challenge taken before
// are their holders' and stay as they are. Afterwards every method returns
// [ErrPASEStateMismatch]; a second Close does nothing.
func (r *PASEResponder) Close() {
	r.verifier.Close()
	clear(r.keys)
	r.keys = nil
	r.closed = true
}

// Session derives the AES-CCM session keys and constructs a
// [channel.Session]. Must be called only after HandlePake3 returned
// nil. The responder encrypts with R2I and decrypts with I2R — the
// same orientation [secure/operational.Manager.OpenFromPase] gives the
// running bridge, and the one matter.js PaseServer.ts:184-192 hands to
// NodeSession.ts:80-90 (`isInitiator: false`).
//
// The session copies both keys (channel.New), so it outlives
// [PASEResponder.Close].
func (r *PASEResponder) Session() (*channel.Session, error) {
	if !r.finished || r.closed {
		return nil, ErrPASEStateMismatch
	}
	return channel.New(channel.Config{
		EncryptKey:  r.keys[16:32], // R2IKey
		DecryptKey:  r.keys[0:16],  // I2RKey
		LocalNodeID: r.cfg.LocalNodeID,
		PeerNodeID:  r.cfg.PeerNodeID,
	})
}

// AttestationChallenge returns the 16-byte attestation challenge for
// downstream Attestation / CSR signing. It is the third slice of the
// same HKDF output the session keys come from, not a derivation of its
// own: a challenge from a second HKDF with its own info string is one
// signed with it fails the commissioner's check.
//
// The slice is a copy and the caller's; it outlives [PASEResponder.Close].
func (r *PASEResponder) AttestationChallenge() ([]byte, error) {
	if !r.finished || r.closed {
		return nil, ErrPASEStateMismatch
	}
	return append([]byte(nil), r.keys[32:48]...), nil
}

// sessionKeys runs the one PASE key schedule of Matter §4.13.2.5 /
// §4.13.4.2: HKDF-SHA256(IKM=Ke, salt="", info="SessionKeys", L=48),
// laid out I2RKey (16) || R2IKey (16) || AttestationChallenge (16).
// Mirrors matter.js NodeSession.ts:80-90 (`createHkdfKey(sharedSecret,
// salt, SESSION_KEYS_INFO, 16*3)`, `attestationKey = keys.slice(32, 48)`)
// and this module's secure/operational OpenFromPase. It runs once, from
// HandlePake3: the copy of Ke it takes is overwritten here, the verifier's
// own by the Close that follows.
func (r *PASEResponder) sessionKeys() ([]byte, error) {
	shared := r.verifier.SharedSecret()
	if len(shared) == 0 {
		return nil, errors.New("commissioning: PASE shared secret unavailable")
	}
	defer clear(shared)
	out, err := hkdf.Key(sha256.New, shared, nil, "SessionKeys", 48)
	if err != nil {
		return nil, fmt.Errorf("commissioning: PASE key derive: %w", err)
	}
	return out, nil
}

// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package sigma

// zero overwrites b. It is how this package ends the life of CASE key
// material: the keys S2K, S3K and the resumption keys once the one message
// each protects is sealed or opened, the HKDF salts that carry the IPK,
// the 48 bytes the session keys are cut from, and — when a handshake
// fails or the responder is reset for another — the ECDH shared secret.
// connectedhomeip does the same through its types: the salts are
// SensitiveDataFixedBuffer and the keys AutoReleaseSessionKey
// (src/protocols/secure_channel/CASESession.cpp:1201, :1207 at 6170af84,
// released by src/crypto/SessionKeystore.h:164 into
// src/crypto/RawKeySessionKeystore.cpp:96-99), the shared secret a
// SensitiveDataBuffer (src/crypto/CHIPCryptoPAL.h:428, :302-306). matter.js
// leaves all of them to the collector
// (packages/protocol/src/session/case/CaseServer.ts:243-285 at db021da0).
//
// Two things are not overwritten, because they must live on. The session
// keys are the handshake's result ([Responder.SessionKeys],
// [Initiator.Result]). And the shared secret of a finished handshake is
// the resumption record the caller stores next ([Responder.ECDHSharedSecret],
// [InitiatorResult.SharedSecret]): it stays for as long
// as the Responder or Initiator is held, and a record handed in through a
// [ResumptionStore] or [InitiatorConfig] stays its owner's.
//
// It is a best effort, and Go sets its limit: the runtime gives no
// guarantee that the bytes overwritten here were the only copy. Values
// are copied when a goroutine's stack grows and when they are passed by
// value; crypto/ecdh keeps the ephemeral private key and crypto/aes the
// expanded key schedule where nothing outside them can reach — the
// reference to the ephemeral key is dropped once the shared secret
// exists, which is all that can be done about it; crypto/hkdf and
// crypto/sha256 keep working state of their own.
//
// So what is overwritten is the short-lived material of the first
// paragraph, in every buffer this package holds a reference to — not every
// secret it holds. The two results above stay for as long as the Responder
// or Initiator is held, and the [Identity] it was configured with — the
// IPK and the signing key — serves every handshake and is its owner's.
func zero(b []byte) {
	clear(b)
}

// secretHook is the test seam that sees a short-lived secret buffer just
// before it is overwritten, so a test can hold the slice and read it
// afterwards. Production code never replaces it.
var secretHook = func(string, []byte) {}

// scrub collects the short-lived key material of one handshake step, so
// that one deferred call overwrites all of it on every way out.
type scrub struct {
	names []string
	bufs  [][]byte
}

// add registers b under name and returns it.
func (s *scrub) add(name string, b []byte) []byte {
	s.names = append(s.names, name)
	s.bufs = append(s.bufs, b)
	return b
}

// wipe overwrites every registered buffer ([zero]).
func (s *scrub) wipe() {
	for i, b := range s.bufs {
		secretHook(s.names[i], b)
		zero(b)
	}
}

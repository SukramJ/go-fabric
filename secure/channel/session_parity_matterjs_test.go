// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package channel_test

// A port of matter.js packages/protocol/test/session/SecureSessionTest.ts at
// 862ad75f: the NodeSession cases — the peer's paths per invoke, decrypt and
// encrypt against the pinned vectors. The GroupSession cases live beside
// their subject in groups/session_parity_matterjs_test.go.

import (
	"encoding/hex"
	"testing"

	"github.com/SukramJ/go-fabric/client/exchange"
	"github.com/SukramJ/go-fabric/secure/channel"
	"github.com/SukramJ/go-fabric/secure/sessionparams"
	"github.com/SukramJ/go-fabric/transport/message"
)

// Vectors of SecureSessionTest.ts:28-33.
const (
	secureSessionEncryptKey       = "66951379d0a6d151cf5472cccf13f360"
	secureSessionDecryptKey       = "bacb178b2588443d5d5b1e4559e7accc"
	secureSessionEncryptedMessage = "001d350022145300ec2b931025dada82ed67521c966d2454d131a271023be699e4e2796650f568e590fd9b65f456c720a60a0da127eaa53974c5d41d3d933ed7b58a9ce5b5cb96ad94a7762611c48774cf75458327e74c34668a45dc9943546f8a6aa1dcd40bd4b8014befb49954a097a60cbdff333ee3f2fd1f49"
	secureSessionEncryptedBytes   = "1f9c4e278a2e2a755ebb4fcb9478211efb09aa9518fcafb56d74f135544636037c16fb6b62347794da0c5bde142e1a8b1cc96575e9e55471c08b58f7640b7d7f4173c8ff967c39e9961f30a29cb1f64f68df4b5bc1e742587f778eeb9ec586c162ff384558596792a2c1e43c150cd0e9ec1484c50950f17cd6c084d07caed94ce45c20004210cbde48da44ebcf7d931657f03e07e3ea29ae41868b804bf39e628323cd025507773f07268301aa1e77a82927fce041241839cee4114f6307b6befe3befde87a2d3f13eeef96b27b36e788d907b44bef2d195aa802692f4f12acc015aede3cd29da272d1e4b7f3f59683d25bf08f0e29fba2a8a9b"
	secureSessionDecryptedBytes   = "153600172403312504fcff18172402002403302404001817240200240330240401181724020024033024040218172402002403302404031817240200240328240402181724020024032824040418172403312404031818290324ff0118"
	// secureSessionPayload is MESSAGE.payload, SecureSessionTest.ts:54.
	secureSessionPayload = "1536011535012600799ac60c37012402002403312404031824021418181535012600b771f32f3701240200240328240404182502018018181535012600b771f32f3701240200240328240402182502f1ff18181535012600ddad82d637012402002403302404031824020218181535012600ddad82d637012402002403302404021824020018181535012600ddad82d6370124020024033024040118350224003c1818181535012600ddad82d637012402002403302404001824020018181535012600799ac60c37012402002403312504fcff18240201181818290424ff0118"
)

// secureSession is SecureSessionTest.ts:59-71: node ids unspecified (0), peer
// session id 0x8d4b. initialCounter primes the outbound counter, which the
// Go Session numbers itself.
func secureSession(t *testing.T, initialCounter uint32) *channel.Session {
	t.Helper()
	s, err := channel.New(channel.Config{
		EncryptKey: mustHex(t, secureSessionEncryptKey), DecryptKey: mustHex(t, secureSessionDecryptKey),
		PeerSessionID: 0x8d4b, InitialCounter: initialCounter,
	})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestSecureSessionParameters(t *testing.T) {
	t.Parallel()
	// The Go session takes its peer parameters already resolved: every
	// constructor feeds them through exchange.PeerParameters, the matter.js
	// SessionParameters factory NodeSession applies (NodeSession.ts).
	t.Run("assumes one path per invoke for a peer that reports zero", func(t *testing.T) {
		t.Parallel()
		p := exchange.PeerParameters(&sessionparams.Parameters{MaxPathsPerInvoke: 0})
		if p.MaxPathsPerInvoke != 1 {
			t.Fatalf("MaxPathsPerInvoke = %d, want 1", p.MaxPathsPerInvoke)
		}
	})
}

func TestSecureSessionDecrypt(t *testing.T) {
	t.Parallel()
	t.Run("decrypts a message", func(t *testing.T) {
		t.Parallel()
		wire := mustHex(t, secureSessionEncryptedMessage)
		hdr, n, err := message.UnmarshalHeader(wire)
		if err != nil {
			t.Fatal(err)
		}
		plain, dup, err := secureSession(t, 1).Decrypt(&hdr, hdr.NonceSecurityFlags(), wire[n:])
		if err != nil || dup {
			t.Fatalf("Decrypt: dup=%v err=%v", dup, err)
		}
		// result.payload is the application payload after the payload header.
		_, m, err := message.UnmarshalProtocolHeader(plain)
		if err != nil {
			t.Fatal(err)
		}
		if got := hex.EncodeToString(plain[m:]); got != secureSessionDecryptedBytes {
			t.Fatalf("payload = %s\nwant      %s", got, secureSessionDecryptedBytes)
		}
	})
}

func TestSecureSessionEncrypt(t *testing.T) {
	t.Parallel()
	t.Run("encrypts a message", func(t *testing.T) {
		t.Parallel()
		// MESSAGE, SecureSessionTest.ts:35-55. messageId 12519906 is the
		// counter the session hands out first.
		s := secureSession(t, 12519906)
		// NodeSession.encode stamps the peer session id over 0x351d
		// (NodeSession.ts:219); the Go caller does the same.
		hdr := message.Header{SessionID: s.PeerSessionID(), SessionType: message.SessionUnsecured} // type 0: unicast
		ph := message.ProtocolHeader{
			NeedsAck: true, HasAck: true, AckCounter: 0x00531422,
			Opcode: 0x05, ExchangeID: 0x048d, ProtocolID: 0x0001,
		}
		plain := append(ph.Marshal(), mustHex(t, secureSessionPayload)...)
		res, err := s.Encrypt(&hdr, hdr.SecurityFlags(), plain)
		if err != nil {
			t.Fatal(err)
		}
		if res.Counter != 12519906 {
			t.Fatalf("counter = %d", res.Counter)
		}
		if got := hex.EncodeToString(res.Ciphertext); got != secureSessionEncryptedBytes {
			t.Fatalf("applicationPayload = %s\nwant               %s", got, secureSessionEncryptedBytes)
		}
	})
}

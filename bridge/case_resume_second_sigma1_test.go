// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"crypto/ecdh"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock/clocktest"
	"github.com/SukramJ/go-fabric/secure/aesccm"
	"github.com/SukramJ/go-fabric/secure/sigma"
	"github.com/SukramJ/go-fabric/tlv"
	"github.com/SukramJ/go-fabric/transport/mrp"
)

// resumingSigma1 is a Sigma1 resuming from the record (secret, rid) with a
// fresh initiator random: every call yields other bytes of one resumption.
func resumingSigma1(t *testing.T, secret, rid []byte) []byte {
	t.Helper()
	var random [32]byte
	if _, err := rand.Read(random[:]); err != nil {
		t.Fatal(err)
	}
	salt := append(append([]byte(nil), random[:]...), rid...)
	key, err := hkdf.Key(sha256.New, secret, salt, sigma.HKDFInfoSigma1Resume, 16)
	if err != nil {
		t.Fatal(err)
	}
	nonce := make([]byte, aesccm.NonceSize)
	copy(nonce, "NCASE_SigmaS1")
	cipher, err := aesccm.New(key)
	if err != nil {
		t.Fatal(err)
	}
	mic, err := cipher.Seal(nil, nonce, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	eph, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	enc := tlv.NewEncoder()
	enc.StartStruct(tlv.AnonymousTag())
	enc.PutOctets(tlv.ContextTag(1), random[:])
	enc.PutUint(tlv.ContextTag(2), 0x1234)
	enc.PutOctets(tlv.ContextTag(3), make([]byte, 32))
	enc.PutOctets(tlv.ContextTag(4), eph.PublicKey().Bytes())
	enc.PutOctets(tlv.ContextTag(6), rid)
	enc.PutOctets(tlv.ContextTag(7), mic)
	if err := enc.EndContainer(); err != nil {
		t.Fatal(err)
	}
	b, err := enc.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// TestCASEResumeSecondSigma1FailsTheFirstWait: a Sigma1 that is not a copy
// of the resuming one, arriving on the exchange while the resumed session
// waits for the initiator's Success, fails that wait — the session is closed
// by entry at once, never orphaned without its deadline; a copy of the
// resuming Sigma1 leaves the wait. matter.js reads the second Sigma1 as
// another message type in waitForSuccess (SecureChannelMessenger.ts:146-152,
// rejected at :94-98) and force-closes (CaseServer.ts:204-210 at b25ed960); a
// copy is a duplicate there and never read (ExchangeManager.ts:353-359,
// MessageExchange.ts:514-524). Fable re-read N1.
func TestCASEResumeSecondSigma1FailsTheFirstWait(t *testing.T) {
	t.Parallel()
	for name, provided := range map[string]bool{"singleton": false, "provider": true} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			clk := clocktest.New(time.Unix(1_700_000_000, 0))
			r := newUnsecuredRigOn(t, clk)
			r.b.AttachSessionRegistry(&fakeSessionRegistry{})
			ipk := newCaseTestIPK(t)
			id := newCaseTestIdentity(t, 0xBBBB, 1, ipk)
			first, secret, rid := buildResumeSigma1(t)
			rec := &completions{}
			var next atomic.Uint32
			next.Store(0x2000)
			adapter := func() *CaseAdapter {
				responder := sigma.NewResponder(id, caseTestVerifier{}, 0)
				responder.SetResumptionStore(&fakeResumptionStore{id: rid, secret: secret})
				responder.SetSessionIDRenewer(func(uint16) (uint16, bool) { return uint16(next.Add(1)), true })
				a := NewCaseAdapter(responder)
				a.SetOnSessionEstablished(func(sigma.SessionKeys, uint16) error { return nil })
				a.setOnCompletion(rec.hook)
				return a
			}
			if provided {
				r.b.AttachCaseHandlerProvider(NewPerExchangeCaseProvider(adapter).Resolve)
			} else {
				r.b.AttachCaseHandler(adapter())
			}
			p := newPipelinePeer(t, r.b, 0xD00D)
			if err := p.send(mrp.SCOpcodeSigma1, 0x0400, first, nil); err != nil {
				t.Fatal(err)
			}
			_, counter, _ := p.recv(mrp.SCOpcodeSigma2Resume, "Sigma2_Resume")
			// A copy of the resuming Sigma1 — its retransmission, or Apple's
			// over another interface, under the same message counter —
			// leaves the wait.
			p.counter--
			if err := p.send(mrp.SCOpcodeSigma1, 0x0400, first, nil); err != nil {
				t.Fatal(err)
			}
			if got := rec.snapshot(); len(got) != 0 {
				t.Fatalf("completions %+v after a copy of the resuming Sigma1, want none", got)
			}
			if err := p.send(mrp.StandaloneAckOpcode, 0x0400, nil, &counter); err != nil {
				t.Fatal(err)
			}
			if provided {
				// The provider's adapter still holds the Sigma1's record
				// after the acknowledgement: a copy of the same bytes under
				// a new counter is recognised by them and leaves the wait
				// too (BD-Matter-CASE-ResumeWaitSigma1Copies).
				if err := p.send(mrp.SCOpcodeSigma1, 0x0400, first, nil); err != nil {
					t.Fatal(err)
				}
				if got := rec.snapshot(); len(got) != 0 {
					t.Fatalf("completions %+v after the same bytes again, want none", got)
				}
			}
			if err := p.send(mrp.SCOpcodeSigma1, 0x0400, resumingSigma1(t, secret, rid), nil); err != nil {
				t.Fatal(err)
			}
			if got := rec.snapshot(); len(got) == 0 || got[0].sessionID != 0x2001 || got[0].completed {
				t.Fatalf("completions %+v at the second Sigma1, want session 0x2001 failed first", got)
			}
		})
	}
}

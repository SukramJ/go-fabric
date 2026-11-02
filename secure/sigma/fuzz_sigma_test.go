// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package sigma

import (
	"bytes"
	"testing"

	"github.com/SukramJ/go-fabric/secure/sessionparams"
)

// FuzzUnmarshalSigma1 feeds arbitrary bytes to the Sigma1 decoder — the
// first attacker-controlled TLV a CASE responder parses, before any
// authentication, with the session-parameter struct nested in it
// (W6-test-06). It must not panic, and a Sigma1 it accepts must survive
// Marshal: the re-encoded message decodes again and encodes to the same
// bytes.
func FuzzUnmarshalSigma1(f *testing.F) {
	full := Sigma1{
		InitiatorSessionID: 0x1234,
		InitiatorEphPubKey: bytes.Repeat([]byte{0x04}, 65),
		ResumptionID:       bytes.Repeat([]byte{0x11}, 16),
		InitiatorResumeMIC: bytes.Repeat([]byte{0x22}, 16),
		InitiatorSessionParams: &sessionparams.Parameters{
			SessionIdleInterval:   500,
			SessionActiveInterval: 300,
		},
	}
	f.Add(full.Marshal())
	f.Add(Sigma1{InitiatorSessionID: 1, InitiatorEphPubKey: bytes.Repeat([]byte{0x04}, 65)}.Marshal())
	f.Add([]byte{})
	f.Add([]byte{0x15, 0x18})
	f.Fuzz(func(t *testing.T, data []byte) {
		s, err := UnmarshalSigma1(data)
		if err != nil {
			return
		}
		enc := s.Marshal()
		again, err := UnmarshalSigma1(enc)
		if err != nil {
			t.Fatalf("a decoded Sigma1 re-encodes to bytes the decoder refuses: %v", err)
		}
		if !bytes.Equal(again.Marshal(), enc) {
			t.Fatalf("Sigma1 round trip is not stable: %x", data)
		}
	})
}

// FuzzUnmarshalSigma3 is FuzzUnmarshalSigma1 for Sigma3, which a responder
// decodes before it can verify anything the initiator sent.
func FuzzUnmarshalSigma3(f *testing.F) {
	f.Add(Sigma3{Encrypted3: bytes.Repeat([]byte{0x33}, 120)}.Marshal())
	f.Add([]byte{})
	f.Add([]byte{0x15, 0x30, 0x01, 0x00, 0x18})
	f.Fuzz(func(t *testing.T, data []byte) {
		s, err := UnmarshalSigma3(data)
		if err != nil {
			return
		}
		enc := s.Marshal()
		again, err := UnmarshalSigma3(enc)
		if err != nil {
			t.Fatalf("a decoded Sigma3 re-encodes to bytes the decoder refuses: %v", err)
		}
		if !bytes.Equal(again.Marshal(), enc) {
			t.Fatalf("Sigma3 round trip is not stable: %x", data)
		}
	})
}

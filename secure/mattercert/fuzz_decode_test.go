// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package mattercert

import (
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"reflect"
	"testing"
)

// Seed corpus. Long seeds live in testdata as a JSON array of
// {label, description, bytesHex} records — the shape the wire-fixture
// files in tlv/ and im/ already use — so a reviewer can see what each
// seed is without decoding a Go byte literal.
//
//go:embed testdata/mattercert-fuzz-seeds.json
var fuzzSeedsJSON []byte

// fuzzSeed is one record from mattercert-fuzz-seeds.json.
type fuzzSeed struct {
	Label       string `json:"label"`
	Description string `json:"description"`
	BytesHex    string `json:"bytesHex"`
}

// FuzzDecode drives [Decode] with arbitrary bytes.
//
// Certificates reach this decoder before anything about the sender is
// trusted: a commissioner ships its RCAC/ICAC/NOC inside Sigma3 and
// AddNOC, so every byte here is attacker-supplied and pre-authentication.
// The asserted property is therefore the weak one that actually holds —
// Decode terminates and does not panic for any input — plus, on the
// inputs it accepts, that the accessors reachable from a decoded
// certificate are equally total. What a malformed input's error VALUE is
// is deliberately not asserted: the error taxonomy is free to change
// without this target going red.
//
// The package has no TLV encoder for [Certificate], so there is no
// decode(encode(x)) round-trip to assert. The available equivalent is
// idempotence over [Certificate.Raw], which Decode documents as a copy of
// its input: re-decoding that copy must reproduce the same certificate.
func FuzzDecode(f *testing.F) {
	// Short shapes inline, mirroring im/fuzz_invoke_test.go.
	f.Add([]byte{})
	f.Add([]byte{0x15, 0x18}) // empty anonymous struct

	var seeds []fuzzSeed
	if err := json.Unmarshal(fuzzSeedsJSON, &seeds); err != nil {
		f.Fatalf("parse mattercert-fuzz-seeds.json: %v", err)
	}
	if len(seeds) == 0 {
		f.Fatal("mattercert-fuzz-seeds.json is empty")
	}
	for _, s := range seeds {
		raw, err := hex.DecodeString(s.BytesHex)
		if err != nil {
			f.Fatalf("seed %q: decode hex: %v", s.Label, err)
		}
		f.Add(raw)
	}

	f.Fuzz(func(t *testing.T, raw []byte) {
		cert, err := Decode(raw)
		if err != nil {
			// Any error is acceptable; a panic or a hang is not, and the
			// harness reports both on its own.
			return
		}
		if cert == nil {
			t.Fatal("Decode returned nil cert with nil error")
		}

		// Raw is contracted to be the input bytes, retained for chain
		// hashing. A decoder that handed back a sub-slice or a mutated
		// copy would break signature verification downstream.
		if !reflect.DeepEqual(cert.Raw, raw) {
			t.Fatalf("cert.Raw = %x, want the input %x", cert.Raw, raw)
		}

		again, err := Decode(cert.Raw)
		if err != nil {
			t.Fatalf("re-decoding cert.Raw failed after a successful Decode: %v", err)
		}
		if !reflect.DeepEqual(cert, again) {
			t.Fatalf("Decode is not idempotent over cert.Raw:\nfirst  %+v\nsecond %+v", cert, again)
		}

		// Everything reachable from an accepted certificate has to be
		// total as well — these run on attacker-supplied field values.
		_, _ = cert.PublicKeyECDSA()
		_, _ = TBSToDER(cert)
		_ = cert.IsRoot()
		_ = cert.IsICA()
		_ = cert.IsNOC()
	})
}

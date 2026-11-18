// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package sessionparams_test

import (
	"encoding/hex"
	"encoding/json"
	"maps"
	"os"
	"testing"

	"github.com/SukramJ/go-fabric/secure/sessionparams"
	"github.com/SukramJ/go-fabric/tlv"
)

// contextFields lists the context-tagged unsigned fields of the
// anonymous structure in b: tag → value. The width each is written with is
// not compared — matter.js writes the minimal one, this module the field's
// (Encode's doc); both decoders take either.
func contextFields(t *testing.T, b []byte) map[uint32]uint64 {
	t.Helper()
	dec := tlv.NewDecoder(b)
	open, err := dec.Next()
	if err != nil || !open.IsContainer {
		t.Fatalf("no structure in % x: %v", b, err)
	}
	out := map[uint32]uint64{}
	for {
		el, err := dec.Next()
		if err != nil {
			t.Fatalf("% x: %v", b, err)
		}
		if el.IsEndContainer {
			return out
		}
		out[el.Tag.Number] = el.Uint
	}
}

// TestSessionParametersParityMatterJS: the session parameters this module
// states carry the fields matter.js's do, for a node with each TCP role —
// SUPPORTED_TRANSPORTS (tag 8) always, 0 for a node without TCP, and
// MAX_TCP_MESSAGE_SIZE (tag 9) only with a TCP role. The bytes are
// matter.js's own encoder's at db021da0
// (notes/parity/matter/generate-session-params-fixtures.ts), for the object
// SessionManager hands its PASE and CASE servers
// (packages/protocol/src/session/SessionManager.ts:391-400,
// packages/node/src/behavior/system/network/ServerNetworkRuntime.ts:204-210
// at db021da0). Review II, R2-P6-par-2905.
func TestSessionParametersParityMatterJS(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile("testdata/session-params-fixtures.json")
	if err != nil {
		t.Fatal(err)
	}
	var fx struct {
		Cases []struct{ Label, Hex string }
	}
	if err := json.Unmarshal(raw, &fx); err != nil {
		t.Fatal(err)
	}
	ours := map[string]sessionparams.Parameters{
		"no_tcp":     {},
		"tcp_client": {SupportedTransports: sessionparams.TransportTCPClient, MaxTCPMessageSize: 64000},
		"tcp_server": {SupportedTransports: sessionparams.TransportTCPServer, MaxTCPMessageSize: 64000},
		"tcp_both":   {SupportedTransports: sessionparams.TransportTCPClient | sessionparams.TransportTCPServer, MaxTCPMessageSize: 64000},
	}
	if len(fx.Cases) != len(ours) {
		t.Fatalf("%d fixture cases, want %d", len(fx.Cases), len(ours))
	}
	for _, c := range fx.Cases {
		p, ok := ours[c.Label]
		if !ok {
			t.Fatalf("fixture case %q has no counterpart", c.Label)
		}
		b, err := hex.DecodeString(c.Hex)
		if err != nil {
			t.Fatal(err)
		}
		enc := tlv.NewEncoder()
		p.Encode(enc, tlv.AnonymousTag())
		got, err := enc.Bytes()
		if err != nil {
			t.Fatal(err)
		}
		if want, have := contextFields(t, b), contextFields(t, got); !maps.Equal(want, have) {
			t.Errorf("%s: fields %v, matter.js states %v", c.Label, have, want)
		}
	}
}

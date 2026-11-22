// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package softwareupdate

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"thingfabric.dev/go-fabric/controller/client"
)

// TestNoteAttestedIdentity: a query whose ids are not the node's attested
// ones is one Warn line with both pairs (review II decision 105); matching
// or unknown attested ids log nothing.
func TestNoteAttestedIdentity(t *testing.T) {
	t.Parallel()
	u16 := func(v uint16) *uint16 { return &v }
	attested := client.Record{AttestedVendorID: u16(0xFFF1), AttestedProductID: u16(0x8001)}
	for _, tc := range []struct {
		name     string
		rec      client.Record
		vid, pid uint16
		logged   bool
	}{
		{"another vendor", attested, 0xFFF2, 0x8001, true},
		{"another product", attested, 0xFFF1, 0x8002, true},
		{"the attested ids", attested, 0xFFF1, 0x8001, false},
		{"unknown attested ids", client.Record{}, 0xFFF2, 0x8002, false},
		{"half-known attested ids", client.Record{AttestedVendorID: u16(0xFFF1)}, 0xFFF2, 0x8002, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var buf bytes.Buffer
			r := newManagerRig(t, func(c *Config) { c.Logger = slog.New(slog.NewJSONHandler(&buf, nil)) })
			r.m.noteAttestedIdentity(testPeer, tc.rec, tc.vid, tc.pid)
			lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
			if !tc.logged {
				if buf.Len() != 0 {
					t.Fatalf("logged %q, want nothing", buf.String())
				}
				return
			}
			if len(lines) != 1 {
				t.Fatalf("logged %d lines %q, want one", len(lines), buf.String())
			}
			var got map[string]any
			if err := json.Unmarshal([]byte(lines[0]), &got); err != nil {
				t.Fatal(err)
			}
			want := map[string]any{
				"level": "WARN", "msg": "ota.manager.attested_identity_mismatch", "node": peerString(testPeer),
				"attested_vendor_id": float64(0xFFF1), "attested_product_id": float64(0x8001),
				"vendor_id": float64(tc.vid), "product_id": float64(tc.pid),
			}
			for k, v := range want {
				if got[k] != v {
					t.Errorf("%s = %v, want %v (line %s)", k, got[k], v, lines[0])
				}
			}
		})
	}
}

// TestValidatePeerDetailsWithoutNodeIsUnchanged: the identity note sits
// behind the node lookup; a peer the registry does not know is refused as
// before and logs no identity line.
func TestValidatePeerDetailsWithoutNodeIsUnchanged(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	r := newManagerRig(t, func(c *Config) { c.Logger = slog.New(slog.NewJSONHandler(&buf, nil)) })
	if r.m.validatePeerDetails(testPeer, 1, 0xFFF1, 0x8001) {
		t.Fatal("an unknown peer's details validated")
	}
	if strings.Contains(buf.String(), "attested_identity_mismatch") {
		t.Fatalf("logged %q", buf.String())
	}
}

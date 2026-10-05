// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"testing"

	mattercore "github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
)

// TestGeneralDiagnosticsResponsesCrossTheWire pins the two GeneralDiagnostics
// responses on the wire (general-diagnostics.element.ts:103-119): the
// response command ids TimeSnapshotResponse 0x02 and PayloadTestResponse
// 0x04, and their fields. Before, TimeSnapshot went out under its request's
// id with an empty struct — the default arm of the fields writer.
func TestGeneralDiagnosticsResponsesCrossTheWire(t *testing.T) {
	t.Parallel()
	cases := []struct {
		resp    any
		wantCmd uint32
		check   func(t *testing.T, m map[uint8]any)
	}{
		{mattercore.TimeSnapshotResponse{SystemTimeMs: 1234}, 0x02, func(t *testing.T, m map[uint8]any) {
			t.Helper()
			if m[0] != uint64(1234) {
				t.Errorf("SystemTimeMs = %v", m[0])
			}
			if v, ok := m[1]; !ok || v != nil {
				t.Errorf("PosixTimeMs = %v (present %v), want null", v, ok)
			}
		}},
		{mattercore.PayloadTestResponse{Payload: []byte{7, 7, 7}}, 0x04, func(t *testing.T, m map[uint8]any) {
			t.Helper()
			if b, _ := m[0].([]byte); len(b) != 3 || b[0] != 7 {
				t.Errorf("Payload = %v", m[0])
			}
		}},
	}
	for _, tc := range cases {
		ent := im.InvokeResponseEntry{Path: im.ConcreteCommandPath{Cluster: 0x0033, Command: 0x01}, Response: tc.resp, HasResponse: true}
		rewriteInvokeResponseCommand(&ent)
		if ent.Path.Command != tc.wantCmd {
			t.Errorf("%T: response command 0x%02X, want 0x%02X", tc.resp, ent.Path.Command, tc.wantCmd)
		}
		enc := tlv.NewEncoder()
		defaultCommandFieldsWriter(enc, tlv.AnonymousTag(), tc.resp)
		raw, err := enc.Bytes()
		if err != nil {
			t.Fatal(err)
		}
		dec := tlv.NewDecoder(raw)
		if _, err := dec.Next(); err != nil {
			t.Fatal(err)
		}
		m, err := decodeGenericTagMap(dec)
		if err != nil {
			t.Fatal(err)
		}
		tc.check(t, m)
	}
}

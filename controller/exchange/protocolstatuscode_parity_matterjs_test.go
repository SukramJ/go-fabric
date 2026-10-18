// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package exchange

// A port of matter.js packages/protocol/test/protocol/ProtocolStatusCodeTest.ts
// at 862ad75f: the StatusReport body with and without protocol data, for a
// standard and a vendor-specific protocol. The matter.js schema's
// { protocolId, vendorId } is the Go 32-bit ProtocolID (vendor << 16 |
// protocol), encoded by StatusReport.Encode and decoded by DecodeStatusReport.

import (
	"encoding/hex"
	"testing"
)

func TestProtocolStatusCode(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		in   StatusReport
		wire string
	}{
		{
			// GeneralStatusCode.Failure, BDX_PROTOCOL_ID, BdxStatusCode.StartOffsetNotSupported.
			name: "Encodes and decodes without protocol data",
			in:   StatusReport{GeneralCode: 1, ProtocolID: 0x0002, ProtocolCode: 0x52},
			wire: "0100020000005200",
		},
		{
			name: "Encodes and decodes vendor specific without protocol data",
			in:   StatusReport{GeneralCode: 0, ProtocolID: 0xfff1<<16 | 0xaabb, ProtocolCode: 0},
			wire: "0000bbaaf1ff0000",
		},
		{
			name: "Encodes and decodes vendor specific with protocol data",
			in: StatusReport{
				GeneralCode: 1, ProtocolID: 0xfff1<<16 | 0xaabb, ProtocolCode: 9921,
				ProtocolData: []byte{0x55, 0x66, 0xee, 0xff},
			},
			wire: "0100bbaaf1ffc1265566eeff",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			enc := tc.in.Encode()
			if got := hex.EncodeToString(enc); got != tc.wire {
				t.Errorf("encoded = %s, want %s", got, tc.wire)
			}
			dec, ok := DecodeStatusReport(enc)
			if !ok {
				t.Fatal("decode failed")
			}
			if dec.GeneralCode != tc.in.GeneralCode || dec.ProtocolID != tc.in.ProtocolID ||
				dec.ProtocolCode != tc.in.ProtocolCode ||
				hex.EncodeToString(dec.ProtocolData) != hex.EncodeToString(tc.in.ProtocolData) {
				t.Errorf("decoded = %+v, want %+v", dec, tc.in)
			}
			if tc.in.ProtocolData == nil && dec.ProtocolData != nil {
				t.Errorf("protocol data = %X, want none", dec.ProtocolData)
			}
		})
	}
}

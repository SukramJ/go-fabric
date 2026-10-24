// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package interaction

import (
	"encoding/hex"
	"encoding/json"
	"os"
	"testing"

	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
)

// FuzzDecodeReport feeds arbitrary bytes to the controller's two decoders
// of a device's answer to a read or subscribe: decodeReport, which reads
// every ReportData chunk, and im.UnmarshalSubscribeResponseTLV, which
// reads the SubscribeResponse that ends the priming. Invariant: each one
// decodes or returns an error and never panics. The decoded attribute and
// event report count is bounded by the input length: every report takes
// at least one byte, so a larger count would mean the decoder invents
// entries.
func FuzzDecodeReport(f *testing.F) {
	raw, err := os.ReadFile("../../im/testdata/im-wire-fixtures.json")
	if err != nil {
		f.Fatal(err)
	}
	var fixtures []struct {
		Type     string `json:"type"`
		BytesHex string `json:"bytesHex"`
	}
	if err := json.Unmarshal(raw, &fixtures); err != nil {
		f.Fatal(err)
	}
	seeded := 0
	for _, fx := range fixtures {
		if fx.Type != "ReportData" && fx.Type != "SubscribeResponse" {
			continue
		}
		b, err := hex.DecodeString(fx.BytesHex)
		if err != nil {
			f.Fatalf("fixture hex: %v", err)
		}
		f.Add(b)
		seeded++
	}
	if seeded == 0 {
		f.Fatal("no ReportData or SubscribeResponse fixture seeded")
	}
	f.Add([]byte{})
	f.Add([]byte{0x15, 0x18})
	f.Fuzz(func(t *testing.T, payload []byte) {
		if len(payload) > 1<<16 {
			return
		}
		if rd, err := decodeReport(payload); err == nil {
			if n := len(rd.Reports) + len(rd.EventReports); n > len(payload) {
				t.Fatalf("decoded %d reports from %d bytes", n, len(payload))
			}
		}
		_, _ = im.UnmarshalSubscribeResponseTLV(tlv.NewDecoder(payload))
	})
}

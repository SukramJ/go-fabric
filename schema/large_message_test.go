// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package schema_test

import (
	"encoding/json"
	"testing"

	"github.com/SukramJ/go-fabric/parity"
	"github.com/SukramJ/go-fabric/schema"
)

// TestCommandLargeMessage pins representative request commands that carry
// the matter.js "L" quality at 862ad75f (camera-av-stream-management,
// commodity-price, push-av-stream-transport and tls-certificate-management
// elements) and commands that do not.
func TestCommandLargeMessage(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name             string
		cluster, command uint32
		want             bool
	}{
		{"CameraAvStreamManagement/CaptureSnapshot", 0x0551, 0x0C, true},
		{"CommodityPrice/GetDetailedForecastRequest", 0x0095, 0x02, true},
		{"PushAvStreamTransport/AllocatePushTransport", 0x0555, 0x00, true},
		{"PushAvStreamTransport/FindTransport", 0x0555, 0x06, true},
		{"TlsCertificateManagement/ClientCsr", 0x0801, 0x07, true},
		{"CameraAvStreamManagement/VideoStreamAllocate", 0x0551, 0x03, false},
		{"OnOff/On", 0x0006, 0x01, false},
		{"UnknownCluster", 0xFFF1FC00, 0x00, false},
	}
	for _, c := range cases {
		if got := schema.CommandLargeMessage(c.cluster, c.command); got != c.want {
			t.Errorf("%s: CommandLargeMessage(0x%04X, 0x%02X) = %v, want %v", c.name, c.cluster, c.command, got, c.want)
		}
	}
}

// TestCommandLargeMessageMatchesSnapshot holds the generated pin against the
// model it is generated from: CommandLargeMessage answers true for exactly
// the request commands whose effective quality in parity/schema.json carries
// "largeMessage", across every cluster of the snapshot. A stale
// large_message_gen.go (the generator skipped after a snapshot refresh)
// fails here.
func TestCommandLargeMessageMatchesSnapshot(t *testing.T) {
	t.Parallel()
	var snap struct {
		Clusters []struct {
			ID       uint32 `json:"id"`
			Name     string `json:"name"`
			Commands []struct {
				ID        uint32 `json:"id"`
				Name      string `json:"name"`
				Direction string `json:"direction"`
				Effective *struct {
					Quality struct {
						LargeMessage bool `json:"largeMessage"`
					} `json:"quality"`
				} `json:"effective"`
			} `json:"commands"`
		} `json:"clusters"`
	}
	if err := json.Unmarshal(parity.SchemaJSON(), &snap); err != nil {
		t.Fatalf("parse snapshot: %v", err)
	}
	total := 0
	for _, c := range snap.Clusters {
		want := map[uint32]string{}
		for _, m := range c.Commands {
			if m.Direction == "request" && m.Effective != nil && m.Effective.Quality.LargeMessage {
				want[m.ID] = m.Name
			}
		}
		total += len(want)
		for id := range uint32(0x100) {
			name, wantL := want[id]
			if got := schema.CommandLargeMessage(c.ID, id); got != wantL {
				t.Errorf("%s (0x%04X) command 0x%02X %s: CommandLargeMessage = %v, snapshot largeMessage = %v", c.Name, c.ID, id, name, got, wantL)
			}
		}
	}
	if total == 0 {
		t.Fatal("snapshot carries no largeMessage request command: the extractor lost the command quality")
	}
}

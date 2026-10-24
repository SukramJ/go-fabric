// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package mdns_test

import (
	"strconv"
	"testing"

	"github.com/SukramJ/go-fabric/mdns"
)

// TestBuildOperationalService_ICD holds the operational record of an ICD:
// the ICD key, the threshold as SAT, and no SII for a LIT ICD (matter.js
// IcdManagementServer.ts:482-495, MdnsAdvertisement.ts:209-211).
func TestBuildOperationalService_ICD(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		mode    uint8
		omitSII bool
	}{{0, false}, {1, true}} {
		cfg := operationalCfg()
		mode := tc.mode
		cfg.ICD, cfg.SessionActiveThreshold, cfg.OmitSessionIdleInterval = &mode, 5000, tc.omitSII
		keys := map[string]string{}
		for _, r := range mdns.BuildOperationalService(cfg).TXT {
			keys[r.Key] = r.Value
		}
		if _, hasSII := keys["SII"]; hasSII == tc.omitSII {
			t.Errorf("mode %d: SII present %v", tc.mode, hasSII)
		}
		if keys["ICD"] != strconv.Itoa(int(tc.mode)) || keys["SAT"] != "5000" || keys["SAI"] == "" {
			t.Errorf("mode %d: TXT %v", tc.mode, keys)
		}
	}
}

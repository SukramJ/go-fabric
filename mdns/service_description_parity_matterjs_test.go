// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package mdns_test

// A port of matter.js packages/protocol/test/advertisement/ServiceDescriptionTest.ts at
// 862ad75f: ICD operating mode and session intervals on an operational
// service description.

import (
	"testing"

	"github.com/SukramJ/go-fabric/mdns"
)

func serviceDescriptionTXT(svc mdns.Service) map[string]string {
	out := make(map[string]string, len(svc.TXT))
	for _, r := range svc.TXT {
		out[r.Key] = r.Value
	}
	return out
}

// TestServiceDescriptionOperational covers ServiceDescriptionTest.ts:11-33.
// matter.js checks the fields of the ServiceDescription.Operational
// object; Go has no description object between the configuration and the
// record, so the port checks that BuildOperationalService carries the same
// values into the operational record's TXT keys
// (MdnsAdvertisement.ts:183-209).
func TestServiceDescriptionOperational(t *testing.T) {
	t.Parallel()
	const lit, sit uint8 = 1, 0 // IcdManagement.OperatingMode
	t.Run("carries ICD operating mode and session intervals on operational descriptions", func(t *testing.T) {
		t.Parallel()
		icd := lit
		svc := mdns.BuildOperationalService(mdns.OperationalServiceConfig{
			ICD:                    &icd,
			SessionIdleInterval:    30_000, // Seconds(30)
			SessionActiveInterval:  3_000,  // Seconds(3)
			SessionActiveThreshold: 5_000,  // Seconds(5)
		})
		if svc.ServiceType != mdns.ServiceTypeOperational {
			t.Errorf("service type %q, want the operational one", svc.ServiceType)
		}
		txt := serviceDescriptionTXT(svc)
		for k, want := range map[string]string{"ICD": "1", "SII": "30000", "SAI": "3000", "SAT": "5000"} {
			if txt[k] != want {
				t.Errorf("%s = %q, want %q", k, txt[k], want)
			}
		}
	})
	t.Run("keeps icd: Sit (value 0) defined, not dropped as falsy", func(t *testing.T) {
		t.Parallel()
		icd := sit
		txt := serviceDescriptionTXT(mdns.BuildOperationalService(mdns.OperationalServiceConfig{ICD: &icd}))
		if v, ok := txt["ICD"]; !ok || v != "0" {
			t.Errorf("ICD = %q (present %v), want 0", v, ok)
		}
	})
}

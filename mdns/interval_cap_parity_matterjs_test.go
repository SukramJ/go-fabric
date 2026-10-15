// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package mdns_test

import (
	"testing"

	"github.com/SukramJ/go-fabric/mdns"
)

// txtValue returns the value of key in svc's TXT record, "" when absent.
func txtValue(svc mdns.Service, key string) string {
	for _, r := range svc.TXT {
		if r.Key == key {
			return r.Value
		}
	}
	return ""
}

// TestParityMatterJS_AdvertisedIntervalsAreCapped mirrors matter.js
// 0ceb202d packages/protocol/src/session/SessionIntervals.ts:69-91
// (forAdvertisement) on both records: SII and SAI above one hour
// (maxAdvertisedInterval, :57) are advertised as 3600000, SAT above 65535
// (maxActiveThreshold, :63) as 65535; a value at its bound is kept and
// zero still means the default.
func TestParityMatterJS_AdvertisedIntervalsAreCapped(t *testing.T) {
	t.Parallel()
	build := map[string]func(sii, sai, sat uint32) mdns.Service{
		"operational": func(sii, sai, sat uint32) mdns.Service {
			return mdns.BuildOperationalService(mdns.OperationalServiceConfig{
				SessionIdleInterval: sii, SessionActiveInterval: sai, SessionActiveThreshold: sat,
			})
		},
		"commissionable": func(sii, sai, sat uint32) mdns.Service {
			return mdns.BuildCommissionableService(mdns.CommissionableServiceConfig{
				SessionIdleInterval: sii, SessionActiveInterval: sai, SessionActiveThreshold: sat,
			})
		},
	}
	for name, b := range build {
		for _, tc := range []struct {
			name          string
			sii, sai, sat uint32
			want          map[string]string
		}{
			{"SII capped", 3_600_001, 300, 4000, map[string]string{"SII": "3600000", "SAI": "300", "SAT": "4000"}},
			{"SAI capped", 500, 7_200_000, 4000, map[string]string{"SII": "500", "SAI": "3600000", "SAT": "4000"}},
			{"SAT capped", 500, 300, 65536, map[string]string{"SII": "500", "SAI": "300", "SAT": "65535"}},
			{"bounds kept", 3_600_000, 3_600_000, 65535, map[string]string{"SII": "3600000", "SAI": "3600000", "SAT": "65535"}},
			{"zero is the default", 0, 0, 0, map[string]string{"SAI": "300", "SAT": "4000"}},
		} {
			svc := b(tc.sii, tc.sai, tc.sat)
			for key, want := range tc.want {
				if got := txtValue(svc, key); got != want {
					t.Errorf("%s / %s: %s=%q, want %q", name, tc.name, key, got, want)
				}
			}
		}
	}
}

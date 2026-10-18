// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package mdns_test

// A port of matter.js packages/protocol/test/advertisement/AdvertisementTest.ts at
// 862ad75f: privacy masking of a commissionable advertisement.

import (
	"slices"
	"testing"

	"github.com/SukramJ/go-fabric/mdns"
)

// TestAdvertisementPrivacyMasking covers AdvertisementTest.ts:53-95. Go
// has no Advertisement lifecycle class; the commissionable record a
// BuildCommissionableService call yields is what matter.js's
// CommissionableMdnsAdvertisement broadcasts while unmasked (VP TXT key
// and the _V vendor subtype, CommissionableMdnsAdvertisement.ts:66-68,
// :94-96). The masked cases stay unported: by_design
// BD-Matter-mDNS-VPAlwaysAdvertised.
func TestAdvertisementPrivacyMasking(t *testing.T) {
	t.Parallel()
	t.Run("masks nothing before the extended-announcement period", func(t *testing.T) {
		t.Parallel()
		// AdvertisementTest.ts:13-20: vendor 1, product 1, device type 1,
		// mode 1, discriminator 0xf00.
		svc := mdns.BuildCommissionableService(mdns.CommissionableServiceConfig{
			DeviceName:        "Test",
			DeviceTypeID:      1,
			VendorID:          1,
			ProductID:         1,
			CommissioningMode: 1,
			Discriminator:     0xf00,
		})
		var vp string
		for _, r := range svc.TXT {
			if r.Key == "VP" {
				vp = r.Value
			}
		}
		if vp != "1+1" {
			t.Errorf("VP = %q, want 1+1 (not masked)", vp)
		}
		if !slices.Contains(svc.Subtypes, "_V1") {
			t.Errorf("subtypes %v lack the vendor subtype _V1", svc.Subtypes)
		}
	})
}

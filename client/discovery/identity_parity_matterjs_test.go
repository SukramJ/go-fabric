// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package discovery_test

import (
	"testing"

	"github.com/SukramJ/go-fabric/client/discovery"
)

// TestCommissioningIdentityParity ports matter.js's CommissioningIdentityTest
// (packages/node/test/behavior/system/controller/discovery/CommissioningIdentityTest.ts
// at 862ad75f): CommissioningDiscovery.identityMismatch, here
// discovery.MismatchOf. matter.js's undefined is 0, the unspecified value
// both codecs report as absent.
func TestCommissioningIdentityParity(t *testing.T) {
	t.Parallel()
	const vendor, otherVendor, product, otherProduct = 0xfff1, 0xfff2, 0x8001, 0x8002
	id := func(v, p uint16) discovery.Identity { return discovery.Identity{VendorID: v, ProductID: p} }
	none := func(t *testing.T, payload, advertised discovery.Identity) {
		t.Helper()
		if m := discovery.MismatchOf(payload, advertised); m != nil {
			t.Fatalf("mismatch %+v, want none", *m)
		}
	}
	want := func(t *testing.T, payload, advertised discovery.Identity, w discovery.IdentityMismatch) {
		t.Helper()
		if m := discovery.MismatchOf(payload, advertised); m == nil || *m != w {
			t.Fatalf("mismatch %v, want %+v", m, w)
		}
	}
	t.Run("accepts a device advertising what the payload names", func(t *testing.T) {
		t.Parallel()
		none(t, id(vendor, product), id(vendor, product))
	})
	t.Run("names the vendor a device advertises instead", func(t *testing.T) {
		t.Parallel()
		want(t, id(vendor, 0), id(otherVendor, 0), discovery.IdentityMismatch{Facet: "vendor", Payload: vendor, Advertised: otherVendor})
	})
	t.Run("names the product a device advertises instead", func(t *testing.T) {
		t.Parallel()
		want(t, id(vendor, product), id(vendor, otherProduct), discovery.IdentityMismatch{Facet: "product", Payload: product, Advertised: otherProduct})
	})
	t.Run("accepts when the payload names no identity", func(t *testing.T) {
		t.Parallel()
		none(t, id(0, 0), id(vendor, product))
	})
	t.Run("accepts when the advertisement states no identity", func(t *testing.T) {
		t.Parallel()
		none(t, id(vendor, product), id(0, 0))
	})
	t.Run("judges each half on its own presence", func(t *testing.T) {
		t.Parallel()
		none(t, id(vendor, product), id(vendor, 0))
		none(t, id(0, product), id(otherVendor, product))
		want(t, id(vendor, 0), id(otherVendor, product), discovery.IdentityMismatch{Facet: "vendor", Payload: vendor, Advertised: otherVendor})
	})
	t.Run("compares only what both sides state", func(t *testing.T) {
		t.Parallel()
		none(t, id(0, 0), id(0, 0))
		none(t, id(0, product), id(0, product))
	})
}

// TestAdvertisedIdentity: the VP record's vendor and product, the
// "<vendor>" form without one (§ 4.3.1).
func TestAdvertisedIdentity(t *testing.T) {
	t.Parallel()
	vp := func(s string) discovery.Device { return discovery.Device{TXTData: discovery.TXTData{VP: &s}} }
	for _, c := range []struct {
		d    discovery.Device
		want discovery.Identity
	}{
		{discovery.Device{}, discovery.Identity{}},
		{vp("65521+32769"), discovery.Identity{VendorID: 65521, ProductID: 32769}},
		{vp("65521"), discovery.Identity{VendorID: 65521}},
		{vp("x+y"), discovery.Identity{}},
	} {
		if got := c.d.AdvertisedIdentity(); got != c.want {
			t.Errorf("%+v: %+v, want %+v", c.d.VP, got, c.want)
		}
	}
}

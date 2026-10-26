// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package discovery

import (
	"strconv"
	"strings"
)

// Identity is what an onboarding payload or a commissionable advertisement
// says about a device's identity — matter.js CommissioningDiscovery.Identity
// (packages/node/src/behavior/system/controller/discovery/CommissioningDiscovery.ts:96-100
// at 862ad75f). 0 is unspecified (§ 2.5.2, § 2.5.3): the payload codecs
// and the advertisement parser report it as absent.
type Identity struct {
	VendorID, ProductID uint16
}

// IdentityMismatch is the one identifier on which an advertisement
// contradicts the payload — CommissioningDiscovery.IdentityMismatch
// (:102-107): Facet is "vendor" or "product".
type IdentityMismatch struct {
	Facet               string
	Payload, Advertised uint16
}

// MismatchOf is why advertised is not the device payload names, nil when
// it may be — CommissioningDiscovery.identityMismatch (:109-140): a
// discriminator is all discovery narrows by, so the vendor and product a
// payload names are checked against the device's VP record once it is
// found; either side may say nothing, and absence never rejects. It
// mirrors CHIP's SetUpCodePairer::NodeMatchesCurrentFilter.
func MismatchOf(payload, advertised Identity) *IdentityMismatch {
	if payload.VendorID != 0 && advertised.VendorID != 0 && payload.VendorID != advertised.VendorID {
		return &IdentityMismatch{Facet: "vendor", Payload: payload.VendorID, Advertised: advertised.VendorID}
	}
	if payload.ProductID != 0 && advertised.ProductID != 0 && payload.ProductID != advertised.ProductID {
		return &IdentityMismatch{Facet: "product", Payload: payload.ProductID, Advertised: advertised.ProductID}
	}
	return nil
}

// AdvertisedIdentity is the vendor and product of d's VP record
// ("<vendor>+<product>" or "<vendor>", decimal, § 4.3.1); 0 for one the
// record does not state or that does not parse.
func (d Device) AdvertisedIdentity() Identity {
	if d.VP == nil {
		return Identity{}
	}
	vendor, product, _ := strings.Cut(*d.VP, "+")
	var id Identity
	if v, err := strconv.ParseUint(vendor, 10, 16); err == nil {
		id.VendorID = uint16(v)
	}
	if p, err := strconv.ParseUint(product, 10, 16); err == nil {
		id.ProductID = uint16(p)
	}
	return id
}

// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controller

import (
	"context"
	"crypto/rand"
	"fmt"

	"github.com/SukramJ/go-fabric/mdns"
)

// Advertise is the commissioner record a controller publishes over DNS-SD
// (`_matterd._udp`, Matter §4.3.3), so a device that offers
// commissioner discovery (user-directed commissioning) finds it: matter.js's
// ServiceDescription.Commissioner (packages/protocol/src/advertisement/
// ServiceDescription.ts:94-110 at 862ad75f), broadcast by its
// CommissionerMdnsAdvertisement (packages/protocol/src/advertisement/mdns/
// CommissionerMdnsAdvertisement.ts:20-71). matter.js advertises it only
// when the program asks; so does [Config].Advertise.
type Advertise struct {
	// Name is the DN TXT key (the device name a device shows); "" omits it.
	Name string
	// DeviceType is the commissioner's device type (DT and `_T<type>`);
	// 0 omits both, as matter.js's optional deviceType.
	DeviceType uint32
	// VendorID and ProductID are the VP TXT key and the `_V<vendor>`
	// subtype; a VendorID of 0 is our fabric's vendor ID.
	VendorID  uint16
	ProductID uint16
	// PrivacyMasked omits VP and `_V<vendor>`, matter.js's
	// omitPrivateDetails (packages/protocol/src/advertisement/
	// Advertisement.ts:189-191).
	PrivacyMasked bool
}

// advertise publishes the commissioner record of a on the controller's UDP
// port, through adv (mdns.NewZeroconf when nil, then closed by
// [Controller.Close]). The instance id is 8 random bytes, as matter.js's
// MdnsAdvertiser.createInstanceId (MdnsAdvertiser.ts:102-104).
func (c *Controller) advertise(ctx context.Context, a Advertise, adv mdns.Advertiser, vendor uint16) error {
	own := adv == nil
	if own {
		adv = mdns.NewZeroconf()
	}
	if a.VendorID == 0 {
		a.VendorID = vendor
	}
	cfg := mdns.CommissionerServiceConfig{
		DeviceName: a.Name, DeviceTypeID: a.DeviceType, VendorID: a.VendorID, ProductID: a.ProductID,
		PrivacyMasked: a.PrivacyMasked, Port: uint16(c.udp.LocalAddr().Port), //nolint:gosec // G115: a UDP port
	}
	_, _ = rand.Read(cfg.InstanceID[:])
	svc := mdns.BuildCommissionerService(cfg)
	if err := adv.Publish(ctx, svc); err != nil {
		if own {
			_ = adv.Close()
		}
		return fmt.Errorf("controller: commissioner advertisement: %w", err)
	}
	c.unadvertise = func(ctx context.Context) error {
		err := adv.Withdraw(ctx, svc.InstanceName, mdns.ServiceTypeCommissioner)
		if own {
			if cerr := adv.Close(); err == nil {
				err = cerr
			}
		}
		return err
	}
	return nil
}

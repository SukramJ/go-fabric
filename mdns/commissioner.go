// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package mdns

import (
	"encoding/hex"
	"fmt"
	"net"
	"strconv"
	"strings"
)

// ServiceTypeCommissioner is the commissioner discovery service label,
// Matter §4.3.3: matter.js MATTER_COMMISSIONER_SERVICE_QNAME
// (packages/protocol/src/mdns/MdnsConsts.ts:12 at 862ad75f).
const ServiceTypeCommissioner = "_matterd._udp"

// CommissionerServiceConfig drives [BuildCommissionerService]: matter.js's
// ServiceDescription.Commissioner (packages/protocol/src/advertisement/
// ServiceDescription.ts:94-110 at 862ad75f) — a ProductDescription whose
// device type is optional — and the advertiser's omitPrivateDetails.
type CommissionerServiceConfig struct {
	// InstanceID is the instance label's 8 random bytes, rendered as 16
	// upper-case hex digits (matter.js MdnsAdvertiser.createInstanceId,
	// MdnsAdvertiser.ts:102-104).
	InstanceID [8]byte
	// DeviceName is the DN TXT key; "" omits it.
	DeviceName string
	// DeviceTypeID is the commissioner's device type, the DT TXT key and
	// the `_T<type>` subtype; 0 omits both, matter.js's undefined
	// deviceType (CommissionerMdnsAdvertisement.ts:47-52, :60-63).
	DeviceTypeID uint32
	// VendorID and ProductID are the VP TXT key and the `_V<vendor>`
	// subtype, both omitted when PrivacyMasked.
	VendorID  uint16
	ProductID uint16
	// PrivacyMasked omits the vendor details — matter.js's
	// isPrivacyMasked forced by omitPrivateDetails
	// (packages/protocol/src/advertisement/Advertisement.ts:189-191).
	PrivacyMasked bool
	// Port is the controller's UDP port (SRV).
	Port uint16
	// HostName and Addresses are the SRV target and its addresses, as
	// in [CommissionableServiceConfig].
	HostName  string
	Addresses []net.IP
}

// BuildCommissionerService is the `_matterd._udp` record a commissioner
// advertises, Matter §4.3.3. It mirrors matter.js's
// CommissionerMdnsAdvertisement (packages/protocol/src/advertisement/
// mdns/CommissionerMdnsAdvertisement.ts at 862ad75f): the instance
// `<id>._matterd._udp.local` (:24-30, MdnsConsts.ts:46), the `_V<vendor>`
// subtype unless privacy masked (:41-45), the `_T<deviceType>` subtype when
// a device type is given (:47-52), and the TXT keys DN, DT and — unless
// privacy masked — VP as `<vendor>+<product>` (:57-71). The session
// intervals are not advertised: matter.js omits SII, SAI and SAT at their
// defaults (MdnsAdvertisement.ts:186-200), which a controller of this
// module uses, and the T key is operational only (:204-207).
func BuildCommissionerService(cfg CommissionerServiceConfig) Service {
	var txt []TXTRecord
	if cfg.DeviceName != "" {
		txt = append(txt, TXTRecord{Key: "DN", Value: cfg.DeviceName})
	}
	var subtypes []string
	if cfg.DeviceTypeID != 0 {
		txt = append(txt, TXTRecord{Key: "DT", Value: strconv.FormatUint(uint64(cfg.DeviceTypeID), 10)})
	}
	if !cfg.PrivacyMasked {
		txt = append(txt, TXTRecord{Key: "VP", Value: fmt.Sprintf("%d+%d", cfg.VendorID, cfg.ProductID)})
		subtypes = append(subtypes, fmt.Sprintf("_V%d", cfg.VendorID))
	}
	if cfg.DeviceTypeID != 0 {
		subtypes = append(subtypes, fmt.Sprintf("_T%d", cfg.DeviceTypeID))
	}
	host := cfg.HostName
	if host == "" {
		host = defaultHostName()
	}
	return Service{
		InstanceName: strings.ToUpper(hex.EncodeToString(cfg.InstanceID[:])),
		ServiceType:  ServiceTypeCommissioner,
		Port:         cfg.Port,
		HostName:     host,
		Addresses:    append([]net.IP(nil), cfg.Addresses...),
		TXT:          txt,
		Subtypes:     subtypes,
	}
}

// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package wpasupplicant

import (
	"math"
	"strings"

	"github.com/godbus/dbus/v5"

	netcommdef "github.com/SukramJ/go-fabric/cluster/spec/networkcommissioning"
	"github.com/SukramJ/go-fabric/contract"
)

// Matter's bounds on a scan result (WiFiInterfaceScanResultStruct):
// connectedhomeip's kMaxWiFiSSIDLength and kWiFiBSSIDLength.
const (
	maxSSIDLength = 32
	bssidLength   = 6
)

// secEAP is connectedhomeip's internal sentinel (bit 7): an EAP-only
// network is not reported as Unencrypted, and the bit is masked off before
// the result is returned. Mirrors connectedhomeip
// src/platform/Linux/ConnectivityManagerImpl_NetworkManagementWpaSupplicant.cpp:1429-1435
// at 6170af84 (kEAP).
const secEAP netcommdef.WiFiSecurityBitmap = 1 << 7

// bandAndChannel is the band and channel of a frequency in MHz:
// wpa_supplicant's scan results carry no channel, so connectedhomeip
// resolves both from the frequency. Mirrors connectedhomeip
// src/platform/Linux/ConnectivityManagerImpl_NetworkManagementWpaSupplicant.cpp:1286-1380
// at 6170af84 (GetBandAndChannelFromFrequency), ported whole — including
// the 60 GHz branch its 6 GHz branch (freq >= 5955) shadows, and its
// unsigned arithmetic, which wraps for 916 MHz as it does there.
func bandAndChannel(freq uint32) (band netcommdef.WiFiBandEnum, channel uint16) {
	band = netcommdef.WiFiBandV2G4
	switch {
	case freq <= 931:
		band = netcommdef.WiFiBandV1G
		switch {
		case freq >= 916:
			channel = uint16(((freq - 916) * 2) - 1) //nolint:gosec // chip's uint16 conversion, wraps at 916 MHz as there
		case freq >= 902:
			channel = uint16((freq - 902) * 2) //nolint:gosec // at most 58
		case freq >= 863:
			channel = uint16((freq - 863) * 2) //nolint:gosec // at most 76
		default:
			channel = 1
		}
	case freq <= 2472:
		channel = uint16((freq-2412)/5 + 1) //nolint:gosec // chip's conversion; wraps below 2412 MHz as there
	case freq == 2484:
		channel = 14
	case freq >= 3600 && freq <= 3700:
		band = netcommdef.WiFiBandV3G65
	case freq >= 5035 && freq <= 5945:
		band, channel = netcommdef.WiFiBandV5G, uint16((freq-5000)/5) //nolint:gosec // at most 189
	case freq == 5960 || freq == 5980:
		band, channel = netcommdef.WiFiBandV5G, uint16((freq-5000)/5) //nolint:gosec // 192 or 196
	case freq >= 5955:
		band, channel = netcommdef.WiFiBandV6G, uint16((freq-5950)/5) //nolint:gosec // a uint16 frequency keeps it in range
	case freq >= 58000:
		band = netcommdef.WiFiBandV60G
		channel = band60GChannels[freq]
	}
	return band, channel
}

// band60GChannels are the 60 GHz center frequencies chip names.
var band60GChannels = map[uint32]uint16{
	58320: 1, 60480: 2, 62640: 3, 64800: 4, 66960: 5, 69120: 6,
	59400: 9, 61560: 10, 63720: 11, 65880: 12, 68040: 13,
}

// keyMgmts is the KeyMgmt list of a BSS's WPA or RSN dictionary, nil
// without one.
func keyMgmts(ie map[string]dbus.Variant) []string {
	v, ok := ie["KeyMgmt"]
	if !ok {
		return nil
	}
	list, _ := v.Value().([]string)
	return list
}

// security is the WiFiSecurityBitmap of a BSS from its WPA and RSN
// dictionaries. Mirrors connectedhomeip
// src/platform/Linux/ConnectivityManagerImpl_NetworkManagementWpaSupplicant.cpp:1437-1507
// and :1533-1540 at 6170af84 (IsNetworkWPAPSK, IsNetworkWPA2PSK): WPA's
// wpa-psk and wpa-none are WPA Personal; RSN's wpa-psk, wpa-psk-sha256 and
// wpa-ft-psk are WPA2 Personal and sae is WPA3 Personal; an EAP key
// management keeps the network from being reported as Unencrypted;
// nothing else is Unencrypted. Case-insensitive, as g_strcasecmp. WEP
// reads as Unencrypted there too (its TODO at :1432-1433).
func security(wpa, rsn map[string]dbus.Variant) netcommdef.WiFiSecurityBitmap {
	var sec netcommdef.WiFiSecurityBitmap
	for _, k := range keyMgmts(wpa) {
		switch strings.ToLower(k) {
		case "wpa-psk", "wpa-none":
			sec |= netcommdef.WiFiSecurityWpaPersonal
		case "wpa-eap":
			sec |= secEAP
		}
	}
	for _, k := range keyMgmts(rsn) {
		switch strings.ToLower(k) {
		case "wpa-psk", "wpa-psk-sha256", "wpa-ft-psk":
			sec |= netcommdef.WiFiSecurityWpa2Personal
		case "wpa-eap", "wpa-eap-sha256", "wpa-ft-eap":
			sec |= secEAP
		case "sae":
			sec |= netcommdef.WiFiSecurityWpa3Personal
		}
	}
	if sec == 0 {
		sec = netcommdef.WiFiSecurityUnencrypted
	}
	return sec &^ secEAP
}

// clampSignal is a BSS's Signal (dBm, int16) clamped into int8. Mirrors
// connectedhomeip
// src/platform/Linux/ConnectivityManagerImpl_NetworkManagementWpaSupplicant.cpp:1516-1527
// at 6170af84.
func clampSignal(s int16) int8 {
	switch {
	case s < math.MinInt8:
		return math.MinInt8
	case s > math.MaxInt8:
		return math.MaxInt8
	}
	return int8(s)
}

// scanResult is one BSS's properties (fi.w1.wpa_supplicant1.BSS GetAll)
// as a scan result; false drops it. Mirrors connectedhomeip
// src/platform/Linux/ConnectivityManagerImpl_NetworkManagementWpaSupplicant.cpp:1385-1543
// at 6170af84 (_GetBssInfo): a BSS without SSID or BSSID is gone
// (:1401-1407), an SSID over 32 bytes or a BSSID not of 6 bytes is dropped
// (:1509-1511).
func scanResult(props map[string]dbus.Variant) (contract.WiFiScanResult, bool) {
	ssidV, okSSID := props["SSID"]
	bssidV, okBSSID := props["BSSID"]
	if !okSSID || !okBSSID {
		return contract.WiFiScanResult{}, false
	}
	ssid, _ := ssidV.Value().([]byte)
	bssid, _ := bssidV.Value().([]byte)
	if len(ssid) > maxSSIDLength || len(bssid) != bssidLength {
		return contract.WiFiScanResult{}, false
	}
	signal, _ := props["Signal"].Value().(int16)
	freq, _ := props["Frequency"].Value().(uint16)
	wpa, _ := props["WPA"].Value().(map[string]dbus.Variant)
	rsn, _ := props["RSN"].Value().(map[string]dbus.Variant)
	band, channel := bandAndChannel(uint32(freq))
	return contract.WiFiScanResult{
		Security: uint8(security(wpa, rsn)),
		SSID:     append([]byte(nil), ssid...),
		BSSID:    append([]byte(nil), bssid...),
		Channel:  channel,
		Band:     uint8(band), HasBand: true,
		RSSI: clampSignal(signal), HasRSSI: true,
	}, true
}

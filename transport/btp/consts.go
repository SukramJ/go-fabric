// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package btp

import (
	"strings"
	"time"
)

// The Matter BLE service and its characteristics. Mirrors matter.js
// packages/protocol/src/ble/BleConsts.ts:12-30 (MatterBle.SERVICE_UUID_SHORT,
// SERVICE_UUID, isServiceUuid, C1/C2/C3_CHARACTERISTIC_UUID) at 0ceb202d.
const (
	// ServiceUUIDShort is the 16-bit form of the Matter BLE service UUID.
	ServiceUUIDShort = "fff6"
	// ServiceUUID is the 128-bit (Bluetooth SIG base UUID) form.
	ServiceUUID = "0000fff6-0000-1000-8000-00805f9b34fb"
	// C1CharacteristicUUID is the characteristic the central writes to.
	C1CharacteristicUUID = "18EE2EF5-263D-4559-959F-4F9C429F9D11"
	// C2CharacteristicUUID is the characteristic the peripheral indicates on.
	C2CharacteristicUUID = "18EE2EF5-263D-4559-959F-4F9C429F9D12"
	// C3CharacteristicUUID carries the additional commissioning data.
	C3CharacteristicUUID = "64630238-8772-45F2-B87D-748A83218F04"
)

// IsServiceUUID reports whether uuid names the Matter BLE service, in its
// short or its full form, in any case (BleConsts.ts:23-26).
func IsServiceUUID(uuid string) bool {
	l := strings.ToLower(uuid)
	return l == ServiceUUIDShort || l == ServiceUUID
}

// Segment sizes, window sizes and timers. Mirrors matter.js
// BleConsts.ts:32-79 at 0ceb202d, verbatim.
const (
	// MinimumATTMTU is the 23-byte minimum ATT_MTU less the 3-byte ATT
	// operation header (:32), the smallest BTP segment.
	MinimumATTMTU = 20
	// MaximumBTPMTU is the largest BTP segment (:33).
	MaximumBTPMTU = 244
	// MaximumATTMTU is a BTP segment plus the 3-byte GATT header (:34).
	MaximumATTMTU = MaximumBTPMTU + 3

	// MaximumWindowSize is the server's maximum window size (:44).
	MaximumWindowSize = 255
	// WindowNoAckSendThreshold reserves the remote window's last slot for
	// an acknowledgement (§4.19.4.7, :47).
	WindowNoAckSendThreshold = 1
	// ImmediateAckWindowThreshold is the number of free local-window slots
	// at or below which a pending acknowledgement goes out at once
	// (§4.19.4.8, :50).
	ImmediateAckWindowThreshold = 2

	// ConnRspTimeout is how long a central waits for the handshake
	// response (:56).
	ConnRspTimeout = 15 * time.Second
	// AckTimeout is the acknowledgement-received timer (§4.19.4.8, :62).
	AckTimeout = 15 * time.Second
	// SendAckTimeout is the longest a received segment waits for its
	// acknowledgement, a third of AckTimeout (:64).
	SendAckTimeout = AckTimeout / 3
	// ConnIdleTimeout is how long a central's session may send no unique
	// data before it closes (:70).
	ConnIdleTimeout = 60 * time.Second

	// MaxMatterPayloadSize is the largest Matter message BTP carries, one
	// UDP message (:79; MAX_UDP_MESSAGE_SIZE = 1280 - 48,
	// packages/general/src/net/udp/UdpSocket.ts:12).
	MaxMatterPayloadSize = 1280 - 48
)

// SupportedVersions are the BTP versions this implementation speaks,
// highest first (BleConsts.ts:73).
var SupportedVersions = []int{4}

// SegmentSizeFromATTMTU is the BTP segment size of an ATT_MTU: the
// ATT_MTU less the 3-byte GATT header, clamped to
// [MinimumATTMTU, MaximumBTPMTU] (§4.19.3.1.4, BleConsts.ts:37-42).
func SegmentSizeFromATTMTU(attMTU int) int {
	s := attMTU - 3
	if s < MinimumATTMTU {
		return MinimumATTMTU
	}
	if s > MaximumBTPMTU {
		return MaximumBTPMTU
	}
	return s
}

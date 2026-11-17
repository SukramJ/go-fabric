// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package im

// InteractionModelProtocolID is the Matter Protocol ID under which
// IM messages travel (Matter Core Spec §10.5 Table 17). The
// Secure-Channel layer routes datagrams whose ProtocolHeader carries
// this ProtocolID into the IM dispatcher.
const InteractionModelProtocolID uint16 = 0x0001

// IM message opcodes per Matter Core Spec §10.5 Table 18. These are
// the values the ProtocolHeader.Opcode field takes when ProtocolID
// is [InteractionModelProtocolID]. Receivers fan out by opcode into
// [HandleReadRequest] / [HandleWriteRequest] / [HandleInvokeRequest]; a
// SubscribeRequest is decoded here ([UnmarshalSubscribeRequestTLV]) and
// served by the bridge through the subscription manager
// (im/subscription.Manager; review II, R2-P6-doc-0563).
const (
	OpcodeStatusResponse    uint8 = 0x01
	OpcodeReadRequest       uint8 = 0x02
	OpcodeSubscribeRequest  uint8 = 0x03
	OpcodeSubscribeResponse uint8 = 0x04
	OpcodeReportData        uint8 = 0x05
	OpcodeWriteRequest      uint8 = 0x06
	OpcodeWriteResponse     uint8 = 0x07
	OpcodeInvokeRequest     uint8 = 0x08
	OpcodeInvokeResponse    uint8 = 0x09
	OpcodeTimedRequest      uint8 = 0x0A
)

// IsRequestOpcode reports whether the opcode identifies a request
// message the bridge must respond to (Read / Write / Invoke /
// Subscribe / Timed). Response opcodes (StatusResponse, ReportData,
// WriteResponse, InvokeResponse, SubscribeResponse) return false —
// they answer an exchange this node initiated: a report it sends, or a
// command it invokes itself (the OTA provider link reads an
// InvokeResponse, bridge/ota_provider_link.go).
func IsRequestOpcode(op uint8) bool {
	switch op {
	case OpcodeReadRequest,
		OpcodeWriteRequest,
		OpcodeInvokeRequest,
		OpcodeSubscribeRequest,
		OpcodeTimedRequest:
		return true
	}
	return false
}

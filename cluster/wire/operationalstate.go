// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package wire

// OperationalState (0x0060) and its derived RvcOperationalState (0x0061)
// share every wire shape below; RvcOperationalState adds GoHome and its
// own enum values (matter.js
// packages/model/src/standard/elements/operational-state.element.ts,
// rvc-operational-state.element.ts).
const (
	OperationalStateClusterID    uint32 = 0x0060
	RvcOperationalStateClusterID uint32 = 0x0061
)

// OperationalState command ids (operational-state.element.ts:57-80,
// rvc-operational-state.element.ts:21-30). Every request is fieldless and
// answered by OperationalCommandResponse.
const (
	OperationalStateCmdPause                      uint32 = 0x00
	OperationalStateCmdStop                       uint32 = 0x01
	OperationalStateCmdStart                      uint32 = 0x02
	OperationalStateCmdResume                     uint32 = 0x03
	OperationalStateCmdOperationalCommandResponse uint32 = 0x04
	RvcOperationalStateCmdGoHome                  uint32 = 0x80
)

// Manufacturer-specific operational state and error state ids carry a
// label: the OperationalStateLabel / ErrorStateLabel conformance is
// "OperationalStateID >= 128 & OperationalStateID <= 191"
// (operational-state.element.ts:93-95, :110-113).
const (
	OperationalStateManufacturerMin uint8 = 0x80
	OperationalStateManufacturerMax uint8 = 0xBF
)

// HasOperationalStateLabel reports whether a state or error id lies in the
// manufacturer-specific range, where the struct carries its label.
func HasOperationalStateLabel(id uint8) bool {
	return id >= OperationalStateManufacturerMin && id <= OperationalStateManufacturerMax
}

// OperationalStateStruct is one OperationalStateList entry
// (operational-state.element.ts:90-97): [0] OperationalStateID enum8,
// [1] OperationalStateLabel string max 64, present for a
// manufacturer-specific id only.
type OperationalStateStruct struct {
	OperationalStateID    uint8
	OperationalStateLabel string
}

// ErrorStateStruct is the OperationalError attribute, the
// CommandResponseState field and the OperationalError event's ErrorState
// (operational-state.element.ts:107-115): [0] ErrorStateID enum8,
// [1] ErrorStateLabel string max 64, present for a manufacturer-specific
// id only, [2] ErrorStateDetails string max 64, optional — present when
// not empty.
type ErrorStateStruct struct {
	ErrorStateID      uint8
	ErrorStateLabel   string
	ErrorStateDetails string
}

// OperationalCommandResponse is the response (0x04) to Pause, Stop,
// Start, Resume and GoHome: [0] CommandResponseState
// (operational-state.element.ts:74-80).
type OperationalCommandResponse struct {
	CommandResponseState ErrorStateStruct
}

// OperationalErrorEvent is the OperationalError event (0x00, critical):
// [0] ErrorState (operational-state.element.ts:45-48).
type OperationalErrorEvent struct {
	ErrorState ErrorStateStruct
}

// ElapsedS is a nullable elapsed-s (uint32 seconds) value.
type ElapsedS struct {
	Null    bool
	Seconds uint32
}

// OperationCompletionEvent is the OperationCompletion event (0x01, info,
// operational-state.element.ts:50-55): [0] CompletionErrorCode enum8,
// [1] TotalOperationalTime elapsed-s (optional, nullable), [2] PausedTime
// elapsed-s (optional, nullable). A nil pointer leaves the field out.
type OperationCompletionEvent struct {
	CompletionErrorCode  uint8
	TotalOperationalTime *ElapsedS
	PausedTime           *ElapsedS
}

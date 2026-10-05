// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package wire

// The ModeBase derivations this module serves share the wire shapes below
// (matter.js packages/model/src/standard/elements/mode-base.element.ts).
// ModeSelect (0x0050) is not one of them: it predates ModeBase and has its
// own ModeOptionStruct (cluster/modeselect).
const (
	LaundryWasherModeClusterID uint32 = 0x0051
	RvcRunModeClusterID        uint32 = 0x0054
	RvcCleanModeClusterID      uint32 = 0x0055
	DishwasherModeClusterID    uint32 = 0x0059
)

// ModeBase command ids (mode-base.element.ts:44-56).
const (
	ModeBaseCmdChangeToMode         uint32 = 0x00
	ModeBaseCmdChangeToModeResponse uint32 = 0x01
)

// ChangeToModeFieldNewMode is the context tag of ChangeToMode's NewMode
// (uint8, mandatory; mode-base.element.ts:49).
const ChangeToModeFieldNewMode uint8 = 0x00

// ModeTagStruct is one mode tag (mode-base.element.ts:57-61): [0] MfgCode
// vendor-id, optional — nil leaves it out — and [1] Value, the enum16
// ModeTag.
type ModeTagStruct struct {
	MfgCode *uint16
	Value   uint16
}

// ModeOptionStruct is one SupportedModes entry (mode-base.element.ts:63-71):
// [0] Label string max 64, [1] Mode uint8, [2] ModeTags list.
type ModeOptionStruct struct {
	Label    string
	Mode     uint8
	ModeTags []ModeTagStruct
}

// ChangeToModeRequest is the decoded ChangeToMode payload.
type ChangeToModeRequest struct {
	NewMode uint8
}

// ChangeToModeResponse is the ChangeToModeResponse (0x01,
// mode-base.element.ts:52-56): [0] Status (ModeChangeStatus, enum8) and
// [1] StatusText (string max 64). StatusText is always sent: its
// conformance "[Status == Success], M" makes it mandatory for a failure,
// and matter.js's ModeUtils.assertModeChange sends "" on success too.
type ChangeToModeResponse struct {
	Status     uint8
	StatusText string
}

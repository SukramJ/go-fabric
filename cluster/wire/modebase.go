// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package wire

import (
	"github.com/SukramJ/go-fabric/cluster/spec"
	lwmdef "github.com/SukramJ/go-fabric/cluster/spec/laundrywashermode"
	"github.com/SukramJ/go-fabric/tlv"
)

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

// The values below encode through generated codecs (spec.Encodable), so
// the bridge writes them as matter.js's TlvOfModel does. The four ModeBase
// derivations generate the same ModeOptionStruct, ModeTagStruct and
// ChangeToModeResponse codecs (cluster/spec/laundrywashermode,
// rvcrunmode, rvccleanmode, dishwashermode); these types are shared by
// all four, so they convert to LaundryWasherMode's, and
// TestModeBaseCodecsAgreeAcrossDerivations holds that the choice does not
// change a byte.

// generated returns m as the generated ModeOptionStruct.
func (m ModeOptionStruct) generated() lwmdef.ModeOptionStruct {
	tags := make([]lwmdef.ModeTagStruct, len(m.ModeTags))
	for i, t := range m.ModeTags {
		tags[i] = lwmdef.ModeTagStruct{MfgCode: t.MfgCode, Value: lwmdef.ModeTag(t.Value)}
	}
	return lwmdef.ModeOptionStruct{Label: m.Label, Mode: m.Mode, ModeTags: tags}
}

// EncodeTLV implements spec.Encodable with the generated codec.
func (m ModeOptionStruct) EncodeTLV(enc *tlv.Encoder, tag tlv.Tag) { m.generated().EncodeTLV(enc, tag) }

// ModeOptionList is the SupportedModes attribute value: a list of
// ModeOptionStruct, encoded by the generated codec, so the bridge encodes
// a []ModeOptionStruct by converting it.
type ModeOptionList []ModeOptionStruct

// EncodeTLV implements spec.Encodable.
func (l ModeOptionList) EncodeTLV(enc *tlv.Encoder, tag tlv.Tag) {
	out := make(spec.List[lwmdef.ModeOptionStruct], len(l))
	for i, m := range l {
		out[i] = m.generated()
	}
	out.EncodeTLV(enc, tag)
}

// EncodeTLV implements spec.Encodable with the generated codec.
func (r ChangeToModeResponse) EncodeTLV(enc *tlv.Encoder, tag tlv.Tag) {
	lwmdef.ChangeToModeResponse{Status: lwmdef.ModeChangeStatus(r.Status), StatusText: r.StatusText}.EncodeTLV(enc, tag)
}

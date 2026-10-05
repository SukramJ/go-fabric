// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package bridge

import (
	matteralarm "github.com/SukramJ/go-fabric/cluster/alarm"
	clusterwire "github.com/SukramJ/go-fabric/cluster/wire"
	"github.com/SukramJ/go-fabric/tlv"
)

// encodeApplicationValue writes the structured values of the application
// cluster servers (cluster/alarm, cluster/fan, cluster/pump,
// cluster/opstate, cluster/modebase) and the Switch events: event
// payloads, whose EventDataIB Data slot goes through the same value
// writer as attribute data, and the list / struct attributes. Integers
// inside a structure go out at their smallest width, as matter.js's
// TlvOfModel writes them (application-wire-fixtures.json). It reports
// false for a value it does not know, which the caller then encodes as
// null. Kept out of
// defaultAttributeValueWriter's own switch so each cluster's shapes sit
// next to each other rather than among the root clusters'.
func encodeApplicationValue(enc *tlv.Encoder, tag tlv.Tag, v any) bool {
	switch x := v.(type) {
	case clusterwire.FieldlessEvent:
		// An event that declares no fields still carries a structure in
		// the Data slot (see clusterwire.FieldlessEvent).
		enc.StartStruct(tag)
		_ = enc.EndContainer()
	case matteralarm.AlarmSeverityEvent:
		// SmokeCoAlarm SmokeAlarm / CoAlarm / LowBattery /
		// InterconnectSmokeAlarm / InterconnectCoAlarm: [0]
		// AlarmSeverityLevel, AlarmStateEnum (enum8). matter.js
		// smoke-co-alarm-cluster.element.ts:47-71.
		enc.StartStruct(tag)
		enc.PutUint(tlv.ContextTag(0), uint64(x.AlarmSeverityLevel))
		_ = enc.EndContainer()
	case clusterwire.SwitchInitialPressEvent:
		// Switch InitialPress / LongPress: [0] NewPosition uint8;
		// ShortRelease / LongRelease: [0] PreviousPosition uint8.
		// matter.js switch.element.ts.
		encodeSwitchPosition(enc, tag, x.NewPosition)
	case clusterwire.SwitchLongPressEvent:
		encodeSwitchPosition(enc, tag, x.NewPosition)
	case clusterwire.SwitchShortReleaseEvent:
		encodeSwitchPosition(enc, tag, x.PreviousPosition)
	case clusterwire.SwitchLongReleaseEvent:
		encodeSwitchPosition(enc, tag, x.PreviousPosition)
	default:
		return encodeApplianceValue(enc, tag, v)
	}
	return true
}

// encodeApplianceValue writes the attribute and event values of the
// appliance servers (cluster/opstate, cluster/modebase); it reports false
// for a value it does not know.
func encodeApplianceValue(enc *tlv.Encoder, tag tlv.Tag, v any) bool {
	switch x := v.(type) {
	case []string:
		// A list of strings: OperationalState.PhaseList (a null list is
		// a nil value and never reaches here).
		enc.StartArray(tag)
		for _, str := range x {
			enc.PutUTF8(tlv.AnonymousTag(), str)
		}
		_ = enc.EndContainer()
	case []clusterwire.OperationalStateStruct:
		// OperationalState.OperationalStateList: [0] OperationalStateID,
		// [1] OperationalStateLabel for a manufacturer-specific id
		// (operational-state.element.ts:90-97).
		enc.StartArray(tag)
		for _, e := range x {
			enc.StartStruct(tlv.AnonymousTag())
			enc.PutUint(tlv.ContextTag(0), uint64(e.OperationalStateID))
			if clusterwire.HasOperationalStateLabel(e.OperationalStateID) {
				enc.PutUTF8(tlv.ContextTag(1), e.OperationalStateLabel)
			}
			_ = enc.EndContainer()
		}
		_ = enc.EndContainer()
	case clusterwire.ErrorStateStruct:
		// OperationalState.OperationalError.
		encodeErrorState(enc, tag, x)
	case clusterwire.OperationalErrorEvent:
		// OperationalError event: [0] ErrorState.
		enc.StartStruct(tag)
		encodeErrorState(enc, tlv.ContextTag(0), x.ErrorState)
		_ = enc.EndContainer()
	case clusterwire.OperationCompletionEvent:
		// OperationCompletion event: [0] CompletionErrorCode, [1]
		// TotalOperationalTime and [2] PausedTime, each optional and
		// nullable (operational-state.element.ts:50-55).
		enc.StartStruct(tag)
		enc.PutUint(tlv.ContextTag(0), uint64(x.CompletionErrorCode))
		putElapsed(enc, tlv.ContextTag(1), x.TotalOperationalTime)
		putElapsed(enc, tlv.ContextTag(2), x.PausedTime)
		_ = enc.EndContainer()
	case []clusterwire.ModeOptionStruct:
		// ModeBase SupportedModes: [0] Label, [1] Mode, [2] ModeTags of
		// {[0] MfgCode (optional), [1] Value} (mode-base.element.ts:57-71).
		enc.StartArray(tag)
		for _, m := range x {
			enc.StartStruct(tlv.AnonymousTag())
			enc.PutUTF8(tlv.ContextTag(0), m.Label)
			enc.PutUint(tlv.ContextTag(1), uint64(m.Mode))
			enc.StartArray(tlv.ContextTag(2))
			for _, t := range m.ModeTags {
				enc.StartStruct(tlv.AnonymousTag())
				if t.MfgCode != nil {
					enc.PutUint(tlv.ContextTag(0), uint64(*t.MfgCode))
				}
				enc.PutUint(tlv.ContextTag(1), uint64(t.Value))
				_ = enc.EndContainer()
			}
			_ = enc.EndContainer()
			_ = enc.EndContainer()
		}
		_ = enc.EndContainer()
	default:
		return false
	}
	return true
}

// encodeSwitchPosition writes a Switch event payload: one uint8 at
// context tag 0.
func encodeSwitchPosition(enc *tlv.Encoder, tag tlv.Tag, position uint8) {
	enc.StartStruct(tag)
	enc.PutUint(tlv.ContextTag(0), uint64(position))
	_ = enc.EndContainer()
}

// encodeErrorState writes an OperationalState ErrorStateStruct: [0]
// ErrorStateID, [1] ErrorStateLabel for a manufacturer-specific id, [2]
// ErrorStateDetails when set (operational-state.element.ts:107-115).
func encodeErrorState(enc *tlv.Encoder, tag tlv.Tag, e clusterwire.ErrorStateStruct) {
	enc.StartStruct(tag)
	enc.PutUint(tlv.ContextTag(0), uint64(e.ErrorStateID))
	if clusterwire.HasOperationalStateLabel(e.ErrorStateID) {
		enc.PutUTF8(tlv.ContextTag(1), e.ErrorStateLabel)
	}
	if e.ErrorStateDetails != "" {
		enc.PutUTF8(tlv.ContextTag(2), e.ErrorStateDetails)
	}
	_ = enc.EndContainer()
}

// putElapsed writes an optional, nullable elapsed-s field; nil leaves it
// out.
func putElapsed(enc *tlv.Encoder, tag tlv.Tag, v *clusterwire.ElapsedS) {
	switch {
	case v == nil:
	case v.Null:
		enc.PutNull(tag)
	default:
		enc.PutUint(tag, uint64(v.Seconds))
	}
}

// encodeApplicationResponse writes the command responses of the
// application cluster servers; it reports false for a value it does not
// know.
func encodeApplicationResponse(enc *tlv.Encoder, tag tlv.Tag, v any) bool {
	switch x := v.(type) {
	case clusterwire.OperationalCommandResponse:
		// OperationalCommandResponse: [0] CommandResponseState
		// (operational-state.element.ts:74-80).
		enc.StartStruct(tag)
		encodeErrorState(enc, tlv.ContextTag(0), x.CommandResponseState)
		_ = enc.EndContainer()
	case clusterwire.ChangeToModeResponse:
		// ChangeToModeResponse: [0] Status, [1] StatusText
		// (mode-base.element.ts:52-56).
		enc.StartStruct(tag)
		enc.PutUint(tlv.ContextTag(0), uint64(x.Status))
		enc.PutUTF8(tlv.ContextTag(1), x.StatusText)
		_ = enc.EndContainer()
	default:
		return false
	}
	return true
}

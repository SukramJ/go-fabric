// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"github.com/SukramJ/go-fabric/cluster/spec"
	clusterwire "github.com/SukramJ/go-fabric/cluster/wire"
	"github.com/SukramJ/go-fabric/tlv"
)

// encodeApplicationValue writes the structured values of the application
// cluster servers (cluster/alarm, cluster/fan, cluster/pump,
// cluster/opstate, cluster/modebase) and the Switch events: event
// payloads, whose EventDataIB Data slot goes through the same value
// writer as attribute data, and the list / struct attributes. Every one
// of them encodes through a generated codec (spec.Encodable), as
// matter.js's TlvOfModel writes it (application-wire-fixtures.json,
// appliance-wire-fixtures.json); the event and struct types of
// cluster/wire and cluster/alarm implement it themselves. A list arrives
// as a plain slice, which carries no method, so the three list shapes
// are converted to their generated list here. It reports false for a
// value it does not know, which the caller then encodes as null. Kept out
// of defaultAttributeValueWriter's own switch so each cluster's shapes sit
// next to each other rather than among the root clusters'.
func encodeApplicationValue(enc *tlv.Encoder, tag tlv.Tag, v any) bool {
	switch x := v.(type) {
	case []string:
		// A list of strings: OperationalState.PhaseList (a null list is
		// a nil value and never reaches here), list[string] in the
		// generated codec.
		spec.PutList(spec.PutString)(enc, tag, x)
	case []clusterwire.OperationalStateStruct:
		// OperationalState.OperationalStateList.
		clusterwire.OperationalStateList(x).EncodeTLV(enc, tag)
	case []clusterwire.ModeOptionStruct:
		// ModeBase SupportedModes.
		clusterwire.ModeOptionList(x).EncodeTLV(enc, tag)
	case spec.Encodable:
		// A value of a generated cluster definition (cluster/spec/...):
		// a struct, a spec.List of structs or an event payload, which
		// encodes itself as matter.js's TlvOfModel does.
		x.EncodeTLV(enc, tag)
	default:
		return false
	}
	return true
}

// encodeApplicationResponse writes the command responses of the
// application cluster servers — OperationalCommandResponse,
// ChangeToModeResponse and every generated response payload — through
// their generated codecs; it reports false for a value it does not know.
func encodeApplicationResponse(enc *tlv.Encoder, tag tlv.Tag, v any) bool {
	x, ok := v.(spec.Encodable)
	if !ok {
		return false
	}
	x.EncodeTLV(enc, tag)
	return true
}

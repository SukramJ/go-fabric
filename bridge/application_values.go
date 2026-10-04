// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package bridge

import (
	matteralarm "github.com/SukramJ/go-fabric/cluster/alarm"
	clusterwire "github.com/SukramJ/go-fabric/cluster/wire"
	"github.com/SukramJ/go-fabric/tlv"
)

// encodeApplicationValue writes the structured values of the application
// cluster servers (cluster/alarm, cluster/fan, cluster/pump) — today
// only event payloads, whose EventDataIB Data slot goes through the same
// value writer as attribute data. It reports false for a value it does
// not know, which the caller then encodes as null. Kept out of
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
	default:
		return false
	}
	return true
}

// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package alarm_test

import (
	"bytes"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/alarm"
	"github.com/SukramJ/go-fabric/cluster/spec"
	sdef "github.com/SukramJ/go-fabric/cluster/spec/smokecoalarm"
	"github.com/SukramJ/go-fabric/tlv"
)

// TestAlarmSeverityEventMatchesEveryGeneratedSeverityEvent holds the
// claim AlarmSeverityEvent rests on: it is the generated SmokeAlarm
// payload, and the generated CoAlarm, LowBattery, InterconnectSmokeAlarm
// and InterconnectCoAlarm payloads write the same bytes, so one type
// carries all five events.
func TestAlarmSeverityEventMatchesEveryGeneratedSeverityEvent(t *testing.T) {
	t.Parallel()
	encode := func(v spec.Encodable) []byte {
		enc := tlv.NewEncoder()
		v.EncodeTLV(enc, tlv.AnonymousTag())
		b, err := enc.Bytes()
		if err != nil {
			t.Fatalf("encode %T: %v", v, err)
		}
		return b
	}
	for _, level := range []alarm.AlarmState{alarm.AlarmWarning, alarm.AlarmCritical} {
		want := encode(alarm.AlarmSeverityEvent{AlarmSeverityLevel: level})
		for _, other := range []spec.Encodable{
			sdef.CoAlarmEvent{AlarmSeverityLevel: level},
			sdef.LowBatteryEvent{AlarmSeverityLevel: level},
			sdef.InterconnectSmokeAlarmEvent{AlarmSeverityLevel: level},
			sdef.InterconnectCoAlarmEvent{AlarmSeverityLevel: level},
		} {
			if got := encode(other); !bytes.Equal(got, want) {
				t.Errorf("%T(%d) = %x, AlarmSeverityEvent = %x", other, level, got, want)
			}
		}
	}
}

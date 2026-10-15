// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package energy

import (
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/tlv"
)

// TestCommodityWireValue: every list type the four commodity servers store
// reads as a spec.List that encodes as a TLV array, a nil list stays null,
// and any other value passes unchanged. A plain slice would reach the
// bridge's attribute writer without an EncodeTLV and go on the wire as
// null (bridge/reply.go defaultAttributeValueWriter) — the cause of the
// TC-SETRF-2.2/2.3/3.1 and TC-COMMTR-3.1 failures in CI run 38114710064.
func TestCommodityWireValue(t *testing.T) {
	t.Parallel()
	for name, v := range map[string]any{
		"DayEntries":       []TariffDayEntry{{DayEntryId: 1}},
		"DayPatterns":      []TariffDayPattern{{DayPatternId: 1}},
		"TariffComponents": []TariffComponent{{TariffComponentId: 1}},
		"TariffPeriods":    []TariffPeriod{{TariffComponentIds: []uint32{1}}},
		"IndividualDays":   []TariffDay{{Date: 1}},
		"CalendarPeriods":  []TariffCalendarPeriod{{DayPatternIds: []uint32{1}}},
		"MeteredQuantity":  []MeteredQuantity{{Quantity: 1}},
		"PriceForecast":    []CommodityPrice{{PeriodStart: 1}},
		"ForecastConds":    []GridConditions{{PeriodStart: 1}},
	} {
		got, ok := commodityWireValue(v).(spec.Encodable)
		if !ok {
			t.Errorf("%s reads as %T, not a spec.Encodable", name, commodityWireValue(v))
			continue
		}
		enc := tlv.NewEncoder()
		got.EncodeTLV(enc, tlv.AnonymousTag())
		b, err := enc.Bytes()
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		el, err := tlv.NewDecoder(b).Next()
		if err != nil || el.Type != tlv.TypeArray {
			t.Errorf("%s encodes as %v (%v), want an array", name, el.Type, err)
		}
	}
	if v := commodityWireValue([]TariffComponent(nil)); v != nil {
		t.Errorf("a nil list reads as %#v, want null", v)
	}
	if v := commodityWireValue(uint32(7)); v != uint32(7) {
		t.Errorf("a scalar reads as %#v", v)
	}
	if v, ok := commodityRead(nil, false); v != nil || ok {
		t.Errorf("an absent attribute reads as %v %v", v, ok)
	}
}

// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package energy

import "github.com/SukramJ/go-fabric/cluster/spec"

// commodityWireValue is what the four commodity servers' MatterRead hands
// the bridge for a stored value: a list attribute of structs as its
// generated [spec.List], which encodes itself as a TLV array. The servers
// keep plain slices internally (their validation and derivation compare
// and index them), but a plain slice carries no EncodeTLV, and the
// bridge's attribute writer writes a value it does not know as null
// (bridge/reply.go defaultAttributeValueWriter, the default arm after
// encodeApplicationValue). That is how TC-SETRF-2.2 step 4, TC-SETRF-2.3,
// TC-SETRF-3.1 step 27 and TC-COMMTR-3.1 step 6 saw Null for a list the
// test event trigger had set (CI run 38114710064). The other servers
// return spec.List the same way (cluster/servicearea, cluster/core
// actions). A nil slice stays the attribute's null; any other value is
// returned as it is.
func commodityWireValue(v any) any {
	switch l := v.(type) {
	case []TariffDayEntry:
		return wireList(l)
	case []TariffDayPattern:
		return wireList(l)
	case []TariffComponent:
		return wireList(l)
	case []TariffPeriod:
		return wireList(l)
	case []TariffDay:
		return wireList(l)
	case []TariffCalendarPeriod:
		return wireList(l)
	case []MeteredQuantity:
		return wireList(l)
	case []CommodityPrice:
		return wireList(l)
	case []GridConditions:
		return wireList(l)
	}
	return v
}

// wireList is l as a [spec.List]; nil for a nil l.
func wireList[T spec.Encodable](l []T) any {
	if l == nil {
		return nil
	}
	return spec.List[T](l)
}

// commodityRead is a commodity server's MatterRead over its generated
// server's read.
func commodityRead(v any, ok bool) (any, bool) {
	if !ok {
		return v, ok
	}
	return commodityWireValue(v), true
}

// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package peer

import (
	"reflect"
	"testing"
	"time"
)

// TestMergeIgnoresNegativeDurations: a negative pause or interval is unset,
// as zero is — the base keeps its value — so a host's sign error cannot
// make the connection process retry without a pause; only
// ProvenAddressRetention, whose negative value means "retain nothing",
// takes it (review II, R2-P6-api-2155).
func TestMergeIgnoresNegativeDurations(t *testing.T) {
	t.Parallel()
	neg := -time.Second
	// Every field, by reflection, so a field added later is held too.
	var over Timing
	var fill func(v reflect.Value)
	fill = func(v reflect.Value) {
		for i := range v.NumField() {
			f := v.Field(i)
			switch {
			case f.Type() == reflect.TypeFor[time.Duration]():
				f.SetInt(int64(neg))
			case f.Kind() == reflect.Int:
				f.SetInt(-1)
			case f.Kind() == reflect.Struct:
				fill(f)
			default:
				t.Fatalf("field %s of kind %s: the test does not know how to set it", v.Type().Field(i).Name, f.Kind())
			}
		}
	}
	fill(reflect.ValueOf(&over).Elem())
	want := DefaultTiming()
	want.ProvenAddressRetention = neg
	if got := Merge(DefaultTiming(), over); got != want {
		t.Fatalf("Merge took a negative value:\n got %+v\nwant %+v", got, want)
	}
}

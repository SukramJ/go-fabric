// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package otbr

import (
	"encoding/json"
	"testing"

	"github.com/SukramJ/go-fabric/thread"
	"github.com/SukramJ/go-fabric/thread/otbr/otbrtest"
)

// The cases of matter.js packages/thread-br-client/test/TranslationTest.ts
// at 862ad75f that test the records [Translate] keeps — 13 of 22, ported by
// meaning. Left out, as the records are not translated (translate.go):
// the post-2024 ipv6Addresses field and the six parseIpv6 cases (IPv6
// addresses), the empty unknown list (thread.DiagnosticResponse keeps no
// unknown TLVs) and the parentPriority case (connectivity). The first
// case's connectivity, network data, IPv6, channel pages and
// maxChildTimeout assertions are left out for the same reason.

func fixtureEntries(t *testing.T) []any {
	t.Helper()
	v, err := decodeJSON([]byte(otbrtest.Diagnostics))
	if err != nil {
		t.Fatal(err)
	}
	return NormalizeKeys(v).([]any)
}

func entryWithRloc16(t *testing.T, rloc16 int64) any {
	t.Helper()
	for _, e := range fixtureEntries(t) {
		if n, _ := e.(map[string]any)["rloc16"].(json.Number).Int64(); n == rloc16 {
			return e
		}
	}
	t.Fatalf("no entry %d", rloc16)
	return nil
}

func translate(t *testing.T, entry any) thread.DiagnosticResponse {
	t.Helper()
	r, err := Translate(entry)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// thread-br-client/test/TranslationTest.ts:21-83.
func TestTranslateRouterWithChildren(t *testing.T) {
	t.Parallel()
	d := translate(t, entryWithRloc16(t, 5120))
	if d.Rloc16 == nil || *d.Rloc16 != 5120 || hexOf(d.ExtMACAddress) != "0011223344556603" {
		t.Errorf("identity %v %x", d.Rloc16, d.ExtMACAddress)
	}
	if d.Mode == nil || *d.Mode != (thread.Mode{RxOnWhenIdle: true, FullThreadDevice: true, FullNetworkData: true}) {
		t.Errorf("mode %+v", d.Mode)
	}
	if d.Route64 == nil || d.Route64.IDSequence != 198 || len(d.Route64.Entries) != 5 || d.Route64.Entries[0].RouterID != 5 ||
		d.Route64.Entries[2] != (thread.Route64Entry{RouterID: 29, LinkQualityOut: 3, LinkQualityIn: 3, RouteCost: 1}) {
		t.Errorf("route64 %+v", d.Route64)
	}
	if d.LeaderData == nil || *d.LeaderData != (thread.LeaderData{PartitionID: 305419896, Weighting: 64, DataVersion: 147, StableDataVersion: 205, LeaderRouterID: 61}) {
		t.Errorf("leader data %+v", d.LeaderData)
	}
	if d.MacCounters == nil || d.MacCounters.IfInUcastPkts != 76259 || d.MacCounters.IfOutDiscards != 29 {
		t.Errorf("mac counters %+v", d.MacCounters)
	}
	if len(d.ChildTable) != 1 || d.ChildTable[0].ChildID != 3 || d.ChildTable[0].TimeoutExponent != 12 || d.ChildTable[0].Mode.RxOnWhenIdle ||
		d.ChildTable[0].TimeoutSeconds != 256 {
		t.Errorf("child table %+v", d.ChildTable)
	}
}

// thread-br-client/test/TranslationTest.ts:85-96.
func TestTranslateWithoutChildren(t *testing.T) {
	t.Parallel()
	d := translate(t, entryWithRloc16(t, 18432))
	if d.Rloc16 == nil || *d.Rloc16 != 18432 || d.ChildTable == nil || len(d.ChildTable) != 0 {
		t.Errorf("entry %+v", d)
	}
}

// thread-br-client/test/TranslationTest.ts:98-112: a hex-string rloc16, and a malformed one
// omitted rather than mis-decoded.
func TestTranslateRloc16(t *testing.T) {
	t.Parallel()
	if d := translate(t, map[string]any{"rloc16": "0x4800"}); d.Rloc16 == nil || *d.Rloc16 != 18432 {
		t.Errorf("hex rloc16 %v", d.Rloc16)
	}
	for _, bad := range []string{"0x10000", "garbage"} {
		if d := translate(t, map[string]any{"rloc16": bad}); d.Rloc16 != nil {
			t.Errorf("%s: rloc16 %d", bad, *d.Rloc16)
		}
	}
}

// thread-br-client/test/TranslationTest.ts:120-126.
func TestTranslateAllFixtureEntries(t *testing.T) {
	t.Parallel()
	for _, e := range fixtureEntries(t) {
		if d := translate(t, e); d.Rloc16 == nil {
			t.Errorf("entry %v without rloc16", e)
		}
	}
}

func mle(overrides map[string]any) map[string]any {
	m := map[string]any{
		"radioDisabledCount": json.Number("1"), "detachedRoleCount": json.Number("2"), "childRoleCount": json.Number("3"),
		"routerRoleCount": json.Number("4"), "leaderRoleCount": json.Number("5"), "attachAttemptsCount": json.Number("6"),
		"partIdChangesCount": json.Number("7"), "betterPartIdAttachAttemptsCount": json.Number("8"), "newParentCount": json.Number("9"),
		"totalTrackingTime": json.Number("100000"), "radioDisabledTime": json.Number("200000"), "detachedRoleTime": json.Number("300000"),
		"childRoleTime": json.Number("400000"), "routerRoleTime": json.Number("500000"), "leaderRoleTime": json.Number("600000"),
	}
	for k, v := range overrides {
		m[k] = v
	}
	return m
}

// thread-br-client/test/TranslationTest.ts:178-225: the OTBR's MLE counter names, absence,
// 64-bit strings, the uint64 bound, a negative and a malformed counter.
func TestTranslateMleCounters(t *testing.T) {
	t.Parallel()
	d := translate(t, map[string]any{"mleCounters": mle(nil)})
	if c := d.MleCounters; c == nil || c.DisabledRole != 1 || c.ParentChanges != 9 || c.TrackedTime != 100000 || c.LeaderTime != 600000 {
		t.Errorf("counters %+v", d.MleCounters)
	}
	if d := translate(t, map[string]any{"rloc16": json.Number("0")}); d.MleCounters != nil {
		t.Error("counters without the field")
	}
	d = translate(t, map[string]any{"mleCounters": mle(map[string]any{"totalTrackingTime": "9007199254740993", "radioDisabledTime": "200000"})})
	if c := d.MleCounters; c == nil || c.TrackedTime != 9007199254740993 || c.DisabledTime != 200000 {
		t.Errorf("string counters %+v", d.MleCounters)
	}
	d = translate(t, map[string]any{"mleCounters": mle(map[string]any{"totalTrackingTime": "18446744073709551615"})})
	if c := d.MleCounters; c == nil || c.TrackedTime != 18446744073709551615 {
		t.Errorf("uint64 maximum %+v", d.MleCounters)
	}
	for name, v := range map[string]any{"past uint64": "18446744073709551616", "negative": json.Number("-1"), "malformed": "12.5", "number past uint64": json.Number("1e20")} {
		d := translate(t, map[string]any{"rloc16": json.Number("5120"), "mleCounters": mle(map[string]any{"totalTrackingTime": v})})
		if d.MleCounters != nil || d.Rloc16 == nil || *d.Rloc16 != 5120 {
			t.Errorf("%s: counters %+v, node %v", name, d.MleCounters, d.Rloc16)
		}
	}
	if d := translate(t, map[string]any{"mleCounters": mle(map[string]any{"childRoleCount": json.Number("70000")})}); d.MleCounters != nil {
		t.Error("a role count past uint16 was kept")
	}
	if d := translate(t, map[string]any{"mleCounters": mle(map[string]any{"totalTrackingTime": json.Number("1.5")})}); d.MleCounters == nil || d.MleCounters.TrackedTime != 1 {
		t.Errorf("a fractional time is truncated (Math.trunc): %+v", d.MleCounters)
	}
}

// thread-br-client/test/TranslationTest.ts:227-238.
func TestTranslateModeAllZero(t *testing.T) {
	t.Parallel()
	zero := json.Number("0")
	d := translate(t, map[string]any{"extAddress": "0011223344556677", "rloc16": zero, "mode": map[string]any{"rxOnWhenIdle": zero, "deviceType": zero, "networkData": zero}})
	if d.Mode == nil || *d.Mode != (thread.Mode{}) || hexOf(d.ExtMACAddress) != "0011223344556677" {
		t.Errorf("mode %+v", d.Mode)
	}
}

// The skips translation.ts:26-38 makes: a malformed field goes, the node
// stays; a record that is not an object is an error.
func TestTranslateSkipsMalformedFields(t *testing.T) {
	t.Parallel()
	d := translate(t, map[string]any{
		"rloc16":      json.Number("1024"),
		"extAddress":  "zz",
		"mode":        map[string]any{"rxOnWhenIdle": "x"},
		"route":       map[string]any{"idSequence": json.Number("1")},
		"leaderData":  map[string]any{},
		"macCounters": map[string]any{},
		"childTable":  []any{"x"},
		"version":     json.Number("5"),
	})
	if d.Rloc16 == nil || d.ExtMACAddress != nil || d.Mode != nil || d.Route64 != nil || d.LeaderData != nil || d.MacCounters != nil || d.ChildTable != nil {
		t.Errorf("entry %+v", d)
	}
	if d.Version == nil || *d.Version != 5 {
		t.Errorf("version %v", d.Version)
	}
	for _, route := range []any{
		map[string]any{"idSequence": json.Number("1"), "routeData": []any{"x"}},
		map[string]any{"idSequence": json.Number("1"), "routeData": []any{map[string]any{"routeId": json.Number("300")}}},
	} {
		if d := translate(t, map[string]any{"route": route}); d.Route64 != nil {
			t.Errorf("route %v kept", route)
		}
	}
	for _, child := range []any{
		map[string]any{"childId": json.Number("1"), "timeout": json.Number("12")},
		map[string]any{"childId": json.Number("1"), "timeout": json.Number("99"), "mode": map[string]any{}},
		map[string]any{"childId": json.Number("1"), "timeout": json.Number("2"), "mode": map[string]any{"rxOnWhenIdle": json.Number("1")}},
	} {
		if d := translate(t, map[string]any{"childTable": []any{child}}); d.ChildTable != nil {
			t.Errorf("child %v kept", child)
		}
	}
	zero := json.Number("0")
	d = translate(t, map[string]any{"childTable": []any{map[string]any{"childId": json.Number("1"), "timeout": json.Number("2"), "mode": map[string]any{"rxOnWhenIdle": zero, "deviceType": zero, "networkData": zero}}}})
	if len(d.ChildTable) != 1 || d.ChildTable[0].TimeoutSeconds != 1 {
		t.Errorf("an exponent under 4 is floored: %+v", d.ChildTable)
	}
	if _, err := Translate([]any{}); err == nil {
		t.Error("a record that is not an object")
	}
}

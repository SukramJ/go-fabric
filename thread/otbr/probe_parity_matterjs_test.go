// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package otbr

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/thread/otbr/otbrtest"
)

// The ten cases of matter.js packages/thread-br-client/test/
// OtbrRestProbeTest.ts at 862ad75f and the eleven of CaseNormalizerTest.ts,
// ported by meaning.

func probe(t *testing.T, s *stub, timeout time.Duration) *Capability {
	t.Helper()
	c, err := Probe(context.Background(), "br.example", 8081, timeout, &http.Client{Transport: s})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// OtbrRestProbeTest.ts:32-52.
func TestOtbrRestProbePascalLegacy(t *testing.T) {
	t.Parallel()
	s := newStub().on("GET", "/node", 200, otbrtest.NodePascal).on("GET", "/api/diagnostics", 404, "").on("GET", "/diagnostics", 200, "[]")
	c := probe(t, s, 500*time.Millisecond)
	if c == nil || c.KeyFormat != KeyFormatPascal || c.DiagnosticsAPI != DiagnosticsAPILegacy || c.NetworkName != "TestNet" ||
		hexOf(c.ExtPANID) != "1122334455667788" || c.BaseURL != "http://br.example:8081" || c.ProbedAt.IsZero() {
		t.Errorf("capability %+v", c)
	}
}

// OtbrRestProbeTest.ts:54-71.
func TestOtbrRestProbeCamelCollection(t *testing.T) {
	t.Parallel()
	s := newStub().on("GET", "/node", 200, otbrtest.NodeCamel).on("GET", "/api/diagnostics", 200, "[]")
	c := probe(t, s, 500*time.Millisecond)
	if c == nil || c.KeyFormat != KeyFormatCamel || c.DiagnosticsAPI != DiagnosticsAPICollection || c.NetworkName != "MockThread" ||
		hexOf(c.ExtPANID) != "1122334455667799" {
		t.Errorf("capability %+v", c)
	}
}

// OtbrRestProbeTest.ts:73-86.
func TestOtbrRestProbeNoDiagnostics(t *testing.T) {
	t.Parallel()
	s := newStub().on("GET", "/node", 200, otbrtest.NodeCamel).on("GET", "/api/diagnostics", 404, "").on("GET", "/diagnostics", 404, "")
	if c := probe(t, s, 500*time.Millisecond); c == nil || c.DiagnosticsAPI != DiagnosticsAPINone {
		t.Errorf("capability %+v", c)
	}
}

// OtbrRestProbeTest.ts:88-103: the probe reads /node and the diagnostics
// statuses, never /api/actions.
func TestOtbrRestProbeNeverFetchesActions(t *testing.T) {
	t.Parallel()
	s := newStub().on("GET", "/node", 200, otbrtest.NodeCamel).on("GET", "/api/diagnostics", 200, "[]")
	probe(t, s, 500*time.Millisecond)
	seen := make([]string, 0, len(s.requests))
	for _, r := range s.requests {
		seen = append(seen, r.url)
	}
	if !slices.Equal(seen, []string{"http://br.example:8081/node", "http://br.example:8081/api/diagnostics"}) {
		t.Errorf("requests %v", seen)
	}
}

// OtbrRestProbeTest.ts:105-187: a network error, a body that is not JSON,
// an object without networkName/extPanId, an empty object, an error
// status and a timeout are all "not an OTBR".
func TestOtbrRestProbeMisses(t *testing.T) {
	t.Parallel()
	for name, s := range map[string]*stub{
		"garbage":       newStub().on("GET", "/node", 200, "not even json"),
		"no fields":     newStub().on("GET", "/node", 200, `{"foo":1}`),
		"empty object":  newStub().on("GET", "/node", 200, `{}`),
		"error status":  newStub().on("GET", "/node", 500, "err"),
		"network error": {routes: map[string]func(*http.Request) (*http.Response, error){"GET /node": func(*http.Request) (*http.Response, error) { return nil, errors.New("fetch failed") }}},
		"timeout": {routes: map[string]func(*http.Request) (*http.Response, error){"GET /node": func(r *http.Request) (*http.Response, error) {
			<-r.Context().Done()
			return nil, r.Context().Err()
		}}},
	} {
		if c := probe(t, s, 20*time.Millisecond); c != nil {
			t.Errorf("%s: capability %+v, want none", name, c)
		}
	}
	if _, err := Probe(context.Background(), "", 0, 0, nil); err == nil {
		t.Error("a probe without a host")
	}
}

// CaseNormalizerTest.ts:10-33: the irregular keys and the first letter.
func TestNormalizeKeysIrregularAndFirstLetter(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{
		"BaId": "baId", "IP6AddressList": "ip6AddressList", "MACCounters": "macCounters", "PSKc": "pskc", "RxOnWhenIdle": "rxOnWhenIdle",
	} {
		got := NormalizeKeys(map[string]any{in: 1}).(map[string]any)
		if _, ok := got[want]; !ok || len(got) != 1 {
			t.Errorf("%s → %v, want %s", in, got, want)
		}
	}
	got := NormalizeKeys(map[string]any{"MACCounters": map[string]any{"IfInErrors": 1}}).(map[string]any)
	if got["macCounters"].(map[string]any)["ifInErrors"] != 1 {
		t.Errorf("nested %v", got)
	}
}

// CaseNormalizerTest.ts:35-82: idempotence, nesting, arrays, primitives,
// the empty containers, deep mixtures.
func TestNormalizeKeysStructure(t *testing.T) {
	t.Parallel()
	in := map[string]any{"BaId": "x", "IP6AddressList": []any{"a", "b"}, "MACCounters": map[string]any{"IfInErrors": 1, "IfOutErrors": 2}, "NetworkName": "TestNet"}
	once := NormalizeKeys(in)
	if twice := NormalizeKeys(once); !equalJSON(once, twice) {
		t.Errorf("not idempotent: %v / %v", once, twice)
	}
	nested := NormalizeKeys(map[string]any{"LeaderData": map[string]any{"PartitionId": 1, "LeaderRouterId": 2}})
	if !equalJSON(nested, map[string]any{"leaderData": map[string]any{"partitionId": 1, "leaderRouterId": 2}}) {
		t.Errorf("nested %v", nested)
	}
	arr := NormalizeKeys([]any{map[string]any{"ChildId": 1}, map[string]any{"ChildId": 2}})
	if !equalJSON(arr, []any{map[string]any{"childId": 1}, map[string]any{"childId": 2}}) {
		t.Errorf("array %v", arr)
	}
	for _, v := range []any{42, "hello", true, nil} {
		if NormalizeKeys(v) != v {
			t.Errorf("primitive %v changed", v)
		}
	}
	if !equalJSON(NormalizeKeys(map[string]any{}), map[string]any{}) || !equalJSON(NormalizeKeys([]any{}), []any{}) {
		t.Error("empty containers changed")
	}
	deep := NormalizeKeys(map[string]any{"Outer": []any{map[string]any{"Inner": map[string]any{"DeepKey": 1}}, map[string]any{"Inner": map[string]any{"DeepKey": 2}}}})
	want := map[string]any{"outer": []any{map[string]any{"inner": map[string]any{"deepKey": 1}}, map[string]any{"inner": map[string]any{"deepKey": 2}}}}
	if !equalJSON(deep, want) {
		t.Errorf("deep %v", deep)
	}
	if NormalizeKeys(map[string]any{"": 1}).(map[string]any)[""] != 1 {
		t.Error("the empty key changed")
	}
}

// detectKeyFormat (caseNormalizer.ts:32-47): any upper-case first letter
// is pascal, lower-case keys camel, no non-empty key no signal.
func TestDetectKeyFormat(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		in   map[string]any
		want KeyFormat
	}{
		{map[string]any{"networkName": 1, "State": 1}, KeyFormatPascal},
		{map[string]any{"networkName": 1}, KeyFormatCamel},
		{map[string]any{}, ""},
		{map[string]any{"": 1}, ""},
	} {
		if got := DetectKeyFormat(c.in); got != c.want {
			t.Errorf("%v: %q, want %q", c.in, got, c.want)
		}
	}
}

func equalJSON(a, b any) bool {
	switch x := a.(type) {
	case map[string]any:
		y, ok := b.(map[string]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for k, v := range x {
			if !equalJSON(v, y[k]) {
				return false
			}
		}
		return true
	case []any:
		y, ok := b.([]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for i := range x {
			if !equalJSON(x[i], y[i]) {
				return false
			}
		}
		return true
	}
	return a == b
}

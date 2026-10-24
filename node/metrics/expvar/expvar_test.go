// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package expvarmetrics_test

import (
	"encoding/json"
	"expvar"
	"testing"

	"github.com/SukramJ/go-fabric/contract"
	expvarmetrics "github.com/SukramJ/go-fabric/node/metrics/expvar"
)

// TestRoundTrip: every name of the set is published at zero, a counter
// adds and a gauge sets, and expvar's own view (what /debug/vars serves)
// reads the same values; a second New with the prefix shares them.
func TestRoundTrip(t *testing.T) {
	m := expvarmetrics.New("cbtest.")
	for _, name := range contract.MetricNames() {
		v := expvar.Get("cbtest." + name)
		if v == nil {
			t.Fatalf("%s not published", name)
		}
		if v.String() != "0" {
			t.Errorf("%s = %s before any event", name, v.String())
		}
	}
	m.Counter(contract.MetricIMReceived, 2)
	m.Counter(contract.MetricIMReceived, 3)
	m.Gauge(contract.MetricSessionsOpen, 4)
	m.Gauge(contract.MetricSessionsOpen, 1)
	m.Counter("custom.name", 1)
	var got int64
	if err := json.Unmarshal([]byte(expvar.Get("cbtest."+contract.MetricIMReceived).String()), &got); err != nil || got != 5 {
		t.Errorf("im.received = %d (%v), want 5", got, err)
	}
	if v := m.Value(contract.MetricSessionsOpen); v != 1 {
		t.Errorf("sessions.open = %d, want 1", v)
	}
	if expvar.Get("cbtest.custom.name") == nil {
		t.Error("a name outside the set is not published on first use")
	}
	again := expvarmetrics.New("cbtest.")
	if v := again.Value(contract.MetricIMReceived); v != 5 {
		t.Errorf("a second New sees im.received = %d, want the shared 5", v)
	}
	expvar.NewString("cbtest.other")
	if v := again.Value("other"); v != 0 {
		t.Errorf("a name taken by another type = %d", v)
	}
}

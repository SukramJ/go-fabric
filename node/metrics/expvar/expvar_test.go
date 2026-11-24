// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package expvarmetrics_test

import (
	"encoding/json"
	"expvar"
	"strconv"
	"sync/atomic"
	"testing"

	"thingfabric.dev/go-fabric/contract"
	expvarmetrics "thingfabric.dev/go-fabric/node/metrics/expvar"
)

// roundTripRunA8 numbers the runs of TestRoundTrip in one process: expvar
// registration is process-global and permanent, so each run (go test
// -count=N) publishes under a prefix of its own.
var roundTripRunA8 atomic.Int64

// TestRoundTrip: every name of the set is published at zero, a counter
// adds and a gauge sets, and expvar's own view (what /debug/vars serves)
// reads the same values; a second New with the prefix shares them.
func TestRoundTrip(t *testing.T) {
	prefix := "cbtest" + strconv.FormatInt(roundTripRunA8.Add(1), 10) + "."
	m := expvarmetrics.New(prefix)
	for _, name := range contract.MetricNames() {
		v := expvar.Get(prefix + name)
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
	if err := json.Unmarshal([]byte(expvar.Get(prefix+contract.MetricIMReceived).String()), &got); err != nil || got != 5 {
		t.Errorf("im.received = %d (%v), want 5", got, err)
	}
	if v := m.Value(contract.MetricSessionsOpen); v != 1 {
		t.Errorf("sessions.open = %d, want 1", v)
	}
	if expvar.Get(prefix+"custom.name") == nil {
		t.Error("a name outside the set is not published on first use")
	}
	again := expvarmetrics.New(prefix)
	if v := again.Value(contract.MetricIMReceived); v != 5 {
		t.Errorf("a second New sees im.received = %d, want the shared 5", v)
	}
	expvar.NewString(prefix + "other")
	if v := again.Value("other"); v != 0 {
		t.Errorf("a name taken by another type = %d", v)
	}
}

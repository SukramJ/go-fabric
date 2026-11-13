// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"encoding/json"
	"expvar"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/node"
)

// TestMetricsEndpointServesNoCommandLine: --metrics-expvar answers any HTTP
// client of its address, and the standard library's own /debug/vars
// publishes os.Args as "cmdline" — the setup passcode, the test enable key
// and the simulated Wi-Fi passphrase are flags of this daemon. The endpoint
// serves the node's metrics and nothing of the command line (review II,
// R2-P0-sec-E03). Not parallel: it replaces os.Args.
func TestMetricsEndpointServesNoCommandLine(t *testing.T) {
	const (
		passcode   = "34567812"
		enableKey  = "00112233445566778899aabbccddeeff"
		passphrase = "correct-horse-battery"
	)
	saved := os.Args
	os.Args = []string{
		"reference-bridge", "--metrics-expvar", "127.0.0.1:0",
		"--passcode", passcode, "--enable-key=" + enableKey, "--wifi-sim-ssid", "HomeNet:" + passphrase,
	}
	t.Cleanup(func() { os.Args = saved })

	var cfg node.Config
	applyMetrics(&cfg, "127.0.0.1:0")
	cfg.Metrics.Counter(contract.MetricSessionsOpen, 3)

	get := func(h http.Handler) string {
		t.Helper()
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/debug/vars", http.NoBody))
		body, err := io.ReadAll(rec.Result().Body)
		if err != nil || rec.Code != http.StatusOK {
			t.Fatalf("GET /debug/vars: status %d, %v", rec.Code, err)
		}
		return string(body)
	}

	// The premise: the standard library's handler does publish them.
	if std := get(expvar.Handler()); !strings.Contains(std, passcode) || !strings.Contains(std, passphrase) {
		t.Fatal("precondition: expvar's own handler no longer publishes the command line; this test proves nothing")
	}

	body := get(metricsHandler())
	for name, secret := range map[string]string{"--passcode": passcode, "--enable-key": enableKey, "the --wifi-sim-ssid passphrase": passphrase} {
		if strings.Contains(body, secret) {
			t.Errorf("the metrics endpoint serves %s", name)
		}
	}
	if strings.Contains(body, "cmdline") {
		t.Error("the metrics endpoint serves expvar's cmdline")
	}
	// What the endpoint is documented to serve is still there, as JSON.
	var vars map[string]json.RawMessage
	if err := json.Unmarshal([]byte(body), &vars); err != nil {
		t.Fatalf("the body is not a JSON object: %v\n%s", err, body)
	}
	if got := string(vars[metricsPrefix+contract.MetricSessionsOpen]); got != "3" {
		t.Errorf("%s%s = %q, want 3", metricsPrefix, contract.MetricSessionsOpen, got)
	}
	for name := range vars {
		if !strings.HasPrefix(name, metricsPrefix) {
			t.Errorf("the metrics endpoint serves %q, which is not one of the node's metrics", name)
		}
	}
}

// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package otbrtest_test

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/SukramJ/go-fabric/thread/otbr/otbrtest"
)

func do(t *testing.T, srv *otbrtest.Server, method, path, body string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(method, srv.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

// The fake's agent behaviour: the attach after an enable, the 409s, the
// faults, the casing, the diagnostics flavours.
func TestServer(t *testing.T) {
	t.Parallel()
	for _, camel := range []bool{false, true} {
		srv := otbrtest.New(otbrtest.Options{Camel: camel, AttachPolls: 2})
		if srv.Host() == "" || srv.Port() == 0 {
			t.Fatal("no address")
		}
		if st, _ := do(t, srv, "PUT", "/node/state", `"enable"`); st != http.StatusConflict {
			t.Errorf("enable without a dataset: %d", st)
		}
		if st, _ := do(t, srv, "GET", "/node/dataset/active", ""); st != http.StatusNoContent {
			t.Errorf("no dataset: %d", st)
		}
		if st, _ := do(t, srv, "PUT", "/node/dataset/active", "zz"); st != http.StatusBadRequest {
			t.Errorf("bad hex: %d", st)
		}
		if st, _ := do(t, srv, "PUT", "/node/dataset/active", "0e080000000000000001020811223344556677880306616263646566"); st != http.StatusOK {
			t.Errorf("put: %d", st)
		}
		do(t, srv, "PUT", "/node/state", `"enable"`)
		var states []string
		for range 3 {
			_, body := do(t, srv, "GET", "/node/state", "")
			states = append(states, strings.TrimSpace(body))
		}
		if strings.Join(states, ",") != `"detached","detached","leader"` {
			t.Errorf("states %v", states)
		}
		if st, _ := do(t, srv, "PUT", "/node/dataset/active", "00"); st != http.StatusConflict {
			t.Errorf("replace while up: %d", st)
		}
		if st, _ := do(t, srv, "DELETE", "/node/dataset/active", ""); st != http.StatusConflict {
			t.Errorf("delete while up: %d", st)
		}
		if st, _ := do(t, srv, "PUT", "/node/state", `"bogus"`); st != http.StatusBadRequest {
			t.Errorf("bogus state: %d", st)
		}
		_, node := do(t, srv, "GET", "/node", "")
		var obj map[string]any
		if err := json.Unmarshal([]byte(node), &obj); err != nil {
			t.Fatal(err)
		}
		key := "ExtPanId"
		if camel {
			key = "extPanId"
		}
		if obj[key] != "1122334455667788" {
			t.Errorf("camel %v: node %v", camel, obj)
		}
		for _, path := range []string{"/node/ba-id", "/node/ext-address", "/node/ext-panid", "/node/rloc16", "/node/rloc", "/node/network-name", "/node/num-of-router", "/node/leader-data", "/diagnostics", "/node/dataset/active"} {
			if st, _ := do(t, srv, "GET", path, ""); st != http.StatusOK {
				t.Errorf("%s: %d", path, st)
			}
		}
		if st, _ := do(t, srv, "GET", "/api/diagnostics", ""); st != http.StatusNotFound {
			t.Errorf("legacy /api/diagnostics: %d", st)
		}
		do(t, srv, "PUT", "/node/dataset/pending", "0e0800")
		if len(srv.Pending()) != 3 || srv.Active() == nil || srv.State() != "leader" {
			t.Errorf("pending %x, active %x, %s", srv.Pending(), srv.Active(), srv.State())
		}
		srv.SetState("disabled")
		if st, _ := do(t, srv, "DELETE", "/node/dataset/active", ""); st != http.StatusOK || srv.Active() != nil {
			t.Errorf("delete: %d", st)
		}
		srv.Fail("GET", "/node", http.StatusTeapot)
		if st, _ := do(t, srv, "GET", "/node", ""); st != http.StatusTeapot {
			t.Errorf("fault: %d", st)
		}
		srv.Fail("GET", "/node", 0)
		srv.SetDown(true)
		if st, _ := do(t, srv, "GET", "/node", ""); st != http.StatusServiceUnavailable {
			t.Errorf("down: %d", st)
		}
		srv.SetDown(false)
		if st, _ := do(t, srv, "GET", "/nothing", ""); st != http.StatusNotFound {
			t.Errorf("unknown: %d", st)
		}
		if len(srv.Requests()) == 0 {
			t.Error("no requests recorded")
		}
		srv.Close()
	}
	for flavour, want := range map[string][2]int{"collection": {200, 200}, "none": {404, 404}} {
		srv := otbrtest.New(otbrtest.Options{Diagnostics: flavour, Active: []byte{0x02, 0x08, 1, 2, 3, 4, 5, 6, 7, 8}})
		if st, _ := do(t, srv, "GET", "/api/diagnostics", ""); st != want[0] {
			t.Errorf("%s /api/diagnostics: %d", flavour, st)
		}
		if st, _ := do(t, srv, "GET", "/diagnostics", ""); st != want[1] {
			t.Errorf("%s /diagnostics: %d", flavour, st)
		}
		if srv.State() != "leader" {
			t.Errorf("%s: a border router with a dataset starts as %s", flavour, srv.State())
		}
		srv.Close()
	}
	srv := otbrtest.New(otbrtest.Options{Active: []byte{0x02, 0x05}})
	defer srv.Close()
	if _, body := do(t, srv, "GET", "/node/ext-panid", ""); strings.TrimSpace(body) != `"0000000000000000"` {
		t.Errorf("a truncated dataset's ext PAN ID: %s", body)
	}
}

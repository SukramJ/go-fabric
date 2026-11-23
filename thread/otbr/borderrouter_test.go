// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package otbr

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"thingfabric.dev/go-fabric/thread"
	"thingfabric.dev/go-fabric/thread/otbr/otbrtest"
)

// The adapter against the fake border router, in both key formats:
// GenericThreadBorderRouterDelegate.cpp at connectedhomeip 6170af84 over
// REST (ADR 0027 decision 4).

func fakeDataset(t *testing.T) []byte {
	t.Helper()
	ds, err := thread.DecodeDatasetHex(otbrtest.DatasetActiveHex)
	if err != nil {
		t.Fatal(err)
	}
	return ds.Raw
}

func newBR(t *testing.T, opts otbrtest.Options) (*BorderRouter, *otbrtest.Server) {
	t.Helper()
	srv := otbrtest.New(opts)
	t.Cleanup(srv.Close)
	c, err := NewClient(Config{Host: srv.Host(), Port: srv.Port(), Timeout: 2 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	br, err := NewBorderRouter(c, BorderRouterConfig{Name: "go-fabric OTBR", ThreadVersion: 5, PollInterval: 5 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	return br, srv
}

func TestBorderRouterReads(t *testing.T) {
	t.Parallel()
	for _, camel := range []bool{false, true} {
		br, _ := newBR(t, otbrtest.Options{Camel: camel, Active: fakeDataset(t)})
		ctx := context.Background()
		if br.BorderRouterName() != "go-fabric OTBR" || br.Client() == nil {
			t.Error("name or client")
		}
		if id, err := br.BorderAgentID(ctx); err != nil || len(id) != 16 {
			t.Errorf("camel %v: agent id %x, %v", camel, id, err)
		}
		if v, err := br.ThreadVersion(ctx); err != nil || v != 5 {
			t.Errorf("camel %v: version %d, %v", camel, v, err)
		}
		if on, err := br.InterfaceEnabled(ctx); err != nil || !on {
			t.Errorf("camel %v: interface %v, %v", camel, on, err)
		}
		if ds, err := br.Dataset(ctx, false); err != nil || !bytes.Equal(ds, fakeDataset(t)) {
			t.Errorf("camel %v: active %x, %v", camel, ds, err)
		}
		if ds, err := br.Dataset(ctx, true); err != nil || ds != nil {
			t.Errorf("camel %v: pending %x, %v", camel, ds, err)
		}
		d, err := br.Diagnostics(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if d.Role != thread.RoleLeader || d.Rloc16 != 0x7400 || hexOf(d.ExtAddress) != "0011223344556601" || len(d.Mesh) != 5 ||
			d.Self == nil || *d.Self.Rloc16 != 0x7400 || d.LeaderData.LeaderRouterID != 61 {
			t.Errorf("camel %v: diagnostics %+v", camel, d)
		}
	}
}

// SetActiveDataset onto a border router without a dataset: PUT, enable,
// attach after the polls; uncommitted, the fail-safe's revert clears it
// (GenericThreadBorderRouterDelegate.cpp:127-143, :191-212).
func TestBorderRouterActivationAndRevert(t *testing.T) {
	t.Parallel()
	br, srv := newBR(t, otbrtest.Options{AttachPolls: 3})
	ctx := context.Background()
	ds := fakeDataset(t)
	if err := br.SetActiveDataset(ctx, ds); err != nil {
		t.Fatal(err)
	}
	if srv.State() != "leader" || !bytes.Equal(srv.Active(), ds) {
		t.Errorf("after the activation: %s, %x", srv.State(), srv.Active())
	}
	if err := br.RevertActiveDataset(ctx); err != nil {
		t.Fatal(err)
	}
	if srv.State() != "disabled" || srv.Active() != nil {
		t.Errorf("after the revert: %s, %x", srv.State(), srv.Active())
	}
	// Committed, a revert keeps it.
	if err := br.SetActiveDataset(ctx, ds); err != nil {
		t.Fatal(err)
	}
	if err := br.CommitActiveDataset(ctx); err != nil {
		t.Fatal(err)
	}
	if err := br.RevertActiveDataset(ctx); err != nil || srv.Active() == nil {
		t.Errorf("a committed dataset was reverted: %v", err)
	}
}

// A border router without the DELETE route (ot-br-posix at main answers
// 405): the revert disables the interface and leaves the dataset in place.
func TestBorderRouterRevertWithoutDeleteRoute(t *testing.T) {
	t.Parallel()
	br, srv := newBR(t, otbrtest.Options{AttachPolls: 3, NoDelete: true})
	ctx := context.Background()
	ds := fakeDataset(t)
	if err := br.SetActiveDataset(ctx, ds); err != nil {
		t.Fatal(err)
	}
	if err := br.RevertActiveDataset(ctx); err != nil {
		t.Fatalf("revert against a 405: %v", err)
	}
	if srv.State() != "disabled" || !bytes.Equal(srv.Active(), ds) {
		t.Errorf("after the revert: %s, %x, want disabled with the dataset in place", srv.State(), srv.Active())
	}
	// A second activation still works from the disabled state.
	if err := br.SetActiveDataset(ctx, ds); err != nil || srv.State() != "leader" {
		t.Errorf("re-activation: %v, %s", err, srv.State())
	}
}

// Replacing an active dataset disables the interface first (the OTBR's
// 409), an identical attached one is a no-op, the empty one clears.
func TestBorderRouterReplaceAndClear(t *testing.T) {
	t.Parallel()
	ds := fakeDataset(t)
	br, srv := newBR(t, otbrtest.Options{Active: ds})
	ctx := context.Background()
	n := len(srv.Requests())
	if err := br.SetActiveDataset(ctx, ds); err != nil {
		t.Fatal(err)
	}
	for _, r := range srv.Requests()[n:] {
		if r.Method != http.MethodGet {
			t.Errorf("the identical dataset wrote %s %s", r.Method, r.Path)
		}
	}
	other := bytes.Clone(ds)
	other[len(other)-1] ^= 1
	if err := br.SetActiveDataset(ctx, other); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(srv.Active(), other) || srv.State() != "leader" {
		t.Errorf("replace: %x, %s", srv.Active(), srv.State())
	}
	// A replacement keeps the dataset configured: a revert does not clear.
	if err := br.RevertActiveDataset(ctx); err != nil || srv.Active() == nil {
		t.Errorf("a replaced dataset was reverted: %v", err)
	}
	if err := br.SetActiveDataset(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if srv.Active() != nil || srv.State() != "disabled" {
		t.Errorf("clear: %x, %s", srv.Active(), srv.State())
	}
	if err := br.SetPendingDataset(ctx, ds); err != nil || !bytes.Equal(srv.Pending(), ds) {
		t.Errorf("pending %x, %v", srv.Pending(), err)
	}
}

// An activation that does not attach ends with its context; the REST
// failures surface.
func TestBorderRouterFailures(t *testing.T) {
	t.Parallel()
	br, srv := newBR(t, otbrtest.Options{AttachPolls: 1 << 30})
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := br.SetActiveDataset(ctx, fakeDataset(t)); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("an activation that never attaches = %v", err)
	}
	bg := context.Background()
	srv.Fail("PUT", "/node/state", 500)
	if err := br.SetActiveDataset(bg, fakeDataset(t)); err == nil {
		t.Error("a refused disable passed")
	}
	if err := br.RevertActiveDataset(bg); err == nil {
		t.Error("a refused revert passed")
	}
	srv.Fail("PUT", "/node/state", 0)
	srv.Fail("PUT", "/node/dataset/active", 500)
	srv.SetState("disabled")
	if err := br.SetActiveDataset(bg, fakeDataset(t)); err == nil {
		t.Error("a refused PUT passed")
	}
	srv.Fail("PUT", "/node/dataset/active", 0)
	srv.Fail("DELETE", "/node/dataset/active", 500)
	if err := br.SetActiveDataset(bg, nil); err == nil {
		t.Error("a refused DELETE passed")
	}
	srv.Fail("DELETE", "/node/dataset/active", 0)
	srv.SetDown(true)
	if _, err := br.InterfaceEnabled(bg); err == nil {
		t.Error("a silent border router's interface")
	}
	if err := br.CommitActiveDataset(bg); err == nil {
		t.Error("a silent border router committed")
	}
	if err := br.SetActiveDataset(bg, fakeDataset(t)); err == nil {
		t.Error("a silent border router activated")
	}
	if _, err := br.Diagnostics(bg); err == nil {
		t.Error("a silent border router's diagnostics")
	}
	srv.SetDown(false)
	srv.Fail("GET", "/diagnostics", 404)
	if d, err := br.Diagnostics(bg); err != nil || d.Self != nil || d.Mesh != nil {
		t.Errorf("without /diagnostics: %+v, %v", d, err)
	}
	if _, err := NewBorderRouter(nil, BorderRouterConfig{Name: "x"}); err == nil {
		t.Error("no client")
	}
	if _, err := NewBorderRouter(br.Client(), BorderRouterConfig{}); err == nil {
		t.Error("no name")
	}
	nov, _ := NewBorderRouter(br.Client(), BorderRouterConfig{Name: "x"})
	if _, err := nov.ThreadVersion(bg); !errors.Is(err, ErrNoThreadVersion) {
		t.Errorf("no version = %v", err)
	}
}

// The border router's own record carries the Version (DiagnosticResponse
// .version); a record found by RLOC16 alone counts as its own.
func TestBorderRouterVersionFromItsRecord(t *testing.T) {
	t.Parallel()
	br, _ := newBR(t, otbrtest.Options{DiagnosticsBody: `[{"Rloc16":29696,"Version":4},{"Rloc16":1024,"ExtAddress":"00"},"x"]`})
	if v, err := br.ThreadVersion(context.Background()); err != nil || v != 4 {
		t.Errorf("version %d, %v", v, err)
	}
	d, _ := br.Diagnostics(context.Background())
	if len(d.Mesh) != 2 {
		t.Errorf("mesh %+v", d.Mesh)
	}
}

// TestResponseBodyBounded (W4-ctlint-10): an answer over maxResponseBody
// fails the request instead of being buffered whole; matter.js reads the
// whole answer (OtbrRestClient.ts:757-788).
func TestResponseBodyBounded(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"State":"`))
		_, _ = w.Write(bytes.Repeat([]byte("x"), maxResponseBody))
		_, _ = w.Write([]byte(`"}`))
	}))
	t.Cleanup(srv.Close)
	u, _ := url.Parse(srv.URL)
	port, _ := strconv.Atoi(u.Port())
	c, err := NewClient(Config{Host: u.Hostname(), Port: port, Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Node(context.Background()); err == nil {
		t.Fatal("an answer over maxResponseBody was read")
	}
}

// TestResponseBodyBoundIsOneMiB: an answer over 1 MiB fails as a transport
// failure before any decode — a JSON body decodes into a tree many times
// its size (review II, R2-P6-bound-2023).
func TestResponseBodyBoundIsOneMiB(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"State":"`))
		_, _ = w.Write(bytes.Repeat([]byte("x"), 2<<20))
		_, _ = w.Write([]byte(`"}`))
	}))
	t.Cleanup(srv.Close)
	u, _ := url.Parse(srv.URL)
	port, _ := strconv.Atoi(u.Port())
	c, err := NewClient(Config{Host: u.Hostname(), Port: port, Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.Node(context.Background())
	expectCode(t, err, CodeUnreachable)
}

// TestNumOfRouterIsAByteCount: a router count that is no integer within a
// byte is a protocol error, in /node and /node/num-of-router alike, as
// Rloc16 is range-checked (review II, R2-P6-wire-2024).
func TestNumOfRouterIsAByteCount(t *testing.T) {
	t.Parallel()
	for _, bad := range []any{1e300, 2.5, 256, -1} {
		s := newStub().on("GET", "/node", 200, camelWith(t, "routerCount", bad))
		_, err := s.client(t).Node(context.Background())
		expectCode(t, err, CodeProtocol)
		body, _ := json.Marshal(bad)
		s = newStub().on("GET", "/node/num-of-router", 200, string(body))
		_, err = s.client(t).NumOfRouter(context.Background())
		expectCode(t, err, CodeProtocol)
	}
	s := newStub().on("GET", "/node", 200, camelWith(t, "routerCount", 255)).on("GET", "/node/num-of-router", 200, "3")
	if node, err := s.client(t).Node(context.Background()); err != nil || node.NumOfRouter != 255 {
		t.Errorf("node %+v, %v", node, err)
	}
	if n, err := s.client(t).NumOfRouter(context.Background()); err != nil || n != 3 {
		t.Errorf("num-of-router %d, %v", n, err)
	}
}

// TestFailedActivationReenablesTheInterface: an activation that disabled
// the Thread interface of a border router with a dataset and then failed
// enables it again, its error joined (review II, R2-P6-err-2025).
func TestFailedActivationReenablesTheInterface(t *testing.T) {
	t.Parallel()
	for _, fail := range []string{"/node/dataset/active", "/node/state enable"} {
		t.Run(fail, func(t *testing.T) {
			t.Parallel()
			br, srv := newBR(t, otbrtest.Options{Active: fakeDataset(t)})
			other := bytes.Clone(fakeDataset(t))
			other[len(other)-1] ^= 0xff
			if fail == "/node/dataset/active" {
				srv.Fail("PUT", "/node/dataset/active", 409)
			}
			ctx := context.Background()
			if fail == "/node/state enable" {
				// The enable is refused once, the re-enable answered.
				hc := br.Client().http
				orig := hc.Transport
				if orig == nil {
					orig = http.DefaultTransport
				}
				refused := false
				hc.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
					if r.Method == http.MethodPut && r.URL.Path == "/node/state" && !refused {
						b, _ := r.GetBody()
						if body, _ := io.ReadAll(b); string(body) == `"enable"` {
							refused = true
							return respond(500, ""), nil
						}
					}
					return orig.RoundTrip(r)
				})
			}
			if err := br.SetActiveDataset(ctx, other); err == nil {
				t.Fatal("the failed activation succeeded")
			}
			reqs := srv.Requests()
			last := reqs[len(reqs)-1]
			if last.Method != http.MethodPut || last.Path != "/node/state" || last.Body != `"enable"` {
				t.Fatalf("last request %+v, want the interface enabled again", last)
			}
			if srv.State() == "disabled" {
				t.Fatal("the border router is left disabled")
			}
		})
	}
}

// TestDefaultClientRefusesRedirects: the default client does not follow a
// redirect — neither a GET nor the dataset PUT reaches where the answer
// points — while a host's own client is used as given (review II,
// R2-P6-sec-2028, decision 196).
func TestDefaultClientRefusesRedirects(t *testing.T) {
	t.Parallel()
	var elsewhere atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		elsewhere.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(target.Close)
	br := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+r.URL.Path, http.StatusTemporaryRedirect)
	}))
	t.Cleanup(br.Close)
	u, _ := url.Parse(br.URL)
	port, _ := strconv.Atoi(u.Port())
	c, err := NewClient(Config{Host: u.Hostname(), Port: port, Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	_, err = c.Node(ctx)
	if e := expectCode(t, err, CodeProtocol); e.HTTPStatus != http.StatusTemporaryRedirect {
		t.Errorf("status %d, want the redirect's own", e.HTTPStatus)
	}
	if err := c.setDataset(ctx, false, fakeDataset(t)); err == nil {
		t.Error("a redirected dataset PUT succeeded")
	}
	if n := elsewhere.Load(); n != 0 {
		t.Fatalf("%d requests followed the redirect", n)
	}
	own, err := NewClient(Config{Host: u.Hostname(), Port: port, Timeout: 5 * time.Second, HTTPClient: &http.Client{}})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = own.Node(ctx)
	if elsewhere.Load() == 0 {
		t.Fatal("the host's own client was not used as given")
	}
}

// formOutOfBand plays another writer of the border router (its own UI, a
// second controller): a dataset put and the interface enabled, attached
// after the server's polls — not an activation of this delegate.
func formOutOfBand(t *testing.T, br *BorderRouter, srv *otbrtest.Server, ds []byte) {
	t.Helper()
	ctx := context.Background()
	if err := br.c.setDataset(ctx, false, ds); err != nil {
		t.Fatal(err)
	}
	if err := br.c.setState(ctx, true); err != nil {
		t.Fatal(err)
	}
	srv.SetState("leader")
}

// Only an activation of this delegate leaves the dataset uncommitted: a
// CommissioningComplete that finds the border router not attached does not
// arm the fail-safe's revert against a dataset no activation of this node
// set, and a revert that cleared returns the flag (review II,
// R3-fsm-3350; decision 545, ahead of chip's
// GenericThreadBorderRouterDelegate.cpp:186-212).
func TestRevertTouchesOnlyAnUncommittedActivation(t *testing.T) {
	t.Parallel()
	ds := fakeDataset(t)
	for _, noDelete := range []bool{false, true} {
		// (a) a CommissioningComplete while the border router is detached.
		br, srv := newBR(t, otbrtest.Options{Active: ds, NoDelete: noDelete})
		ctx := context.Background()
		srv.SetState("detached")
		if err := br.CommitActiveDataset(ctx); err != nil {
			t.Fatal(err)
		}
		srv.SetState("leader")
		if err := br.RevertActiveDataset(ctx); err != nil {
			t.Fatal(err)
		}
		if srv.State() != "leader" || !bytes.Equal(srv.Active(), ds) {
			t.Errorf("noDelete %v, commit while detached + revert: %s, %x, want leader with the dataset", noDelete, srv.State(), srv.Active())
		}

		// (b) commissioned while the border router had no network, the
		// network formed out of band afterwards.
		br, srv = newBR(t, otbrtest.Options{NoDelete: noDelete})
		if err := br.CommitActiveDataset(ctx); err != nil {
			t.Fatal(err)
		}
		formOutOfBand(t, br, srv, ds)
		if err := br.RevertActiveDataset(ctx); err != nil {
			t.Fatal(err)
		}
		if srv.State() != "leader" || !bytes.Equal(srv.Active(), ds) {
			t.Errorf("noDelete %v, fresh + formed out of band + revert: %s, %x, want leader with the dataset", noDelete, srv.State(), srv.Active())
		}

		// (c) a revert that cleared an activation leaves nothing armed: a
		// network formed afterwards survives the next expiry.
		br, srv = newBR(t, otbrtest.Options{AttachPolls: 2, NoDelete: noDelete})
		if err := br.SetActiveDataset(ctx, ds); err != nil {
			t.Fatal(err)
		}
		if err := br.RevertActiveDataset(ctx); err != nil || srv.State() != "disabled" {
			t.Fatalf("noDelete %v, the uncommitted activation was not reverted: %v, %s", noDelete, err, srv.State())
		}
		formOutOfBand(t, br, srv, ds)
		if err := br.RevertActiveDataset(ctx); err != nil {
			t.Fatal(err)
		}
		if srv.State() != "leader" || !bytes.Equal(srv.Active(), ds) {
			t.Errorf("noDelete %v, second revert: %s, %x, want leader with the dataset", noDelete, srv.State(), srv.Active())
		}
	}
}

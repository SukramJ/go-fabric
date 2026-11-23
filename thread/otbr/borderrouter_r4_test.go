// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package otbr

import (
	"context"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"thingfabric.dev/go-fabric/thread/otbr/otbrtest"
)

// hookTransport hands each request to the real transport; before, refuse
// fails it with a transport error without sending it and pause blocks it
// until released, and after, hold blocks its answer until released.
type hookTransport struct {
	refuse func(*http.Request) bool
	pause  func(*http.Request) bool
	hold   func(*http.Request) bool
	held   chan struct{}
	resume chan struct{}
}

func (h *hookTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if h.refuse != nil && h.refuse(r) {
		return nil, errors.New("connection reset")
	}
	if h.pause != nil && h.pause(r) {
		h.held <- struct{}{}
		<-h.resume
	}
	resp, err := http.DefaultTransport.RoundTrip(r)
	if h.hold != nil && h.hold(r) {
		h.held <- struct{}{}
		<-h.resume
	}
	return resp, err
}

func newHookedBR(t *testing.T, opts otbrtest.Options, h *hookTransport) (*BorderRouter, *otbrtest.Server) {
	t.Helper()
	srv := otbrtest.New(opts)
	t.Cleanup(srv.Close)
	c, err := NewClient(Config{Host: srv.Host(), Port: srv.Port(), Timeout: 2 * time.Second, HTTPClient: &http.Client{Transport: h}})
	if err != nil {
		t.Fatal(err)
	}
	br, err := NewBorderRouter(c, BorderRouterConfig{Name: "go-fabric OTBR", ThreadVersion: 5, PollInterval: 5 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	return br, srv
}

func isPut(r *http.Request, path string) bool {
	return r.Method == http.MethodPut && r.URL.Path == path
}

// An activation whose outcome is not known — the PUT failed in transport,
// or the border router answered 5xx — may have written the dataset, so
// the revert stays owed, as in chip, which saves the flag false before the
// attach and leaves it on an error (GenericThreadBorderRouterDelegate.cpp
// :131, :136-140 at 6170af84): only a 4xx proves nothing was written
// (review II, R4-fsm-4302).
func TestActivationOfUnknownOutcomeKeepsTheRevertOwed(t *testing.T) {
	t.Parallel()
	ds := fakeDataset(t)
	ctx := context.Background()
	for _, name := range []string{"transport", "503"} {
		var refuse atomic.Bool
		h := &hookTransport{refuse: func(r *http.Request) bool { return refuse.Load() && isPut(r, "/node/dataset/active") }}
		br, srv := newHookedBR(t, otbrtest.Options{}, h)
		if name == "transport" {
			refuse.Store(true)
		} else {
			srv.Fail(http.MethodPut, "/node/dataset/active", http.StatusServiceUnavailable)
		}
		if err := br.SetActiveDataset(ctx, ds); err == nil {
			t.Fatalf("%s: the activation succeeded", name)
		}
		refuse.Store(false)
		srv.Fail(http.MethodPut, "/node/dataset/active", 0)
		br.mu.Lock()
		configured := br.configured
		br.mu.Unlock()
		if configured {
			t.Errorf("%s: an activation of unknown outcome left the dataset configured, want the revert owed", name)
		}
	}
}

// A clear in flight does not drop an activation that started while it ran:
// the activation's "not configured" survives the clear's end, and the next
// revert clears the activation's dataset (review II, R4-fsm-4303).
func TestClearKeepsAnActivationThatOverlapsIt(t *testing.T) {
	t.Parallel()
	ds := fakeDataset(t)
	ctx := context.Background()
	h := &hookTransport{held: make(chan struct{}), resume: make(chan struct{})}
	var holdDelete atomic.Bool
	h.hold = func(r *http.Request) bool {
		return holdDelete.Load() && r.Method == http.MethodDelete && r.URL.Path == "/node/dataset/active"
	}
	br, srv := newHookedBR(t, otbrtest.Options{}, h)

	// An uncommitted activation, then its revert, whose DELETE has landed
	// but whose answer is held.
	if err := br.SetActiveDataset(ctx, ds); err != nil {
		t.Fatal(err)
	}
	holdDelete.Store(true)
	revert := make(chan error, 1)
	go func() { revert <- br.RevertActiveDataset(ctx) }()
	<-h.held
	holdDelete.Store(false)
	if srv.Active() != nil {
		t.Fatalf("precondition: the revert's DELETE has not landed: %x", srv.Active())
	}

	// A second activation between the clear's REST calls and its end.
	if err := br.SetActiveDataset(ctx, ds); err != nil {
		t.Fatal(err)
	}
	h.resume <- struct{}{}
	if err := <-revert; err != nil {
		t.Fatal(err)
	}

	// The second activation is uncommitted: its expiry clears it.
	if err := br.RevertActiveDataset(ctx); err != nil {
		t.Fatal(err)
	}
	if srv.State() != "disabled" || srv.Active() != nil {
		t.Errorf("the overlapping activation was not reverted: %s, %x", srv.State(), srv.Active())
	}
}

// A refused activation returns the flag to what it was before it, not to
// configured: an earlier activation of this delegate that is still
// uncommitted, and whose PUT lands after the refused one read no active
// dataset, is still cleared by the revert (review II, R4-fsm-4302, the
// Fable diff read's B1).
func TestRefusedActivationKeepsAnEarlierUncommittedOne(t *testing.T) {
	t.Parallel()
	ds := fakeDataset(t)
	ctx := context.Background()
	h := &hookTransport{held: make(chan struct{}), resume: make(chan struct{})}
	var paused atomic.Bool
	h.pause = func(r *http.Request) bool {
		return isPut(r, "/node/dataset/active") && paused.CompareAndSwap(false, true)
	}
	br, srv := newHookedBR(t, otbrtest.Options{}, h)

	// Activation A is held before its PUT is sent.
	first := make(chan error, 1)
	go func() { first <- br.SetActiveDataset(ctx, ds) }()
	<-h.held

	// Activation B reads no active dataset and is refused with a 400.
	srv.Fail(http.MethodPut, "/node/dataset/active", http.StatusBadRequest)
	if err := br.SetActiveDataset(ctx, ds); err == nil {
		t.Fatal("the refused activation succeeded")
	}
	srv.Fail(http.MethodPut, "/node/dataset/active", 0)

	// A's PUT lands and A attaches; nothing commits it.
	h.resume <- struct{}{}
	if err := <-first; err != nil {
		t.Fatal(err)
	}
	br.mu.Lock()
	configured := br.configured
	br.mu.Unlock()
	if configured {
		t.Error("the refused activation marked the earlier uncommitted one configured")
	}
	if err := br.RevertActiveDataset(ctx); err != nil {
		t.Fatal(err)
	}
	if srv.State() != "disabled" || srv.Active() != nil {
		t.Errorf("the earlier uncommitted activation was not reverted: %s, %x", srv.State(), srv.Active())
	}
}

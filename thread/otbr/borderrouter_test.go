// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package otbr

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/thread"
	"github.com/SukramJ/go-fabric/thread/otbr/otbrtest"
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

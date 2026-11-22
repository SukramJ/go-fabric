// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

import (
	"bytes"
	"context"
	"testing"
	"time"

	"thingfabric.dev/go-fabric/secure/operational"
	"thingfabric.dev/go-fabric/secure/spake2"
)

// TestBasicWindowsUseFreshSalts (review II, decision 251): the PASE
// acceptor of the node's basic window answers PBKDFParamRequest with the
// salt of the window open now — a fresh 32 bytes per window open, as
// matter.js DeviceCommissioner.ts:129-136, :214 — the same salt for every
// exchange of one window, another for the next window.
func TestBasicWindowsUseFreshSalts(t *testing.T) {
	t.Parallel()
	sessions := operational.NewManager(testStore(t))
	p := paseParams{passcode: 20202021, iterations: 1000, life: context.Background(), salts: &windowSalt{passcode: 20202021, iterations: 1000}}
	saltOf := func() []byte {
		t.Helper()
		pa, err := buildPaseAdapter(sessions, &RootClusters{}, p, nil)
		if err != nil {
			t.Fatal(err)
		}
		_, resp, err := pa.ProcessPBKDFParamRequest(pbkdfParamRequest())
		if err != nil {
			t.Fatal(err)
		}
		r, err := spake2.DecodePBKDFParamResponse(resp)
		if err != nil || r.Parameters == nil {
			t.Fatalf("PBKDFParamResponse %+v, err %v", r, err)
		}
		return r.Parameters.Salt
	}
	first := time.Date(2026, 11, 18, 10, 0, 0, 0, time.UTC)
	if err := p.salts.renew(first); err != nil {
		t.Fatal(err)
	}
	a1, a2 := saltOf(), saltOf()
	if len(a1) != 32 || !bytes.Equal(a1, a2) {
		t.Fatalf("one window's exchanges: salts %x and %x; want one 32-byte salt", a1, a2)
	}
	if err := p.salts.renew(first); err != nil {
		t.Fatal(err)
	}
	if again := saltOf(); !bytes.Equal(again, a1) {
		t.Fatal("a second transition of the same window open drew a new salt")
	}
	if err := p.salts.renew(first.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	b := saltOf()
	if bytes.Equal(b, a1) {
		t.Fatalf("the next window answers the previous window's salt %x", b)
	}
	// A window that closes and opens again at the same instant (a frozen
	// clock) is another window.
	p.salts.end()
	if err := p.salts.renew(first.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if c := saltOf(); bytes.Equal(c, b) {
		t.Fatal("a window reopened at the same instant answers the closed window's salt")
	}
}

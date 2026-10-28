// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package dcl

// Ports of matter.js packages/protocol/test/dcl/DclVendorInfoServiceTest.ts
// and DclVendorInfoServiceSeedTest.ts at 862ad75f, by meaning, on the mock
// ledger of the client's parity tests (dclClientMockFetch) and a fake
// clock for the periodic refresh.

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock/clocktest"
)

const vendorsPath = "/dcl/vendorinfo/vendors"

func vendorRecord(id int, name string) map[string]any {
	return map[string]any{
		"vendorID": id, "vendorName": name, "companyLegalName": name + " Inc.", "companyPreferredName": name,
		"vendorLandingPageURL": "https://" + strings.ToLower(name) + ".example", "creator": "cosmos1", "schemaVersion": 0,
	}
}

func vendorsPage(records ...map[string]any) map[string]any {
	return map[string]any{"vendorInfo": records, "pagination": map[string]any{"next_key": nil, "total": len(records)}}
}

type vendorRig struct {
	mock  *dclClientMockFetch
	store *MemoryStore
	clock *clocktest.Fake
}

func newVendorRig() *vendorRig {
	return &vendorRig{mock: &dclClientMockFetch{}, store: &MemoryStore{}, clock: clocktest.New(time.Unix(1_000_000, 0))}
}

func (r *vendorRig) open(t *testing.T, edit func(*VendorInfoOptions)) *VendorInfoService {
	t.Helper()
	opts := VendorInfoOptions{Client: dclClientWith(r.mock, "https://dcl.test"), Store: r.store, Clock: r.clock}
	if edit != nil {
		edit(&opts)
	}
	s, err := NewVendorInfoService(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s
}

func (r *vendorRig) fetches() int {
	n := 0
	for _, c := range r.mock.callLog() {
		if strings.Contains(c, vendorsPath) {
			n++
		}
	}
	return n
}

func (r *vendorRig) stored(t *testing.T) []VendorInfo {
	t.Helper()
	b, _ := r.store.Load(context.Background())
	var out []VendorInfo
	if len(b) > 0 {
		if err := json.Unmarshal(b, &out); err != nil {
			t.Fatal(err)
		}
	}
	return out
}

func TestDclVendorInfoServiceParity(t *testing.T) {
	t.Parallel()
	t.Run("fetches and stores vendor information on initialization", func(t *testing.T) {
		t.Parallel()
		r := newVendorRig()
		r.mock.addResponse(vendorsPath, vendorsPage(vendorRecord(4447, "Acme"), vendorRecord(4448, "Globex")))
		s := r.open(t, nil)
		if v, ok := s.InfoFor(4447); !ok || v.VendorName != "Acme" {
			t.Fatalf("InfoFor(4447) = %+v, %v", v, ok)
		}
		if got := r.stored(t); len(got) != 2 {
			t.Fatalf("stored %d vendors, want 2", len(got))
		}
	})
	t.Run("loads vendor information from storage on subsequent initialization", func(t *testing.T) {
		t.Parallel()
		r := newVendorRig()
		r.mock.addResponse(vendorsPath, vendorsPage(vendorRecord(4447, "Acme")))
		r.open(t, nil).Close()
		s := r.open(t, func(o *VendorInfoOptions) { o.Offline = true })
		if v, ok := s.InfoFor(4447); !ok || v.VendorName != "Acme" {
			t.Fatalf("the stored vendor is gone: %+v, %v", v, ok)
		}
	})
	t.Run("returns undefined for unknown vendor ID", func(t *testing.T) {
		t.Parallel()
		r := newVendorRig()
		r.mock.addResponse(vendorsPath, vendorsPage(vendorRecord(4447, "Acme")))
		if _, ok := r.open(t, nil).InfoFor(9999); ok {
			t.Fatal("an unknown vendor resolved")
		}
	})
	t.Run("returns vendor map with all vendors", func(t *testing.T) {
		t.Parallel()
		r := newVendorRig()
		r.mock.addResponse(vendorsPath, vendorsPage(vendorRecord(4447, "Acme"), vendorRecord(4448, "Globex")))
		// The two ledger vendors and the five static ones, in id order.
		vs := r.open(t, nil).Vendors()
		if len(vs) != 7 || vs[0].VendorID != 4447 || vs[len(vs)-1].VendorID != 0xFFF4 {
			t.Fatalf("vendors %+v", vs)
		}
	})
	t.Run("normalizes DCL field names to camelCase", func(t *testing.T) {
		t.Parallel()
		// By meaning: a record without the optional preferred name and
		// landing page has them "" (#normalizeDclVendor, :394-403).
		r := newVendorRig()
		r.mock.addResponse(vendorsPath, vendorsPage(map[string]any{"vendorID": 4447, "vendorName": "Acme", "companyLegalName": "Acme Inc.", "creator": "c", "schemaVersion": 0}))
		v, _ := r.open(t, nil).InfoFor(4447)
		want := VendorInfo{VendorID: 4447, VendorName: "Acme", CompanyLegalName: "Acme Inc.", Creator: "c"}
		if v != want {
			t.Fatalf("%+v, want %+v", v, want)
		}
	})
	t.Run("includes test vendor (65521) if not in DCL", func(t *testing.T) {
		t.Parallel()
		r := newVendorRig()
		r.mock.addResponse(vendorsPath, vendorsPage())
		if v, ok := r.open(t, nil).InfoFor(0xFFF1); !ok || v.VendorName != "Test Vendor" || v.Creator != "System" {
			t.Fatalf("%+v, %v", v, ok)
		}
	})
	t.Run("includes Nabu Casa (4939) if not in DCL", func(t *testing.T) {
		t.Parallel()
		r := newVendorRig()
		r.mock.addResponse(vendorsPath, vendorsPage())
		if v, ok := r.open(t, nil).InfoFor(4939); !ok || v.VendorName != "Nabu Casa" {
			t.Fatalf("%+v, %v", v, ok)
		}
	})
	t.Run("does not duplicate hardcoded vendors if already in DCL", func(t *testing.T) {
		t.Parallel()
		r := newVendorRig()
		r.mock.addResponse(vendorsPath, vendorsPage(vendorRecord(4939, "NabuFromDCL")))
		s := r.open(t, nil)
		n := 0
		for _, v := range s.Vendors() {
			if v.VendorID == 4939 {
				n++
				if v.VendorName != "NabuFromDCL" {
					t.Fatalf("the static vendor won: %+v", v)
				}
			}
		}
		if n != 1 {
			t.Fatalf("%d entries of 4939", n)
		}
	})
	t.Run("fetches all pages when pagination is present", func(t *testing.T) {
		t.Parallel()
		r := newVendorRig()
		r.mock.addResponse(vendorsPath, map[string]any{"vendorInfo": []any{vendorRecord(4447, "Acme")}, "pagination": map[string]any{"next_key": "page2"}})
		r.mock.addResponse("pagination.key=page2", vendorsPage(vendorRecord(4448, "Globex")))
		s := r.open(t, nil)
		for _, id := range []uint16{4447, 4448} {
			if _, ok := s.InfoFor(id); !ok {
				t.Fatalf("vendor %d of the pages missing", id)
			}
		}
	})
	t.Run("handles single page response without pagination", func(t *testing.T) {
		t.Parallel()
		r := newVendorRig()
		r.mock.addResponse(vendorsPath, map[string]any{"vendorInfo": []any{vendorRecord(4447, "Acme")}})
		if _, ok := r.open(t, nil).InfoFor(4447); !ok || r.fetches() != 1 {
			t.Fatalf("ok %v, fetches %d", ok, r.fetches())
		}
	})
	t.Run("refetches vendors when update is called", func(t *testing.T) {
		t.Parallel()
		r := newVendorRig()
		r.mock.addResponse(vendorsPath, vendorsPage(vendorRecord(4447, "Acme")))
		s := r.open(t, nil)
		r.mock.addResponse(vendorsPath, vendorsPage(vendorRecord(4447, "Acme"), vendorRecord(4449, "Initech")))
		if err := s.Update(context.Background()); err != nil {
			t.Fatal(err)
		}
		if _, ok := s.InfoFor(4449); !ok || r.fetches() != 2 {
			t.Fatalf("ok %v, fetches %d", ok, r.fetches())
		}
	})
	t.Run("handles concurrent update requests gracefully", func(t *testing.T) {
		t.Parallel()
		r := newVendorRig()
		r.mock.addResponse(vendorsPath, vendorsPage(vendorRecord(4447, "Acme")))
		s := r.open(t, nil)
		gate := &gatedTransport{inner: r.mock, gate: make(chan struct{}), entered: make(chan struct{}, 4)}
		s.client.HTTPClient = &http.Client{Transport: gate}
		var wg sync.WaitGroup
		for range 3 {
			wg.Go(func() { _ = s.Update(context.Background()) })
		}
		<-gate.entered
		time.Sleep(20 * time.Millisecond)
		close(gate.gate)
		wg.Wait()
		if n := gate.calls.Load(); n != 1 {
			t.Fatalf("%d fetches for three concurrent updates, want 1", n)
		}
	})
	t.Run("handles DCL fetch failures gracefully", func(t *testing.T) {
		t.Parallel()
		r := newVendorRig()
		r.mock.addStatus(vendorsPath, map[string]any{"code": 13, "message": "internal", "details": []any{}}, http.StatusInternalServerError)
		s := r.open(t, nil)
		if _, ok := s.InfoFor(0xFFF1); !ok {
			t.Fatal("the static vendors are gone after a failed fetch")
		}
	})
	t.Run("handles empty vendor list", func(t *testing.T) {
		t.Parallel()
		r := newVendorRig()
		r.mock.addResponse(vendorsPath, vendorsPage())
		if n := len(r.open(t, nil).Vendors()); n != 5 {
			t.Fatalf("%d vendors, want the five static ones", n)
		}
	})
	t.Run("starts periodic update timer by default", func(t *testing.T) {
		t.Parallel()
		r := newVendorRig()
		r.mock.addResponse(vendorsPath, vendorsPage())
		r.open(t, nil)
		r.clock.Advance(DefaultVendorUpdateInterval)
		waitFor(t, func() bool { return r.fetches() == 2 })
	})
	t.Run("does not start timer when updateInterval is null", func(t *testing.T) {
		t.Parallel()
		r := newVendorRig()
		r.mock.addResponse(vendorsPath, vendorsPage())
		r.open(t, func(o *VendorInfoOptions) { o.UpdateInterval = -1 })
		if n := len(r.clock.Armed()); n != 0 {
			t.Fatalf("%d timers armed", n)
		}
	})
	t.Run("stops timer on close", func(t *testing.T) {
		t.Parallel()
		r := newVendorRig()
		r.mock.addResponse(vendorsPath, vendorsPage())
		s := r.open(t, nil)
		s.Close()
		r.clock.Advance(DefaultVendorUpdateInterval)
		time.Sleep(10 * time.Millisecond)
		if n := r.fetches(); n != 1 {
			t.Fatalf("%d fetches after close", n)
		}
	})
	t.Run("stops the timer and closes storage when the in-flight fetch fails during close()", func(t *testing.T) {
		t.Parallel()
		r := newVendorRig()
		r.mock.addResponse(vendorsPath, vendorsPage())
		s := r.open(t, nil)
		gate := &gatedTransport{inner: failingTransport{}, gate: make(chan struct{}), entered: make(chan struct{}, 1)}
		s.client.HTTPClient = &http.Client{Transport: gate}
		go func() { _ = s.Update(context.Background()) }()
		<-gate.entered
		closed := make(chan struct{})
		go func() { s.Close(); close(closed) }()
		select {
		case <-closed:
			t.Fatal("Close returned before the in-flight fetch settled")
		case <-time.After(20 * time.Millisecond):
		}
		close(gate.gate)
		<-closed
	})
	t.Run("resolves concurrent close() calls only after cleanup is done", func(t *testing.T) {
		t.Parallel()
		r := newVendorRig()
		r.mock.addResponse(vendorsPath, vendorsPage())
		s := r.open(t, nil)
		var wg sync.WaitGroup
		for range 2 {
			wg.Go(s.Close)
		}
		wg.Wait()
		if n := len(r.clock.Armed()); n != 0 {
			t.Fatalf("%d timers armed after close", n)
		}
	})
	t.Run("uses custom update interval", func(t *testing.T) {
		t.Parallel()
		r := newVendorRig()
		r.mock.addResponse(vendorsPath, vendorsPage())
		r.open(t, func(o *VendorInfoOptions) { o.UpdateInterval = time.Hour })
		r.clock.Advance(time.Hour)
		waitFor(t, func() bool { return r.fetches() == 2 })
	})
	t.Run("uses custom dclConfig URL when provided", func(t *testing.T) {
		t.Parallel()
		r := newVendorRig()
		r.mock.addResponse(vendorsPath, vendorsPage())
		r.open(t, nil)
		if calls := r.mock.callLog(); len(calls) == 0 || !strings.HasPrefix(calls[0], "https://dcl.test/") {
			t.Fatalf("calls %v", calls)
		}
	})
	t.Run("uses default production URL when dclConfig is not provided", func(t *testing.T) {
		t.Parallel()
		s, err := NewVendorInfoService(context.Background(), VendorInfoOptions{Offline: true})
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close()
		if got := s.client.baseURL(); got != ProductionURL {
			t.Fatalf("%q, want the production ledger", got)
		}
	})
}

func TestDclVendorInfoServiceProductInfoParity(t *testing.T) {
	t.Parallel()
	model := map[string]any{
		"vid": 4447, "pid": 1, "deviceTypeId": 10, "productName": "Lock", "productLabel": "Lock Pro", "partNumber": "L-1",
		"discoveryCapabilitiesBitmask": 6, "commissioningCustomFlow": 2, "commissioningCustomFlowUrl": "https://acme.example/flow",
		"commissioningModeInitialStepsHint": 1, "commissioningModeSecondaryStepsHint": 4, "userManualUrl": "https://acme.example/manual",
		"lsfUrl": "https://acme.example/lsf", "lsfRevision": 3, "enhancedSetupFlowOptions": 1,
		"enhancedSetupFlowTCUrl": "https://acme.example/tc", "enhancedSetupFlowTCRevision": 2, "maintenanceUrl": "https://acme.example/m",
		"schemaVersion": 0,
	}
	open := func(t *testing.T, offline bool) (*vendorRig, *VendorInfoService) {
		r := newVendorRig()
		r.mock.addResponse(vendorsPath, vendorsPage())
		r.mock.addResponse("/dcl/model/models/4447/1", map[string]any{"model": model})
		r.mock.addStatus("/dcl/model/models/4447/2", map[string]any{"code": CodeNotFound, "message": "not found", "details": []any{}}, http.StatusNotFound)
		r.mock.addStatus("/dcl/model/models/4447/3", map[string]any{"code": 13, "message": "internal", "details": []any{}}, http.StatusInternalServerError)
		return r, r.open(t, func(o *VendorInfoOptions) { o.Offline = offline })
	}
	t.Run("returns ProductInfo with decoded fields for a known VID/PID", func(t *testing.T) {
		t.Parallel()
		_, s := open(t, false)
		p, ok, err := s.ProductInfoFor(context.Background(), 4447, 1)
		if err != nil || !ok {
			t.Fatalf("%v, %v", ok, err)
		}
		if p.DeviceTypeID != 10 || p.ProductName != "Lock" || p.DiscoveryCapabilities != 6 || p.CommissioningFlow != 2 ||
			p.CommissioningModeSecondaryStepsHint != 4 || p.EnhancedSetupFlowMaintenanceURL != "https://acme.example/m" {
			t.Fatalf("%+v", p)
		}
	})
	t.Run("keeps revisions whose URL is set", func(t *testing.T) {
		t.Parallel()
		_, s := open(t, false)
		p, _, _ := s.ProductInfoFor(context.Background(), 4447, 1)
		if p.LSFRevision != 3 || p.EnhancedSetupFlowTCRevision != 2 {
			t.Fatalf("%+v", p)
		}
	})
	t.Run("returns undefined for an unknown VID/PID combination", func(t *testing.T) {
		t.Parallel()
		_, s := open(t, false)
		if _, ok, err := s.ProductInfoFor(context.Background(), 4447, 2); ok || err != nil {
			t.Fatalf("%v, %v", ok, err)
		}
	})
	t.Run("rethrows non-NotFound DCL errors", func(t *testing.T) {
		t.Parallel()
		_, s := open(t, false)
		var re *ResponseError
		if _, _, err := s.ProductInfoFor(context.Background(), 4447, 3); !errors.As(err, &re) {
			t.Fatalf("err %v, want the ledger's", err)
		}
	})
	t.Run("returns undefined when service is closed", func(t *testing.T) {
		t.Parallel()
		_, s := open(t, false)
		s.Close()
		if _, ok, err := s.ProductInfoFor(context.Background(), 4447, 1); ok || err != nil {
			t.Fatalf("%v, %v", ok, err)
		}
	})
	t.Run("answers a product lookup as absent without asking the DCL", func(t *testing.T) {
		t.Parallel()
		r, s := open(t, true)
		if _, ok, err := s.ProductInfoFor(context.Background(), 4447, 1); ok || err != nil || len(r.mock.callLog()) != 0 {
			t.Fatalf("%v, %v, calls %v", ok, err, r.mock.callLog())
		}
	})
}

func TestDclVendorInfoServiceSeedParity(t *testing.T) {
	t.Parallel()
	seed := &VendorSeed{BuiltAt: "2026-10-01", ExpectedCount: 3, Vendors: []VendorInfo{
		{VendorID: 4447, VendorName: "Acme"}, {VendorID: 4448, VendorName: "Globex"}, {VendorID: 4449, VendorName: "Initech"},
	}}
	t.Run("applies seed when storage is smaller than expectedCount", func(t *testing.T) {
		t.Parallel()
		r := newVendorRig()
		s := r.open(t, func(o *VendorInfoOptions) { o.Offline, o.Seed = true, seed })
		if _, ok := s.InfoFor(4449); !ok || len(r.stored(t)) != 3 {
			t.Fatalf("ok %v, stored %d", ok, len(r.stored(t)))
		}
	})
	t.Run("skips seed when storage already holds expectedCount vendors", func(t *testing.T) {
		t.Parallel()
		r := newVendorRig()
		held := []VendorInfo{{VendorID: 1, VendorName: "A"}, {VendorID: 2, VendorName: "B"}, {VendorID: 3, VendorName: "C"}}
		b, _ := json.Marshal(held)
		_ = r.store.Save(context.Background(), b)
		s := r.open(t, func(o *VendorInfoOptions) { o.Offline, o.Seed = true, seed })
		if _, ok := s.InfoFor(4447); ok {
			t.Fatal("the seed replaced a full store")
		}
		if _, ok := s.InfoFor(1); !ok {
			t.Fatal("the stored vendor is gone")
		}
	})
	t.Run("no-seed: existing behavior unchanged", func(t *testing.T) {
		t.Parallel()
		r := newVendorRig()
		r.mock.addResponse(vendorsPath, vendorsPage(vendorRecord(4447, "Acme")))
		if _, ok := r.open(t, nil).InfoFor(4447); !ok {
			t.Fatal("no vendor")
		}
	})
	t.Run("stores only DCL vendors, not the built-in test vendors", func(t *testing.T) {
		t.Parallel()
		r := newVendorRig()
		r.mock.addResponse(vendorsPath, vendorsPage(vendorRecord(4447, "Acme")))
		r.open(t, nil)
		for _, v := range r.stored(t) {
			if v.VendorID >= 0xFFF1 || v.VendorID == 4939 {
				t.Fatalf("a static vendor was stored: %+v", v)
			}
		}
	})
	t.Run("serves seeded vendors without asking the DCL and starts no update timer", func(t *testing.T) {
		t.Parallel()
		r := newVendorRig()
		s := r.open(t, func(o *VendorInfoOptions) { o.Offline, o.Seed = true, seed })
		if _, ok := s.InfoFor(4448); !ok || len(r.mock.callLog()) != 0 || len(r.clock.Armed()) != 0 {
			t.Fatalf("ok %v, calls %v, timers %d", ok, r.mock.callLog(), len(r.clock.Armed()))
		}
	})
	t.Run("serves the test vendors with fresh storage and no seed", func(t *testing.T) {
		t.Parallel()
		r := newVendorRig()
		if _, ok := r.open(t, func(o *VendorInfoOptions) { o.Offline = true }).InfoFor(0xFFF2); !ok {
			t.Fatal("no test vendor")
		}
	})
	t.Run("prefers a stored DCL record over the built-in test vendor", func(t *testing.T) {
		t.Parallel()
		r := newVendorRig()
		b, _ := json.Marshal([]VendorInfo{{VendorID: 0xFFF1, VendorName: "Stored"}})
		_ = r.store.Save(context.Background(), b)
		if v, _ := r.open(t, func(o *VendorInfoOptions) { o.Offline = true }).InfoFor(0xFFF1); v.VendorName != "Stored" {
			t.Fatalf("%+v", v)
		}
	})
	t.Run("serves vendors stored by an earlier run", func(t *testing.T) {
		t.Parallel()
		r := newVendorRig()
		r.mock.addResponse(vendorsPath, vendorsPage(vendorRecord(4447, "Acme")))
		r.open(t, nil).Close()
		if _, ok := r.open(t, func(o *VendorInfoOptions) { o.Offline = true }).InfoFor(4447); !ok {
			t.Fatal("the earlier run's vendor is gone")
		}
	})
	t.Run("observers learn every refresh", func(t *testing.T) {
		t.Parallel()
		// Go-only: the Observe hook (matter.js's service has none).
		r := newVendorRig()
		r.mock.addResponse(vendorsPath, vendorsPage(vendorRecord(4447, "Acme")))
		s := r.open(t, func(o *VendorInfoOptions) { o.UpdateInterval = -1 })
		var got []VendorUpdate
		var mu sync.Mutex
		stop := s.Observe(func(u VendorUpdate) { mu.Lock(); got = append(got, u); mu.Unlock() })
		_ = s.Update(context.Background())
		stop()
		_ = s.Update(context.Background())
		mu.Lock()
		defer mu.Unlock()
		if len(got) != 1 || got[0].Vendors != 1 || got[0].Err != nil {
			t.Fatalf("%+v", got)
		}
	})
}

// gatedTransport holds every request until gate closes and counts them.
type gatedTransport struct {
	inner   http.RoundTripper
	gate    chan struct{}
	entered chan struct{}
	calls   atomic.Int32
}

func (g *gatedTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	g.calls.Add(1)
	select {
	case g.entered <- struct{}{}:
	default:
	}
	<-g.gate
	return g.inner.RoundTrip(r)
}

type failingTransport struct{}

func (failingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("network down")
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition not met")
		}
		time.Sleep(time.Millisecond)
	}
}

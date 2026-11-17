// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package dcl

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// TestVendorInfoCloseCancelsThePeriodicRefresh (review II,
// R2-P6-conc-1906): a periodic refresh against a ledger that keeps
// paginating, a page at a time, is cancelled by Close, which returns
// after the page in flight rather than after every page the ledger would
// still answer.
func TestVendorInfoCloseCancelsThePeriodicRefresh(t *testing.T) {
	t.Parallel()
	var pages atomic.Int32
	var periodic atomic.Bool
	started := make(chan struct{}, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := pages.Add(1)
		if periodic.Load() {
			select {
			case started <- struct{}{}:
			default:
			}
		}
		select {
		case <-time.After(50 * time.Millisecond):
		case <-r.Context().Done():
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if !periodic.Load() {
			_, _ = w.Write([]byte(`{"vendorInfo":[]}`)) // the constructor's refresh ends
			return
		}
		_, _ = fmt.Fprintf(w, `{"vendorInfo":[],"pagination":{"next_key":"k%d"}}`, n)
	}))
	t.Cleanup(srv.Close)
	clk := newManualClock()
	s, err := NewVendorInfoService(context.Background(), VendorInfoOptions{
		Client: &Client{BaseURL: srv.URL, HTTPClient: srv.Client()}, Clock: clk, UpdateInterval: time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	periodic.Store(true)
	clk.advance(time.Hour)
	<-started
	closed := make(chan struct{})
	go func() { s.Close(); close(closed) }()
	select {
	case <-closed:
	case <-time.After(10 * time.Second):
		t.Fatalf("Close still waits after %d pages", pages.Load())
	}
}

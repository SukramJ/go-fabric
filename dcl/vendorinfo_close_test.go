// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package dcl

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// TestVendorInfoCloseCancelsThePeriodicRefresh (review II,
// R2-P6-conc-1906): a periodic refresh whose page the ledger holds open (a
// request timeout of an hour) is cancelled by Close, which returns with
// the request rather than after it.
func TestVendorInfoCloseCancelsThePeriodicRefresh(t *testing.T) {
	t.Parallel()
	var periodic atomic.Bool
	started := make(chan struct{}, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !periodic.Load() {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"vendorInfo":[]}`)) // the constructor's refresh ends
			return
		}
		select {
		case started <- struct{}{}:
		default:
		}
		<-r.Context().Done() // the page is held open until the request ends
	}))
	t.Cleanup(srv.Close)
	t.Cleanup(srv.CloseClientConnections) // runs first: ends a page still held open
	clk := newManualClock()
	s, err := NewVendorInfoService(context.Background(), VendorInfoOptions{
		Client: &Client{BaseURL: srv.URL, HTTPClient: srv.Client(), Timeout: time.Hour}, Clock: clk, UpdateInterval: time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	periodic.Store(true)
	clk.advance(time.Hour)
	<-started
	closed := make(chan struct{})
	go func() { s.Close(); close(closed) }()
	watchdog, stop := context.WithTimeout(context.Background(), 10*time.Second)
	defer stop()
	select {
	case <-closed:
	case <-watchdog.Done():
		t.Fatal("Close still waits for the periodic refresh's request")
	}
}

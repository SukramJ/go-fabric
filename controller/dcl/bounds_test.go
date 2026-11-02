// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package dcl

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// TestResponseBounds (W4-ctlint-10): an answer over maxResponseBody is an
// error, not a buffer of whatever the endpoint sends, and a listing whose
// next_key never ends stops after maxPages pages. matter.js bounds
// neither (DclClient.ts:62-121); both bounds sit far above the production
// ledger's answers.
func TestResponseBounds(t *testing.T) {
	t.Parallel()
	var pages atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasPrefix(r.URL.Path, "/dcl/vendorinfo/vendors"):
			n := pages.Add(1)
			_, _ = fmt.Fprintf(w, `{"vendorInfo":[],"pagination":{"next_key":"k%d"}}`, n)
		default:
			_, _ = w.Write([]byte(`{"model":"`))
			_, _ = w.Write([]byte(strings.Repeat("x", maxResponseBody)))
			_, _ = w.Write([]byte(`"}`))
		}
	}))
	t.Cleanup(srv.Close)
	c := &Client{BaseURL: srv.URL, HTTPClient: srv.Client()}
	if _, err := c.Vendors(context.Background()); err == nil || pages.Load() != maxPages {
		t.Fatalf("endless pagination: %v after %d pages, want an error after %d", err, pages.Load(), maxPages)
	}
	if _, err := c.Model(context.Background(), 0xFFF1, 0x8000); err == nil {
		t.Fatal("an answer over maxResponseBody was read")
	}
}

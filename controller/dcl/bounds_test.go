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

// TestListingBound: the items of one paginated listing are bounded in
// total, not only per answer — a listing whose pages keep coming stops
// once its items pass maxListingBytes (review II, R2-P2-alloc-08).
func TestListingBound(t *testing.T) {
	t.Parallel()
	const pageItems = 96 << 10
	var pages atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		n := pages.Add(1)
		_, _ = fmt.Fprintf(w, `{"vendorInfo":[{"vendorName":"%s"}],"pagination":{"next_key":"k%d"}}`, strings.Repeat("x", pageItems), n)
	}))
	t.Cleanup(srv.Close)
	c := &Client{BaseURL: srv.URL, HTTPClient: srv.Client()}
	_, err := c.Vendors(t.Context())
	if want := int32(maxListingBytes/pageItems + 1); err == nil || pages.Load() > want {
		t.Fatalf("a listing of %d-byte pages: %v after %d pages, want an error after at most %d", pageItems, err, pages.Load(), want)
	}
}

// TestListingItemBound: the items of one paginated listing are bounded by
// their count, which bounds what they decode to — an item costs three
// bytes on the wire ("{},") and a whole Vendor once decoded. A page with
// more items than maxListingItems is refused before they are all
// decoded, and a listing whose pages keep coming stops once its items pass
// it (review II, R2-P2-dr-ctl-03).
func TestListingItemBound(t *testing.T) {
	t.Parallel()
	listing := func(t *testing.T, pageItems int) (*Client, *atomic.Int32) {
		t.Helper()
		var pages atomic.Int32
		items := strings.TrimSuffix(strings.Repeat("{},", pageItems), ",")
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			n := pages.Add(1)
			_, _ = fmt.Fprintf(w, `{"vendorInfo":[%s],"pagination":{"next_key":"k%d"}}`, items, n)
		}))
		t.Cleanup(srv.Close)
		return &Client{BaseURL: srv.URL, HTTPClient: srv.Client()}, &pages
	}
	t.Run("one page", func(t *testing.T) {
		t.Parallel()
		c, pages := listing(t, maxListingItems+1)
		if got, err := c.Vendors(t.Context()); err == nil || pages.Load() != 1 {
			t.Fatalf("a page of %d items: %d vendors, %v after %d pages, want an error after one", maxListingItems+1, len(got), err, pages.Load())
		}
	})
	t.Run("many pages", func(t *testing.T) {
		t.Parallel()
		const pageItems = 4 << 10
		c, pages := listing(t, pageItems)
		_, err := c.Vendors(t.Context())
		if want := int32(maxListingItems/pageItems + 1); err == nil || pages.Load() > want {
			t.Fatalf("a listing of %d-item pages: %v after %d pages, want an error after at most %d", pageItems, err, pages.Load(), want)
		}
	})
}

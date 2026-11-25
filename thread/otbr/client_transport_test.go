// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package otbr

import (
	"net/http"
	"testing"
)

// TestDefaultClientOwnsItsTransport: the default client does not share
// http.DefaultTransport, whose CloseIdleConnections — called by every
// httptest.Server.Close and by any host code — breaks a non-replayable
// PUT that picked up an idle connection ("http: CloseIdleConnections
// called"; the CI race flake of TestBorderRouterReplaceAndClear).
func TestDefaultClientOwnsItsTransport(t *testing.T) {
	t.Parallel()
	c, err := NewClient(Config{Host: "127.0.0.1", Port: 8081})
	if err != nil {
		t.Fatal(err)
	}
	tr, ok := c.http.Transport.(*http.Transport)
	if !ok || tr == nil || tr == http.DefaultTransport {
		t.Fatalf("default transport %T %p, want an own *http.Transport", c.http.Transport, c.http.Transport)
	}
	if c.http.CheckRedirect == nil {
		t.Fatal("the default client follows redirects")
	}
}

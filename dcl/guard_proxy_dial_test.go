// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package dcl

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"testing"
)

// TestGuardDropsTheHostsDots (review II, 5b-dcl R7): a host is judged
// without its leading and trailing dots, so a dotted spelling of
// localhost or of a refused literal is refused as the plain one is.
func TestGuardDropsTheHostsDots(t *testing.T) {
	t.Parallel()
	g := &destinationGuard{}
	for _, host := range []string{".localhost", "..localhost", "localhost..", ".LOCALHOST.", ".127.0.0.1", "10.0.0.1.."} {
		if err := g.refusedHostPort(host, "443"); !errors.Is(err, errDestination) {
			t.Errorf("%q: %v, want errDestination", host, err)
		}
	}
	for _, host := range []string{"crl.example", ".crl.example", "foo.localhost"} {
		if err := g.refusedHostPort(host, "443"); err != nil {
			t.Errorf("%q: %v, want a name", host, err)
		}
	}
}

// TestHeld5bDclF6Retry (review II, 5b-dcl F6).
func TestHeld5bDclF6Retry(t *testing.T) {
	t.Parallel()
	answers := []*url.URL{{Scheme: "http", Host: "proxy.example:3128"}, nil}
	g := &destinationGuard{proxy: func(*http.Request) (*url.URL, error) {
		p := answers[0]
		answers = answers[1:]
		return p, nil
	}}
	d := &proxyDial{}
	req, err := http.NewRequestWithContext(context.WithValue(t.Context(), proxyDialKey{}, d), http.MethodGet, "http://crl.example/x", http.NoBody)
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err := g.proxyFor(req); err != nil {
			t.Fatal(err)
		}
	}
	if a := d.addr.Load(); a != nil {
		t.Fatalf("recorded %q after a nil answer, want nothing", *a)
	}
}

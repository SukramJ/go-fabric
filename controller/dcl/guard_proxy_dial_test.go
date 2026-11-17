// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package dcl

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"sync/atomic"
	"testing"
)

// TestHeld5bDclF6 (review II, 5b-dcl F6).
func TestHeld5bDclF6(t *testing.T) {
	t.Parallel()
	var hits atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		_, _ = w.Write([]byte("crl"))
	}))
	t.Cleanup(proxy.Close)
	_, port, err := net.SplitHostPort(proxy.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	proxyURL := &url.URL{Scheme: "http", Host: net.JoinHostPort("proxy.example", port)}
	s := &CertificateStore{opts: CertificateOptions{Client: &Client{guard: newGuardedClient(guardConfig{
		resolver: stubResolver(map[string][]netip.Addr{"proxy.example": {netip.MustParseAddr("127.0.0.1")}}),
		proxy: func(r *http.Request) (*url.URL, error) {
			if r.URL.Hostname() == "proxy.example" {
				return nil, nil
			}
			return proxyURL, nil
		},
	})}}}
	if body, err := s.downloadCRL(t.Context(), "http://crl.example/x.crl", 0); err != nil || string(body) != "crl" {
		t.Fatalf("through the proxy: %q, %v", body, err)
	}
	direct := "http://" + net.JoinHostPort("proxy.example", port) + "/x.crl"
	if _, err := s.downloadCRL(t.Context(), direct, 0); !errors.Is(err, errDestination) {
		t.Fatalf("second request: %v, want errDestination", err)
	}
	if n := hits.Load(); n != 1 {
		t.Fatalf("the server answered %d requests, want 1", n)
	}
}

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

// TestHeld5bDclR8 (review II, 5b-dcl R8, decision 218).
func TestHeld5bDclR8(t *testing.T) {
	t.Parallel()
	var asked atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		asked.Add(1)
		_, _ = w.Write([]byte("crl"))
	}))
	t.Cleanup(proxy.Close)
	proxyURL, err := url.Parse(proxy.URL)
	if err != nil {
		t.Fatal(err)
	}
	for _, scheme := range []string{"http", "socks5"} {
		p := *proxyURL
		p.Scheme = scheme
		s := &CertificateStore{opts: CertificateOptions{Client: &Client{guard: newGuardedClient(guardConfig{
			resolver: stubResolver(nil),
			proxy:    func(*http.Request) (*url.URL, error) { return &p, nil },
		})}}}
		for _, u := range []string{
			"http://１２７.０.０.１/x", "http://ｌｏｃａｌｈｏｓｔ。/x", "http://bücher.example/x",
			"https://bücher.example/x", "http://127.0.0.1\u200b/x",
		} {
			if _, err := s.downloadCRL(t.Context(), u, 0); !errors.Is(err, errDestination) {
				t.Errorf("%s through a %s proxy: %v, want errDestination", u, scheme, err)
			}
		}
	}
	if n := asked.Load(); n != 0 {
		t.Fatalf("the proxy was asked %d times, want never", n)
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

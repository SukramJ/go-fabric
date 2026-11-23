// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package dcl

import (
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"sync/atomic"
	"testing"

	"thingfabric.dev/go-fabric/internal/dclseam"
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
	s := &CertificateStore{opts: CertificateOptions{Client: guardedClient(t, dclseam.GuardConfig{
		Resolver: stubResolver(map[string][]netip.Addr{"proxy.example": {netip.MustParseAddr("127.0.0.1")}}),
		Proxy: func(r *http.Request) (*url.URL, error) {
			if r.URL.Hostname() == "proxy.example" {
				return nil, nil
			}
			return proxyURL, nil
		},
	})}}
	if body, err := s.downloadCRL(t.Context(), "http://crl.example/x.crl", 0); err != nil || string(body) != "crl" {
		t.Fatalf("through the proxy: %q, %v", body, err)
	}
	direct := "http://" + net.JoinHostPort("proxy.example", port) + "/x.crl"
	if _, err := s.downloadCRL(t.Context(), direct, 0); !dclseam.IsDestinationRefused(err) {
		t.Fatalf("second request: %v, want the guard's refusal", err)
	}
	if n := hits.Load(); n != 1 {
		t.Fatalf("the server answered %d requests, want 1", n)
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
		s := &CertificateStore{opts: CertificateOptions{Client: guardedClient(t, dclseam.GuardConfig{
			Resolver: stubResolver(nil),
			Proxy:    func(*http.Request) (*url.URL, error) { return &p, nil },
		})}}
		for _, u := range []string{
			"http://１２７.０.０.１/x", "http://ｌｏｃａｌｈｏｓｔ。/x", "http://bücher.example/x",
			"https://bücher.example/x", "http://127.0.0.1\u200b/x",
		} {
			if _, err := s.downloadCRL(t.Context(), u, 0); !dclseam.IsDestinationRefused(err) {
				t.Errorf("%s through a %s proxy: %v, want the guard's refusal", u, scheme, err)
			}
		}
	}
	if n := asked.Load(); n != 0 {
		t.Fatalf("the proxy was asked %d times, want never", n)
	}
}

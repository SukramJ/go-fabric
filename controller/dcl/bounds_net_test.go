// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package dcl

// The ledger-named hosts get bounded requests: a revocation lookup opens
// a bounded number of CRL downloads at once, a download does not follow
// a redirect from https to http, and every GitHub request carries a
// deadline (review II, R2-P3-net-05, R2-P3-net-06, R2-P3-net-07).

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// TestRevocationLookupBoundsItsConcurrentDownloads: however many CRL
// distribution points the ledger lists for an issuer, one lookup holds
// at most maxConcurrentCRLDownloads downloads open at once.
func TestRevocationLookupBoundsItsConcurrentDownloads(t *testing.T) {
	t.Parallel()
	const points = 64
	x := newRevocationFixture(t)
	crl := signCRL(t, x.pki.pai, crlSpec{revoked: []*big.Int{big.NewInt(7)}})

	var inflight, peak atomic.Int32
	release := make(chan struct{})
	var once sync.Once
	open := func() { once.Do(func() { close(release) }) }
	t.Cleanup(open) // before the server's Close, which waits for the handlers
	list := make([]map[string]any, 0, points)
	for i := range points {
		path := fmt.Sprintf("/crl/%d", i)
		x.f.route(path, func(w http.ResponseWriter, r *http.Request) {
			n := inflight.Add(1)
			defer inflight.Add(-1)
			for {
				p := peak.Load()
				if n <= p || peak.CompareAndSwap(p, n) {
					break
				}
			}
			if n > maxConcurrentCRLDownloads {
				open() // the bound is broken: let every download end
			}
			// The host holds each download until the bound is broken or
			// the request's own timeout (revocationTimeout) ends the
			// first ones, which lets the rest through.
			select {
			case <-release:
			case <-r.Context().Done():
				open()
			}
			w.WriteHeader(http.StatusNotFound)
		})
		list = append(list, revocationPoint(x.pki.pai, x.pki.pai, nil, false, 0xFFF1, 0x8000, x.f.url(path), crl))
	}
	x.f.publishPoints(x.pki.pai, list...)

	x.dacRevoked(t)

	if p := peak.Load(); p > maxConcurrentCRLDownloads {
		t.Fatalf("one revocation lookup held %d CRL downloads open at the same time (the ledger listed %d points for the issuer), want at most %d", p, points, maxConcurrentCRLDownloads)
	}
}

// TestOTADownloadRefusesARedirectFromHTTPSToHTTP: an image named by an
// https URL is not taken from a plain-http redirect target — with the
// module's default client and with a host's client that has no redirect
// policy of its own.
func TestOTADownloadRefusesARedirectFromHTTPSToHTTP(t *testing.T) {
	t.Parallel()
	img := bytes.Repeat([]byte{0xA5}, 512)
	var plainHits atomic.Int32
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		plainHits.Add(1)
		_, _ = w.Write(img)
	}))
	t.Cleanup(plain.Close)
	secure := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, plain.URL+"/image.ota", http.StatusFound)
	}))
	t.Cleanup(secure.Close)

	var buf bytes.Buffer
	o := &OTAVersions{Client: &Client{HTTPClient: secure.Client()}}
	err := o.Download(t.Context(), SoftwareVersion{OTAURL: secure.URL + "/image.ota"}, &buf)
	if plainHits.Load() != 0 || err == nil {
		t.Fatalf("the download of %s followed the redirect to %s: %d request(s) over plain http, %d bytes taken from it, err = %v; want the redirect refused",
			secure.URL+"/image.ota", plain.URL, plainHits.Load(), buf.Len(), err)
	}
	// An https target stays allowed.
	same := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/moved.ota" {
			_, _ = w.Write(img)
			return
		}
		http.Redirect(w, r, "/moved.ota", http.StatusFound)
	}))
	t.Cleanup(same.Close)
	buf.Reset()
	o = &OTAVersions{Client: &Client{HTTPClient: same.Client()}}
	if err := o.Download(t.Context(), SoftwareVersion{OTAURL: same.URL + "/image.ota"}, &buf); err != nil || buf.Len() != len(img) {
		t.Fatalf("an https redirect to https: %d bytes, err %v", buf.Len(), err)
	}
}

// TestCRLDownloadRefusesARedirectFromHTTPSToHTTP: the CRL download keeps
// the scheme of the URL the ledger named.
func TestCRLDownloadRefusesARedirectFromHTTPSToHTTP(t *testing.T) {
	t.Parallel()
	var plainHits atomic.Int32
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		plainHits.Add(1)
		_, _ = w.Write([]byte("crl"))
	}))
	t.Cleanup(plain.Close)
	secure := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, plain.URL+"/x.crl", http.StatusFound)
	}))
	t.Cleanup(secure.Close)
	s := &CertificateStore{opts: CertificateOptions{Client: &Client{HTTPClient: secure.Client()}}}
	if body, err := s.downloadCRL(t.Context(), secure.URL+"/x.crl", 0); err == nil || plainHits.Load() != 0 {
		t.Fatalf("the CRL download followed a redirect to plain http: %q, err %v, %d plain request(s)", body, err, plainHits.Load())
	}
}

// deadlineProbe answers every request, failing the GitHub API's, and
// records the ones whose context carries no deadline.
type deadlineProbe struct {
	mu       sync.Mutex
	unbound  []string
	gitHub   int
	gitHubAt string
}

func (p *deadlineProbe) RoundTrip(req *http.Request) (*http.Response, error) {
	_, bounded := req.Context().Deadline()
	p.mu.Lock()
	if !bounded {
		p.unbound = append(p.unbound, req.URL.String())
	}
	isGitHub := req.URL.Host == p.gitHubAt
	if isGitHub {
		p.gitHub++
	}
	p.mu.Unlock()
	if isGitHub {
		return nil, errors.New("no answer")
	}
	body := `{"approvedRootCertificates":{"certs":[],"schemaVersion":0}}`
	return &http.Response{StatusCode: http.StatusOK, Status: "200 OK", Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
}

// TestGitHubRequestsCarryADeadline: with GitHubConfig.Timeout unset,
// every request of the first update — the ledger's and GitHub's — has a
// deadline, so a server that never answers cannot hold
// OpenCertificateStore.
func TestGitHubRequestsCarryADeadline(t *testing.T) {
	t.Parallel()
	probe := &deadlineProbe{gitHubAt: "api.github.test"}
	s, err := OpenCertificateStore(context.Background(), CertificateOptions{
		Client:                  &Client{BaseURL: "https://ledger.test", HTTPClient: &http.Client{Transport: probe}},
		FetchTestCertificates:   true,
		FetchGitHubCertificates: true,
		GitHub:                  GitHubConfig{APIURL: "https://api.github.test"},
		UpdateInterval:          -1,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	probe.mu.Lock()
	defer probe.mu.Unlock()
	if probe.gitHub == 0 {
		t.Fatal("the update sent no request to the GitHub API: the test does not reach the path")
	}
	if len(probe.unbound) != 0 {
		t.Fatalf("%d request(s) of the first update carry no deadline, so a server that never answers holds OpenCertificateStore: %v", len(probe.unbound), probe.unbound)
	}
}

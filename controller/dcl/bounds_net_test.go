// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package dcl

// The ledger-named hosts get bounded requests: a revocation lookup opens
// a bounded number of CRL downloads at once, a download does not follow
// a redirect from https to http, and every GitHub request carries a
// deadline (review II, R2-P3-net-05, R2-P3-net-06, R2-P3-net-07). A
// download of a URL a ledger record names connects to public addresses
// only unless the host supplies its client or admits private ones
// (Client.AllowPrivateDestinations).

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// TestRevocationLookupBoundsItsConcurrentDownloads: however many CRL
// distribution points the ledger lists for an issuer, one lookup holds
// at most maxConcurrentCRLDownloads downloads open at once.
//
// The downloads are counted where the lookup holds them, at the client:
// from the request until its response body is closed or the request
// failed. A request that failed on its own deadline has closed its
// connection when it returns (net/http persistConn.roundTrip closes it
// in cancelRequest before it returns), but the host's handler learns of
// the close only when its connection's background read does, after the
// next download may have arrived — a count at the host's handlers saw
// five (review II, R2-P5-sup-17).
func TestRevocationLookupBoundsItsConcurrentDownloads(t *testing.T) {
	t.Parallel()
	const points = 64
	release := make(chan struct{})
	var once sync.Once
	open := func() { once.Do(func() { close(release) }) }
	t.Cleanup(open) // before the server's Close, which waits for the handlers
	downloads := &countedDownloads{limit: maxConcurrentCRLDownloads, broken: open}
	x := newRevocationFixtureWith(t, downloads.wrap)
	crl := signCRL(t, x.pki.pai, crlSpec{revoked: []*big.Int{big.NewInt(7)}})

	list := make([]map[string]any, 0, points)
	for i := range points {
		path := fmt.Sprintf("/crl/%d", i)
		x.f.route(path, func(w http.ResponseWriter, r *http.Request) {
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

	if p := downloads.peak.Load(); p > maxConcurrentCRLDownloads {
		t.Fatalf("one revocation lookup held %d CRL downloads open at the same time (the ledger listed %d points for the issuer), want at most %d", p, points, maxConcurrentCRLDownloads)
	} else if p < maxConcurrentCRLDownloads {
		t.Fatalf("one revocation lookup held at most %d CRL downloads open at the same time, want the bound %d reached: the count saw no concurrency", p, maxConcurrentCRLDownloads)
	}
}

// countedDownloads counts the CRL downloads in flight through the
// client's transport — from RoundTrip until the response body is closed,
// or RoundTrip's error — and their peak; broken runs once the count
// passes limit.
type countedDownloads struct {
	limit          int32
	broken         func()
	inflight, peak atomic.Int32
}

func (d *countedDownloads) wrap(rt http.RoundTripper) http.RoundTripper {
	return roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if !strings.HasPrefix(r.URL.Path, "/crl/") {
			return rt.RoundTrip(r)
		}
		n := d.inflight.Add(1)
		for p := d.peak.Load(); n > p && !d.peak.CompareAndSwap(p, n); p = d.peak.Load() {
		}
		if n > d.limit {
			d.broken() // let every download end
		}
		var done sync.Once
		end := func() { done.Do(func() { d.inflight.Add(-1) }) }
		resp, err := rt.RoundTrip(r)
		if err != nil {
			end()
			return nil, err
		}
		resp.Body = &endOnClose{ReadCloser: resp.Body, end: end}
		return resp, nil
	})
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// endOnClose runs end when the body is closed.
type endOnClose struct {
	io.ReadCloser
	end func()
}

func (b *endOnClose) Close() error {
	err := b.ReadCloser.Close()
	b.end()
	return err
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

// TestPublicDestination: the guard's address classes — loopback, private,
// link-local, unspecified and multicast refused, in IPv4, IPv6 and
// IPv4-mapped IPv6 form; a public address admitted.
func TestPublicDestination(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		addr   string
		public bool
	}{
		{"127.0.0.1", false},
		{"127.1.2.3", false},
		{"::1", false},
		{"10.1.2.3", false},
		{"172.16.0.1", false},
		{"192.168.1.1", false},
		{"fc00::1", false},
		{"fd12:3456::1", false},
		{"169.254.169.254", false},
		{"fe80::1", false},
		{"fe80::1%lo0", false},
		{"0.0.0.0", false},
		{"::", false},
		{"224.0.0.251", false},
		{"239.1.2.3", false},
		{"ff02::fb", false},
		{"ff01::1", false},
		{"ff0e::1", false},
		{"::ffff:127.0.0.1", false},
		{"::ffff:10.0.0.1", false},
		{"::ffff:169.254.0.1", false},
		{"::ffff:0.0.0.0", false},
		{"8.8.8.8", true},
		{"1.1.1.1", true},
		{"2001:4860:4860::8888", true},
		{"::ffff:8.8.8.8", true},
		{"172.32.0.1", true},
	} {
		if got := publicDestination(netip.MustParseAddr(c.addr)); got != c.public {
			t.Errorf("publicDestination(%s) = %v, want %v", c.addr, got, c.public)
		}
	}
	if publicDestination(netip.Addr{}) {
		t.Error("the zero Addr is public")
	}
}

// stubResolver resolves each name of names to its addresses, and no other
// name, without leaving the process: a DNS server over an in-memory
// stream, which net's resolver frames as it frames TCP (a conn that is
// not a PacketConn, net/dnsclient_unix.go:194-198 at go1.27.4).
func stubResolver(names map[string][]netip.Addr) *net.Resolver {
	return &net.Resolver{PreferGo: true, Dial: func(context.Context, string, string) (net.Conn, error) {
		c, srv := net.Pipe()
		go serveDNS(srv, names)
		return c, nil
	}}
}

// serveDNS answers the A and AAAA queries on c from names: the question
// echoed, one answer per address of the asked family, NXDOMAIN for a name
// names lacks.
func serveDNS(c net.Conn, names map[string][]netip.Addr) {
	defer func() { _ = c.Close() }()
	for {
		var l [2]byte
		if _, err := io.ReadFull(c, l[:]); err != nil {
			return
		}
		q := make([]byte, binary.BigEndian.Uint16(l[:]))
		if _, err := io.ReadFull(c, q); err != nil || len(q) < 12 {
			return
		}
		end, labels := 12, []string{}
		for end < len(q) && q[end] != 0 {
			n := int(q[end])
			if end+1+n > len(q) {
				return
			}
			labels = append(labels, string(q[end+1:end+1+n]))
			end += 1 + n
		}
		end++
		if end+4 > len(q) {
			return
		}
		qtype := binary.BigEndian.Uint16(q[end : end+2])
		addrs, known := names[strings.ToLower(strings.Join(labels, "."))]
		var answers []byte
		count := 0
		for _, a := range addrs {
			if (qtype != 1 || !a.Is4()) && (qtype != 28 || !a.Is6()) {
				continue
			}
			ip := a.AsSlice()
			answers = append(answers, 0xC0, 12) // the question's name
			answers = binary.BigEndian.AppendUint16(answers, qtype)
			answers = append(answers, 0, 1, 0, 0, 0, 60)                      // class IN, TTL 60
			answers = binary.BigEndian.AppendUint16(answers, uint16(len(ip))) //nolint:gosec // 4 or 16
			answers = append(answers, ip...)
			count++
		}
		rcode := byte(0)
		if !known {
			rcode = 3
		}
		resp := append([]byte{q[0], q[1], 0x81, 0x80 | rcode, 0, 1, 0, byte(count), 0, 0, 0, 0}, q[12:end+4]...)
		resp = append(resp, answers...)
		out := binary.BigEndian.AppendUint16(nil, uint16(len(resp))) //nolint:gosec // a short answer
		if _, err := c.Write(append(out, resp...)); err != nil {
			return
		}
	}
}

// connCountingTLSServer is a TLS server that serves body and counts the
// connections it accepted, whether or not a request followed.
func connCountingTLSServer(t *testing.T, body []byte) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var conns atomic.Int32
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(body) }))
	srv.Config.ConnState = func(_ net.Conn, s http.ConnState) {
		if s == http.StateNew {
			conns.Add(1)
		}
	}
	srv.Config.ErrorLog = discardLog()
	srv.StartTLS()
	t.Cleanup(srv.Close)
	return srv, &conns
}

// trusting is the TLS configuration that trusts srv's certificate.
func trusting(srv *httptest.Server) *tls.Config {
	pool := x509.NewCertPool()
	pool.AddCert(srv.Certificate())
	return &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}
}

// TestOTADownloadConnectsToPublicDestinationsOnly: without a host client
// the download of a ledger record's image refuses, when it dials, a
// loopback URL, a name that resolves to a private or loopback address,
// and a redirect hop to a private, link-local or loopback destination —
// before a connection is made. AllowPrivateDestinations admits them; a
// host's HTTPClient is the host's policy and is not guarded.
func TestOTADownloadConnectsToPublicDestinationsOnly(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	img := bytes.Repeat([]byte{0x5A}, 512)
	srv, conns := connCountingTLSServer(t, img)
	record := SoftwareVersion{OTAURL: srv.URL + "/image.ota"}

	// The URL itself, with the module's client: no Client, and a Client
	// without an HTTPClient.
	for _, o := range []*OTAVersions{{}, {Client: &Client{}}} {
		var buf bytes.Buffer
		if err := o.Download(ctx, record, &buf); !errors.Is(err, errDestination) || buf.Len() != 0 {
			t.Fatalf("download of %s: %d bytes, err %v; want the destination refused", record.OTAURL, buf.Len(), err)
		}
	}
	if n := conns.Load(); n != 0 {
		t.Fatalf("%d connection(s) reached the loopback server; want the dial refused", n)
	}

	// A name that resolves to a refused address: the check is on the
	// address dialled, not on the name.
	resolver := stubResolver(map[string][]netip.Addr{
		"private.example":   {netip.MustParseAddr("10.1.2.3")},
		"linklocal.example": {netip.MustParseAddr("169.254.169.254")},
		"loopback.example":  {netip.MustParseAddr("127.0.0.1"), netip.MustParseAddr("::1")},
		"mapped.example":    {netip.MustParseAddr("::ffff:192.168.0.1")},
	})
	named := &OTAVersions{Client: &Client{guard: newGuardedClient(guardConfig{resolver: resolver})}}
	for _, name := range []string{"private.example", "linklocal.example", "loopback.example", "mapped.example"} {
		if err := named.Download(ctx, SoftwareVersion{OTAURL: "https://" + name + "/image.ota"}, io.Discard); !errors.Is(err, errDestination) {
			t.Fatalf("download from %s: err %v; want the destination refused", name, err)
		}
	}

	// A redirect hop: the first server is admitted (by the test seam),
	// each hop it names is dialled through the guard.
	target, targetConns := connCountingTLSServer(t, img)
	redirecting := httptest.NewUnstartedServer(nil)
	self := "https://" + redirecting.Listener.Addr().String()
	hop := map[string]string{
		"/private":   "https://private.example/image.ota",
		"/linklocal": "https://linklocal.example/image.ota",
		"/loopback":  target.URL + "/image.ota",
		"/self":      self + "/image.ota",
	}
	redirecting.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if to, ok := hop[r.URL.Path]; ok {
			http.Redirect(w, r, to, http.StatusFound)
			return
		}
		_, _ = w.Write(img)
	})
	redirecting.StartTLS()
	t.Cleanup(redirecting.Close)
	first := redirecting.Listener.Addr().(*net.TCPAddr).AddrPort()
	admitFirst := func(ap netip.AddrPort) bool {
		return ap.Port() == first.Port() && ap.Addr().Unmap() == first.Addr().Unmap()
	}
	redirected := &OTAVersions{Client: &Client{guard: newGuardedClient(guardConfig{resolver: resolver, admit: admitFirst, tls: trusting(redirecting)})}}
	for _, path := range []string{"/private", "/linklocal", "/loopback"} {
		if err := redirected.Download(ctx, SoftwareVersion{OTAURL: self + path}, io.Discard); !errors.Is(err, errDestination) {
			t.Fatalf("download redirected by %s to %s: err %v; want the hop refused", path, hop[path], err)
		}
	}
	if n := targetConns.Load(); n != 0 {
		t.Fatalf("%d connection(s) reached the redirect's loopback target; want the hop's dial refused", n)
	}
	// The seam admits the first server: a redirect to it is followed, so
	// the refusals above are the hops'.
	var buf bytes.Buffer
	if err := redirected.Download(ctx, SoftwareVersion{OTAURL: self + "/self"}, &buf); err != nil || buf.Len() != len(img) {
		t.Fatalf("a redirect to the admitted server: %d bytes, err %v; want the image", buf.Len(), err)
	}

	// AllowPrivateDestinations: the module's guard is off, the loopback
	// server is dialled (its certificate is not trusted by
	// http.DefaultClient, so the TLS handshake fails after the connection).
	open := &OTAVersions{Client: &Client{AllowPrivateDestinations: true}}
	if err := open.Download(ctx, record, io.Discard); errors.Is(err, errDestination) || conns.Load() == 0 {
		t.Fatalf("with AllowPrivateDestinations: err %v, %d connection(s); want the server dialled", err, conns.Load())
	}

	// A host's HTTPClient is the host's policy: not guarded.
	buf.Reset()
	hosted := &OTAVersions{Client: &Client{HTTPClient: srv.Client()}}
	if err := hosted.Download(ctx, record, &buf); err != nil || buf.Len() != len(img) {
		t.Fatalf("with a host HTTPClient: %d bytes, err %v; want the image", buf.Len(), err)
	}
}

// TestOTADownloadByURLSchemesAndDestinations: a ledger record's OTA URL
// is fetched over https only; a URL the host supplies (DownloadHostURL)
// over http or https, to any address, without a host client.
func TestOTADownloadByURLSchemesAndDestinations(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	img := bytes.Repeat([]byte{0x3C}, 256)
	var hits atomic.Int32
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		_, _ = w.Write(img)
	}))
	t.Cleanup(plain.Close)
	o := &OTAVersions{}
	var oe *OTAError
	if err := o.Download(ctx, SoftwareVersion{OTAURL: plain.URL + "/image.ota"}, io.Discard); !errors.As(err, &oe) ||
		!strings.Contains(err.Error(), `Unsupported protocol "http:" in OTA URL`) || hits.Load() != 0 {
		t.Fatalf("a ledger record's http URL: err %v, %d request(s); want the scheme refused before a request", err, hits.Load())
	}
	var buf bytes.Buffer
	if err := o.DownloadHostURL(ctx, SoftwareVersion{OTAURL: plain.URL + "/image.ota"}, &buf); err != nil || buf.Len() != len(img) {
		t.Fatalf("a host's http URL on loopback: %d bytes, err %v; want the image", buf.Len(), err)
	}
	if err := o.DownloadHostURL(ctx, SoftwareVersion{OTAURL: "ftp://example.com/image.ota"}, io.Discard); err == nil ||
		!strings.Contains(err.Error(), `Unsupported protocol "ftp:" in OTA URL`) {
		t.Fatalf("a host's ftp URL: err %v; want the scheme refused", err)
	}
}

// TestCRLDownloadConnectsToPublicDestinationsOnly: the CRL download of a
// distribution point the ledger names is guarded as the OTA download is.
func TestCRLDownloadConnectsToPublicDestinationsOnly(t *testing.T) {
	t.Parallel()
	var hits atomic.Int32
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		_, _ = w.Write([]byte("crl"))
	}))
	t.Cleanup(plain.Close)
	s := &CertificateStore{opts: CertificateOptions{Client: &Client{}}}
	if body, err := s.downloadCRL(t.Context(), plain.URL+"/x.crl", 0); !errors.Is(err, errDestination) || hits.Load() != 0 {
		t.Fatalf("the CRL download from loopback: %q, err %v, %d request(s); want the destination refused", body, err, hits.Load())
	}
	named := &CertificateStore{opts: CertificateOptions{Client: &Client{guard: newGuardedClient(guardConfig{
		resolver: stubResolver(map[string][]netip.Addr{"crl.example": {netip.MustParseAddr("192.168.10.10")}}),
	})}}}
	if _, err := named.downloadCRL(t.Context(), "http://crl.example/x.crl", 0); !errors.Is(err, errDestination) {
		t.Fatalf("the CRL download from a name resolving to a private address: err %v; want the destination refused", err)
	}
	open := &CertificateStore{opts: CertificateOptions{Client: &Client{AllowPrivateDestinations: true}}}
	if body, err := open.downloadCRL(t.Context(), plain.URL+"/x.crl", 0); err != nil || string(body) != "crl" {
		t.Fatalf("with AllowPrivateDestinations: %q, err %v; want the CRL", body, err)
	}
}

// TestGuardBehindAProxy: under a proxy (from the environment in
// production; a test seam here) the guard dials the proxy, and a host
// name is the proxy's to resolve; a URL whose host is localhost or an IP
// address of a refused class is refused before the hand-over — the first
// URL and a redirect hop alike.
func TestGuardBehindAProxy(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	var asked []string
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		asked = append(asked, r.URL.String())
		mu.Unlock()
		if r.URL.Path == "/hop" {
			http.Redirect(w, r, "http://10.0.0.1/x.crl", http.StatusFound)
			return
		}
		_, _ = w.Write([]byte("crl"))
	}))
	t.Cleanup(proxy.Close)
	proxyURL, err := url.Parse(proxy.URL)
	if err != nil {
		t.Fatal(err)
	}
	seen := func() []string {
		mu.Lock()
		defer mu.Unlock()
		return slices.Clone(asked)
	}
	s := &CertificateStore{opts: CertificateOptions{Client: &Client{guard: newGuardedClient(guardConfig{
		resolver: stubResolver(nil),
		proxy:    func(*http.Request) (*url.URL, error) { return proxyURL, nil },
	})}}}
	body, err := s.downloadCRL(t.Context(), "http://crl.example/x.crl", 0)
	if err != nil || string(body) != "crl" || !slices.Equal(seen(), []string{"http://crl.example/x.crl"}) {
		t.Fatalf("a host name through a loopback proxy: %q, err %v, the proxy was asked for %v; want the CRL through the proxy", body, err, seen())
	}
	for _, u := range []string{
		"http://127.0.0.1:8080/x.crl", "http://[::1]/x.crl", "http://10.0.0.1/x.crl", "http://[fd00::1]/x.crl",
		"http://[::ffff:192.168.0.1]/x.crl", "http://169.254.169.254/x.crl", "http://[fe80::1%25eth0]/x.crl",
		"http://224.0.0.1/x.crl", "http://0.0.0.0/x.crl", "http://[::]/x.crl", "http://localhost/x.crl", "http://LOCALHOST./x.crl",
		"https://localhost:8443/x.crl",
	} {
		if _, err := s.downloadCRL(t.Context(), u, 0); !errors.Is(err, errDestination) {
			t.Fatalf("%s through the proxy: err %v; want it refused before the hand-over", u, err)
		}
	}
	if got := seen(); len(got) != 1 {
		t.Fatalf("the proxy was asked for %v; want no refused URL handed over", got)
	}
	if _, err := s.downloadCRL(t.Context(), "http://crl.example/hop", 0); !errors.Is(err, errDestination) {
		t.Fatalf("a redirect to a private literal through the proxy: err %v; want the hop refused", err)
	}
	if got := seen(); len(got) != 2 || got[1] != "http://crl.example/hop" {
		t.Fatalf("the proxy was asked for %v; want the hop not handed over", got)
	}
}

func discardLog() *log.Logger { return log.New(io.Discard, "", 0) }

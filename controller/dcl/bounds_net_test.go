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
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
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

	"thingfabric.dev/go-fabric/internal/dclseam"
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
	if body, err := s.downloadCRL(t.Context(), plain.URL+"/x.crl", 0); !dclseam.IsDestinationRefused(err) || hits.Load() != 0 {
		t.Fatalf("the CRL download from loopback: %q, err %v, %d request(s); want the destination refused", body, err, hits.Load())
	}
	named := &CertificateStore{opts: CertificateOptions{Client: guardedClient(t, dclseam.GuardConfig{
		Resolver: stubResolver(map[string][]netip.Addr{"crl.example": {netip.MustParseAddr("192.168.10.10")}}),
	})}}
	if _, err := named.downloadCRL(t.Context(), "http://crl.example/x.crl", 0); !dclseam.IsDestinationRefused(err) {
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
	s := &CertificateStore{opts: CertificateOptions{Client: guardedClient(t, dclseam.GuardConfig{
		Resolver: stubResolver(nil),
		Proxy:    func(*http.Request) (*url.URL, error) { return proxyURL, nil },
	})}}
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
		if _, err := s.downloadCRL(t.Context(), u, 0); !dclseam.IsDestinationRefused(err) {
			t.Fatalf("%s through the proxy: err %v; want it refused before the hand-over", u, err)
		}
	}
	if got := seen(); len(got) != 1 {
		t.Fatalf("the proxy was asked for %v; want no refused URL handed over", got)
	}
	if _, err := s.downloadCRL(t.Context(), "http://crl.example/hop", 0); !dclseam.IsDestinationRefused(err) {
		t.Fatalf("a redirect to a private literal through the proxy: err %v; want the hop refused", err)
	}
	if got := seen(); len(got) != 2 || got[1] != "http://crl.example/hop" {
		t.Fatalf("the proxy was asked for %v; want the hop not handed over", got)
	}
}

// TestGitHubNamedURLsConnectToPublicDestinationsOnly: the URLs the
// GitHub API's answers name (a subtree, a file) are fetched as a URL a
// ledger record names: without a host client, a name resolving to a
// private address, a loopback server and a private IP literal are
// refused; a destination the guard admits (the test seam stands in for a
// public address) is fetched. The configured APIURL is the host's.
func TestGitHubNamedURLsConnectToPublicDestinationsOnly(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	var otherHits atomic.Int32
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		otherHits.Add(1)
		_, _ = w.Write([]byte("x"))
	}))
	t.Cleanup(other.Close)
	api := httptest.NewUnstartedServer(nil)
	self := "http://" + api.Listener.Addr().String()
	api.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/project-chip/connectedhomeip/git/trees/master":
			_, _ = fmt.Fprintf(w, `{"tree":[
				{"path":"public","type":"blob","url":%q},
				{"path":"loopback","type":"blob","url":%q},
				{"path":"literal","type":"blob","url":"http://10.0.0.1/b"},
				{"path":"named","type":"tree","url":"http://private.example/t"}]}`, self+"/b/public", other.URL+"/b")
		case "/b/public":
			_, _ = w.Write([]byte("der"))
		default:
			http.NotFound(w, r)
		}
	})
	api.Start()
	t.Cleanup(api.Close)
	first := api.Listener.Addr().(*net.TCPAddr).AddrPort()
	c := guardedClient(t, dclseam.GuardConfig{
		Resolver: stubResolver(map[string][]netip.Addr{"private.example": {netip.MustParseAddr("10.1.2.3")}}),
		Admit: func(ap netip.AddrPort) bool {
			return ap.Port() == first.Port() && ap.Addr().Unmap() == first.Addr().Unmap()
		},
	})
	repo := newGitHubClient(GitHubConfig{APIURL: self}, c).repo()
	if b, err := repo.getBinary(ctx, "public"); err != nil || string(b) != "der" {
		t.Fatalf("a named URL the guard admits: %q, err %v; want the file", b, err)
	}
	for _, name := range []string{"loopback", "literal"} {
		if _, err := repo.getBinary(ctx, name); !dclseam.IsDestinationRefused(err) {
			t.Fatalf("the named URL of %s: err %v; want the destination refused", name, err)
		}
	}
	dir, err := repo.cd(ctx, "named")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := dir.ls(ctx); !dclseam.IsDestinationRefused(err) {
		t.Fatalf("a named subtree resolving to a private address: err %v; want the destination refused", err)
	}
	if n := otherHits.Load(); n != 0 {
		t.Fatalf("%d request(s) reached the loopback server a named URL pointed at; want none", n)
	}
}

// TestGuardJudgesTheHostAsSent: a host is judged in the form it reaches a
// proxy or a dial. Behind a proxy, a host the transport maps to localhost
// or to a refused address (fullwidth digits, ideographic full stops,
// fullwidth letters: net/http maps the host to ASCII before a CONNECT,
// canonicalAddr, net/http/transport.go:3219-3225 at go1.27.4) and an IPv4
// address in a legacy spelling a resolver or proxy reads (decimal, hex,
// octal, short forms, a trailing dot) are refused before the proxy is
// asked — as the first URL and as a redirect hop; a URL whose host stays a
// name reaches the proxy as one. No proxy is handed a host outside ASCII
// (5b-dcl R8).
func TestGuardJudgesTheHostAsSent(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	var asked []string
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		asked = append(asked, r.Method+" "+r.RequestURI)
		mu.Unlock()
		if r.URL.Path == "/hop" {
			http.Redirect(w, r, r.URL.Query().Get("to"), http.StatusFound)
			return
		}
		if r.Method == http.MethodConnect {
			http.Error(w, "no tunnel in this test", http.StatusBadGateway)
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
	s := &CertificateStore{opts: CertificateOptions{Client: guardedClient(t, dclseam.GuardConfig{
		Resolver: stubResolver(nil),
		Proxy:    func(*http.Request) (*url.URL, error) { return proxyURL, nil },
	})}}
	refused := []string{
		"http://2130706433/x", "https://2130706433/x", "http://0x7f000001/x", "http://0x7f.0.0.1/x", "http://0177.0.0.1/x",
		"http://010.0.0.1/x", "http://127.1/x", "http://10.1/x", "http://127.0.0.1./x", "http://0XA9.0XFE.0.1/x",
		"https://１２７.０.０.１/x", "https://127。0。0。1/x", "https://１０．０．０．１/x", "https://ｌｏｃａｌｈｏｓｔ/x", "https://ＬＯＣＡＬＨＯＳＴ/x",
	}
	for _, u := range refused {
		before := len(seen())
		if _, err := s.downloadCRL(t.Context(), u, 0); !dclseam.IsDestinationRefused(err) {
			t.Errorf("%s: err %v; want it refused", u, err)
		}
		if got := seen()[before:]; len(got) != 0 {
			t.Errorf("%s: the proxy was asked %v; want nothing handed over", u, got)
		}
	}
	for _, to := range []string{"https://１０.０.０.１/x", "http://0x0a000001/x", "http://169.254.169.254./x", "https://ｌｏｃａｌｈｏｓｔ/x"} {
		before := len(seen())
		if _, err := s.downloadCRL(t.Context(), "http://crl.example/hop?to="+url.QueryEscape(to), 0); !dclseam.IsDestinationRefused(err) {
			t.Errorf("a redirect to %s: err %v; want the hop refused", to, err)
		}
		if got := seen()[before:]; len(got) != 1 {
			t.Errorf("a redirect to %s: the proxy was asked %v; want only the first request", to, got)
		}
	}
	// Names stay names: a public-looking name and an all-numeric label that
	// is no IPv4 spelling.
	for _, u := range []string{"http://crl.example/x", "http://999.0.0.1/x", "http://1.2.3.4.5/x", "http://08.0.0.1/x"} {
		if _, err := s.downloadCRL(t.Context(), u, 0); err != nil {
			t.Errorf("%s: err %v; want it handed to the proxy as a name", u, err)
		}
	}
	before := len(seen())
	if _, err := s.downloadCRL(t.Context(), "http://１２７.０.０.１/x", 0); !dclseam.IsDestinationRefused(err) {
		t.Errorf("a host outside ASCII over plain http through the proxy: err %v; want it refused", err)
	}
	if got := seen()[before:]; len(got) != 0 {
		t.Errorf("the proxy was asked %v; want nothing handed over", got)
	}

	socks := &CertificateStore{opts: CertificateOptions{Client: guardedClient(t, dclseam.GuardConfig{
		Resolver: stubResolver(nil),
		Proxy:    func(*http.Request) (*url.URL, error) { return url.Parse("socks5://" + proxyURL.Host) },
	})}}
	if _, err := socks.downloadCRL(t.Context(), "http://ｃｒｌ.example/x", 0); !dclseam.IsDestinationRefused(err) {
		t.Errorf("a host outside ASCII through a SOCKS proxy: err %v; want it refused", err)
	}
}

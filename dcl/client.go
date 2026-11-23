// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package dcl

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// The ledger's REST endpoints — matter.js DclConfig.production and
// DclConfig.test (packages/protocol/src/dcl/DclConfig.ts:17-25 at
// b25ed960).
const (
	ProductionURL = "https://on.dcl.csa-iot.org"
	TestNetURL    = "https://on.test-net.dcl.csa-iot.org"
)

// DefaultTimeout bounds one ledger request — DclClient.ts:39
// (DEFAULT_DCL_TIMEOUT = Seconds(5)).
const DefaultTimeout = 5 * time.Second

// CodeNotFound is the gRPC NOT_FOUND code the ledger answers an absent
// record with (DclRestApiTypes.ts:20-38, DclErrorCodes.NotFound).
const CodeNotFound = 5

// Error is a ledger answer the client cannot use — matter.js MatterDclError
// (DclClient.ts:42).
type Error struct{ Message string }

func (e *Error) Error() string { return e.Message }

func dclErrorf(format string, args ...any) error {
	return &Error{Message: fmt.Sprintf(format, args...)}
}

// ResponseError is a request that failed — matter.js MatterDclResponseError
// (DclClient.ts:45-52): the ledger's error body when it answered one, code
// 500 with the cause's message when the request itself failed
// (DclClient.ts:109-119).
type ResponseError struct {
	// Path is the requested path.
	Path string
	// Code, Message and Details are DclApiErrorResponse
	// (DclRestApiTypes.ts:40-44).
	Code    int
	Message string
	Details []string
	// Err is the cause of a failed request, nil for a ledger error body.
	Err error
}

func (e *ResponseError) Error() string {
	return fmt.Sprintf("Error fetching %s from DCL: %d - %s", e.Path, e.Code, e.Message)
}

func (e *ResponseError) Unwrap() error { return e.Err }

// IsNotFound reports whether err is the ledger's NOT_FOUND answer.
func IsNotFound(err error) bool {
	var re *ResponseError
	return errors.As(err, &re) && re.Code == CodeNotFound
}

// Client reads the ledger's REST API — matter.js DclClient
// (DclClient.ts:55-299). The zero value asks the production ledger with
// the default HTTP client and timeout.
type Client struct {
	// BaseURL is the ledger; empty is [ProductionURL].
	BaseURL string
	// HTTPClient sends the requests; nil is http.DefaultClient for the
	// ledger's own requests and, for the download of a URL a ledger
	// record names (an OTA image, a CRL distribution point) or the GitHub
	// API's answer names (the development-certificate mirror), the module's
	// guarded client: it connects to public addresses only (see
	// AllowPrivateDestinations). A host's HTTPClient is the host's policy
	// for those downloads too: the destination guard does not apply to
	// it, only the refusal of a redirect away from https does.
	HTTPClient *http.Client
	// AllowPrivateDestinations lets the download of a URL a ledger
	// record names connect to any address. Without it, when HTTPClient
	// is nil, the connection to a loopback, private, link-local,
	// unspecified or multicast address (IPv4 and IPv6, an IPv4-mapped
	// IPv6 address as its IPv4 address) is refused when it is dialled —
	// for the URL itself and for every redirect hop, after the name is
	// resolved. Only those five classes are refused: other special-use
	// ranges — shared address space 100.64.0.0/10, the NAT64 prefix
	// 64:ff9b::/96, 6to4 2002::/16, Teredo 2001::/32 and the like — are
	// not. A lab whose ledger names hosts on the local network sets
	// it. The switch that counts is the one of the client whose download
	// it is: softwareupdate.Images downloads a test-net record through
	// [OTAVersions].TestClient, every other ledger-named URL through the
	// client it was asked with.
	//
	// The environment's proxy (HTTPS_PROXY, HTTP_PROXY, NO_PROXY, as
	// net/http reads them) stays in use. A URL — the first one or a
	// redirect hop — whose host is localhost or an IP address of the
	// refused classes is refused before it is handed to the proxy or
	// dialled: the host as net/http sends it (mapped to ASCII for a
	// CONNECT), lower-cased, leading and trailing dots dropped, an IPv4
	// address in any inet_aton-style spelling (decimal, octal,
	// hexadecimal, short forms) included. No proxy is handed a host
	// outside ASCII.
	// A host name sent through the proxy is the proxy's to resolve and
	// connect to; the guard dials only the proxy.
	AllowPrivateDestinations bool
	// Timeout bounds each request; 0 is [DefaultTimeout].
	Timeout time.Duration
	// Logger receives the client's debug and warning lines; nil discards
	// them.
	Logger *slog.Logger

	// guard is the guarded client of the ledger-named downloads; nil is
	// the module's (sharedGuard). A test seam.
	guard *http.Client
}

// NewClient returns a client of the ledger at baseURL.
func NewClient(baseURL string) *Client { return &Client{BaseURL: baseURL} }

func (c *Client) log() *slog.Logger {
	if c == nil || c.Logger == nil {
		return slog.New(slog.DiscardHandler)
	}
	return c.Logger
}

func (c *Client) httpClient() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return http.DefaultClient
}

// ledgerURLClient is the client of the download of a URL a ledger
// record names: the host's HTTPClient, else the guarded client unless
// AllowPrivateDestinations; each with the redirect rule of
// downloadClient.
func (c *Client) ledgerURLClient() *http.Client {
	switch {
	case c != nil && c.HTTPClient != nil:
		return downloadClient(c.HTTPClient)
	case c != nil && c.AllowPrivateDestinations:
		return downloadClient(http.DefaultClient)
	case c != nil && c.guard != nil:
		return downloadClient(c.guard)
	}
	return downloadClient(sharedGuard())
}

// hostURLClient is the client of the download of a URL the host supplied
// ([OTAVersions.DownloadHostURL]): the host's HTTPClient or
// http.DefaultClient, with the redirect rule of downloadClient and no
// destination guard.
func (c *Client) hostURLClient() *http.Client {
	if c != nil && c.HTTPClient != nil {
		return downloadClient(c.HTTPClient)
	}
	return downloadClient(http.DefaultClient)
}

// errDestination is a ledger-named download refused at the dial: the
// address it would connect to is not public.
var errDestination = errors.New("dcl: destination refused")

// publicDestination reports whether a ledger-named download may connect
// to a: not loopback, private, link-local, unspecified or multicast, an
// IPv4-mapped IPv6 address judged as its IPv4 address.
func publicDestination(a netip.Addr) bool {
	a = a.Unmap()
	return a.IsValid() && !a.IsLoopback() && !a.IsPrivate() && !a.IsLinkLocalUnicast() &&
		!a.IsLinkLocalMulticast() && !a.IsInterfaceLocalMulticast() && !a.IsMulticast() && !a.IsUnspecified()
}

// guardConfig configures newGuardedClient. Its zero value is the
// module's guard; the fields are test seams.
type guardConfig struct {
	// resolver resolves the names; nil is net.DefaultResolver.
	resolver *net.Resolver
	// admit, when it reports true, admits an address the guard refuses.
	admit func(netip.AddrPort) bool
	// proxy is the transport's proxy; nil is http.ProxyFromEnvironment.
	proxy func(*http.Request) (*url.URL, error)
	// tls is the transport's TLS configuration; nil is net/http's.
	tls *tls.Config
}

// destinationGuard refuses, at the dial, an address that is not public,
// and before the hand-over to a proxy or the dial, a URL whose host is
// localhost or such an address; the dial to the proxy a request was
// handed is not checked — that request's, and no other dial to the same
// address (review II, 5b-dcl F6).
type destinationGuard struct {
	admit func(netip.AddrPort) bool
	proxy func(*http.Request) (*url.URL, error)
}

// proxyDialKey is the request context's key of its proxyDial.
type proxyDialKey struct{}

// proxyDial is the dial address of the proxy one request was handed
// (proxyFor records it); the dial to exactly that address, under that
// request's context, is the one the guard does not check.
type proxyDial struct{ addr atomic.Pointer[string] }

// guardedTransport gives every request, each redirect hop included, its
// own proxyDial before the transport sees it.
type guardedTransport struct{ *http.Transport }

func (t guardedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return t.Transport.RoundTrip(req.WithContext(context.WithValue(req.Context(), proxyDialKey{}, &proxyDial{})))
}

// control is the dialer's Control: it sees the resolved address of
// every connection attempt, so a name that resolves to a refused address
// is refused whatever it resolved to before.
func (g *destinationGuard) control(_, address string, _ syscall.RawConn) error {
	ap, err := netip.ParseAddrPort(address)
	if err != nil {
		return fmt.Errorf("%w: %s: %w", errDestination, address, err)
	}
	if g.admit != nil && g.admit(ap) {
		return nil
	}
	if !publicDestination(ap.Addr()) {
		return fmt.Errorf("%w: %s is not a public address (Client.AllowPrivateDestinations admits it)", errDestination, ap.Addr())
	}
	return nil
}

// refusedHost reports an error when u's host is localhost or an IP
// address the guard refuses (refusedHostPort) — the check a URL passes
// before the transport hands it to a proxy or dials it, and again on
// each redirect. A host with characters outside ASCII is judged as the
// transport sends it, in connectTarget (through a proxy) or control
// (dialled).
func (g *destinationGuard) refusedHost(u *url.URL) error {
	port := u.Port()
	if port == "" {
		port = map[string]string{"http": "80", "https": "443"}[strings.ToLower(u.Scheme)]
	}
	return g.refusedHostPort(u.Hostname(), port)
}

// refusedHostPort reports an error when host — lower-cased, its leading
// and trailing dots dropped (review II, 5b-dcl R7) — is localhost or an
// IPv4 or IPv6 address the guard refuses, an IPv4 address in any spelling an inet_aton-style parser
// accepts included (ipv4Legacy). A part with a leading 0 is read both as
// octal, as inet_aton reads it, and as decimal, as a parser that knows no
// octal reads it; either reading in a refused class refuses the host. Any
// other host is a name: the dial checks the address it resolves to, or
// the proxy resolves it.
func (g *destinationGuard) refusedHostPort(host, port string) error {
	host = strings.Trim(strings.ToLower(host), ".")
	if host == "localhost" {
		return fmt.Errorf("%w: %s names localhost (Client.AllowPrivateDestinations admits it)", errDestination, host)
	}
	var readings []netip.Addr
	if a, err := netip.ParseAddr(host); err == nil {
		readings = append(readings, a)
	} else {
		if a, ok := ipv4Legacy(host, true); ok {
			readings = append(readings, a)
		}
		if a, ok := ipv4Legacy(host, false); ok {
			readings = append(readings, a)
		}
	}
	p, perr := strconv.ParseUint(port, 10, 16)
	for _, a := range readings {
		if perr == nil && g.admit != nil && g.admit(netip.AddrPortFrom(a, uint16(p))) {
			continue
		}
		if !publicDestination(a) {
			return fmt.Errorf("%w: %s (%s) is not a public address (Client.AllowPrivateDestinations admits it)", errDestination, host, a)
		}
	}
	return nil
}

// ipv4Legacy parses host as an IPv4 address in the spellings an
// inet_aton-style parser (a resolver, a proxy) accepts: one to four
// dot-separated parts, each decimal, octal with a leading 0 (decimal
// instead when octal is false) or hexadecimal with a leading 0x, the last
// part filling the bytes the parts before it leave (127.1 is 127.0.0.1,
// 2130706433 is the whole address).
func ipv4Legacy(host string, octal bool) (netip.Addr, bool) {
	parts := strings.Split(host, ".")
	if len(parts) > 4 {
		return netip.Addr{}, false
	}
	vals := make([]uint64, len(parts))
	for k, p := range parts {
		base, digits := 10, p
		switch {
		case strings.HasPrefix(p, "0x"):
			base, digits = 16, p[2:]
			if digits == "" {
				digits = "0"
			}
		case octal && len(p) > 1 && p[0] == '0':
			base, digits = 8, p[1:]
		}
		if digits == "" || strings.ContainsAny(digits, "+-_") {
			return netip.Addr{}, false
		}
		v, err := strconv.ParseUint(digits, base, 32)
		if err != nil {
			return netip.Addr{}, false
		}
		vals[k] = v
	}
	last := len(vals) - 1
	var n uint64
	for k := range last {
		if vals[k] > 0xff {
			return netip.Addr{}, false
		}
		n |= vals[k] << (24 - 8*k)
	}
	if vals[last] >= 1<<(8*(4-last)) {
		return netip.Addr{}, false
	}
	n |= vals[last]
	var b [4]byte
	binary.BigEndian.PutUint32(b[:], uint32(n)) //nolint:gosec // n < 1<<32: each part bounded above
	return netip.AddrFrom4(b), true
}

// connectTarget is the transport's GetProxyConnectHeader: it sees the
// target of a CONNECT as the transport writes it (canonicalAddr) before
// the proxy is asked, and refuses it as refusedHostPort does. net/http
// maps a host to ASCII where it can; a host it cannot map reaches here
// unmapped and is refused. proxyFor refuses a host outside ASCII before
// any proxy is asked, so this check is a second line.
func (g *destinationGuard) connectTarget(_ context.Context, _ *url.URL, target string) (http.Header, error) {
	host, port, err := net.SplitHostPort(target)
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %w", errDestination, target, err)
	}
	if !isASCII(host) {
		return nil, fmt.Errorf("%w: %q: a host net/http could not map to ASCII", errDestination, host)
	}
	if err := g.refusedHostPort(host, port); err != nil {
		return nil, err
	}
	return nil, nil
}

// checkRedirect is the guarded client's redirect policy: a hop to a
// refused host is refused, and net/http's default limit of ten applies
// (downloadClient defers to a client's own policy and adds its limit
// only without one).
func (g *destinationGuard) checkRedirect(req *http.Request, via []*http.Request) error {
	if err := g.refusedHost(req.URL); err != nil {
		return err
	}
	if len(via) >= 10 {
		return errors.New("dcl: stopped after 10 redirects")
	}
	return nil
}

// proxyFor is the transport's Proxy: it refuses a URL whose host the
// guard refuses (refusedHost), then returns the proxy the environment
// names for req and records its dial address in req's proxyDial, so that
// the dial to it for req is not checked. Through any proxy a host with
// characters outside ASCII is refused (review II, 5b-dcl R8).
func (g *destinationGuard) proxyFor(req *http.Request) (*url.URL, error) {
	if err := g.refusedHost(req.URL); err != nil {
		return nil, err
	}
	p, err := g.proxy(req)
	if p != nil && !isASCII(req.URL.Hostname()) {
		return nil, fmt.Errorf("%w: %s through a proxy: a host outside ASCII is not handed to a proxy", errDestination, req.URL.Redacted())
	}
	d, _ := req.Context().Value(proxyDialKey{}).(*proxyDial)
	switch {
	case d == nil:
	case p == nil:
		// The latest answer is the one the dial follows (net/http asks
		// again on a retry of the request; review II, 5b-dcl F6).
		d.addr.Store(nil)
	default:
		port := p.Port()
		if port == "" {
			port = map[string]string{"http": "80", "https": "443", "socks5": "1080", "socks5h": "1080"}[strings.ToLower(p.Scheme)]
		}
		addr := net.JoinHostPort(p.Hostname(), port)
		d.addr.Store(&addr)
	}
	return p, err
}

// newGuardedClient is the client of a ledger-named download when the
// host supplies none: net/http's default transport settings, with a
// dialer that refuses a non-public address.
func newGuardedClient(cfg guardConfig) *http.Client {
	g := &destinationGuard{admit: cfg.admit, proxy: cfg.proxy}
	if g.proxy == nil {
		g.proxy = http.ProxyFromEnvironment
	}
	guarded := &net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second, Resolver: cfg.resolver, Control: g.control}
	direct := &net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second, Resolver: cfg.resolver}
	return &http.Client{CheckRedirect: g.checkRedirect, Transport: guardedTransport{&http.Transport{
		Proxy:                 g.proxyFor,
		GetProxyConnectHeader: g.connectTarget,
		// The dial's context carries the request's values, its proxyDial
		// among them: net/http retains them for the dial
		// (src/net/http/transport.go:1591-1596 at go1.27.4).
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			if d, ok := ctx.Value(proxyDialKey{}).(*proxyDial); ok {
				if p := d.addr.Load(); p != nil && *p == addr {
					return direct.DialContext(ctx, network, addr)
				}
			}
			return guarded.DialContext(ctx, network, addr)
		},
		TLSClientConfig:       cfg.tls,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}}}
}

// isASCII reports whether s has only ASCII characters.
func isASCII(s string) bool {
	for k := range len(s) {
		if s[k] >= 0x80 {
			return false
		}
	}
	return true
}

// sharedGuard is the module's guarded client, one for the process so
// that its connections are reused.
var sharedGuard = sync.OnceValue(func() *http.Client { return newGuardedClient(guardConfig{}) })

// downloadClient is hc for the download of a URL a ledger record names —
// an OTA image, a CRL: a redirect away from https is refused, so the
// bytes of a URL the ledger named https come over https (review II,
// R2-P3-net-06). hc's own redirect policy applies besides; without one,
// net/http's default (ten redirects, src/net/http/client.go:845-850 at
// go1.27.4). matter.js passes its fetch no redirect option
// (packages/protocol/src/dcl/DclOtaUpdateService.ts:690-693 at db021da0),
// so redirects are the platform fetch's default.
func downloadClient(hc *http.Client) *http.Client {
	c := *hc
	own := hc.CheckRedirect
	c.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if first := via[0].URL; strings.EqualFold(first.Scheme, "https") && !strings.EqualFold(req.URL.Scheme, "https") {
			return fmt.Errorf("dcl: redirect from %s to %s refused: it leaves https", first.Redacted(), req.URL.Redacted())
		}
		if own != nil {
			return own(req, via)
		}
		if len(via) >= 10 {
			return errors.New("dcl: stopped after 10 redirects")
		}
		return nil
	}
	return &c
}

func (c *Client) timeout() time.Duration {
	if c.Timeout > 0 {
		return c.Timeout
	}
	return DefaultTimeout
}

// baseURL is the ledger URL the client asks.
func (c *Client) baseURL() string {
	if c.BaseURL == "" {
		return ProductionURL
	}
	return c.BaseURL
}

// fetchJSON is DclClient.#fetchJson (DclClient.ts:91-121): a GET of path
// resolved against the base URL, with a JSON Content-Type header and the
// timeout; a non-OK answer is the ledger's error body, any other failure
// a ResponseError of code 500.
func (c *Client) fetchJSON(ctx context.Context, path string, out any) error {
	wrap := func(err error) error {
		var re *ResponseError
		if errors.As(err, &re) {
			return err
		}
		return &ResponseError{Path: path, Code: 500, Message: err.Error(), Details: []string{}, Err: err}
	}
	base, err := url.Parse(c.baseURL())
	if err != nil {
		return wrap(err)
	}
	ref, err := url.Parse(path)
	if err != nil {
		return wrap(err)
	}
	u := base.ResolveReference(ref).String()
	c.log().Debug("dcl.fetch", slog.String("url", u))
	ctx, cancel := context.WithTimeout(ctx, c.timeout())
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, http.NoBody)
	if err != nil {
		return wrap(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return wrap(err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(http.MaxBytesReader(nil, resp.Body, maxResponseBody))
	if err != nil {
		return wrap(err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		var apiErr struct {
			Code    int      `json:"code"`
			Message string   `json:"message"`
			Details []string `json:"details"`
		}
		if err := json.Unmarshal(body, &apiErr); err != nil {
			return wrap(err)
		}
		return &ResponseError{Path: path, Code: apiErr.Code, Message: apiErr.Message, Details: apiErr.Details}
	}
	if err := json.Unmarshal(body, out); err != nil {
		return wrap(err)
	}
	return nil
}

// maxResponseBody bounds a ledger or GitHub answer the client reads,
// maxPages the pages of one paginated listing, maxListingBytes the wire
// bytes of the items those pages hold together (review II, R2-P2-alloc-08:
// the first two alone multiply to 16 GiB) and maxListingItems their count
// (review II, R2-P2-dr-ctl-03: an item of three wire bytes, "{},",
// decodes to a whole struct, so the bytes alone let a listing decode to
// gigabytes). Decoded, a listing holds at most maxListingItems structs
// and the strings of at most maxListingBytes: a misbehaving endpoint
// costs bounded memory (W4-ctlint-10). matter.js reads the whole answer
// (DclClient.ts:91-121, Github.ts:120-141) and follows next_key without a
// bound (:62-89); the bounds sit far above the production ledger's
// answers.
const (
	maxResponseBody = 16 << 20
	maxPages        = 1000
	maxListingBytes = 64 << 20
	maxListingItems = 1 << 16
)

// fetchPaginated is DclClient.#fetchPaginatedJson (DclClient.ts:62-89):
// the items of field, page after page while the answer names a
// pagination.next_key, which the next request carries as pagination.key —
// at most maxPages pages holding at most maxListingBytes of items and at
// most maxListingItems of them.
func fetchPaginated[T any](ctx context.Context, c *Client, path, field string) ([]T, error) {
	var all []T
	listed := 0
	nextKey := ""
	for page := 0; page == 0 || nextKey != ""; page++ {
		if page == maxPages {
			return nil, fmt.Errorf("dcl: %s: more than %d pages", path, maxPages)
		}
		current := path
		if nextKey != "" {
			sep := "?"
			if strings.Contains(path, "?") {
				sep = "&"
			}
			current = path + sep + "pagination.key=" + encodeURIComponent(nextKey)
		}
		var answer map[string]json.RawMessage
		if err := c.fetchJSON(ctx, current, &answer); err != nil {
			return nil, err
		}
		// Items that are not an array are skipped (:79-82).
		if raw, ok := answer[field]; ok {
			if listed += len(raw); listed > maxListingBytes {
				return nil, fmt.Errorf("dcl: %s: the listing's items pass %d bytes", path, maxListingBytes)
			}
			var over bool
			if all, over = appendItems(all, raw); over {
				return nil, fmt.Errorf("dcl: %s: the listing passes %d items", path, maxListingItems)
			}
		}
		nextKey = ""
		if raw, ok := answer["pagination"]; ok {
			var p struct {
				NextKey *string `json:"next_key"`
			}
			if json.Unmarshal(raw, &p) == nil && p.NextKey != nil {
				nextKey = *p.NextKey
			}
		}
	}
	return all, nil
}

// appendItems appends the items of the JSON array raw to all, one item
// decoded at a time; over is true, and nothing returned, before it would
// decode an item past maxListingItems. Items that are not an array of T
// append nothing, as json.Unmarshal's error skipped them before.
func appendItems[T any](all []T, raw json.RawMessage) (_ []T, over bool) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	if tok, _ := dec.Token(); tok != json.Delim('[') { // an unreadable token is none
		return all, false
	}
	start := len(all)
	for dec.More() {
		if len(all) == maxListingItems {
			return nil, true
		}
		var item T
		if dec.Decode(&item) != nil {
			return all[:start], false
		}
		all = append(all, item)
	}
	return all, false
}

// encodeURIComponent is JavaScript's: every byte but A-Z a-z 0-9 and
// - _ . ! ~ * ' ( ) percent-encoded.
func encodeURIComponent(s string) string {
	var b strings.Builder
	for i := range len(s) {
		ch := s[i]
		if ch >= 'A' && ch <= 'Z' || ch >= 'a' && ch <= 'z' || ch >= '0' && ch <= '9' || strings.IndexByte("-_.!~*'()", ch) >= 0 {
			b.WriteByte(ch)
			continue
		}
		fmt.Fprintf(&b, "%%%02X", ch)
	}
	return b.String()
}

// RootCertificates lists the approved root certificates (PAAs) by subject
// and key identifier — DclClient.fetchRootCertificateList
// (DclClient.ts:123-131): GET /dcl/pki/root-certificates, schema version 0.
func (c *Client) RootCertificates(ctx context.Context) ([]SubjectReference, error) {
	var resp struct {
		ApprovedRootCertificates *rootCertificateList `json:"approvedRootCertificates"`
	}
	if err := c.fetchJSON(ctx, "/dcl/pki/root-certificates", &resp); err != nil {
		return nil, err
	}
	if resp.ApprovedRootCertificates == nil || resp.ApprovedRootCertificates.SchemaVersion == nil || *resp.ApprovedRootCertificates.SchemaVersion != 0 {
		return nil, dclErrorf("Unsupported DCL Root Certificate schema version: %s", schemaVersionText(resp.ApprovedRootCertificates))
	}
	return resp.ApprovedRootCertificates.Certs, nil
}

// rootCertificateList is DclPkiRootCertificatesResponse's record
// (DclRestApiTypes.ts:52-57).
type rootCertificateList struct {
	Certs         []SubjectReference `json:"certs"`
	SchemaVersion *int               `json:"schemaVersion"`
}

func schemaVersionText(v *rootCertificateList) string {
	if v == nil || v.SchemaVersion == nil {
		return "undefined"
	}
	return strconv.Itoa(*v.SchemaVersion)
}

// hexPairs splits a key identifier into the pairs of the ledger's form,
// colon-separated uppercase hex (DclClient.ts:141-143).
var hexPairs = regexp.MustCompile(`.{1,2}`)

// CertificatesBySubjectKeyID returns every certificate the ledger holds
// with the key identifier skid (hex, colons allowed) — DclClient.
// fetchCertificatesBySubjectKeyId (DclClient.ts:140-152): GET
// /dcl/pki/all-certificates?subjectKeyId=<AA:BB:…>, the groups and
// certificates of schema version 0. None is an empty list.
func (c *Client) CertificatesBySubjectKeyID(ctx context.Context, skid string) ([]Certificate, error) {
	normalized := strings.ToUpper(strings.ReplaceAll(skid, ":", ""))
	withColons := normalized
	if pairs := hexPairs.FindAllString(normalized, -1); pairs != nil {
		withColons = strings.Join(pairs, ":")
	}
	var resp struct {
		Certificates []certificateGroup `json:"certificates"`
	}
	if err := c.fetchJSON(ctx, "/dcl/pki/all-certificates?subjectKeyId="+encodeURIComponent(withColons), &resp); err != nil {
		return nil, err
	}
	var out []Certificate
	groups := slices.DeleteFunc(slices.Clone(resp.Certificates), func(g certificateGroup) bool { return g.SchemaVersion != 0 })
	c.warnDropped(len(resp.Certificates)-len(groups), "certificate group")
	for _, g := range groups {
		out = append(out, c.supportedCertificates(g.Certs)...)
	}
	return out, nil
}

// certificateGroup is one subject's certificates in
// DclPkiAllCertificatesBySkidResponse (DclRestApiTypes.ts:60-67).
type certificateGroup struct {
	Certs         []Certificate `json:"certs"`
	SchemaVersion int           `json:"schemaVersion"`
}

// RootCertificateBySubject returns the approved certificates of one root —
// DclClient.fetchRootCertificateBySubject (DclClient.ts:154-169): GET
// /dcl/pki/certificates/{subject}/{subjectKeyId}, which must answer the
// subject and key identifier asked for, schema version 0.
func (c *Client) RootCertificateBySubject(ctx context.Context, ref SubjectReference) ([]Certificate, error) {
	path := "/dcl/pki/certificates/" + encodeURIComponent(ref.Subject) + "/" + encodeURIComponent(ref.SubjectKeyID)
	var resp struct {
		ApprovedCertificates *struct {
			Subject       string        `json:"subject"`
			SubjectKeyID  string        `json:"subjectKeyId"`
			Certs         []Certificate `json:"certs"`
			SchemaVersion *int          `json:"schemaVersion"`
		} `json:"approvedCertificates"`
	}
	if err := c.fetchJSON(ctx, path, &resp); err != nil {
		return nil, err
	}
	a := resp.ApprovedCertificates
	if a == nil || a.Subject != ref.Subject || a.SubjectKeyID != ref.SubjectKeyID || a.SchemaVersion == nil || *a.SchemaVersion != 0 {
		return nil, dclErrorf("Root certificate not found for subject: %s, subjectKeyId: %s", ref.Subject, ref.SubjectKeyID)
	}
	return c.supportedCertificates(a.Certs), nil
}

// Model returns the device model of vid/pid — DclClient.fetchModelByVidPid
// (DclClient.ts:171-184): GET /dcl/model/models/{vid}/{pid}, which must
// answer that vid and pid, schema version 0.
func (c *Client) Model(ctx context.Context, vid, pid uint16) (DeviceModel, error) {
	var resp struct {
		Model *DeviceModel `json:"model"`
	}
	if err := c.fetchJSON(ctx, fmt.Sprintf("/dcl/model/models/%d/%d", vid, pid), &resp); err != nil {
		return DeviceModel{}, err
	}
	m := resp.Model
	if m == nil || m.VID != vid || m.PID != pid || m.SchemaVersion != 0 {
		return DeviceModel{}, dclErrorf("Model not found for VID: %d, PID: %d", vid, pid)
	}
	return m.mapped(), nil
}

// ModelVersions lists the software versions of vid/pid — DclClient.
// fetchModelVersionsByVidPid (DclClient.ts:186-199): GET
// /dcl/model/versions/{vid}/{pid}, which must answer that vid and pid,
// schema version 0.
func (c *Client) ModelVersions(ctx context.Context, vid, pid uint16) ([]uint32, error) {
	var resp struct {
		ModelVersions *struct {
			VID              uint16   `json:"vid"`
			PID              uint16   `json:"pid"`
			SoftwareVersions []uint32 `json:"softwareVersions"`
			SchemaVersion    int      `json:"schemaVersion"`
		} `json:"modelVersions"`
	}
	if err := c.fetchJSON(ctx, fmt.Sprintf("/dcl/model/versions/%d/%d", vid, pid), &resp); err != nil {
		return nil, err
	}
	m := resp.ModelVersions
	if m == nil || m.VID != vid || m.PID != pid || m.SchemaVersion != 0 {
		return nil, dclErrorf("Model versions not found for VID: %d, PID: %d", vid, pid)
	}
	return m.SoftwareVersions, nil
}

// ModelVersion returns the record of one software version — DclClient.
// fetchModelVersionByVidPidSoftwareVersion (DclClient.ts:201-222): GET
// /dcl/model/versions/{vid}/{pid}/{softwareVersion}, which must answer that
// vid, pid and version, schema version 0.
func (c *Client) ModelVersion(ctx context.Context, vid, pid uint16, softwareVersion uint32) (SoftwareVersion, error) {
	var resp struct {
		ModelVersion *rawSoftwareVersion `json:"modelVersion"`
	}
	if err := c.fetchJSON(ctx, fmt.Sprintf("/dcl/model/versions/%d/%d/%d", vid, pid, softwareVersion), &resp); err != nil {
		return SoftwareVersion{}, err
	}
	m := resp.ModelVersion
	if m == nil || m.VID != vid || m.PID != pid || m.SoftwareVersion != softwareVersion || m.SchemaVersion != 0 {
		return SoftwareVersion{}, dclErrorf("Model version not found for VID: %d, PID: %d, Software Version: %d", vid, pid, softwareVersion)
	}
	return m.mapped(), nil
}

// ComplianceInfo returns the compliance record of a software version for
// a certification program — DclClient.fetchComplianceInfo
// (DclClient.ts:229-257): GET
// /dcl/compliance/compliance-info/{vid}/{pid}/{softwareVersion}/{certificationType},
// which must answer that vid, pid, version and program, schema version 0
// or 1.
func (c *Client) ComplianceInfo(ctx context.Context, vid, pid uint16, softwareVersion uint32, certificationType string) (ComplianceInfo, error) {
	var resp struct {
		ComplianceInfo *ComplianceInfo `json:"complianceInfo"`
	}
	path := fmt.Sprintf("/dcl/compliance/compliance-info/%d/%d/%d/%s", vid, pid, softwareVersion, encodeURIComponent(certificationType))
	if err := c.fetchJSON(ctx, path, &resp); err != nil {
		return ComplianceInfo{}, err
	}
	info := resp.ComplianceInfo
	if info == nil || info.VID != vid || info.PID != pid || info.SoftwareVersion != softwareVersion || info.CertificationType != certificationType {
		return ComplianceInfo{}, dclErrorf("Compliance info not found for VID: %d, PID: %d, Software Version: %d, Certification Type: %s", vid, pid, softwareVersion, certificationType)
	}
	// Schema versions 0 and 1 are the ones Core §11.23.10.15 defines.
	if info.SchemaVersion != 0 && info.SchemaVersion != 1 {
		return ComplianceInfo{}, dclErrorf("Unsupported DCL compliance info schema version %d for VID: %d, PID: %d, Software Version: %d", info.SchemaVersion, vid, pid, softwareVersion)
	}
	return *info, nil
}

// Vendors lists every vendor record — DclClient.fetchAllVendors
// (DclClient.ts:262-265): GET /dcl/vendorinfo/vendors, paginated under
// vendorInfo, the records of schema version 0.
func (c *Client) Vendors(ctx context.Context) ([]Vendor, error) {
	raw, err := fetchPaginated[Vendor](ctx, c, "/dcl/vendorinfo/vendors", "vendorInfo")
	if err != nil {
		return nil, err
	}
	n := len(raw)
	out := slices.DeleteFunc(raw, func(v Vendor) bool { return v.SchemaVersion != 0 })
	c.warnDropped(n-len(out), "vendor")
	return out, nil
}

// RevocationPoints lists every revocation distribution point — DclClient.
// fetchRevocationDistributionPoints (DclClient.ts:271-280): GET
// /dcl/pki/revocation-points, paginated under PkiRevocationDistributionPoint,
// the records of schema version 0.
func (c *Client) RevocationPoints(ctx context.Context) ([]RevocationPoint, error) {
	raw, err := fetchPaginated[rawRevocationPoint](ctx, c, "/dcl/pki/revocation-points", "PkiRevocationDistributionPoint")
	if err != nil {
		return nil, err
	}
	return c.supportedPoints(raw), nil
}

// RevocationPointsByIssuer lists the revocation distribution points of the
// issuer with key identifier issuerSKID — DclClient.
// fetchRevocationDistributionPointsByIssuer (DclClient.ts:285-298): GET
// /dcl/pki/revocation-points/{issuerSubjectKeyId}; an answer without the
// record is no point, one of another schema version an error.
func (c *Client) RevocationPointsByIssuer(ctx context.Context, issuerSKID string) ([]RevocationPoint, error) {
	var resp struct {
		ByIssuer *struct {
			Points        []rawRevocationPoint `json:"points"`
			SchemaVersion int                  `json:"schemaVersion"`
		} `json:"pkiRevocationDistributionPointsByIssuerSubjectKeyID"`
	}
	if err := c.fetchJSON(ctx, "/dcl/pki/revocation-points/"+encodeURIComponent(issuerSKID), &resp); err != nil {
		return nil, err
	}
	if resp.ByIssuer == nil {
		return nil, nil
	}
	if resp.ByIssuer.SchemaVersion != 0 {
		return nil, dclErrorf("Unsupported DCL revocation points schema version %d for issuer %s", resp.ByIssuer.SchemaVersion, issuerSKID)
	}
	return c.supportedPoints(resp.ByIssuer.Points), nil
}

// warnDropped is supportedRecords' warning (DclClient.ts:305-313).
func (c *Client) warnDropped(n int, kind string) {
	if n > 0 {
		c.log().Warn("dcl.unsupported_schema_version", slog.String("kind", kind), slog.Int("ignored", n),
			slog.String("message", fmt.Sprintf("Ignoring %d DCL %s record(s) with unsupported schema version", n, kind)))
	}
}

// supportedCertificates is supportedRecords for certificates: those of
// schema version 0, a later version may change what a field means.
func (c *Client) supportedCertificates(in []Certificate) []Certificate {
	out := slices.DeleteFunc(slices.Clone(in), func(x Certificate) bool { return x.SchemaVersion != 0 })
	c.warnDropped(len(in)-len(out), "certificate")
	return out
}

func (c *Client) supportedPoints(in []rawRevocationPoint) []RevocationPoint {
	out := make([]RevocationPoint, 0, len(in))
	for i := range in {
		if in[i].SchemaVersion == 0 {
			out = append(out, in[i].mapped())
		}
	}
	c.warnDropped(len(in)-len(out), "revocation point")
	return out
}

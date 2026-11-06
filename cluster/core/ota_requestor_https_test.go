// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	provdef "github.com/SukramJ/go-fabric/cluster/spec/otasoftwareupdateprovider"
	otadef "github.com/SukramJ/go-fabric/cluster/spec/otasoftwareupdaterequestor"
)

// The HTTPS download of the requestor: matter.js #handleDownload and
// #handleHttpsDownload (OtaSoftwareUpdateRequestorServer.ts:1008-1115);
// matter.js has no test of it at the pin, so these hold the Go branch to
// its code, plus the size cap and the timeout BD-Matter-OTARequestor-HTTPS
// records.

// otaHTTPSServer serves the harness image over TLS. handler, when set,
// replaces the plain answer; hits counts the requests.
type otaHTTPSServer struct {
	srv  *httptest.Server
	hits atomic.Int32
}

func newOtaHTTPSServer(t *testing.T, handler http.HandlerFunc) *otaHTTPSServer {
	t.Helper()
	s := &otaHTTPSServer{}
	s.srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.hits.Add(1)
		handler(w, r)
	}))
	t.Cleanup(s.srv.Close)
	return s
}

// imageHandler answers with img, stating its Content-Length or, without
// it, flushing the body in two chunks so the answer is chunked.
func imageHandler(img []byte, contentLength bool) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		if contentLength {
			w.Header().Set("Content-Length", strconv.Itoa(len(img)))
			_, _ = w.Write(img)
			return
		}
		half := len(img) / 2
		_, _ = w.Write(img[:half])
		w.(http.Flusher).Flush() //nolint:forcetypeassert // httptest's writer flushes
		_, _ = w.Write(img[half:])
	}
}

// newHTTPSHarness is a harness announcing BDX and HTTPS, its provider
// offering srv's /image.ota.
func newHTTPSHarness(t *testing.T, s *otaHTTPSServer, mutate func(*OTARequestorConfig)) *otaHTTPSHarness {
	t.Helper()
	h := newOtaHarness(t, func(c *OTARequestorConfig) {
		c.TransferProtocols = []DownloadProtocol{DownloadProtocolBDXSynchronous, DownloadProtocolHTTPS}
		c.HTTPClient = s.srv.Client()
		if mutate != nil {
			mutate(c)
		}
	})
	h.provider.answer = func(int) (provdef.QueryImageResponse, error) { return h.offering(s.srv.URL+"/image.ota", 2), nil }
	h.addActive(t)
	return &otaHTTPSHarness{otaHarness: h}
}

type otaHTTPSHarness struct{ *otaHarness }

// offering is an UpdateAvailable answer naming uri.
func (h *otaHarness) offering(uri string, version uint32) provdef.QueryImageResponse {
	resp := h.available(version)
	resp.ImageUri = &uri
	return resp
}

func (h *otaHarness) downloadErrors() []otadef.DownloadErrorEvent {
	events := h.events.of(otadef.EventDownloadError)
	out := make([]otadef.DownloadErrorEvent, 0, len(events))
	for _, e := range events {
		out = append(out, e.(otadef.DownloadErrorEvent)) //nolint:forcetypeassert // test
	}
	return out
}

func (h *otaHarness) progressValue() (uint8, bool) {
	v, _ := h.r.MatterRead(otadef.AttrUpdateStateProgress)
	p, ok := v.(uint8)
	return p, ok
}

// failedDownload asserts the one DownloadError of a failed HTTPS download,
// its platform code and bytes, the Downloading → Idle/Failure transition
// and that neither the file nor its temporary remained.
func (h *otaHarness) failedDownload(t *testing.T, code int64, bytes uint64) {
	t.Helper()
	errs := h.downloadErrors()
	if len(errs) != 1 {
		t.Fatalf("DownloadError %+v, want one", errs)
	}
	if de := errs[0]; de.SoftwareVersion != 2 || de.PlatformCode.Null || de.PlatformCode.Value != code || de.BytesDownloaded != bytes {
		t.Fatalf("DownloadError %+v, want code %d after %d bytes", de, code, bytes)
	}
	tr := h.events.transitions()
	if l := tr[len(tr)-1]; l.PreviousState != otadef.UpdateStateDownloading || l.NewState != otadef.UpdateStateIdle || l.Reason != otadef.ChangeReasonFailure {
		t.Fatalf("last transition %+v", l)
	}
	if len(h.applier.applied) != 0 {
		t.Fatal("failed download applied")
	}
	h.noDownloadLeft(t)
}

func (h *otaHarness) noDownloadLeft(t *testing.T) {
	t.Helper()
	entries, err := os.ReadDir(h.dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("download directory keeps %v", entries)
	}
}

// TestOTARequestorHTTPS_Download: with HTTPS announced (:852, :1372), an
// https:// offer moves to Downloading (:1098-1102), the GET's body becomes
// the download location (:1049), validated (:1109) and applied — with and
// without Content-Length, the progress from it or from the approximation
// (:1015-1018, #updateProgress :1064-1070).
func TestOTARequestorHTTPS_Download(t *testing.T) {
	t.Parallel()
	for _, contentLength := range []bool{true, false} {
		t.Run("Content-Length="+strconv.FormatBool(contentLength), func(t *testing.T) {
			t.Parallel()
			var img []byte
			s := newOtaHTTPSServer(t, func(w http.ResponseWriter, r *http.Request) { imageHandler(img, contentLength)(w, r) })
			h := newHTTPSHarness(t, s, nil)
			img = h.image(2)

			h.r.performUpdateQuery(context.Background())

			q := h.provider.queries[0]
			if want := []provdef.DownloadProtocolEnum{provdef.DownloadProtocolBdxSynchronous, provdef.DownloadProtocolHttps}; !slices.Equal(q.ProtocolsSupported, want) {
				t.Fatalf("ProtocolsSupported %v, want %v", q.ProtocolsSupported, want)
			}
			if want, got := []string{"1>2/1", "2>4/1", "4>5/1", "5>1/1"}, states(h.events.transitions()); !slices.Equal(got, want) {
				t.Fatalf("transitions %v, want %v", got, want)
			}
			if s.hits.Load() != 1 || h.provider.transfers != 0 {
				t.Fatalf("%d GETs, %d BDX transfers", s.hits.Load(), h.provider.transfers)
			}
			if len(h.applier.applied) != 1 || h.applier.applied[0] != 2 {
				t.Fatalf("applied %v", h.applier.applied)
			}
			if got, err := os.ReadFile(h.applier.files[0]); err != nil || !slices.Equal(got, img) {
				t.Fatalf("downloaded file differs (%v)", err)
			}
		})
	}

	// The progress the body leaves behind, before handleDownload sets 100:
	// 99 % of a stated size; the approximation, 0 for 2.6 kB, without one.
	for contentLength, want := range map[bool]uint8{true: 99, false: 0} {
		var img []byte
		s := newOtaHTTPSServer(t, func(w http.ResponseWriter, r *http.Request) { imageHandler(img, contentLength)(w, r) })
		h := newHTTPSHarness(t, s, nil)
		img = h.image(2)
		if err := h.r.handleHTTPSDownload(context.Background(), s.srv.URL+"/image.ota"); err != nil {
			t.Fatal(err)
		}
		if p, ok := h.progressValue(); !ok || p != want {
			t.Fatalf("Content-Length=%v: progress %d (%v), want %d", contentLength, p, ok, want)
		}
	}
}

// TestOTARequestorHTTPS_Redirect: the module's client (HTTPClient nil)
// follows a redirect to an https:// target as fetch does, and refuses one
// to http://, which fetch would follow: an otaDownloadError carrying the
// redirect's status (BD-Matter-OTARequestor-HTTPS). A host's client keeps
// its own policy and follows it.
func TestOTARequestorHTTPS_Redirect(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		toPlain, hostClient bool
	}{
		"https":             {},
		"http-refused":      {toPlain: true},
		"http-host-follows": {toPlain: true, hostClient: true},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var img []byte
			var plainHits atomic.Int32
			plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				plainHits.Add(1)
				imageHandler(img, true)(w, r)
			}))
			t.Cleanup(plain.Close)
			s := newOtaHTTPSServer(t, func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.URL.Path != "/image.ota":
					imageHandler(img, true)(w, r)
				case tc.toPlain:
					http.Redirect(w, r, plain.URL+"/image.ota", http.StatusFound)
				default:
					http.Redirect(w, r, "/moved.ota", http.StatusFound)
				}
			})
			h := newHTTPSHarness(t, s, func(c *OTARequestorConfig) {
				if !tc.hostClient {
					// The module's client, over a transport that trusts the
					// test server's certificate.
					c.HTTPClient = otaDefaultHTTPClient(s.srv.Client().Transport)
				}
			})
			img = h.image(2)
			h.r.performUpdateQuery(context.Background())
			if tc.toPlain && !tc.hostClient {
				h.failedDownload(t, http.StatusFound, 0)
				if plainHits.Load() != 0 {
					t.Fatal("the http:// target was requested")
				}
				return
			}
			if len(h.applier.applied) != 1 || s.hits.Load()+plainHits.Load() != 2 {
				t.Fatalf("%d+%d requests, applied %v", s.hits.Load(), plainHits.Load(), h.applier.applied)
			}
		})
	}
}

// TestOTARequestorHTTPS_StatusNotOK: a status outside 2xx is an
// OtaDownloadError carrying it (:1010-1011) — DownloadError with the status
// as platform code and no bytes (#emitDownloadErrorEvent, :927-929), Idle
// with Failure (:930).
func TestOTARequestorHTTPS_StatusNotOK(t *testing.T) {
	t.Parallel()
	s := newOtaHTTPSServer(t, http.NotFound)
	h := newHTTPSHarness(t, s, nil)
	h.r.performUpdateQuery(context.Background())
	h.failedDownload(t, http.StatusNotFound, 0)
}

// TestOTARequestorHTTPS_TruncatedBody: a body that ends before its
// Content-Length fails while streaming — an OtaDownloadError with the
// status and the bytes received (:1052-1060) — and leaves no file.
func TestOTARequestorHTTPS_TruncatedBody(t *testing.T) {
	t.Parallel()
	var img []byte
	s := newOtaHTTPSServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(img)))
		_, _ = w.Write(img[:1000])
	})
	h := newHTTPSHarness(t, s, nil)
	img = h.image(2)
	h.r.performUpdateQuery(context.Background())
	h.failedDownload(t, http.StatusOK, 1000)
}

// TestOTARequestorHTTPS_SizeCap: a download past HTTPSMaxImageSize fails —
// at once when Content-Length states more, after the bytes up to the cap
// when the answer is chunked (BD-Matter-OTARequestor-HTTPS).
func TestOTARequestorHTTPS_SizeCap(t *testing.T) {
	t.Parallel()
	const limit = 2000
	for contentLength, bytes := range map[bool]uint64{true: 0, false: limit} {
		t.Run("Content-Length="+strconv.FormatBool(contentLength), func(t *testing.T) {
			t.Parallel()
			var img []byte
			s := newOtaHTTPSServer(t, func(w http.ResponseWriter, r *http.Request) { imageHandler(img, contentLength)(w, r) })
			h := newHTTPSHarness(t, s, func(c *OTARequestorConfig) { c.HTTPSMaxImageSize = limit })
			img = h.image(2)
			h.r.performUpdateQuery(context.Background())
			h.failedDownload(t, http.StatusOK, bytes)
		})
	}
}

// TestOTARequestorHTTPS_Stall: a download that receives nothing for
// HTTPSIdleTimeout on the requestor's clock fails — with the status and the
// bytes received once the answer began, with UpdateState Unknown's value
// while it waits for the response (BD-Matter-OTARequestor-HTTPS).
func TestOTARequestorHTTPS_Stall(t *testing.T) {
	t.Parallel()
	const timeout = 45 * time.Second
	// The answer states 100 bytes and sends 99: progress reads 98 % only
	// once all 99 arrived (:1066).
	const part = 99
	for _, began := range []bool{true, false} {
		t.Run("began="+strconv.FormatBool(began), func(t *testing.T) {
			t.Parallel()
			release := make(chan struct{})
			s := newOtaHTTPSServer(t, func(w http.ResponseWriter, r *http.Request) {
				if began {
					w.Header().Set("Content-Length", strconv.Itoa(part+1))
					_, _ = w.Write(make([]byte, part))
					w.(http.Flusher).Flush() //nolint:forcetypeassert // httptest's writer flushes
				}
				select {
				case <-r.Context().Done():
				case <-release:
				}
			})
			t.Cleanup(func() { close(release) }) // before the server's Close, which waits for the handler
			h := newHTTPSHarness(t, s, func(c *OTARequestorConfig) { c.HTTPSIdleTimeout = timeout })

			done := make(chan struct{})
			go func() {
				defer close(done)
				h.r.performUpdateQuery(context.Background())
			}()
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			ready := func() bool {
				if !slices.Contains(h.clock.pending(), timeout) {
					return false
				}
				if !began {
					return s.hits.Load() == 1
				}
				p, ok := h.progressValue()
				return ok && p == 98
			}
			for !ready() {
				if ctx.Err() != nil {
					t.Fatal("the download never reached the stalled point")
				}
				h.clock.Yield()
			}
			h.clock.advance(timeout)
			select {
			case <-done:
			case <-ctx.Done():
				t.Fatal("the stall did not end the download")
			}
			if began {
				h.failedDownload(t, http.StatusOK, part)
			} else {
				h.failedDownload(t, int64(otadef.UpdateStateUnknown), 0)
			}
		})
	}
}

// TestOTARequestorHTTPS_SlowButSteady: the idle timeout bounds no total
// time — a download that waits just under it for the response and again
// inside the body finishes after more than the timeout in all.
func TestOTARequestorHTTPS_SlowButSteady(t *testing.T) {
	t.Parallel()
	const idle = 45 * time.Second
	var img []byte
	first, next := make(chan struct{}), make(chan struct{})
	s := newOtaHTTPSServer(t, func(w http.ResponseWriter, r *http.Request) {
		for _, gate := range []chan struct{}{first, next} {
			select {
			case <-gate:
			case <-r.Context().Done():
				return
			}
			if gate == first {
				w.Header().Set("Content-Length", strconv.Itoa(len(img)))
				_, _ = w.Write(img[:len(img)/2])
				w.(http.Flusher).Flush() //nolint:forcetypeassert // httptest's writer flushes
			}
		}
		_, _ = w.Write(img[len(img)/2:])
	})
	h := newHTTPSHarness(t, s, func(c *OTARequestorConfig) { c.HTTPSIdleTimeout = idle })
	img = h.image(2)
	start := h.clock.Now()

	done := make(chan struct{})
	go func() {
		defer close(done)
		h.r.performUpdateQuery(context.Background())
	}()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	waitFor := func(what string, ok func() bool) {
		t.Helper()
		for !ok() || !slices.Contains(h.clock.pending(), idle) {
			if ctx.Err() != nil {
				t.Fatalf("never %s", what)
			}
			h.clock.Yield()
		}
	}
	waitFor("requested", func() bool { return s.hits.Load() == 1 })
	h.clock.advance(idle - time.Second)
	close(first)
	waitFor("half received", func() bool { p, ok := h.progressValue(); return ok && p == 49 })
	h.clock.advance(idle - time.Second)
	close(next)
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("the download did not finish")
	}
	if len(h.applier.applied) != 1 || len(h.downloadErrors()) != 0 {
		t.Fatalf("applied %v, DownloadError %+v", h.applier.applied, h.downloadErrors())
	}
	if took := h.clock.Now().Sub(start); took <= idle {
		t.Fatalf("took %v, not past the idle timeout", took)
	}
}

// TestOTARequestorHTTPS_Refused: an http:// URI is refused whatever is
// announced (:1105-1106), an https:// URI while HTTPS is not announced
// (:1095-1096): no request, no DownloadError (neither is an
// OtaDownloadError, :927), Idle with Failure (:930).
func TestOTARequestorHTTPS_Refused(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		scheme    string
		protocols []DownloadProtocol
	}{
		"http":              {"http", []DownloadProtocol{DownloadProtocolBDXSynchronous, DownloadProtocolHTTPS}},
		"https-unannounced": {"https", nil},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			s := newOtaHTTPSServer(t, http.NotFound)
			h := newOtaHarness(t, func(c *OTARequestorConfig) {
				c.TransferProtocols = tc.protocols
				c.HTTPClient = s.srv.Client()
			})
			uri := tc.scheme + "://" + s.srv.Listener.Addr().String() + "/image.ota"
			h.provider.answer = func(int) (provdef.QueryImageResponse, error) { return h.offering(uri, 2), nil }
			h.addActive(t)
			h.r.performUpdateQuery(context.Background())
			if want, got := []string{"1>2/1", "2>1/2"}, states(h.events.transitions()); !slices.Equal(got, want) {
				t.Fatalf("transitions %v, want %v", got, want)
			}
			if s.hits.Load() != 0 || len(h.downloadErrors()) != 0 {
				t.Fatalf("%d requests, DownloadError %+v", s.hits.Load(), h.downloadErrors())
			}
		})
	}
}

// TestOTARequestorHTTPS_FetchRejected: a GET that gets no response is no
// OtaDownloadError — fetch rejects, MatterError.accept rethrows (:925) to
// #queryForUpdate, which treats the provider as without update and returns
// to Idle with Success (:632-640); no DownloadError.
func TestOTARequestorHTTPS_FetchRejected(t *testing.T) {
	t.Parallel()
	s := newOtaHTTPSServer(t, http.NotFound)
	h := newHTTPSHarness(t, s, nil)
	s.srv.Close()
	h.r.performUpdateQuery(context.Background())
	if want, got := []string{"1>2/1", "2>4/1", "4>1/1"}, states(h.events.transitions()); !slices.Equal(got, want) {
		t.Fatalf("transitions %v, want %v", got, want)
	}
	if len(h.downloadErrors()) != 0 {
		t.Fatalf("DownloadError %+v", h.downloadErrors())
	}
	if a := h.r.active[0]; a.PreviousQueryTimestamp == nil {
		t.Fatal("provider not marked as without update")
	}
	h.noDownloadLeft(t)
}

// TestOTARequestorHTTPS_BDXOfferStillWorks: HTTPS announced or not, a
// bdx:// offer downloads over BDX (:1077-1093); the default announces BDX
// alone (:1372).
func TestOTARequestorHTTPS_BDXOfferStillWorks(t *testing.T) {
	t.Parallel()
	for name, protocols := range map[string][]DownloadProtocol{
		"default":    nil,
		"with-https": {DownloadProtocolBDXSynchronous, DownloadProtocolHTTPS},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			h := newOtaHarness(t, func(c *OTARequestorConfig) { c.TransferProtocols = protocols })
			h.addActive(t)
			h.r.performUpdateQuery(context.Background())
			want := protocols
			if want == nil {
				want = []DownloadProtocol{DownloadProtocolBDXSynchronous}
			}
			if got := h.provider.queries[0].ProtocolsSupported; !slices.Equal(got, want) {
				t.Fatalf("ProtocolsSupported %v, want %v", got, want)
			}
			if h.provider.transfers != 1 || len(h.applier.applied) != 1 {
				t.Fatalf("%d transfers, applied %v", h.provider.transfers, h.applier.applied)
			}
		})
	}

	// BDX not announced: a bdx:// offer is refused (:1078-1083). The
	// config refuses such a list, so the branch is reached by setting the
	// requestor's own copy.
	s := newOtaHTTPSServer(t, http.NotFound)
	h := newHTTPSHarness(t, s, nil)
	h.r.cfg.TransferProtocols = []DownloadProtocol{DownloadProtocolHTTPS}
	h.provider.answer = func(int) (provdef.QueryImageResponse, error) { return h.available(2), nil }
	h.r.performUpdateQuery(context.Background())
	if h.provider.transfers != 0 || len(h.applier.applied) != 0 {
		t.Fatalf("%d transfers, applied %v", h.provider.transfers, h.applier.applied)
	}
}

// TestOTARequestorHTTPS_Config: a protocol the requestor cannot download
// over is refused, as is a list without BDX synchronous, which matter.js's
// query always carries by default (:1367-1372); the list is the
// requestor's own copy.
func TestOTARequestorHTTPS_Config(t *testing.T) {
	t.Parallel()
	base := OTARequestorConfig{NodeInformation: func() OtaNodeInformation { return OtaNodeInformation{} }, DownloadDir: t.TempDir()}
	for _, p := range []DownloadProtocol{provdef.DownloadProtocolBdxAsynchronous, provdef.DownloadProtocolVendorSpecific} {
		cfg := base
		cfg.TransferProtocols = []DownloadProtocol{DownloadProtocolBDXSynchronous, p}
		if _, err := NewOTASoftwareUpdateRequestor(cfg); err == nil {
			t.Fatalf("protocol %d accepted", p)
		}
	}
	noBDX := base
	noBDX.TransferProtocols = []DownloadProtocol{DownloadProtocolHTTPS}
	if _, err := NewOTASoftwareUpdateRequestor(noBDX); err == nil {
		t.Fatal("a list without BDX synchronous accepted")
	}
	cfg := base
	list := []DownloadProtocol{DownloadProtocolBDXSynchronous, DownloadProtocolHTTPS}
	cfg.TransferProtocols = list
	r, err := NewOTASoftwareUpdateRequestor(cfg)
	if err != nil {
		t.Fatal(err)
	}
	list[1] = DownloadProtocolBDXSynchronous
	hc, ok := r.cfg.HTTPClient.(*http.Client)
	if r.cfg.TransferProtocols[1] != DownloadProtocolHTTPS || !ok || hc.Transport != nil || hc.CheckRedirect == nil ||
		r.cfg.HTTPSMaxImageSize != defaultOtaHTTPSMaxImageSize || r.cfg.HTTPSIdleTimeout != defaultOtaHTTPSIdleTimeout {
		t.Fatalf("config %+v", r.cfg)
	}
	if !errors.Is(&otaFetchRejected{cause: errOtaHTTPSStalled}, errOtaHTTPSStalled) {
		t.Fatal("otaFetchRejected does not unwrap")
	}

	// The module's redirect policy: https:// targets, ten at most.
	req := func(raw string) *http.Request {
		q, err := http.NewRequest(http.MethodGet, raw, http.NoBody)
		if err != nil {
			t.Fatal(err)
		}
		return q
	}
	if err := otaCheckRedirect(req("https://a.example/x"), make([]*http.Request, 9)); err != nil {
		t.Fatal(err)
	}
	if err := otaCheckRedirect(req("https://a.example/x"), make([]*http.Request, 10)); err == nil {
		t.Fatal("an eleventh redirect followed")
	}
	if err := otaCheckRedirect(req("http://a.example/x"), nil); !errors.Is(err, errOtaHTTPSRedirectScheme) {
		t.Fatalf("http:// target: %v", err)
	}
}

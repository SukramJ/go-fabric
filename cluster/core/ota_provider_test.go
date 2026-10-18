// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/bdx"
	"github.com/SukramJ/go-fabric/clock/clocktest"
	provdef "github.com/SukramJ/go-fabric/cluster/spec/otasoftwareupdateprovider"
	"github.com/SukramJ/go-fabric/im"
)

const (
	otaTestFabric   = uint8(1)
	otaTestPeerNode = uint64(0x00000000000ABCDE)
	otaTestOwnNode  = uint64(0x0000000000001234)
)

// otaTestSource offers one image to every query.
type otaTestSource struct {
	img   OTAImage
	none  bool
	bytes []byte
}

func (s *otaTestSource) QueryImage(context.Context, OTAPeer, provdef.QueryImageRequest) (OTAImage, bool, error) {
	return s.img, !s.none, nil
}

func (s *otaTestSource) Open(_ context.Context, file string) (bdx.Blob, error) {
	if file != string(s.img.FileDesignator) {
		return nil, fs.ErrNotExist
	}
	return bytes.NewReader(s.bytes), nil
}

type otaStatusLog struct {
	mu  sync.Mutex
	got []OTAUpdateStatus
}

func (l *otaStatusLog) add(_ OTAPeer, s OTAUpdateStatus, _ uint32) {
	l.mu.Lock()
	l.got = append(l.got, s)
	l.mu.Unlock()
}

func (l *otaStatusLog) states() []OTAUpdateStatus {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]OTAUpdateStatus(nil), l.got...)
}

type otaFixture struct {
	p      *OTAProvider
	src    *otaTestSource
	bdx    *bdx.Responder
	clock  *clocktest.Fake
	status *otaStatusLog
}

func newOTAFixture(t *testing.T, mutate func(*OTAProviderConfig)) *otaFixture {
	t.Helper()
	f := &otaFixture{
		src: &otaTestSource{
			img:   OTAImage{FileDesignator: bdx.FileDesignator("fff1-8001-v2.ota"), SoftwareVersion: 2, SoftwareVersionString: "2.0"},
			bytes: bytes.Repeat([]byte{0x42}, 3000),
		},
		bdx:    bdx.NewResponder(nil),
		clock:  clocktest.New(time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)),
		status: &otaStatusLog{},
	}
	cfg := OTAProviderConfig{
		ImageSource: f.src, BDX: f.bdx,
		NodeID: func(fabric uint8) (uint64, bool) { return otaTestOwnNode, fabric == otaTestFabric },
		Clock:  f.clock, OnStatusChange: f.status.add,
	}
	if mutate != nil {
		mutate(&cfg)
	}
	f.bdx.SetClock(f.clock) // the transfers' activity on the provider's clock, as a node wires it
	p, err := NewOTAProvider(cfg)
	if err != nil {
		t.Fatal(err)
	}
	f.p = p
	return f
}

// otaCtx is an invoke from the test requestor on sessionID.
func otaCtx(sessionID uint16) context.Context {
	ctx := im.WithFabricFilter(context.Background(), false, otaTestFabric)
	ctx = im.WithSubject(ctx, otaTestPeerNode, nil)
	return WithInvokeSessionID(ctx, sessionID)
}

var otaTestPeer = OTAPeer{FabricIndex: otaTestFabric, NodeID: otaTestPeerNode}

func bdxQuery() provdef.QueryImageRequest {
	return provdef.QueryImageRequest{
		VendorId: 0xFFF1, ProductId: 0x8001, SoftwareVersion: 1,
		ProtocolsSupported: []provdef.DownloadProtocolEnum{provdef.DownloadProtocolBdxSynchronous},
	}
}

func (f *otaFixture) query(t *testing.T, sessionID uint16, req provdef.QueryImageRequest) provdef.QueryImageResponse {
	t.Helper()
	resp, err := f.p.MatterInvoke(otaCtx(sessionID), provdef.CmdQueryImage, req)
	if err != nil {
		t.Fatalf("QueryImage: %v", err)
	}
	r, ok := resp.(provdef.QueryImageResponse)
	if !ok {
		t.Fatalf("QueryImage answered %T", resp)
	}
	return r
}

func (f *otaFixture) apply(t *testing.T, token []byte, version uint32) provdef.ApplyUpdateResponse {
	t.Helper()
	resp, err := f.p.MatterInvoke(otaCtx(7), provdef.CmdApplyUpdateRequest, provdef.ApplyUpdateRequestRequest{UpdateToken: token, NewVersion: version})
	if err != nil {
		t.Fatalf("ApplyUpdateRequest: %v", err)
	}
	return resp.(provdef.ApplyUpdateResponse)
}

func statusesEqual(a, b []OTAUpdateStatus) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestOTAProvider_NotAvailable: no image, NotAvailable alone (OP:173-178).
func TestOTAProvider_NotAvailable(t *testing.T) {
	t.Parallel()
	f := newOTAFixture(t, nil)
	f.src.none = true
	r := f.query(t, 7, bdxQuery())
	if r.Status != provdef.StatusNotAvailable || r.ImageUri != nil || r.UpdateToken != nil || r.DelayedActionTime != nil {
		t.Fatalf("response %+v", r)
	}
}

// TestOTAProvider_UpdateAvailable: the image over BDX — a 32-byte random
// token (OP:53, :240-241), the URI bdx://<own node id, 16 upper-case hex>/
// ota/<file> (OP:381, SoftwareUpdateManager.ts:454), the version, no
// consent asked; the peer is enabled for the "ota" scope; the update is
// Querying.
func TestOTAProvider_UpdateAvailable(t *testing.T) {
	t.Parallel()
	f := newOTAFixture(t, nil)
	r := f.query(t, 7, bdxQuery())
	if r.Status != provdef.StatusUpdateAvailable || r.UpdateToken == nil || len(*r.UpdateToken) != 32 {
		t.Fatalf("response %+v", r)
	}
	if want := "bdx://0000000000001234/ota/fff1-8001-v2.ota"; r.ImageUri == nil || *r.ImageUri != want {
		t.Fatalf("ImageUri %v, want %s", r.ImageUri, want)
	}
	if *r.SoftwareVersion != 2 || *r.SoftwareVersionString != "2.0" || r.UserConsentNeeded != nil || r.MetadataForRequestor != nil || r.DelayedActionTime != nil {
		t.Fatalf("response %+v", r)
	}
	if fd, node, err := bdx.ParseBdxURI(*r.ImageUri); err != nil || node != otaTestOwnNode || string(fd) != "ota/fff1-8001-v2.ota" {
		t.Fatalf("the requestor's parse: %q %x %v", fd, node, err)
	}
	if got := f.status.states(); !statusesEqual(got, []OTAUpdateStatus{OTAUpdateQuerying}) {
		t.Fatalf("states %v", got)
	}
	r2 := f.query(t, 7, bdxQuery())
	if r2.Status != provdef.StatusUpdateAvailable || bytes.Equal(*r2.UpdateToken, *r.UpdateToken) {
		t.Fatal("a second query without a running transfer is a fresh update (OP:194-196)")
	}
}

// otaPipe is an in-memory bdx.Exchange pair.
type otaPipe struct {
	in, out chan bdx.Message
}

func newOTAPipe() (requestor, provider *otaPipe) {
	a, b := make(chan bdx.Message, 64), make(chan bdx.Message, 64)
	return &otaPipe{in: a, out: b}, &otaPipe{in: b, out: a}
}

func (p *otaPipe) Send(_ context.Context, proto uint16, op uint8, payload []byte) error {
	p.out <- bdx.Message{ProtocolID: proto, Opcode: op, Payload: append([]byte(nil), payload...)}
	return nil
}

func (p *otaPipe) Receive(ctx context.Context) (bdx.Message, error) {
	select {
	case m := <-p.in:
		return m, nil
	case <-ctx.Done():
		return bdx.Message{}, ctx.Err()
	}
}
func (p *otaPipe) MaxPayloadSize() int { return 1178 }
func (p *otaPipe) Close() error        { return nil }

// startDownload opens the requestor's receiver-driven transfer of the
// image on session sessionID and stops after the ReceiveAccept; the
// returned messenger continues it. done closes when the responder ends.
func (f *otaFixture) startDownload(t *testing.T, sessionID uint16, uri string) (m *bdx.Messenger, done chan struct{}) {
	t.Helper()
	fd, _, err := bdx.ParseBdxURI(uri)
	if err != nil {
		t.Fatal(err)
	}
	req, resp := newOTAPipe()
	init, _ := bdx.Init{TransferControl: bdx.TransferControl{ReceiverDrive: true}, MaxBlockSize: 1024, FileDesignator: fd}.Encode()
	done = make(chan struct{})
	go func() {
		defer close(done)
		f.bdx.HandleExchange(context.Background(), bdx.Peer{FabricIndex: otaTestFabric, NodeID: otaTestPeerNode, SessionID: sessionID},
			bdx.Message{ProtocolID: bdx.ProtocolID, Opcode: uint8(bdx.MessageReceiveInit), Payload: init}, resp)
	}()
	m = bdx.NewMessenger(req, 5*time.Second)
	if _, _, err := m.Next(context.Background(), bdx.MessageReceiveAccept); err != nil {
		t.Fatalf("ReceiveAccept: %v", err)
	}
	return m, done
}

// TestOTAProvider_BusyWhileDownloading: a query while the peer's transfer
// runs on the same session is Busy with a two-minute retry (OP:226-236);
// the transfer moves the update to Downloading (OP:322-327).
func TestOTAProvider_BusyWhileDownloading(t *testing.T) {
	t.Parallel()
	f := newOTAFixture(t, nil)
	r := f.query(t, 7, bdxQuery())
	_, done := f.startDownload(t, 7, *r.ImageUri)
	busy := f.query(t, 7, bdxQuery())
	if busy.Status != provdef.StatusBusy || busy.DelayedActionTime == nil || *busy.DelayedActionTime != 120 || busy.ImageUri != nil {
		t.Fatalf("response %+v", busy)
	}
	if got := f.status.states(); !statusesEqual(got, []OTAUpdateStatus{OTAUpdateQuerying, OTAUpdateDownloading}) {
		t.Fatalf("states %v", got)
	}
	// A stale entry (older than 15 minutes, OtaProgress.ts:15) does not
	// block: the query proceeds although the transfer still runs.
	f.clock.Advance(16 * time.Minute)
	if r := f.query(t, 7, bdxQuery()); r.Status != provdef.StatusUpdateAvailable {
		t.Fatalf("after 16 minutes: %+v", r)
	}
	_ = f.bdx.DisablePeerForScope(bdx.Peer{FabricIndex: otaTestFabric, NodeID: otaTestPeerNode}, "ota", true)
	<-done
}

// TestOTAProvider_RebootDetected: a query on another session than the
// running transfer's, after its last activity, is a rebooted requestor —
// the old transfer is cancelled and the query answered afresh
// (OP:197-225).
func TestOTAProvider_RebootDetected(t *testing.T) {
	t.Parallel()
	f := newOTAFixture(t, nil)
	r := f.query(t, 7, bdxQuery())
	_, done := f.startDownload(t, 7, *r.ImageUri)
	f.clock.Advance(time.Second)
	r2 := f.query(t, 8, bdxQuery())
	if r2.Status != provdef.StatusUpdateAvailable {
		t.Fatalf("response %+v", r2)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the old transfer was not cancelled")
	}
}

// TestOTAProvider_DownloadToApply: the transfer moves the update to
// Downloading and WaitForApply; ApplyUpdateRequest answers Proceed at once
// (OP:443-459, applyDelay 0) — the image source stated the consent — and
// NotifyUpdateApplied ends it (OP:476).
func TestOTAProvider_DownloadToApply(t *testing.T) {
	t.Parallel()
	f := newOTAFixture(t, nil)
	r := f.query(t, 7, bdxQuery())
	m, done := f.startDownload(t, 7, *r.ImageUri)
	ctx := context.Background()
	var got []byte
	for c := uint32(0); ; c++ {
		_ = m.SendBlockQuery(ctx, c)
		typ, blk, err := m.ReadBlock(ctx)
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, blk.Data...)
		if typ == bdx.MessageBlockEOF {
			_ = m.SendBlockAckEOF(ctx, c)
			break
		}
	}
	<-done
	if !bytes.Equal(got, f.src.bytes) {
		t.Fatalf("served %d bytes", len(got))
	}
	a := f.apply(t, *r.UpdateToken, 2)
	if a.Action != provdef.ApplyUpdateActionProceed || a.DelayedActionTime != 0 {
		t.Fatalf("ApplyUpdateResponse %+v", a)
	}
	if _, err := f.p.MatterInvoke(otaCtx(7), provdef.CmdNotifyUpdateApplied, provdef.NotifyUpdateAppliedRequest{UpdateToken: *r.UpdateToken, SoftwareVersion: 2}); err != nil {
		t.Fatal(err)
	}
	want := []OTAUpdateStatus{OTAUpdateQuerying, OTAUpdateDownloading, OTAUpdateWaitForApply, OTAUpdateApplying, OTAUpdateDone}
	if got := f.status.states(); !statusesEqual(got, want) {
		t.Fatalf("states %v, want %v", got, want)
	}
	if f.p.detailsForPeer(otaTestPeer, nil) != nil {
		t.Fatal("Done left the entry behind (OP:614-618)")
	}
	// The consent went with the update (SoftwareUpdateManager.ts:1229-1232).
	if a := f.apply(t, *r.UpdateToken, 2); a.Action != provdef.ApplyUpdateActionDiscontinue {
		t.Fatalf("a second apply after Done: %+v", a)
	}
}

// fixedPolicy answers every apply the same.
type fixedPolicy struct {
	action provdef.ApplyUpdateActionEnum
	delay  time.Duration
}

func (p fixedPolicy) ApplyUpdate(context.Context, OTAPeer, provdef.ApplyUpdateRequestRequest) (provdef.ApplyUpdateActionEnum, time.Duration, error) {
	return p.action, p.delay, nil
}

// TestOTAProvider_DelayedActionTime: the apply delay goes out in whole
// seconds rounded up and at most a day (OP:441-446).
func TestOTAProvider_DelayedActionTime(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		delay time.Duration
		want  uint32
	}{{0, 0}, {1200 * time.Millisecond, 2}, {90 * time.Second, 90}, {48 * time.Hour, 86_400}} {
		f := newOTAFixture(t, func(c *OTAProviderConfig) { c.ApplyPolicy = fixedPolicy{provdef.ApplyUpdateActionProceed, tc.delay} })
		r := f.query(t, 7, bdxQuery())
		if a := f.apply(t, *r.UpdateToken, 2); a.Action != provdef.ApplyUpdateActionProceed || a.DelayedActionTime != tc.want {
			t.Errorf("delay %v: %+v, want %d", tc.delay, a, tc.want)
		}
	}
}

// otaConsentFunc answers the consent request.
type otaConsentFunc func() (OTAConsentState, time.Duration)

func (f otaConsentFunc) RequestConsent(context.Context, OTAPeer, provdef.QueryImageRequest, OTAImage) (OTAConsentState, time.Duration, error) {
	s, d := f()
	return s, d, nil
}

// TestOTAProvider_Consent: an image that needs a consent the requestor
// cannot give is asked for (OP:245-284): without a consent port (Unknown,
// OP:488) and when denied it is NotAvailable, while obtaining it is Busy
// after the port's delay or 120 s (OP:248, :269-273), granted it is
// offered with UserConsentNeeded (OP:399). A requestor that can consent
// gets it without asking.
func TestOTAProvider_Consent(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		consent OTAConsent
		can     bool
		status  provdef.StatusEnum
		delay   uint32
	}{
		{"no port", nil, false, provdef.StatusNotAvailable, 0},
		{"denied", otaConsentFunc(func() (OTAConsentState, time.Duration) { return OTAConsentDenied, 0 }), false, provdef.StatusNotAvailable, 0},
		{"obtaining, default delay", otaConsentFunc(func() (OTAConsentState, time.Duration) { return OTAConsentObtaining, 0 }), false, provdef.StatusBusy, 120},
		{"obtaining, own delay", otaConsentFunc(func() (OTAConsentState, time.Duration) { return OTAConsentObtaining, 30 * time.Second }), false, provdef.StatusBusy, 30},
		{"granted", otaConsentFunc(func() (OTAConsentState, time.Duration) { return OTAConsentGranted, 0 }), false, provdef.StatusUpdateAvailable, 0},
		{"requestor can consent", nil, true, provdef.StatusUpdateAvailable, 0},
	} {
		f := newOTAFixture(t, nil)
		f.p.cfg.Consent = tc.consent
		f.src.img.ConsentRequired = true
		req := bdxQuery()
		if tc.can {
			yes := true
			req.RequestorCanConsent = &yes
		}
		r := f.query(t, 7, req)
		if r.Status != tc.status {
			t.Errorf("%s: status %d, want %d", tc.name, r.Status, tc.status)
			continue
		}
		if tc.delay != 0 && (r.DelayedActionTime == nil || *r.DelayedActionTime != tc.delay) {
			t.Errorf("%s: delay %v, want %d", tc.name, r.DelayedActionTime, tc.delay)
		}
		if tc.status == provdef.StatusUpdateAvailable && (r.UserConsentNeeded == nil || !*r.UserConsentNeeded) {
			t.Errorf("%s: UserConsentNeeded not set", tc.name)
		}
		if tc.can {
			// The requestor consents itself, but the provider holds no
			// consent for the version: matter.js discontinues the apply
			// (OP:430-437).
			if a := f.apply(t, *r.UpdateToken, 2); a.Action != provdef.ApplyUpdateActionDiscontinue || a.DelayedActionTime != 120 {
				t.Errorf("%s: apply %+v, want Discontinue after 120 s", tc.name, a)
			}
		}
	}
}

// TestOTAProvider_DirectConsentApply: a consent granted for the update in
// progress allows its apply while no other state change reset it
// (OP:504-509).
func TestOTAProvider_DirectConsentApply(t *testing.T) {
	t.Parallel()
	f := newOTAFixture(t, nil)
	f.p.cfg.Consent = otaConsentFunc(func() (OTAConsentState, time.Duration) { return OTAConsentGranted, 0 })
	f.src.img.ConsentRequired = true
	r := f.query(t, 7, bdxQuery())
	if a := f.apply(t, *r.UpdateToken, 2); a.Action != provdef.ApplyUpdateActionProceed {
		t.Fatalf("apply %+v", a)
	}
}

// TestOTAProvider_Protocols: HTTPS when the requestor supports only that
// and the image has a URI (OP:382-386), else DownloadProtocolNotSupported
// (OP:387-391).
func TestOTAProvider_Protocols(t *testing.T) {
	t.Parallel()
	f := newOTAFixture(t, nil)
	req := bdxQuery()
	req.ProtocolsSupported = []provdef.DownloadProtocolEnum{provdef.DownloadProtocolHttps}
	if r := f.query(t, 7, req); r.Status != provdef.StatusDownloadProtocolNotSupported {
		t.Fatalf("no URI: %+v", r)
	}
	f.src.img.HTTPSURI = "https://example.invalid/image.ota"
	if r := f.query(t, 7, req); r.Status != provdef.StatusUpdateAvailable || *r.ImageUri != f.src.img.HTTPSURI {
		t.Fatalf("HTTPS: %+v", r)
	}
}

// TestOTAProvider_BDXNotEnabled: BDX already enabled for the peer with
// another configuration while a transfer runs is Busy for five minutes
// (OP:292-305).
func TestOTAProvider_BDXNotEnabled(t *testing.T) {
	t.Parallel()
	f := newOTAFixture(t, nil)
	r := f.query(t, 7, bdxQuery())
	_, done := f.startDownload(t, 7, *r.ImageUri)
	f.p.cfg.BDXMaxBlockSize = 100
	f.p.removeInProgress(otaTestPeer) // past the in-progress check
	busy := f.query(t, 7, bdxQuery())
	if busy.Status != provdef.StatusBusy || *busy.DelayedActionTime != 300 {
		t.Fatalf("response %+v", busy)
	}
	_ = f.bdx.DisablePeerForScope(bdx.Peer{FabricIndex: otaTestFabric, NodeID: otaTestPeerNode}, "ota", true)
	<-done
}

// TestOTAProvider_NeedsACASEPeer: the commands need a remote actor on a
// node session (OP:168-170).
func TestOTAProvider_NeedsACASEPeer(t *testing.T) {
	t.Parallel()
	f := newOTAFixture(t, nil)
	_, err := f.p.MatterInvoke(context.Background(), provdef.CmdQueryImage, bdxQuery())
	var se interface{ MatterStatusCode() im.StatusCode }
	if !errors.As(err, &se) || se.MatterStatusCode() != im.StatusUnsupportedAccess {
		t.Fatalf("err = %v", err)
	}
	if _, err := NewOTAProvider(OTAProviderConfig{}); !errors.Is(err, ErrNoImageSource) {
		t.Fatalf("no image source: %v", err)
	}
	f.src.img.FileDesignator = bdx.FileDesignator("ota/x")
	if _, err := f.p.MatterInvoke(otaCtx(7), provdef.CmdQueryImage, bdxQuery()); err == nil || !strings.Contains(err.Error(), "plain file name") {
		t.Fatalf("designator with a slash: %v", err)
	}
}

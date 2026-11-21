// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"runtime"
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

// attempts is the number of update attempts the fixture's default manager
// runs.
func (f *otaFixture) attempts() int {
	m := f.p.mgr.(*otaStandaloneManager)
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.set.All())
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
	// The responder stamps the transfer's last activity when it records
	// the negotiation, after it sent the ReceiveAccept read above
	// (serveSender in bdx/serve.go); a test that moves the clock next
	// would otherwise race the stamp and find the transfer active at the
	// new time (review II, F-db021da0-37).
	peer := bdx.Peer{FabricIndex: otaTestFabric, NodeID: otaTestPeerNode}
	deadline := time.Now().Add(5 * time.Second)
	for {
		if s := f.bdx.SessionFor(peer, otaScope); s != nil {
			if _, ok := s.Negotiated(); ok {
				return m, done
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("the transfer was accepted and its negotiation never recorded")
		}
		runtime.Gosched()
	}
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
	if f.p.mgr.AttemptFor(otaTestPeer, *r.UpdateToken) != nil {
		t.Fatal("Done left the attempt running (SoftwareUpdateManager.ts:1978-1979 at b25ed960)")
	}
	// The token's attempt ended, so a second apply under it is an apply
	// under an unknown token, allowed for a version above 0
	// (SoftwareUpdateManager.ts:1912-1915 at b25ed960).
	if a := f.apply(t, *r.UpdateToken, 2); a.Action != provdef.ApplyUpdateActionProceed {
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
// offered. A requestor that can consent gets it without asking, with
// UserConsentNeeded (OP:312 at b25ed960: only a delegated consent).
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
		if needed := r.UserConsentNeeded != nil && *r.UserConsentNeeded; tc.status == provdef.StatusUpdateAvailable && needed != tc.can {
			t.Errorf("%s: UserConsentNeeded %v, want %v", tc.name, needed, tc.can)
		}
		if tc.can {
			// The requestor obtains the consent itself: the offer allows
			// the apply (OtaUpdateAttempts.ts:81 at b25ed960).
			if a := f.apply(t, *r.UpdateToken, 2); a.Action != provdef.ApplyUpdateActionProceed {
				t.Errorf("%s: apply %+v, want Proceed", tc.name, a)
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
	m := f.p.mgr.(*otaStandaloneManager)
	m.mu.Lock()
	m.maxBlock = 100
	for _, a := range m.set.All() {
		m.set.EndAttempt(a) // past the admission
	}
	m.mu.Unlock()
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

// TestOTAProvider_ApplyWhileTheTransferCloses: the requestor's
// ApplyUpdateRequest arrives while the responder still closes the transfer
// that just ended — the invoke runs on its exchange goroutine, the
// session's OnClosed listener on the responder's. The listener's decision
// reads the update's state under the provider's lock; before it did, the
// race detector flagged the read against ApplyUpdateRequest's write
// (TestReferenceControllerOTA, CI race leg 2026-11-05…07). The interleaving
// is the scheduler's, so the test repeats the window; under -race one
// unsynchronised read is enough to fail it.
func TestOTAProvider_ApplyWhileTheTransferCloses(t *testing.T) {
	t.Parallel()
	for range 40 {
		f := newOTAFixture(t, nil)
		r := f.query(t, 7, bdxQuery())
		m, done := f.startDownload(t, 7, *r.ImageUri)
		ctx := context.Background()
		for c := uint32(0); ; c++ {
			_ = m.SendBlockQuery(ctx, c)
			typ, _, err := m.ReadBlock(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if typ == bdx.MessageBlockEOF {
				_ = m.SendBlockAckEOF(ctx, c)
				break
			}
		}
		// No wait for done: the apply races the responder's close.
		if a := f.apply(t, *r.UpdateToken, 2); a.Action != provdef.ApplyUpdateActionProceed {
			t.Fatalf("ApplyUpdateResponse %+v", a)
		}
		<-done
	}
}

// TestOTAProvider_ClosedTransferCancelsOnlyUnsettled: the transfer
// session's closed listener (OP:284-293 at b25ed960) cancels an attempt
// that is querying or downloading and leaves one that waits for its apply
// or applies; the manager decides and writes under one hold of its lock
// (ReportAttemptStatus's unless), so an apply between the two is not
// overwritten.
func TestOTAProvider_ClosedTransferCancelsOnlyUnsettled(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		state OTAUpdateStatus
		want  OTAUpdateStatus
	}{
		{OTAUpdateQuerying, OTAUpdateCancelled},
		{OTAUpdateDownloading, OTAUpdateCancelled},
		{OTAUpdateWaitForApply, OTAUpdateWaitForApply},
		{OTAUpdateApplying, OTAUpdateApplying},
	} {
		f := newOTAFixture(t, nil)
		a := f.p.mgr.BeginQuery(otaTestPeer)
		f.p.mgr.OfferUpdate(a, []byte{1, 2, 3}, 2)
		f.p.mgr.ReportAttemptStatus(a, tc.state)
		f.p.mgr.ReportAttemptStatus(a, OTAUpdateCancelled, OTAUpdateWaitForApply, OTAUpdateApplying, OTAUpdateDone, OTAUpdateCancelled)
		if got := a.Status(); got != tc.want {
			t.Errorf("closed in %v: %v, want %v", tc.state, got, tc.want)
		}
	}
}

// TestOTAProvider_RebootJudgedBySessionActivity: with the sessions'
// activity at hand ([OTAProviderConfig].SessionActivity), a query on
// another session cancels the running transfer only when the transfer's
// session was last active before the query's — matter.js compares the two
// sessions' activeTimestamp (OtaSoftwareUpdateProviderServer.ts:197-201
// at db021da0; review II, F-db021da0-27). A transfer whose session heard
// from the peer after the query's session did is not a reboot: Busy.
func TestOTAProvider_RebootJudgedBySessionActivity(t *testing.T) {
	t.Parallel()
	base := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	activity := map[uint16]time.Time{7: base.Add(2 * time.Second), 8: base.Add(time.Second)}
	f := newOTAFixture(t, func(cfg *OTAProviderConfig) {
		cfg.SessionActivity = func(id uint16) (time.Time, bool) {
			at, ok := activity[id]
			return at, ok
		}
	})
	r := f.query(t, 7, bdxQuery())
	_, done := f.startDownload(t, 7, *r.ImageUri)
	f.clock.Advance(time.Minute)
	// The transfer's session (7) heard from the peer after the query's
	// session (8): no reboot, although the transfer's last block is older
	// than now.
	if busy := f.query(t, 8, bdxQuery()); busy.Status != provdef.StatusBusy {
		t.Fatalf("query on a session older than the transfer's: %+v, want Busy", busy)
	}
	// The query's session is the newer one: the requestor rebooted.
	activity[8] = base.Add(3 * time.Second)
	if r2 := f.query(t, 8, bdxQuery()); r2.Status != provdef.StatusUpdateAvailable {
		t.Fatalf("query on the newer session: %+v", r2)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("the old transfer was not cancelled")
	}
}

// TestOTAStandaloneManagerUnderItsLock (decision 292 (b)): the default
// manager changes the attempts only under its lock — the provider's
// queries, applies and notifications and a consent removal race here, and
// -race reports a path that does not; the outcome is not asserted.
func TestOTAStandaloneManagerUnderItsLock(t *testing.T) {
	t.Parallel()
	f := newOTAFixture(t, nil)
	var wg sync.WaitGroup
	for g := range 4 {
		wg.Go(func() {
			for i := range 25 {
				r := f.query(t, uint16(10+g), bdxQuery())
				if r.UpdateToken == nil {
					continue
				}
				switch (g + i) % 3 {
				case 0:
					f.apply(t, *r.UpdateToken, 2)
				case 1:
					f.removeConsent(0, true)
				default:
					_, _ = f.p.MatterInvoke(otaCtx(7), provdef.CmdNotifyUpdateApplied, provdef.NotifyUpdateAppliedRequest{UpdateToken: *r.UpdateToken, SoftwareVersion: 2})
				}
				_ = f.p.MatterHeldFabrics()
			}
		})
	}
	wg.Wait()
}

// TestOTAUpdateAttemptZeroValue (review L5): an attempt or a set a host
// builds as a zero value works on the system clock instead of panicking.
func TestOTAUpdateAttemptZeroValue(t *testing.T) {
	t.Parallel()
	var a OTAUpdateAttempt
	a.Touch()
	if a.IsStale(a.LastActivity()) || a.Ended() {
		t.Fatalf("zero attempt stale %v ended %v", a.IsStale(a.LastActivity()), a.Ended())
	}
	var s OTAUpdateAttempts
	b := s.Begin(OTAPeer{FabricIndex: 1, NodeID: 2})
	if err := s.MakeOffer(b, []byte{1}, 2); err != nil {
		t.Fatal(err)
	}
	if err := s.MakeOffer(b, []byte{1}, 3); err == nil {
		t.Error("a second offer was accepted")
	}
	ended := false
	b.OnEnd(func() { ended = true })
	s.End(b)
	if !ended || len(s.All()) != 0 {
		t.Errorf("ended %v, %d attempts left", ended, len(s.All()))
	}
}

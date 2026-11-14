// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock"
	"github.com/SukramJ/go-fabric/clock/clocktest"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/controller/ble"
	"github.com/SukramJ/go-fabric/controller/ble/bletest"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/transport/btp"
)

// bleAdvert is the harness's basic commissioning window.
var bleWindow = CommissioningAdvertisement{Discriminator: 3840, VendorID: 0xFFF1, ProductID: 0x8001, CommissioningMode: 1}

// newBLEHarness is the connection harness with an in-memory BLE radio as
// Config.BLE, on clk (nil: the system clock), on the radio for bleWindow.
func newBLEHarness(t *testing.T, clk clock.Clock) (*tcpHarness, *bletest.Peripheral) {
	t.Helper()
	p := &bletest.Peripheral{Address: "C0:FF:EE:00:00:01", Discriminator: 3840, VendorID: 0xFFF1, ProductID: 0x8001}
	h := newConnHarness(t, Config{Listen: "127.0.0.1:0", BLE: p.Radio(), Clock: clk})
	if err := h.bridge.StartBLE(context.Background(), bleWindow); err != nil {
		t.Fatalf("StartBLE: %v", err)
	}
	waitFor(t, "the GATT service served", func() bool { s, _ := p.Radio().Serving(); return s })
	return h, p
}

// bleCentral is a central's BTP session to the harness's radio, driven by
// hand: the handshake request on C1, the response as the first C2
// indication (BtpSessionHandler.createAsCentral's side of
// BlenoBleServer.ts:288-323).
type bleCentral struct {
	conn    ble.Connection
	s       *btp.Session
	replies chan []byte
}

func dialRadio(t *testing.T, p *bletest.Peripheral) *bleCentral {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, err := bletest.NewCentral(p).Connect(ctx, ble.Peripheral{Address: p.Address})
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	c := &bleCentral{conn: conn, replies: make(chan []byte, 16)}
	var mu sync.Mutex
	var sess *btp.Session
	resp := make(chan []byte, 1)
	if err := conn.WriteC1(ctx, btp.EncodeHandshakeRequest(btp.HandshakeRequest{Versions: btp.SupportedVersions, ATTMTU: 244, ClientWindowSize: 255})); err != nil {
		t.Fatal(err)
	}
	if err := conn.SubscribeC2(ctx, func(v []byte) {
		mu.Lock()
		s := sess
		mu.Unlock()
		if s == nil {
			resp <- v
			return
		}
		_ = s.HandleIncoming(v)
	}); err != nil {
		t.Fatal(err)
	}
	var r []byte
	select {
	case r = <-resp:
	case <-ctx.Done():
		t.Fatal("no handshake response")
	}
	s, err := btp.NewCentral(r, btp.Callbacks{
		Write:      func(b []byte) error { return conn.WriteC1(context.Background(), b) },
		Disconnect: conn.Disconnect,
		Message:    func(m []byte) error { c.replies <- m; return nil },
	}, 244, btp.Options{})
	if err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	sess = s
	mu.Unlock()
	c.s = s
	t.Cleanup(func() { _ = conn.Disconnect() })
	return c
}

func (c *bleCentral) disconnected() bool {
	select {
	case <-c.conn.Disconnected():
		return true
	default:
		return false
	}
}

// TestBLERequestRepliedWithoutMRP: a request over the BTP session of the
// bridge's radio is answered on it without MRP — the session is reliable
// (matter.js Ble.ts:38, Session.ts:368-370) — and the session is bound
// to the connection.
func TestBLERequestRepliedWithoutMRP(t *testing.T) {
	h, p := newBLEHarness(t, nil)
	c := dialRadio(t, p)
	if err := c.s.Send(h.sealedInvoke(0x310)); err != nil {
		t.Fatal(err)
	}
	proto := h.open(waitReply(t, c.replies))
	if proto.Opcode != im.OpcodeInvokeResponse || proto.ExchangeID != 0x310 {
		t.Fatalf("reply opcode=0x%02X exchange=0x%X", proto.Opcode, proto.ExchangeID)
	}
	if proto.NeedsAck || proto.HasAck {
		t.Fatalf("reply over BLE carries MRP flags: R=%v A=%v", proto.NeedsAck, proto.HasAck)
	}
	if n := h.ack.Pending(); n != 0 {
		t.Fatalf("%d acknowledgement obligations registered for a message over BLE", n)
	}
	raw, ok := h.bridge.sessionPeerAddrs.Load(harnessLocalSessionID)
	if !ok {
		t.Fatal("session not bound to the connection")
	}
	r, isConn := raw.(*connRoute)
	if !isConn || !r.conn.Reliable() || r.conn.Remote().Network() != "ble" {
		t.Fatalf("session bound to %v, want the BLE connection", raw)
	}

	// The last session's close leaves a BLE connection standing: matter.js
	// closes only a TCP channel then (ExchangeManager.ts:770-775).
	// The close of the last session's route is synchronous
	// (unbindSessionRoute → closeConnRoute).
	h.bridge.SessionClosed(harnessLocalSessionID)
	if c.disconnected() {
		t.Fatal("the BLE connection closed with its last session")
	}
}

// TestBLEDisconnectEvictsSessions: a lost BLE connection closes every
// session bound to it (matter.js Ble.ts:17-22, BleChannelClosedError), and
// the next central is served.
func TestBLEDisconnectEvictsSessions(t *testing.T) {
	h, p := newBLEHarness(t, nil)
	c := dialRadio(t, p)
	if err := c.s.Send(h.sealedInvoke(0x311)); err != nil {
		t.Fatal(err)
	}
	waitReply(t, c.replies)
	_ = c.conn.Disconnect()
	waitFor(t, "the registry closing the bound session", func() bool {
		h.registry.mu.Lock()
		defer h.registry.mu.Unlock()
		return slices.Contains(h.registry.closed, harnessLocalSessionID)
	})
	if _, ok := h.bridge.sessionPeerAddrs.Load(harnessLocalSessionID); ok {
		t.Fatal("evicted session still has a route")
	}
	waitFor(t, "the radio free for the next central", func() bool { _, n := p.Radio().Serving(); return n == 0 })
	next := dialRadio(t, p)
	if next.disconnected() {
		t.Fatal("the next central was refused")
	}
}

// TestBLEOneCentralAtATime: a second central that connects while one is
// served is disconnected (matter.js's peripheral serves a single
// connection, BlenoBleServer.ts:129-133); the first stays.
func TestBLEOneCentralAtATime(t *testing.T) {
	h, p := newBLEHarness(t, nil)
	first := dialRadio(t, p)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	second, err := bletest.NewCentral(p).Connect(ctx, ble.Peripheral{Address: p.Address})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-second.Disconnected():
	case <-ctx.Done():
		t.Fatal("the second central was not disconnected")
	}
	if first.disconnected() {
		t.Fatal("the first central was disconnected")
	}
	// The first central's session still carries a request and its reply.
	if err := first.s.Send(h.sealedInvoke(0x312)); err != nil {
		t.Fatal(err)
	}
	if proto := h.open(waitReply(t, first.replies)); proto.ExchangeID != 0x312 {
		t.Fatalf("reply on exchange 0x%X", proto.ExchangeID)
	}
}

// TestStopBLE: StopBLE stops the advertisement, disconnects the central
// and unregisters the GATT service (matter.js endUncommissionedMode,
// ServerNetworkRuntime.ts:294-308); a second StopBLE does nothing.
func TestStopBLE(t *testing.T) {
	h, p := newBLEHarness(t, nil)
	c := dialRadio(t, p)
	if err := h.bridge.StopBLE(context.Background()); err != nil {
		t.Fatal(err)
	}
	if serving, _ := p.Radio().Serving(); serving {
		t.Fatal("still serving after StopBLE")
	}
	if _, advertising := p.Radio().Advertising(); advertising {
		t.Fatal("still advertising after StopBLE")
	}
	if !c.disconnected() {
		t.Fatal("the central is still connected after StopBLE")
	}
	if err := h.bridge.StopBLE(context.Background()); err != nil {
		t.Fatal(err)
	}
	// Start again: served and advertised anew.
	if err := h.bridge.StartBLE(context.Background(), bleWindow); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "served again", func() bool { s, _ := p.Radio().Serving(); return s })
	waitFor(t, "advertised again", func() bool { _, a := p.Radio().Advertising(); return a })
}

// TestStartBLERefusals: no radio configured, a window that is not basic
// (BLE announces CM=1 only, BleAdvertiser.ts:37-45), a bridge not started.
func TestStartBLERefusals(t *testing.T) {
	h := newConnHarness(t, Config{Listen: "127.0.0.1:0"})
	if err := h.bridge.StartBLE(context.Background(), bleWindow); !errors.Is(err, errNoBLE) {
		t.Fatalf("without Config.BLE: %v", err)
	}
	if err := h.bridge.StopBLE(context.Background()); err != nil {
		t.Fatalf("StopBLE without BLE: %v", err)
	}
	p := &bletest.Peripheral{Address: "C0:FF:EE:00:00:02"}
	b, err := New(wbEmptySnapshotter, newFakeAdvertiser(), Config{Listen: "127.0.0.1:0", BLE: p.Radio()}, nil)
	if err != nil {
		t.Fatal(err)
	}
	enhanced := bleWindow
	enhanced.CommissioningMode = 2
	if err := b.StartBLE(context.Background(), enhanced); err == nil {
		t.Fatal("an enhanced window was advertised over BLE")
	}
	if err := b.StartBLE(context.Background(), bleWindow); !errors.Is(err, ErrNotStarted) {
		t.Fatalf("before Start: %v", err)
	}
	if serving, _ := p.Radio().Serving(); serving {
		t.Fatal("a refused StartBLE touched the radio")
	}
}

// waitArmed waits until clk has a timer of exactly d armed.
func waitArmed(t *testing.T, clk *clocktest.Fake, d time.Duration) {
	t.Helper()
	waitFor(t, "a timer of "+d.String(), func() bool { return slices.Contains(clk.Armed(), d) })
}

func waitAds(t *testing.T, r *bletest.Radio, n int) []contract.BLEAdvertisement {
	t.Helper()
	var ads []contract.BLEAdvertisement
	waitFor(t, "the advertisement", func() bool { ads, _ = r.Advertisements(); return len(ads) >= n })
	if len(ads) != n {
		t.Fatalf("%d advertisements, want %d", len(ads), n)
	}
	return ads
}

// TestBLEAdvertisementScheduleParityMatterJS: the payload at 20 ms for
// 30 s, at 150 ms until 15 minutes, then the extended announcement — no
// vendor and product id, the extended flag — at 1285 ms, until the 2-day
// timeout, after which the advertisement stops (matter.js
// BleAdvertisement.ts:13-85, BleAdvertiser.ts:100-108,
// CommissioningConstants.ts:10-11).
func TestBLEAdvertisementScheduleParityMatterJS(t *testing.T) {
	clk := clocktest.New(time.Unix(1_800_000_000, 0))
	h, p := newBLEHarness(t, clk)
	r := p.Radio()
	normal := btp.EncodeAdvertisementData(3840, 0xFFF1, 0x8001, false, false)
	ads := waitAds(t, r, 1)
	if !bytes.Equal(ads[0].Data, normal) || ads[0].Interval != 20*time.Millisecond {
		t.Fatalf("early: %x at %v", ads[0].Data, ads[0].Interval)
	}
	waitArmed(t, clk, 30*time.Second)
	clk.Advance(30 * time.Second)
	ads = waitAds(t, r, 2)
	if !bytes.Equal(ads[1].Data, normal) || ads[1].Interval != 150*time.Millisecond {
		t.Fatalf("late: %x at %v", ads[1].Data, ads[1].Interval)
	}
	waitArmed(t, clk, 15*time.Minute-30*time.Second)
	clk.Advance(15*time.Minute - 30*time.Second)
	ads = waitAds(t, r, 3)
	extended := btp.EncodeAdvertisementData(3840, 0, 0, false, true)
	if !bytes.Equal(ads[2].Data, extended) || ads[2].Interval != 1285*time.Millisecond {
		t.Fatalf("extended: %x at %v", ads[2].Data, ads[2].Interval)
	}
	if ext := extended[len(extended)-1]; ext != 0x02 {
		t.Fatalf("extended flags byte %#x", ext)
	}
	// A StartBLE for the same window keeps the schedule running.
	if err := h.bridge.StartBLE(context.Background(), bleWindow); err != nil {
		t.Fatal(err)
	}
	waitArmed(t, clk, 48*time.Hour-15*time.Minute)
	if ads, _ := r.Advertisements(); len(ads) != 3 {
		t.Fatalf("a repeated StartBLE restarted the schedule: %d advertisements", len(ads))
	}
	clk.Advance(48*time.Hour - 15*time.Minute)
	waitFor(t, "the advertisement stopped at the timeout", func() bool {
		_, advertising := r.Advertising()
		return !advertising
	})
	if _, stops := r.Advertisements(); stops != 1 {
		t.Fatalf("%d stops, want 1", stops)
	}
}

// TestStartBLENewWindowRestartsSchedule: a window under another
// discriminator replaces the advertisement and restarts its schedule.
func TestStartBLENewWindowRestartsSchedule(t *testing.T) {
	clk := clocktest.New(time.Unix(1_800_000_000, 0))
	h, p := newBLEHarness(t, clk)
	r := p.Radio()
	waitAds(t, r, 1)
	waitArmed(t, clk, 30*time.Second)
	other := bleWindow
	other.Discriminator = 1234
	if err := h.bridge.StartBLE(context.Background(), other); err != nil {
		t.Fatal(err)
	}
	ads := waitAds(t, r, 2)
	if !bytes.Equal(ads[1].Data, btp.EncodeAdvertisementData(1234, 0xFFF1, 0x8001, false, false)) || ads[1].Interval != 20*time.Millisecond {
		t.Fatalf("restarted: %x at %v", ads[1].Data, ads[1].Interval)
	}
}

// TestWithdrawBLE: the window's end stops only the advertisement
// (BleAdvertisement.ts:69-71); the service and the central stay, and the
// next StartBLE advertises again on the same service.
func TestWithdrawBLE(t *testing.T) {
	h, p := newBLEHarness(t, nil)
	c := dialRadio(t, p)
	// A secure session bound to the connection: the central is
	// commissioning, so the window's end leaves it connected.
	if err := c.s.Send(h.sealedInvoke(0x312)); err != nil {
		t.Fatal(err)
	}
	waitReply(t, c.replies)
	if err := h.bridge.WithdrawBLE(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, advertising := p.Radio().Advertising(); advertising {
		t.Fatal("still advertising after WithdrawBLE")
	}
	if serving, n := p.Radio().Serving(); !serving || n != 1 || c.disconnected() {
		t.Fatalf("serving %v with %d centrals after WithdrawBLE", serving, n)
	}
	if err := h.bridge.WithdrawBLE(context.Background()); err != nil {
		t.Fatalf("a second WithdrawBLE: %v", err)
	}
	if err := h.bridge.StartBLE(context.Background(), bleWindow); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "advertised again", func() bool { _, a := p.Radio().Advertising(); return a })
	if c.disconnected() {
		t.Fatal("the central was dropped by the second StartBLE")
	}
	if err := c.s.Send(h.sealedInvoke(0x313)); err != nil {
		t.Fatal(err)
	}
	waitReply(t, c.replies)
}

// hookedRadio is a radio whose Serve, once its own returned, waits for
// release and then runs after.
type hookedRadio struct {
	*bletest.Radio
	release chan struct{}
	after   func()
}

func (r *hookedRadio) Serve(ctx context.Context, c3 []byte, accept func(contract.BLEConnection)) error {
	err := r.Radio.Serve(ctx, c3, accept)
	if r.release != nil {
		<-r.release
	}
	if r.after != nil {
		r.after()
	}
	return err
}

// TestStartBLERefusedOnceStopping: a StartBLE that runs while Stop takes
// the bridge off the radio is refused, so nothing it would start outlives
// Stop.
func TestStartBLERefusedOnceStopping(t *testing.T) {
	p := &bletest.Peripheral{Address: "C0:FF:EE:00:00:03"}
	hr := &hookedRadio{Radio: p.Radio()}
	h := newConnHarness(t, Config{Listen: "127.0.0.1:0", BLE: hr})
	var during error
	hr.after = func() { during = h.bridge.StartBLE(context.Background(), bleWindow) }
	if err := h.bridge.StartBLE(context.Background(), bleWindow); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "served", func() bool { s, _ := p.Radio().Serving(); return s })
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := h.bridge.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(during, ErrNotStarted) {
		t.Fatalf("StartBLE during Stop: %v, want ErrNotStarted", during)
	}
	if serving, _ := p.Radio().Serving(); serving {
		t.Fatal("serving after Stop")
	}
	if _, advertising := p.Radio().Advertising(); advertising {
		t.Fatal("advertising after Stop")
	}
}

// TestStartBLEWaitsForALateServe: a StopBLE that gave up waiting leaves the
// old Serve running; the next StartBLE waits for it and registers the
// service again instead of running into the old one.
func TestStartBLEWaitsForALateServe(t *testing.T) {
	p := &bletest.Peripheral{Address: "C0:FF:EE:00:00:04"}
	hr := &hookedRadio{Radio: p.Radio(), release: make(chan struct{})}
	h := newConnHarness(t, Config{Listen: "127.0.0.1:0", BLE: hr})
	if err := h.bridge.StartBLE(context.Background(), bleWindow); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "served", func() bool { s, _ := p.Radio().Serving(); return s })
	short, cancel := context.WithCancel(context.Background())
	cancel()
	if err := h.bridge.StopBLE(short); !errors.Is(err, context.Canceled) {
		t.Fatalf("StopBLE with an ended context: %v", err)
	}
	started := make(chan error, 1)
	go func() { started <- h.bridge.StartBLE(context.Background(), bleWindow) }()
	select {
	case err := <-started:
		t.Fatalf("StartBLE returned (%v) while the old Serve still ran", err)
	default:
	}
	close(hr.release)
	if err := <-started; err != nil {
		t.Fatal(err)
	}
	waitFor(t, "served again", func() bool { s, _ := p.Radio().Serving(); return s })
	waitFor(t, "advertised again", func() bool { _, a := p.Radio().Advertising(); return a })
}

// TestStopBLEBeforeSubscription: StopBLE between the central's handshake
// request and its subscription drops the central and unregisters the
// service; the subscription that follows fails.
func TestStopBLEBeforeSubscription(t *testing.T) {
	h, p := newBLEHarness(t, nil)
	conn, err := bletest.NewCentral(p).Connect(context.Background(), ble.Peripheral{Address: p.Address})
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.WriteC1(context.Background(), btp.EncodeHandshakeRequest(btp.HandshakeRequest{Versions: btp.SupportedVersions, ATTMTU: 244, ClientWindowSize: 255})); err != nil {
		t.Fatal(err)
	}
	if err := h.bridge.StopBLE(context.Background()); err != nil {
		t.Fatal(err)
	}
	<-conn.Disconnected()
	if err := conn.SubscribeC2(context.Background(), func([]byte) {}); err == nil {
		t.Fatal("a subscription after StopBLE succeeded")
	}
	if serving, _ := p.Radio().Serving(); serving {
		t.Fatal("serving after StopBLE")
	}
}

// TestStopBLEWithIndicationPending: StopBLE returns while an indication
// waits for a central that does not confirm it, and the central is gone.
func TestStopBLEWithIndicationPending(t *testing.T) {
	h, p := newBLEHarness(t, nil)
	conn, err := bletest.NewCentral(p).Connect(context.Background(), ble.Peripheral{Address: p.Address})
	if err != nil {
		t.Fatal(err)
	}
	stuck := make(chan struct{})
	got := make(chan struct{}, 1)
	if err := conn.WriteC1(context.Background(), btp.EncodeHandshakeRequest(btp.HandshakeRequest{Versions: btp.SupportedVersions, ATTMTU: 244, ClientWindowSize: 255})); err != nil {
		t.Fatal(err)
	}
	// The central receives the handshake response and does not return:
	// the indication stays unconfirmed.
	if err := conn.SubscribeC2(context.Background(), func([]byte) { got <- struct{}{}; <-stuck }); err != nil {
		t.Fatal(err)
	}
	<-got
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := h.bridge.StopBLE(ctx); err != nil {
		t.Fatalf("StopBLE with an indication pending: %v", err)
	}
	<-conn.Disconnected()
	close(stuck)
}

// TestWithdrawBLEDisconnectsCentralWithoutSecureSession: the window's end
// disconnects a central that has no secure session bound to its
// connection — one that never completed PASE — so it does not hold the
// bridge's one BLE slot into the next window. connectedhomeip closes
// every BLE connection when the window closes while it still listens for
// PASE (src/app/server/CommissioningWindowManager.cpp:431-438 at
// 6170af84) (review II, R2-P3-ble-05).
func TestWithdrawBLEDisconnectsCentralWithoutSecureSession(t *testing.T) {
	h, p := newBLEHarness(t, nil)
	idle := dialRadio(t, p) // the BTP handshake only; no Matter message at all

	if err := h.bridge.WithdrawBLE(t.Context()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	select {
	case <-idle.conn.Disconnected():
	case <-ctx.Done():
		t.Fatal("a central with no secure session is still connected after the window ended")
	}

	// The next window's commissioner gets the slot once the bridge saw
	// the disconnect.
	waitFor(t, "the slot freed", func() bool {
		h.bridge.ble.mu.Lock()
		defer h.bridge.ble.mu.Unlock()
		return h.bridge.ble.central == nil
	})
	if err := h.bridge.StartBLE(t.Context(), bleWindow); err != nil {
		t.Fatal(err)
	}
	next := dialRadio(t, p)
	if err := next.s.Send(h.sealedInvoke(0x314)); err != nil {
		t.Fatal(err)
	}
	waitReply(t, next.replies)
}

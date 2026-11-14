// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"sync"
	"time"

	"github.com/SukramJ/go-fabric/clock"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/transport"
	"github.com/SukramJ/go-fabric/transport/btp"
)

// BLE commissioning: the bridge serves the Matter GATT service on the
// host's [contract.BLEPeripheral] as it serves TCP on its listener — each
// central's BTP session is one more connection of the transport-neutral
// receive path, its replies sent back on it without MRP — and advertises
// the commissionable payload on matter.js's schedule. Whoever announces the
// commissioning window decides when ([Bridge.StartBLE]; [Bridge.WithdrawBLE]
// at the window's end; [Bridge.StopBLE] once the node is commissioned); the
// node facade does so for an uncommissioned node only.

// The BLE advertisement's schedule — matter.js BleAdvertisement.run
// (packages/protocol/src/advertisement/ble/BleAdvertisement.ts:13-20,
// :43-71 at db021da0) with BleAdvertiser.Configuration's defaults
// (BleAdvertiser.ts:100-108): the same payload at the early interval for
// 30 s, at the late interval until 15 minutes, then the extended
// announcement at the extended interval, until the advertisement's
// timeout (MAXIMUM_COMMISSIONING_TIMEOUT, 2 days,
// packages/types/src/commissioning/CommissioningConstants.ts:11).
const (
	bleEarlyInterval    = 20 * time.Millisecond
	bleLateInterval     = 150 * time.Millisecond
	bleExtendedInterval = 1285 * time.Millisecond

	bleEarlySleep    = 30 * time.Second
	bleLateSleep     = 15*time.Minute - bleEarlySleep
	bleExtendedSleep = 48*time.Hour - bleLateSleep

	bleAdvertisementTimeout = 48 * time.Hour
	// bleExtendedAfter is when an advertisement is an extended
	// announcement: STANDARD_COMMISSIONING_TIMEOUT
	// (CommissioningConstants.ts:10; Advertisement.ts:180-182).
	bleExtendedAfter = 15 * time.Minute
)

// errNoBLE is StartBLE on a bridge without a [Config].BLE.
var errNoBLE = errors.New("bridge: no BLE peripheral configured (Config.BLE)")

// bleRuntime is the bridge's BLE side while it is on the radio.
type bleRuntime struct {
	// mu guards every field below.
	mu sync.Mutex
	// running is set between Start and Stop: StartBLE refuses outside
	// it, so nothing StartBLE begins outlives Stop.
	running bool
	// serveCancel ends the GATT service's Serve, nil once asked to end;
	// serveDone closes when Serve returned and stays set until then, so
	// the next StartBLE waits for the old Serve rather than racing it.
	serveCancel context.CancelFunc
	serveDone   chan struct{}
	// advCancel ends the advertisement's schedule, nil once asked to end;
	// advDone closes when it stopped advertising and stays set until
	// then. adv is what it advertises.
	advCancel context.CancelFunc
	advDone   chan struct{}
	adv       bleAdvert
	// central is the one connected central, nil without one; centralConn
	// is its BTP connection once the BTP handshake opened one.
	central     contract.BLEConnection
	centralConn transport.Conn
	// receivers holds the receiver of every BTP connection until its
	// handlers returned, so [Bridge.Stop] waits for them
	// (waitBLEHandlers) as it waits for the UDP and TCP listeners' ones.
	receivers map[*connReceiver]struct{}
}

// bleAdvert is what the BLE advertisement carries of a
// [CommissioningAdvertisement].
type bleAdvert struct {
	discriminator, vendorID, productID uint16
}

// setBLERunning marks the bridge started (true) or stopping (false) for
// StartBLE.
func (b *Bridge) setBLERunning(on bool) {
	b.ble.mu.Lock()
	b.ble.running = on
	b.ble.mu.Unlock()
}

// StartBLE puts the node on the Bluetooth radio of [Config].BLE for the
// commissioning window adv announces: it registers the Matter GATT service
// (once; C3 empty — matter.js serves additional data only when
// `commissioning.ble.aad` is set, BleAdvertisement.ts:35, :82), accepts one
// central at a time — a second one that connects meanwhile is
// disconnected, as matter.js's peripheral serves a single connection
// (BlenoBleServer.ts:129-133) — and serves each central's BTP session
// through the receive path in delivery order, without MRP (the session is
// reliable; matter.js Session.ts:368-370). A lost connection evicts the
// sessions bound to it (matter.js Ble.ts:17-22, BleChannelClosedError).
//
// The advertisement carries adv's discriminator, vendor and product id
// on matter.js's schedule (20 ms for 30 s, 150 ms until 15 minutes, then
// the extended announcement — no vendor and product id, the extended
// flag — at 1285 ms, for 2 days; BleAdvertisement.ts:13-85). A second call
// with the same discriminator, vendor and product id keeps the running
// schedule, as matter.js does not restart a broadcasting advertisement
// (Advertisement.ts:83-87); a different one, or a call after
// [Bridge.WithdrawBLE], starts it anew with the service left registered.
// BLE announces a basic window only: adv.CommissioningMode must be 1
// (matter.js BleAdvertiser.getAdvertisement, BleAdvertiser.ts:37-45).
//
// A service the platform refuses or withdraws ends the advertisement as
// well — StartBLE cannot report it, Serve fails on its own goroutine
// (logged as matter.ble.serve) — and the next StartBLE registers the
// service again.
//
// A service or advertisement still ending after a [Bridge.StopBLE] that
// gave up waiting is waited for first, until ctx ends. The bridge must be
// started; [Bridge.Stop] ends what StartBLE started.
func (b *Bridge) StartBLE(ctx context.Context, adv CommissioningAdvertisement) error {
	p := b.cfg.BLE
	if p == nil {
		return errNoBLE
	}
	if adv.CommissioningMode != 1 {
		return fmt.Errorf("bridge: BLE advertises a basic commissioning window (CM=1), not CM=%d", adv.CommissioningMode)
	}
	want := bleAdvert{discriminator: adv.Discriminator, vendorID: adv.VendorID, productID: adv.ProductID}

	for {
		b.ble.mu.Lock()
		if !b.ble.running {
			b.ble.mu.Unlock()
			return ErrNotStarted
		}
		ending := b.ble.serveDone
		if b.ble.serveCancel != nil || ending == nil {
			break // locked
		}
		b.ble.mu.Unlock()
		select {
		case <-ending:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if b.ble.serveCancel == nil {
		serveCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
		done := make(chan struct{})
		b.ble.serveCancel, b.ble.serveDone = cancel, done
		go b.serveBLE(serveCtx, cancel, done, p)
		b.logger.Info("matter.ble.serving")
	}
	if b.ble.advCancel != nil && b.ble.adv == want {
		b.ble.mu.Unlock()
		return nil
	}
	prevCancel, prevDone := b.ble.advCancel, b.ble.advDone
	advCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	done := make(chan struct{})
	b.ble.advCancel, b.ble.advDone, b.ble.adv = cancel, done, want
	b.ble.mu.Unlock()

	if prevCancel != nil {
		prevCancel()
	}
	go func() {
		defer close(done)
		defer func() {
			b.ble.mu.Lock()
			if b.ble.advDone == done {
				b.ble.advCancel, b.ble.advDone, b.ble.adv = nil, nil, bleAdvert{}
			}
			b.ble.mu.Unlock()
		}()
		if prevDone != nil {
			// The advertisement it replaces stops first, so its stop
			// cannot end this one.
			select {
			case <-prevDone:
			case <-advCtx.Done():
				return
			}
		}
		b.advertiseBLE(advCtx, p, want)
	}()
	return nil
}

// serveBLE runs the GATT service of one StartBLE until it ends, then
// closes done.
func (b *Bridge) serveBLE(serveCtx context.Context, cancel context.CancelFunc, done chan struct{}, p contract.BLEPeripheral) {
	defer close(done)
	err := p.Serve(serveCtx, nil, b.acceptBLE)
	if err != nil {
		b.logger.Warn("matter.ble.serve", slog.Any("err", err))
	}
	// The service ended — asked to, or on its own (refused, withdrawn):
	// the next StartBLE registers it again. A service that failed on its
	// own also ends the advertisement: the node does not announce itself
	// as commissionable over BLE with no service behind it (review II,
	// R2-P1-req-06).
	var advCancel context.CancelFunc
	b.ble.mu.Lock()
	if b.ble.serveDone == done {
		b.ble.serveCancel, b.ble.serveDone = nil, nil
		if err != nil && serveCtx.Err() == nil {
			advCancel = b.ble.advCancel
			b.ble.advCancel = nil
		}
	}
	b.ble.mu.Unlock()
	if advCancel != nil {
		advCancel()
	}
	cancel()
}

// WithdrawBLE ends the BLE advertisement and leaves the GATT service and a
// commissioning central alone — matter.js's end of a commissioning window,
// which stops the BLE advertisement (BleAdvertisement.ts:69-71) while the
// BLE transport stays until the node is commissioned
// (ServerNetworkRuntime.ts:294-308). A connected central with no secure
// session bound to its connection — one that never completed PASE — is
// disconnected, as connectedhomeip closes every BLE connection when the
// window closes while it still listens for PASE
// (src/app/server/CommissioningWindowManager.cpp:431-438 at 6170af84):
// nothing else would end its hold on the one BLE slot (review II,
// R2-P3-ble-05). It waits for the advertisement to stop until ctx ends;
// without one it does nothing more.
func (b *Bridge) WithdrawBLE(ctx context.Context) error {
	b.ble.mu.Lock()
	cancel, done := b.ble.advCancel, b.ble.advDone
	b.ble.advCancel = nil
	central, conn := b.ble.central, b.ble.centralConn
	b.ble.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if central != nil && !b.connHasSecureSession(conn) {
		b.logger.Info("matter.ble.central_dropped", slog.String("peer", central.Remote()),
			slog.String("reason", "the commissioning window ended before PASE completed"))
		_ = central.Disconnect()
	}
	return waitBLE(ctx, done)
}

// connHasSecureSession reports whether a secure session is bound to c
// (nil: none).
func (b *Bridge) connHasSecureSession(c transport.Conn) bool {
	if c == nil {
		return false
	}
	raw, ok := b.connRoutes.Load(c)
	if !ok {
		return false
	}
	r, _ := raw.(*connRoute)
	if r == nil {
		return false
	}
	b.routeBindMu.Lock()
	defer b.routeBindMu.Unlock()
	return r.sessions > 0
}

// StopBLE takes the node off the radio: the advertisement stops, the
// connected central is disconnected and the GATT service unregistered
// (matter.js ServerNetworkRuntime.endUncommissionedMode,
// ServerNetworkRuntime.ts:294-308, deletes the BLE advertiser and closes
// the transport). It waits for the radio until ctx ends; without a running
// BLE side it does nothing.
func (b *Bridge) StopBLE(ctx context.Context) error {
	b.ble.mu.Lock()
	advCancel, advDone := b.ble.advCancel, b.ble.advDone
	serveCancel, serveDone := b.ble.serveCancel, b.ble.serveDone
	b.ble.advCancel, b.ble.serveCancel = nil, nil
	b.ble.mu.Unlock()
	if advDone == nil && serveDone == nil {
		return nil
	}
	if advCancel != nil {
		advCancel()
	}
	if serveCancel != nil {
		serveCancel()
	}
	if err := waitBLE(ctx, advDone); err != nil {
		return err
	}
	if err := waitBLE(ctx, serveDone); err != nil {
		return err
	}
	b.logger.Info("matter.ble.stopped")
	return nil
}

// waitBLE waits for done (nil: nothing to wait for) until ctx ends.
func waitBLE(ctx context.Context, done chan struct{}) error {
	if done == nil {
		return nil
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// advertiseBLE runs the advertisement's schedule until ctx ends or the
// timeout passed, then stops advertising — matter.js BleAdvertisement.run
// (BleAdvertisement.ts:29-72).
func (b *Bridge) advertiseBLE(ctx context.Context, p contract.BLEPeripheral, adv bleAdvert) {
	clk := b.clk()
	start := clk.Now()
	defer b.stopAdvertisingBLE(p) //nolint:contextcheck // the schedule's own context has ended; stopping runs on a fresh bounded one
	steps := []struct {
		sleep, interval time.Duration
	}{
		{bleEarlySleep, bleEarlyInterval},
		{bleLateSleep, bleLateInterval},
		{bleExtendedSleep, bleExtendedInterval},
	}
	timeout := bleAdvertisementTimeout
	extended := false
	data := btp.EncodeAdvertisementData(adv.discriminator, adv.vendorID, adv.productID, false, false)
	for _, step := range steps {
		// The extended announcement re-encodes the payload: no vendor and
		// product id, the extended flag (BleAdvertisement.ts:51-56, :74-85).
		if !extended && clock.Since(clk, start) >= bleExtendedAfter {
			extended = true
			data = btp.EncodeAdvertisementData(adv.discriminator, 0, 0, false, true)
		}
		if err := p.Advertise(ctx, contract.BLEAdvertisement{Data: data, Interval: step.interval}); err != nil {
			if ctx.Err() == nil {
				b.logger.Warn("matter.ble.advertise", slog.Any("err", err))
			}
			return
		}
		b.logger.Debug("matter.ble.advertising",
			slog.Int("discriminator", int(adv.discriminator)),
			slog.Duration("interval", step.interval))
		if !clock.Sleep(clk, min(timeout, step.sleep), ctx.Done()) {
			return
		}
		timeout -= step.sleep
		if timeout <= 0 {
			return
		}
	}
}

// stopAdvertisingBLE ends the advertisement, bounded by
// [Config].AdvertiseTimeout — matter.js's `finally` of
// BleAdvertisement.run (BleAdvertisement.ts:69-71).
func (b *Bridge) stopAdvertisingBLE(p contract.BLEPeripheral) {
	stopCtx, cancel := clock.WithTimeout(context.Background(), b.clk(), b.cfg.AdvertiseTimeout)
	defer cancel()
	if err := p.StopAdvertising(stopCtx); err != nil {
		b.logger.Debug("matter.ble.stop_advertising", slog.Any("err", err))
	}
}

// stopBLEForShutdown is [Bridge.Stop]'s StopBLE: StartBLE refuses from
// here on, so nothing it would begin outlives Stop.
func (b *Bridge) stopBLEForShutdown(ctx context.Context) {
	b.setBLERunning(false)
	if err := b.StopBLE(ctx); err != nil {
		b.logger.Warn("matter.ble.stop", slog.Any("err", err))
	}
}

// trackBLEReceiver records r until its handlers returned.
func (b *Bridge) trackBLEReceiver(r *connReceiver) {
	b.ble.mu.Lock()
	defer b.ble.mu.Unlock()
	if b.ble.receivers == nil {
		b.ble.receivers = make(map[*connReceiver]struct{})
	}
	b.ble.receivers[r] = struct{}{}
}

// untrackBLEReceiver forgets r once its handlers returned.
func (b *Bridge) untrackBLEReceiver(r *connReceiver) {
	b.ble.mu.Lock()
	delete(b.ble.receivers, r)
	b.ble.mu.Unlock()
}

// waitBLEHandlers is [Bridge.Stop]'s wait for the handlers of messages
// delivered over BLE, bounded by ctx: every receiver stops delivering and
// its handlers in flight are waited for, so none of them writes into a
// store the host closes or erases after Stop. Stop calls it after the
// handler context was cancelled, so a handler that honours it ends
// promptly.
func (b *Bridge) waitBLEHandlers(ctx context.Context) {
	b.ble.mu.Lock()
	rs := make([]*connReceiver, 0, len(b.ble.receivers))
	for r := range b.ble.receivers {
		rs = append(rs, r)
	}
	b.ble.mu.Unlock()
	if len(rs) == 0 {
		return
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for _, r := range rs {
			r.close() // no delivery after this, so wait cannot race an Add
			r.wait()
		}
	}()
	select {
	case <-done:
	case <-ctx.Done():
		b.logger.Warn("matter.bridge.stop.ble_handlers_timeout", slog.Any("err", ctx.Err()))
	}
}

// acceptBLE is the GATT service's accept: one central at a time, its BTP
// session served through the receive path.
func (b *Bridge) acceptBLE(c contract.BLEConnection) {
	b.ble.mu.Lock()
	if b.ble.central != nil {
		b.ble.mu.Unlock()
		b.logger.Info("matter.ble.central_refused", slog.String("peer", c.Remote()))
		_ = c.Disconnect()
		return
	}
	b.ble.central = c
	b.ble.mu.Unlock()
	b.logger.Info("matter.ble.central_connected", slog.String("peer", c.Remote()))
	go func() {
		<-c.Disconnected()
		b.ble.mu.Lock()
		if b.ble.central == c {
			b.ble.central, b.ble.centralConn = nil, nil
		}
		b.ble.mu.Unlock()
		b.logger.Info("matter.ble.central_disconnected", slog.String("peer", c.Remote()))
	}()
	err := btp.Accept(c, func(conn *btp.Conn) func([]byte) {
		b.ble.mu.Lock()
		if b.ble.central == c {
			b.ble.centralConn = conn
		}
		b.ble.mu.Unlock()
		r := b.newConnReceiver(conn)
		b.trackBLEReceiver(r)
		go func() {
			select {
			case <-conn.Closed():
			case <-c.Disconnected():
			}
			r.close()
			r.wait()
			b.untrackBLEReceiver(r)
		}()
		return r.deliver
	}, btp.Options{TimeSource: b.clk(), Logger: b.logger})
	if err != nil {
		b.logger.Warn("matter.ble.accept", slog.Any("err", err))
		_ = c.Disconnect()
	}
}

// connReceiver delivers the Matter messages of one connection of a
// transport without a listener of the bridge's own (BTP) to the receive
// path: in delivery order, each once its predecessor returned or released
// its turn — the contract of transport/tcp's ordered handler, so a handler
// that waits for the peer's answer on the same connection lets that
// answer through.
type connReceiver struct {
	b    *Bridge
	conn transport.Conn

	// inFlight holds one slot per message that is delivered and not yet
	// finished ([connReceiverMaxInFlight]).
	inFlight chan struct{}
	// stopped closes with the receiver, ending a delivery that waits for
	// a slot.
	stopped chan struct{}

	// mu guards prev and done.
	mu   sync.Mutex
	prev chan struct{}
	done bool
	wg   sync.WaitGroup
}

// connReceiverMaxInFlight bounds the messages of one connection that are
// delivered but not yet finished: beyond it deliver waits, so the
// transport takes no further message from the peer — for BTP the central's
// next write is not consumed — until a handler returns or the connection
// closes. It is transport/tcp's bound on the same queue, by the same means
// (maxInFlightPerConn: the reader stops reading). Without it every message
// a peer sent while a handler held the connection's turn was one more
// goroutine and one more copy.
const connReceiverMaxInFlight = 64

func (b *Bridge) newConnReceiver(conn transport.Conn) *connReceiver {
	first := make(chan struct{})
	close(first)
	return &connReceiver{
		b: b, conn: conn, prev: first,
		inFlight: make(chan struct{}, connReceiverMaxInFlight),
		stopped:  make(chan struct{}),
	}
}

// deliver hands one complete Matter message to the bridge; it does not
// wait for the handler, only — with [connReceiverMaxInFlight] messages in
// flight — for one of them to finish.
func (r *connReceiver) deliver(msg []byte) {
	select {
	case r.inFlight <- struct{}{}:
	case <-r.stopped:
		return
	}
	r.mu.Lock()
	if r.done {
		r.mu.Unlock()
		<-r.inFlight
		return
	}
	prev := r.prev
	turn := make(chan struct{})
	r.prev = turn
	r.wg.Add(1)
	r.mu.Unlock()
	msg = append([]byte(nil), msg...)
	go func() {
		defer r.wg.Done()
		defer func() { <-r.inFlight }()
		var once sync.Once
		release := func() { once.Do(func() { close(turn) }) }
		defer release()
		defer r.recoverHandler(len(msg))
		<-prev
		// As handleTCPMessage: the dispatch context carries the release of
		// the connection's turn.
		ctx := context.WithValue(r.b.handlerContext(), sessionTurnKey{}, release)
		_ = r.b.receive(ctx, msg, r.conn)
	}()
}

// recoverHandler recovers a panic of a delivered message's handler, so a
// message over BLE cannot end the process: logged and counted as the UDP
// listener's safeDispatch and the TCP connection's safeHandle do (review
// II, R2-P1-dr-req-08). deliver's other defers give back the turn and the
// place in flight.
func (r *connReceiver) recoverHandler(msgLen int) {
	p := recover()
	if p == nil {
		return
	}
	r.b.panicRecovered()
	r.b.logger.Error("matter.ble.dispatch_panic",
		slog.Any("panic", p),
		slog.String("peer", r.conn.Remote().String()),
		slog.Int("message_bytes", msgLen),
		slog.String("stack", string(debug.Stack())))
}

// close stops delivering and evicts every session bound to the
// connection, as a dropped TCP connection does.
func (r *connReceiver) close() {
	r.mu.Lock()
	if !r.done {
		r.done = true
		close(r.stopped)
	}
	r.mu.Unlock()
	r.b.evictConn(r.conn)
}

// wait waits for every delivered message's handler to return.
func (r *connReceiver) wait() { r.wg.Wait() }

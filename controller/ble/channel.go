// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package ble

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/SukramJ/go-fabric/client/exchange"
	"github.com/SukramJ/go-fabric/clock"
	"github.com/SukramJ/go-fabric/secure/sessionparams"
	"github.com/SukramJ/go-fabric/transport/btp"
)

// ErrHandshakeTimeout is a peripheral that sent no BTP handshake response
// within btp.ConnRspTimeout — matter.js's BleError("Handshake Response
// not received", packages/nodejs-ble/src/NobleBleChannel.ts:643-652).
var ErrHandshakeTimeout = errors.New("ble: BTP handshake response not received")

// ErrChannelClosed is the cause a PASE session over a lost channel is
// closed with — matter.js BleChannelClosedError
// (packages/protocol/src/ble/Ble.ts:17-22). It wraps btp.ErrDisconnected,
// so the commissioning flow reads it as a channel drop. [Channel.Send]
// returns it for a message the channel can no longer carry.
var ErrChannelClosed = fmt.Errorf("ble: commissioning channel closed: %w", btp.ErrDisconnected)

// ATTMTUSettleTimeout is how long [Dial] waits for a pending ATT_MTU
// exchange before it sizes the BTP segment without it — matter.js
// ATT_MTU_SETTLE_TIMEOUT (packages/nodejs-ble/src/NobleBleChannel.ts:40-41
// at 862ad75f).
const ATTMTUSettleTimeout = 2 * time.Second

// DisconnectTimeout bounds each C2 unsubscribe: a GATT stack can leave
// the operation pending when the peripheral vanished without a disconnect
// event — matter.js BLE_DISCONNECT_TIMEOUT
// (packages/nodejs-ble/src/NobleBleChannel.ts:38 at 862ad75f) and its
// unsubscribeC2 (:573-588), which runs the unsubscribe under it (:579).
const DisconnectTimeout = 5 * time.Second

// DialOptions configure [Dial]; the zero value is the system clock, no
// log, the default session parameters and the segment size the ATT_MTU
// gives.
type DialOptions struct {
	// Clock paces the BTP timers, the handshake timeout and the channel's
	// exchanges.
	Clock exchange.Clock
	// Logger receives the channel's diagnostics.
	Logger *slog.Logger
	// LocalParameters are our session parameters in PASE.
	LocalParameters *sessionparams.Parameters
}

// Channel is a BTP session to one peripheral as a commissioning channel:
// the exchange manager whose one carrier is the session, reliable, so no
// MRP runs — matter.js NobleBleChannel
// (packages/nodejs-ble/src/NobleBleChannel.ts:670-1022 at 862ad75f) under
// the ExchangeManager it feeds. It satisfies commissioner.Channel.
//
// The session behind the channel is replaceable: a peripheral that
// completes the handshake and then answers no BTP packet before the
// acknowledgement timeout is given a fresh session at the minimum segment
// size, once, with the unacknowledged messages replayed — matter.js's
// renegotiation (NobleBleChannel.ts:864-918 at 862ad75f), an interop
// workaround its session documents as having no basis in the
// specification (packages/protocol/src/ble/BtpSessionHandler.ts:67-84 at
// 862ad75f). Every [Channel.Send] waits for a renegotiation in flight; a
// renegotiation that fails closes the channel. See
// BD-Matter-BTP-RenegotiationAfterStall.
type Channel struct {
	peripheral Peripheral
	conn       Connection
	mgr        *exchange.Manager
	addr       *net.UDPAddr
	log        *slog.Logger
	clock      exchange.Clock
	// gattCtx is the context of the session's GATT operations; they
	// outlive Dial's context.
	gattCtx context.Context
	// life ends when the channel closes or is lost; the renegotiation
	// handshake runs under it (NobleBleChannel.ts:728 #lifetime at
	// 862ad75f) and a Send parked on a renegotiation wakes on it.
	life *lifetime

	// mu guards session, btpConn, c2, swapped, renegotiation, closing and
	// lost.
	mu      sync.Mutex
	session *btp.Session
	btpConn *btp.Conn
	// c2 is the C2 subscription's handler (:850 #c2DataHandler).
	c2 *handshakeState
	// swapped is closed when session is replaced, so watch re-reads it.
	swapped chan struct{}
	// renegotiation is nil until a renegotiation starts and closed when it
	// ended, whichever way (NobleBleChannel.ts:868-870 at 862ad75f: "it
	// must exist before the first step runs, settle once the channel
	// ends").
	renegotiation chan struct{}
	// stallReported is closed when the first stall report arrived, so a
	// Send that met the suspended session before it can wait for the
	// renegotiation it starts.
	stallReported chan struct{}
	closing       bool
	// lost is the session closed or the connection gone
	// (NobleBleChannel.ts:830-834, :932-934 at 862ad75f).
	lost bool

	// released is the GATT connection given up: C2 unsubscribed and the
	// connection dropped, by the session's close or by Close.
	released atomic.Bool

	closeOnce sync.Once
	closed    chan struct{}
}

// lifetime is the channel's lifetime context and its cancel, behind one
// pointer so Channel stays comparable.
type lifetime struct {
	ctx context.Context
	end context.CancelFunc
}

// handshakeState routes C2 data: to the handshake until the response
// arrived, to the session afterwards (NobleBleChannel.ts:609-620,
// :837-847 at 862ad75f); detached, it drops everything (:850-855
// #detachDataHandler). Each handshake has its own.
type handshakeState struct {
	// mu guards session and detached.
	mu       sync.Mutex
	session  *btp.Session
	detached bool
	response chan []byte
	log      *slog.Logger
	address  string
}

func newHandshakeState(log *slog.Logger, address string) *handshakeState {
	return &handshakeState{response: make(chan []byte, 1), log: log, address: address}
}

// attach routes C2 data to s from now on.
func (h *handshakeState) attach(s *btp.Session) {
	h.mu.Lock()
	h.session = s
	h.mu.Unlock()
}

// detach drops every C2 datum from now on.
func (h *handshakeState) detach() {
	h.mu.Lock()
	h.detached = true
	h.mu.Unlock()
}

func (h *handshakeState) onC2(data []byte) {
	h.mu.Lock()
	s, detached := h.session, h.detached
	h.mu.Unlock()
	if detached {
		h.log.Debug("matter.ble.c2_after_detach", slog.String("address", h.address))
		return
	}
	if s == nil {
		if btp.IsHandshakeResponse(data) {
			select {
			case h.response <- append([]byte(nil), data...):
			default:
			}
			return
		}
		h.log.Debug("matter.ble.c2_before_handshake", slog.String("address", h.address))
		return
	}
	if err := s.HandleIncoming(data); err != nil {
		h.log.Info("matter.ble.rx_error", slog.String("address", h.address), slog.Any("err", err))
	}
}

// Dial opens the commissioning channel to p — matter.js
// NobleBleChannel.create (NobleBleChannel.ts:671-715) and
// performBtpHandshake (:593-668): the GATT connection, the segment size
// from the ATT_MTU (a pending exchange awaited for [ATTMTUSettleTimeout]
// when the connection reports one, the minimum while it stays unknown),
// the handshake request (versions, segment size, the maximum window)
// written on C1, C2 subscribed, the handshake response awaited for
// btp.ConnRspTimeout, then the central's BTP session on C1 and C2. A lost
// GATT connection closes the session and the channel.
func Dial(ctx context.Context, central Central, p Peripheral, opts DialOptions) (*Channel, error) {
	if opts.Clock == nil {
		opts.Clock = exchange.SystemClock{}
	}
	if opts.Logger == nil {
		opts.Logger = slog.New(slog.DiscardHandler)
	}
	conn, err := central.Connect(ctx, p)
	if err != nil {
		return nil, fmt.Errorf("ble: connect %s: %w", p.Address, err)
	}
	segment, err := segmentSizeFor(ctx, conn, opts.Clock, opts.Logger, p.Address)
	if err != nil {
		_ = conn.Disconnect()
		return nil, err
	}
	h := newHandshakeState(opts.Logger, p.Address)
	resp, err := handshake(ctx, conn, h, segment, opts.Clock)
	if err != nil {
		_ = conn.Disconnect()
		return nil, err
	}
	c := &Channel{
		peripheral: p, conn: conn, log: opts.Logger, clock: opts.Clock, c2: h,
		swapped: make(chan struct{}), stallReported: make(chan struct{}), closed: make(chan struct{}),
	}
	// The address the manager names the peer by; the carrier ignores it.
	c.addr = &net.UDPAddr{IP: net.IPv6zero, Zone: "ble-" + p.Address}
	// The session's GATT operations outlive Dial's context.
	c.gattCtx = context.WithoutCancel(ctx)
	c.life = &lifetime{}
	c.life.ctx, c.life.end = context.WithCancel(c.gattCtx)
	session, err := c.adoptSession(resp, segment)
	if err != nil {
		c.life.end()
		_ = conn.Disconnect()
		return nil, err
	}
	c.mgr, err = exchange.NewManager(c, exchange.Config{Reliable: true, Clock: opts.Clock, Logger: opts.Logger, LocalParameters: opts.LocalParameters})
	if err != nil {
		c.life.end()
		_ = session.Close()
		return nil, err
	}
	h.attach(session)
	go c.watch()
	return c, nil
}

// segmentSizeFor sizes the BTP segment from the connection's ATT_MTU —
// matter.js attMtuOf (NobleBleChannel.ts:526-567 at 862ad75f) and create
// (:679-690): an unknown MTU on a connection that reports its exchange
// ([MTUExchange]) is awaited for [ATTMTUSettleTimeout]; the exchange
// landing, the timeout or the connection going settles it, and the
// context ending fails it. "Deriving the BTP segment size from an unknown
// MTU would pin the session to the 20-byte minimum for its whole life".
func segmentSizeFor(ctx context.Context, conn Connection, clk exchange.Clock, log *slog.Logger, address string) (int, error) {
	mtu := conn.ATTMTU()
	if mtu <= 0 {
		if x, ok := conn.(MTUExchange); ok {
			if err := ctx.Err(); err != nil {
				return 0, fmt.Errorf("ble: %s: ATT_MTU wait: %w", address, err)
			}
			expired := make(chan struct{})
			t := clk.AfterFunc(ATTMTUSettleTimeout, func() { close(expired) })
			select {
			case <-x.ATTMTUExchanged():
				mtu = conn.ATTMTU()
			case <-expired:
			case <-conn.Disconnected():
			case <-ctx.Done():
				t.Stop()
				return 0, fmt.Errorf("ble: %s: ATT_MTU wait: %w", address, ctx.Err())
			}
			t.Stop()
		}
	}
	if mtu <= 0 {
		log.Info("matter.ble.att_mtu_unknown", slog.String("address", address), slog.Int("fragment_size", btp.MinimumATTMTU))
		return btp.MinimumATTMTU, nil
	}
	segment := btp.SegmentSizeFromATTMTU(mtu)
	log.Debug("matter.ble.segment_size", slog.String("address", address), slog.Int("fragment_size", segment), slog.Int("att_mtu", mtu))
	return segment, nil
}

// unsubscribeC2 is matter.js unsubscribeC2
// (packages/nodejs-ble/src/NobleBleChannel.ts:573-588 at 862ad75f): the
// unsubscribe under [DisconnectTimeout] on the clock, so a peripheral that
// vanished without a disconnect event cannot hold the caller.
func unsubscribeC2(ctx context.Context, conn Connection, c exchange.Clock) error {
	bounded, cancel := clock.WithTimeout(ctx, c, DisconnectTimeout)
	defer cancel()
	err := conn.UnsubscribeC2(bounded)
	if err != nil && ctx.Err() == nil && bounded.Err() != nil {
		return fmt.Errorf("ble: unsubscribe C2 not completed within %v: %w", DisconnectTimeout, err)
	}
	return err
}

// handshake is performBtpHandshake (NobleBleChannel.ts:593-668). The C1
// write and the C2 subscribe run under the btp.ConnRspTimeout attempt
// as the response wait does (:602-606, :634-641 at 862ad75f: "Every
// await below must end on abort or timeout"), so a write that never
// completes ends in [ErrHandshakeTimeout]. A context that ends meanwhile
// is the caller's abort (:645-647 at 862ad75f).
func handshake(ctx context.Context, conn Connection, h *handshakeState, segment int, c exchange.Clock) ([]byte, error) {
	req := btp.EncodeHandshakeRequest(btp.HandshakeRequest{Versions: btp.SupportedVersions, ATTMTU: uint16(segment), ClientWindowSize: btp.MaximumWindowSize}) //nolint:gosec // a segment size, at most 244
	attempt, cancel := clock.WithTimeout(ctx, c, btp.ConnRspTimeout)
	defer cancel()
	// timedOut abandons the handshake whose attempt expired: C2
	// unsubscribed while the connection is up (:652-654 at 862ad75f).
	timedOut := func() ([]byte, error) {
		select {
		case <-conn.Disconnected():
		default:
			_ = unsubscribeC2(context.WithoutCancel(ctx), conn, c)
		}
		return nil, ErrHandshakeTimeout
	}
	if err := conn.WriteC1(attempt, req); err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("ble: %s: BTP handshake: %w", h.address, ctx.Err())
		}
		if attempt.Err() != nil {
			return timedOut()
		}
		return nil, fmt.Errorf("ble: write the BTP handshake request: %w", err)
	}
	if err := conn.SubscribeC2(attempt, h.onC2); err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("ble: %s: BTP handshake: %w", h.address, ctx.Err())
		}
		if attempt.Err() != nil {
			return timedOut()
		}
		return nil, fmt.Errorf("ble: subscribe C2: %w", err)
	}
	select {
	case resp := <-h.response:
		return resp, nil
	case <-conn.Disconnected():
		return nil, fmt.Errorf("ble: %s disconnected during the BTP handshake: %w", h.address, btp.ErrDisconnected)
	case <-attempt.Done():
		if ctx.Err() != nil {
			_ = unsubscribeC2(context.WithoutCancel(ctx), conn, c)
			return nil, ctx.Err()
		}
		return timedOut()
	}
}

// adoptSession installs a freshly handshaken BTP session — matter.js
// #adoptSession (NobleBleChannel.ts:770-848 at 862ad75f): the central's
// session on C1 and C2, its stall observed, swapped in under the lock so
// Send and watch see it. A channel lost meanwhile leaves the session
// suspended, "so nothing else would stop its timers" (:818-826). The
// caller routes C2 data to it.
func (c *Channel) adoptSession(handshakeResponse []byte, segment int) (*btp.Session, error) {
	session, err := btp.NewCentral(handshakeResponse, btp.Callbacks{
		Write: func(b []byte) error { return c.conn.WriteC1(c.gattCtx, b) },
		// Unsubscribing C2 closes the BTP session (§4.19.4.10), then the
		// connection goes (NobleBleChannel.ts:789-805).
		Disconnect: func() error { return c.releaseGATT(true) },
		Message:    c.deliver,
	}, segment, btp.Options{TimeSource: c.clock, Logger: c.log})
	if err != nil {
		return nil, err
	}
	// Mirrors matter.js packages/nodejs-ble/src/NobleBleChannel.ts:835 at
	// 862ad75f: session.stalledAfterHandshake.once(#startRenegotiation).
	session.OnStall(c.startRenegotiation)
	c.mu.Lock()
	if !c.usableLocked() {
		c.mu.Unlock()
		session.Suspend()
		return nil, fmt.Errorf("ble: %s: channel lost while establishing the BTP session: %w", c.peripheral.Address, ErrChannelClosed)
	}
	c.session = session
	c.btpConn = btp.NewConn(session, btp.Addr{Peripheral: c.peripheral.Address})
	close(c.swapped)
	c.swapped = make(chan struct{})
	c.mu.Unlock()
	return session, nil
}

// releaseGATT gives the GATT connection up, once: C2 unsubscribed when
// the session's close asks for it (NobleBleChannel.ts:789-805) and the
// connection dropped. Close calls it for a session that is suspended,
// without the unsubscribe (:1013-1019).
func (c *Channel) releaseGATT(unsubscribe bool) error {
	if !c.released.CompareAndSwap(false, true) {
		return nil
	}
	if unsubscribe {
		if err := unsubscribeC2(c.gattCtx, c.conn, c.clock); err != nil {
			c.log.Debug("matter.ble.unsubscribe_failed", slog.Any("err", err))
		}
	}
	return c.conn.Disconnect()
}

// startRenegotiation mirrors matter.js
// packages/nodejs-ble/src/NobleBleChannel.ts:864-889 at 862ad75f
// (#startRenegotiation): at most once per channel — a session already at
// the minimum segment size never reports the stall — and every Send waits
// for it. A failure closes the channel unless the channel is closing
// already (:872-888).
func (c *Channel) startRenegotiation(replay [][]byte) {
	c.mu.Lock()
	if c.renegotiation != nil {
		c.mu.Unlock()
		return
	}
	done := make(chan struct{})
	c.renegotiation = done
	close(c.stallReported)
	c.mu.Unlock()
	go func() {
		defer close(done)
		err := c.renegotiate(replay)
		if err == nil {
			return
		}
		c.mu.Lock()
		closing := c.closing
		c.mu.Unlock()
		if closing {
			c.log.Debug("matter.ble.renegotiation_ended_by_close", slog.String("address", c.peripheral.Address))
			return
		}
		if c.connected() {
			c.log.Warn("matter.ble.renegotiation_failed", slog.String("address", c.peripheral.Address), slog.Any("err", err))
		} else {
			c.log.Debug("matter.ble.renegotiation_ended_by_disconnect", slog.String("address", c.peripheral.Address), slog.Any("err", err))
		}
		if cerr := c.Close(); cerr != nil {
			c.log.Debug("matter.ble.close_after_renegotiation_failed", slog.Any("err", cerr))
		}
	}()
}

// renegotiate mirrors matter.js
// packages/nodejs-ble/src/NobleBleChannel.ts:891-918 at 862ad75f
// (#renegotiate): the C2 handler detached — the stalled session is
// suspended already — and C2 unsubscribed, which closes the peripheral's
// BTP session (§4.19.4.10) and leaves the BLE connection; a failure there
// may leave the peer holding the old session, so no new handshake is
// attempted. The unsubscribe runs under [DisconnectTimeout] (:579 at
// 862ad75f), the handshake's write and subscribe under its
// btp.ConnRspTimeout attempt, so a peripheral that vanished without a
// disconnect event fails the renegotiation within the bound and the
// channel closes. Then the handshake at btp.MinimumATTMTU under the
// channel's lifetime, with a fresh C2 handler, the session adopted, and
// the unacknowledged messages replayed in order.
func (c *Channel) renegotiate(replay [][]byte) error {
	c.mu.Lock()
	old := c.c2
	c.mu.Unlock()
	old.detach()
	c.log.Info("matter.ble.renegotiate", slog.String("address", c.peripheral.Address), slog.Int("fragment_size", btp.MinimumATTMTU))
	if err := unsubscribeC2(c.life.ctx, c.conn, c.clock); err != nil {
		return fmt.Errorf("ble: unsubscribe C2 for the renegotiation: %w", err)
	}
	if err := c.assertRenegotiable(); err != nil {
		return err
	}
	h := newHandshakeState(c.log, c.peripheral.Address)
	resp, err := handshake(c.life.ctx, c.conn, h, btp.MinimumATTMTU, c.clock)
	if err != nil {
		return err
	}
	c.mu.Lock()
	c.c2 = h
	c.mu.Unlock()
	if err := c.assertRenegotiable(); err != nil {
		return err
	}
	session, err := c.adoptSession(resp, btp.MinimumATTMTU)
	if err != nil {
		return err
	}
	h.attach(session)
	if err := c.assertRenegotiable(); err != nil {
		return err
	}
	for _, msg := range replay {
		if err := session.Send(msg); err != nil {
			return fmt.Errorf("ble: replay after the renegotiation: %w", err)
		}
	}
	return nil
}

// assertRenegotiable is #assertRenegotiable (NobleBleChannel.ts:920-926).
func (c *Channel) assertRenegotiable() error {
	c.mu.Lock()
	usable := c.usableLocked()
	c.mu.Unlock()
	if !usable {
		return fmt.Errorf("ble: %s: channel lost while renegotiating the BTP session: %w", c.peripheral.Address, ErrChannelClosed)
	}
	return nil
}

// connected is the GATT connection still up (NobleBleChannel.ts:928-930).
func (c *Channel) connected() bool {
	select {
	case <-c.conn.Disconnected():
		return false
	default:
		return true
	}
}

// usableLocked is #usable (NobleBleChannel.ts:932-934): not closing, the
// session not lost, the connection up.
func (c *Channel) usableLocked() bool {
	return !c.closing && !c.lost && c.connected()
}

// watch closes the channel when the current session closes or the
// connection drops (NobleBleChannel.ts:738-754, :830-834); a session
// swapped in by a renegotiation is watched in its turn.
func (c *Channel) watch() {
	for {
		c.mu.Lock()
		s, swapped := c.session, c.swapped
		c.mu.Unlock()
		select {
		case <-s.Closed():
		case <-c.conn.Disconnected():
			_ = s.Close()
		case <-swapped:
			continue
		}
		c.mu.Lock()
		c.lost = true
		c.mu.Unlock()
		c.life.end()
		c.closeOnce.Do(func() { close(c.closed) })
		return
	}
}

// deliver hands a reassembled Matter message to the exchange manager.
func (c *Channel) deliver(msg []byte) error {
	c.mgr.HandleDatagram(msg, c.addr)
	return nil
}

// Send is the exchange manager's carrier: one Matter message on the BTP
// session; dst is the channel's own address. It waits for a renegotiation
// in flight — the replay goes first — and returns [ErrChannelClosed] when
// the channel is lost meanwhile (NobleBleChannel.ts:944-953 at 862ad75f:
// "BtpFlowError from the suspended session would not read as channel
// loss downstream").
func (c *Channel) Send(_ *net.UDPAddr, payload []byte) error {
	for {
		c.mu.Lock()
		wait := c.renegotiation
		c.mu.Unlock()
		if wait != nil {
			select {
			case <-wait:
			case <-c.life.ctx.Done():
			}
		}
		c.mu.Lock()
		usable, conn := c.usableLocked(), c.btpConn
		c.mu.Unlock()
		if !usable {
			return ErrChannelClosed
		}
		err := conn.Send(payload)
		if err == nil || !conn.Session().Stalled() {
			return err
		}
		// The session stalled between the lookup and the send, its report
		// not yet through to startRenegotiation (matter.js suspends and
		// emits in one step): wait for the renegotiation it starts and
		// send on the new session.
		select {
		case <-c.stallReported:
		case <-c.life.ctx.Done():
		}
	}
}

// Exchanges is the manager whose sessions run over the channel.
func (c *Channel) Exchanges() *exchange.Manager { return c.mgr }

// PeerAddress is the address the manager names the peripheral by.
func (c *Channel) PeerAddress() *net.UDPAddr { return c.addr }

// Peripheral is the peripheral the channel runs to.
func (c *Channel) Peripheral() Peripheral { return c.peripheral }

// PeripheralAddress is the peripheral's address — the BLE address the
// commissioner hands the scanners once the device is commissioned
// (commissioner.CommissionedScanner).
func (c *Channel) PeripheralAddress() string { return c.peripheral.Address }

// Conn is the current BTP session as a transport.Conn; a renegotiation
// replaces it, so send through [Channel.Send], which waits for one.
func (c *Channel) Conn() *btp.Conn {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.btpConn
}

// Closed is closed when the channel is gone.
func (c *Channel) Closed() <-chan struct{} { return c.closed }

// ClosedCause is the error a session over the lost channel ends with.
func (c *Channel) ClosedCause() error { return ErrChannelClosed }

// Close closes the exchange manager and the BTP session, which
// unsubscribes C2 and disconnects — matter.js close
// (NobleBleChannel.ts:1004-1021 at 862ad75f). A renegotiation in flight
// is abandoned, its parked sends released, and the connection a
// suspended session no longer owns is dropped here.
func (c *Channel) Close() error {
	c.mu.Lock()
	c.closing = true
	s := c.session
	c.mu.Unlock()
	c.life.end()
	c.mgr.Close()
	err := s.Close()
	if rerr := c.releaseGATT(false); err == nil {
		err = rerr
	}
	c.closeOnce.Do(func() { close(c.closed) })
	return err
}

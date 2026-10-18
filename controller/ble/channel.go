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

	"github.com/SukramJ/go-fabric/controller/exchange"
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
// so the commissioning flow reads it as a channel drop.
var ErrChannelClosed = fmt.Errorf("ble: commissioning channel closed: %w", btp.ErrDisconnected)

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
// (packages/nodejs-ble/src/NobleBleChannel.ts:670-860) under the
// ExchangeManager it feeds. It satisfies commissioner.Channel.
type Channel struct {
	peripheral Peripheral
	conn       Connection
	session    *btp.Session
	btpConn    *btp.Conn
	mgr        *exchange.Manager
	addr       *net.UDPAddr
	log        *slog.Logger

	closeOnce sync.Once
	closed    chan struct{}
}

// handshakeState routes C2 data: to the handshake until the response
// arrived, to the session afterwards (NobleBleChannel.ts:609-620, :826-836).
type handshakeState struct {
	mu       sync.Mutex
	session  *btp.Session
	response chan []byte
	log      *slog.Logger
	address  string
}

func (h *handshakeState) onC2(data []byte) {
	h.mu.Lock()
	s := h.session
	h.mu.Unlock()
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
		h.log.Info("matter.ble.rx_error", slog.String("address", h.address), slog.String("err", err.Error()))
	}
}

// Dial opens the commissioning channel to p — matter.js
// NobleBleChannel.create (NobleBleChannel.ts:670-714) and
// performBtpHandshake (:592-662): the GATT connection, the segment size
// from the ATT_MTU (the minimum while it is unknown), the handshake
// request (versions, segment size, the maximum window) written on C1,
// C2 subscribed, the handshake response awaited for btp.ConnRspTimeout,
// then the central's BTP session on C1 and C2. A lost GATT connection
// closes the session and the channel.
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
	segment := btp.MinimumATTMTU
	if mtu := conn.ATTMTU(); mtu > 0 {
		segment = btp.SegmentSizeFromATTMTU(mtu)
	}
	h := &handshakeState{response: make(chan []byte, 1), log: opts.Logger, address: p.Address}
	resp, err := handshake(ctx, conn, h, segment, opts.Clock)
	if err != nil {
		_ = conn.Disconnect()
		return nil, err
	}
	c := &Channel{peripheral: p, conn: conn, log: opts.Logger, closed: make(chan struct{})}
	// The address the manager names the peer by; the carrier ignores it.
	c.addr = &net.UDPAddr{IP: net.IPv6zero, Zone: "ble-" + p.Address}
	// The session's GATT operations outlive Dial's context.
	gattCtx := context.WithoutCancel(ctx)
	session, err := btp.NewCentral(resp, btp.Callbacks{
		Write: func(b []byte) error { return conn.WriteC1(gattCtx, b) },
		Disconnect: func() error {
			// Unsubscribing C2 closes the BTP session (§4.19.4.10), then
			// the connection goes (NobleBleChannel.ts:784-800).
			if err := conn.UnsubscribeC2(gattCtx); err != nil {
				c.log.Debug("matter.ble.unsubscribe_failed", slog.String("err", err.Error()))
			}
			return conn.Disconnect()
		},
		Message: c.deliver,
	}, segment, btp.Options{TimeSource: opts.Clock, Logger: opts.Logger})
	if err != nil {
		_ = conn.Disconnect()
		return nil, err
	}
	c.session = session
	c.btpConn = btp.NewConn(session, btp.Addr{Peripheral: p.Address})
	c.mgr, err = exchange.NewManager(c, exchange.Config{Reliable: true, Clock: opts.Clock, Logger: opts.Logger, LocalParameters: opts.LocalParameters})
	if err != nil {
		_ = session.Close()
		return nil, err
	}
	h.mu.Lock()
	h.session = session
	h.mu.Unlock()
	go c.watch()
	return c, nil
}

// handshake is performBtpHandshake (NobleBleChannel.ts:592-662).
func handshake(ctx context.Context, conn Connection, h *handshakeState, segment int, clock exchange.Clock) ([]byte, error) {
	req := btp.EncodeHandshakeRequest(btp.HandshakeRequest{Versions: btp.SupportedVersions, ATTMTU: uint16(segment), ClientWindowSize: btp.MaximumWindowSize}) //nolint:gosec // a segment size, at most 244
	expired := make(chan struct{})
	t := clock.AfterFunc(btp.ConnRspTimeout, func() { close(expired) })
	defer t.Stop()
	if err := conn.WriteC1(ctx, req); err != nil {
		return nil, fmt.Errorf("ble: write the BTP handshake request: %w", err)
	}
	if err := conn.SubscribeC2(ctx, h.onC2); err != nil {
		return nil, fmt.Errorf("ble: subscribe C2: %w", err)
	}
	select {
	case resp := <-h.response:
		return resp, nil
	case <-expired:
		_ = conn.UnsubscribeC2(context.WithoutCancel(ctx))
		return nil, ErrHandshakeTimeout
	case <-conn.Disconnected():
		return nil, fmt.Errorf("ble: %s disconnected during the BTP handshake: %w", h.address, btp.ErrDisconnected)
	case <-ctx.Done():
		_ = conn.UnsubscribeC2(context.WithoutCancel(ctx))
		return nil, ctx.Err()
	}
}

// watch closes the channel when the session closes or the connection
// drops (NobleBleChannel.ts:735-754, :812-817).
func (c *Channel) watch() {
	select {
	case <-c.session.Closed():
	case <-c.conn.Disconnected():
		_ = c.session.Close()
	}
	c.closeOnce.Do(func() { close(c.closed) })
}

// deliver hands a reassembled Matter message to the exchange manager.
func (c *Channel) deliver(msg []byte) error {
	c.mgr.HandleDatagram(msg, c.addr)
	return nil
}

// Send is the exchange manager's carrier: one Matter message on the BTP
// session; dst is the channel's own address.
func (c *Channel) Send(_ *net.UDPAddr, payload []byte) error {
	return c.btpConn.Send(payload)
}

// Exchanges is the manager whose sessions run over the channel.
func (c *Channel) Exchanges() *exchange.Manager { return c.mgr }

// PeerAddress is the address the manager names the peripheral by.
func (c *Channel) PeerAddress() *net.UDPAddr { return c.addr }

// Peripheral is the peripheral the channel runs to.
func (c *Channel) Peripheral() Peripheral { return c.peripheral }

// Conn is the BTP session as a transport.Conn.
func (c *Channel) Conn() *btp.Conn { return c.btpConn }

// Closed is closed when the channel is gone.
func (c *Channel) Closed() <-chan struct{} { return c.closed }

// ClosedCause is the error a session over the lost channel ends with.
func (c *Channel) ClosedCause() error { return ErrChannelClosed }

// Close closes the exchange manager and the BTP session, which
// unsubscribes C2 and disconnects.
func (c *Channel) Close() error {
	c.mgr.Close()
	err := c.session.Close()
	c.closeOnce.Do(func() { close(c.closed) })
	return err
}

// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package btp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"github.com/SukramJ/go-fabric/clock"
)

// GATTConnection is one central's GATT connection to the Matter service,
// as the peripheral sees it: what [Accept] serves a BTP session on. It is
// the method set of the host port contract.BLEConnection, which every
// implementation of that port satisfies; it is restated here because the
// transport layer sits below the port contracts (script/archcheck).
type GATTConnection interface {
	// Remote names the central.
	Remote() string
	// OnWrite receives every C1 write, in order.
	OnWrite(func(value []byte))
	// OnSubscribe receives the C2 subscription with the ATT_MTU.
	OnSubscribe(func(attMTU int))
	// OnUnsubscribe receives the end of the C2 subscription.
	OnUnsubscribe(func())
	// Indicate sends an indication on C2 and returns once the central
	// confirmed it.
	Indicate(ctx context.Context, value []byte) error
	// Disconnect drops the connection.
	Disconnect() error
	// Disconnected is closed when the connection is gone.
	Disconnected() <-chan struct{}
}

// ErrNoConnection is returned by [Accept] for a nil connection or a nil
// open function.
var ErrNoConnection = errors.New("btp: nil GATT connection or open function")

// Accept serves the peripheral's side of the Matter GATT service on c —
// matter.js BlenoBleServer (packages/nodejs-ble/src/BlenoBleServer.ts at
// db021da0), with the platform cut away. It registers its handlers on c
// and returns at once:
//
//   - A C1 write of a handshake request (a 9-byte value starting 0x65 0x6C)
//     is kept, and [ConnRspTimeout] starts: the central must subscribe C2
//     before it ends, or the connection is dropped (:251-264, :148-150,
//     :413-416; BleConsts.ts:56). Any other C1 write feeds the session; one
//     without a session is refused (:265-275).
//   - The C2 subscription starts the session from the kept request, its
//     segment size bounded by the ATT_MTU less the 3-byte ATT header
//     (:288-323) — refused before a handshake request and while a
//     session runs (:289-296). The session's handshake response and
//     every later packet go out as indications, each complete once the
//     central confirmed it (:304-310, :329-336).
//     A kept request that states a window of 0 starts no session and
//     drops the connection — connectedhomeip's rule, not matter.js's
//     ([NewPeripheral]).
//   - open receives the connection once the session is established, before
//     any of its messages, and returns the receiver of every reassembled
//     Matter message, in order; nil drops them (:316-322).
//   - The end of the C2 subscription closes the session and drops the
//     connection (:81-84, :461-475); so does the session's own close — a
//     missed acknowledgement, a protocol violation (:312-313). A dropped
//     connection closes the session (:195-213).
//
// A refused write or subscription is logged: the platform has answered
// the central's GATT operation already (matter.js answers a refused write
// with RESULT_UNLIKELY_ERROR, :51-57, which a platform that delivers C1
// over a socket cannot). opts paces the timer and the session and names
// the logger.
func Accept(c GATTConnection, open func(conn *Conn) (handle func(msg []byte)), opts Options) error {
	if c == nil || open == nil {
		return ErrNoConnection
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &gattServer{c: c, open: open, opts: opts, clock: clock.Or(opts.TimeSource), log: opts.Logger, ctx: ctx, cancel: cancel}
	c.OnWrite(s.handleC1Write)
	c.OnSubscribe(s.handleC2Subscribe)
	c.OnUnsubscribe(s.handleC2Unsubscribe)
	go s.watch()
	return nil
}

// gattServer is one connection's BTP server — the per-connection state of
// matter.js BlenoBleServer (:136-157).
type gattServer struct {
	c     GATTConnection
	open  func(conn *Conn) (handle func(msg []byte))
	opts  Options
	clock clock.Clock
	log   *slog.Logger
	// ctx ends when the connection is gone: an indication waiting for its
	// confirmation gives up.
	ctx    context.Context
	cancel context.CancelFunc

	// mu guards handshake, timer, timerGen, session, handle and closing.
	// It is held while the session is created, so a C1 write that races
	// the subscription cannot pass for a second request or find no
	// session.
	mu        sync.Mutex
	handshake []byte
	timer     clock.Timer
	timerGen  uint64
	session   *Session
	handle    func(msg []byte)
	closing   bool
}

// isHandshakeRequest is matter.js's test for a handshake request on C1:
// the first two bytes and the length (:256).
func isHandshakeRequest(data []byte) bool {
	return len(data) == 9 && data[0] == 0x65 && data[1] == 0x6c
}

// handleC1Write is handleC1WriteRequest (:251-277).
func (s *gattServer) handleC1Write(data []byte) {
	data = append([]byte(nil), data...)
	s.mu.Lock()
	if s.closing {
		s.mu.Unlock()
		return
	}
	if isHandshakeRequest(data) {
		// A handshake request (re)starts the timer and is kept until the
		// subscription; one that arrives on an open session is kept too,
		// as matter.js does (its TODO at :264).
		s.startTimerLocked()
		s.handshake = data
		s.mu.Unlock()
		s.log.Debug("matter.btp.handshake_request", slog.String("peer", s.c.Remote()))
		return
	}
	session := s.session
	s.mu.Unlock()
	if session == nil {
		s.log.Warn("matter.btp.c1_refused", slog.String("peer", s.c.Remote()),
			slog.Any("err", &FlowError{Message: "Received Matter data but no BTP session was initialized"}))
		return
	}
	if err := session.HandleIncoming(data); err != nil {
		s.log.Info("matter.btp.rx_failed", slog.String("peer", s.c.Remote()), slog.Any("err", err))
	}
}

// handleC2Subscribe is handleC2SubscribeRequest (:288-327).
func (s *gattServer) handleC2Subscribe(attMTU int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closing {
		return
	}
	if s.handshake == nil {
		s.log.Warn("matter.btp.c2_refused", slog.String("peer", s.c.Remote()),
			slog.Any("err", &FlowError{Message: "Subscription request received before handshake Request"}))
		return
	}
	if s.session != nil {
		s.log.Warn("matter.btp.c2_refused", slog.String("peer", s.c.Remote()),
			slog.Any("err", &FlowError{Message: "Subscription request received but BTP session already initialized. Cannot handle two sessions!"}))
		return
	}
	s.stopTimerLocked()
	// matter.js takes min(Bleno.mtu - 3, maxValueSize) (:300): the ATT_MTU
	// less the ATT header is both.
	session, err := NewPeripheral(attMTU-3, s.handshake, Callbacks{
		Write:      s.indicate,
		Disconnect: s.disconnect,
		Message:    s.deliver,
	}, s.opts)
	if err != nil {
		s.log.Warn("matter.btp.session_failed", slog.String("peer", s.c.Remote()), slog.Any("err", err))
		return
	}
	s.handshake = nil
	s.session = session
	conn := NewConn(session, Addr{Peripheral: s.c.Remote()})
	s.handle = s.open(conn)
	// A close the session starts itself (the acknowledgement timeout, a
	// protocol violation) drops the connection through its Disconnect
	// callback (:313, :326).
}

// handleC2Unsubscribe is the C2 unsubscribe: the server closes (:81-84).
func (s *gattServer) handleC2Unsubscribe() {
	s.log.Debug("matter.btp.c2_unsubscribe", slog.String("peer", s.c.Remote()))
	s.close()
}

// indicate is the session's write callback: an indication on C2 that
// completes with the central's confirmation (:304-310). A connection gone
// meanwhile is ErrDisconnected, which ends the session without an error.
func (s *gattServer) indicate(packet []byte) error {
	if err := s.c.Indicate(s.ctx, packet); err != nil {
		select {
		case <-s.c.Disconnected():
			return fmt.Errorf("%w: %w", ErrDisconnected, err)
		default:
		}
		if s.ctx.Err() != nil {
			return fmt.Errorf("%w: %w", ErrDisconnected, err)
		}
		return err
	}
	return nil
}

// deliver hands one reassembled Matter message to the receiver open
// returned (:316-322).
func (s *gattServer) deliver(msg []byte) error {
	s.mu.Lock()
	handle := s.handle
	s.mu.Unlock()
	if handle != nil {
		handle(msg)
	}
	return nil
}

// disconnect is the session's disconnect callback: the server closes
// (:313). matter.js does not await the close inside the session's own
// call; here the close runs on its own goroutine, because the session may
// call this while the subscription that creates it holds the server's
// lock (an unsupported version, :105-109 of BtpSessionHandler.ts).
func (s *gattServer) disconnect() error {
	go s.close()
	return nil
}

// close is BlenoBleServer.close (:461-475): once, the timer stopped, the
// connection dropped, the session closed.
func (s *gattServer) close() {
	s.mu.Lock()
	if s.closing {
		s.mu.Unlock()
		return
	}
	s.closing = true
	s.stopTimerLocked()
	session := s.session
	s.mu.Unlock()
	if err := s.c.Disconnect(); err != nil {
		s.log.Debug("matter.btp.disconnect_failed", slog.String("peer", s.c.Remote()), slog.Any("err", err))
	}
	if session != nil {
		_ = session.Close()
	}
}

// watch closes the session when the connection drops — the platform's
// disconnect event (:195-213) — and ends the wait of an indication.
func (s *gattServer) watch() {
	<-s.c.Disconnected()
	s.cancel()
	s.mu.Lock()
	s.closing = true
	s.stopTimerLocked()
	session := s.session
	s.mu.Unlock()
	if session != nil {
		_ = session.Close()
	}
}

// startTimerLocked (re)starts the handshake timer (:258): when it fires
// before the subscription, the connection is dropped (:413-416).
func (s *gattServer) startTimerLocked() {
	s.stopTimerLocked()
	gen := s.timerGen
	s.timer = s.clock.AfterFunc(ConnRspTimeout, func() {
		s.mu.Lock()
		// A callback of a timer replaced meanwhile (it fired while a
		// repeated request restarted the timer) leaves the new one alone.
		fired := s.timerGen == gen
		if fired {
			s.timer = nil
		}
		s.mu.Unlock()
		if !fired {
			return
		}
		s.log.Warn("matter.btp.handshake_timeout", slog.String("peer", s.c.Remote()))
		s.close()
	})
}

// stopTimerLocked stops the handshake timer (:297, :467).
func (s *gattServer) stopTimerLocked() {
	if s.timer != nil {
		s.timer.Stop()
		s.timer = nil
	}
	s.timerGen++
}

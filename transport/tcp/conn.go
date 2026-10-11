// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package tcp

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"runtime/debug"
	"sync"
	"sync/atomic"

	"github.com/SukramJ/go-fabric/transport"
)

// Handler handles one message of a connection. release hands the
// connection's turn to its next message before the handler returns; a
// handler calls it before it waits for something only a later message
// of the same connection can bring (the peer's answer to what it just
// sent). Returning releases the turn too; release is idempotent. The
// handler owns msg.
type Handler func(c *Conn, msg []byte, release func())

// maxInFlightPerConn bounds the messages of one connection that are
// read but not yet finished. Beyond it the reader stops reading, which
// the peer sees as TCP backpressure — a reliable stream is never
// dropped from, unlike the UDP listener's saturated pool. matter.js
// consumes the socket by async iteration for the same backpressure
// (TcpChannel.ts:81-82).
const maxInFlightPerConn = 64

// readChunkSize is the size of one socket read. A Go detail with no
// matter.js counterpart; the assembler accepts chunks of any size.
const readChunkSize = 16 * 1024

// Conn is one TCP connection carrying length-framed Matter messages —
// the Go translation of matter.js TcpChannel. It implements
// [transport.Conn] with Reliable() == true.
type Conn struct {
	nc             net.Conn
	remote         net.Addr
	maxPayloadSize int
	incoming       bool
	logger         *slog.Logger

	writeMu sync.Mutex

	closeOnce sync.Once
	closed    chan struct{}
	closeErr  error

	// received is set by the first complete message; the listener's
	// idle timer reads it (TcpTransport.ts:89-104 receivedData).
	received atomic.Bool

	recoveredPanics *atomic.Uint64
}

var _ transport.Conn = (*Conn)(nil)

func newConn(nc net.Conn, maxPayloadSize int, incoming bool, logger *slog.Logger, panics *atomic.Uint64) *Conn {
	if maxPayloadSize <= FramingHeaderSize {
		maxPayloadSize = DefaultMaxPayloadSize
	}
	if logger == nil {
		logger = slog.Default()
	}
	if panics == nil {
		panics = new(atomic.Uint64)
	}
	return &Conn{
		nc:              nc,
		remote:          nc.RemoteAddr(),
		maxPayloadSize:  maxPayloadSize,
		incoming:        incoming,
		logger:          logger,
		closed:          make(chan struct{}),
		recoveredPanics: panics,
	}
}

// Send frames msg with its 4-byte little-endian length and writes it.
// Returns [ErrMessageTooLarge] above [Conn.MaxMessageSize] and
// [ErrConnClosed] after the connection closed. Mirrors matter.js
// TcpChannel.ts:119-133 send.
func (c *Conn) Send(msg []byte) error {
	if c.isClosed() {
		return ErrConnClosed
	}
	frame, err := encodeFrame(msg, c.maxPayloadSize)
	if err != nil {
		return err
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if _, err := c.nc.Write(frame); err != nil {
		if c.isClosed() {
			return ErrConnClosed
		}
		return fmt.Errorf("tcp: write %s: %w", c.remote, err)
	}
	return nil
}

// Remote is the peer's address (a [*net.TCPAddr]).
func (c *Conn) Remote() net.Addr { return c.remote }

// Reliable is true: TCP guarantees delivery, so MRP is not used over
// it (TcpChannel.ts:44, Session.ts:355-356).
func (c *Conn) Reliable() bool { return true }

// Incoming reports whether the peer opened the connection.
func (c *Conn) Incoming() bool { return c.incoming }

// MaxMessageSize is the largest message [Conn.Send] accepts: the
// maximum payload size minus the length prefix (TcpChannel.ts:114-117).
func (c *Conn) MaxMessageSize() int { return c.maxPayloadSize - FramingHeaderSize }

// Closed is closed once the connection is.
func (c *Conn) Closed() <-chan struct{} { return c.closed }

// Err reports why the connection closed: nil while it is open or after
// a local [Conn.Close], else the framing violation or read error that
// closed it.
func (c *Conn) Err() error {
	select {
	case <-c.closed:
		return c.closeErr
	default:
		return nil
	}
}

// Close closes the connection. Idempotent.
func (c *Conn) Close() error {
	c.close(nil)
	return nil
}

func (c *Conn) close(reason error) {
	c.closeOnce.Do(func() {
		c.closeErr = reason
		close(c.closed)
		_ = c.nc.Close()
	})
}

func (c *Conn) isClosed() bool {
	select {
	case <-c.closed:
		return true
	default:
		return false
	}
}

// String names the connection the way matter.js TcpChannel.ts:100-108
// does: the scheme, the peer and, for an inbound connection, the
// peer's ephemeral port marked as such.
func (c *Conn) String() string {
	if c.incoming {
		return "tcp://" + c.remote.String() + " (inbound)"
	}
	return "tcp://" + c.remote.String()
}

// serve reads the connection until it closes, handing every complete
// message to handler in arrival order. Returns once the connection is
// closed; a handler still running finishes on its own goroutine.
func (c *Conn) serve(handler Handler) {
	defer c.close(nil)

	asm := newAssembler(c.maxPayloadSize)
	sem := make(chan struct{}, maxInFlightPerConn)
	prev := make(chan struct{})
	close(prev)
	buf := make([]byte, readChunkSize)
	for {
		n, err := c.nc.Read(buf)
		if n > 0 {
			msgs, ferr := asm.push(buf[:n])
			for _, msg := range msgs {
				c.received.Store(true)
				select {
				case sem <- struct{}{}:
				case <-c.closed:
					return
				}
				done := make(chan struct{})
				var once sync.Once
				release := func() { once.Do(func() { close(done) }) }
				go func(prev <-chan struct{}, msg []byte) {
					defer func() { <-sem }()
					defer release()
					<-prev
					// matter.js stops delivering once the channel closed
					// (TcpChannel.ts:203-205).
					if c.isClosed() {
						return
					}
					c.safeHandle(handler, msg, release)
				}(prev, msg)
				prev = done
			}
			if ferr != nil {
				c.logger.Warn("matter.tcp.frame",
					slog.String("conn", c.String()),
					slog.String("err", ferr.Error()))
				c.close(ferr)
				return
			}
		}
		if err != nil {
			if !c.isClosed() && !errors.Is(err, net.ErrClosed) {
				c.logger.Debug("matter.tcp.read",
					slog.String("conn", c.String()),
					slog.String("err", err.Error()))
				c.close(err)
			}
			return
		}
	}
}

// safeHandle recovers a handler panic so one bad message cannot take
// the connection's reader down; logged and counted like the UDP
// listener's safeDispatch.
func (c *Conn) safeHandle(handler Handler, msg []byte, release func()) {
	defer func() {
		if r := recover(); r != nil {
			total := c.recoveredPanics.Add(1)
			c.logger.Error("matter.tcp.dispatch_panic",
				slog.Any("panic", r),
				slog.String("conn", c.String()),
				slog.Int("message_bytes", len(msg)),
				slog.Uint64("recovered_total", total),
				slog.String("stack", string(debug.Stack())))
		}
	}()
	handler(c, msg, release)
}

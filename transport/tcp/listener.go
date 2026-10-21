// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package tcp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/SukramJ/go-fabric/clock"
)

// MatterPort is the IANA-registered Matter port (5540). The TCP
// listener shares it with UDP (matter.js ServerNetworkRuntime.ts:203-214
// listens on the UDP interface's port).
const MatterPort = 5540

// MaxConnectionsPerPeerIP caps the inbound connections from one peer IP
// address, any port. Mirrors matter.js
// packages/protocol/src/transport/tcp/TcpTransport.ts:27-28
// (MAX_CONNECTIONS_PER_PEER_IP = 3).
const MaxConnectionsPerPeerIP = 3

// NewConnectionIdleTimeout is how long an inbound connection may stay
// open without delivering a complete message before it is closed.
// Mirrors TcpTransport.ts:30-31 (NEW_CONNECTION_IDLE_TIMEOUT =
// Seconds(10)); the timer stops on the first message (:98-103).
const NewConnectionIdleTimeout = 10 * time.Second

// ErrListenerClosed is returned by [Listener.Serve] when it is called
// on a closed listener.
var ErrListenerClosed = errors.New("tcp: listener closed")

// errIdle closes a connection whose first message did not arrive in
// time.
var errIdle = errors.New("tcp: no message within the new-connection idle timeout")

// Config controls the listener.
type Config struct {
	// LocalAddr is the bind address. Empty defaults to `[::]:5540`.
	LocalAddr string
	// PreferIPv4 forces an IPv4-only socket. Default (false) opens an
	// IPv6 dual-stack socket which also accepts IPv4 connections.
	PreferIPv4 bool
	// MaxPayloadSize is the maximum frame size (length prefix plus
	// message). Zero means [DefaultMaxPayloadSize] (TcpTransport.ts:62).
	MaxPayloadSize int
	// Logger receives the listener's log lines. nil means slog.Default.
	Logger *slog.Logger
	// Clock paces the idle timeout of a new connection; nil is
	// [clock.System].
	Clock clock.Clock
}

// Listener accepts inbound Matter TCP connections — the server role of
// matter.js TcpTransport (TcpTransport.ts:78-112).
type Listener struct {
	ln             *net.TCPListener
	maxPayloadSize int
	logger         *slog.Logger
	idleTimeout    time.Duration
	clock          clock.Clock

	recoveredPanics atomic.Uint64

	// work is the goroutines of the accepted connections — each one's
	// reader and its close watcher — added under mu while the listener
	// is open and waited for by [Listener.WaitIdle].
	work sync.WaitGroup

	// mu guards conns and closed.
	mu     sync.Mutex
	conns  map[*Conn]struct{}
	closed bool
}

// bindAddr resolves the (network, address) pair Listen binds for cfg.
func bindAddr(cfg Config) (network, addr string) {
	addr = cfg.LocalAddr
	if addr == "" {
		if cfg.PreferIPv4 {
			addr = fmt.Sprintf("0.0.0.0:%d", MatterPort)
		} else {
			addr = fmt.Sprintf("[::]:%d", MatterPort)
		}
	}
	network = "tcp"
	if cfg.PreferIPv4 {
		network = "tcp4"
	}
	return network, addr
}

// Listen opens the TCP socket per cfg.
func Listen(cfg Config) (*Listener, error) {
	network, addr := bindAddr(cfg)
	tcpAddr, err := net.ResolveTCPAddr(network, addr)
	if err != nil {
		return nil, fmt.Errorf("tcp: resolve %s: %w", addr, err)
	}
	ln, err := net.ListenTCP(network, tcpAddr)
	if err != nil {
		return nil, fmt.Errorf("tcp: listen %s: %w", addr, err)
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	maxPayload := cfg.MaxPayloadSize
	if maxPayload <= FramingHeaderSize {
		maxPayload = DefaultMaxPayloadSize
	}
	return &Listener{
		ln:             ln,
		maxPayloadSize: maxPayload,
		logger:         logger,
		idleTimeout:    NewConnectionIdleTimeout,
		clock:          clock.Or(cfg.Clock),
		conns:          make(map[*Conn]struct{}),
	}, nil
}

// Addr returns the bound address — useful when binding port 0.
func (l *Listener) Addr() *net.TCPAddr {
	addr, _ := l.ln.Addr().(*net.TCPAddr)
	return addr
}

// RecoveredPanics reports how many handler panics the listener's
// connections recovered.
func (l *Listener) RecoveredPanics() uint64 { return l.recoveredPanics.Load() }

// Conns reports the number of open connections.
func (l *Listener) Conns() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.conns)
}

// Serve accepts connections until ctx is cancelled or the listener is
// closed. Every complete message goes to handler (see [Handler] for
// the ordering contract); onClose, when non-nil, runs once per
// accepted connection after it closed — the disconnect notification
// matter.js's ExchangeManager subscribes to for session eviction
// (TcpTransport.ts:216-223, ExchangeManager.ts:605-614). It is safe to
// call Serve at most once.
func (l *Listener) Serve(ctx context.Context, handler Handler, onClose func(*Conn)) error {
	if handler == nil {
		return errors.New("tcp: nil handler")
	}
	if l.isClosed() {
		return ErrListenerClosed
	}
	// Serve owns the cancel watcher and returns only after it did.
	done, watched := make(chan struct{}), make(chan struct{})
	defer func() {
		close(done)
		<-watched
	}()
	go func() {
		defer close(watched)
		select {
		case <-ctx.Done():
			_ = l.Close()
		case <-done:
		}
	}()
	for {
		nc, err := l.ln.Accept()
		if err != nil {
			if l.isClosed() || errors.Is(err, net.ErrClosed) {
				return nil
			}
			return fmt.Errorf("tcp: accept: %w", err)
		}
		l.accept(nc, handler, onClose)
	}
}

// accept registers one inbound connection, applying the per-peer cap
// and the idle timer. Mirrors TcpTransport.ts:78-112.
func (l *Listener) accept(nc net.Conn, handler Handler, onClose func(*Conn)) {
	c := newConn(nc, l.maxPayloadSize, true, l.logger, &l.recoveredPanics)
	ip := peerIP(c.remote)

	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		c.close(ErrListenerClosed)
		return
	}
	n := 0
	for other := range l.conns {
		if peerIP(other.remote) == ip {
			n++
		}
	}
	if n >= MaxConnectionsPerPeerIP {
		l.mu.Unlock()
		l.logger.Warn("matter.tcp.reject",
			slog.String("conn", c.String()),
			slog.String("reason", "too many connections from this IP"))
		c.close(nil)
		return
	}
	l.conns[c] = struct{}{}
	l.work.Add(2) // the close watcher and the reader below
	l.mu.Unlock()

	// Detect and close zombie connections (TcpTransport.ts:89-104).
	idle := l.clock.AfterFunc(l.idleTimeout, func() {
		if !c.received.Load() {
			l.logger.Debug("matter.tcp.idle_close",
				slog.String("conn", c.String()))
			c.close(errIdle)
		}
	})

	go func() {
		defer l.work.Done()
		<-c.Closed()
		idle.Stop()
		l.mu.Lock()
		delete(l.conns, c)
		l.mu.Unlock()
		if onClose != nil {
			onClose(c)
		}
	}()
	go func() {
		defer l.work.Done()
		c.serve(handler)
	}()
}

// WaitIdle blocks until every accepted connection's reader, its handler
// calls and its close notification have returned, or ctx ends (its error
// is returned then). A host calls it after [Listener.Close], as for the
// UDP listener's WaitIdle, so no handler — and no host code a handler
// calls — still runs once shutdown reports done. Close does not wait
// itself: it runs on Serve's cancel and may be called from a handler.
func (l *Listener) WaitIdle(ctx context.Context) error {
	idle := make(chan struct{})
	go func() {
		l.work.Wait()
		close(idle)
	}()
	select {
	case <-idle:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("tcp: wait idle: %w", ctx.Err())
	}
}

// Close stops accepting and closes every open connection. Idempotent.
// Mirrors TcpTransport.ts:184-195.
func (l *Listener) Close() error {
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		return nil
	}
	l.closed = true
	conns := make([]*Conn, 0, len(l.conns))
	for c := range l.conns {
		conns = append(conns, c)
	}
	l.mu.Unlock()
	err := l.ln.Close()
	for _, c := range conns {
		c.close(nil)
	}
	return err
}

func (l *Listener) isClosed() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.closed
}

// peerIP is the address part of a connection's remote, the key of the
// per-peer cap (TcpTransport.ts:229-237 #countConnectionsFromIp).
func peerIP(a net.Addr) string {
	if t, ok := a.(*net.TCPAddr); ok {
		return t.IP.String()
	}
	host, _, err := net.SplitHostPort(a.String())
	if err != nil {
		return a.String()
	}
	return host
}

// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package tcp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sync/atomic"
)

// Dialer opens outgoing Matter TCP connections — the client role of
// matter.js TcpTransport (TcpTransport.ts:149-182 openChannel /
// #connect). The connection uses the same framing and handler contract
// as an inbound one. The zero value is usable.
type Dialer struct {
	// MaxPayloadSize is the maximum frame size. Zero means
	// [DefaultMaxPayloadSize] (TcpTransport.ts:62).
	MaxPayloadSize int
	// Logger receives the connections' log lines. nil means
	// slog.Default.
	Logger *slog.Logger

	recoveredPanics atomic.Uint64
}

// Dial connects to addr ("host:port"; IPv6 first, as net.Dialer
// resolves) and starts reading it, handing every message to handler.
// Close the returned connection when done.
func (d *Dialer) Dial(ctx context.Context, addr string, handler Handler) (*Conn, error) {
	if handler == nil {
		return nil, errors.New("tcp: nil handler")
	}
	var nd net.Dialer
	nc, err := nd.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("tcp: dial %s: %w", addr, err)
	}
	// An abort that fired while the connect completed must not leave
	// an orphaned connection (TcpTransport.ts:174-180).
	if err := ctx.Err(); err != nil {
		_ = nc.Close()
		return nil, fmt.Errorf("tcp: dial %s: %w", addr, err)
	}
	c := newConn(nc, d.MaxPayloadSize, false, d.Logger, &d.recoveredPanics)
	go c.serve(handler)
	return c, nil
}

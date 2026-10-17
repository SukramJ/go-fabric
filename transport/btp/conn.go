// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package btp

import (
	"fmt"
	"net"

	"github.com/SukramJ/go-fabric/transport"
)

// Addr is a BLE peer's address: the peripheral's address as the platform
// names it (a MAC address under BlueZ). Network is "ble".
type Addr struct{ Peripheral string }

// Network names the transport.
func (a Addr) Network() string { return "ble" }

// String is the peripheral's address.
func (a Addr) String() string { return a.Peripheral }

// Conn is a BTP session as a [transport.Conn]: one Matter message per
// [Conn.Send], reliable, so the layers above run no MRP over it — matter.js
// BleChannel (packages/protocol/src/ble/Ble.ts:36-41: isReliable true,
// maxPayloadSize MAX_MATTER_PAYLOAD_SIZE). Received messages arrive on the
// session's [Callbacks].Message.
type Conn struct {
	s      *Session
	remote Addr
}

var _ transport.Conn = (*Conn)(nil)

// NewConn wraps s, a session with the peer at remote.
func NewConn(s *Session, remote Addr) *Conn { return &Conn{s: s, remote: remote} }

// Session is the BTP session the connection sends on.
func (c *Conn) Session() *Session { return c.s }

// Send sends one Matter message. A message above [MaxMatterPayloadSize]
// is refused, as matter.js's exchange layer refuses one above the
// channel's maxPayloadSize.
func (c *Conn) Send(msg []byte) error {
	if len(msg) > MaxMatterPayloadSize {
		return &FlowError{Message: fmt.Sprintf("Matter message of %d bytes exceeds the BTP maximum of %d bytes", len(msg), MaxMatterPayloadSize)}
	}
	return c.s.Send(msg)
}

// Remote is the peer's address.
func (c *Conn) Remote() net.Addr { return c.remote }

// Reliable is true: MRP does not run over BTP (Ble.ts:38).
func (c *Conn) Reliable() bool { return true }

// MaxMessageSize is the largest Matter message [Conn.Send] takes.
func (c *Conn) MaxMessageSize() int { return MaxMatterPayloadSize }

// Close closes the session.
func (c *Conn) Close() error { return c.s.Close() }

// Closed is closed when the session closes.
func (c *Conn) Closed() <-chan struct{} { return c.s.Closed() }

// String names the connection for the log.
func (c *Conn) String() string { return "ble://" + c.remote.Peripheral }

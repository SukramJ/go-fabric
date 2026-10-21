// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridgetest

import (
	"errors"
	"sync"

	"github.com/SukramJ/go-fabric/bridge"
	"github.com/SukramJ/go-fabric/internal/bridgeseam"
	"github.com/SukramJ/go-fabric/transport"
)

// ErrNoConn is returned by [ServeConn] for a nil connection.
var ErrNoConn = errors.New("bridgetest: nil connection")

// ConnServer serves the Matter messages of one connection of a transport
// the bridge does not listen on itself — the BTP peripheral of
// controller/ble/bletest — through the bridge's transport-neutral receive
// path: replies go back on the connection, without MRP when it is
// reliable. A node of this module never advertises over BLE (ADR 0024,
// decision 1); this is how a test commissions one over BTP anyway.
type ConnServer struct {
	b    *bridge.Bridge
	conn transport.Conn

	// mu guards done, prev and wg.
	mu   sync.Mutex
	prev chan struct{}
	done bool
	wg   sync.WaitGroup
}

// ServeConn returns the server of conn's messages on b.
func ServeConn(b *bridge.Bridge, conn transport.Conn) (*ConnServer, error) {
	if b == nil {
		return nil, ErrNoBridge
	}
	if conn == nil {
		return nil, ErrNoConn
	}
	first := make(chan struct{})
	close(first)
	return &ConnServer{b: b, conn: conn, prev: first}, nil
}

// Deliver hands one complete Matter message to the bridge. Messages are
// handled in delivery order, each once its predecessor returned or
// released its turn — the contract of transport/tcp's ordered handler, so
// a handler that waits for the peer's answer on the same connection lets
// that answer through. Deliver does not wait for the handler.
func (s *ConnServer) Deliver(msg []byte) {
	s.mu.Lock()
	if s.done {
		s.mu.Unlock()
		return
	}
	prev := s.prev
	turn := make(chan struct{})
	s.prev = turn
	s.wg.Add(1)
	s.mu.Unlock()
	msg = append([]byte(nil), msg...)
	go func() {
		defer s.wg.Done()
		var once sync.Once
		release := func() { once.Do(func() { close(turn) }) }
		defer release()
		<-prev
		bridgeseam.ReceiveOnConn(s.b, s.conn, msg, release)
	}()
}

// Close stops delivering and evicts every session bound to the
// connection, as a dropped TCP connection does.
func (s *ConnServer) Close() {
	s.mu.Lock()
	s.done = true
	s.mu.Unlock()
	bridgeseam.ConnClosed(s.b, s.conn)
}

// Wait waits for every delivered message's handler to return.
func (s *ConnServer) Wait() { s.wg.Wait() }

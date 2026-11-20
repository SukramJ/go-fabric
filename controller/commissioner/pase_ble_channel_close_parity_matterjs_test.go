// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package commissioner

// A port of matter.js packages/node/test/node/PaseBleChannelCloseTest.ts at
// b25ed960 (#4908, #4922): a PASE session over a commissioning channel
// ends with the channel — at once while PASE runs, when the channel closed
// while the session was created, and during the commissioning flow.
// matter.js's SilentBleChannel is losingChannel; its ForwardingBleChannel,
// a TCP channel to a mock device presented as BLE, is forwardingChannel, a
// UDP manager to a node on loopback presented as a [Channel].

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/client/discovery"
	"github.com/SukramJ/go-fabric/client/exchange"
	"github.com/SukramJ/go-fabric/controller/ca"
	"github.com/SukramJ/go-fabric/transport/btp"
	"github.com/SukramJ/go-fabric/transport/message"
	"github.com/SukramJ/go-fabric/transport/udp"
)

// errBLEGone is the test channels' ClosedCause: like ble.ErrChannelClosed
// (matter.js BleChannelClosedError) it wraps btp.ErrDisconnected.
var errBLEGone = fmt.Errorf("test: BLE channel closed: %w", btp.ErrDisconnected)

// losingChannel takes the first PASE message, never answers and loses the
// link as it sends it — matter.js's SilentBleChannel (:31-61).
type losingChannel struct {
	mgr       *exchange.Manager
	closed    chan struct{}
	loseOnce  sync.Once
	lostAt    atomic.Int64
	closes    atomic.Int32
	closedAt  atomic.Int64
	closeOnce sync.Once
}

func (c *losingChannel) Send(*net.UDPAddr, []byte) error {
	c.loseOnce.Do(func() {
		c.lostAt.Store(time.Now().UnixNano())
		c.closeOnce.Do(func() { close(c.closed) })
	})
	return nil
}
func (c *losingChannel) Exchanges() *exchange.Manager { return c.mgr }
func (c *losingChannel) PeerAddress() *net.UDPAddr {
	return &net.UDPAddr{IP: net.IPv6zero, Zone: "ble-test"}
}
func (c *losingChannel) Closed() <-chan struct{} { return c.closed }
func (c *losingChannel) ClosedCause() error      { return errBLEGone }
func (c *losingChannel) Close() error {
	c.closedAt.CompareAndSwap(0, time.Now().UnixNano())
	c.closes.Add(1)
	c.closeOnce.Do(func() { close(c.closed) })
	return nil
}

// forwardingChannel is a [Channel] whose manager speaks UDP to a node: a
// complete PASE runs over a channel the test can declare lost —
// matter.js's ForwardingBleChannel (:63-112).
type forwardingChannel struct {
	mgr       *exchange.Manager
	peer      *net.UDPAddr
	closed    chan struct{}
	closeOnce sync.Once
	// onInbound, when set, is asked for each datagram the node sends, after
	// the manager took it.
	onInbound func(b []byte)
}

func (c *forwardingChannel) Exchanges() *exchange.Manager { return c.mgr }
func (c *forwardingChannel) PeerAddress() *net.UDPAddr    { return c.peer }
func (c *forwardingChannel) Closed() <-chan struct{}      { return c.closed }
func (c *forwardingChannel) ClosedCause() error           { return errBLEGone }
func (c *forwardingChannel) loseLink()                    { c.closeOnce.Do(func() { close(c.closed) }) }

func (c *forwardingChannel) Close() error {
	c.loseLink()
	return nil
}

func newForwardingChannel(t *testing.T, peer *net.UDPAddr, onInbound func(c *forwardingChannel, b []byte)) *forwardingChannel {
	t.Helper()
	l, err := udp.New(udp.Config{LocalAddr: "127.0.0.1:0", PreferIPv4: true})
	if err != nil {
		t.Fatal(err)
	}
	mgr, err := exchange.NewManager(l, exchange.Config{})
	if err != nil {
		t.Fatal(err)
	}
	c := &forwardingChannel{mgr: mgr, peer: peer, closed: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	served := make(chan struct{})
	go func() {
		defer close(served)
		_ = l.Serve(ctx, func(b []byte, src *net.UDPAddr) {
			mgr.HandleDatagram(b, src)
			if onInbound != nil {
				onInbound(c, b)
			}
		})
	}()
	t.Cleanup(func() {
		mgr.Close()
		cancel()
		<-served
	})
	return c
}

// paseStatusReport reports a datagram that is the device's StatusReport on
// the unsecured session — the end of a successful PASE.
func paseStatusReport(b []byte) bool {
	hdr, n, err := message.UnmarshalHeader(b)
	if err != nil || hdr.SessionID != 0 {
		return false
	}
	proto, _, err := message.UnmarshalProtocolHeader(b[n:])
	return err == nil && proto.ProtocolID == 0 && proto.Opcode == 0x40
}

// channelCommissioner is a commissioner whose context is a peer-less UDP
// manager: the commissioning runs over the channel.
func channelCommissioner(t *testing.T) (*Commissioner, *Fabric) {
	t.Helper()
	authority, err := ca.New(ca.Config{})
	if err != nil {
		t.Fatal(err)
	}
	fabric, err := NewFabric(authority, FabricConfig{FabricID: 7, RootNodeID: 1, VendorID: 0xFFF1})
	if err != nil {
		t.Fatal(err)
	}
	udpMgr, err := exchange.NewManager(&blackhole{closed: make(chan struct{})}, exchange.Config{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(udpMgr.Close)
	return &Commissioner{ctx: Context{Exchanges: udpMgr, Clock: exchange.SystemClock{}}, log: discard()}, fabric
}

// commissionOverForwarding commissions a node on loopback over a
// forwardingChannel and reports the PASE step's outcome.
func commissionOverForwarding(t *testing.T, onInbound func(c *forwardingChannel, b []byte)) (error, StepPhase) {
	t.Helper()
	_, addr := startNode(t, nil, "127.0.0.1:0")
	ch := newForwardingChannel(t, addr, onInbound)
	ctl := newController(t, discovery.NewNames(newFakeMDNS(), discard()), "127.0.0.1:0")
	c, fabric := ctl.com, ctl.fabric
	var pase atomic.Int32
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, err := c.Commission(ctx, Options{
		Fabric: fabric, Passcode: nodePasscode, NodeID: commissionedNode, Channel: ch, Timeout: 20 * time.Second,
		Progress: func(s Step) {
			if s.Name == StepPASE && s.Phase != StepStarted {
				pase.Store(int32(s.Phase))
			}
		},
	})
	return err, StepPhase(pase.Load())
}

func TestPaseBleChannelClose(t *testing.T) {
	t.Parallel()

	t.Run("ends the PASE session as soon as its BLE channel closes and fails after the channel is released", func(t *testing.T) {
		t.Parallel()
		ch := &losingChannel{closed: make(chan struct{})}
		mgr, err := exchange.NewManager(ch, exchange.Config{Reliable: true})
		if err != nil {
			t.Fatal(err)
		}
		defer mgr.Close()
		ch.mgr = mgr
		c, fabric := channelCommissioner(t)
		done := make(chan error, 1)
		go func() {
			_, err := c.Commission(context.Background(), Options{Fabric: fabric, Passcode: 20202021, Channel: ch, Timeout: 90 * time.Second})
			done <- err
		}()
		select {
		case err := <-done:
			if !errors.Is(err, errBLEGone) {
				t.Fatalf("Commission: %v, want the channel's close as the cause", err)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("PASE waited for the device after its channel closed")
		}
		if lost, closedAt := ch.lostAt.Load(), ch.closedAt.Load(); closedAt == 0 || time.Duration(closedAt-lost) >= 5*time.Second {
			t.Fatalf("the channel was released %v after its loss", time.Duration(closedAt-lost))
		}
		if n := ch.closes.Load(); n != 1 {
			t.Fatalf("channel closed %d times before the failure", n)
		}
	})

	t.Run("does not use a PASE session whose BLE channel closed while the session was created", func(t *testing.T) {
		t.Parallel()
		var once sync.Once
		err, phase := commissionOverForwarding(t, func(c *forwardingChannel, b []byte) {
			if paseStatusReport(b) {
				once.Do(c.loseLink)
			}
		})
		if !errors.Is(err, errBLEGone) {
			t.Fatalf("Commission: %v, want the channel's close as the cause", err)
		}
		if phase != StepFailed {
			t.Fatalf("PASE step %v, want failed: the commissioner used a session whose channel had closed", phase)
		}
	})

	t.Run("closes the PASE session when its BLE channel closes during the commissioning flow", func(t *testing.T) {
		t.Parallel()
		var pased atomic.Bool
		var once sync.Once
		err, phase := commissionOverForwarding(t, func(c *forwardingChannel, b []byte) {
			if paseStatusReport(b) {
				pased.Store(true)
				return
			}
			// The first datagram of the flow's: the link goes.
			if pased.Load() {
				once.Do(c.loseLink)
			}
		})
		if phase != StepSucceeded {
			t.Fatalf("PASE step %v, want succeeded", phase)
		}
		if !errors.Is(err, errBLEGone) {
			t.Fatalf("Commission: %v, want the channel's close as the cause", err)
		}
	})
}

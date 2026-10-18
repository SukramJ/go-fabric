// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package bletest is an in-memory BLE stack for tests: a [Central] that
// implements ble.Central and the [Peripheral]s it reaches, each a GATT
// server of the Matter service whose BTP side is a transport/btp
// peripheral session. Writes and indications travel asynchronously, in
// order, on goroutines of their own, as a radio delivers them.
//
// The peripheral's GATT behaviour follows matter.js's BLE server
// (packages/nodejs-ble/src/BlenoBleServer.ts at 0ceb202d): the handshake
// request arrives on C1, and the session — with its handshake response —
// starts when the central subscribes to C2.
package bletest

import (
	"context"
	"errors"
	"sync"

	"github.com/SukramJ/go-fabric/controller/ble"
	"github.com/SukramJ/go-fabric/transport/btp"
)

// ErrUnknownPeripheral is a Connect to an address no peripheral has.
var ErrUnknownPeripheral = errors.New("bletest: unknown peripheral")

// Central is an in-memory ble.Central.
type Central struct {
	mu          sync.Mutex
	callback    func(ble.Peripheral, []byte)
	peripherals map[string]*Peripheral
	order       []string
	scanning    bool
	starts      int
	stops       int
}

var _ ble.Central = (*Central)(nil)

// NewCentral returns a central reaching the peripherals.
func NewCentral(ps ...*Peripheral) *Central {
	c := &Central{peripherals: map[string]*Peripheral{}}
	for _, p := range ps {
		c.Add(p)
	}
	return c
}

// Add makes p reachable; it advertises while the central scans.
func (c *Central) Add(p *Peripheral) {
	c.mu.Lock()
	if _, ok := c.peripherals[p.Address]; !ok {
		c.order = append(c.order, p.Address)
	}
	c.peripherals[p.Address] = p
	scanning := c.scanning
	c.mu.Unlock()
	if scanning {
		c.Advertise(p.Address)
	}
}

// SetDiscoveryCallback implements ble.Central.
func (c *Central) SetDiscoveryCallback(f func(ble.Peripheral, []byte)) {
	c.mu.Lock()
	c.callback = f
	c.mu.Unlock()
}

// StartScanning implements ble.Central: every peripheral advertises once.
func (c *Central) StartScanning(context.Context) error {
	c.mu.Lock()
	c.scanning = true
	c.starts++
	order := append([]string(nil), c.order...)
	c.mu.Unlock()
	for _, a := range order {
		c.Advertise(a)
	}
	return nil
}

// StopScanning implements ble.Central.
func (c *Central) StopScanning(context.Context) error {
	c.mu.Lock()
	c.scanning = false
	c.stops++
	c.mu.Unlock()
	return nil
}

// Scanning reports whether the central scans, and how often it was asked
// to start and to stop.
func (c *Central) Scanning() (scanning bool, starts, stops int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.scanning, c.starts, c.stops
}

// Advertise reports the peripheral's advertisement to the discovery
// callback.
func (c *Central) Advertise(address string) {
	c.mu.Lock()
	p := c.peripherals[address]
	cb := c.callback
	c.mu.Unlock()
	if p == nil || cb == nil {
		return
	}
	cb(ble.Peripheral{Address: p.Address}, p.ServiceData())
}

// Connect implements ble.Central.
func (c *Central) Connect(_ context.Context, p ble.Peripheral) (ble.Connection, error) {
	c.mu.Lock()
	per := c.peripherals[p.Address]
	c.mu.Unlock()
	if per == nil {
		return nil, ErrUnknownPeripheral
	}
	return per.accept(), nil
}

// Peripheral is a commissionable device's BLE side: its advertisement and
// the GATT server of the Matter service.
type Peripheral struct {
	// Address is the peripheral's address.
	Address string
	// Discriminator, VendorID and ProductID are advertised.
	Discriminator, VendorID, ProductID uint16
	// ATTMTU is the connection's ATT_MTU; 0 is btp.MaximumATTMTU.
	ATTMTU int
	// Serve is called once per BTP session with its connection; the
	// function it returns receives every Matter message of the session,
	// in order, and closed is called when the session ends. Nil drops
	// every message.
	Serve func(conn *btp.Conn) (handle func(msg []byte), closed func())
}

// ServiceData is the Matter service data the peripheral advertises
// (btp.EncodeAdvertisementData without the AD structure headers).
func (p *Peripheral) ServiceData() []byte {
	return btp.EncodeAdvertisementData(p.Discriminator, p.VendorID, p.ProductID, false, false)[7:]
}

func (p *Peripheral) attMTU() int {
	if p.ATTMTU == 0 {
		return btp.MaximumATTMTU
	}
	return p.ATTMTU
}

// pipe delivers byte slices in order on one goroutine until stopped.
type pipe struct {
	ch   chan []byte
	stop chan struct{}
}

func newPipe(deliver func([]byte)) *pipe {
	p := &pipe{ch: make(chan []byte, 4096), stop: make(chan struct{})}
	go func() {
		for {
			select {
			case b := <-p.ch:
				deliver(b)
			case <-p.stop:
				return
			}
		}
	}()
	return p
}

func (p *pipe) send(b []byte) bool {
	select {
	case <-p.stop:
		return false
	default:
	}
	select {
	case p.ch <- append([]byte(nil), b...):
		return true
	case <-p.stop:
		return false
	}
}

// connection is one GATT connection, both ends.
type connection struct {
	p *Peripheral

	mu         sync.Mutex
	onC2       func([]byte)
	request    []byte
	started    bool
	session    *btp.Session
	handle     func([]byte)
	closedHook func()
	messages   *pipe
	toPeriph   *pipe
	toCentral  *pipe
	gone       chan struct{}
	goneOnce   sync.Once
}

func (p *Peripheral) accept() *connection {
	c := &connection{p: p, gone: make(chan struct{})}
	c.toPeriph = newPipe(c.peripheralReceive)
	c.toCentral = newPipe(func(b []byte) {
		c.mu.Lock()
		on := c.onC2
		c.mu.Unlock()
		if on != nil {
			on(b)
		}
	})
	return c
}

// peripheralReceive is a C1 write arriving at the GATT server: the
// handshake request first, BTP packets after.
func (c *connection) peripheralReceive(b []byte) {
	c.mu.Lock()
	s := c.session
	if s == nil {
		c.request = b
		c.mu.Unlock()
		c.maybeStart()
		return
	}
	c.mu.Unlock()
	_ = s.HandleIncoming(b)
}

// maybeStart starts the session once the request arrived and C2 is
// subscribed, whichever comes last.
func (c *connection) maybeStart() {
	c.mu.Lock()
	if c.started || c.request == nil || c.onC2 == nil {
		c.mu.Unlock()
		return
	}
	c.started = true
	c.mu.Unlock()
	c.startSession()
}

// startSession answers the stored handshake request once C2 is subscribed.
func (c *connection) startSession() {
	// The lock is held until the session is in place, so the central's
	// first packet cannot pass for a second request.
	c.mu.Lock()
	defer c.mu.Unlock()
	req := c.request
	s, err := btp.NewPeripheral(btp.SegmentSizeFromATTMTU(c.p.attMTU()), req, btp.Callbacks{
		Write: func(b []byte) error {
			if !c.toCentral.send(b) {
				return btp.ErrDisconnected
			}
			return nil
		},
		Disconnect: func() error { go c.drop(); return nil },
		Message: func(m []byte) error {
			c.mu.Lock()
			q := c.messages
			c.mu.Unlock()
			if q != nil {
				q.send(m)
			}
			return nil
		},
	}, btp.Options{})
	if err != nil {
		go c.drop()
		return
	}
	conn := btp.NewConn(s, btp.Addr{Peripheral: "central-of-" + c.p.Address})
	c.session = s
	if c.p.Serve != nil {
		c.handle, c.closedHook = c.p.Serve(conn)
	}
	handle := c.handle
	c.messages = newPipe(func(m []byte) {
		if handle != nil {
			handle(m)
		}
	})
}

func (c *connection) drop() {
	c.goneOnce.Do(func() {
		close(c.gone)
		close(c.toPeriph.stop)
		close(c.toCentral.stop)
		c.mu.Lock()
		s, hook, msgs := c.session, c.closedHook, c.messages
		c.mu.Unlock()
		if s != nil {
			go func() { _ = s.Close() }()
		}
		if msgs != nil {
			close(msgs.stop)
		}
		if hook != nil {
			hook()
		}
	})
}

// ATTMTU implements ble.Connection.
func (c *connection) ATTMTU() int { return c.p.attMTU() }

// WriteC1 implements ble.Connection.
func (c *connection) WriteC1(_ context.Context, data []byte) error {
	if !c.toPeriph.send(data) {
		return btp.ErrDisconnected
	}
	return nil
}

// SubscribeC2 implements ble.Connection.
func (c *connection) SubscribeC2(_ context.Context, on func([]byte)) error {
	select {
	case <-c.gone:
		return btp.ErrDisconnected
	default:
	}
	c.mu.Lock()
	c.onC2 = on
	c.mu.Unlock()
	c.maybeStart()
	return nil
}

// UnsubscribeC2 implements ble.Connection: the peripheral's session ends.
func (c *connection) UnsubscribeC2(context.Context) error {
	c.mu.Lock()
	c.onC2 = nil
	s := c.session
	c.mu.Unlock()
	if s != nil {
		go func() { _ = s.Close() }()
	}
	return nil
}

// ReadC3 implements ble.Connection: no additional data.
func (c *connection) ReadC3(context.Context) ([]byte, error) {
	return nil, errors.New("bletest: no C3 data")
}

// Disconnect implements ble.Connection.
func (c *connection) Disconnect() error {
	c.drop()
	return nil
}

// Disconnected implements ble.Connection.
func (c *connection) Disconnected() <-chan struct{} { return c.gone }

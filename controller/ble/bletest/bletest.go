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
// starts when the central subscribes to C2. A peripheral can also play
// the stalled peer matter.js's channel tests model
// (packages/nodejs-ble/test/NobleBleChannelTest.ts:208-346 at 862ad75f,
// handshakeOnlyMatterService): it completes the handshake at the offered
// segment size and then answers no BTP packet — see [Peripheral.Stall]
// and [Faults].
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
	// mu guards callback, order, peripherals, scanning, starts and stops.
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
// the GATT server of the Matter service. Set its fields before the central
// connects.
type Peripheral struct {
	// Address is the peripheral's address.
	Address string
	// Discriminator, VendorID and ProductID are advertised.
	Discriminator, VendorID, ProductID uint16
	// ATTMTU is the connection's ATT_MTU; 0 is btp.MaximumATTMTU.
	ATTMTU int
	// ATTMTUReady, when set, is the ATT_MTU exchange still in flight when
	// a connection opens: the connection reports an unknown ATT_MTU (0)
	// and implements ble.MTUExchange until the test closes it, after which
	// it reports ATTMTU. Nil is an exchange complete at connect.
	ATTMTUReady <-chan struct{}
	// Serve is called once per BTP session with its connection; the
	// function it returns receives every Matter message of the session,
	// in order, and closed is called when the session ends. Nil drops
	// every message.
	Serve func(conn *btp.Conn) (handle func(msg []byte), closed func())
	// Stall, when set, is asked at every handshake with the segment size
	// the central offered. A nil result serves the session; a receiver
	// makes the peripheral complete the handshake at the offered size and
	// then answer no BTP packet of that session, each one handed to the
	// receiver instead, the connection kept — the stalled peripherals
	// matter.js's renegotiation workaround targets
	// (packages/nodejs-ble/test/NobleBleChannelTest.ts:208-212 at 862ad75f).
	Stall func(offeredSegmentSize int) (withhold func(packet []byte))
	// Faults are the GATT failures every connection shows.
	Faults Faults

	// mu guards conns, handshakes and unsubscribes.
	mu           sync.Mutex
	conns        []*connection
	handshakes   []int
	unsubscribes int
}

// Faults are GATT failures a peripheral or its platform shows, after the
// fakes of matter.js's channel tests
// (packages/nodejs-ble/test/NobleBleChannelTest.ts:212-346 at 862ad75f).
// Writes are counted per connection, from 1.
type Faults struct {
	// UnsubscribeC2 is the error every UnsubscribeC2 returns, the
	// subscription left standing; nil succeeds.
	UnsubscribeC2 error
	// WithholdHandshakeFrom leaves the handshake request of that number
	// and every later one unanswered, so the central waits out its
	// timer; 0 answers every one.
	WithholdHandshakeFrom int
	// HangHandshakeWriteFrom leaves the C1 write of the handshake request
	// of that number and every later one pending until the write's
	// context ends or the connection drops; 0 writes every one.
	HangHandshakeWriteFrom int
	// FailDataWrite fails the BTP data write of that number as a lost
	// connection (btp.ErrDisconnected), the packet dropped; 0 fails none.
	FailDataWrite int
	// HangDataWriteFrom leaves the BTP data write of that number and every
	// later one pending until the write's context ends or the connection
	// drops; 0 writes every one.
	HangDataWriteFrom int
}

// Observation is what the peripheral's GATT server saw.
type Observation struct {
	// HandshakeSegmentSizes are the segment sizes the handshake requests
	// offered, in order, over every connection.
	HandshakeSegmentSizes []int
	// Unsubscribes counts the UnsubscribeC2 calls over every connection.
	Unsubscribes int
}

// Observed reports what the peripheral saw so far.
func (p *Peripheral) Observed() Observation {
	p.mu.Lock()
	defer p.mu.Unlock()
	return Observation{HandshakeSegmentSizes: append([]int(nil), p.handshakes...), Unsubscribes: p.unsubscribes}
}

// DropConnections drops every connection the peripheral accepted, as a
// peripheral that goes away does.
func (p *Peripheral) DropConnections() {
	p.mu.Lock()
	conns := append([]*connection(nil), p.conns...)
	p.mu.Unlock()
	for _, c := range conns {
		c.drop()
	}
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

	// mu guards closedHook, handle, messages, onC2, request, session,
	// started, withhold, handshakeWrites and dataWrites.
	mu         sync.Mutex
	onC2       func([]byte)
	request    []byte
	started    bool
	session    *btp.Session
	withhold   func([]byte)
	handle     func([]byte)
	closedHook func()
	messages   *pipe
	toPeriph   *pipe
	toCentral  *pipe
	gone       chan struct{}
	goneOnce   sync.Once

	handshakeWrites, dataWrites int
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
	p.mu.Lock()
	p.conns = append(p.conns, c)
	p.mu.Unlock()
	return c
}

// peripheralReceive is a C1 write arriving at the GATT server: the
// handshake request first, BTP packets after — withheld for a stalled
// session.
func (c *connection) peripheralReceive(b []byte) {
	c.mu.Lock()
	if w := c.withhold; w != nil {
		c.mu.Unlock()
		w(b)
		return
	}
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

// startSession answers the stored handshake request once C2 is subscribed
// — or withholds the answer, or stalls the session, as the peripheral's
// faults and Stall say.
func (c *connection) startSession() {
	// The lock is held until the session is in place, so the central's
	// first packet cannot pass for a second request.
	c.mu.Lock()
	defer c.mu.Unlock()
	req := c.request
	if from := c.p.Faults.WithholdHandshakeFrom; from > 0 && c.handshakeWrites >= from {
		return
	}
	if c.p.Stall != nil {
		decoded, err := btp.DecodeHandshakeRequest(req)
		if err != nil {
			go c.drop()
			return
		}
		if withhold := c.p.Stall(int(decoded.ATTMTU)); withhold != nil {
			c.withhold = withhold
			resp := btp.EncodeHandshakeResponse(btp.HandshakeResponse{Version: 4, ATTMTU: decoded.ATTMTU, WindowSize: 4})
			c.toCentral.send(resp)
			return
		}
	}
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

// ATTMTU implements ble.Connection: 0 while the peripheral's ATT_MTU
// exchange is still in flight.
func (c *connection) ATTMTU() int {
	if ready := c.p.ATTMTUReady; ready != nil {
		select {
		case <-ready:
		default:
			return 0
		}
	}
	return c.p.attMTU()
}

// ATTMTUExchanged implements ble.MTUExchange: the peripheral's ATTMTUReady,
// or an exchange complete at connect.
func (c *connection) ATTMTUExchanged() <-chan struct{} {
	if ready := c.p.ATTMTUReady; ready != nil {
		return ready
	}
	done := make(chan struct{})
	close(done)
	return done
}

// isHandshakeRequest tells a handshake request from a BTP data packet by
// its decoding, as the fake service of NobleBleChannelTest.ts does by the
// header bytes (:238 at 862ad75f).
func isHandshakeRequest(data []byte) bool {
	_, err := btp.DecodeHandshakeRequest(data)
	return err == nil
}

// WriteC1 implements ble.Connection, with the peripheral's write faults.
func (c *connection) WriteC1(ctx context.Context, data []byte) error {
	var hang bool
	if isHandshakeRequest(data) {
		c.mu.Lock()
		c.handshakeWrites++
		n := c.handshakeWrites
		c.mu.Unlock()
		if decoded, err := btp.DecodeHandshakeRequest(data); err == nil {
			c.p.mu.Lock()
			c.p.handshakes = append(c.p.handshakes, int(decoded.ATTMTU))
			c.p.mu.Unlock()
		}
		hang = c.p.Faults.HangHandshakeWriteFrom > 0 && n >= c.p.Faults.HangHandshakeWriteFrom
	} else {
		c.mu.Lock()
		c.dataWrites++
		n := c.dataWrites
		c.mu.Unlock()
		if n == c.p.Faults.FailDataWrite {
			return btp.ErrDisconnected
		}
		hang = c.p.Faults.HangDataWriteFrom > 0 && n >= c.p.Faults.HangDataWriteFrom
	}
	if hang {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-c.gone:
			return btp.ErrDisconnected
		}
	}
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

// UnsubscribeC2 implements ble.Connection: the peripheral's BTP session
// ends (§4.19.4.10) and the GATT server takes a new handshake, as
// matter.js's server closes on the unsubscribe (BlenoBleServer.ts:81-84 at
// 862ad75f). A served session's end drops the connection, as that server's
// close does; a stalled session's end leaves it, as the fake peer of
// NobleBleChannelTest.ts does.
func (c *connection) UnsubscribeC2(context.Context) error {
	c.p.mu.Lock()
	c.p.unsubscribes++
	c.p.mu.Unlock()
	if err := c.p.Faults.UnsubscribeC2; err != nil {
		return err
	}
	c.mu.Lock()
	c.onC2 = nil
	s := c.session
	c.session, c.request, c.started, c.withhold = nil, nil, false, nil
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

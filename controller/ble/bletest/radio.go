// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bletest

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/controller/ble"
	"github.com/SukramJ/go-fabric/transport/btp"
)

// ErrNoService is a Connect to a [Radio] that serves no Matter GATT
// service: its node is not on the radio.
var ErrNoService = errors.New("bletest: no Matter GATT service")

// ErrServing is a second concurrent [Radio.Serve].
var ErrServing = errors.New("bletest: the Matter GATT service is served already")

// Radio is a node's Bluetooth radio in the in-memory model: a
// contract.BLEPeripheral a node is built with (node.Network.BLE,
// bridge.Config.BLE), which the centrals reach as its [Peripheral]. While
// the node serves the Matter GATT service on it ([Radio.Serve]) every
// connection a central opens reaches the node's BTP server through the
// contract.BLEConnection the radio hands to Serve's accept — the node's
// production path — and the peripheral advertises only while the node
// does, the payload the node gives. The connections deliver writes,
// subscriptions and indications asynchronously, in order, as the
// peripheral's own connections do; an indication completes once the
// central's subscriber received it. It is safe for concurrent use.
type Radio struct {
	p *Peripheral

	// mu guards adv, ads, stops, serving, accept, c3 and links.
	mu      sync.Mutex
	adv     *contract.BLEAdvertisement
	ads     []contract.BLEAdvertisement
	stops   int
	serving bool
	accept  func(contract.BLEConnection)
	c3      []byte
	links   []*radioLink
}

var _ contract.BLEPeripheral = (*Radio)(nil)

// Radio returns the peripheral as a node's radio, the same one on every
// call: from then on a central's connection reaches the node that serves
// on the radio, in place of the peripheral's own BTP side (Serve, Stall
// and Faults are not used), and the peripheral advertises what the node
// advertises. Address and ATTMTU (with ATTMTUReady) keep their meaning;
// Discriminator, VendorID and ProductID describe the device to the
// models that read them but are not what the central's scan reports.
func (p *Peripheral) Radio() *Radio {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.radio == nil {
		p.radio = &Radio{p: p}
	}
	return p.radio
}

// Advertise implements contract.BLEPeripheral: the advertisement replaces
// the running one, and every central that scans sees it at once.
func (r *Radio) Advertise(_ context.Context, adv contract.BLEAdvertisement) error {
	if _, ok := matterServiceData(adv.Data); !ok {
		return fmt.Errorf("bletest: advertisement %x carries no Matter service data", adv.Data)
	}
	a := contract.BLEAdvertisement{Data: bytes.Clone(adv.Data), Interval: adv.Interval}
	r.mu.Lock()
	r.adv = &a
	r.ads = append(r.ads, a)
	r.mu.Unlock()
	r.p.mu.Lock()
	centrals := append([]*Central(nil), r.p.centrals...)
	r.p.mu.Unlock()
	for _, c := range centrals {
		if c.isScanning() {
			c.Advertise(r.p.Address)
		}
	}
	return nil
}

// StopAdvertising implements contract.BLEPeripheral.
func (r *Radio) StopAdvertising(context.Context) error {
	r.mu.Lock()
	if r.adv != nil {
		r.stops++
	}
	r.adv = nil
	r.mu.Unlock()
	return nil
}

// Serve implements contract.BLEPeripheral: the service stays registered
// until ctx ends, then every connection is dropped. A second concurrent
// Serve returns [ErrServing].
func (r *Radio) Serve(ctx context.Context, c3 []byte, accept func(contract.BLEConnection)) error {
	r.mu.Lock()
	if r.serving {
		r.mu.Unlock()
		return ErrServing
	}
	r.serving, r.accept, r.c3 = true, accept, bytes.Clone(c3)
	r.mu.Unlock()
	<-ctx.Done()
	r.mu.Lock()
	r.serving, r.accept, r.c3 = false, nil, nil
	links := r.links
	r.links = nil
	r.mu.Unlock()
	for _, l := range links {
		l.drop()
	}
	return nil
}

// Advertising reports the running advertisement.
func (r *Radio) Advertising() (contract.BLEAdvertisement, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.adv == nil {
		return contract.BLEAdvertisement{}, false
	}
	return *r.adv, true
}

// Advertisements are every advertisement the node started, in order, and
// how often it stopped a running one.
func (r *Radio) Advertisements() (ads []contract.BLEAdvertisement, stops int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]contract.BLEAdvertisement(nil), r.ads...), r.stops
}

// Serving reports whether the node serves the Matter GATT service, and
// how many centrals are connected to it.
func (r *Radio) Serving() (serving bool, connections int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.serving, len(r.links)
}

// serviceData is the Matter service data of the running advertisement,
// nil without one.
func (r *Radio) serviceData() []byte {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.adv == nil {
		return nil
	}
	d, _ := matterServiceData(r.adv.Data)
	return d
}

// matterServiceData finds the service data of the Matter service (16-bit
// UUID 0xFFF6) among the AD structures of data.
func matterServiceData(data []byte) ([]byte, bool) {
	for len(data) > 0 {
		n := int(data[0])
		if n == 0 || n+1 > len(data) {
			return nil, false
		}
		ad := data[1 : n+1]
		if ad[0] == 0x16 && len(ad) >= 3 && ad[1] == 0xf6 && ad[2] == 0xff {
			return bytes.Clone(ad[3:]), true
		}
		data = data[n+1:]
	}
	return nil, false
}

// connect opens a connection to the radio for a central: the node's
// accept runs before the central holds it, so the node's handlers see
// every event.
func (r *Radio) connect() (ble.Connection, error) {
	r.mu.Lock()
	if !r.serving {
		r.mu.Unlock()
		return nil, ErrNoService
	}
	accept := r.accept
	l := newRadioLink(r)
	r.links = append(r.links, l)
	r.mu.Unlock()
	accept(radioPeripheralSide{l})
	return radioCentralSide{l}, nil
}

// forget removes a dropped link.
func (r *Radio) forget(l *radioLink) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, x := range r.links {
		if x == l {
			r.links = append(r.links[:i], r.links[i+1:]...)
			return
		}
	}
}

// eventPipe runs functions in order on one goroutine until stopped.
type eventPipe struct {
	ch   chan func()
	stop chan struct{}
}

func newEventPipe() *eventPipe {
	p := &eventPipe{ch: make(chan func(), 4096), stop: make(chan struct{})}
	go func() {
		for {
			select {
			case f := <-p.ch:
				f()
			case <-p.stop:
				return
			}
		}
	}()
	return p
}

func (p *eventPipe) post(f func()) bool {
	select {
	case <-p.stop:
		return false
	default:
	}
	select {
	case p.ch <- f:
		return true
	case <-p.stop:
		return false
	}
}

// radioLink is one connection to a radio, both ends.
type radioLink struct {
	r *Radio

	// mu guards onWrite, onSub, onUnsub and onC2.
	mu       sync.Mutex
	onWrite  func([]byte)
	onSub    func(int)
	onUnsub  func()
	onC2     func([]byte)
	toPeriph *eventPipe
	toCent   *eventPipe
	gone     chan struct{}
	goneOnce sync.Once
}

func newRadioLink(r *Radio) *radioLink {
	return &radioLink{r: r, toPeriph: newEventPipe(), toCent: newEventPipe(), gone: make(chan struct{})}
}

func (l *radioLink) drop() {
	l.goneOnce.Do(func() {
		close(l.gone)
		close(l.toPeriph.stop)
		close(l.toCent.stop)
		l.r.forget(l)
	})
}

// radioPeripheralSide is the link as the node's GATT server sees it.
type radioPeripheralSide struct{ l *radioLink }

var _ contract.BLEConnection = radioPeripheralSide{}

func (s radioPeripheralSide) Remote() string { return "central-of-" + s.l.r.p.Address }

func (s radioPeripheralSide) OnWrite(f func([]byte)) {
	s.l.mu.Lock()
	s.l.onWrite = f
	s.l.mu.Unlock()
}

func (s radioPeripheralSide) OnSubscribe(f func(int)) {
	s.l.mu.Lock()
	s.l.onSub = f
	s.l.mu.Unlock()
}

func (s radioPeripheralSide) OnUnsubscribe(f func()) {
	s.l.mu.Lock()
	s.l.onUnsub = f
	s.l.mu.Unlock()
}

// Indicate delivers value to the central's subscriber and returns once it
// did — the confirmation.
func (s radioPeripheralSide) Indicate(ctx context.Context, value []byte) error {
	value = bytes.Clone(value)
	confirmed := make(chan struct{})
	if !s.l.toCent.post(func() {
		s.l.mu.Lock()
		on := s.l.onC2
		s.l.mu.Unlock()
		if on != nil {
			on(value)
		}
		close(confirmed)
	}) {
		return btp.ErrDisconnected
	}
	select {
	case <-confirmed:
		return nil
	case <-s.l.gone:
		return btp.ErrDisconnected
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s radioPeripheralSide) Disconnect() error { s.l.drop(); return nil }

func (s radioPeripheralSide) Disconnected() <-chan struct{} { return s.l.gone }

// radioCentralSide is the link as the central sees it.
type radioCentralSide struct{ l *radioLink }

var _ ble.Connection = radioCentralSide{}

func (s radioCentralSide) ATTMTU() int {
	if ready := s.l.r.p.ATTMTUReady; ready != nil {
		select {
		case <-ready:
		default:
			return 0
		}
	}
	return s.l.r.p.attMTU()
}

// ATTMTUExchanged implements ble.MTUExchange, as the peripheral's own
// connections do.
func (s radioCentralSide) ATTMTUExchanged() <-chan struct{} {
	if ready := s.l.r.p.ATTMTUReady; ready != nil {
		return ready
	}
	done := make(chan struct{})
	close(done)
	return done
}

func (s radioCentralSide) WriteC1(_ context.Context, data []byte) error {
	data = bytes.Clone(data)
	if !s.l.toPeriph.post(func() {
		s.l.mu.Lock()
		on := s.l.onWrite
		s.l.mu.Unlock()
		if on != nil {
			on(data)
		}
	}) {
		return btp.ErrDisconnected
	}
	return nil
}

func (s radioCentralSide) SubscribeC2(_ context.Context, on func([]byte)) error {
	s.l.mu.Lock()
	s.l.onC2 = on
	s.l.mu.Unlock()
	mtu := s.ATTMTU()
	if mtu == 0 {
		mtu = s.l.r.p.attMTU()
	}
	if !s.l.toPeriph.post(func() {
		s.l.mu.Lock()
		h := s.l.onSub
		s.l.mu.Unlock()
		if h != nil {
			h(mtu)
		}
	}) {
		return btp.ErrDisconnected
	}
	return nil
}

func (s radioCentralSide) UnsubscribeC2(context.Context) error {
	s.l.mu.Lock()
	s.l.onC2 = nil
	s.l.mu.Unlock()
	if !s.l.toPeriph.post(func() {
		s.l.mu.Lock()
		h := s.l.onUnsub
		s.l.mu.Unlock()
		if h != nil {
			h()
		}
	}) {
		return btp.ErrDisconnected
	}
	return nil
}

func (s radioCentralSide) ReadC3(context.Context) ([]byte, error) {
	s.l.r.mu.Lock()
	defer s.l.r.mu.Unlock()
	return bytes.Clone(s.l.r.c3), nil
}

func (s radioCentralSide) Disconnect() error { s.l.drop(); return nil }

func (s radioCentralSide) Disconnected() <-chan struct{} { return s.l.gone }

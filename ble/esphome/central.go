// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package esphome

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/SukramJ/go-fabric/clock"
	"github.com/SukramJ/go-fabric/controller/ble"
)

// The central's defaults.
const (
	// DefaultConnectTimeout bounds NewCentral's first connection to each
	// proxy.
	DefaultConnectTimeout = 30 * time.Second
	// DefaultSilenceTimeout is how long a subscribed proxy may forward no
	// advertisement before the central presumes another client took it.
	DefaultSilenceTimeout = 30 * time.Second
	// DefaultClientInfo is the client name the proxy logs.
	DefaultClientInfo = "go-fabric"
	// sightingFreshness is how far back a proxy's report of a peripheral
	// counts when Connect picks the proxy with the strongest signal.
	sightingFreshness = 30 * time.Second
)

// Options configure [NewCentral]; the zero value is the system clock, no
// log and net.Dialer.
type Options struct {
	// Logger receives the central's diagnostics.
	Logger *slog.Logger
	// Clock paces the timeouts, the keep-alive and the reconnects.
	Clock clock.Clock
	// Dial opens the TCP connection to a proxy.
	Dial func(ctx context.Context, network, address string) (net.Conn, error)
	// ClientInfo is the name the central gives itself in HelloRequest.
	ClientInfo string
	// ConnectTimeout bounds the first connection to each proxy.
	ConnectTimeout time.Duration
	// SilenceTimeout is how long a subscribed proxy may stay silent
	// before the central presumes its subscription taken.
	SilenceTimeout time.Duration
}

// sighting is one proxy's last report of a peripheral.
type sighting struct {
	rssi     int32
	addrType uint32
	at       time.Time
}

// Central is a [ble.Central] over ESPHome Bluetooth proxies. Scanning runs
// on every proxy; a peripheral two proxies see is reported by both, and
// Connect goes through the proxy that last saw it with the strongest
// signal. A proxy that drops is reconnected with backoff; it does not fail
// the central. Close releases the proxies.
type Central struct {
	opts    Options
	log     *slog.Logger
	clk     clock.Clock
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	proxies []*proxy

	// mu guards callback, scanning and sightings.
	mu        sync.Mutex
	callback  func(ble.Peripheral, []byte)
	scanning  bool
	sightings map[uint64]map[*proxy]sighting
}

var (
	_ ble.Central      = (*Central)(nil)
	_ ble.Reachability = (*Central)(nil)
)

// NewCentral connects to the proxies and returns the central over them.
// Each proxy's first connection is made here: a proxy that refuses — a
// wrong or missing key, no active connections — fails NewCentral naming
// it, and so does every proxy being unreachable; a proxy that is only
// unreachable while another answers is retried in the background.
func NewCentral(proxies []Proxy, opts Options) (*Central, error) {
	if len(proxies) == 0 {
		return nil, ErrNoProxies
	}
	if opts.Logger == nil {
		opts.Logger = slog.New(slog.DiscardHandler)
	}
	if opts.Clock == nil {
		opts.Clock = clock.System{}
	}
	if opts.Dial == nil {
		var d net.Dialer
		opts.Dial = d.DialContext
	}
	if opts.ClientInfo == "" {
		opts.ClientInfo = DefaultClientInfo
	}
	if opts.ConnectTimeout <= 0 {
		opts.ConnectTimeout = DefaultConnectTimeout
	}
	if opts.SilenceTimeout <= 0 {
		opts.SilenceTimeout = DefaultSilenceTimeout
	}
	c := &Central{opts: opts, log: opts.Logger, clk: opts.Clock, sightings: map[uint64]map[*proxy]sighting{}}
	c.ctx, c.cancel = context.WithCancel(context.Background())
	for _, cfg := range proxies {
		p, err := newProxy(c, cfg)
		if err != nil {
			return nil, err
		}
		c.proxies = append(c.proxies, p)
	}
	first := make([]*apiConn, len(c.proxies))
	errs := make([]error, len(c.proxies))
	var wg sync.WaitGroup
	ctx, cancel := clock.WithTimeout(c.ctx, c.clk, opts.ConnectTimeout)
	for i, p := range c.proxies {
		wg.Go(func() { first[i], errs[i] = p.dial(ctx) })
	}
	wg.Wait()
	cancel()
	var failed []error
	refused := false
	for i, err := range errs {
		if err != nil {
			failed = append(failed, fmt.Errorf("proxy %s: %w", c.proxies[i].name, err))
			refused = refused || permanent(err)
		}
	}
	if refused || len(failed) == len(c.proxies) {
		for _, a := range first {
			if a != nil {
				a.close(context.Canceled)
			}
		}
		c.cancel()
		return nil, errors.Join(failed...)
	}
	for _, err := range failed {
		c.log.Warn("ble.esphome.proxy_unreachable", slog.Any("err", err))
	}
	for i, p := range c.proxies {
		c.wg.Add(1)
		go p.run(first[i])
	}
	return c, nil
}

// Close disconnects from every proxy; the connections through them are
// gone.
func (c *Central) Close() error {
	c.cancel()
	c.wg.Wait()
	return nil
}

func (c *Central) isScanning() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.scanning
}

// SetDiscoveryCallback implements ble.Central.
func (c *Central) SetDiscoveryCallback(f func(ble.Peripheral, []byte)) {
	c.mu.Lock()
	c.callback = f
	c.mu.Unlock()
}

// StartScanning implements ble.Central: every connected proxy is
// subscribed to its advertisements; the proxies scan continuously on their
// own. A scan that starts afresh clears the busy marks, so a new pairing
// may take a proxy again; asked again while a scan runs, it fails with
// ErrProxyBusy when every proxy is busy.
func (c *Central) StartScanning(context.Context) error {
	c.mu.Lock()
	fresh := !c.scanning
	c.scanning = true
	c.mu.Unlock()
	busy := 0
	for _, p := range c.proxies {
		if fresh {
			p.reset()
		}
		p.mu.Lock()
		if p.busy {
			busy++
		}
		p.mu.Unlock()
		p.subscribe()
	}
	if busy == len(c.proxies) {
		return ErrProxyBusy
	}
	return nil
}

// StopScanning implements ble.Central: the reports stop, and a proxy
// without a connection through it is unsubscribed.
func (c *Central) StopScanning(context.Context) error {
	c.mu.Lock()
	c.scanning = false
	c.mu.Unlock()
	for _, p := range c.proxies {
		p.unsubscribeIfIdle()
	}
	return nil
}

// discovered is a Matter advertisement one proxy forwarded.
func (c *Central) discovered(p *proxy, addr uint64, addrType uint32, rssi int32, data []byte) {
	c.mu.Lock()
	s := c.sightings[addr]
	if s == nil {
		s = map[*proxy]sighting{}
		c.sightings[addr] = s
	}
	s[p] = sighting{rssi: rssi, addrType: addrType, at: c.clk.Now()}
	cb, scanning := c.callback, c.scanning
	c.mu.Unlock()
	if cb != nil && scanning {
		cb(ble.Peripheral{Address: macString(addr)}, data)
	}
}

// forgetProxy drops a proxy's reports when its connection ends: a
// peripheral it alone saw is not reachable until a proxy reports it again.
func (c *Central) forgetProxy(p *proxy) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for addr, s := range c.sightings {
		delete(s, p)
		if len(s) == 0 {
			delete(c.sightings, addr)
		}
	}
}

// IsPeripheralReachable implements ble.Reachability — matter.js's
// isPeripheralReachable for "a transport that routes through remote proxies"
// (packages/protocol/src/common/BleScanner.ts:46-52): a connected proxy that
// is not busy reported the peripheral and has a connection slot for it
// (BluetoothConnectionsFreeResponse, api.proto:2089-2100).
func (c *Central) IsPeripheralReachable(address string) bool {
	addr, err := parseMAC(address)
	if err != nil {
		return false
	}
	c.mu.Lock()
	ps := make([]*proxy, 0, len(c.sightings[addr]))
	for p := range c.sightings[addr] {
		ps = append(ps, p)
	}
	c.mu.Unlock()
	for _, p := range ps {
		if p.usable() && p.hasSlotFor(addr) {
			return true
		}
	}
	return false
}

// choose picks the proxy for a connect to addr: of the usable proxies that
// reported it within sightingFreshness of its latest report, the one with
// the strongest signal.
func (c *Central) choose(addr uint64) (*proxy, sighting, error) {
	c.mu.Lock()
	type cand struct {
		p *proxy
		s sighting
	}
	cs := make([]cand, 0, len(c.sightings[addr]))
	var latest time.Time
	for p, s := range c.sightings[addr] {
		cs = append(cs, cand{p, s})
		if s.at.After(latest) {
			latest = s.at
		}
	}
	c.mu.Unlock()
	if len(cs) == 0 {
		return nil, sighting{}, ErrUnknownPeripheral
	}
	var best *cand
	lastErr := ErrProxyUnavailable
	for i := range cs {
		x := &cs[i]
		if latest.Sub(x.s.at) > sightingFreshness {
			continue
		}
		x.p.mu.Lock()
		busy, up := x.p.busy, x.p.api != nil
		x.p.mu.Unlock()
		if busy {
			lastErr = ErrProxyBusy
			continue
		}
		if !up {
			continue
		}
		if best == nil || x.s.rssi > best.s.rssi || (x.s.rssi == best.s.rssi && x.p.name < best.p.name) {
			best = x
		}
	}
	if best == nil {
		return nil, sighting{}, lastErr
	}
	return best.p, best.s, nil
}

// Connect implements ble.Central: through the proxy chosen for the
// peripheral, the connect, the MTU and the Matter service's handles.
func (c *Central) Connect(ctx context.Context, p ble.Peripheral) (ble.Connection, error) {
	addr, err := parseMAC(p.Address)
	if err != nil {
		return nil, err
	}
	px, s, err := c.choose(addr)
	if err != nil {
		return nil, fmt.Errorf("esphome: connect %s: %w", p.Address, err)
	}
	c.log.Debug("ble.esphome.connect", slog.String("address", p.Address), slog.String("proxy", px.name), slog.Int("rssi", int(s.rssi)))
	k, err := px.connect(ctx, addr, s.addrType)
	if err != nil {
		return nil, err
	}
	return k, nil
}

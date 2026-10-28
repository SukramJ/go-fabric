// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package esphome

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/SukramJ/go-fabric/transport/btp"

	"github.com/SukramJ/go-fabric/ble/esphome/internal/wire"
)

// DefaultPort is the ESPHome API's port (esphome/components/api/__init__.py
// DEFAULT_PORT; aioesphomeapi/log_reader.py).
const DefaultPort = "6053"

// The reconnect backoff.
const (
	initialBackoff = time.Second
	maxBackoff     = time.Minute
)

// Proxy names one ESPHome Bluetooth proxy.
type Proxy struct {
	// Addr is host:port; the port defaults to 6053.
	Addr string
	// Key is the API encryption key, base64 of 32 bytes as the device's
	// `api: encryption: key:` holds it; empty is the plaintext API.
	Key string
	// Name labels the proxy in the log; empty is Addr.
	Name string
}

// ParseProxy parses host[:port][,key] — the reference controller's
// --bluetooth-proxy.
func ParseProxy(spec string) (Proxy, error) {
	addr, key, _ := strings.Cut(spec, ",")
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return Proxy{}, fmt.Errorf("esphome: empty proxy address in %q", spec)
	}
	return Proxy{Addr: addr, Key: strings.TrimSpace(key)}, nil
}

// withPort is addr with the default port when it has none.
func withPort(addr string) string {
	if _, _, err := net.SplitHostPort(addr); err == nil {
		return addr
	}
	return net.JoinHostPort(strings.Trim(addr, "[]"), DefaultPort)
}

// proxy is the central's session with one proxy: the API connection,
// reconnected with backoff, the advertisement subscription, the GATT
// connections through it and its connection slots.
type proxy struct {
	c    *Central
	name string
	psk  []byte
	// ctx ends the proxy's run loop: the central's close, or the proxy
	// leaving a discovered set (stop).
	ctx  context.Context
	stop context.CancelFunc
	// static is a proxy NewCentral was given; it stays regardless of
	// discovery. key is a discovered proxy's instance name, lower case.
	static bool
	key    string

	// mu guards everything below.
	mu sync.Mutex
	// addrs are the host:port the proxy is dialled at, in order; the
	// first is the one that answered last. Discovery replaces them when
	// the proxy's DNS-SD records move.
	addrs      []string
	api        *apiConn
	flags      uint32
	subscribed bool
	lastAdv    time.Time
	conns      map[uint64]*connection
	// free, limit and allocated are the last BluetoothConnectionsFreeResponse;
	// haveFree is false until one arrived.
	free, limit uint32
	allocated   map[uint64]bool
	haveFree    bool
	// takenOnce is a subscription lost once, busy a second time
	// (doc.go, "One subscriber per proxy").
	takenOnce, busy bool
	wake            chan struct{}
}

func newProxy(c *Central, cfg Proxy, more ...string) (*proxy, error) {
	addr := withPort(cfg.Addr)
	p := &proxy{c: c, addrs: append([]string{addr}, more...), name: cfg.Name, conns: map[uint64]*connection{}, wake: make(chan struct{}, 1)}
	p.ctx, p.stop = context.WithCancel(c.ctx)
	if p.name == "" {
		p.name = addr
	}
	if cfg.Key != "" {
		k, err := wire.DecodeKey(cfg.Key)
		if err != nil {
			return nil, fmt.Errorf("proxy %s: %w", p.name, err)
		}
		p.psk = k
	}
	return p, nil
}

// permanent reports whether err is a refusal no retry cures: a key or a
// protocol mismatch, or a proxy without the features commissioning needs.
func permanent(err error) bool {
	for _, e := range []error{ErrNoActiveConnections, ErrNoRemoteCaching, ErrIncompatibleAPI, wire.ErrInvalidKey, wire.ErrRequiresEncryption, wire.ErrPlaintextDevice} {
		if errors.Is(err, e) {
			return true
		}
	}
	return false
}

// dial opens and checks one API connection at the proxy's addresses; the
// one that answered moves to the front.
func (p *proxy) dial(ctx context.Context) (*apiConn, error) {
	p.mu.Lock()
	addrs := slices.Clone(p.addrs)
	p.mu.Unlock()
	a, addr, err := dialFirst(ctx, addrs, p.psk, &p.c.opts)
	if err != nil {
		return nil, err
	}
	p.mu.Lock()
	if i := slices.Index(p.addrs, addr); i > 0 {
		p.addrs = append([]string{addr}, slices.Delete(p.addrs, i, i+1)...)
	}
	p.mu.Unlock()
	return a, nil
}

// dialFirst opens and checks an API connection at the first of addrs that
// answers; a permanent refusal ends the attempt, since every address is
// the same device.
func dialFirst(ctx context.Context, addrs []string, psk []byte, o *Options) (*apiConn, string, error) {
	var errs []error
	for _, addr := range addrs {
		a, err := dialChecked(ctx, addr, psk, o)
		if err == nil {
			return a, addr, nil
		}
		errs = append(errs, err)
		if permanent(err) || ctx.Err() != nil {
			return nil, "", err
		}
	}
	if len(errs) == 1 {
		return nil, "", errs[0]
	}
	return nil, "", errors.Join(errs...)
}

// dialChecked opens one API connection and checks the features a
// commissioning proxy needs: ACTIVE_CONNECTIONS (`active: true`) and the
// V3 connect REMOTE_CACHING selects (aioesphomeapi client.py:1330-1343;
// the firmware sets both with active, bluetooth_proxy.h:207-235).
func dialChecked(ctx context.Context, addr string, psk []byte, o *Options) (*apiConn, error) {
	a, err := dialAPI(ctx, addr, psk, o)
	if err != nil {
		return nil, err
	}
	switch {
	case a.flags&wire.FeatureActiveConnections == 0:
		a.close(ErrNoActiveConnections)
		return nil, ErrNoActiveConnections
	case a.flags&wire.FeatureRemoteCaching == 0:
		a.close(ErrNoRemoteCaching)
		return nil, ErrNoRemoteCaching
	}
	return a, nil
}

// setAddrs takes the addresses discovery found a proxy at. The address
// that answered last stays in front while it is among them. When the set
// changed — the proxy moved, or took a further address — a reconnect
// backoff is cut short and setAddrs reports true.
func (p *proxy) setAddrs(addrs []string) bool {
	p.mu.Lock()
	was, now := slices.Sorted(slices.Values(p.addrs)), slices.Sorted(slices.Values(addrs))
	changed := !slices.Equal(was, now)
	if slices.Contains(addrs, p.addrs[0]) {
		rest := slices.DeleteFunc(slices.Clone(addrs), func(a string) bool { return a == p.addrs[0] })
		p.addrs = append(p.addrs[:1:1], rest...)
	} else {
		p.addrs = slices.Clone(addrs)
	}
	p.mu.Unlock()
	if changed {
		p.wakeUp()
	}
	return changed
}

// at reports whether the proxy is dialled at one of addrs.
func (p *proxy) at(addrs []string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, a := range addrs {
		if slices.Contains(p.addrs, a) {
			return true
		}
	}
	return false
}

// idle reports whether the proxy has neither an API connection nor a GATT
// connection through it: dropping it disturbs nothing.
func (p *proxy) idle() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.api == nil && len(p.conns) == 0
}

func (p *proxy) wakeUp() {
	select {
	case p.wake <- struct{}{}:
	default:
	}
}

// run keeps the proxy connected until the central closes: first is the
// connection NewCentral opened, nil when that failed.
func (p *proxy) run(first *apiConn) {
	defer p.c.wg.Done()
	backoff := initialBackoff
	a := first
	for {
		if a == nil {
			if !p.waitUsable() {
				return
			}
			var err error
			a, err = p.dial(p.ctx)
			if err != nil {
				if p.ctx.Err() != nil {
					return
				}
				p.c.log.Warn("ble.esphome.proxy_unreachable", slog.String("proxy", p.name), slog.Any("err", err), slog.Duration("retry", backoff))
				if !p.sleep(backoff) {
					return
				}
				backoff = min(2*backoff, maxBackoff)
				continue
			}
		}
		backoff = initialBackoff
		cause := p.attach(a)
		p.detach(a, cause)
		a = nil
		if !p.sleep(backoff) {
			return
		}
	}
}

// waitUsable waits while the proxy is busy; false when the central closed.
func (p *proxy) waitUsable() bool {
	for {
		p.mu.Lock()
		busy := p.busy
		p.mu.Unlock()
		if !busy {
			return true
		}
		select {
		case <-p.ctx.Done():
			return false
		case <-p.wake:
		}
	}
}

// sleep waits d, or until woken (a new address, a fresh scan); false when
// the proxy stopped.
func (p *proxy) sleep(d time.Duration) bool {
	t := p.c.clk.NewTimer(d)
	defer t.Stop()
	select {
	case <-p.ctx.Done():
		return false
	case <-t.C():
		return true
	case <-p.wake:
		return true
	}
}

// attach runs one API connection until it ends and returns its cause: the
// subscription when the central needs it, the reader, the watch for a
// subscription another client took.
func (p *proxy) attach(a *apiConn) error {
	a.handle = p.handle
	p.mu.Lock()
	p.api, p.flags = a, a.flags
	p.mu.Unlock()
	p.c.log.Info("ble.esphome.proxy_connected", slog.String("proxy", p.name), slog.String("device", a.name),
		slog.Bool("encrypted", p.psk != nil), slog.Uint64("feature_flags", uint64(a.flags)))
	go a.run()
	if p.c.isScanning() {
		p.subscribe()
	}
	tk := p.c.clk.NewTicker(p.c.opts.SilenceTimeout / 4)
	defer tk.Stop()
	for {
		select {
		case <-a.done:
			return a.cause()
		case <-p.ctx.Done():
			a.close(context.Canceled)
			return context.Canceled
		case now := <-tk.C():
			if err := p.checkSilence(a, now); err != nil {
				a.close(err)
				return err
			}
		}
	}
}

// checkSilence is the watch for a lost subscription. A proxy forwards
// advertisements in batches every 100 to 200 ms while it has a subscriber
// (bluetooth_proxy.cpp:118-121, :686-709); a subscribed connection that receives none
// for SilenceTimeout has lost the subscription to another client, which the
// firmware does not announce (bluetooth_proxy.cpp:740-765).
func (p *proxy) checkSilence(a *apiConn, now time.Time) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.subscribed || p.api != a || now.Sub(p.lastAdv) < p.c.opts.SilenceTimeout {
		return nil
	}
	if p.takenOnce {
		p.busy = true
		p.c.log.Warn("ble.esphome.proxy_busy", slog.String("proxy", p.name),
			slog.String("hint", "another API client, such as Home Assistant, took the proxy's subscription again; dedicate the proxy or retry"))
		return ErrProxyBusy
	}
	p.takenOnce = true
	p.c.log.Warn("ble.esphome.proxy_subscription_lost", slog.String("proxy", p.name), slog.Duration("silent", now.Sub(p.lastAdv)))
	return fmt.Errorf("%w: no advertisement for %v", ErrProxyUnavailable, now.Sub(p.lastAdv))
}

// detach ends the state of a connection: every GATT connection through it
// is gone, its sightings and slots forgotten.
func (p *proxy) detach(a *apiConn, cause error) {
	p.mu.Lock()
	if p.api == a {
		p.api = nil
	}
	p.subscribed = false
	p.haveFree = false
	conns := make([]*connection, 0, len(p.conns))
	for _, k := range p.conns {
		conns = append(conns, k)
	}
	p.mu.Unlock()
	for _, k := range conns {
		k.markGone()
	}
	p.c.forgetProxy(p)
	if !errors.Is(cause, context.Canceled) {
		p.c.log.Info("ble.esphome.proxy_lost", slog.String("proxy", p.name), slog.Any("err", cause))
		// A proxy whose address moved (DHCP) is found again by its name.
		p.c.triggerRescan()
	}
}

// send writes m on the current connection.
func (p *proxy) send(m wire.Message) error {
	p.mu.Lock()
	a := p.api
	p.mu.Unlock()
	if a == nil {
		return ErrProxyUnavailable
	}
	return a.send(m)
}

// subscribe subscribes to the advertisements — raw where the proxy has
// them (RAW_ADVERTISEMENTS; client.py:1254-1277), the parsed, deprecated
// form otherwise — and to the connection slots
// (SubscribeBluetoothConnectionsFreeRequest, client.py:1279-1290).
func (p *proxy) subscribe() {
	p.mu.Lock()
	a := p.api
	if a == nil || p.subscribed {
		p.mu.Unlock()
		return
	}
	p.subscribed = true
	p.lastAdv = p.c.clk.Now()
	var flags uint32
	if p.flags&wire.FeatureRawAdvertisements != 0 {
		flags = wire.SubscriptionFlagRawAdvertisements
	}
	p.mu.Unlock()
	p.c.log.Info("ble.esphome.proxy_taken", slog.String("proxy", p.name),
		slog.String("note", "subscribing replaces the proxy's current API subscriber, such as Home Assistant's Bluetooth integration, until this central lets go"))
	_ = a.send(&wire.SubscribeBluetoothLEAdvertisementsRequest{Flags: flags})
	_ = a.send(wire.Empty(wire.TypeSubscribeBluetoothConnectionsFreeRequest))
}

// unsubscribeIfIdle drops the subscription when the central does not scan
// and no connection runs through the proxy: the firmware disconnects every
// connection of a subscriber that leaves (bluetooth_proxy.cpp:637-648), so a
// connection keeps it.
func (p *proxy) unsubscribeIfIdle() {
	p.mu.Lock()
	a := p.api
	if a == nil || !p.subscribed || len(p.conns) > 0 || p.c.isScanning() {
		p.mu.Unlock()
		return
	}
	p.subscribed = false
	p.mu.Unlock()
	_ = a.send(wire.Empty(wire.TypeUnsubscribeBluetoothLEAdvertisementsRequest))
}

// reset clears the busy mark for a new scan and wakes the run loop.
func (p *proxy) reset() {
	p.mu.Lock()
	p.busy, p.takenOnce = false, false
	p.mu.Unlock()
	p.wakeUp()
}

// handle is a message the API connection does not answer itself.
func (p *proxy) handle(typ uint16, payload []byte) {
	switch typ {
	case wire.TypeBluetoothLERawAdvertisementsResponse:
		var m wire.BluetoothLERawAdvertisementsResponse
		if m.Unmarshal(payload) != nil {
			return
		}
		p.noteAdvertisement()
		for _, adv := range m.Advertisements {
			if data, ok := matterServiceData(adv.Data); ok {
				p.c.discovered(p, adv.Address, adv.AddressType, adv.RSSI, data)
			}
		}
	case wire.TypeBluetoothLEAdvertisementResponse:
		var m wire.BluetoothLEAdvertisementResponse
		if m.Unmarshal(payload) != nil {
			return
		}
		p.noteAdvertisement()
		for _, sd := range m.ServiceData {
			if btp.IsServiceUUID(sd.UUID) && isMatterServiceData(sd.Data) {
				p.c.discovered(p, m.Address, m.AddressType, m.RSSI, sd.Data)
			}
		}
	case wire.TypeBluetoothConnectionsFreeResponse:
		var m wire.BluetoothConnectionsFreeResponse
		if m.Unmarshal(payload) != nil {
			return
		}
		p.mu.Lock()
		p.free, p.limit, p.haveFree = m.Free, m.Limit, true
		p.allocated = map[uint64]bool{}
		for _, x := range m.Allocated {
			p.allocated[x] = true
		}
		p.mu.Unlock()
	case wire.TypeBluetoothDeviceConnectionResponse:
		var m wire.BluetoothDeviceConnectionResponse
		if m.Unmarshal(payload) != nil {
			return
		}
		if k := p.conn(m.Address); k != nil {
			k.onConnectionState(m)
		}
	case wire.TypeBluetoothGATTGetServicesResponse:
		var m wire.BluetoothGATTGetServicesResponse
		if m.Unmarshal(payload) != nil {
			return
		}
		if k := p.conn(m.Address); k != nil {
			k.onReply(typ, &wire.AddressHandle{Address: m.Address}, &m)
		}
	case wire.TypeBluetoothGATTGetServicesDoneResponse, wire.TypeBluetoothGATTReadResponse,
		wire.TypeBluetoothGATTWriteResponse, wire.TypeBluetoothGATTNotifyResponse, wire.TypeBluetoothGATTErrorResponse:
		var m wire.AddressHandle
		if m.Unmarshal(typ, payload) != nil {
			return
		}
		if k := p.conn(m.Address); k != nil {
			k.onReply(typ, &m, nil)
		}
	case wire.TypeBluetoothGATTNotifyDataResponse:
		var m wire.AddressHandle
		if m.Unmarshal(typ, payload) != nil {
			return
		}
		if k := p.conn(m.Address); k != nil {
			k.onNotify(m.Handle, m.Data)
		}
	}
}

func (p *proxy) noteAdvertisement() {
	p.mu.Lock()
	p.lastAdv = p.c.clk.Now()
	p.mu.Unlock()
}

func (p *proxy) conn(addr uint64) *connection {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.conns[addr]
}

// forget drops a connection that is gone.
func (p *proxy) forget(k *connection) {
	p.mu.Lock()
	if p.conns[k.addr] == k {
		delete(p.conns, k.addr)
	}
	p.mu.Unlock()
	p.unsubscribeIfIdle()
}

// usable reports whether the proxy is connected and not busy.
func (p *proxy) usable() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.api != nil && !p.busy
}

// hasSlotFor reports whether the proxy can connect to addr: a free slot, or
// one it holds for addr already; true until the first
// BluetoothConnectionsFreeResponse.
func (p *proxy) hasSlotFor(addr uint64) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return !p.haveFree || p.free > 0 || p.allocated[addr]
}

// connect opens a GATT connection to addr: BluetoothDeviceRequest
// CONNECT_V3_WITHOUT_CACHE with the advertisement's address type
// (client.py:1311-1420; the firmware answers a request without the address
// type with connected=false, bluetooth_proxy.cpp:340-345), the answer with
// the MTU, then the GATT database.
func (p *proxy) connect(ctx context.Context, addr uint64, addrType uint32) (*connection, error) {
	p.mu.Lock()
	switch {
	case p.busy:
		p.mu.Unlock()
		return nil, fmt.Errorf("proxy %s: %w", p.name, ErrProxyBusy)
	case p.api == nil:
		p.mu.Unlock()
		return nil, fmt.Errorf("proxy %s: %w", p.name, ErrProxyUnavailable)
	}
	if old := p.conns[addr]; old != nil {
		p.mu.Unlock()
		old.markGone()
		p.mu.Lock()
	}
	k := newConnection(p, addr)
	p.conns[addr] = k
	p.mu.Unlock()
	p.subscribe()
	fail := func(err error) (*connection, error) {
		k.markGone()
		return nil, err
	}
	if err := p.send(&wire.BluetoothDeviceRequest{Address: addr, RequestType: wire.DeviceRequestConnectV3WithoutCache, HasAddressType: true, AddressType: addrType}); err != nil {
		return fail(err)
	}
	t := p.c.clk.NewTimer(bleTimeout)
	defer t.Stop()
	select {
	case m := <-k.connected:
		if !m.Connected {
			return fail(fmt.Errorf("proxy %s: %w", p.name, &GATTError{Op: "connect", Code: m.Error}))
		}
		k.mtu = int(m.MTU)
	case <-k.gone:
		// A refusal closes the connection as it answers; report it.
		select {
		case m := <-k.connected:
			if !m.Connected {
				return nil, fmt.Errorf("proxy %s: %w", p.name, &GATTError{Op: "connect", Code: m.Error})
			}
		default:
		}
		return nil, fmt.Errorf("proxy %s: connect %s: %w", p.name, k.address, btp.ErrDisconnected)
	case <-t.C():
		// On a timeout the client sends the disconnect
		// (client.py:1377-1420).
		_ = p.send(&wire.BluetoothDeviceRequest{Address: addr, RequestType: wire.DeviceRequestDisconnect})
		return fail(fmt.Errorf("proxy %s: connect %s: %w", p.name, k.address, ErrTimeout))
	case <-ctx.Done():
		_ = p.send(&wire.BluetoothDeviceRequest{Address: addr, RequestType: wire.DeviceRequestDisconnect})
		return fail(ctx.Err())
	}
	if err := k.discover(ctx); err != nil {
		_ = k.Disconnect()
		return nil, fmt.Errorf("proxy %s: %s: %w", p.name, k.address, err)
	}
	return k, nil
}

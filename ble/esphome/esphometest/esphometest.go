// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package esphometest is an in-process ESPHome Bluetooth proxy for tests:
// the native API over a TCP listener, plaintext or with Noise when a key is
// set, in front of controller/ble/bletest's peripherals. What it does with
// each message is what the firmware's bluetooth_proxy component does
// (esphome/components/bluetooth_proxy and bluetooth_connection at 1cc6c9d1,
// read for its behaviour, nothing copied): one subscriber — the newest —
// gets the advertisements and every GATT reply; a notify registration is
// local and the client writes the CCCD; a subscriber that leaves takes its
// connections with it.
package esphometest

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/SukramJ/go-fabric/controller/ble"
	"github.com/SukramJ/go-fabric/controller/ble/bletest"
	"github.com/SukramJ/go-fabric/transport/btp"

	"github.com/SukramJ/go-fabric/ble/esphome/internal/wire"
)

// The GATT database every peripheral shows through the proxy: the Matter
// service with C1 (write), C2 (indicate) and its CCCD, and C3 (read) when
// Options.C3 is set.
const (
	ServiceHandle = 0x000c
	C1Handle      = 0x000e
	C2Handle      = 0x0010
	CCCDHandle    = 0x0011
	C3Handle      = 0x0013
)

// DefaultFeatureFlags are the flags of a proxy with `active: true`
// (bluetooth_proxy.h:207-235).
const DefaultFeatureFlags = wire.FeaturePassiveScan | wire.FeatureActiveConnections | wire.FeatureRemoteCaching | wire.FeatureRawAdvertisements

// Options configure a fake proxy; the zero value is a plaintext proxy with
// DefaultFeatureFlags, API 1.14, three slots, an RSSI of -60 and a batch of
// advertisements every 50 ms.
type Options struct {
	// Key is the API key, base64 of 32 bytes; empty is plaintext.
	Key string
	// Name and MAC are the device's.
	Name, MAC string
	// FeatureFlags are the Bluetooth proxy flags; 0 is DefaultFeatureFlags.
	FeatureFlags uint32
	// APIVersionMinor is the API minor version the proxy reports; 0 is 14.
	// From 15 the flags are read from DeviceCapabilitiesResponse.
	APIVersionMinor uint32
	// LegacyUUIDs sends every UUID in its 128-bit form, as for a client
	// below API 1.12.
	LegacyUUIDs bool
	// RSSI is the signal every advertisement reports; 0 is -60.
	RSSI int32
	// Slots is the number of connection slots; 0 is 3.
	Slots int
	// AdvertiseInterval is the batch interval; 0 is 50 ms.
	AdvertiseInterval time.Duration
	// MTU overrides the ATT_MTU a connect reports; 0 is the peripheral's.
	MTU uint32
	// C3 is what C3 reads; nil leaves C3 out of the database.
	C3 []byte
}

// Proxy is a fake ESPHome Bluetooth proxy.
type Proxy struct {
	opts    Options
	psk     []byte
	ln      net.Listener
	central *bletest.Central

	// mu guards everything below.
	mu          sync.Mutex
	peripherals []*bletest.Peripheral
	rssi        map[string]int32
	sessions    map[*session]bool
	subscriber  *session
	slots       map[uint64]*slot
	events      []string
	closed      bool
	stop        chan struct{}
	wg          sync.WaitGroup
}

// slot is one connection slot in use.
type slot struct {
	addr     uint64
	conn     ble.Connection
	notifyOn bool
}

// session is one API client.
type session struct {
	nc  net.Conn
	f   wire.Framer
	raw bool
}

// New starts a fake proxy on a loopback port in front of the peripherals.
func New(opts Options, peripherals ...*bletest.Peripheral) (*Proxy, error) {
	if opts.FeatureFlags == 0 {
		opts.FeatureFlags = DefaultFeatureFlags
	}
	if opts.APIVersionMinor == 0 {
		opts.APIVersionMinor = 14
	}
	if opts.RSSI == 0 {
		opts.RSSI = -60
	}
	if opts.Slots == 0 {
		opts.Slots = 3
	}
	if opts.AdvertiseInterval == 0 {
		opts.AdvertiseInterval = 50 * time.Millisecond
	}
	if opts.Name == "" {
		opts.Name = "fake-proxy"
	}
	if opts.MAC == "" {
		opts.MAC = "AABBCCDDEEFF"
	}
	p := &Proxy{
		opts: opts, central: bletest.NewCentral(peripherals...), peripherals: peripherals,
		rssi: map[string]int32{}, sessions: map[*session]bool{}, slots: map[uint64]*slot{}, stop: make(chan struct{}),
	}
	if opts.Key != "" {
		k, err := base64.StdEncoding.DecodeString(opts.Key)
		if err != nil || len(k) != 32 {
			return nil, errors.New("esphometest: the key is not base64 of 32 bytes")
		}
		p.psk = k
	}
	var lc net.ListenConfig
	ln, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	p.ln = ln
	p.wg.Add(2)
	go p.accept(ln)
	go p.advertise()
	return p, nil
}

// Addr is the proxy's host:port.
func (p *Proxy) Addr() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.ln.Addr().String()
}

// Port is the proxy's TCP port.
func (p *Proxy) Port() uint16 {
	p.mu.Lock()
	defer p.mu.Unlock()
	_, port, _ := net.SplitHostPort(p.ln.Addr().String())
	n, _ := strconv.ParseUint(port, 10, 16)
	return uint16(n)
}

// Move puts the proxy on a new loopback port and drops its clients, as a
// device that took a new address from DHCP does; the old port refuses
// connections, and a [Network] the proxy advertises on answers with the
// new one from here on.
func (p *Proxy) Move() error {
	var lc net.ListenConfig
	ln, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		_ = ln.Close()
		return errors.New("esphometest: the proxy is closed")
	}
	old := p.ln
	p.ln = ln
	p.wg.Add(1)
	p.mu.Unlock()
	_ = old.Close()
	go p.accept(ln)
	p.DropClients()
	return nil
}

// Close stops the proxy.
func (p *Proxy) Close() {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return
	}
	p.closed = true
	close(p.stop)
	ln := p.ln
	p.mu.Unlock()
	_ = ln.Close()
	p.DropClients()
	p.wg.Wait()
}

// Add makes a peripheral reachable.
func (p *Proxy) Add(per *bletest.Peripheral) {
	p.mu.Lock()
	p.peripherals = append(p.peripherals, per)
	p.mu.Unlock()
	p.central.Add(per)
}

// SetRSSI sets the signal the proxy reports for one peripheral.
func (p *Proxy) SetRSSI(address string, rssi int32) {
	p.mu.Lock()
	p.rssi[address] = rssi
	p.mu.Unlock()
}

// Take is another API client — Home Assistant — subscribing: from now on
// the proxy sends this client's sessions nothing, until one of them
// subscribes again (bluetooth_proxy.cpp:740-765, "the newest subscriber
// wins").
func (p *Proxy) Take() {
	p.mu.Lock()
	p.subscriber = nil
	p.events = append(p.events, "taken")
	p.mu.Unlock()
}

// DropClients closes every API connection, as a proxy that reboots or
// leaves the network does.
func (p *Proxy) DropClients() {
	p.mu.Lock()
	ss := make([]*session, 0, len(p.sessions))
	for s := range p.sessions {
		ss = append(ss, s)
	}
	p.mu.Unlock()
	for _, s := range ss {
		_ = s.nc.Close()
	}
}

// DisconnectPeripheral drops the GATT connection to address from the
// peripheral's side.
func (p *Proxy) DisconnectPeripheral(address string) {
	addr, _ := parseMAC(address)
	p.mu.Lock()
	sl := p.slots[addr]
	p.mu.Unlock()
	if sl != nil {
		_ = sl.conn.Disconnect()
	}
}

// Events is the log of what the proxy was asked, in order: "subscribe",
// "unsubscribe", "connect <mac>", "services", "write <handle>",
// "notify <handle> <on|off>", "cccd <handle> <hex>", "indicate",
// "read <handle>", "disconnect <mac>", "taken".
func (p *Proxy) Events() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.events...)
}

// Clients is the number of API clients connected.
func (p *Proxy) Clients() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.sessions)
}

// Connections is the number of slots in use.
func (p *Proxy) Connections() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.slots)
}

func (p *Proxy) event(format string, args ...any) {
	p.mu.Lock()
	p.events = append(p.events, fmt.Sprintf(format, args...))
	p.mu.Unlock()
}

func (p *Proxy) accept(ln net.Listener) {
	defer p.wg.Done()
	for {
		nc, err := ln.Accept()
		if err != nil {
			return
		}
		p.wg.Add(1)
		go p.serve(nc)
	}
}

// serve runs one API client.
func (p *Proxy) serve(nc net.Conn) {
	defer p.wg.Done()
	defer func() { _ = nc.Close() }()
	s := &session{nc: nc}
	if p.psk != nil {
		n, err := wire.ServerHandshake(nc, p.psk, wire.ServerHello{Name: p.opts.Name, MAC: strings.ToLower(p.opts.MAC)}, nil)
		if err != nil {
			return
		}
		s.f = n
	} else {
		s.f = wire.NewPlaintext(nc)
	}
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return
	}
	p.sessions[s] = true
	p.mu.Unlock()
	defer p.leave(s)
	for {
		typ, payload, err := s.f.ReadMessage()
		if errors.Is(err, wire.ErrRequiresEncryption) {
			// A Noise client at a plaintext device: the device answers
			// with the plaintext marker and a text
			// (api_frame_helper_plaintext.cpp:211-235).
			_, _ = nc.Write([]byte("\x00Bad indicator byte"))
		}
		if err != nil {
			return
		}
		if !p.handle(s, typ, payload) {
			return
		}
	}
}

// leave ends a session; a subscriber that leaves takes its connections
// with it (bluetooth_proxy.cpp:637-648).
func (p *Proxy) leave(s *session) {
	p.mu.Lock()
	delete(p.sessions, s)
	var drop []*slot
	if p.subscriber == s {
		p.subscriber = nil
		for _, sl := range p.slots {
			drop = append(drop, sl)
		}
	}
	p.mu.Unlock()
	for _, sl := range drop {
		_ = sl.conn.Disconnect()
	}
}

// toSubscriber sends m to the subscriber, if there is one.
func (p *Proxy) toSubscriber(m wire.Message) {
	p.mu.Lock()
	s := p.subscriber
	p.mu.Unlock()
	if s != nil {
		_ = s.f.WriteMessage(m)
	}
}

func (p *Proxy) handle(s *session, typ uint16, payload []byte) bool {
	switch typ {
	case wire.TypeHelloRequest:
		_ = s.f.WriteMessage(&wire.HelloResponse{APIVersionMajor: 1, APIVersionMinor: p.opts.APIVersionMinor, ServerInfo: "esphometest", Name: p.opts.Name})
	case wire.TypeDeviceInfoRequest:
		_ = s.f.WriteMessage(&wire.DeviceInfoResponse{Name: p.opts.Name, MACAddress: p.opts.MAC, BluetoothProxyFeatureFlags: p.opts.FeatureFlags, BluetoothMACAddress: p.opts.MAC})
	case wire.TypeDeviceCapabilitiesRequest:
		_ = s.f.WriteMessage(&wire.DeviceCapabilitiesResponse{BluetoothProxyFeatureFlags: p.opts.FeatureFlags, BluetoothMACAddress: p.opts.MAC})
	case wire.TypePingRequest:
		_ = s.f.WriteMessage(wire.Empty(wire.TypePingResponse))
	case wire.TypeDisconnectRequest:
		_ = s.f.WriteMessage(wire.Empty(wire.TypeDisconnectResponse))
		return false
	case wire.TypeSubscribeBluetoothLEAdvertisementsRequest:
		var m wire.SubscribeBluetoothLEAdvertisementsRequest
		_ = m.Unmarshal(payload)
		p.mu.Lock()
		s.raw = m.Flags&wire.SubscriptionFlagRawAdvertisements != 0
		p.subscriber = s
		p.events = append(p.events, "subscribe")
		p.mu.Unlock()
	case wire.TypeUnsubscribeBluetoothLEAdvertisementsRequest:
		p.mu.Lock()
		p.events = append(p.events, "unsubscribe")
		p.mu.Unlock()
		p.leaveSubscription(s)
	case wire.TypeSubscribeBluetoothConnectionsFreeRequest:
		_ = s.f.WriteMessage(p.free())
	case wire.TypeBluetoothDeviceRequest:
		var m wire.BluetoothDeviceRequest
		_ = m.Unmarshal(payload)
		p.deviceRequest(&m)
	case wire.TypeBluetoothGATTGetServicesRequest, wire.TypeBluetoothGATTWriteRequest, wire.TypeBluetoothGATTWriteDescriptorRequest,
		wire.TypeBluetoothGATTNotifyRequest, wire.TypeBluetoothGATTReadRequest:
		var m wire.AddressHandle
		_ = m.Unmarshal(typ, payload)
		p.gatt(&m)
	}
	return true
}

// leaveSubscription is an unsubscribe: the subscriber's connections go.
func (p *Proxy) leaveSubscription(s *session) {
	p.mu.Lock()
	var drop []*slot
	if p.subscriber == s {
		p.subscriber = nil
		for _, sl := range p.slots {
			drop = append(drop, sl)
		}
	}
	p.mu.Unlock()
	for _, sl := range drop {
		_ = sl.conn.Disconnect()
	}
}

func (p *Proxy) free() *wire.BluetoothConnectionsFreeResponse {
	p.mu.Lock()
	defer p.mu.Unlock()
	m := &wire.BluetoothConnectionsFreeResponse{Limit: uint32(p.opts.Slots)} //nolint:gosec // a handful of slots
	m.Free = m.Limit - uint32(len(p.slots))                                  //nolint:gosec // bounded by Slots
	for a := range p.slots {
		m.Allocated = append(m.Allocated, a)
	}
	return m
}

func (p *Proxy) deviceRequest(m *wire.BluetoothDeviceRequest) {
	mac := macString(m.Address)
	switch m.RequestType {
	case wire.DeviceRequestDisconnect:
		p.event("disconnect %s", mac)
		p.mu.Lock()
		sl := p.slots[m.Address]
		p.mu.Unlock()
		if sl != nil {
			_ = sl.conn.Disconnect()
		} else {
			p.toSubscriber(&wire.BluetoothDeviceConnectionResponse{Address: m.Address})
		}
	case wire.DeviceRequestConnectV3WithCache, wire.DeviceRequestConnectV3WithoutCache:
		p.event("connect %s type=%d", mac, m.AddressType)
		// A connect without the address type is refused
		// (bluetooth_proxy.cpp:340-345), and so is one without a free slot
		// (:334-339).
		p.mu.Lock()
		full := len(p.slots) >= p.opts.Slots && p.slots[m.Address] == nil
		p.mu.Unlock()
		if !m.HasAddressType || full {
			p.toSubscriber(&wire.BluetoothDeviceConnectionResponse{Address: m.Address})
			return
		}
		conn, err := p.central.Connect(context.Background(), ble.Peripheral{Address: mac})
		if err != nil {
			p.toSubscriber(&wire.BluetoothDeviceConnectionResponse{Address: m.Address, Error: 0x3e})
			return
		}
		sl := &slot{addr: m.Address, conn: conn}
		p.mu.Lock()
		p.slots[m.Address] = sl
		p.mu.Unlock()
		mtu := p.opts.MTU
		if mtu == 0 {
			mtu = uint32(conn.ATTMTU()) //nolint:gosec // an ATT_MTU
		}
		p.toSubscriber(&wire.BluetoothDeviceConnectionResponse{Address: m.Address, Connected: true, MTU: mtu})
		p.toSubscriber(p.free())
		p.wg.Add(1)
		go func() {
			defer p.wg.Done()
			select {
			case <-conn.Disconnected():
			case <-p.stop:
				_ = conn.Disconnect()
			}
			p.mu.Lock()
			if p.slots[m.Address] == sl {
				delete(p.slots, m.Address)
			}
			p.mu.Unlock()
			p.toSubscriber(&wire.BluetoothDeviceConnectionResponse{Address: m.Address, Error: 0x13})
			p.toSubscriber(p.free())
		}()
	}
}

func (p *Proxy) database() wire.GATTService {
	u := func(s string) wire.UUID {
		b, _ := hex.DecodeString(strings.ReplaceAll(s, "-", ""))
		return wire.UUID{Words: [2]uint64{binary.BigEndian.Uint64(b[:8]), binary.BigEndian.Uint64(b[8:])}}
	}
	short := func(v uint32, long string) wire.UUID {
		if p.opts.LegacyUUIDs {
			return u(long)
		}
		return wire.UUID{Short: v}
	}
	svc := wire.GATTService{UUID: short(0xfff6, btp.ServiceUUID), Handle: ServiceHandle, Characteristics: []wire.GATTCharacteristic{
		{UUID: u(btp.C1CharacteristicUUID), Handle: C1Handle, Properties: 0x08},
		{
			UUID: u(btp.C2CharacteristicUUID), Handle: C2Handle, Properties: 0x20,
			Descriptors: []wire.GATTDescriptor{{UUID: short(0x2902, "00002902-0000-1000-8000-00805f9b34fb"), Handle: CCCDHandle}},
		},
	}}
	if p.opts.C3 != nil {
		svc.Characteristics = append(svc.Characteristics, wire.GATTCharacteristic{UUID: u(btp.C3CharacteristicUUID), Handle: C3Handle, Properties: 0x02})
	}
	return svc
}

func (p *Proxy) gatt(m *wire.AddressHandle) {
	p.mu.Lock()
	sl := p.slots[m.Address]
	p.mu.Unlock()
	gattErr := func(code int32) {
		p.toSubscriber(&wire.AddressHandle{MessageType: wire.TypeBluetoothGATTErrorResponse, Address: m.Address, Handle: m.Handle, Error: code})
	}
	if sl == nil {
		// GATT_NOT_CONNECTED (bluetooth_proxy.cpp:152-158).
		gattErr(0x85)
		return
	}
	ctx := context.Background()
	switch m.MessageType {
	case wire.TypeBluetoothGATTGetServicesRequest:
		p.event("services")
		p.toSubscriber(&wire.BluetoothGATTGetServicesResponse{Address: m.Address, Services: []wire.GATTService{
			// A non-Matter service first, as a real database has.
			{UUID: wire.UUID{Short: 0x1800}, Handle: 0x0001, Characteristics: []wire.GATTCharacteristic{{UUID: wire.UUID{Short: 0x2a00}, Handle: 0x0003, Properties: 0x02}}},
		}})
		p.toSubscriber(&wire.BluetoothGATTGetServicesResponse{Address: m.Address, Services: []wire.GATTService{p.database()}})
		p.toSubscriber(&wire.AddressHandle{MessageType: wire.TypeBluetoothGATTGetServicesDoneResponse, Address: m.Address})
	case wire.TypeBluetoothGATTWriteRequest:
		p.event("write 0x%04x", m.Handle)
		if m.Handle != C1Handle {
			gattErr(0x01)
			return
		}
		if err := sl.conn.WriteC1(ctx, m.Data); err != nil {
			gattErr(0x85)
			return
		}
		if m.Flag {
			p.toSubscriber(&wire.AddressHandle{MessageType: wire.TypeBluetoothGATTWriteResponse, Address: m.Address, Handle: m.Handle})
		}
	case wire.TypeBluetoothGATTNotifyRequest:
		on := map[bool]string{true: "on", false: "off"}[m.Flag]
		p.event("notify 0x%04x %s", m.Handle, on)
		if m.Handle != C2Handle {
			gattErr(0x01)
			return
		}
		p.mu.Lock()
		sl.notifyOn = m.Flag
		p.mu.Unlock()
		p.toSubscriber(&wire.AddressHandle{MessageType: wire.TypeBluetoothGATTNotifyResponse, Address: m.Address, Handle: m.Handle})
	case wire.TypeBluetoothGATTWriteDescriptorRequest:
		p.event("cccd 0x%04x %x", m.Handle, m.Data)
		if m.Handle != CCCDHandle {
			gattErr(0x01)
			return
		}
		var err error
		if len(m.Data) > 0 && m.Data[0]&0x03 != 0 {
			addr := m.Address
			err = sl.conn.SubscribeC2(ctx, func(b []byte) {
				p.mu.Lock()
				on := sl.notifyOn
				p.mu.Unlock()
				if !on {
					return
				}
				p.event("indicate")
				p.toSubscriber(&wire.AddressHandle{MessageType: wire.TypeBluetoothGATTNotifyDataResponse, Address: addr, Handle: C2Handle, Data: b})
			})
		} else {
			err = sl.conn.UnsubscribeC2(ctx)
		}
		if err != nil {
			gattErr(0x85)
			return
		}
		p.toSubscriber(&wire.AddressHandle{MessageType: wire.TypeBluetoothGATTWriteResponse, Address: m.Address, Handle: m.Handle})
	case wire.TypeBluetoothGATTReadRequest:
		p.event("read 0x%04x", m.Handle)
		if m.Handle != C3Handle || p.opts.C3 == nil {
			gattErr(0x0a)
			return
		}
		p.toSubscriber(&wire.AddressHandle{MessageType: wire.TypeBluetoothGATTReadResponse, Address: m.Address, Handle: m.Handle, Data: p.opts.C3})
	}
}

// noiseAdvertisement is a device that is no Matter peripheral: flags and a
// 16-bit service data AD of another UUID, which the central drops.
var noiseAdvertisement = []byte{0x02, 0x01, 0x06, 0x05, 0x16, 0x0d, 0x18, 0x01, 0x02}

// advertise forwards a batch of advertisements to the subscriber every
// AdvertiseInterval (bluetooth_proxy.cpp:118-121, :686-709): every peripheral's, and a
// device's that is none.
func (p *Proxy) advertise() {
	defer p.wg.Done()
	t := time.NewTicker(p.opts.AdvertiseInterval)
	defer t.Stop()
	for {
		select {
		case <-p.stop:
			return
		case <-t.C:
		}
		p.mu.Lock()
		s := p.subscriber
		ps := append([]*bletest.Peripheral(nil), p.peripherals...)
		rssi := make(map[string]int32, len(p.rssi))
		for k, v := range p.rssi {
			rssi[k] = v
		}
		p.mu.Unlock()
		if s == nil {
			continue
		}
		batch := &wire.BluetoothLERawAdvertisementsResponse{Advertisements: []wire.RawAdvertisement{{Address: 0x112233445566, RSSI: -80, Data: noiseAdvertisement}}}
		for _, per := range ps {
			addr, err := parseMAC(per.Address)
			if err != nil {
				continue
			}
			r := p.opts.RSSI
			if v, ok := rssi[per.Address]; ok {
				r = v
			}
			ad := btp.EncodeAdvertisementData(per.Discriminator, per.VendorID, per.ProductID, false, false)
			if !s.raw {
				_ = s.f.WriteMessage(&wire.BluetoothLEAdvertisementResponse{Address: addr, RSSI: r, AddressType: 1, ServiceData: []wire.ServiceData{{UUID: btp.ServiceUUID, Data: ad[7:]}}})
				continue
			}
			batch.Advertisements = append(batch.Advertisements, wire.RawAdvertisement{Address: addr, RSSI: r, AddressType: 1, Data: ad})
		}
		if s.raw {
			_ = s.f.WriteMessage(batch)
		}
	}
}

func macString(a uint64) string {
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], a)
	h := strings.ToUpper(hex.EncodeToString(b[2:]))
	return h[0:2] + ":" + h[2:4] + ":" + h[4:6] + ":" + h[6:8] + ":" + h[8:10] + ":" + h[10:12]
}

func parseMAC(s string) (uint64, error) {
	b, err := hex.DecodeString(strings.ReplaceAll(s, ":", ""))
	if err != nil || len(b) != 6 {
		return 0, fmt.Errorf("esphometest: %q is not a MAC address", s)
	}
	var w [8]byte
	copy(w[2:], b)
	return binary.BigEndian.Uint64(w[:]), nil
}

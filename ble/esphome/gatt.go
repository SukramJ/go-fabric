// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package esphome

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/SukramJ/go-fabric/clock"
	"github.com/SukramJ/go-fabric/controller/ble"
	"github.com/SukramJ/go-fabric/transport/btp"

	"github.com/SukramJ/go-fabric/ble/esphome/internal/wire"
)

// The bounds of the GATT operations. aioesphomeapi waits DEFAULT_BLE_TIMEOUT
// for a connect, the services, a read and a write (client.py:224), ten
// seconds for a notify registration (client.py:1732); the teardown is bounded
// as matter.js bounds it on noble, BLE_DISCONNECT_TIMEOUT
// (packages/nodejs-ble/src/NobleBleChannel.ts:38).
const (
	bleTimeout        = 30 * time.Second
	notifyTimeout     = 10 * time.Second
	disconnectTimeout = 5 * time.Second
)

// Characteristic properties (Bluetooth Core, Vol 3, Part G, §3.3.1.1).
const (
	propNotify   = 0x10
	propIndicate = 0x20
)

// The CCCD values: indications, notifications, neither.
var (
	cccdIndicate = []byte{0x02, 0x00}
	cccdNotify   = []byte{0x01, 0x00}
	cccdOff      = []byte{0x00, 0x00}
)

// reply is the outcome of one GATT operation.
type reply struct {
	data     []byte
	services []wire.GATTService
	err      error
}

// op is the one GATT operation in flight on a connection: the reply type
// and handle it waits for (aioesphomeapi matches replies by address and
// handle, client.py:1215-1244).
type op struct {
	name     string
	want     uint16
	handle   uint32
	services bool
	got      []wire.GATTService
	done     chan reply
}

// connection is a GATT connection to one peripheral through one proxy.
type connection struct {
	p       *proxy
	addr    uint64
	address string
	log     *slog.Logger
	clk     clock.Clock

	// Set once by connect, read-only after.
	mtu                int
	c1, c2, cccd, c3   uint32
	c2Props            uint32
	connected          chan wire.BluetoothDeviceConnectionResponse
	gone               chan struct{}
	goneOnce           sync.Once
	opMu               sync.Mutex // one operation at a time
	mu                 sync.Mutex // guards pending, on, queue, delivering
	pending            *op
	on                 func([]byte)
	queue              [][]byte
	delivering         bool
	connectingFinished bool
}

var _ ble.Connection = (*connection)(nil)

func newConnection(p *proxy, addr uint64) *connection {
	return &connection{
		p: p, addr: addr, address: macString(addr), log: p.c.log, clk: p.c.clk,
		connected: make(chan wire.BluetoothDeviceConnectionResponse, 1),
		gone:      make(chan struct{}),
	}
}

// onConnectionState is a BluetoothDeviceConnectionResponse for the
// connection's address: the answer to the connect, or a disconnect —
// connected=false — at any time (bluetooth_proxy.cpp send_device_disconnected_).
func (k *connection) onConnectionState(m wire.BluetoothDeviceConnectionResponse) {
	k.mu.Lock()
	connecting := !k.connectingFinished
	k.connectingFinished = true
	k.mu.Unlock()
	if connecting {
		select {
		case k.connected <- m:
		default:
		}
	}
	if !m.Connected {
		k.markGone()
	}
}

// onReply routes a GATT reply to the operation in flight.
func (k *connection) onReply(typ uint16, m *wire.AddressHandle, svc *wire.BluetoothGATTGetServicesResponse) {
	k.mu.Lock()
	o := k.pending
	var r *reply
	switch {
	case o == nil:
	case typ == wire.TypeBluetoothGATTErrorResponse && (o.services || m.Handle == o.handle):
		r = &reply{err: &GATTError{Op: o.name, Handle: m.Handle, Code: m.Error}}
	case o.services && typ == wire.TypeBluetoothGATTGetServicesResponse:
		o.got = append(o.got, svc.Services...)
	case o.services && typ == wire.TypeBluetoothGATTGetServicesDoneResponse:
		r = &reply{services: o.got}
	case !o.services && typ == o.want && m.Handle == o.handle:
		r = &reply{data: m.Data}
	}
	if r != nil {
		k.pending = nil
	}
	k.mu.Unlock()
	if r != nil {
		o.done <- *r
	}
}

// do runs one operation: m sent, its reply awaited for d. An operation that
// overruns ends the connection: the proxy no longer answers, which is a lost
// link or another subscriber, and no BTP session survives either.
func (k *connection) do(ctx context.Context, o *op, m wire.Message, d time.Duration) (reply, error) {
	k.opMu.Lock()
	defer k.opMu.Unlock()
	if k.isGone() {
		return reply{}, fmt.Errorf("esphome: %s %s: %w", o.name, k.address, btp.ErrDisconnected)
	}
	o.done = make(chan reply, 1)
	k.mu.Lock()
	k.pending = o
	k.mu.Unlock()
	unpend := func() {
		k.mu.Lock()
		if k.pending == o {
			k.pending = nil
		}
		k.mu.Unlock()
	}
	if err := k.p.send(m); err != nil {
		unpend()
		return reply{}, fmt.Errorf("esphome: %s %s: %w: %w", o.name, k.address, btp.ErrDisconnected, err)
	}
	t := k.clk.NewTimer(d)
	defer t.Stop()
	select {
	case r := <-o.done:
		return r, r.err
	case <-k.gone:
		unpend()
		return reply{}, fmt.Errorf("esphome: %s %s: %w", o.name, k.address, btp.ErrDisconnected)
	case <-t.C():
		unpend()
		k.log.Info("ble.esphome.gatt_timeout", slog.String("proxy", k.p.name), slog.String("address", k.address), slog.String("op", o.name))
		k.markGone()
		return reply{}, fmt.Errorf("esphome: %s %s: %w: %w", o.name, k.address, ErrTimeout, btp.ErrDisconnected)
	case <-ctx.Done():
		unpend()
		return reply{}, ctx.Err()
	}
}

// discover reads the GATT database (BluetoothGATTGetServicesRequest: the
// services in batches, then the done message; client.py:1600-1622) and
// takes the Matter service's handles: C1, C2 with its CCCD, C3 when there.
func (k *connection) discover(ctx context.Context) error {
	r, err := k.do(ctx, &op{name: "services", services: true},
		&wire.AddressHandle{MessageType: wire.TypeBluetoothGATTGetServicesRequest, Address: k.addr}, bleTimeout)
	if err != nil {
		return err
	}
	words := func(u wire.UUID) [2]uint64 {
		if u.Is128() {
			return u.Words
		}
		return shortWords(u.Short)
	}
	for _, s := range r.services {
		if words(s.UUID) != serviceWords {
			continue
		}
		for _, c := range s.Characteristics {
			switch words(c.UUID) {
			case c1Words:
				k.c1 = c.Handle
			case c2Words:
				k.c2, k.c2Props = c.Handle, c.Properties
				for _, d := range c.Descriptors {
					if words(d.UUID) == cccdWords {
						k.cccd = d.Handle
					}
				}
			case c3Words:
				k.c3 = c.Handle
			}
		}
	}
	if k.c1 == 0 || k.c2 == 0 || k.cccd == 0 {
		return ErrNoMatterService
	}
	return nil
}

// ATTMTU implements ble.Connection: the MTU of the connect's answer; the
// firmware reports the negotiated value, or 23 when the exchange failed
// (bluetooth_connection_bluedroid.cpp:715-733).
func (k *connection) ATTMTU() int { return k.mtu }

// WriteC1 implements ble.Connection: BluetoothGATTWriteRequest with
// response=true (api.proto:2030-2040), answered by BluetoothGATTWriteResponse
// or BluetoothGATTErrorResponse (client.py:1673-1728). A peripheral that is
// gone wraps btp.ErrDisconnected (NobleBleChannel.ts:770-781).
func (k *connection) WriteC1(ctx context.Context, data []byte) error {
	_, err := k.do(ctx, &op{name: "write", want: wire.TypeBluetoothGATTWriteResponse, handle: k.c1},
		&wire.AddressHandle{MessageType: wire.TypeBluetoothGATTWriteRequest, Address: k.addr, Handle: k.c1, Flag: true, Data: data}, bleTimeout)
	return err
}

// SubscribeC2 implements ble.Connection: the proxy registers for the
// characteristic (BluetoothGATTNotifyRequest, api.proto:2062-2070), which is
// local only — "the CCCD write is the API client's responsibility"
// (bluetooth_connection_bluedroid.cpp:325) — so the CCCD is written next,
// 02 00 for C2's indications (as Home Assistant's bleak-esphome backend
// writes it for an indicate-only characteristic). The indications arrive as
// BluetoothGATTNotifyDataResponse and reach on in order, on the
// connection's own goroutine.
func (k *connection) SubscribeC2(ctx context.Context, on func(data []byte)) error {
	k.mu.Lock()
	k.on = on
	k.mu.Unlock()
	if _, err := k.do(ctx, &op{name: "notify", want: wire.TypeBluetoothGATTNotifyResponse, handle: k.c2},
		&wire.AddressHandle{MessageType: wire.TypeBluetoothGATTNotifyRequest, Address: k.addr, Handle: k.c2, Flag: true}, notifyTimeout); err != nil {
		k.stopDelivery()
		return err
	}
	value := cccdIndicate
	if k.c2Props&propIndicate == 0 && k.c2Props&propNotify != 0 {
		value = cccdNotify
	}
	if _, err := k.writeCCCD(ctx, value, bleTimeout); err != nil {
		k.stopDelivery()
		return err
	}
	return nil
}

func (k *connection) writeCCCD(ctx context.Context, value []byte, d time.Duration) (reply, error) {
	return k.do(ctx, &op{name: "cccd", want: wire.TypeBluetoothGATTWriteResponse, handle: k.cccd},
		&wire.AddressHandle{MessageType: wire.TypeBluetoothGATTWriteDescriptorRequest, Address: k.addr, Handle: k.cccd, Data: value}, d)
}

// UnsubscribeC2 implements ble.Connection: the CCCD cleared, which ends
// the peripheral's BTP session (§4.19.4.10, NobleBleChannel.ts:571-574),
// then the proxy's registration dropped, fire and forget as aioesphomeapi
// drops it (client.py:1778-1791).
func (k *connection) UnsubscribeC2(ctx context.Context) error {
	k.stopDelivery()
	if k.isGone() {
		return nil
	}
	_, err := k.writeCCCD(ctx, cccdOff, disconnectTimeout)
	_ = k.p.send(&wire.AddressHandle{MessageType: wire.TypeBluetoothGATTNotifyRequest, Address: k.addr, Handle: k.c2})
	if errors.Is(err, btp.ErrDisconnected) {
		return nil
	}
	return err
}

// ReadC3 implements ble.Connection (BluetoothGATTReadRequest,
// api.proto:2009-2016, answered by BluetoothGATTReadResponse).
func (k *connection) ReadC3(ctx context.Context) ([]byte, error) {
	if k.c3 == 0 {
		return nil, ErrNoC3
	}
	r, err := k.do(ctx, &op{name: "read", want: wire.TypeBluetoothGATTReadResponse, handle: k.c3},
		&wire.AddressHandle{MessageType: wire.TypeBluetoothGATTReadRequest, Address: k.addr, Handle: k.c3}, bleTimeout)
	return r.data, err
}

// Disconnect implements ble.Connection: BluetoothDeviceRequest DISCONNECT
// (api.proto:1918), answered by BluetoothDeviceConnectionResponse
// connected=false, bounded by disconnectTimeout. The connection is gone
// afterwards either way.
func (k *connection) Disconnect() error {
	if k.isGone() {
		return nil
	}
	_ = k.p.send(&wire.BluetoothDeviceRequest{Address: k.addr, RequestType: wire.DeviceRequestDisconnect})
	t := k.clk.NewTimer(disconnectTimeout)
	defer t.Stop()
	select {
	case <-k.gone:
	case <-t.C():
		k.markGone()
	}
	return nil
}

// Disconnected implements ble.Connection.
func (k *connection) Disconnected() <-chan struct{} { return k.gone }

func (k *connection) isGone() bool {
	select {
	case <-k.gone:
		return true
	default:
		return false
	}
}

// markGone ends the connection: Disconnected closes, the operation in
// flight fails, indications stop, the proxy forgets it.
func (k *connection) markGone() {
	k.goneOnce.Do(func() {
		close(k.gone)
		k.stopDelivery()
		k.p.forget(k)
	})
}

// onNotify is a BluetoothGATTNotifyDataResponse for the connection.
func (k *connection) onNotify(handle uint32, data []byte) {
	if handle != k.c2 {
		return
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.on == nil {
		return
	}
	k.queue = append(k.queue, data)
	if !k.delivering {
		k.delivering = true
		go k.deliver()
	}
}

// deliver hands the queued indications to the subscriber, in order, until
// the queue is empty.
func (k *connection) deliver() {
	for {
		k.mu.Lock()
		if len(k.queue) == 0 || k.on == nil {
			k.queue = nil
			k.delivering = false
			k.mu.Unlock()
			return
		}
		v, on := k.queue[0], k.on
		k.queue = k.queue[1:]
		k.mu.Unlock()
		on(v)
	}
}

func (k *connection) stopDelivery() {
	k.mu.Lock()
	k.on = nil
	k.queue = nil
	k.mu.Unlock()
}

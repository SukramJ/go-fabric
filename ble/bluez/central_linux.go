// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

//go:build linux

package bluez

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/SukramJ/go-fabric/controller/ble"
	"github.com/SukramJ/go-fabric/transport/btp"
)

// The bounds matter.js puts on noble's operations
// (packages/nodejs-ble/src/NobleBleChannel.ts at 0ceb202d).
const (
	// connectTimeout bounds Device1.Connect — the "BLE connect timeout"
	// of two minutes (:203).
	connectTimeout = 2 * time.Minute
	// interviewTimeout bounds the wait for the resolved GATT database —
	// the "BLE interview timeout" of one minute (:220).
	interviewTimeout = time.Minute
	// disconnectTimeout bounds StopNotify and Disconnect, which wait on a
	// peripheral that may have vanished — BLE_DISCONNECT_TIMEOUT (:38).
	disconnectTimeout = 5 * time.Second
)

// matterServiceDataLength is the length of the Matter service data a
// commissionable peripheral advertises; matter.js ignores any other
// (NobleBleClient.ts:189-195).
const matterServiceDataLength = 8

// ErrNoAdapter is NewCentral naming an adapter BlueZ does not have.
var ErrNoAdapter = errors.New("bluez: no such adapter")

// DefaultAdapter is the adapter NewCentral uses for an empty name.
const DefaultAdapter = "hci0"

// NewCentral is a [ble.Central] on the BlueZ adapter (hci0 when empty)
// over the system bus. The returned value also has Close, which releases
// the bus. Reaching org.bluez needs system-bus rights: membership in the
// bluetooth group on most distributions, or a polkit rule; without them
// the first call fails with org.freedesktop.DBus.Error.AccessDenied.
func NewCentral(adapter string) (ble.Central, error) {
	if adapter == "" {
		adapter = DefaultAdapter
	}
	path := adapterPath(adapter)
	b, err := dialSystemBus(path)
	if err != nil {
		return nil, err
	}
	c, err := newCentral(b, path)
	if err != nil {
		_ = b.Close()
		return nil, err
	}
	return c, nil
}

func adapterPath(adapter string) dbus.ObjectPath { return dbus.ObjectPath("/org/bluez/" + adapter) }

// central is the BlueZ Central.
type central struct {
	bus     bus
	adapter dbus.ObjectPath
	done    chan struct{}

	mu         sync.Mutex
	closed     bool
	scanning   bool
	callback   func(ble.Peripheral, []byte)
	discovered map[string][]byte          // address → the last Matter service data
	addresses  map[dbus.ObjectPath]string // device path → address
	devices    map[string]dbus.ObjectPath // address → device path
	conns      map[dbus.ObjectPath]*connection
	notifying  map[dbus.ObjectPath]*connection // C2 path → its connection
	watchers   map[dbus.ObjectPath]map[chan struct{}]struct{}
}

var (
	_ ble.Central    = (*central)(nil)
	_ ble.Connection = (*connection)(nil)
)

// newCentral is the central over b: the adapter must exist; the devices
// BlueZ already knows give their addresses.
func newCentral(b bus, adapter dbus.ObjectPath) (*central, error) {
	c := &central{
		bus: b, adapter: adapter, done: make(chan struct{}),
		discovered: map[string][]byte{}, addresses: map[dbus.ObjectPath]string{}, devices: map[string]dbus.ObjectPath{},
		conns: map[dbus.ObjectPath]*connection{}, notifying: map[dbus.ObjectPath]*connection{},
		watchers: map[dbus.ObjectPath]map[chan struct{}]struct{}{},
	}
	objs, err := getManagedObjects(context.Background(), b)
	if err != nil {
		return nil, err
	}
	if _, ok := objs[adapter][ifaceAdapter]; !ok {
		return nil, fmt.Errorf("%w: %s", ErrNoAdapter, adapter)
	}
	for path, ifaces := range objs {
		if props, ok := ifaces[ifaceDevice]; ok && c.under(path) {
			c.learnAddress(path, props)
		}
	}
	go c.dispatch()
	return c, nil
}

// Close stops the central's connections and releases the bus.
func (c *central) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	conns := make([]*connection, 0, len(c.conns))
	for _, conn := range c.conns {
		conns = append(conns, conn)
	}
	c.mu.Unlock()
	for _, conn := range conns {
		conn.markGone()
	}
	close(c.done)
	return c.bus.Close()
}

func (c *central) under(path dbus.ObjectPath) bool {
	return strings.HasPrefix(string(path), string(c.adapter)+"/")
}

// learnAddress records a device's address from its Address property
// (org.bluez.Device.rst, Properties, "string Address"), else from its
// object path (dev_XX_XX_XX_XX_XX_XX).
func (c *central) learnAddress(path dbus.ObjectPath, props map[string]dbus.Variant) string {
	addr, ok := variantAs[string](props["Address"], hasKey(props, "Address"))
	if !ok {
		c.mu.Lock()
		addr, ok = c.addresses[path]
		c.mu.Unlock()
		if ok {
			return addr
		}
		last := string(path)[strings.LastIndex(string(path), "/")+1:]
		addr = strings.ReplaceAll(strings.TrimPrefix(last, "dev_"), "_", ":")
	}
	c.mu.Lock()
	c.addresses[path] = addr
	c.devices[addr] = path
	c.mu.Unlock()
	return addr
}

func hasKey(m map[string]dbus.Variant, k string) bool { _, ok := m[k]; return ok }

// SetDiscoveryCallback implements ble.Central: the peripherals discovered
// so far are reported to the new callback at once
// (NobleBleClient.ts:139-144).
func (c *central) SetDiscoveryCallback(f func(ble.Peripheral, []byte)) {
	c.mu.Lock()
	c.callback = f
	known := make(map[string][]byte, len(c.discovered))
	for a, d := range c.discovered {
		known[a] = d
	}
	c.mu.Unlock()
	if f == nil {
		return
	}
	for a, d := range known {
		f(ble.Peripheral{Address: a}, d)
	}
}

// StartScanning implements ble.Central: a discovery filter for LE with
// every advertisement reported, then the discovery (org.bluez.Adapter.rst,
// Methods, "void SetDiscoveryFilter(dict filter)" with Transport "le" and
// DuplicateData, and "void StartDiscovery()") — noble's
// startScanningAsync([SERVICE_UUID_SHORT], allowDuplicates true)
// (NobleBleClient.ts:84), with the service filter in serviceData. No UUIDs
// entry: BlueZ turns it into the kernel's service discovery, which passes
// only advertisements carrying the UUID in a UUID16/32/128 field, and a
// Matter advertisement carries Flags and Service Data only —
// connectedhomeip's Linux scanner sets Transport alone for that reason
// (src/platform/Linux/bluez/ChipDeviceScanner.cpp:227-236 at 6170af84);
// the ble-virtual job's run 39100460695 showed BlueZ issuing MGMT Start
// Service Discovery and reporting no device for the ADV_IND it received.
// Asked again while the discovery runs, it asks BlueZ again, so a
// discovery the adapter ended resumes; BlueZ's InProgress answer is the
// discovery already running.
func (c *central) StartScanning(ctx context.Context) error {
	c.mu.Lock()
	closed := c.closed
	c.mu.Unlock()
	if closed {
		return nil // NobleBleClient.startScanning when closing (:148)
	}
	filter := map[string]dbus.Variant{
		"Transport":     dbus.MakeVariant("le"),
		"DuplicateData": dbus.MakeVariant(true),
	}
	if _, err := call(ctx, c.bus, c.adapter, ifaceAdapter+".SetDiscoveryFilter", filter); err != nil {
		return err
	}
	if _, err := call(ctx, c.bus, c.adapter, ifaceAdapter+".StartDiscovery"); err != nil && dbusName(err) != "org.bluez.Error.InProgress" {
		return err
	}
	c.mu.Lock()
	c.scanning = true
	c.mu.Unlock()
	return nil
}

// StopScanning implements ble.Central (org.bluez.Adapter.rst, Methods,
// "void StopDiscovery()"). Without a discovery of ours it does nothing; an
// adapter that is off (NotReady) has none to stop.
func (c *central) StopScanning(ctx context.Context) error {
	c.mu.Lock()
	if c.closed || !c.scanning {
		c.mu.Unlock()
		return nil
	}
	c.scanning = false
	c.mu.Unlock()
	if _, err := call(ctx, c.bus, c.adapter, ifaceAdapter+".StopDiscovery"); err != nil && dbusName(err) != "org.bluez.Error.NotReady" {
		return err
	}
	return nil
}

// dispatch routes the bus's signals until the central closes.
func (c *central) dispatch() {
	for {
		select {
		case <-c.done:
			return
		case sig, ok := <-c.bus.Signals():
			if !ok {
				c.busGone()
				return
			}
			c.handle(sig)
		}
	}
}

// busGone ends every connection when the bus closes under the central.
func (c *central) busGone() {
	c.mu.Lock()
	conns := make([]*connection, 0, len(c.conns))
	for _, conn := range c.conns {
		conns = append(conns, conn)
	}
	c.mu.Unlock()
	for _, conn := range conns {
		conn.markGone()
	}
}

func (c *central) handle(sig *dbus.Signal) {
	switch sig.Name {
	case sigIfacesAdded:
		var path dbus.ObjectPath
		var ifaces map[string]map[string]dbus.Variant
		if dbus.Store(sig.Body, &path, &ifaces) != nil || !c.under(path) {
			return
		}
		if props, ok := ifaces[ifaceDevice]; ok {
			addr := c.learnAddress(path, props)
			c.serviceData(addr, props)
		}
	case sigIfacesRemoved:
		var path dbus.ObjectPath
		var ifaces []string
		if dbus.Store(sig.Body, &path, &ifaces) != nil {
			return
		}
		for _, i := range ifaces {
			if i == ifaceDevice {
				c.mu.Lock()
				conn := c.conns[path]
				c.mu.Unlock()
				if conn != nil {
					conn.markGone()
				}
			}
		}
	case sigPropsChanged:
		if !c.under(sig.Path) || len(sig.Body) < 2 {
			return
		}
		iface, _ := sig.Body[0].(string)
		changed, _ := sig.Body[1].(map[string]dbus.Variant)
		switch iface {
		case ifaceDevice:
			c.deviceChanged(sig.Path, changed)
		case ifaceGattChar:
			if v, ok := variantAs[[]byte](changed["Value"], hasKey(changed, "Value")); ok {
				c.mu.Lock()
				conn := c.notifying[sig.Path]
				c.mu.Unlock()
				if conn != nil {
					conn.enqueue(v)
				}
			}
		}
	}
}

// deviceChanged is a Device1 PropertiesChanged: new service data is an
// advertisement (with DuplicateData every one, org.bluez.Adapter.rst,
// SetDiscoveryFilter, "bool DuplicateData"); Connected false ends the
// connection (org.bluez.Device.rst, Properties, "boolean Connected"); every
// change wakes the device's watchers.
func (c *central) deviceChanged(path dbus.ObjectPath, changed map[string]dbus.Variant) {
	if hasKey(changed, "ServiceData") {
		c.serviceData(c.learnAddress(path, changed), changed)
	}
	if connected, ok := variantAs[bool](changed["Connected"], hasKey(changed, "Connected")); ok && !connected {
		c.mu.Lock()
		conn := c.conns[path]
		c.mu.Unlock()
		if conn != nil {
			conn.markGone()
		}
	}
	c.mu.Lock()
	for w := range c.watchers[path] {
		select {
		case w <- struct{}{}:
		default:
		}
	}
	c.mu.Unlock()
}

// serviceData reports the Matter service data in a device's ServiceData
// (org.bluez.Device.rst, Properties, "dict ServiceData": UUID → bytes) —
// NobleBleClient.handleDiscoveredDevice (:165-201), which ignores data of
// any length but eight.
func (c *central) serviceData(addr string, props map[string]dbus.Variant) {
	sd, ok := variantAs[map[string]dbus.Variant](props["ServiceData"], hasKey(props, "ServiceData"))
	if !ok {
		return
	}
	var data []byte
	found := false
	for uuid, v := range sd {
		if strings.EqualFold(uuid, btp.ServiceUUID) {
			data, found = variantAs[[]byte](v, true)
			break
		}
	}
	if !found || len(data) != matterServiceDataLength {
		return
	}
	data = append([]byte(nil), data...)
	c.mu.Lock()
	c.discovered[addr] = data
	cb := c.callback
	c.mu.Unlock()
	if cb != nil {
		cb(ble.Peripheral{Address: addr}, data)
	}
}

// watch returns a channel woken on every Device1 change of path, and its
// release.
func (c *central) watch(path dbus.ObjectPath) (woken <-chan struct{}, release func()) {
	w := make(chan struct{}, 1)
	c.mu.Lock()
	if c.watchers[path] == nil {
		c.watchers[path] = map[chan struct{}]struct{}{}
	}
	c.watchers[path][w] = struct{}{}
	c.mu.Unlock()
	return w, func() {
		c.mu.Lock()
		delete(c.watchers[path], w)
		if len(c.watchers[path]) == 0 {
			delete(c.watchers, path)
		}
		c.mu.Unlock()
	}
}

// devicePath is the peripheral's object path: the one BlueZ reported, else
// the adapter's dev_XX_XX_XX_XX_XX_XX child.
func (c *central) devicePath(address string) dbus.ObjectPath {
	c.mu.Lock()
	defer c.mu.Unlock()
	if p, ok := c.devices[address]; ok {
		return p
	}
	return c.adapter + dbus.ObjectPath("/dev_"+strings.ReplaceAll(strings.ToUpper(address), ":", "_"))
}

// Connect implements ble.Central — the connection and interview of
// NobleBleCentralInterface.openChannel (NobleBleChannel.ts:299-440):
// Device1.Connect within two minutes (org.bluez.Device.rst, Methods,
// "void Connect()"), the GATT database resolved within one minute
// (Properties, "boolean ServicesResolved"), then the Matter service and
// its C1, C2 and C3 characteristics (org.bluez.GattService.rst and
// org.bluez.GattCharacteristic.rst, Properties "UUID", "Device",
// "Service"). A service without C1 or C2 is passed over (:378-384); none
// left is [ErrNoMatterService] and the connection is dropped.
func (c *central) Connect(ctx context.Context, p ble.Peripheral) (ble.Connection, error) {
	path := c.devicePath(p.Address)
	c.mu.Lock()
	closed := c.closed
	open := c.conns[path]
	c.mu.Unlock()
	if closed {
		return nil, errors.New("bluez: central closed")
	}
	if open != nil {
		return nil, fmt.Errorf("bluez: %s: a connection is already open", p.Address)
	}
	cctx, cancel := context.WithTimeout(ctx, connectTimeout)
	_, err := call(cctx, c.bus, path, ifaceDevice+".Connect")
	cancel()
	if err != nil && dbusName(err) != "org.bluez.Error.AlreadyConnected" {
		return nil, err
	}
	conn, err := c.interview(ctx, path)
	if err != nil {
		dctx, dcancel := context.WithTimeout(context.WithoutCancel(ctx), disconnectTimeout)
		_, _ = call(dctx, c.bus, path, ifaceDevice+".Disconnect")
		dcancel()
		return nil, err
	}
	return conn, nil
}

// interview waits for the resolved GATT database and finds the Matter
// characteristics.
func (c *central) interview(ctx context.Context, path dbus.ObjectPath) (*connection, error) {
	ictx, cancel := context.WithTimeout(ctx, interviewTimeout)
	defer cancel()
	woken, release := c.watch(path)
	defer release()
	for {
		v, err := getProperty(ictx, c.bus, path, ifaceDevice, "Connected")
		if err != nil {
			return nil, err
		}
		if connected, _ := variantAs[bool](v, true); !connected {
			return nil, fmt.Errorf("bluez: %s: %w", path, btp.ErrDisconnected)
		}
		v, err = getProperty(ictx, c.bus, path, ifaceDevice, "ServicesResolved")
		if err != nil {
			return nil, err
		}
		if resolved, _ := variantAs[bool](v, true); resolved {
			break
		}
		select {
		case <-woken:
		case <-ictx.Done():
			return nil, fmt.Errorf("bluez: %s: services not resolved: %w", path, ictx.Err())
		}
	}
	objs, err := getManagedObjects(ictx, c.bus)
	if err != nil {
		return nil, err
	}
	conn := c.matterService(path, objs)
	if conn == nil {
		return nil, fmt.Errorf("%w: %s", ErrNoMatterService, path)
	}
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil, errors.New("bluez: central closed")
	}
	c.conns[path] = conn
	c.mu.Unlock()
	// A disconnect between the property read and the registration.
	if v, err := getProperty(ictx, c.bus, path, ifaceDevice, "Connected"); err == nil {
		if connected, _ := variantAs[bool](v, true); !connected {
			conn.markGone()
		}
	}
	return conn, nil
}

// matterService is the connection over the first Matter service of the
// device that has C1 and C2; nil when none has.
func (c *central) matterService(dev dbus.ObjectPath, objs managedObjects) *connection {
	for svc, ifaces := range objs {
		props, ok := ifaces[ifaceGattService]
		if !ok {
			continue
		}
		owner, _ := variantAs[dbus.ObjectPath](props["Device"], hasKey(props, "Device"))
		uuid, _ := variantAs[string](props["UUID"], hasKey(props, "UUID"))
		if owner != dev || !strings.EqualFold(uuid, btp.ServiceUUID) {
			continue
		}
		conn := &connection{c: c, dev: dev, gone: make(chan struct{})}
		for ch, ifaces := range objs {
			cp, ok := ifaces[ifaceGattChar]
			if !ok {
				continue
			}
			if s, _ := variantAs[dbus.ObjectPath](cp["Service"], hasKey(cp, "Service")); s != svc {
				continue
			}
			u, _ := variantAs[string](cp["UUID"], hasKey(cp, "UUID"))
			switch {
			case strings.EqualFold(u, btp.C1CharacteristicUUID):
				conn.c1 = ch
				// org.bluez.GattCharacteristic.rst, Properties, "uint16 MTU":
				// the link's ATT_MTU, where this BlueZ offers it.
				if mtu, ok := variantAs[uint16](cp["MTU"], hasKey(cp, "MTU")); ok {
					conn.mtu = int(mtu)
				}
			case strings.EqualFold(u, btp.C2CharacteristicUUID):
				conn.c2 = ch
			case strings.EqualFold(u, btp.C3CharacteristicUUID):
				conn.c3 = ch
			}
		}
		if conn.c1 != "" && conn.c2 != "" {
			return conn
		}
	}
	return nil
}

// connection is a GATT connection to one peripheral's Matter service.
type connection struct {
	c          *central
	dev        dbus.ObjectPath
	c1, c2, c3 dbus.ObjectPath
	mtu        int
	gone       chan struct{}
	goneOnce   sync.Once
	// mu guards delivering, on, queue and unsubscribe.
	mu          sync.Mutex
	on          func([]byte)
	queue       [][]byte
	delivering  bool
	unsubscribe bool
}

// ATTMTU implements ble.Connection: the MTU property of C1, 0 where BlueZ
// has none, which selects the BTP minimum (NobleBleChannel.ts:679-681).
func (k *connection) ATTMTU() int { return k.mtu }

// WriteC1 implements ble.Connection: a write with response
// (org.bluez.GattCharacteristic.rst, Methods, "void WriteValue(array{byte}
// value, dict options)", option type "request"). A write to a peripheral
// that is gone wraps btp.ErrDisconnected (NobleBleChannel.ts:770-781).
func (k *connection) WriteC1(ctx context.Context, data []byte) error {
	if k.isGone() {
		return fmt.Errorf("bluez: write C1: %w", btp.ErrDisconnected)
	}
	_, err := call(ctx, k.c.bus, k.c1, ifaceGattChar+".WriteValue", data, map[string]dbus.Variant{"type": dbus.MakeVariant("request")})
	if err != nil && (k.isGone() || dbusName(err) == "org.bluez.Error.NotConnected") {
		return fmt.Errorf("%w: %w", btp.ErrDisconnected, err)
	}
	return err
}

// SubscribeC2 implements ble.Connection: C2's values arrive as
// PropertiesChanged of its Value after StartNotify
// (org.bluez.GattCharacteristic.rst, Methods, "void StartNotify()";
// Properties, "array{byte} Value"); on receives each, in order, on the
// connection's own goroutine.
func (k *connection) SubscribeC2(ctx context.Context, on func(data []byte)) error {
	if k.isGone() {
		return fmt.Errorf("bluez: subscribe C2: %w", btp.ErrDisconnected)
	}
	k.mu.Lock()
	k.on = on
	k.unsubscribe = false
	k.mu.Unlock()
	k.c.mu.Lock()
	k.c.notifying[k.c2] = k
	k.c.mu.Unlock()
	if _, err := call(ctx, k.c.bus, k.c2, ifaceGattChar+".StartNotify"); err != nil {
		k.c.mu.Lock()
		delete(k.c.notifying, k.c2)
		k.c.mu.Unlock()
		return err
	}
	return nil
}

// UnsubscribeC2 implements ble.Connection (org.bluez.GattCharacteristic.rst,
// Methods, "void StopNotify()"), bounded as matter.js bounds it
// (NobleBleChannel.ts:571-574, BLE_DISCONNECT_TIMEOUT).
func (k *connection) UnsubscribeC2(ctx context.Context) error {
	k.stopNotify()
	if k.isGone() {
		return nil
	}
	uctx, cancel := context.WithTimeout(ctx, disconnectTimeout)
	defer cancel()
	_, err := call(uctx, k.c.bus, k.c2, ifaceGattChar+".StopNotify")
	return err
}

func (k *connection) stopNotify() {
	k.c.mu.Lock()
	if k.c.notifying[k.c2] == k {
		delete(k.c.notifying, k.c2)
	}
	k.c.mu.Unlock()
	k.mu.Lock()
	k.on = nil
	k.queue = nil
	k.unsubscribe = true
	k.mu.Unlock()
}

// ReadC3 implements ble.Connection (org.bluez.GattCharacteristic.rst,
// Methods, "array{byte} ReadValue(dict options)").
func (k *connection) ReadC3(ctx context.Context) ([]byte, error) {
	if k.c3 == "" {
		return nil, errors.New("bluez: the peripheral has no C3")
	}
	body, err := call(ctx, k.c.bus, k.c3, ifaceGattChar+".ReadValue", map[string]dbus.Variant{})
	if err != nil {
		return nil, err
	}
	var v []byte
	if err := dbus.Store(body, &v); err != nil {
		return nil, wrap(ifaceGattChar+".ReadValue", k.c3, err)
	}
	return v, nil
}

// Disconnect implements ble.Connection (org.bluez.Device.rst, Methods,
// "void Disconnect()"), bounded by BLE_DISCONNECT_TIMEOUT
// (NobleBleChannel.ts:267). The connection is gone afterwards either way.
func (k *connection) Disconnect() error {
	if k.isGone() {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), disconnectTimeout)
	defer cancel()
	_, err := call(ctx, k.c.bus, k.dev, ifaceDevice+".Disconnect")
	k.markGone()
	if dbusName(err) == "org.bluez.Error.NotConnected" {
		return nil
	}
	return err
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

// markGone ends the connection: Disconnected closes, notifications stop,
// the central forgets it.
func (k *connection) markGone() {
	k.goneOnce.Do(func() {
		k.stopNotify()
		k.c.mu.Lock()
		if k.c.conns[k.dev] == k {
			delete(k.c.conns, k.dev)
		}
		k.c.mu.Unlock()
		close(k.gone)
	})
}

// enqueue hands a C2 value to the delivery goroutine, starting it.
func (k *connection) enqueue(v []byte) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.on == nil || k.unsubscribe {
		return
	}
	k.queue = append(k.queue, append([]byte(nil), v...))
	if !k.delivering {
		k.delivering = true
		go k.deliver()
	}
}

// deliver hands the queued C2 values to the subscriber, in order, until
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

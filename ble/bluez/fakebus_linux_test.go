// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

//go:build linux

package bluez

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/SukramJ/go-fabric/controller/ble"
	"github.com/SukramJ/go-fabric/controller/ble/bletest"
	"github.com/SukramJ/go-fabric/transport/btp"
)

// fakeBus is an in-process org.bluez: an object tree with properties, the
// calls the central makes, and the signals BlueZ would send.
type fakeBus struct {
	mu      sync.Mutex
	objs    managedObjects
	calls   []string
	args    map[string][]any
	errs    map[string]error // "path method" or "method" → the error reply
	devices map[dbus.ObjectPath]*fakeDevice
	sigs    chan *dbus.Signal
	closed  bool
	// afterManaged, when set, changes the objects (under mu, without a
	// signal) once a GetManagedObjects answer is taken: a change BlueZ
	// made between that read and the next.
	afterManaged func(managedObjects)
	// exports are the methods this process exported (path → interface →
	// member → func); emitted are the signals it sent.
	exports map[dbus.ObjectPath]map[string]map[string]any
	emitted []*dbus.Signal
	// app and adv are what bluetoothd holds registered: the application's
	// objects as it read them, the advertisement's properties.
	app managedObjects
	adv map[string]dbus.Variant
	// onDisconnect, when set, runs once inside the next Device1.Disconnect
	// bluetoothd answers.
	onDisconnect func()
}

const testAdapter = dbus.ObjectPath("/org/bluez/hci0")

func newFakeBus() *fakeBus {
	b := &fakeBus{
		objs:    managedObjects{testAdapter: {ifaceAdapter: {"Address": dbus.MakeVariant("00:AA:01:00:00:23")}}},
		args:    map[string][]any{},
		errs:    map[string]error{},
		devices: map[dbus.ObjectPath]*fakeDevice{},
		sigs:    make(chan *dbus.Signal, 4096),
		exports: map[dbus.ObjectPath]map[string]map[string]any{},
	}
	return b
}

// Export implements bus: what this process exports, recorded.
func (b *fakeBus) Export(path dbus.ObjectPath, iface string, methods map[string]any) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if methods == nil {
		delete(b.exports[path], iface)
		if len(b.exports[path]) == 0 {
			delete(b.exports, path)
		}
		return nil
	}
	if b.exports[path] == nil {
		b.exports[path] = map[string]map[string]any{}
	}
	b.exports[path][iface] = methods
	return nil
}

// Emit implements bus: a signal this process sends, recorded; a
// PropertiesChanged of the registered advertisement updates what
// bluetoothd holds, as BlueZ's property watch does.
func (b *fakeBus) Emit(path dbus.ObjectPath, name string, values ...any) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.emitted = append(b.emitted, &dbus.Signal{Path: path, Name: name, Body: copyBody(values)})
	if name == sigPropsChanged && len(values) >= 2 && values[0] == ifaceAdvertise && b.adv != nil {
		if changed, ok := values[1].(map[string]dbus.Variant); ok {
			maps.Copy(b.adv, changed)
		}
	}
	return nil
}

// method is an exported method, nil when it is not exported.
func (b *fakeBus) method(path dbus.ObjectPath, iface, member string) any {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.exports[path][iface][member]
}

// exported reports whether anything is exported at path.
func (b *fakeBus) exported(path dbus.ObjectPath) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.exports[path]) > 0
}

func (b *fakeBus) Signals() <-chan *dbus.Signal { return b.sigs }

func (b *fakeBus) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.closed {
		b.closed = true
		close(b.sigs)
	}
	return nil
}

func (b *fakeBus) emit(s *dbus.Signal) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.closed {
		s.Body = copyBody(s.Body)
		b.sigs <- s
	}
}

// copyBody copies a signal's maps, so the central never reads a map the
// bus changes afterwards.
func copyBody(body []any) []any {
	out := make([]any, len(body))
	for i, v := range body {
		switch m := v.(type) {
		case map[string]dbus.Variant:
			out[i] = maps.Clone(m)
		case map[string]map[string]dbus.Variant:
			c := make(map[string]map[string]dbus.Variant, len(m))
			for k, props := range m {
				c[k] = maps.Clone(props)
			}
			out[i] = c
		default:
			out[i] = v
		}
	}
	return out
}

// called is the calls so far, as "path method".
func (b *fakeBus) called() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]string(nil), b.calls...)
}

func (b *fakeBus) lastArgs(method string) []any {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.args[method]
}

// errHang, given to failWith, makes the call block until its context ends.
var errHang = errors.New("fakebus: hang")

func (b *fakeBus) failWith(key string, err error) {
	b.mu.Lock()
	b.errs[key] = err
	b.mu.Unlock()
}

func (b *fakeBus) Call(ctx context.Context, path dbus.ObjectPath, method string, args ...any) ([]any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	b.mu.Lock()
	b.calls = append(b.calls, string(path)+" "+method)
	b.args[method] = args
	err, ok := b.errs[string(path)+" "+method]
	if !ok {
		err, ok = b.errs[method]
	}
	dev := b.devices[path]
	if dev == nil {
		for p, d := range b.devices {
			if strings.HasPrefix(string(path), string(p)+"/") {
				dev = d
			}
		}
	}
	b.mu.Unlock()
	if ok {
		if errors.Is(err, errHang) {
			<-ctx.Done() // a call the daemon never answers
			return nil, ctx.Err()
		}
		return nil, err
	}
	switch method {
	case methodGetManaged:
		b.mu.Lock()
		defer b.mu.Unlock()
		out := managedObjects{}
		for p, ifaces := range b.objs {
			out[p] = map[string]map[string]dbus.Variant{}
			for i, props := range ifaces {
				out[p][i] = map[string]dbus.Variant{}
				for k, v := range props {
					out[p][i][k] = v
				}
			}
		}
		if b.afterManaged != nil {
			b.afterManaged(b.objs)
		}
		return []any{out}, nil
	case methodGetProperty:
		b.mu.Lock()
		defer b.mu.Unlock()
		iface, name := args[0].(string), args[1].(string)
		v, ok := b.objs[path][iface][name]
		if !ok {
			return nil, dbus.Error{Name: "org.freedesktop.DBus.Error.InvalidArgs", Body: []any{"No such property '" + name + "'"}}
		}
		return []any{v}, nil
	}
	if dev != nil {
		return dev.call(ctx, path, method, args)
	}
	return b.bluetoothd(path, method, args)
}

// bluetoothd answers the peripheral's calls as bluetoothd does: the
// application's registration reads its objects through the exported
// ObjectManager, the advertisement's through the exported Properties.
func (b *fakeBus) bluetoothd(path dbus.ObjectPath, method string, args []any) ([]any, error) {
	switch method {
	case ifaceGattManager + ".RegisterApplication":
		root, _ := args[0].(dbus.ObjectPath)
		get, ok := b.method(root, ifaceObjectMgr, "GetManagedObjects").(func() (map[dbus.ObjectPath]map[string]map[string]dbus.Variant, *dbus.Error))
		if !ok {
			return nil, dbus.Error{Name: "org.bluez.Error.Failed", Body: []any{"no ObjectManager at " + string(root)}}
		}
		objs, derr := get()
		if derr != nil {
			return nil, derr
		}
		b.mu.Lock()
		b.app = objs
		b.mu.Unlock()
	case ifaceGattManager + ".UnregisterApplication":
		b.mu.Lock()
		b.app = nil
		b.mu.Unlock()
	case ifaceAdvertising + ".RegisterAdvertisement":
		adv, _ := args[0].(dbus.ObjectPath)
		getAll, ok := b.method(adv, ifaceProperties, "GetAll").(func(string) (map[string]dbus.Variant, *dbus.Error))
		if !ok {
			return nil, dbus.Error{Name: "org.bluez.Error.Failed", Body: []any{"no Properties at " + string(adv)}}
		}
		props, derr := getAll(ifaceAdvertise)
		if derr != nil {
			return nil, derr
		}
		b.mu.Lock()
		b.adv = maps.Clone(props)
		b.mu.Unlock()
	case ifaceAdvertising + ".UnregisterAdvertisement":
		b.mu.Lock()
		b.adv = nil
		b.mu.Unlock()
	case ifaceDevice + ".Disconnect":
		b.mu.Lock()
		hook := b.onDisconnect
		b.onDisconnect = nil
		b.mu.Unlock()
		if hook != nil {
			hook()
		}
		b.setProps(path, ifaceDevice, map[string]dbus.Variant{"Connected": dbus.MakeVariant(false)})
	}
	return nil, nil
}

// registered is what bluetoothd holds: the application's objects and the
// advertisement's properties (nil when not registered).
func (b *fakeBus) registered() (app managedObjects, adv map[string]dbus.Variant) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.app, maps.Clone(b.adv)
}

// fakeCentral is a central connected to the peripheral, as bluetoothd
// serves it: the C1 socket it writes to, the C2 socket it reads
// indications from and confirms on.
type fakeCentral struct {
	b      *fakeBus
	device dbus.ObjectPath
	write  *os.File
	notify *os.File
}

// connectCentral adds a connected central's Device1 under the adapter.
func (b *fakeBus) connectCentral(addr string) *fakeCentral {
	path := devPath(addr)
	b.addObject(path, map[string]map[string]dbus.Variant{ifaceDevice: {
		"Address": dbus.MakeVariant(addr), "Connected": dbus.MakeVariant(true),
	}})
	return &fakeCentral{b: b, device: path}
}

func (fc *fakeCentral) options(mtu uint16) map[string]dbus.Variant {
	return map[string]dbus.Variant{"device": dbus.MakeVariant(fc.device), "mtu": dbus.MakeVariant(mtu), "link": dbus.MakeVariant("LE")}
}

type acquireFunc = func(map[string]dbus.Variant) (dbus.UnixFD, uint16, *dbus.Error)

// acquire calls the exported AcquireWrite (C1) or AcquireNotify (C2), as
// bluetoothd does at the central's first write and at its subscription.
func (fc *fakeCentral) acquire(char dbus.ObjectPath, member string, mtu uint16) (*os.File, error) {
	f, ok := fc.b.method(char, ifaceGattChar, member).(acquireFunc)
	if !ok {
		return nil, fmt.Errorf("%s not exported at %s", member, char)
	}
	fd, gotMTU, derr := f(fc.options(mtu))
	if derr != nil {
		return nil, derr
	}
	if gotMTU != mtu {
		return nil, fmt.Errorf("%s answered MTU %d, want %d", member, gotMTU, mtu)
	}
	return os.NewFile(uintptr(fd), member), nil
}

func (fc *fakeCentral) acquireWrite(char dbus.ObjectPath, mtu uint16) error {
	f, err := fc.acquire(char, "AcquireWrite", mtu)
	if err == nil {
		fc.write = f
	}
	return err
}

func (fc *fakeCentral) acquireNotify(char dbus.ObjectPath, mtu uint16) error {
	f, err := fc.acquire(char, "AcquireNotify", mtu)
	if err == nil {
		fc.notify = f
	}
	return err
}

// writeC1 is one C1 write over the acquired socket.
func (fc *fakeCentral) writeC1(v []byte) error {
	_, err := fc.write.Write(v)
	return err
}

// indication reads the next indication from the C2 socket.
func (fc *fakeCentral) indication() ([]byte, error) {
	if err := fc.notify.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return nil, err
	}
	buf := make([]byte, 512)
	n, err := fc.notify.Read(buf)
	if err != nil {
		return nil, err
	}
	return buf[:n], nil
}

// confirm is BlueZ 5.80's confirmation: a byte 1 on the C2 socket.
func (fc *fakeCentral) confirm() error {
	_, err := fc.notify.Write([]byte{1})
	return err
}

// unsubscribe is the central's CCC write of 0: bluetoothd closes its C2
// socket.
func (fc *fakeCentral) unsubscribe() error { return fc.notify.Close() }

// disconnect is the link lost: Connected false, the sockets closed.
func (fc *fakeCentral) disconnect() {
	fc.b.setProps(fc.device, ifaceDevice, map[string]dbus.Variant{"Connected": dbus.MakeVariant(false)})
	if fc.write != nil {
		_ = fc.write.Close()
	}
	if fc.notify != nil {
		_ = fc.notify.Close()
	}
}

// setProps sets properties of an object and sends their PropertiesChanged.
func (b *fakeBus) setProps(path dbus.ObjectPath, iface string, props map[string]dbus.Variant) {
	b.mu.Lock()
	if b.objs[path] == nil {
		b.objs[path] = map[string]map[string]dbus.Variant{}
	}
	if b.objs[path][iface] == nil {
		b.objs[path][iface] = map[string]dbus.Variant{}
	}
	for k, v := range props {
		b.objs[path][iface][k] = v
	}
	b.mu.Unlock()
	b.emit(&dbus.Signal{Path: path, Name: sigPropsChanged, Body: []any{iface, props, []string{}}})
}

// addObject adds an object and sends InterfacesAdded.
func (b *fakeBus) addObject(path dbus.ObjectPath, ifaces map[string]map[string]dbus.Variant) {
	b.mu.Lock()
	b.objs[path] = map[string]map[string]dbus.Variant{}
	for k, props := range ifaces {
		b.objs[path][k] = maps.Clone(props)
	}
	b.mu.Unlock()
	b.emit(&dbus.Signal{Path: "/", Name: sigIfacesAdded, Body: []any{path, ifaces}})
}

// fakeDevice is a BlueZ device whose GATT side is a bletest peripheral.
type fakeDevice struct {
	bus     *fakeBus
	path    dbus.ObjectPath
	central *bletest.Central
	addr    string
	mtu     uint16
	// noC2 leaves C2 out of the GATT database.
	noC2 bool
	// lateResolve sends ServicesResolved after Connect has returned.
	lateResolve bool

	mu   sync.Mutex
	conn ble.Connection
}

func devPath(addr string) dbus.ObjectPath {
	return testAdapter + dbus.ObjectPath("/dev_"+strings.ReplaceAll(addr, ":", "_"))
}

// addDevice puts a bletest peripheral behind the bus.
func (b *fakeBus) addDevice(p *bletest.Peripheral, mtu uint16) *fakeDevice {
	d := &fakeDevice{bus: b, path: devPath(p.Address), central: bletest.NewCentral(p), addr: p.Address, mtu: mtu}
	b.mu.Lock()
	b.devices[d.path] = d
	b.mu.Unlock()
	return d
}

// advertise sends the device's advertisement as BlueZ does: the device
// object with its ServiceData the first time, a ServiceData change after.
func (d *fakeDevice) advertise(serviceData []byte) {
	sd := dbus.MakeVariant(map[string]dbus.Variant{btp.ServiceUUID: dbus.MakeVariant(serviceData)})
	d.bus.mu.Lock()
	_, known := d.bus.objs[d.path]
	d.bus.mu.Unlock()
	if !known {
		d.bus.addObject(d.path, map[string]map[string]dbus.Variant{ifaceDevice: {
			"Address": dbus.MakeVariant(d.addr), "Connected": dbus.MakeVariant(false), "ServicesResolved": dbus.MakeVariant(false), "ServiceData": sd,
		}})
		return
	}
	d.bus.setProps(d.path, ifaceDevice, map[string]dbus.Variant{"ServiceData": sd})
}

func (d *fakeDevice) svc() dbus.ObjectPath { return d.path + "/service000a" }
func (d *fakeDevice) char(n int) dbus.ObjectPath {
	return dbus.ObjectPath(fmt.Sprintf("%s/char%04x", d.svc(), n))
}

func (d *fakeDevice) connection() ble.Connection { d.mu.Lock(); defer d.mu.Unlock(); return d.conn }

func (d *fakeDevice) setConnected(on bool) {
	d.bus.setProps(d.path, ifaceDevice, map[string]dbus.Variant{"Connected": dbus.MakeVariant(on)})
}

func (d *fakeDevice) call(ctx context.Context, path dbus.ObjectPath, method string, args []any) ([]any, error) {
	switch {
	case path == d.path && method == ifaceDevice+".Connect":
		conn, err := d.central.Connect(ctx, ble.Peripheral{Address: d.addr})
		if err != nil {
			return nil, dbus.Error{Name: "org.bluez.Error.Failed", Body: []any{err.Error()}}
		}
		d.mu.Lock()
		d.conn = conn
		d.mu.Unlock()
		d.setConnected(true)
		resolve := func() {
			chars := map[dbus.ObjectPath]string{d.char(0x0b): btp.C1CharacteristicUUID, d.char(0x0f): btp.C3CharacteristicUUID}
			if !d.noC2 {
				chars[d.char(0x0d)] = btp.C2CharacteristicUUID
			}
			d.bus.addObject(d.svc(), map[string]map[string]dbus.Variant{ifaceGattService: {
				"UUID": dbus.MakeVariant(btp.ServiceUUID), "Device": dbus.MakeVariant(d.path), "Primary": dbus.MakeVariant(true),
			}})
			for p, u := range chars {
				props := map[string]dbus.Variant{"UUID": dbus.MakeVariant(strings.ToLower(u)), "Service": dbus.MakeVariant(d.svc())}
				if d.mtu != 0 {
					props["MTU"] = dbus.MakeVariant(d.mtu)
				}
				d.bus.addObject(p, map[string]map[string]dbus.Variant{ifaceGattChar: props})
			}
			d.bus.setProps(d.path, ifaceDevice, map[string]dbus.Variant{"ServicesResolved": dbus.MakeVariant(true)})
		}
		if d.lateResolve {
			go func() { time.Sleep(50 * time.Millisecond); resolve() }()
		} else {
			resolve()
		}
		return nil, nil
	case path == d.path && method == ifaceDevice+".Disconnect":
		if c := d.connection(); c != nil {
			_ = c.Disconnect()
		}
		d.bus.setProps(d.path, ifaceDevice, map[string]dbus.Variant{"Connected": dbus.MakeVariant(false), "ServicesResolved": dbus.MakeVariant(false)})
		return nil, nil
	case path == d.char(0x0b) && method == ifaceGattChar+".WriteValue":
		return nil, d.connection().WriteC1(ctx, args[0].([]byte))
	case path == d.char(0x0d) && method == ifaceGattChar+".StartNotify":
		return nil, d.connection().SubscribeC2(ctx, func(v []byte) {
			d.bus.setProps(d.char(0x0d), ifaceGattChar, map[string]dbus.Variant{"Value": dbus.MakeVariant(v)})
		})
	case path == d.char(0x0d) && method == ifaceGattChar+".StopNotify":
		return nil, d.connection().UnsubscribeC2(ctx)
	case path == d.char(0x0f) && method == ifaceGattChar+".ReadValue":
		return []any{[]byte{0x15, 0x30}}, nil
	}
	return nil, nil
}

// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

//go:build linux

package bluez

import (
	"context"
	"errors"
	"fmt"
	"maps"
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
}

const testAdapter = dbus.ObjectPath("/org/bluez/hci0")

func newFakeBus() *fakeBus {
	b := &fakeBus{
		objs:    managedObjects{testAdapter: {ifaceAdapter: {"Address": dbus.MakeVariant("00:AA:01:00:00:23")}}},
		args:    map[string][]any{},
		errs:    map[string]error{},
		devices: map[dbus.ObjectPath]*fakeDevice{},
		sigs:    make(chan *dbus.Signal, 4096),
	}
	return b
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
	return nil, nil
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

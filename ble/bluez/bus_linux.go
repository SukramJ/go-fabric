// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

//go:build linux

package bluez

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"syscall"

	"github.com/godbus/dbus/v5"
)

// The D-Bus names this package speaks.
const (
	service           = "org.bluez"
	ifaceAdapter      = "org.bluez.Adapter1"
	ifaceDevice       = "org.bluez.Device1"
	ifaceGattService  = "org.bluez.GattService1"
	ifaceGattChar     = "org.bluez.GattCharacteristic1"
	ifaceGattManager  = "org.bluez.GattManager1"
	ifaceAdvertising  = "org.bluez.LEAdvertisingManager1"
	ifaceAdvertise    = "org.bluez.LEAdvertisement1"
	ifaceProperties   = "org.freedesktop.DBus.Properties"
	ifaceObjectMgr    = "org.freedesktop.DBus.ObjectManager"
	sigIfacesAdded    = ifaceObjectMgr + ".InterfacesAdded"
	sigIfacesRemoved  = ifaceObjectMgr + ".InterfacesRemoved"
	sigPropsChanged   = ifaceProperties + ".PropertiesChanged"
	methodGetManaged  = ifaceObjectMgr + ".GetManagedObjects"
	methodGetProperty = ifaceProperties + ".Get"
)

// bus is the seam around the system bus: a method call on an org.bluez
// object, the signals of org.bluez, the objects this process exports to
// bluetoothd and the signals it sends from them, and closing. The Central
// and the Peripheral speak only through it, so a test drives them over an
// in-process bus that plays bluetoothd.
type bus interface {
	// Call calls method (interface.member) on the org.bluez object at
	// path and returns the reply's body.
	Call(ctx context.Context, path dbus.ObjectPath, method string, args ...any) ([]any, error)
	// Signals delivers org.bluez's ObjectManager and Properties signals,
	// in order.
	Signals() <-chan *dbus.Signal
	// Export serves methods (member name → func, godbus's method table)
	// as interface iface of this process's object at path; nil withdraws
	// them. A dbus.UnixFD a method returns is this process's copy of the
	// descriptor: the bus closes it once the reply carrying it was sent,
	// so the receiver holds the only copy — closing it is what the other
	// end sees.
	Export(path dbus.ObjectPath, iface string, methods map[string]any) error
	// Emit sends the signal name (interface.member) from this process's
	// object at path.
	Emit(path dbus.ObjectPath, name string, values ...any) error
	// Close releases the bus.
	Close() error
}

// managedObjects is GetManagedObjects' reply.
type managedObjects = map[dbus.ObjectPath]map[string]map[string]dbus.Variant

// systemBus is the bus over godbus's system-bus connection.
type systemBus struct {
	conn    *dbus.Conn
	signals chan *dbus.Signal
	sent    *sentFDs
	// exportMu serialises Export: see there.
	exportMu sync.Mutex
}

// dialSystemBus connects to the system bus and subscribes to org.bluez's
// InterfacesAdded, InterfacesRemoved and PropertiesChanged under the
// adapter's path.
func dialSystemBus(adapterPath dbus.ObjectPath) (*systemBus, error) {
	sent := newSentFDs()
	conn, err := dbus.ConnectSystemBus(dbus.WithOutgoingInterceptor(sent.intercept), dbus.WithSerialGenerator(sent))
	if err != nil {
		return nil, wrap("connect to the system bus", "", err)
	}
	matches := [][]dbus.MatchOption{
		{dbus.WithMatchSender(service), dbus.WithMatchInterface(ifaceObjectMgr), dbus.WithMatchMember("InterfacesAdded")},
		{dbus.WithMatchSender(service), dbus.WithMatchInterface(ifaceObjectMgr), dbus.WithMatchMember("InterfacesRemoved")},
		{dbus.WithMatchSender(service), dbus.WithMatchInterface(ifaceProperties), dbus.WithMatchMember("PropertiesChanged"), dbus.WithMatchPathNamespace(adapterPath)},
	}
	for _, m := range matches {
		if err := conn.AddMatchSignal(m...); err != nil {
			_ = conn.Close()
			return nil, wrap("org.freedesktop.DBus.AddMatch", "", err)
		}
	}
	b := &systemBus{conn: conn, signals: make(chan *dbus.Signal, 256), sent: sent}
	conn.Signal(b.signals)
	return b, nil
}

func (b *systemBus) Call(ctx context.Context, path dbus.ObjectPath, method string, args ...any) ([]any, error) {
	call := b.conn.Object(service, path).CallWithContext(ctx, method, 0, args...)
	if call.Err != nil {
		return nil, call.Err
	}
	return call.Body, nil
}

func (b *systemBus) Signals() <-chan *dbus.Signal { return b.signals }

// Export is serialised here because godbus does not do it: its export
// reads the handler's object map without the handler's lock
// (Conn.export, export.go:396 and :406 at v5.2.4, through
// defaultHandler.PathExists, default_handler.go:41-43) while AddObject
// and DeleteObject write it under that lock (default_handler.go:104-113).
// Two exports at once — the GATT application's from Serve and the
// advertisement's from Advertise, which the bridge runs on two goroutines
// — are a concurrent map read and write, which the runtime ends the
// process for. An incoming call's lookup takes the handler's lock
// (LookupObject, default_handler.go:67-69) and needs no help.
func (b *systemBus) Export(path dbus.ObjectPath, iface string, methods map[string]any) error {
	b.exportMu.Lock()
	defer b.exportMu.Unlock()
	if methods == nil {
		return b.conn.Export(nil, path, iface)
	}
	return b.conn.ExportMethodTable(methods, path, iface)
}

func (b *systemBus) Emit(path dbus.ObjectPath, name string, values ...any) error {
	return b.conn.Emit(path, name, values...)
}

func (b *systemBus) Close() error {
	err := b.conn.Close()
	b.sent.closeAll()
	return err
}

// sentFDs closes the descriptors a method reply passes once the reply was
// sent: godbus's interceptor sees the reply (with its serial) before it
// goes out, and its serial generator retires the serial once it went out.
// It is the serial generator godbus uses otherwise (a serial is not reused
// while in use).
type sentFDs struct {
	mu      sync.Mutex
	next    uint32
	used    map[uint32]bool
	pending map[uint32][]int
}

func newSentFDs() *sentFDs {
	return &sentFDs{next: 1, used: map[uint32]bool{0: true}, pending: map[uint32][]int{}}
}

// GetSerial implements dbus.SerialGenerator.
func (s *sentFDs) GetSerial() uint32 {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := s.next
	for s.used[n] {
		n++
	}
	s.used[n] = true
	s.next = n + 1
	return n
}

// RetireSerial implements dbus.SerialGenerator: a reply's serial is
// retired once it was sent, and the descriptors it carried are closed.
func (s *sentFDs) RetireSerial(serial uint32) {
	s.mu.Lock()
	delete(s.used, serial)
	fds := s.pending[serial]
	delete(s.pending, serial)
	s.mu.Unlock()
	for _, fd := range fds {
		_ = syscall.Close(fd)
	}
}

// intercept records the descriptors of a method reply about to be sent.
func (s *sentFDs) intercept(msg *dbus.Message) {
	if msg.Type != dbus.TypeMethodReply {
		return
	}
	var fds []int
	for _, v := range msg.Body {
		if fd, ok := v.(dbus.UnixFD); ok {
			fds = append(fds, int(fd))
		}
	}
	if len(fds) == 0 {
		return
	}
	s.mu.Lock()
	s.pending[msg.Serial()] = append(s.pending[msg.Serial()], fds...)
	s.mu.Unlock()
}

// closeAll closes the descriptors of replies that were never sent.
func (s *sentFDs) closeAll() {
	s.mu.Lock()
	pending := s.pending
	s.pending = map[uint32][]int{}
	s.mu.Unlock()
	for _, fds := range pending {
		for _, fd := range fds {
			_ = syscall.Close(fd)
		}
	}
}

// wrap is err as an [*Error] carrying the D-Bus error name.
func wrap(op string, path dbus.ObjectPath, err error) error {
	if err == nil {
		return nil
	}
	e := &Error{Op: op, Path: string(path), Err: err}
	var de dbus.Error
	var dep *dbus.Error
	switch {
	case errors.As(err, &de):
		e.Name = de.Name
	case errors.As(err, &dep):
		e.Name = dep.Name
	}
	return e
}

// dbusName is err's D-Bus error name, empty when it has none.
func dbusName(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Name
	}
	return ""
}

// variantAs is v's value as T.
func variantAs[T any](v dbus.Variant, ok bool) (T, bool) {
	var zero T
	if !ok {
		return zero, false
	}
	t, ok := v.Value().(T)
	return t, ok
}

// call is b.Call wrapped as an [*Error].
func call(ctx context.Context, b bus, path dbus.ObjectPath, method string, args ...any) ([]any, error) {
	body, err := b.Call(ctx, path, method, args...)
	return body, wrap(method, path, err)
}

// getProperty reads one property (org.freedesktop.DBus.Properties.Get).
func getProperty(ctx context.Context, b bus, path dbus.ObjectPath, iface, name string) (dbus.Variant, error) {
	body, err := call(ctx, b, path, methodGetProperty, iface, name)
	if err != nil {
		return dbus.Variant{}, err
	}
	var v dbus.Variant
	if err := dbus.Store(body, &v); err != nil {
		return dbus.Variant{}, wrap(methodGetProperty, path, fmt.Errorf("%s.%s: %w", iface, name, err))
	}
	return v, nil
}

// getManagedObjects is org.bluez's object tree.
func getManagedObjects(ctx context.Context, b bus) (managedObjects, error) {
	body, err := call(ctx, b, "/", methodGetManaged)
	if err != nil {
		return nil, err
	}
	var objs managedObjects
	if err := dbus.Store(body, &objs); err != nil {
		return nil, wrap(methodGetManaged, "/", err)
	}
	return objs, nil
}

// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

//go:build linux

package bluez

import (
	"context"
	"errors"
	"fmt"

	"github.com/godbus/dbus/v5"
)

// The D-Bus names this package speaks.
const (
	service           = "org.bluez"
	ifaceAdapter      = "org.bluez.Adapter1"
	ifaceDevice       = "org.bluez.Device1"
	ifaceGattService  = "org.bluez.GattService1"
	ifaceGattChar     = "org.bluez.GattCharacteristic1"
	ifaceProperties   = "org.freedesktop.DBus.Properties"
	ifaceObjectMgr    = "org.freedesktop.DBus.ObjectManager"
	sigIfacesAdded    = ifaceObjectMgr + ".InterfacesAdded"
	sigIfacesRemoved  = ifaceObjectMgr + ".InterfacesRemoved"
	sigPropsChanged   = ifaceProperties + ".PropertiesChanged"
	methodGetManaged  = ifaceObjectMgr + ".GetManagedObjects"
	methodGetProperty = ifaceProperties + ".Get"
)

// bus is the seam around the system bus: a method call on an org.bluez
// object, the signals of org.bluez, and closing. The Central speaks only
// through it, so a test drives the Central over an in-process bus.
type bus interface {
	// Call calls method (interface.member) on the org.bluez object at
	// path and returns the reply's body.
	Call(ctx context.Context, path dbus.ObjectPath, method string, args ...any) ([]any, error)
	// Signals delivers org.bluez's ObjectManager and Properties signals,
	// in order.
	Signals() <-chan *dbus.Signal
	// Close releases the bus.
	Close() error
}

// managedObjects is GetManagedObjects' reply.
type managedObjects = map[dbus.ObjectPath]map[string]map[string]dbus.Variant

// systemBus is the bus over godbus's system-bus connection.
type systemBus struct {
	conn    *dbus.Conn
	signals chan *dbus.Signal
}

// dialSystemBus connects to the system bus and subscribes to org.bluez's
// InterfacesAdded, InterfacesRemoved and PropertiesChanged under the
// adapter's path.
func dialSystemBus(adapterPath dbus.ObjectPath) (*systemBus, error) {
	conn, err := dbus.ConnectSystemBus()
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
	b := &systemBus{conn: conn, signals: make(chan *dbus.Signal, 256)}
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

func (b *systemBus) Close() error { return b.conn.Close() }

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

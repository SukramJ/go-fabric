// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

//go:build linux

package wpasupplicant

import (
	"context"
	"fmt"

	"github.com/godbus/dbus/v5"
)

// systemBus is the bus over godbus's system-bus connection.
type systemBus struct {
	conn    *dbus.Conn
	signals chan *dbus.Signal
}

// dialSystemBus connects to the system bus. It subscribes to nothing yet:
// the driver's Watch names the one interface object it follows.
func dialSystemBus() (*systemBus, error) {
	conn, err := dbus.ConnectSystemBus()
	if err != nil {
		return nil, fmt.Errorf("wpasupplicant: connect to the system bus: %w", err)
	}
	b := &systemBus{conn: conn, signals: make(chan *dbus.Signal, 256)}
	conn.Signal(b.signals)
	return b, nil
}

// Watch adds watchRules' match rules — what godbus's AddMatchSignal sends,
// the rule written out.
func (b *systemBus) Watch(ctx context.Context, iface dbus.ObjectPath) error {
	for _, rule := range watchRules(iface) {
		if err := b.conn.BusObject().CallWithContext(ctx, "org.freedesktop.DBus.AddMatch", 0, rule).Store(); err != nil {
			return fmt.Errorf("wpasupplicant: org.freedesktop.DBus.AddMatch: %w", err)
		}
	}
	return nil
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

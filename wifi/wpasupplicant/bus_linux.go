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

// dialSystemBus connects to the system bus and subscribes to
// wpa_supplicant's ScanDone and PropertiesChanged signals under its root
// object.
func dialSystemBus() (*systemBus, error) {
	conn, err := dbus.ConnectSystemBus()
	if err != nil {
		return nil, fmt.Errorf("wpasupplicant: connect to the system bus: %w", err)
	}
	matches := [][]dbus.MatchOption{
		{dbus.WithMatchSender(service), dbus.WithMatchInterface(ifaceInterface), dbus.WithMatchMember("ScanDone"), dbus.WithMatchPathNamespace(rootPath)},
		{dbus.WithMatchSender(service), dbus.WithMatchInterface(ifaceProperties), dbus.WithMatchMember("PropertiesChanged"), dbus.WithMatchPathNamespace(rootPath)},
	}
	for _, m := range matches {
		if err := conn.AddMatchSignal(m...); err != nil {
			_ = conn.Close()
			return nil, fmt.Errorf("wpasupplicant: org.freedesktop.DBus.AddMatch: %w", err)
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

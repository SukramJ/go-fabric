// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

//go:build linux

package bluez

import (
	"fmt"
	"net"
	"sync"
	"testing"

	"github.com/godbus/dbus/v5"
)

// TestSystemBusExportsAreSerialised: exports and withdrawals from several
// goroutines at once — Serve's application and Advertise's advertisement
// in the bridge — do not meet in godbus's object map, which its export
// reads without a lock. Without the serialisation the runtime ends the
// test binary with "concurrent map read and map write".
func TestSystemBusExportsAreSerialised(t *testing.T) {
	t.Parallel()
	local, remote := net.Pipe()
	t.Cleanup(func() { _ = remote.Close() })
	// A connection that never authenticates: Export touches only the
	// connection's handler.
	conn, err := dbus.NewConn(local)
	if err != nil {
		t.Fatal(err)
	}
	b := &systemBus{conn: conn, sent: newSentFDs()}
	t.Cleanup(func() { _ = b.Close() })
	methods := map[string]any{"Release": func() *dbus.Error { return nil }}
	var wg sync.WaitGroup
	for g := range 8 {
		wg.Go(func() {
			for i := range 500 {
				path := dbus.ObjectPath(fmt.Sprintf("/test/g%d/o%d", g, i))
				if err := b.Export(path, ifaceAdvertise, methods); err != nil {
					t.Errorf("export %s: %v", path, err)
					return
				}
				if err := b.Export(path, ifaceAdvertise, nil); err != nil {
					t.Errorf("withdraw %s: %v", path, err)
					return
				}
			}
		})
	}
	wg.Wait()
}

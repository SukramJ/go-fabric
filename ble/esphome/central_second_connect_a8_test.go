// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package esphome_test

import (
	"context"
	"strings"
	"sync"
	"testing"

	"thingfabric.dev/go-fabric/ble/esphome"
	"thingfabric.dev/go-fabric/ble/esphome/esphometest"
	"thingfabric.dev/go-fabric/controller/ble"
)

// TestSecondConnectForOneAddressIsRefused: a Connect for an address the
// proxy holds a connection for — open, or still connecting — is refused:
// the first connection stays up and no second CONNECT reaches the proxy,
// as the BlueZ central refuses it (review II, R4-conc-4352, decision 659).
func TestSecondConnectForOneAddressIsRefused(t *testing.T) {
	const addr = "C0:FF:EE:00:43:52"
	connects := func(events []string) int {
		n := 0
		for _, e := range events {
			if strings.HasPrefix(e, "connect "+addr) {
				n++
			}
		}
		return n
	}
	stillUp := func(t *testing.T, conn ble.Connection) {
		t.Helper()
		select {
		case <-conn.Disconnected():
			t.Fatal("the second Connect ended the first connection")
		default:
		}
		if _, err := conn.ReadC3(context.Background()); err != nil {
			t.Fatalf("the first connection after the second Connect: ReadC3 %v", err)
		}
	}

	t.Run("sequential", func(t *testing.T) {
		p := peripheral(addr)
		f := fake(t, esphometest.Options{C3: []byte{0x43}}, p)
		c := central(t, esphome.Options{}, esphome.Proxy{Addr: f.Addr(), Name: "lab"})
		scanFor(t, c, addr)
		first, err := c.Connect(context.Background(), ble.Peripheral{Address: addr})
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = first.Disconnect() }()
		if second, err := c.Connect(context.Background(), ble.Peripheral{Address: addr}); err == nil {
			_ = second.Disconnect()
			t.Fatal("the second Connect for one address succeeded")
		}
		stillUp(t, first)
		if n := connects(f.Events()); n != 1 {
			t.Fatalf("%d CONNECTs reached the proxy, want 1: %q", n, f.Events())
		}
	})

	t.Run("concurrent", func(t *testing.T) {
		p := peripheral(addr)
		f := fake(t, esphometest.Options{C3: []byte{0x43}}, p)
		c := central(t, esphome.Options{}, esphome.Proxy{Addr: f.Addr(), Name: "lab"})
		scanFor(t, c, addr)
		var (
			wg    sync.WaitGroup
			conns [2]ble.Connection
			errs  [2]error
		)
		for i := range conns {
			wg.Go(func() { conns[i], errs[i] = c.Connect(context.Background(), ble.Peripheral{Address: addr}) })
		}
		wg.Wait()
		var up ble.Connection
		for i := range conns {
			if errs[i] == nil {
				if up != nil {
					t.Fatal("both concurrent Connects for one address succeeded")
				}
				up = conns[i]
			}
		}
		if up == nil {
			t.Fatalf("neither concurrent Connect succeeded: %v", errs)
		}
		defer func() { _ = up.Disconnect() }()
		stillUp(t, up)
		if n := connects(f.Events()); n != 1 {
			t.Fatalf("%d CONNECTs reached the proxy, want 1: %q", n, f.Events())
		}
	})
}

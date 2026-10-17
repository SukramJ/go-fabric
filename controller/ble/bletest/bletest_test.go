// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bletest_test

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/controller/ble"
	"github.com/SukramJ/go-fabric/controller/ble/bletest"
	"github.com/SukramJ/go-fabric/transport/btp"
)

func TestCentralScansAndPeripheralServes(t *testing.T) {
	got := make(chan []byte, 1)
	closed := make(chan struct{})
	p := &bletest.Peripheral{
		Address: "aa", Discriminator: 1737, VendorID: 4476, ProductID: 32769,
		Serve: func(conn *btp.Conn) (func([]byte), func()) {
			return func(m []byte) { got <- m; _ = conn.Send(m) }, func() { close(closed) }
		},
	}
	c := bletest.NewCentral()
	var ads [][]byte
	c.SetDiscoveryCallback(func(_ ble.Peripheral, d []byte) { ads = append(ads, d) })
	if err := c.StartScanning(context.Background()); err != nil {
		t.Fatal(err)
	}
	c.Add(p) // advertises at once while scanning
	if len(ads) != 1 || !bytes.Equal(ads[0], []byte{0x00, 0xc9, 0x06, 0x7c, 0x11, 0x01, 0x80, 0x00}) {
		t.Fatalf("advertisements %x", ads)
	}
	if err := c.StopScanning(context.Background()); err != nil {
		t.Fatal(err)
	}
	if scanning, starts, stops := c.Scanning(); scanning || starts != 1 || stops != 1 {
		t.Fatalf("scanning %v %d %d", scanning, starts, stops)
	}
	ch, err := ble.Dial(context.Background(), c, ble.Peripheral{Address: "aa"}, ble.DialOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := ch.Conn().Send([]byte{1, 2, 3}); err != nil {
		t.Fatal(err)
	}
	select {
	case m := <-got:
		if !bytes.Equal(m, []byte{1, 2, 3}) {
			t.Fatalf("got %x", m)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no message")
	}
	_ = ch.Close()
	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		t.Fatal("the peripheral's session did not close")
	}
}

func TestConnectionOperations(t *testing.T) {
	c := bletest.NewCentral(&bletest.Peripheral{Address: "bb", ATTMTU: 50})
	conn, err := c.Connect(context.Background(), ble.Peripheral{Address: "bb"})
	if err != nil {
		t.Fatal(err)
	}
	if conn.ATTMTU() != 50 {
		t.Fatalf("ATT_MTU %d", conn.ATTMTU())
	}
	if _, err := conn.ReadC3(context.Background()); err == nil {
		t.Fatal("C3 data")
	}
	if err := conn.Disconnect(); err != nil {
		t.Fatal(err)
	}
	<-conn.Disconnected()
	if err := conn.WriteC1(context.Background(), []byte{1}); err == nil {
		t.Fatal("write after disconnect")
	}
	if err := conn.SubscribeC2(context.Background(), func([]byte) {}); err == nil {
		t.Fatal("subscribe after disconnect")
	}
	if err := conn.UnsubscribeC2(context.Background()); err != nil {
		t.Fatal(err)
	}
	// A peripheral that refuses the BTP version drops the connection.
	c.Advertise("unknown")
}

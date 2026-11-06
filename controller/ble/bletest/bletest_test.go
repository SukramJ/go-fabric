// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bletest_test

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"testing"
	"testing/synctest"
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
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	ch, err := ble.Dial(ctx, c, ble.Peripheral{Address: "aa"}, ble.DialOptions{})
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
	case <-ctx.Done():
		t.Fatal("no message")
	}
	_ = ch.Close()
	select {
	case <-closed:
	case <-ctx.Done():
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

// TestPeripheralStallAndFaults drives a stalled peripheral and its GATT
// faults through the connection: the handshake completed at the offered
// size and the data withheld, the unsubscribe that lets a new handshake
// start, each fault, the pending ATT_MTU exchange and the dropped
// connection.
func TestPeripheralStallAndFaults(t *testing.T) {
	request := func(segment int) []byte {
		return btp.EncodeHandshakeRequest(btp.HandshakeRequest{Versions: btp.SupportedVersions, ATTMTU: uint16(segment), ClientWindowSize: 4}) //nolint:gosec // a segment size
	}
	t.Run("stalls a session above the served size and serves one at it", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			var withheld [][]byte
			got := make(chan []byte, 1)
			p := &bletest.Peripheral{
				Address: "cc",
				Stall: func(offered int) func([]byte) {
					if offered <= btp.MinimumATTMTU {
						return nil
					}
					return func(b []byte) { withheld = append(withheld, b) }
				},
				Serve: func(*btp.Conn) (func([]byte), func()) { return func(m []byte) { got <- m }, nil },
			}
			defer p.DropConnections()
			conn, err := bletest.NewCentral(p).Connect(t.Context(), ble.Peripheral{Address: "cc"})
			if err != nil {
				t.Fatal(err)
			}
			c2 := make(chan []byte, 8)
			if err := conn.WriteC1(t.Context(), request(btp.MaximumBTPMTU)); err != nil {
				t.Fatal(err)
			}
			if err := conn.SubscribeC2(t.Context(), func(b []byte) { c2 <- b }); err != nil {
				t.Fatal(err)
			}
			synctest.Wait()
			resp, err := btp.DecodeHandshakeResponse(<-c2)
			if err != nil || resp.ATTMTU != btp.MaximumBTPMTU || resp.Version != 4 || resp.WindowSize != 4 {
				t.Fatalf("handshake response %+v, %v", resp, err)
			}
			if err := conn.WriteC1(t.Context(), []byte{0x05, 0x00, 0x01, 0x00, 0x42}); err != nil {
				t.Fatal(err)
			}
			synctest.Wait()
			if len(withheld) != 1 || len(c2) != 0 {
				t.Fatalf("withheld %d packets, %d indications", len(withheld), len(c2))
			}
			if o := p.Observed(); !slices.Equal(o.HandshakeSegmentSizes, []int{btp.MaximumBTPMTU}) || o.Unsubscribes != 0 {
				t.Fatalf("observed %+v", o)
			}
			// The unsubscribe ends the stalled session and leaves the
			// connection; the next handshake at the minimum is served — by
			// a peripheral session that answers with its own size, as
			// matter.js's does for an offer at the minimum.
			if err := conn.UnsubscribeC2(t.Context()); err != nil {
				t.Fatal(err)
			}
			if err := conn.WriteC1(t.Context(), request(btp.MinimumATTMTU)); err != nil {
				t.Fatal(err)
			}
			if err := conn.SubscribeC2(t.Context(), func(b []byte) { c2 <- b }); err != nil {
				t.Fatal(err)
			}
			synctest.Wait()
			if resp, err := btp.DecodeHandshakeResponse(<-c2); err != nil || resp.ATTMTU != btp.MaximumBTPMTU {
				t.Fatalf("second handshake response %+v, %v", resp, err)
			}
			// One BTP packet: beginning and ending segment, ack 0, sequence 0, length 3, payload 1 2 3.
			if err := conn.WriteC1(t.Context(), []byte{0x0d, 0x00, 0x00, 0x03, 0x00, 1, 2, 3}); err != nil {
				t.Fatal(err)
			}
			synctest.Wait()
			if m := <-got; !bytes.Equal(m, []byte{1, 2, 3}) {
				t.Fatalf("served message %x", m)
			}
			if o := p.Observed(); !slices.Equal(o.HandshakeSegmentSizes, []int{btp.MaximumBTPMTU, btp.MinimumATTMTU}) || o.Unsubscribes != 1 {
				t.Fatalf("observed %+v", o)
			}
			p.DropConnections()
			<-conn.Disconnected()
		})
	})
	t.Run("faults", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			p := &bletest.Peripheral{Address: "dd", Faults: bletest.Faults{
				UnsubscribeC2: errors.New("unsubscribe failed"), WithholdHandshakeFrom: 1, FailDataWrite: 1, HangDataWriteFrom: 2,
			}}
			defer p.DropConnections()
			conn, err := bletest.NewCentral(p).Connect(t.Context(), ble.Peripheral{Address: "dd"})
			if err != nil {
				t.Fatal(err)
			}
			if err := conn.UnsubscribeC2(t.Context()); err == nil || err.Error() != "unsubscribe failed" {
				t.Fatalf("UnsubscribeC2 = %v", err)
			}
			c2 := make(chan []byte, 8)
			if err := conn.WriteC1(t.Context(), request(btp.MaximumBTPMTU)); err != nil {
				t.Fatal(err)
			}
			if err := conn.SubscribeC2(t.Context(), func(b []byte) { c2 <- b }); err != nil {
				t.Fatal(err)
			}
			synctest.Wait()
			if len(c2) != 0 {
				t.Fatal("the withheld handshake was answered")
			}
			if err := conn.WriteC1(t.Context(), []byte{0x05, 0x00, 0x01, 0x00, 0x42}); !errors.Is(err, btp.ErrDisconnected) {
				t.Fatalf("the failed data write = %v", err)
			}
			hung := make(chan error, 1)
			go func() { hung <- conn.WriteC1(t.Context(), []byte{0x05, 0x00, 0x02, 0x00, 0x42}) }()
			synctest.Wait()
			if len(hung) != 0 {
				t.Fatal("the hung data write returned")
			}
			p.DropConnections()
			if err := <-hung; !errors.Is(err, btp.ErrDisconnected) {
				t.Fatalf("the hung data write after the drop = %v", err)
			}
			if o := p.Observed(); o.Unsubscribes != 1 || !slices.Equal(o.HandshakeSegmentSizes, []int{btp.MaximumBTPMTU}) {
				t.Fatalf("observed %+v", o)
			}
		})
	})
	t.Run("hung handshake write ends with its context", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			p := &bletest.Peripheral{Address: "ee", Faults: bletest.Faults{HangHandshakeWriteFrom: 1}}
			defer p.DropConnections()
			conn, err := bletest.NewCentral(p).Connect(t.Context(), ble.Peripheral{Address: "ee"})
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			hung := make(chan error, 1)
			go func() { hung <- conn.WriteC1(ctx, request(btp.MaximumBTPMTU)) }()
			synctest.Wait()
			cancel()
			if err := <-hung; !errors.Is(err, context.Canceled) {
				t.Fatalf("the hung handshake write = %v", err)
			}
		})
	})
	t.Run("pending ATT_MTU exchange", func(t *testing.T) {
		ready := make(chan struct{})
		p := &bletest.Peripheral{Address: "ff", ATTMTU: 100, ATTMTUReady: ready}
		conn, err := bletest.NewCentral(p).Connect(context.Background(), ble.Peripheral{Address: "ff"})
		if err != nil {
			t.Fatal(err)
		}
		x, ok := conn.(ble.MTUExchange)
		if !ok || conn.ATTMTU() != 0 {
			t.Fatalf("exchange %v, ATT_MTU %d", ok, conn.ATTMTU())
		}
		close(ready)
		<-x.ATTMTUExchanged()
		if conn.ATTMTU() != 100 {
			t.Fatalf("ATT_MTU %d after the exchange", conn.ATTMTU())
		}
		// Known at connect: the exchange is complete.
		settled, err := bletest.NewCentral(&bletest.Peripheral{Address: "gg"}).Connect(context.Background(), ble.Peripheral{Address: "gg"})
		if err != nil {
			t.Fatal(err)
		}
		select {
		case <-settled.(ble.MTUExchange).ATTMTUExchanged():
		default:
			t.Fatal("an exchange complete at connect is still pending")
		}
	})
}

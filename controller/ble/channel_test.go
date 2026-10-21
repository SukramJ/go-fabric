// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package ble_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/client/exchange"
	"github.com/SukramJ/go-fabric/clock/clocktest"
	"github.com/SukramJ/go-fabric/controller/ble"
	"github.com/SukramJ/go-fabric/controller/ble/bletest"
	"github.com/SukramJ/go-fabric/transport/btp"
	"github.com/SukramJ/go-fabric/transport/message"
	"github.com/SukramJ/go-fabric/transport/mrp"
)

// echo is a peripheral that answers every unsecured Matter message with
// one on the same exchange, as a responder, carrying the payload back.
func echo(t *testing.T, got chan<- message.ProtocolHeader) *bletest.Peripheral {
	t.Helper()
	return &bletest.Peripheral{
		Address: "11:22:33:44:55:66", Discriminator: 3840, VendorID: 0xFFF1, ProductID: 0x8001, ATTMTU: 100,
		Serve: func(conn *btp.Conn) (func([]byte), func()) {
			var counter uint32 = 7
			return func(msg []byte) {
				hdr, hl, err := message.UnmarshalHeader(msg)
				if err != nil {
					t.Error(err)
					return
				}
				proto, pl, err := message.UnmarshalProtocolHeader(msg[hl:])
				if err != nil {
					t.Error(err)
					return
				}
				got <- proto
				counter++
				rh := message.Header{MessageCounter: counter, DestSize: message.DestNodeID, DestNodeID: hdr.SourceNodeID}
				rp := message.ProtocolHeader{NeedsAck: true, Opcode: mrp.SCOpcodePBKDFParamResponse, ExchangeID: proto.ExchangeID, ProtocolID: proto.ProtocolID}
				out := append(rh.Marshal(), rp.Marshal()...)
				_ = conn.Send(append(out, msg[hl+pl:]...))
			}, nil
		},
	}
}

func TestDialRunsExchangesWithoutMRP(t *testing.T) {
	got := make(chan message.ProtocolHeader, 4)
	p := echo(t, got)
	central := bletest.NewCentral(p)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ch, err := ble.Dial(ctx, central, ble.Peripheral{Address: p.Address}, ble.DialOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if ch.Peripheral().Address != p.Address || ch.Conn().Session().FragmentSize() != 97 || ch.PeerAddress() == nil {
		t.Fatalf("channel %v %d %v", ch.Peripheral(), ch.Conn().Session().FragmentSize(), ch.PeerAddress())
	}
	s, err := ch.Exchanges().NewUnsecuredSession(ch.PeerAddress(), nil)
	if err != nil {
		t.Fatal(err)
	}
	x, err := ch.Exchanges().Initiate(s, mrp.SecureChannelProtocolID, exchange.Options{})
	if err != nil {
		t.Fatal(err)
	}
	payload := make([]byte, 300) // several BTP segments
	payload[299] = 0x5a
	if err := x.Send(ctx, mrp.SCOpcodePBKDFParamRequest, payload, exchange.SendOptions{}); err != nil {
		t.Fatal(err)
	}
	if proto := <-got; proto.NeedsAck || proto.HasAck {
		t.Fatalf("request over BTP asks for MRP: %+v", proto)
	}
	msg, err := x.Next(ctx, exchange.ReceiveOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if msg.Opcode != mrp.SCOpcodePBKDFParamResponse || len(msg.Payload) != 300 || msg.Payload[299] != 0x5a || msg.RequiresAck {
		t.Fatalf("response %+v", msg)
	}
	if err := ch.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ch.Closed():
	case <-time.After(5 * time.Second):
		t.Fatal("not closed")
	}
	if !errors.Is(ch.ClosedCause(), btp.ErrDisconnected) {
		t.Fatal("the channel's close cause is no disconnect")
	}
}

// silent is a connection whose peripheral never answers the handshake.
type silent struct {
	gone chan struct{}
}

func (s *silent) ATTMTU() int                                     { return 0 }
func (s *silent) WriteC1(context.Context, []byte) error           { return nil }
func (s *silent) SubscribeC2(context.Context, func([]byte)) error { return nil }
func (s *silent) UnsubscribeC2(context.Context) error             { return nil }
func (s *silent) ReadC3(context.Context) ([]byte, error)          { return nil, errors.New("none") }
func (s *silent) Disconnect() error                               { return nil }
func (s *silent) Disconnected() <-chan struct{}                   { return s.gone }

type silentCentral struct{ conn *silent }

func (c *silentCentral) SetDiscoveryCallback(func(ble.Peripheral, []byte)) {}
func (c *silentCentral) StartScanning(context.Context) error               { return nil }
func (c *silentCentral) StopScanning(context.Context) error                { return nil }
func (c *silentCentral) Connect(context.Context, ble.Peripheral) (ble.Connection, error) {
	return c.conn, nil
}

func TestDialHandshakeTimeoutAndDisconnect(t *testing.T) {
	clock := clocktest.New(time.Unix(0, 0))
	c := &silentCentral{conn: &silent{gone: make(chan struct{})}}
	errc := make(chan error, 1)
	go func() {
		_, err := ble.Dial(context.Background(), c, ble.Peripheral{Address: "x"}, ble.DialOptions{Clock: clock})
		errc <- err
	}()
	// The handshake arms its response timeout; nothing answers, so it
	// expires once the clock passes btp.ConnRspTimeout.
	deadline := time.Now().Add(5 * time.Second)
	for !slices.Contains(clock.Armed(), btp.ConnRspTimeout) {
		if time.Now().After(deadline) {
			t.Fatalf("no %v handshake timeout armed (armed %v)", btp.ConnRspTimeout, clock.Armed())
		}
		time.Sleep(time.Millisecond)
	}
	clock.Advance(btp.ConnRspTimeout)
	if err := <-errc; !errors.Is(err, ble.ErrHandshakeTimeout) {
		t.Fatalf("Dial = %v", err)
	}
	// A connection that drops during the handshake.
	c2 := &silentCentral{conn: &silent{gone: make(chan struct{})}}
	close(c2.conn.gone)
	if _, err := ble.Dial(context.Background(), c2, ble.Peripheral{Address: "y"}, ble.DialOptions{}); !errors.Is(err, btp.ErrDisconnected) {
		t.Fatalf("Dial on a dropped connection = %v", err)
	}
	// An unknown peripheral.
	if _, err := ble.Dial(context.Background(), bletest.NewCentral(), ble.Peripheral{Address: "z"}, ble.DialOptions{}); !errors.Is(err, bletest.ErrUnknownPeripheral) {
		t.Fatalf("Dial to an unknown peripheral = %v", err)
	}
}

func TestChannelClosesWhenPeripheralDrops(t *testing.T) {
	got := make(chan message.ProtocolHeader, 4)
	p := echo(t, got)
	central := bletest.NewCentral(p)
	ch, err := ble.Dial(context.Background(), central, ble.Peripheral{Address: p.Address}, ble.DialOptions{})
	if err != nil {
		t.Fatal(err)
	}
	// The peripheral's side ends the session: the central sees C2 go.
	_ = ch.Conn().Session().Close()
	select {
	case <-ch.Closed():
	case <-time.After(5 * time.Second):
		t.Fatal("the channel did not close")
	}
}

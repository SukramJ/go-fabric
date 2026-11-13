// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package ble_test

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"sync"
	"testing"
	"testing/synctest"
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
	synctest.Test(t, func(t *testing.T) {
		clock := clocktest.New(time.Unix(0, 0))
		c := &silentCentral{conn: &silent{gone: make(chan struct{})}}
		errc := make(chan error, 1)
		go func() {
			_, err := ble.Dial(context.Background(), c, ble.Peripheral{Address: "x"}, ble.DialOptions{Clock: clock})
			errc <- err
		}()
		// The handshake arms its response timeout; nothing answers, so it
		// expires once the clock passes btp.ConnRspTimeout.
		synctest.Wait() // the handshake has armed what it waits on
		if !slices.Contains(clock.Armed(), btp.ConnRspTimeout) {
			t.Fatalf("no %v handshake timeout armed (armed %v)", btp.ConnRspTimeout, clock.Armed())
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
	})
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

// TestRenegotiationBoundsItsGATTCalls covers the bounds matter.js puts on
// the renegotiation's GATT calls (packages/nodejs-ble/src/NobleBleChannel.ts:579,
// :602-606 at 862ad75f): a peripheral that vanished without a disconnect
// event leaves the unsubscribe or the handshake write pending, and the
// renegotiation fails within the bound, closing the channel.
func TestRenegotiationBoundsItsGATTCalls(t *testing.T) {
	payload := []byte{0x00, 0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88, 0x99}
	small := []byte{0xaa, 0xbb}
	cases := []struct {
		name  string
		fault func(*bletest.Faults)
		bound time.Duration
	}{
		{"an unsubscribe that never completes", func(f *bletest.Faults) { f.HangUnsubscribeC2From = 1 }, ble.DisconnectTimeout},
		{"a handshake write that never completes", func(f *bletest.Faults) { f.HangHandshakeWriteFrom = 2 }, btp.ConnRspTimeout},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				clock := clocktest.New(time.Unix(0, 0))
				peer := newStallPeer(btp.MinimumATTMTU)
				tc.fault(&peer.p.Faults)
				ch := peer.dial(t, clock)
				stall(t, clock, ch, peer, payload)
				parked := parkSend(ch, small)
				synctest.Wait()
				if isClosed(ch.Closed()) {
					t.Fatal("the channel closed before the bound")
				}
				if !slices.Contains(clock.Armed(), tc.bound) {
					t.Fatalf("no %v bound armed (armed %v)", tc.bound, clock.Armed())
				}
				clock.Advance(tc.bound)
				synctest.Wait()
				if !isClosed(ch.Closed()) {
					t.Fatal("the channel stayed open after the bound")
				}
				if err := <-parked; !errors.Is(err, btp.ErrDisconnected) {
					t.Fatalf("the parked send = %v", err)
				}
			})
		})
	}
}

// recorder is a log handler that keeps each record's message.
type recorder struct {
	mu   sync.Mutex
	msgs []string
}

func (r *recorder) Enabled(context.Context, slog.Level) bool { return true }

func (r *recorder) Handle(_ context.Context, rec slog.Record) error {
	r.mu.Lock()
	r.msgs = append(r.msgs, rec.Message)
	r.mu.Unlock()
	return nil
}

func (r *recorder) WithAttrs([]slog.Attr) slog.Handler { return r }
func (r *recorder) WithGroup(string) slog.Handler      { return r }

func (r *recorder) seen() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.msgs)
}

// answering is a connection whose peripheral answers the handshake; it
// keeps the C2 handler, as a GATT stack can still call it after the
// unsubscribe.
type answering struct {
	silent
	mu sync.Mutex
	on func([]byte)
}

func (a *answering) SubscribeC2(_ context.Context, on func([]byte)) error {
	a.mu.Lock()
	a.on = on
	a.mu.Unlock()
	resp := btp.EncodeHandshakeResponse(btp.HandshakeResponse{Version: 4, ATTMTU: btp.MinimumATTMTU, WindowSize: 4})
	go on(resp)
	return nil
}

func (a *answering) indicate(data []byte) {
	a.mu.Lock()
	on := a.on
	a.mu.Unlock()
	on(data)
}

// answeringConnCentral connects every peripheral over conn.
type answeringConnCentral struct {
	silentCentral
	conn *answering
}

func (c *answeringConnCentral) Connect(context.Context, ble.Peripheral) (ble.Connection, error) {
	return c.conn, nil
}

// TestCloseDetachesTheC2Handler: data on C2 after Close reaches no
// session — matter.js's close detaches the handler
// (packages/nodejs-ble/src/NobleBleChannel.ts:1010 at 862ad75f).
func TestCloseDetachesTheC2Handler(t *testing.T) {
	conn := &answering{silent: silent{gone: make(chan struct{})}}
	rec := &recorder{}
	ch, err := ble.Dial(t.Context(), &answeringConnCentral{conn: conn}, ble.Peripheral{Address: "x"},
		ble.DialOptions{Clock: clocktest.New(time.Unix(0, 0)), Logger: slog.New(rec)})
	if err != nil {
		t.Fatal(err)
	}
	if err := ch.Close(); err != nil {
		t.Fatal(err)
	}
	conn.indicate([]byte{0x05, 0x00, 0x00, 0x00})
	msgs := rec.seen()
	if !slices.Contains(msgs, "matter.ble.c2_after_detach") || slices.Contains(msgs, "matter.btp.rx_inactive") {
		t.Fatalf("C2 data after Close reached the session: %v", msgs)
	}
}

// TestSessionCloseEndsTheChannelBeforeTheGATTRelease: a BTP session that
// closes on its own (its ack or idle timer, a peripheral gone silent)
// ends the channel at once, while the C2 unsubscribe still waits on the
// dead link — matter.js's disconnect callback starts the unsubscribe and
// the disconnect without awaiting them
// (packages/nodejs-ble/src/NobleBleChannel.ts:788-805 at db021da0), so
// the channel reports its loss and a send in that window reads as
// channel loss, not as a bare BTP flow error (review II, R2-P1-sec-05).
func TestSessionCloseEndsTheChannelBeforeTheGATTRelease(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		clock := clocktest.New(time.Unix(0, 0))
		peer := newStallPeer(btp.MinimumATTMTU)
		peer.p.Faults.HangUnsubscribeC2From = 1
		ch := peer.dial(t, clock)
		closed := make(chan error, 1)
		go func() { closed <- ch.Conn().Session().Close() }()
		synctest.Wait() // the session's close is in the unsubscribe
		if !slices.Contains(clock.Armed(), ble.DisconnectTimeout) {
			t.Fatalf("no %v unsubscribe bound armed (armed %v)", ble.DisconnectTimeout, clock.Armed())
		}
		if !isClosed(ch.Closed()) {
			t.Fatal("the channel is still open while the GATT release waits")
		}
		if err := ch.Send(nil, []byte{0x01}); !errors.Is(err, ble.ErrChannelClosed) {
			t.Fatalf("Send during the GATT release = %v, want ErrChannelClosed", err)
		}
		clock.Advance(ble.DisconnectTimeout)
		if err := <-closed; err != nil {
			t.Fatalf("session Close = %v", err)
		}
	})
}

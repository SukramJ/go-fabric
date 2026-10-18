// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package btp

import (
	"bytes"
	"errors"
	"sync"
	"testing"
	"time"
)

// gattLink is an asynchronous in-memory GATT pair: every write is queued
// and delivered to the other side's HandleIncoming on that side's own
// goroutine, in order, as a BLE stack delivers writes and indications.
type gattLink struct {
	toPeripheral, toCentral chan []byte
	done                    chan struct{}
}

func newGATTLink() *gattLink {
	return &gattLink{toPeripheral: make(chan []byte, 1024), toCentral: make(chan []byte, 1024), done: make(chan struct{})}
}

func (l *gattLink) pump(ch chan []byte, s func() *Session) {
	for {
		select {
		case b := <-ch:
			_ = s().HandleIncoming(b)
		case <-l.done:
			return
		}
	}
}

func TestConnOverAsyncGATTPair(t *testing.T) {
	link := newGATTLink()
	defer close(link.done)
	var mu sync.Mutex
	var atPeripheral, atCentral [][]byte
	got := make(chan struct{}, 256)

	var resp []byte
	ready := false
	var peripheral, central *Session
	var err error
	peripheral, err = NewPeripheral(MaximumBTPMTU, EncodeHandshakeRequest(HandshakeRequest{Versions: SupportedVersions, ATTMTU: 100, ClientWindowSize: MaximumWindowSize}), Callbacks{
		Write: func(b []byte) error {
			if !ready {
				resp = b
				return nil
			}
			link.toCentral <- append([]byte(nil), b...)
			return nil
		},
		Disconnect: func() error { return nil },
		Message: func(m []byte) error {
			mu.Lock()
			atPeripheral = append(atPeripheral, m)
			mu.Unlock()
			got <- struct{}{}
			// Echo every message back, reversed.
			r := bytes.Clone(m)
			for i, j := 0, len(r)-1; i < j; i, j = i+1, j-1 {
				r[i], r[j] = r[j], r[i]
			}
			return peripheral.Send(r)
		},
	}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	ready = true
	central, err = NewCentral(resp, Callbacks{
		Write:      func(b []byte) error { link.toPeripheral <- append([]byte(nil), b...); return nil },
		Disconnect: func() error { return nil },
		Message: func(m []byte) error {
			mu.Lock()
			atCentral = append(atCentral, m)
			mu.Unlock()
			got <- struct{}{}
			return nil
		},
	}, 100, Options{})
	if err != nil {
		t.Fatal(err)
	}
	go link.pump(link.toPeripheral, func() *Session { return peripheral })
	go link.pump(link.toCentral, func() *Session { return central })

	conn := NewConn(central, Addr{Peripheral: "aa:bb:cc:dd:ee:ff"})
	if !conn.Reliable() || conn.Remote().Network() != "ble" || conn.Remote().String() != "aa:bb:cc:dd:ee:ff" || conn.String() != "ble://aa:bb:cc:dd:ee:ff" {
		t.Fatalf("conn %v %v %v", conn.Reliable(), conn.Remote(), conn)
	}
	if conn.MaxMessageSize() != MaxMatterPayloadSize || conn.Session() != central || central.FragmentSize() != 100 || central.WindowSize() != MaximumWindowSize || central.Role() != RoleCentral {
		t.Fatalf("sizes %d %d %d", conn.MaxMessageSize(), central.FragmentSize(), central.WindowSize())
	}
	var fe *FlowError
	if err := conn.Send(make([]byte, MaxMatterPayloadSize+1)); !errors.As(err, &fe) {
		t.Fatalf("oversized send %v", err)
	}
	const n = 20
	sent := make([][]byte, 0, n)
	for i := range n {
		m := bytes.Repeat([]byte{byte(i)}, 50+i*50)
		sent = append(sent, m)
		if err := conn.Send(m); err != nil {
			t.Fatal(err)
		}
	}
	deadline := time.After(10 * time.Second)
	for range 2 * n {
		select {
		case <-got:
		case <-deadline:
			t.Fatal("messages did not arrive")
		}
	}
	mu.Lock()
	defer mu.Unlock()
	for i := range n {
		if !bytes.Equal(atPeripheral[i], sent[i]) {
			t.Fatalf("message %d differs at the peripheral", i)
		}
		if len(atCentral[i]) != len(sent[i]) {
			t.Fatalf("echo %d is %d bytes", i, len(atCentral[i]))
		}
	}
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-conn.Closed():
	default:
		t.Fatal("not closed")
	}
	if RoleCentral.String() != "central" || RolePeripheral.String() != "peripheral" {
		t.Fatal("role names")
	}
}

func TestSessionIdleTimeoutClosesCentral(t *testing.T) {
	clock := &fakeClock{}
	disconnected := false
	c, err := NewCentral(EncodeHandshakeResponse(HandshakeResponse{Version: 4, ATTMTU: 244, WindowSize: 8}), Callbacks{
		Write:      func([]byte) error { return nil },
		Disconnect: func() error { disconnected = true; return nil },
		Message:    func([]byte) error { return nil },
	}, MaximumBTPMTU, Options{Clock: clock})
	if err != nil {
		t.Fatal(err)
	}
	// Acknowledged data keeps the ack timer quiet; the idle timer remains.
	_ = c.Send([]byte{1})
	_ = c.HandleIncoming(mustEncode(t, Packet{Header: Header{HasAckNumber: true}, Payload: Payload{AckNumber: u8(0), SequenceNumber: 1}}))
	clock.Advance(ConnIdleTimeout)
	if !disconnected {
		t.Fatal("idle central not closed")
	}
}

func TestSessionSuspendAndUnsupportedVersion(t *testing.T) {
	c, err := NewCentral(EncodeHandshakeResponse(HandshakeResponse{Version: 4, ATTMTU: 244, WindowSize: 8}), Callbacks{
		Write: func([]byte) error { return nil }, Disconnect: func() error { t.Error("disconnect on suspend"); return nil }, Message: func([]byte) error { return nil },
	}, MaximumBTPMTU, Options{Clock: &fakeClock{}})
	if err != nil {
		t.Fatal(err)
	}
	c.Suspend()
	if isClosed(c) {
		t.Fatal("suspend closed the session")
	}
	if err := c.HandleIncoming([]byte{0x08, 0x00, 0x01}); err != nil {
		t.Fatal(err)
	}
	_, err = NewCentral(EncodeHandshakeResponse(HandshakeResponse{Version: 5, ATTMTU: 244, WindowSize: 8}), Callbacks{}, MaximumBTPMTU, Options{})
	wantProtocolError(t, err, "Unsupported BTP version 5")
	if _, err := NewCentral([]byte{0x65}, Callbacks{}, MaximumBTPMTU, Options{}); !errors.Is(err, ErrTruncated) {
		t.Fatalf("short response %v", err)
	}
	if _, err := NewPeripheral(20, []byte{0x65}, Callbacks{}, Options{}); !errors.Is(err, ErrTruncated) {
		t.Fatalf("short request %v", err)
	}
	if _, err := NewPeripheral(20, unhex(t, handshake185w6), Callbacks{Write: func([]byte) error { return errors.New("gatt") }}, Options{}); err == nil {
		t.Fatal("failed handshake response write accepted")
	}
}

func TestSessionNonProtocolErrorReturned(t *testing.T) {
	s, err := NewPeripheral(20, unhex(t, handshake185w6), Callbacks{
		Write: func([]byte) error { return nil }, Disconnect: func() error { return nil },
		Message: func([]byte) error { return errors.New("handler") },
	}, Options{Clock: &fakeClock{}})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.HandleIncoming(singleSegment(t, nil, 0, []byte{1})); err == nil || err.Error() != "handler" {
		t.Fatalf("got %v", err)
	}
	// A truncated packet is no protocol error either (matter.js's RangeError).
	s2, _ := NewPeripheral(20, unhex(t, handshake185w6), Callbacks{Write: func([]byte) error { return nil }, Disconnect: func() error { return nil }}, Options{Clock: &fakeClock{}})
	if err := s2.HandleIncoming([]byte{0x05}); !errors.Is(err, ErrTruncated) {
		t.Fatalf("truncated %v", err)
	}
	if err := s2.Send([]byte{1}); err == nil {
		t.Fatal("send on a closed session")
	}
}

// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package btp

import (
	"testing"
	"time"
)

// A handshake that states a receive window of 0 is refused on both roles,
// as connectedhomeip does (src/ble/BLEEndPoint.cpp:1014 for the request,
// :1081 for the response, at 6170af84). matter.js takes the value as it
// comes (packages/protocol/src/ble/BtpSessionHandler.ts:123, :180-191 at
// db021da0), so these cases have no matter.js test to port.

// TestBtpSessionRefusesZeroWindowRequest: the peripheral answers no
// request whose client window is 0 and drops the connection.
func TestBtpSessionRefusesZeroWindowRequest(t *testing.T) {
	disconnected := false
	req := EncodeHandshakeRequest(HandshakeRequest{Versions: []int{4}, ATTMTU: 244, ClientWindowSize: 0})
	_, err := NewPeripheral(100, req, Callbacks{
		Write:      func([]byte) error { t.Error("a handshake response was written"); return nil },
		Disconnect: func() error { disconnected = true; return nil },
		Message:    noMessage(t),
	}, Options{TimeSource: newFakeClock()})
	wantProtocolError(t, err, "BTP handshake request states a window size of 0")
	if !disconnected {
		t.Fatal("not disconnected")
	}
}

// TestBtpSessionRefusesZeroWindowResponse: the central takes no session
// from a response whose window is 0.
func TestBtpSessionRefusesZeroWindowResponse(t *testing.T) {
	resp := EncodeHandshakeResponse(HandshakeResponse{Version: 4, ATTMTU: 244, WindowSize: 0})
	s, err := NewCentral(resp, Callbacks{
		Write:      func([]byte) error { t.Error("written on a refused session"); return nil },
		Disconnect: noDisconnect(t),
		Message:    noMessage(t),
	}, MaximumBTPMTU, Options{TimeSource: newFakeClock()})
	wantProtocolError(t, err, "BTP handshake response states a window size of 0")
	if s != nil {
		t.Fatal("a session was returned with the error")
	}
}

// TestBtpSessionAcceptsSmallestWindow: 1 is the smallest window either
// role accepts, and the peripheral answers it unchanged.
func TestBtpSessionAcceptsSmallestWindow(t *testing.T) {
	var got []byte
	req := EncodeHandshakeRequest(HandshakeRequest{Versions: []int{4}, ATTMTU: 244, ClientWindowSize: 1})
	p, err := NewPeripheral(100, req, Callbacks{
		Write:      func(b []byte) error { got = b; return nil },
		Disconnect: func() error { return nil },
		Message:    noMessage(t),
	}, Options{TimeSource: newFakeClock()})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = p.Close() }()
	resp, err := DecodeHandshakeResponse(got)
	if err != nil {
		t.Fatal(err)
	}
	if resp.WindowSize != 1 || p.WindowSize() != 1 {
		t.Fatalf("peripheral window: response %d, session %d, want 1", resp.WindowSize, p.WindowSize())
	}
	c, err := NewCentral(got, Callbacks{
		Write:      func([]byte) error { return nil },
		Disconnect: func() error { return nil },
		Message:    noMessage(t),
	}, MaximumBTPMTU, Options{TimeSource: newFakeClock()})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()
	if c.WindowSize() != 1 {
		t.Fatalf("central window %d, want 1", c.WindowSize())
	}
}

// TestAcceptRefusesZeroWindow: the GATT server opens no session for a
// handshake request with window 0; the subscription that would start it
// drops the connection, and no handshake response is indicated.
func TestAcceptRefusesZeroWindow(t *testing.T) {
	clk := newFakeClock()
	f, o := acceptFake(t, clk)
	f.write(EncodeHandshakeRequest(HandshakeRequest{Versions: []int{4}, ATTMTU: 100, ClientWindowSize: 0}))
	f.subscribe(100)
	select {
	case <-f.Disconnected():
	case <-time.After(5 * time.Second):
		t.Fatal("the connection stayed after a handshake request with window 0")
	}
	if n := o.count(); n != 0 {
		t.Fatalf("%d sessions opened, want none", n)
	}
	if ind := f.indicated(); len(ind) != 0 {
		t.Fatalf("indicated %x, want nothing", ind)
	}
}

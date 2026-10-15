// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package udp

import (
	"bytes"
	"context"
	"errors"
	"net"
	"testing"
	"time"
)

func TestJoinGroupRejectsNonMulticastAndClosedListeners(t *testing.T) {
	l, err := New(Config{LocalAddr: "[::]:0"})
	if err != nil {
		t.Skipf("no IPv6 socket here: %v", err)
	}
	for _, ip := range []net.IP{net.ParseIP("192.0.2.1"), net.ParseIP("2001:db8::1"), net.ParseIP("224.0.0.251"), nil} {
		if err := l.JoinGroup(ip); err == nil {
			t.Errorf("JoinGroup(%v) succeeded", ip)
		}
	}
	_ = l.Close()
	if err := l.LeaveGroup(net.ParseIP("ff35:40:fd00::f:ab00:101")); !errors.Is(err, ErrListenerClosed) {
		t.Fatalf("LeaveGroup on a closed listener: %v", err)
	}
}

// TestJoinGroupWithoutInterfaces pins the failure the bridge retries on:
// when no interface accepts the membership, JoinGroup says so.
func TestJoinGroupWithoutInterfaces(t *testing.T) {
	l, err := New(Config{LocalAddr: "[::]:0"})
	if err != nil {
		t.Skipf("no IPv6 socket here: %v", err)
	}
	defer func() { _ = l.Close() }()
	saved := multicastInterfaces
	multicastInterfaces = func() []net.Interface { return nil }
	defer func() { multicastInterfaces = saved }()
	if err := l.JoinGroup(net.ParseIP("ff35:40:fd00::f:ab00:101")); !errors.Is(err, ErrNoMulticastInterface) {
		t.Fatalf("JoinGroup without interfaces: %v", err)
	}
	multicastInterfaces = func() []net.Interface { return []net.Interface{{Index: 999999, Name: "nope"}} }
	if err := l.JoinGroup(net.ParseIP("ff35:40:fd00::f:ab00:101")); !errors.Is(err, ErrNoMulticastInterface) {
		t.Fatalf("JoinGroup on a missing interface: %v", err)
	}
}

// TestJoinAndLeaveGroupOnLiveInterfaces joins a site-scoped Matter group
// address on whatever multicast interfaces the host has.
func TestJoinAndLeaveGroupOnLiveInterfaces(t *testing.T) {
	l, err := New(Config{LocalAddr: "[::]:0"})
	if err != nil {
		t.Skipf("no IPv6 socket here: %v", err)
	}
	defer func() { _ = l.Close() }()
	group := net.ParseIP("ff35:40:fd00::f:ab00:101")
	if err := l.JoinGroup(group); err != nil {
		t.Skipf("host has no interface that joins IPv6 multicast: %v", err)
	}
	if err := l.LeaveGroup(group); err != nil {
		t.Fatalf("LeaveGroup after a successful JoinGroup: %v", err)
	}
}

func TestSendMulticastRefusals(t *testing.T) {
	l, err := New(Config{LocalAddr: "[::]:0"})
	if err != nil {
		t.Skipf("no IPv6 socket here: %v", err)
	}
	group := net.ParseIP("ff35:40:fd00::f:ab00:101")
	for _, ip := range []net.IP{net.ParseIP("192.0.2.1"), net.ParseIP("2001:db8::1"), net.ParseIP("224.0.0.251"), nil} {
		if err := l.SendMulticast(ip, 5540, []byte{1}); err == nil {
			t.Errorf("SendMulticast(%v) succeeded", ip)
		}
	}
	if err := l.SendMulticast(group, 5540, make([]byte, MaxDatagramSize+1)); !errors.Is(err, ErrPayloadTooLarge) {
		t.Errorf("oversized: %v", err)
	}
	saved := multicastInterfaces
	defer func() { multicastInterfaces = saved }()
	multicastInterfaces = func() []net.Interface { return nil }
	if err := l.SendMulticast(group, 5540, []byte{1}); !errors.Is(err, ErrNoMulticastInterface) {
		t.Errorf("no interfaces: %v", err)
	}
	multicastInterfaces = func() []net.Interface { return []net.Interface{{Index: 999999, Name: "nope"}} }
	if err := l.SendMulticast(group, 5540, []byte{1}); !errors.Is(err, ErrNoMulticastInterface) {
		t.Errorf("a missing interface: %v", err)
	}
	_ = l.Close()
	if err := l.SendMulticast(group, 5540, []byte{1}); !errors.Is(err, ErrListenerClosed) {
		t.Errorf("closed: %v", err)
	}
}

// TestSendMulticastReachesAJoinedListener sends to a group a second socket
// joined, on the host's real interfaces (multicast loopback delivers it
// back to this host). Skipped on a host without an interface that joins
// IPv6 multicast or loops it back.
func TestSendMulticastReachesAJoinedListener(t *testing.T) {
	rx, err := New(Config{LocalAddr: "[::]:0"})
	if err != nil {
		t.Skipf("no IPv6 socket here: %v", err)
	}
	defer func() { _ = rx.Close() }()
	tx, err := New(Config{LocalAddr: "[::]:0"})
	if err != nil {
		t.Skipf("no IPv6 socket here: %v", err)
	}
	defer func() { _ = tx.Close() }()
	group := net.ParseIP("ff05::f00d:5ea1")
	if err := rx.JoinGroup(group); err != nil {
		t.Skipf("host has no interface that joins IPv6 multicast: %v", err)
	}
	got := make(chan []byte, 16)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		_ = rx.Serve(ctx, func(buf []byte, _ *net.UDPAddr) { got <- append([]byte(nil), buf...) })
	}()
	want := []byte("group datagram")
	if err := tx.SendMulticast(group, rx.LocalAddr().Port, want); err != nil {
		t.Skipf("host sends no IPv6 multicast: %v", err)
	}
	select {
	case b := <-got:
		if !bytes.Equal(b, want) {
			t.Fatalf("received % X", b)
		}
	case <-time.After(3 * time.Second):
		t.Skip("multicast loopback delivered nothing on this host")
	}
}

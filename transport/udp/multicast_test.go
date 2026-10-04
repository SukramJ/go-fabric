// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package udp

import (
	"errors"
	"net"
	"testing"
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

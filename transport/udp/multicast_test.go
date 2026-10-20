// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package udp

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"slices"
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

// memberConn is an in-memory socket with its own membership model, as
// fabrictest's network socket is: it records joins and leaves and fails
// the joins err names.
type memberConn struct {
	joined []string
	left   []string
	sent   []string
	err    error
	closed chan struct{}
}

func (c *memberConn) ReadFromUDP([]byte) (int, *net.UDPAddr, error) {
	<-c.closed
	return 0, nil, net.ErrClosed
}
func (*memberConn) WriteToUDP(b []byte, _ *net.UDPAddr) (int, error) { return len(b), nil }
func (*memberConn) LocalAddr() net.Addr                              { return &net.UDPAddr{IP: net.IPv6loopback, Port: MatterPort} }
func (c *memberConn) Close() error                                   { close(c.closed); return nil }

func (c *memberConn) JoinGroup(group net.IP) error {
	if c.err != nil {
		return c.err
	}
	c.joined = append(c.joined, group.String())
	return nil
}

func (c *memberConn) LeaveGroup(group net.IP) error {
	c.left = append(c.left, group.String())
	return nil
}

func (c *memberConn) SendMulticast(group net.IP, port int, payload []byte) error {
	if c.err != nil {
		return c.err
	}
	c.sent = append(c.sent, fmt.Sprintf("[%s]:%d %x", group, port, payload))
	return nil
}

// TestMembershipDelegatesToTheSocketModel pins the seam fabrictest's
// multicast model rests on: a socket that is no kernel socket but carries
// JoinGroup/LeaveGroup holds the membership itself — the address checks
// and the closed check still come first, and its error is the listener's.
func TestMembershipDelegatesToTheSocketModel(t *testing.T) {
	c := &memberConn{closed: make(chan struct{})}
	l := newListener(c)
	group := net.ParseIP("ff05::fa")
	if err := l.JoinGroup(group); err != nil {
		t.Fatalf("JoinGroup: %v", err)
	}
	if err := l.LeaveGroup(group); err != nil {
		t.Fatalf("LeaveGroup: %v", err)
	}
	if len(c.joined) != 1 || c.joined[0] != "ff05::fa" || len(c.left) != 1 || c.left[0] != "ff05::fa" {
		t.Fatalf("socket saw joins %v, leaves %v", c.joined, c.left)
	}
	if err := l.JoinGroup(net.ParseIP("2001:db8::1")); err == nil || len(c.joined) != 1 {
		t.Errorf("a unicast address reached the socket model: %v, joins %v", err, c.joined)
	}
	c.err = errors.New("simulated join failure")
	if err := l.JoinGroup(group); !errors.Is(err, c.err) {
		t.Errorf("JoinGroup error %v, want the socket model's", err)
	}
	_ = l.Close()
	if err := l.JoinGroup(group); !errors.Is(err, ErrListenerClosed) {
		t.Errorf("JoinGroup after Close: %v, want ErrListenerClosed", err)
	}
}

// TestSendMulticastDelegatesToTheSocketModel pins the send half of the
// seam: a socket that carries its own multicast model takes the datagram
// (fabrictest's network then delivers it to every host holding the
// group, matter.js MockUdpSocket.send) — after the listener's own checks
// of the address, the size and Close, and with its error returned as the
// listener's.
func TestSendMulticastDelegatesToTheSocketModel(t *testing.T) {
	c := &memberConn{closed: make(chan struct{})}
	l := newListener(c)
	group := net.ParseIP("ff35:40:fd00::f:ab00:101")
	if err := l.SendMulticast(group, MatterPort, []byte{1, 2}); err != nil {
		t.Fatalf("SendMulticast: %v", err)
	}
	if want := []string{"[ff35:40:fd00::f:ab00:101]:5540 0102"}; !slices.Equal(c.sent, want) {
		t.Fatalf("socket saw sends %v, want %v", c.sent, want)
	}
	if err := l.SendMulticast(net.ParseIP("2001:db8::1"), MatterPort, []byte{1}); err == nil || len(c.sent) != 1 {
		t.Errorf("a unicast address reached the socket model: %v, sends %v", err, c.sent)
	}
	if err := l.SendMulticast(group, MatterPort, make([]byte, MaxDatagramSize+1)); !errors.Is(err, ErrPayloadTooLarge) || len(c.sent) != 1 {
		t.Errorf("an oversized datagram: %v, sends %v", err, c.sent)
	}
	c.err = errors.New("simulated send failure")
	if err := l.SendMulticast(group, MatterPort, []byte{1}); !errors.Is(err, c.err) {
		t.Errorf("SendMulticast error %v, want the socket model's", err)
	}
	c.err = nil
	_ = l.Close()
	if err := l.SendMulticast(group, MatterPort, []byte{1}); !errors.Is(err, ErrListenerClosed) || len(c.sent) != 1 {
		t.Errorf("SendMulticast after Close: %v, sends %v", err, c.sent)
	}
}

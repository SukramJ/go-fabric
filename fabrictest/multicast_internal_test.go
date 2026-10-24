// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package fabrictest

import (
	"context"
	"errors"
	"net"
	"slices"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock"
)

// TestMulticastModelDelivery pins the network side of the multicast
// model: a datagram to a group reaches the socket of every host that
// joined it (matter.js MockNetwork.shouldReceive), and nobody else; a
// leave stops the delivery; FailJoins refuses a join without changing the
// groups; WatchMemberships sees the joins and leaves that changed
// something.
func TestMulticastModelDelivery(t *testing.T) {
	n := newNetwork(clock.System{}, NetOptions{})
	open := func(i int) *socketUnderTest {
		t.Helper()
		l, err := n.listen(hostAddr(i), nil)
		if err != nil {
			t.Fatal(err)
		}
		su := &socketUnderTest{got: make(chan []byte, 4)}
		ctx, cancel := context.WithCancel(context.Background())
		t.Cleanup(func() { cancel(); _ = l.Close() })
		go func() {
			_ = l.Serve(ctx, func(b []byte, _ *net.UDPAddr) { su.got <- slices.Clone(b) })
		}()
		su.join, su.leave = l.JoinGroup, l.LeaveGroup
		su.send = l.Send
		return su
	}
	a, b, c := open(1), open(2), open(3)
	var changes []string
	stop := n.WatchMemberships(func(ch MembershipChange) { changes = append(changes, ch.String()) })
	defer stop()

	group := net.ParseIP("ff05::fa")
	for _, s := range []*socketUnderTest{a, b} {
		if err := s.join(group); err != nil {
			t.Fatal(err)
		}
	}
	if err := a.join(group); err != nil {
		t.Fatalf("a second join of a held group: %v", err)
	}
	if got := n.Members(group); got != 2 {
		t.Fatalf("Members = %d, want 2", got)
	}
	if err := c.send(&net.UDPAddr{IP: group, Port: hostAddr(1).Port}, []byte("hello")); err != nil {
		t.Fatal(err)
	}
	for name, s := range map[string]*socketUnderTest{"a": a, "b": b} {
		select {
		case got := <-s.got:
			if string(got) != "hello" {
				t.Errorf("%s received %q", name, got)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("%s did not receive the group datagram", name)
		}
	}
	select {
	case got := <-c.got:
		t.Fatalf("a host outside the group received %q", got)
	case <-time.After(20 * time.Millisecond):
	}

	if err := b.leave(group); err != nil {
		t.Fatal(err)
	}
	if err := b.leave(group); err != nil {
		t.Fatalf("leaving a group not held: %v", err)
	}
	if got := n.groupsOf(hostAddr(2)); len(got) != 0 {
		t.Errorf("b still holds %v", got)
	}
	refused := errors.New("simulated multicast join failure")
	n.FailJoins(func(net.IP) error { return refused })
	if err := c.join(group); !errors.Is(err, refused) {
		t.Fatalf("join under FailJoins: %v, want the injected error", err)
	}
	if got := n.Members(group); got != 1 {
		t.Errorf("a refused join changed the members: %d, want 1", got)
	}
	n.FailJoins(nil)
	if want := []string{"join ff05::fa", "join ff05::fa", "leave ff05::fa"}; !slices.Equal(changes, want) {
		t.Errorf("changes %v, want %v", changes, want)
	}
}

type socketUnderTest struct {
	got           chan []byte
	join          func(net.IP) error
	leave         func(net.IP) error
	send          func(*net.UDPAddr, []byte) error
	sendMulticast func(net.IP, int, []byte) error
	close         func() error
}

// TestMulticastModelGroupSend pins the send half of the model: a datagram
// the production listener sends to a group (Listener.SendMulticast, which
// hands it to the socket) leaves from the sender's address, passes the
// network's filter as any datagram does, and reaches the socket of every
// host holding the group and nobody else — matter.js MockUdpSocket.send
// with MockNetwork.shouldReceive. A closed socket sends nothing.
func TestMulticastModelGroupSend(t *testing.T) {
	n := newNetwork(clock.System{}, NetOptions{})
	open := func(i int) *socketUnderTest {
		t.Helper()
		l, err := n.listen(hostAddr(i), nil)
		if err != nil {
			t.Fatal(err)
		}
		su := &socketUnderTest{got: make(chan []byte, 4)}
		ctx, cancel := context.WithCancel(context.Background())
		t.Cleanup(func() { cancel(); _ = l.Close() })
		go func() {
			_ = l.Serve(ctx, func(b []byte, _ *net.UDPAddr) { su.got <- slices.Clone(b) })
		}()
		su.join, su.sendMulticast, su.close = l.JoinGroup, l.SendMulticast, l.Close
		return su
	}
	a, b, c := open(1), open(2), open(3)
	group := net.ParseIP("ff35:40:fd00::f:ab00:101")
	for _, s := range []*socketUnderTest{a, b} {
		if err := s.join(group); err != nil {
			t.Fatal(err)
		}
	}
	var seen []Datagram
	n.Filter(func(d Datagram) bool {
		seen = append(seen, d)
		return true
	})
	if err := c.sendMulticast(group, hostAddr(1).Port, []byte("group")); err != nil {
		t.Fatalf("SendMulticast: %v", err)
	}
	for name, s := range map[string]*socketUnderTest{"a": a, "b": b} {
		select {
		case got := <-s.got:
			if string(got) != "group" {
				t.Errorf("%s received %q", name, got)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("%s did not receive the group datagram", name)
		}
	}
	select {
	case got := <-c.got:
		t.Fatalf("the sender outside the group received %q", got)
	case <-time.After(20 * time.Millisecond):
	}
	n.Filter(nil)
	if len(seen) != 1 || !seen[0].From.IP.Equal(hostAddr(3).IP) || !seen[0].To.IP.Equal(group) || seen[0].To.Port != hostAddr(1).Port {
		t.Fatalf("the filter saw %+v, want one datagram from host 3 to the group", seen)
	}
	if err := c.sendMulticast(net.ParseIP("fe80::1"), hostAddr(1).Port, []byte("x")); err == nil {
		t.Error("a unicast destination was sent as a group datagram")
	}
	sent := n.Sent()
	_ = c.close()
	if err := c.sendMulticast(group, hostAddr(1).Port, []byte("late")); err == nil {
		t.Error("a closed socket sent a group datagram")
	}
	if n.Sent() != sent {
		t.Errorf("a closed socket put %d datagrams on the network", n.Sent()-sent)
	}
}

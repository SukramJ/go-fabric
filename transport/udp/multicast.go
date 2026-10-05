// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package udp

import (
	"errors"
	"fmt"
	"net"

	"golang.org/x/net/ipv6"
)

// ErrNoMulticastInterface is returned by [Listener.JoinGroup] when not one
// interface accepted the membership.
var ErrNoMulticastInterface = errors.New("udp: no interface joined the multicast group")

// multicastInterfaces lists the interfaces a membership is added on: every
// interface that is up and multicast-capable. Mirrors matter.js
// NodeJsNetwork.getMembershipMulticastInterfaces for an IPv6 socket that
// is not bound to one interface.
var multicastInterfaces = func() []net.Interface {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	out := make([]net.Interface, 0, len(ifaces))
	for _, ifi := range ifaces {
		if ifi.Flags&net.FlagUp != 0 && ifi.Flags&net.FlagMulticast != 0 {
			out = append(out, ifi)
		}
	}
	return out
}

// JoinGroup makes the listener's socket a member of the IPv6 multicast
// group on every up, multicast-capable interface, so group messages sent
// to it on the listener's port arrive through [Listener.Serve]. A failure
// on one interface does not stop the others; the call fails only when no
// interface joined. Mirrors matter.js NodeJsUdpSocket.addMembership, which
// adds the membership per interface and logs the ones that fail.
func (l *Listener) JoinGroup(group net.IP) error {
	return l.membership(group, true)
}

// LeaveGroup drops the membership [Listener.JoinGroup] added, on every
// interface it can. Mirrors matter.js NodeJsUdpSocket.dropMembership.
func (l *Listener) LeaveGroup(group net.IP) error {
	return l.membership(group, false)
}

func (l *Listener) membership(group net.IP, join bool) error {
	if group.To16() == nil || group.To4() != nil || !group.IsMulticast() {
		return fmt.Errorf("udp: %v is not an IPv6 multicast address", group)
	}
	if l.isClosed() {
		return ErrListenerClosed
	}
	pc := ipv6.NewPacketConn(l.conn)
	addr := &net.UDPAddr{IP: group}
	var (
		done    int
		lastErr error
	)
	for _, ifi := range multicastInterfaces() {
		var err error
		if join {
			err = pc.JoinGroup(&ifi, addr)
		} else {
			err = pc.LeaveGroup(&ifi, addr)
		}
		if err != nil {
			lastErr = err
			continue
		}
		done++
	}
	if done == 0 {
		if lastErr == nil {
			return ErrNoMulticastInterface
		}
		return fmt.Errorf("%w: %v: %w", ErrNoMulticastInterface, group, lastErr)
	}
	return nil
}

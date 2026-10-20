// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package fabrictest

import (
	"bytes"
	"fmt"
	"net"
	"net/netip"
	"slices"
)

// The multicast model. A host's socket joins and leaves IPv6 multicast
// groups through the production transport/udp.Listener (its JoinGroup and
// LeaveGroup hand the membership to a socket that carries its own); the
// network records the groups per host address and delivers a datagram
// sent to a group to the socket open on the datagram's port of every host
// that holds it. It mirrors matter.js MockNetwork
// (packages/general/src/net/mock/MockNetwork.ts at 862ad75f):
// addMembership / dropMembership / isMemberOf on the host, and
// shouldReceive, which takes a multicast datagram for a host that joined
// its group. As there, the membership is the host's: it outlives the
// socket that joined it, so a node that closes its socket without
// dropping a membership leaves it visible ([Device.MulticastGroups]) —
// a kernel drops it with the socket.
//
// A node's group messages leave through its bridge's socket: the device
// hands that socket to the node's group state as its transport
// ([Device.Start]), so a group send is one datagram from the host's
// address to the group, which [Network.Filter] sees and every member
// receives — matter.js sends group messages from the node's operational
// socket the same way (MockUdpSocket.send). The production node sends
// from a socket of its own (node/groupsend.go), which on a site would be
// a kernel socket off the network.
//
// There is one interface per host and nothing routes: a group is joined
// on the site's one network or not at all. A membership the real stack
// joins per interface (transport/udp.Listener joins on every up,
// multicast-capable interface) is one membership here, and an interface
// the site does not have is not modelled.

// MembershipChange is one join or leave a host's socket made, as
// [Network.WatchMemberships] reports it — an entry of matter.js
// recordMembershipChanges (packages/node/test/behaviors/groupcast/
// multicast-helpers.ts), "join <ip>" / "leave <ip>".
type MembershipChange struct {
	// Host is the address of the host whose socket joined or left.
	Host *net.UDPAddr
	// Group is the multicast group.
	Group net.IP
	// Join is true for a join, false for a leave.
	Join bool
}

// String is the change as matter.js's recorder writes it: "join ff05::fa".
func (c MembershipChange) String() string {
	if c.Join {
		return "join " + c.Group.String()
	}
	return "leave " + c.Group.String()
}

// groupKey is a multicast group as a map key; ok is false for anything
// but an IPv6 multicast address.
func groupKey(group net.IP) (netip.Addr, bool) {
	if group.To4() != nil {
		return netip.Addr{}, false
	}
	a, ok := netip.AddrFromSlice(group.To16())
	if !ok || !a.IsMulticast() {
		return netip.Addr{}, false
	}
	return a.WithZone(""), true
}

// join records that the host of s joined group. [Network.FailJoins]
// decides first; a refused join changes nothing. Joining a group the
// host holds is a no-op, as MockNetwork's Set is. Mirrors matter.js
// MockNetwork.addMembership.
func (n *Network) join(s *socket, group net.IP) error {
	g, ok := groupKey(group)
	if !ok {
		return fmt.Errorf("fabrictest: %v is not an IPv6 multicast address", group)
	}
	n.mu.Lock()
	fail := n.joinError
	n.mu.Unlock()
	if fail != nil {
		if err := fail(slices.Clone(group)); err != nil {
			return err
		}
	}
	host := s.key.Addr()
	n.mu.Lock()
	set := n.members[host]
	if set == nil {
		set = make(map[netip.Addr]struct{})
		n.members[host] = set
	}
	_, had := set[g]
	set[g] = struct{}{}
	n.mu.Unlock()
	if !had {
		n.reportMembership(MembershipChange{Host: cloneAddr(s.local), Group: net.IP(g.AsSlice()), Join: true})
	}
	return nil
}

// leave records that the host of s left group; leaving a group it does
// not hold is a no-op. Mirrors matter.js MockNetwork.dropMembership.
func (n *Network) leave(s *socket, group net.IP) error {
	g, ok := groupKey(group)
	if !ok {
		return fmt.Errorf("fabrictest: %v is not an IPv6 multicast address", group)
	}
	host := s.key.Addr()
	n.mu.Lock()
	_, had := n.members[host][g]
	delete(n.members[host], g)
	n.mu.Unlock()
	if had {
		n.reportMembership(MembershipChange{Host: cloneAddr(s.local), Group: net.IP(g.AsSlice()), Join: false})
	}
	return nil
}

func (n *Network) reportMembership(c MembershipChange) {
	n.mu.Lock()
	watchers := make([]func(MembershipChange), 0, len(n.watchers))
	for _, fn := range n.watchers {
		watchers = append(watchers, fn)
	}
	n.mu.Unlock()
	for _, fn := range watchers {
		fn(c)
	}
}

// FailJoins installs fail, which is asked on every multicast join a host's
// socket makes from now on: a non-nil error fails that join — the
// listener's JoinGroup returns it and the host's groups stay unchanged —
// and nil lets it through. nil removes the hook. The counterpart of a test
// replacing matter.js MockNetwork.addMembership with one that throws
// (GroupcastServerTest.ts "multicast join failures"). It is a method
// rather than a [NetOptions] field so NetOptions stays comparable.
func (n *Network) FailJoins(fail func(group net.IP) error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.joinError = fail
}

// WatchMemberships calls fn for every join and leave a host's socket
// makes from now on — not for a join [Network.FailJoins] refused, nor
// for a join of a group the host already holds. fn runs synchronously on
// the joining goroutine and must not block. The counterpart of matter.js
// recordMembershipChanges, which wraps MockNetwork.addMembership and
// dropMembership. stop ends the watch.
func (n *Network) WatchMemberships(fn func(MembershipChange)) (stop func()) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.nextWatcher++
	id := n.nextWatcher
	n.watchers[id] = fn
	return func() {
		n.mu.Lock()
		defer n.mu.Unlock()
		delete(n.watchers, id)
	}
}

// Members is the number of hosts that hold group. The counterpart of
// matter.js MockNetwork.isMemberOf, asked across the site's hosts.
func (n *Network) Members(group net.IP) int {
	g, ok := groupKey(group)
	if !ok {
		return 0
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	count := 0
	for _, set := range n.members {
		if _, in := set[g]; in {
			count++
		}
	}
	return count
}

// groupsOf is the groups the host of addr holds, sorted.
func (n *Network) groupsOf(addr *net.UDPAddr) []net.IP {
	key, ok := addrPort(addr)
	if !ok {
		return nil
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	out := make([]net.IP, 0, len(n.members[key.Addr()]))
	for g := range n.members[key.Addr()] {
		out = append(out, net.IP(g.AsSlice()))
	}
	slices.SortFunc(out, func(a, b net.IP) int { return bytes.Compare(a, b) })
	return out
}

// memberSockets is the open socket on port of every host that holds
// group, for a datagram sent to the group.
func (n *Network) memberSockets(group netip.Addr, port uint16) []*socket {
	n.mu.Lock()
	defer n.mu.Unlock()
	var out []*socket
	for host, set := range n.members {
		if _, in := set[group]; !in {
			continue
		}
		if s := n.sockets[netip.AddrPortFrom(host, port)]; s != nil {
			out = append(out, s)
		}
	}
	return out
}

// MulticastGroups is the IPv6 multicast groups the device's host holds on
// the in-memory network, sorted — the groups its bridge joined for the
// groups with a member endpoint and has not left. nil on loopback, where
// the kernel holds them. The counterpart of matter.js
// MockNetwork.isMemberOf on the node's own network.
func (d *Device) MulticastGroups() []net.IP {
	if d.site.net == nil {
		return nil
	}
	return d.site.net.groupsOf(hostAddr(d.index))
}

// JoinGroup is the socket's membership surface the production listener
// delegates to (transport/udp Listener.JoinGroup).
func (s *socket) JoinGroup(group net.IP) error {
	select {
	case <-s.done:
		return net.ErrClosed
	default:
	}
	return s.net.join(s, group)
}

// LeaveGroup is the counterpart of [socket.JoinGroup].
func (s *socket) LeaveGroup(group net.IP) error {
	select {
	case <-s.done:
		return net.ErrClosed
	default:
	}
	return s.net.leave(s, group)
}

// SendMulticast is the send half of the socket's multicast surface the
// production listener delegates to (transport/udp Listener.SendMulticast):
// the datagram goes onto the network from the socket's address to the
// group on port, through the network's filter and delivery options, and
// reaches the socket on that port of every host holding the group. Mirrors
// matter.js MockUdpSocket.send (packages/general/src/net/mock/
// MockUdpSocket.ts:87-97 at 862ad75f), which hands the packet to the
// simulator's router, and MockNetwork.shouldReceive (MockNetwork.ts:79-81),
// which takes it for a host that joined its group.
func (s *socket) SendMulticast(group net.IP, port int, payload []byte) error {
	if _, ok := groupKey(group); !ok {
		return fmt.Errorf("fabrictest: %v is not an IPv6 multicast address", group)
	}
	select {
	case <-s.done:
		return net.ErrClosed
	default:
	}
	s.net.send(s.local, &net.UDPAddr{IP: slices.Clone(group), Port: port}, payload)
	return nil
}

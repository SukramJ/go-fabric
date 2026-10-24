// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package fabrictest

import (
	"errors"
	"fmt"
	"math/rand/v2"
	"net"
	"net/netip"
	"sync"
	"sync/atomic"
	"time"

	"github.com/SukramJ/go-fabric/clock"
	"github.com/SukramJ/go-fabric/internal/inflight"
	"github.com/SukramJ/go-fabric/internal/udpseam"
	"github.com/SukramJ/go-fabric/transport/udp"
)

// The in-memory network: datagrams between the hosts of a site over
// channels, addressed like UDP. Each host the site adds — a device or a
// controller — is given one IPv6 link-local address, fe80::<index>, and
// its socket listens on port [udp.MatterPort] of it. Both ends
// run the production transport/udp.Listener over that socket
// (internal/udpseam), so the bridge and the controller's exchange manager
// see datagrams exactly as a kernel socket delivers them: a lost one is
// gone, a late one is late, a doubled one arrives twice.

// NetOptions shapes the in-memory network's delivery. The zero value
// delivers every datagram once, at once.
type NetOptions struct {
	// Loss is the probability in [0, 1] that a datagram is dropped.
	Loss float64
	// Delay is the time a datagram is in flight, on the site's clock: with
	// a [github.com/SukramJ/go-fabric/clock/clocktest.Fake] it arrives when
	// the test advances the clock past it, so an MRP test steps the
	// retransmission it provokes. 0 delivers at once.
	Delay time.Duration
	// Duplicate is the probability in [0, 1] that a delivered datagram is
	// delivered a second time.
	Duplicate float64
	// Seed seeds the draws of Loss and Duplicate; equal seeds drop and
	// double the same datagrams of the same sequence.
	Seed uint64
}

// Datagram is one datagram on the in-memory network, as a [Network.Filter]
// sees it.
type Datagram struct {
	// From and To are the sender's and the addressee's address.
	From, To *net.UDPAddr
	// Payload is the datagram; a filter must not modify it.
	Payload []byte
}

// queueDepth is a socket's receive queue; a full queue drops, as a kernel
// socket's buffer does.
const queueDepth = 1024

// Network is a site's in-memory network. It is safe for concurrent use.
type Network struct {
	clk clock.Clock

	// mu guards filter, opts, rnd and sockets.
	mu      sync.Mutex
	opts    NetOptions
	rnd     *rand.Rand
	filter  func(Datagram) bool
	sockets map[netip.AddrPort]*socket
	// members is the multicast groups each host address holds
	// (multicast.go).
	members     map[netip.Addr]map[netip.Addr]struct{}
	joinError   func(group net.IP) error
	watchers    map[uint64]func(MembershipChange)
	nextWatcher uint64

	// sent counts every datagram handed to the network, delivered or not.
	sent atomic.Uint64
}

func newNetwork(clk clock.Clock, opts NetOptions) *Network {
	return &Network{
		clk:      clk,
		opts:     opts,
		rnd:      rand.New(rand.NewPCG(opts.Seed, opts.Seed^0x9E3779B97F4A7C15)), //nolint:gosec // a test network's draws, not a secret
		sockets:  make(map[netip.AddrPort]*socket),
		members:  make(map[netip.Addr]map[netip.Addr]struct{}),
		watchers: make(map[uint64]func(MembershipChange)),
	}
}

// Options are the network's current delivery options.
func (n *Network) Options() NetOptions {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.opts
}

// Set replaces the delivery options from the next datagram on; a datagram
// already waiting out a delay keeps its own. A changed Seed restarts the
// draws.
func (n *Network) Set(opts NetOptions) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if opts.Seed != n.opts.Seed {
		n.rnd = rand.New(rand.NewPCG(opts.Seed, opts.Seed^0x9E3779B97F4A7C15)) //nolint:gosec // as in newNetwork
	}
	n.opts = opts
}

// Filter installs keep, which sees every datagram before the options do
// and drops it by returning false: a test loses one chosen message
// rather than a share of all. nil removes the filter.
func (n *Network) Filter(keep func(Datagram) bool) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.filter = keep
}

// Sent is the number of datagrams handed to the network so far, delivered
// or not.
func (n *Network) Sent() uint64 { return n.sent.Load() }

// hostAddr is the address of host index i.
func hostAddr(i int) *net.UDPAddr {
	var a [16]byte
	a[0], a[1] = 0xfe, 0x80
	a[14], a[15] = byte(i>>8), byte(i) //nolint:gosec // a site has far fewer than 65536 hosts
	return &net.UDPAddr{IP: net.IP(a[:]), Port: udp.MatterPort}
}

// listen opens the socket of addr and returns the production listener
// over it. An address is held by one open socket at a time; a host that
// stopped may listen on its address again.
//
// The socket counts every datagram its listener read as work of g from
// the read until the listener reports it handled (udpseam.Handled,
// internal/inflight UDPDatagrams): a datagram is in the network until it
// is read and in the host's hands until its handler returned, so a site
// never sees it nowhere.
func (n *Network) listen(addr *net.UDPAddr, g *inflight.Group) (*udp.Listener, error) {
	key, ok := addrPort(addr)
	if !ok {
		return nil, fmt.Errorf("fabrictest: %v is not an address of the network", addr)
	}
	s := &socket{net: n, key: key, local: cloneAddr(addr), in: make(chan Datagram, queueDepth), done: make(chan struct{}), group: g}
	n.mu.Lock()
	if _, taken := n.sockets[key]; taken {
		n.mu.Unlock()
		return nil, fmt.Errorf("fabrictest: %v is in use", addr)
	}
	n.sockets[key] = s
	n.mu.Unlock()
	l, ok := udpseam.NewListener(s).(*udp.Listener)
	if !ok {
		_ = s.Close()
		return nil, errors.New("fabrictest: transport/udp installed no listener seam")
	}
	return l, nil
}

// send carries one datagram from src to dst.
func (n *Network) send(src, dst *net.UDPAddr, payload []byte) {
	n.sent.Add(1)
	d := Datagram{From: cloneAddr(src), To: cloneAddr(dst), Payload: append([]byte(nil), payload...)}
	n.mu.Lock()
	keep := n.filter == nil || n.filter(d)
	opts := n.opts
	lost := keep && opts.Loss > 0 && n.rnd.Float64() < opts.Loss
	twice := keep && !lost && opts.Duplicate > 0 && n.rnd.Float64() < opts.Duplicate
	n.mu.Unlock()
	if !keep || lost {
		return
	}
	copies := 1
	if twice {
		copies = 2
	}
	for range copies {
		if opts.Delay <= 0 {
			n.queue(d)
			continue
		}
		n.clk.AfterFunc(opts.Delay, func() { n.queue(d) })
	}
}

// queue puts d on its addressee's receive queue: the socket open on the
// address when d arrives, so a host that restarted in between receives
// it — for a multicast group the socket on d's port of every host that
// holds the group then (multicast.go). No socket, or a full queue, drops
// it.
func (n *Network) queue(d Datagram) {
	key, ok := addrPort(d.To)
	if !ok {
		return
	}
	if key.Addr().IsMulticast() {
		for _, s := range n.memberSockets(key.Addr(), key.Port()) {
			deliver(s, d)
		}
		return
	}
	n.mu.Lock()
	s := n.sockets[key]
	n.mu.Unlock()
	if s == nil {
		return
	}
	deliver(s, d)
}

// deliver puts d on the receive queue of s; a closed socket or a full
// queue drops it.
func deliver(s *socket, d Datagram) {
	select {
	case <-s.done:
	case s.in <- d:
	default:
	}
}

// pending is the number of datagrams queued at a socket and not yet read:
// what [Site.Settle] waits for. A datagram waiting out a delay is not
// pending — it waits for the clock, which a quiet site moves.
func (n *Network) pending() int {
	n.mu.Lock()
	defer n.mu.Unlock()
	total := 0
	for _, s := range n.sockets {
		total += len(s.in)
	}
	return total
}

func (n *Network) release(s *socket) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.sockets[s.key] == s {
		delete(n.sockets, s.key)
	}
}

// socket is one host's end of the network: the udpseam.PacketConn its
// listener reads and writes.
type socket struct {
	net   *Network
	key   netip.AddrPort
	local *net.UDPAddr
	in    chan Datagram
	done  chan struct{}
	once  sync.Once
	// group counts the datagrams read and not yet handled.
	group *inflight.Group
}

func (s *socket) ReadFromUDP(b []byte) (int, *net.UDPAddr, error) {
	select {
	case d := <-s.in:
		// From here the datagram is the host's work, no longer the
		// network's (pending); the instant between the receive and the
		// count is the settle period's to cover.
		s.group.Enter(inflight.UDPDatagrams)
		return copy(b, d.Payload), d.From, nil
	case <-s.done:
		return 0, nil, net.ErrClosed
	}
}

func (s *socket) WriteToUDP(b []byte, addr *net.UDPAddr) (int, error) {
	select {
	case <-s.done:
		return 0, net.ErrClosed
	default:
	}
	s.net.send(s.local, addr, b)
	return len(b), nil
}

func (s *socket) LocalAddr() net.Addr { return cloneAddr(s.local) }

// Handled implements udpseam.Handled: the listener finished with one
// datagram this socket handed it.
func (s *socket) Handled() { s.group.Leave(inflight.UDPDatagrams) }

func (s *socket) Close() error {
	s.once.Do(func() {
		close(s.done)
		s.net.release(s)
	})
	return nil
}

// addrPort is addr as a map key: the address without zone, IPv4-mapped
// addresses unmapped.
func addrPort(addr *net.UDPAddr) (netip.AddrPort, bool) {
	if addr == nil {
		return netip.AddrPort{}, false
	}
	ip, ok := netip.AddrFromSlice(addr.IP)
	if !ok {
		return netip.AddrPort{}, false
	}
	return netip.AddrPortFrom(ip.Unmap().WithZone(""), uint16(addr.Port)), true //nolint:gosec // a UDP port
}

func cloneAddr(a *net.UDPAddr) *net.UDPAddr {
	if a == nil {
		return nil
	}
	return &net.UDPAddr{IP: append(net.IP(nil), a.IP...), Port: a.Port, Zone: a.Zone}
}

// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package peer

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net"
	"net/netip"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/SukramJ/go-fabric/client/discovery"
	"github.com/SukramJ/go-fabric/client/exchange"
	"github.com/SukramJ/go-fabric/client/handshake"
	"github.com/SukramJ/go-fabric/transport/mrp"
)

// ErrNotConnectable is the error of a connection attempt after
// [Set.Disconnect] — matter.js's "Client networking is not initialized"
// (PeerSet.ts:83-88), which the connection process treats as an
// unhandled error.
var ErrNotConnectable = errors.New("peer: client networking is not initialized")

// ErrClosed is the error of [Peer.Connect] on a closed peer — matter.js
// ClosedError("Peer closed") (Peer.ts:551).
var ErrClosed = errors.New("peer: closed")

// UnreachableError ends a [Peer.Connect] whose wait for a session ran
// out — matter.js PeerUnreachableError (PeerCommunicationError.ts:28-35);
// the name drops the package's stutter.
// It is transient (exchange.ErrTransient, :19).
type UnreachableError struct {
	// TimeOffline is how long the peer had been unreachable.
	TimeOffline time.Duration
}

func (e *UnreachableError) Error() string {
	return fmt.Sprintf("peer: unreachable for %s", e.TimeOffline)
}

// Is makes the error match exchange.ErrTransient.
func (e *UnreachableError) Is(target error) bool { return target == exchange.ErrTransient }

// Secure Channel status codes the error handling reads — matter.js
// GeneralStatusCode.Busy (types/src/protocol/definitions/general.ts:33)
// and SecureChannelStatusCode.Busy
// (types/src/protocol/definitions/secure-channel.ts:65-79).
const (
	generalStatusBusy       uint16 = 8
	secureChannelStatusBusy uint16 = 4
)

// kickOrigin says who kicked the connection process — matter.js KickOrigin
// (PeerConnection.ts:49-55).
type kickOrigin int

const (
	kickDiscover kickOrigin = iota // DNS-SD discovery found the fallback address
	kickConnect                    // Peer.Kick or ConnectOptions.Kick
)

func (o kickOrigin) String() string {
	if o == kickDiscover {
		return "discover"
	}
	return "connect"
}

// ConnectOptions configures [Peer.Connect] — matter.js Peer.ConnectOptions
// (Peer.ts:687-752) without the transport, network and MRP-margin options.
type ConnectOptions struct {
	// ConnectionTimeout bounds the wait for a session: 0 is the set's
	// DefaultConnectionTimeout, a negative value waits without bound
	// (matter.js Forever). Unless Kick is set, the time the peer has
	// already been offline is deducted (Peer.ts:464-475).
	ConnectionTimeout time.Duration
	// Kick kicks a connection process already running (Peer.ts:715-720).
	Kick bool
	// Timing overrides the set's timing for a process this call starts
	// (Peer.ts:722-731).
	Timing *Timing
	// HandleError replaces the set's error handler for a process this call
	// starts (Peer.ts:745-751).
	HandleError ErrorHandler
	// Network names the network profile whose throttle the connection
	// process takes a slot of — its connect sub-profile when it has one —
	// for a process this call starts; "" is the peer's medium profile
	// (matter.js PeerConnection.ts:110-118 at 862ad75f, options.network).
	// "icdLit" is the unthrottled profile a sustained subscription to a
	// LIT ICD peer connects on, so its subscribe lands inside the peer's
	// brief active window (SustainedSubscription.ts:133-149). A name no
	// profile has fails the call with [ErrUnknownNetworkProfile].
	Network string
	// AdditionalMrpDelay, when set, is the MRP margin of the CASE exchange
	// in place of the medium profile's connect margin
	// (PeerConnection.ts:111-114).
	AdditionalMrpDelay *time.Duration
	// RequiredTransport is a hard transport constraint for a process this
	// call starts: TCP or UDP only — "Large Message Quality requires TCP"
	// (Peer.ts:734 at 862ad75f); nil for none.
	RequiredTransport *exchange.Transport
	// PreferredTransport overrides the peer's transport preference for a
	// process this call starts; nil keeps it (Peer.ts:736-741).
	PreferredTransport *exchange.Transport
}

// attemptKey is an attempt's (ip, port, transport) — the variant of an
// address matter.js keys its attempts by (PeerConnection.ts:338-358).
type attemptKey struct {
	ap  netip.AddrPort
	tcp bool
}

// target is one variant of an address to attempt: over UDP, or over TCP
// (expandAddresses, PeerConnection.ts:323-336).
type target struct {
	addr discovery.Address
	tcp  bool
}

func (t target) key() attemptKey { return attemptKey{t.addr.AddrPort(), t.tcp} }

func (t target) String() string {
	if t.tcp {
		return "tcp://" + t.addr.String()
	}
	return t.addr.String()
}

// attempt is the connection attempt to one address (PeerConnection.ts:863-866).
type attempt struct {
	addr   discovery.Address
	tcp    bool
	addrNo int
	ctx    context.Context
	cancel context.CancelFunc
	// exchange is the CASE exchange of the current try, which the kick
	// reads.
	exchange atomic.Pointer[exchange.Exchange]
}

// connection is one run of the connection process of a peer — matter.js
// PeerConnection (packages/protocol/src/peer/PeerConnection.ts:93-753 at
// 0ceb202d): an attempt per address and transport variant, over UDP or
// TCP. Its state (:129-177) is guarded by mu.
type connection struct {
	p           *Peer
	timing      Timing
	handleError ErrorHandler
	// network and margin are the profile the process holds a slot of and
	// the CASE exchanges' MRP margin (PeerConnection.ts:110-118).
	network *NetworkProfile
	margin  time.Duration
	// required and preferred are the transport options of the call that
	// started the process (PeerConnection.ts:101-105).
	required, preferred *exchange.Transport
	ctx                 context.Context
	cancel              context.CancelFunc
	done                chan struct{}
	workers             sync.WaitGroup
	wake                chan struct{}

	// mu guards the connection's state: every field below it, session
	// through kickHandlers.
	mu             sync.Mutex
	session        *exchange.Session
	fatal          error
	attempts       map[attemptKey]*attempt
	pending        []target
	fallback       *discovery.Address
	lastAttemptAt  time.Time
	addrsAttempted int
	lastRestartAt  time.Time
	lastKickAt     time.Time
	kickHandlers   map[*attempt]func(kickOrigin)
	// retained are the addresses discovery stopped advertising that the
	// process goes on attempting for Timing.ProvenAddressRetention: the
	// proven host's last addresses, or — no host learned — the stored
	// operational address (retainLocked, issue 507).
	retained map[netip.AddrPort]*retention
	// spent are the operational addresses whose retention expired: not
	// anchored again until discovery advertises them (anchorLocked).
	spent map[netip.AddrPort]bool
	// hosts are the SRV targets of the discovered addresses: a deletion
	// the service reports carries none (discovery.AddressChanges).
	hosts map[netip.AddrPort]string
	// refall wakes retireStaleFallback when the fallback was marked or a
	// retention ended — a change the service's own notification does not
	// carry (rewakeLocked).
	refall chan struct{}
}

// retention is one retained address: the address with the host discovery
// named for it, since when it is no longer advertised, why it is kept and
// the timer of its bound.
type retention struct {
	addr   discovery.Address
	since  time.Time
	reason string
	timer  exchange.Timer
}

// initiateConnection starts the peer's connection process unless one
// runs, which it kicks when opts asks (Peer.ts:628-673). It returns the
// running process, nil on a closed peer.
func (p *Peer) initiateConnection(opts ConnectOptions) *connection {
	// The network profile reads the peer's medium through the host's hook
	// (Peer.SetMedium), which may take the host's own lock: it runs before
	// the peer's lock, as the host's fabric lookup does (ADR 0026 P6).
	network, margin, netErr := p.connectNetwork(opts)
	transports := p.ResolveTransports(opts.RequiredTransport, opts.PreferredTransport)
	p.mu.Lock()
	defer p.mu.Unlock()
	if c := p.connecting; c != nil {
		if opts.Kick {
			go c.kick(kickConnect)
		}
		return c
	}
	if p.closed {
		return nil
	}
	// A session that a process adopted since the caller looked — the
	// process has already ended — answers at once: no second CASE
	// handshake. matter.js checks newestSession() and starts the process in
	// one turn of the event loop (Peer.ts:438-462), so no session can
	// appear in between; here the check runs under the lock that adopting
	// a session takes (W4-ctlnet-04).
	if p.liveSessionLocked(transports) {
		return &connection{p: p, done: closedDone}
	}
	setTiming := p.set.Timing()
	timing := setTiming
	if opts.Timing != nil {
		timing = Merge(setTiming, *opts.Timing)
	}
	handleError := opts.HandleError
	if handleError == nil {
		handleError = p.set.errorHandler()
	}
	if netErr != nil {
		return &connection{fatal: netErr, done: closedDone}
	}
	ctx, cancel := context.WithCancel(p.ctx)
	c := &connection{
		p:            p,
		timing:       timing,
		handleError:  handleError,
		network:      network,
		margin:       margin,
		required:     opts.RequiredTransport,
		preferred:    opts.PreferredTransport,
		ctx:          ctx,
		cancel:       cancel,
		done:         make(chan struct{}),
		wake:         make(chan struct{}, 1),
		attempts:     make(map[attemptKey]*attempt),
		kickHandlers: make(map[*attempt]func(kickOrigin)),
		retained:     make(map[netip.AddrPort]*retention),
		spent:        make(map[netip.AddrPort]bool),
		hosts:        make(map[netip.AddrPort]string),
		refall:       make(chan struct{}, 1),
	}
	p.connecting = c
	p.workers.Add(1)
	go func() {
		defer p.workers.Done()
		c.run()
		p.mu.Lock()
		if p.connecting == c {
			p.connecting = nil
		}
		p.connectionInitiatedAt = time.Time{}
		p.mu.Unlock()
		cancel()
		close(c.done)
	}()
	return c
}

// Connect returns a live session with the peer, establishing one when
// none exists: the newest live session is returned; otherwise the shared
// connection process starts (or is kicked) and Connect waits for it within
// the connection timeout, then looks again. A process that ended with a
// fatal error returns it; a timeout is a [*UnreachableError]; a closed
// peer [ErrClosed]. With a transport list ([Peer.ResolveTransports]) the
// newest session over its first transport that has one is returned.
// Mirrors matter.js Peer.connect (Peer.ts:414-491) without the group
// branch.
func (p *Peer) Connect(ctx context.Context, opts ConnectOptions) (*exchange.Session, error) {
	for {
		if s := p.sessionFor(p.ResolveTransports(opts.RequiredTransport, opts.PreferredTransport)); s != nil {
			return s, nil
		}
		// The process is shared and outlives this call: it runs on the
		// peer's context, not ctx (Peer.ts:636, an Abort of the peer's own).
		c := p.initiateConnection(opts) //nolint:contextcheck // see above
		if c == nil {
			return nil, ErrClosed
		}
		if c.p == nil {
			return nil, c.fatal // the process could not start (connectNetwork)
		}
		if err := p.waitFor(ctx, c, opts); err != nil {
			return nil, err
		}
	}
}

// waitFor waits for the process c within the connection timeout
// (Peer.ts:464-489).
func (p *Peer) waitFor(ctx context.Context, c *connection, opts ConnectOptions) error {
	timeout := opts.ConnectionTimeout
	if timeout == 0 {
		timeout = p.set.Timing().DefaultConnectionTimeout
	}
	var expired <-chan struct{}
	if timeout > 0 {
		if !opts.Kick {
			timeout -= p.TimeOffline()
		}
		if timeout <= 0 {
			return &UnreachableError{TimeOffline: p.TimeOffline()}
		}
		ch := make(chan struct{})
		t := p.set.clock.AfterFunc(timeout, func() { close(ch) })
		defer t.Stop()
		expired = ch
	}
	select {
	case <-c.done:
		c.mu.Lock()
		defer c.mu.Unlock()
		return c.fatal
	case <-ctx.Done():
		return ctx.Err()
	case <-p.ctx.Done():
		return ErrClosed
	case <-expired:
		return &UnreachableError{TimeOffline: p.TimeOffline()}
	}
}

// Kick kicks the connection process: an attempt whose CASE exchange has
// retransmitted enough restarts with a fresh backoff, rate-limited
// (Peer.ts:493-502).
func (p *Peer) Kick() {
	p.mu.Lock()
	c := p.connecting
	p.mu.Unlock()
	if c != nil {
		c.kick(kickConnect)
	}
}

// TimeOffline is how long the connection process has been trying, 0 when
// none runs (Peer.ts:334-346). matter.js reads the service status's
// connectionInitiatedAt; the peer keeps the same instant on its own clock.
func (p *Peer) TimeOffline() time.Duration {
	p.mu.Lock()
	at := p.connectionInitiatedAt
	p.mu.Unlock()
	if at.IsZero() {
		return 0
	}
	return p.set.clock.Now().Sub(at)
}

// stopConnecting aborts the connection process and waits for it
// (Peer.ts:512-520).
func (p *Peer) stopConnecting(ctx context.Context) {
	p.mu.Lock()
	c := p.connecting
	p.mu.Unlock()
	if c == nil {
		return
	}
	c.cancel()
	select {
	case <-c.done:
	case <-ctx.Done():
	}
}

// sessionAdded aborts the connection process: a session from any source
// ends it (Peer.ts:638-639).
func (p *Peer) sessionAdded() {
	p.mu.Lock()
	c := p.connecting
	p.mu.Unlock()
	if c != nil {
		c.cancel()
	}
}

// closedDone is the done channel of a process that never ran.
var closedDone = func() chan struct{} { c := make(chan struct{}); close(c); return c }()

// connectNetwork is the profile a connection process opts start throttles
// on and the MRP margin of its CASE exchanges — PeerConnection.ts:110-118:
// the profile opts.Network names, else the medium profile, its connect
// sub-profile when it has one; the margin is opts.AdditionalMrpDelay, else
// the medium profile's connect margin, else its margin.
func (p *Peer) connectNetwork(opts ConnectOptions) (*NetworkProfile, time.Duration, error) {
	networks := p.set.networks
	network, err := networks.Select(p, opts.Network)
	if err != nil {
		return nil, 0, err
	}
	medium := networks.ForPeer(p)
	margin := medium.AdditionalMrpDelay
	if medium.Connect != nil {
		margin = medium.Connect.AdditionalMrpDelay
	}
	if opts.AdditionalMrpDelay != nil {
		margin = *opts.AdditionalMrpDelay
	}
	if network.Connect != nil {
		network = network.Connect
	}
	return network, margin, nil
}

// run is the body of PeerConnection (:123-215), which first reserves a
// slot of its network profile for the whole process (:110-121).
func (c *connection) run() {
	p := c.p
	slot, err := c.network.Semaphore.Obtain(c.ctx)
	if err != nil {
		return
	}
	defer slot.Close()
	result := make(chan bool, 1)
	p.mu.Lock()
	p.connectionInitiatedAt = p.set.clock.Now()
	p.mu.Unlock()
	p.status.Connecting(result)

	changes := p.service.AddressChanges(c.ctx)
	c.goWork(c.scheduleAttempts)
	c.goWork(c.retireStaleFallback)
	c.mu.Lock()
	c.maybeAttemptFallbackLocked()
	c.mu.Unlock()

	for change := range changes {
		c.mu.Lock()
		switch change.Kind {
		case discovery.AddressAdded:
			c.addAddressLocked(change.Address)
			c.anchorLocked()
		case discovery.AddressDeleted:
			c.withdrawnLocked(change.Address)
			c.maybeAttemptFallbackLocked()
		}
		c.mu.Unlock()
	}

	c.mu.Lock()
	connected := c.session != nil
	// A session, or the end of the process, ends every retention.
	for ap, r := range c.retained {
		r.timer.Stop()
		delete(c.retained, ap)
	}
	c.mu.Unlock()
	if connected {
		p.status.SetReachable(true)
	}
	c.cancel()
	result <- len(p.Sessions()) > 0
	c.workers.Wait()
}

func (c *connection) goWork(fn func()) {
	c.workers.Add(1)
	go func() {
		defer c.workers.Done()
		fn()
	}()
}

// signal wakes the scheduler: an address was added or deleted, or the
// delay was reset.
func (c *connection) signal() {
	select {
	case c.wake <- struct{}{}:
	default:
	}
}

// sleep waits d on the peer's clock; false when ctx ended first.
func (c *connection) sleep(ctx context.Context, d time.Duration, wake <-chan struct{}) bool {
	fired := make(chan struct{})
	t := c.p.set.clock.AfterFunc(d, func() { close(fired) })
	defer t.Stop()
	select {
	case <-fired:
	case <-wake:
	case <-ctx.Done():
		return false
	}
	return true
}

// scheduleAttempts starts an attempt per pending address, the most
// desirable first, each at least DelayBeforeNextAddress after the last
// (PeerConnection.ts:217-262).
//
// The proven host's address may be queued after an attempt at another
// target has started — the service delivers its addresses one at a time
// in the desirability order; preemptForProvenLocked then starts it at
// once, outside the stagger.
func (c *connection) scheduleAttempts() {
	for {
		c.mu.Lock()
		if c.ctx.Err() != nil {
			c.mu.Unlock()
			return
		}
		if len(c.pending) == 0 {
			c.mu.Unlock()
			select {
			case <-c.wake:
			case <-c.ctx.Done():
				return
			}
			continue
		}
		if !c.lastAttemptAt.IsZero() {
			if delay := c.timing.DelayBeforeNextAddress - c.p.set.clock.Now().Sub(c.lastAttemptAt); delay > 0 {
				c.mu.Unlock()
				if !c.sleep(c.ctx, delay, c.wake) {
					return
				}
				continue
			}
		}
		c.initiateAttemptLocked(c.shiftLocked(c.p.service.Addresses()))
		c.mu.Unlock()
	}
}

// shiftLocked takes the most desirable pending address — the heap of
// :145-152 ordered by discovery.CompareDesirability, the variants of one
// address by their index in the transport list, the first of equals —
// with the proven host's addresses ahead of every other (provenHost,
// compareForHost) while discovered, the service's addresses, carries it,
// and the fallback ahead of every other while it does not
// (fallbackFirstLocked), then the retained addresses ahead of the rest
// (retainLocked). The proven host is learned first (learnHost).
func (c *connection) shiftLocked(discovered []discovery.Address) target {
	c.p.learnHost(discovered)
	known := c.p.operationalHost()
	host := provenHost(known, discovered)
	first := c.fallbackFirstLocked(known, discovered)
	rank := func(a, b discovery.Address) int {
		if first != nil {
			if af, bf := a.AddrPort() == first.AddrPort(), b.AddrPort() == first.AddrPort(); af != bf {
				if af {
					return -1
				}
				return 1
			}
		}
		if ar, br := c.retained[a.AddrPort()] != nil, c.retained[b.AddrPort()] != nil; ar != br {
			if ar {
				return -1
			}
			return 1
		}
		return compareForHost(host, a, b)
	}
	transports := c.p.ResolveTransports(c.required, c.preferred)
	index := func(t target) int {
		for i, tr := range transports {
			if (tr == exchange.TransportTCP) == t.tcp {
				return i
			}
		}
		return -1
	}
	best := 0
	for i := 1; i < len(c.pending); i++ {
		d := rank(c.pending[i].addr, c.pending[best].addr)
		if d < 0 || (d == 0 && transports != nil && index(c.pending[i]) < index(c.pending[best])) {
			best = i
		}
	}
	t := c.pending[best]
	c.pending = append(c.pending[:best], c.pending[best+1:]...)
	return t
}

// provenHost is the peer's operational host (Descriptor.OperationalHost)
// while discovery advertises it — some discovered address carries it —
// and empty otherwise: a host no longer advertised at all (a genuine
// re-registration under a new one) gives the parity order back, and the
// next session proves the new host.
//
// Ahead of matter.js and chip: compareDesirability ranks an address by
// class, health and DNS priority and knows no host
// (ServerAddressSet.ts:165-184 at db021da0); chip's lookup scores an
// address by class alone, every IPv6 link-local first, and keeps the
// DNS-SD interface only for link-local (src/lib/dnssd/IPAddressSorter.cpp:44-50,
// src/lib/address_resolve/AddressResolve_DefaultImpl.cpp:130-141 at
// connectedhomeip 6170af84). Neither tells two SRV targets of one instance
// apart. The session does: the node answered from an address one target
// owns (issue 478: a border router answered a Thread node's instance with
// an SRV to its own host, and its link-local address cost every reconnect
// DelayBeforeNextAddress).
func provenHost(host string, discovered []discovery.Address) string {
	if host == "" {
		return ""
	}
	for _, a := range discovered {
		if a.Host == host {
			return host
		}
	}
	return ""
}

// compareForHost orders two addresses for an attempt, negative when a is
// tried first: the proven host's before any other — a ranking, not a
// filter: the other targets stay candidates after it — then
// [discovery.CompareDesirability]. An empty host is the desirability
// order alone.
func compareForHost(host string, a, b discovery.Address) int {
	if host != "" {
		if ah, bh := a.Host == host, b.Host == host; ah != bh {
			if ah {
				return -1
			}
			return 1
		}
	}
	return discovery.CompareDesirability(a, b)
}

// orderForPeer is addrs — discovered, most desirable first — with the
// peer's proven host's addresses moved ahead of the others, each group in
// the order given: the order the address monitor walks the alternates in,
// the same as the connection's (shiftLocked). The proven host is learned
// first (learnHost).
func orderForPeer(p *Peer, addrs []discovery.Address) []discovery.Address {
	out := slices.Clone(addrs)
	p.learnHost(addrs)
	host := provenHost(p.operationalHost(), addrs)
	if host == "" {
		return out
	}
	slices.SortStableFunc(out, func(a, b discovery.Address) int {
		switch ah, bh := a.Host == host, b.Host == host; {
		case ah == bh:
			return 0
		case ah:
			return -1
		}
		return 1
	})
	return out
}

// provenHostAbsent reports whether the peer has a proven host
// (Descriptor.OperationalHost) that no discovered address carries.
func provenHostAbsent(known string, discovered []discovery.Address) bool {
	return known != "" && provenHost(known, discovered) == ""
}

// fallbackFirstLocked is the fallback when it ranks ahead of every other
// pending address — the proven host is known and no discovered address
// carries it (BD-Matter-ProvenOperationalHost, issue 488) — else nil.
func (c *connection) fallbackFirstLocked(known string, discovered []discovery.Address) *discovery.Address {
	if c.fallback == nil || !provenHostAbsent(known, discovered) {
		return nil
	}
	return c.fallback
}

// hostOwning is the SRV target of the discovered address a is — compared
// without the zone, which the session and discovery may spell
// differently — empty when none is.
func hostOwning(discovered []discovery.Address, a *net.UDPAddr) string {
	ap := addressOf(a).AddrPort()
	ap = netip.AddrPortFrom(ap.Addr().WithZone(""), ap.Port())
	for _, d := range discovered {
		if d.Host != "" && netip.AddrPortFrom(d.IP.WithZone(""), d.Port) == ap {
			return d.Host
		}
	}
	return ""
}

// expand is one variant of a per transport to attempt, or its UDP one
// without a transport list (PeerConnection.ts:323-336).
func (c *connection) expand(a discovery.Address) []target {
	transports := c.p.ResolveTransports(c.required, c.preferred)
	if transports == nil {
		return []target{{addr: a}}
	}
	out := make([]target, 0, len(transports))
	for _, tr := range transports {
		out = append(out, target{addr: a, tcp: tr == exchange.TransportTCP})
	}
	return out
}

// pendingAddLocked queues t once (the heap holds an item once,
// general/src/util/Heap.ts:119-138) and reports whether it did. A queued
// entry without a host — the fallback, from the operational address —
// takes the host discovery names for the same address, so the proven
// host ranks it (compareForHost); that counts as queued.
func (c *connection) pendingAddLocked(t target) bool {
	for i, x := range c.pending {
		if x.key() == t.key() {
			if x.addr.Host == "" && t.addr.Host != "" {
				c.pending[i].addr.Host = t.addr.Host
				return true
			}
			return false
		}
	}
	c.pending = append(c.pending, t)
	c.signal()
	return true
}

// preemptForProvenLocked starts a newly queued address of the proven host
// at once: when a carries the peer's operational host and no attempt at
// that host is in flight, the stagger is reset and the scheduler woken;
// shiftLocked ranks the address first, and its attempt sets the stagger
// again for the addresses after it. An attempt already running at another
// target continues. This module's (BD-Matter-ProvenOperationalHost): the
// service delivers its addresses one at a time in the desirability order,
// so the proven host's may arrive after another target's link-local
// address was attempted — a preemption, not a wait.
//
// An attempt queued without a host — the fallback, from the operational
// address — is at the proven host once discovery names the host for its
// address (hostOwning).
func (c *connection) preemptForProvenLocked(a discovery.Address) {
	host := c.p.operationalHost()
	if a.Host == "" || a.Host != host || c.lastAttemptAt.IsZero() {
		return
	}
	var discovered []discovery.Address
	for _, att := range c.attempts {
		if att.addr.Host == host {
			return
		}
		if att.addr.Host == "" {
			if discovered == nil {
				discovered = c.p.service.Addresses()
			}
			if hostOwning(discovered, att.addr.UDPAddr()) == host {
				return
			}
		}
	}
	c.lastAttemptAt = time.Time{}
	c.signal()
}

// pendingDeleteLocked unqueues every variant of a.
func (c *connection) pendingDeleteLocked(a discovery.Address) {
	kept := c.pending[:0]
	for _, x := range c.pending {
		if x.addr.AddrPort() != a.AddrPort() {
			kept = append(kept, x)
		}
	}
	if len(kept) != len(c.pending) {
		c.pending = kept
		c.signal()
	}
}

// retireStaleFallback drops the speculative fallback once discovery
// settled on addresses without it, so the discovered address starts
// without the stagger (PeerConnection.ts:264-303).
//
// It looks again when the service changes and when the process marks the
// fallback or ends a retention (refall): the process marks the fallback on
// its own goroutine after the service's change, so a look at the change
// alone could come before the mark and miss it — the retirement then
// depended on which goroutine ran first (issue 507's reproduction). The
// decision after the stabilization delay is taken under the process's lock
// with the retirement.
func (c *connection) retireStaleFallback() {
	changes := c.p.service.Changes(c.ctx)
	for c.ctx.Err() == nil {
		if !c.fallbackSuperseded() {
			select {
			case _, ok := <-changes:
				if !ok {
					return
				}
			case <-c.refall:
			case <-c.ctx.Done():
				return
			}
			continue
		}
		if !c.sleep(c.ctx, c.timing.AddressChangeStabilizationDelay, nil) {
			return
		}
		c.mu.Lock()
		if c.fallbackSupersededLocked() {
			stale := *c.fallback
			c.fallback = nil
			c.lastAttemptAt = time.Time{}
			c.deleteAddressLocked(stale, "aborting fallback: no longer advertised, switching to discovered address", true)
			c.signal()
		}
		c.mu.Unlock()
	}
}

// rewakeLocked wakes retireStaleFallback to look again.
func (c *connection) rewakeLocked() {
	select {
	case c.refall <- struct{}{}:
	default:
	}
}

// fallbackSuperseded is fallbackSupersededLocked under the process's lock.
func (c *connection) fallbackSuperseded() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.fallbackSupersededLocked()
}

// fallbackSupersededLocked: a fallback is attempted, addresses are known
// and none is the fallback (PeerConnection.ts:305-320). Ahead of
// matter.js, a fallback queued because the proven host is absent from the
// discovered addresses is not superseded by another target's
// (BD-Matter-ProvenOperationalHost): it stays until a discovered address
// carries the host.
//
// Nor is a retained fallback (retainLocked) before its bound. matter.js
// retires a superseded fallback after addressChangeStabilizationDelay
// (PeerConnection.ts:277-322 at db021da0); this module keeps it: another
// target's SRV answering for the node's instance is not evidence that the
// node's address is stale (issue 507: a border router's SRV to its own
// host), and the stagger still starts the discovered targets beside it,
// so nothing is stranded.
func (c *connection) fallbackSupersededLocked() bool {
	fb := c.fallback
	if fb == nil || c.retained[fb.AddrPort()] != nil {
		return false
	}
	addrs := c.p.service.Addresses()
	if len(addrs) == 0 || provenHostAbsent(c.p.operationalHost(), addrs) {
		return false
	}
	for _, a := range addrs {
		if a.AddrPort() == fb.AddrPort() {
			return false
		}
	}
	return true
}

// addAddressLocked queues a discovered address unless it is attempted; a
// fallback that discovery now names is a real address and kicks the
// process (PeerConnection.ts:338-358).
//
// The address advertised again ends its retention; an address of the
// proven host ends the retention of the host's others too: its fresh set
// replaces them, and a retained address it no longer carries is deleted as
// discovery's deletion would have deleted it (provenHostReturnedLocked).
func (c *connection) addAddressLocked(a discovery.Address) {
	ap := a.AddrPort()
	if a.Host != "" {
		c.hosts[ap] = a.Host
	}
	delete(c.spent, ap)
	c.unretainLocked(ap)
	c.provenHostReturnedLocked(a)
	for _, v := range c.expand(a) {
		if _, ok := c.attempts[v.key()]; ok {
			if c.fallback != nil && c.fallback.AddrPort() == a.AddrPort() {
				c.fallback = nil
				c.kickLocked(kickDiscover)
			}
			continue
		}
		if c.pendingAddLocked(v) {
			c.preemptForProvenLocked(v.addr)
		}
	}
}

// maybeAttemptFallbackLocked queues the descriptor's operational address
// when nothing else is attempted, pending or discovered
// (PeerConnection.ts:362-382 at db021da0).
//
// Ahead of matter.js (BD-Matter-ProvenOperationalHost), it is queued too
// when the peer's proven host is known but no discovered address carries
// it, and then ranks ahead of the other targets' addresses
// (fallbackFirstLocked): they follow the operational address's transport
// variants, each DelayBeforeNextAddress after the one before. Another
// responder's cache-flush SRV removes the node's own SRV from the cache
// until the node answers again (RFC 6762 §10.2); the session, not the
// momentary cache, says which target is the node (issue 488).
//
// Without a proven host the operational address is queued, too, while
// discovery advertises other addresses and not it (anchorLocked).
func (c *connection) maybeAttemptFallbackLocked() {
	if c.anchorLocked() || len(c.attempts) > 0 || len(c.pending) > 0 {
		return
	}
	if discovered := c.p.service.Addresses(); len(discovered) > 0 && !provenHostAbsent(c.p.operationalHost(), discovered) {
		return
	}
	op := c.p.Descriptor().OperationalAddress
	if op == nil {
		c.fallback = nil
		return
	}
	a := addressOf(op)
	c.fallback = &a
	c.rewakeLocked()
	for _, v := range c.expand(a) {
		c.pendingAddLocked(v)
	}
}

// retentionOn reports whether the process retains addresses
// (Timing.ProvenAddressRetention positive).
func (c *connection) retentionOn() bool { return c.timing.ProvenAddressRetention > 0 }

// queuedLocked reports whether a variant of a is attempted or pending.
func (c *connection) queuedLocked(a discovery.Address) bool {
	ap := a.AddrPort()
	for k := range c.attempts {
		if k.ap == ap {
			return true
		}
	}
	for _, x := range c.pending {
		if x.addr.AddrPort() == ap {
			return true
		}
	}
	return false
}

// withdrawnLocked handles discovery's deletion of a: the address is
// retained when retainLocked keeps it — marked as fallback where
// matter.js's deleteAddress would have kept it as one — and deleted
// otherwise (PeerConnection.ts:412-440).
func (c *connection) withdrawnLocked(a discovery.Address) {
	ap := a.AddrPort()
	a.Host = c.hosts[ap]
	delete(c.hosts, ap)
	if c.retainLocked(a, "no longer advertised") {
		if c.keptAsFallbackLocked(a, false) {
			c.fallback = &a
			c.rewakeLocked()
		}
		return
	}
	c.deleteAddressLocked(a, "aborting attempt because address is expired", false)
}

// retainLocked keeps an address discovery stopped advertising as a
// candidate for Timing.ProvenAddressRetention, measured on the set's
// clock from now, and reports whether it does. It keeps an attempted or
// pending address of the proven host (Descriptor.OperationalHost) while no
// discovered address carries the host, and — no host learned — the stored
// operational address while discovery does not carry it. A retained
// address ranks ahead of the other targets (shiftLocked), its attempt in
// flight goes on, and the fallback retirement passes it by
// (fallbackSupersededLocked). Its retention ends when the address is
// advertised again (addAddressLocked), when the proven host is advertised
// again without it — it is then deleted (provenHostReturnedLocked) — when
// the process ends (run), and at the bound, when it is deleted as
// discovery's deletion would have deleted it (expireLocked).
//
// Ahead of matter.js, whose deleteAddress keeps the operational address
// only while no other attempt remains (PeerConnection.ts:419-441 at
// db021da0), and of BD-Matter-ProvenOperationalHost, which ranks the
// proven host first only while discovery carries it: another responder's
// cache-flush SRV for the node's instance removes the node's own records
// for as long as it answers (issue 507: a border router answering a Thread
// node's instance with its own host, the SRV set flipping thousands of
// times a day), and with them the addresses the node answers at. The
// node's last addresses — all of them: in issue 507's log the stored
// address was stale and the node answered at another address of its host —
// stay candidates until the bound.
func (c *connection) retainLocked(a discovery.Address, reason string) bool {
	if !c.retentionOn() || !c.queuedLocked(a) {
		return false
	}
	ap := a.AddrPort()
	if c.retained[ap] != nil {
		return true
	}
	discovered := c.p.service.Addresses()
	for _, d := range discovered {
		if d.AddrPort() == ap {
			return false // advertised again already: the next addition follows
		}
	}
	switch host := c.p.operationalHost(); {
	case host != "":
		if a.Host != host || provenHost(host, discovered) != "" {
			return false
		}
	case c.spent[ap]:
		return false
	default:
		op := c.p.Descriptor().OperationalAddress
		if op == nil || addressOf(op).AddrPort() != ap {
			return false
		}
	}
	r := &retention{addr: a, since: c.p.set.clock.Now(), reason: reason}
	r.timer = c.p.set.clock.AfterFunc(c.timing.ProvenAddressRetention, func() { c.expire(ap, r) })
	c.retained[ap] = r
	c.p.log.Info("matter.controller.peer.address_retained", slog.String("address", a.String()), slog.String("host", a.Host), slog.String("reason", reason))
	return true
}

// unretainLocked ends the retention of the address at ap, if any, without
// deleting it.
func (c *connection) unretainLocked(ap netip.AddrPort) {
	r := c.retained[ap]
	if r == nil {
		return
	}
	r.timer.Stop()
	delete(c.retained, ap)
	c.rewakeLocked()
}

// provenHostReturnedLocked: the proven host is advertised again — a
// carries it and discovery carries it now — so its fresh set replaces its
// retained addresses: one the host still carries stays as it is, one it
// no longer carries is deleted as discovery's deletion would have deleted
// it.
func (c *connection) provenHostReturnedLocked(a discovery.Address) {
	host := c.p.operationalHost()
	if host == "" || a.Host != host || len(c.retained) == 0 {
		return
	}
	discovered := c.p.service.Addresses()
	if provenHost(host, discovered) == "" {
		return // gone again already: the next deletion follows
	}
	fresh := map[netip.AddrPort]bool{a.AddrPort(): true}
	for _, d := range discovered {
		if d.Host == host {
			fresh[d.AddrPort()] = true
		}
	}
	for ap, r := range c.retained {
		if r.addr.Host != host || fresh[ap] {
			continue
		}
		c.unretainLocked(ap)
		c.deleteAddressLocked(r.addr, "aborting attempt because the proven host is advertised without its address", false)
	}
}

// anchorLocked queues and retains the stored operational address of a peer
// without a proven host while discovery advertises other addresses and not
// it (retainLocked), and reports whether it did. With nothing discovered
// the operational address is matter.js's fallback instead
// (maybeAttemptFallbackLocked); once its retention expired it is not
// anchored again until discovery advertises it (spent).
func (c *connection) anchorLocked() bool {
	if !c.retentionOn() || c.p.operationalHost() != "" {
		return false
	}
	op := c.p.Descriptor().OperationalAddress
	if op == nil {
		return false
	}
	a := addressOf(op)
	ap := a.AddrPort()
	if c.spent[ap] {
		return false
	}
	if c.retained[ap] != nil {
		return true
	}
	discovered := c.p.service.Addresses()
	if len(discovered) == 0 {
		return false
	}
	for _, d := range discovered {
		if d.AddrPort() == ap {
			return false
		}
	}
	if !c.queuedLocked(a) {
		for _, v := range c.expand(a) {
			c.pendingAddLocked(v)
		}
	}
	return c.retainLocked(a, "operational address not advertised")
}

// expire ends the retention r of the address at ap at its bound: the
// address is deleted as discovery's deletion would have deleted it
// (deleteAddressLocked), and the fallback looked at again.
func (c *connection) expire(ap netip.AddrPort, r *retention) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.ctx.Err() != nil || c.retained[ap] != r {
		return
	}
	c.unretainLocked(ap)
	c.spent[ap] = true
	c.p.log.Info("matter.controller.peer.address_retention_expired",
		slog.String("address", r.addr.String()), slog.String("host", r.addr.Host),
		slog.String("reason", r.reason), slog.Duration("after", c.p.set.clock.Now().Sub(r.since)))
	c.deleteAddressLocked(r.addr, "aborting attempt because the retention of its address expired", false)
	c.maybeAttemptFallbackLocked()
}

// addressOf is an operational address as a discovered one.
func addressOf(u *net.UDPAddr) discovery.Address {
	ip, _ := netip.AddrFromSlice(u.IP)
	ip = ip.Unmap().WithZone(u.Zone)
	return discovery.Address{IP: ip, Port: uint16(u.Port)} //nolint:gosec // a UDP port
}

// initiateAttemptLocked starts the attempt at a unless one runs
// (PeerConnection.ts:381-410), with the part of connect that runs before
// the attempt is registered (:469-484): a non-fallback address while the
// fallback is attempted deletes the fallback — which keeps it as fallback
// when it is the only attempt, and stops marking it either way.
func (c *connection) initiateAttemptLocked(t target) {
	key := t.key()
	if _, ok := c.attempts[key]; ok {
		return
	}
	ctx, cancel := context.WithCancel(c.ctx)
	c.lastAttemptAt = c.p.set.clock.Now()
	c.addrsAttempted++
	att := &attempt{addr: t.addr, tcp: t.tcp, addrNo: c.addrsAttempted, ctx: ctx, cancel: cancel}
	if c.fallback != nil && c.fallback.AddrPort() != key.ap {
		// A retained fallback is not deleted: its attempt goes on beside
		// this one (retainLocked).
		if c.retained[c.fallback.AddrPort()] == nil {
			c.deleteAddressLocked(*c.fallback, "aborting attempt to last known address because device reports address change", false)
		}
		c.fallback = nil
	}
	c.attempts[key] = att
	c.goWork(func() {
		defer cancel()
		c.connect(att)
		c.mu.Lock()
		if c.attempts[key] == att {
			delete(c.attempts, key)
			c.maybeAttemptFallbackLocked()
		}
		c.mu.Unlock()
	})
}

// deleteAddressLocked ends the attempt at a and unqueues it; the
// operational address stays as fallback when it is the last attempt and
// the deletion is not forced (PeerConnection.ts:412-440).
func (c *connection) deleteAddressLocked(a discovery.Address, why string, force bool) {
	ap := a.AddrPort()
	if c.keptAsFallbackLocked(a, force) {
		c.fallback = &a
		c.rewakeLocked()
		return
	}
	c.unretainLocked(ap)
	for k, att := range c.attempts {
		if k.ap != ap {
			continue
		}
		c.p.log.Debug("matter.controller.peer.attempt_aborted", slog.String("address", a.String()), slog.Bool("tcp", k.tcp), slog.String("reason", why))
		att.cancel()
		delete(c.attempts, k)
	}
	c.pendingDeleteLocked(a)
}

// keptAsFallbackLocked is the keep-alive guard of deleteAddressLocked: an
// unforced deletion of the operational address while no attempt at another
// address remains keeps it as fallback (PeerConnection.ts:419-441).
func (c *connection) keptAsFallbackLocked(a discovery.Address, force bool) bool {
	ap := a.AddrPort()
	op := c.p.Descriptor().OperationalAddress
	if force || op == nil || addressOf(op).AddrPort() != ap {
		return false
	}
	for k := range c.attempts {
		if k.ap != ap {
			return false
		}
	}
	return true
}

// kick emits a kick through the throttle (Peer.ts:641-648: at most one
// per KickThrottleInterval, the others dropped).
func (c *connection) kick(origin kickOrigin) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.kickLocked(origin)
}

func (c *connection) kickLocked(origin kickOrigin) {
	now := c.p.set.clock.Now()
	if !c.lastKickAt.IsZero() && !c.lastKickAt.Add(c.timing.KickThrottleInterval).Before(now) {
		return
	}
	c.lastKickAt = now
	for _, h := range c.kickHandlers {
		h(origin)
	}
}

// connect attempts the address until the attempt is aborted, the Sigma1
// messages of every try on the address's one [initialContactSchedule]
// (PeerConnection.ts:471-497 at db021da0).
func (c *connection) connect(att *attempt) {
	p := c.p
	schedule := newInitialContactSchedule(c.timing, func() {
		p.goWork(func() {
			if h := p.set.ctx.EstablishmentUnresponsive; h != nil {
				h(p)
			}
			for _, o := range p.observing() {
				if o.EstablishmentUnresponsive != nil {
					o.EstablishmentUnresponsive()
				}
			}
		})
	})
	for n := 1; att.ctx.Err() == nil; n++ {
		if err := c.attemptOnce(att, schedule, n); err != nil {
			c.handleConnectionError(err, att)
		}
	}
}

// attemptOnce makes one CASE attempt at the address
// (PeerConnection.ts:514-673 at db021da0): a fresh unsecured session with
// the peer's session parameters, CASE with the Sigma1 send options of the
// schedule's next attempt, and the exchange's notifiers of createExchange
// (:847-878) with the attempt observing them. A kick that restarts the
// attempt or an abort returns nil, and so does a Sigma1 the peer never
// acknowledged: the schedule moves on to a fresh one at once, without the
// error handling (:639-653).
func (c *connection) attemptOnce(att *attempt, schedule *initialContactSchedule, attemptNo int) error {
	p := c.p
	// The previous try's exchange is not this try's: until the handshake
	// creates the new one the kick sees none. matter.js registers the kick
	// only after createExchange (PeerConnection.ts:544, :579 at db021da0),
	// where a fresh exchange reports no restart saving; measured on the
	// closed exchange of the try before, a refresh attempt would pass the
	// blocker and be restarted before it sent anything.
	att.exchange.Store(nil)
	if p.set.isDisconnected() {
		return ErrNotConnectable
	}
	fabric, ok := p.set.ctx.Fabrics(p.address)
	if !ok {
		return fmt.Errorf("%w with index %d", ErrNoFabric, p.address.FabricIndex)
	}
	if !c.waitOutBusy(att) {
		return nil
	}
	c.mu.Lock()
	markedFallback := c.fallback != nil && c.fallback.AddrPort() == att.addr.AddrPort()
	retained := c.retained[att.addr.AddrPort()] != nil
	c.mu.Unlock()

	a, err := schedule.begin(c.isDelayedFallback(att))
	if err != nil {
		return err
	}
	defer a.dispose()
	local, restart := context.WithCancel(att.ctx)
	defer restart()
	margin := c.margin
	notifiers := exchange.Options{
		PeerAdditionalMrpDelay: &margin,
		OnSend: func(n int) {
			if n > 0 {
				// Retransmitting: let discovery look for a fresh address.
				p.status.SetReachable(false)
			}
			a.onSend(n)
		},
		OnReceive: func(*exchange.Message, bool) {
			p.status.SetLastReceiptAt(p.set.clock.Now())
			a.onReceive()
		},
	}
	c.mu.Lock()
	c.kickHandlers[att] = func(origin kickOrigin) { c.onKickLocked(origin, att, a, restart) }
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		delete(c.kickHandlers, att)
		c.mu.Unlock()
	}()

	// A refreshing attempt logs at debug (PeerConnection.ts:549-550).
	logConnecting := p.log.Info
	if a.isRefresh {
		logConnecting = p.log.Debug
	}
	logConnecting("matter.controller.peer.connecting",
		slog.String("address", att.addr.String()), slog.Int("address_index", att.addrNo), slog.Int("attempt", attemptNo),
		slog.Bool("fallback", markedFallback), slog.Bool("retained", retained))
	params := p.SessionParameters()
	opts := handshake.CASEOptions{
		PeerParameters:                 &params,
		MaxInitialRetransmissions:      a.maxTransmissions,
		MaxInitialRetransmissionTime:   a.maxRetransmissionTime,
		InitialRetransmissionTime:      a.initialRetransmissionTime,
		InitialRetransmissionTimeLimit: a.retransmissionTimeLimit,
		InitialBackoffOffset:           a.backoffOffset,
		Exchange:                       notifiers,
		OnExchange:                     att.exchange.Store,
		Logger:                         p.log,
	}
	if r := p.set.ctx.Resumption; r != nil {
		opts.Resumption = r
	}
	addr, ch, err := c.attemptAddress(local, att)
	if err != nil || addr == nil {
		return err
	}
	s, _, err := handshake.CASE(local, p.set.ctx.Exchanges, addr, fabric, p.address.NodeID, opts)
	if err == nil && att.tcp && p.tcpDenied(s.ReportedParameters()) {
		// The session-parameter validation of a TCP attempt
		// (PeerConnection.ts:596-621), after the handshake: the peer is
		// marked and the session dropped.
		p.MarkTCPUnsupported()
		s.Close()
		return ErrTCPUnsupported
	}
	if err == nil {
		c.adopt(s)
		return nil
	}
	if ch != nil {
		// No session took the connection over (PeerConnection.ts:514-516,
		// :640-650).
		_ = ch.Close()
	}
	if local.Err() != nil {
		return nil
	}
	return c.afterFailure(att, a, err)
}

// waitOutBusy holds the attempt's next Sigma1 until the peer's Busy
// deadline passed (Peer.busyRemaining); false when the attempt ended
// first. The deadline may move while it waits — another address of the
// peer answered Busy again — so it looks again after each wait.
func (c *connection) waitOutBusy(att *attempt) bool {
	for {
		d := c.p.busyRemaining()
		if d <= 0 {
			return true
		}
		c.p.log.Debug("matter.controller.peer.busy_wait", slog.String("address", att.addr.String()), slog.Duration("delay", d))
		if !c.sleep(att.ctx, d, nil) {
			return false
		}
	}
}

// adopt takes the session the attempt established and ends the process.
func (c *connection) adopt(s *exchange.Session) {
	p := c.p
	c.mu.Lock()
	c.session = s
	c.mu.Unlock()
	p.set.initiated.Store(s, struct{}{})
	go func() { <-s.Closing(); p.set.initiated.Delete(s) }()
	p.set.Adopt(s)
	c.cancel()
}

// afterFailure ends the attempt a with its handshake's err: an
// unacknowledged Sigma1 is the schedule moving on, not a failure, and
// retries at once without the error handling (nil); anything else is
// returned for it (PeerConnection.ts:639-653 at db021da0).
func (c *connection) afterFailure(att *attempt, a *contactAttempt, err error) error {
	log := c.p.log.With(slog.String("address", att.addr.String()))
	switch a.fail(err) {
	case contactSwitched:
		log.Info("matter.controller.peer.sigma1_refresh", slog.Any("err", err))
		// The node never acknowledged: the next address starts at once
		// (advanceAfterFailure; refreshes after this do not advance again).
		c.advanceAfterFailure()
		return nil
	case contactRefreshed:
		log.Debug("matter.controller.peer.sigma1_refresh_again")
		return nil
	case contactRestarted:
		return nil
	}
	return err
}

// isDelayedFallback reports whether an attempt at att starts at the
// maximum retry delay: the fallback address once it is not the first
// address tried (PeerConnection.ts:499-512 at db021da0).
func (c *connection) isDelayedFallback(att *attempt) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.fallback != nil && c.fallback.AddrPort() == att.addr.AddrPort() && c.addrsAttempted > 1
}

// attemptAddress is the address an attempt's handshake runs on: the
// socket's, or — for a TCP attempt — the address the manager names the
// connection by that the attempt opens first, which binds the handshake's
// sessions to it (PeerSet.ts:274-304). A nil address without an error is
// a dial ctx ended.
func (c *connection) attemptAddress(ctx context.Context, att *attempt) (*net.UDPAddr, exchange.Channel, error) {
	addr := att.addr.UDPAddr()
	if !att.tcp {
		return addr, nil, nil
	}
	dial := c.p.set.ctx.DialTCP
	if dial == nil {
		return nil, nil, ErrNoTCP
	}
	ch, err := dial(ctx, addr)
	if err != nil {
		if ctx.Err() != nil {
			return nil, nil, nil //nolint:nilerr // a dial the attempt's end cut short ends the attempt, no error
		}
		return nil, nil, fmt.Errorf("%w: %w", exchange.ErrNetwork, err)
	}
	return c.p.set.ctx.Exchanges.AttachChannel(ch), ch, nil
}

// onKickLocked is the kick handler of one attempt (PeerConnection.ts:579-605
// at db021da0): it restarts the attempt's exchange when the schedule's
// attempt has no blocker — Sigma1 retransmitted enough across the
// schedule's attempts, the peer not answered yet, a restart saving
// enough — and the last restart of the process is older than the origin's
// cooldown. The thresholds are the connection's timing.
func (c *connection) onKickLocked(origin kickOrigin, att *attempt, a *contactAttempt, restart context.CancelFunc) {
	x := att.exchange.Load()
	if x == nil {
		return
	}
	log := c.p.log.With(slog.String("address", att.addr.String()), slog.String("origin", origin.String()))
	delayed := c.fallback != nil && c.fallback.AddrPort() == att.addr.AddrPort() && c.addrsAttempted > 1
	if why := a.kickBlocker(x.RetransmissionRestartSaving(), delayed); why != "" {
		if why == kickBlockedSaving {
			log.Debug("matter.controller.peer.kick_suppressed", slog.String("reason", "restart would save too little time"))
		}
		return
	}
	if c.restartAllowedLocked(origin) {
		log.Info("matter.controller.peer.kick_restart")
		a.restart()
		restart()
		return
	}
	log.Debug("matter.controller.peer.kick_suppressed", slog.Duration("since_last_restart", c.p.set.clock.Now().Sub(c.lastRestartAt)))
}

// restartAllowedLocked is the per-origin restart cooldown of the kick
// handler, shared by every attempt of the process: true, and the restart
// recorded, when no restart happened yet or the last one is at least the
// origin's cooldown old, on the connection's timing. Mirrors matter.js
// packages/protocol/src/peer/PeerConnection.ts:588-597 at db021da0.
func (c *connection) restartAllowedLocked(origin kickOrigin) bool {
	threshold := c.timing.KickRestartCooldown.Connect
	if origin == kickDiscover {
		threshold = c.timing.KickRestartCooldown.AddressChange
	}
	now := c.p.set.clock.Now()
	if c.lastRestartAt.IsZero() || now.Sub(c.lastRestartAt) >= threshold {
		c.lastRestartAt = now
		return true
	}
	return false
}

// handleConnectionError picks the pause after a failed attempt and sleeps
// it (PeerConnection.ts:653-752): a Busy status
// waits the peer's delay plus up to DelayAfterNetworkError of jitter — and
// holds every attempt of the peer for as long (Peer.holdBusy); a
// NoSharedTrustRoots status whose resumption record could be deleted
// retries at once; any other status waits DelayAfterPeerError; a transient
// error DelayAfterNetworkError; anything else DelayAfterUnhandledError. A
// network error marks the service unreachable. The error handler is
// consulted when a delay was chosen; it may replace it or end the process.
func (c *connection) handleConnectionError(err error, att *attempt) {
	p := c.p
	if errors.Is(err, ErrTCPUnsupported) {
		// The peer accepted TCP but denies it (PeerConnection.ts:656-672):
		// with TCP required that is fatal; otherwise the process ends, the
		// peer is flagged, and Connect reconnects over UDP.
		if c.required != nil && *c.required == exchange.TransportTCP {
			p.log.Error("matter.controller.peer.tcp_required_unsupported", slog.String("address", att.addr.String()))
			c.mu.Lock()
			c.fatal = err
			c.mu.Unlock()
		} else {
			p.log.Info("matter.controller.peer.tcp_unsupported_fallback", slog.String("address", att.addr.String()))
		}
		c.cancel()
		return
	}
	delay, hasDelay, category := classifyError(err, c.timing, p.set.rand, c.deleteResumptionRecord)
	if category == "busy" {
		p.holdBusy(delay)
	}
	if flagsUnreachable(err) {
		p.status.SetReachable(false)
	}
	if advancesOnFailure(err, category) {
		c.advanceAfterFailure()
	}
	log := p.log.With(slog.String("address", att.addr.String()))
	if hasDelay && c.handleError != nil {
		d, replace, fatal := c.handleError(err)
		if fatal != nil {
			log.Error("matter.controller.peer.fatal", slog.Any("err", fatal))
			c.mu.Lock()
			c.fatal = fatal
			c.mu.Unlock()
			c.cancel()
			return
		}
		if replace {
			delay = d
		}
	}
	switch category {
	case "busy":
		log.Info("matter.controller.peer.busy", slog.Duration("retry_in", delay))
	case "resumption":
		log.Warn("matter.controller.peer.resumption_rejected")
	default:
		log.Warn("matter.controller.peer.connect_error", slog.String("category", category), slog.Duration("retry_in", delay), slog.Any("err", err))
	}
	if att.ctx.Err() != nil || delay <= 0 {
		return
	}
	c.sleep(att.ctx, delay, nil)
}

// advancesOnFailure reports whether a failed attempt starts the next
// address at once: a timeout or another transient error (the
// "network" category), or a failure of the socket to send — the node was
// not reached. A status report means it was, and keeps the stagger.
func advancesOnFailure(err error, category string) bool {
	return category == "network" || errors.Is(err, exchange.ErrNetwork)
}

// advanceAfterFailure starts the next address at once: the delay is
// reset and the scheduler woken — also when nothing is pending yet, so an
// address queued after the failure starts as soon as it arrives, as
// connectedhomeip's ResolvingAddress state takes the next result the
// resolver delivers. DelayBeforeNextAddress stays the stagger between
// attempts still in flight. Besides the errors of advancesOnFailure, an
// unacknowledged Sigma1 whose contact schedule switches to fresh ones
// (afterFailure, contactSwitched) advances: in connectedhomeip that is
// the CHIP_ERROR_TIMEOUT the exchange's response timer reports once the
// retransmissions are exhausted (src/messaging/ReliableMessageMgr.cpp:186-205,
// src/protocols/secure_channel/CASESession.cpp:575-584 at connectedhomeip
// 6170af84).
//
// A deviation from matter.js, which starts the next address
// delayBeforeNextAddress after the previous attempt started, whatever
// became of it: its scheduler waits out the rest of the delay
// (PeerConnection.ts:236-256 at db021da0), and lastAttemptAt is reset only
// when the fallback is retired (:300-303). connectedhomeip moves to the
// next resolved address at once when CASE fails with a timeout or busy
// (OperationalSessionSetup::OnSessionEstablishmentError → TryNextResult,
// src/app/OperationalSessionSetup.cpp:457-480 at connectedhomeip
// 6170af84). Reason: a dead first candidate — issue 478, a border
// router's address published for a Thread node — otherwise costs every
// reconnect the full stagger.
func (c *connection) advanceAfterFailure() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lastAttemptAt = time.Time{}
	c.signal()
}

// flagsUnreachable is the rediscovery gate of handleConnectionError: a
// failure of the socket to send (ENETUNREACH, EHOSTUNREACH), also as the
// cause of another error, may mean the address no longer routes, so the
// service is marked unreachable for DNS-SD to look again; a transient
// peer error is left to the MRP path. Mirrors matter.js
// packages/protocol/src/peer/PeerConnection.ts:698-702.
func flagsUnreachable(err error) bool { return errors.Is(err, exchange.ErrNetwork) }

// classifyError is the delay table of handleConnectionError
// (PeerConnection.ts:674-696): the pause, whether there is one, and the
// category it logs under. deleteRecord runs only for NoSharedTrustRoots.
func classifyError(err error, timing Timing, rand func() float64, deleteRecord func() bool) (delay time.Duration, hasDelay bool, category string) {
	var sre *exchange.StatusReportError
	switch {
	case errors.As(err, &sre):
		r := sre.Report
		switch {
		case r.GeneralCode == generalStatusBusy && r.ProtocolCode == secureChannelStatusBusy && len(r.ProtocolData) == 2:
			// busyDelay (SecureChannelMessenger.ts:44-50) plus
			// Math.round(Math.random() * delayAfterNetworkError) (:679).
			busy := time.Duration(binary.LittleEndian.Uint16(r.ProtocolData)) * time.Millisecond
			jitter := time.Duration(math.Round(rand()*float64(timing.DelayAfterNetworkError.Milliseconds()))) * time.Millisecond
			return busy + jitter, true, "busy"
		case r.ProtocolCode == mrp.SCStatusProtocolNoSharedTrustRoots && deleteRecord():
			return 0, false, "resumption"
		default:
			return timing.DelayAfterPeerError, true, "peer"
		}
	case errors.Is(err, exchange.ErrTransient):
		return timing.DelayAfterNetworkError, true, "network"
	default:
		return timing.DelayAfterUnhandledError, true, "general"
	}
}

// deleteResumptionRecord forgets the peer's resumption record and reports
// whether there was one (SessionManager.ts:468-480).
func (c *connection) deleteResumptionRecord() bool {
	r := c.p.set.ctx.Resumption
	if r == nil {
		return false
	}
	ok, err := r.DeleteResumptionRecord(c.p.address)
	if err != nil {
		c.p.log.Error("matter.controller.peer.resumption_delete_failed", slog.Any("err", err))
	}
	return ok
}

// SPDX-License-Identifier: MIT
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
	"sync"
	"sync/atomic"
	"time"

	"github.com/SukramJ/go-fabric/controller/discovery"
	"github.com/SukramJ/go-fabric/controller/exchange"
	"github.com/SukramJ/go-fabric/controller/handshake"
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
}

// attempt is the connection attempt to one address (PeerConnection.ts:863-866).
type attempt struct {
	addr   discovery.Address
	addrNo int
	ctx    context.Context
	cancel context.CancelFunc
	// exchange is the CASE exchange of the current try, which the kick
	// reads.
	exchange atomic.Pointer[exchange.Exchange]
}

// connection is one run of the connection process of a peer — matter.js
// PeerConnection (packages/protocol/src/peer/PeerConnection.ts:93-753 at
// 0ceb202d) for UDP. Its state (:129-177) is guarded by mu.
type connection struct {
	p           *Peer
	timing      Timing
	setTiming   Timing
	handleError ErrorHandler
	ctx         context.Context
	cancel      context.CancelFunc
	done        chan struct{}
	workers     sync.WaitGroup
	wake        chan struct{}

	mu             sync.Mutex
	session        *exchange.Session
	fatal          error
	attempts       map[netip.AddrPort]*attempt
	pending        []discovery.Address
	fallback       *discovery.Address
	lastAttemptAt  time.Time
	addrsAttempted int
	lastRestartAt  time.Time
	lastKickAt     time.Time
	kickHandlers   map[*attempt]func(kickOrigin)
}

// initiateConnection starts the peer's connection process unless one
// runs, which it kicks when opts asks (Peer.ts:628-673). It returns the
// running process, nil on a closed peer.
func (p *Peer) initiateConnection(opts ConnectOptions) *connection {
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
	setTiming := p.set.Timing()
	timing := setTiming
	if opts.Timing != nil {
		timing = Merge(setTiming, *opts.Timing)
	}
	handleError := opts.HandleError
	if handleError == nil {
		handleError = p.set.errorHandler()
	}
	ctx, cancel := context.WithCancel(p.ctx)
	c := &connection{
		p:            p,
		timing:       timing,
		setTiming:    setTiming,
		handleError:  handleError,
		ctx:          ctx,
		cancel:       cancel,
		done:         make(chan struct{}),
		wake:         make(chan struct{}, 1),
		attempts:     make(map[netip.AddrPort]*attempt),
		kickHandlers: make(map[*attempt]func(kickOrigin)),
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
// peer [ErrClosed]. Mirrors matter.js Peer.connect (Peer.ts:414-491)
// without the group and transport branches.
func (p *Peer) Connect(ctx context.Context, opts ConnectOptions) (*exchange.Session, error) {
	for {
		if s := p.NewestSession(); s != nil {
			return s, nil
		}
		// The process is shared and outlives this call: it runs on the
		// peer's context, not ctx (Peer.ts:636, an Abort of the peer's own).
		c := p.initiateConnection(opts) //nolint:contextcheck // see above
		if c == nil {
			return nil, ErrClosed
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

// run is the body of PeerConnection (:123-215).
func (c *connection) run() {
	p := c.p
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
		case discovery.AddressDeleted:
			c.deleteAddressLocked(change.Address, "aborting attempt because address is expired", false)
			c.maybeAttemptFallbackLocked()
		}
		c.mu.Unlock()
	}

	c.mu.Lock()
	connected := c.session != nil
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
		c.initiateAttemptLocked(c.shiftLocked())
		c.mu.Unlock()
	}
}

// shiftLocked takes the most desirable pending address — the heap of
// :145-152 ordered by discovery.CompareDesirability, the first of equals.
func (c *connection) shiftLocked() discovery.Address {
	best := 0
	for i := 1; i < len(c.pending); i++ {
		if discovery.CompareDesirability(c.pending[i], c.pending[best]) < 0 {
			best = i
		}
	}
	a := c.pending[best]
	c.pending = append(c.pending[:best], c.pending[best+1:]...)
	return a
}

// pendingAddLocked queues a once (the heap holds an item once,
// general/src/util/Heap.ts:119-138).
func (c *connection) pendingAddLocked(a discovery.Address) {
	for _, x := range c.pending {
		if x.AddrPort() == a.AddrPort() {
			return
		}
	}
	c.pending = append(c.pending, a)
	c.signal()
}

func (c *connection) pendingDeleteLocked(a discovery.Address) {
	for i, x := range c.pending {
		if x.AddrPort() == a.AddrPort() {
			c.pending = append(c.pending[:i], c.pending[i+1:]...)
			c.signal()
			return
		}
	}
}

// retireStaleFallback drops the speculative fallback once discovery
// settled on addresses without it, so the discovered address starts
// without the stagger (PeerConnection.ts:264-303).
func (c *connection) retireStaleFallback() {
	changes := c.p.service.Changes(c.ctx)
	for c.ctx.Err() == nil {
		if !c.fallbackSuperseded() {
			select {
			case _, ok := <-changes:
				if !ok {
					return
				}
			case <-c.ctx.Done():
				return
			}
			continue
		}
		if !c.sleep(c.ctx, c.timing.AddressChangeStabilizationDelay, nil) {
			return
		}
		if !c.fallbackSuperseded() {
			continue
		}
		c.mu.Lock()
		if c.fallback != nil {
			stale := *c.fallback
			c.fallback = nil
			c.lastAttemptAt = time.Time{}
			c.deleteAddressLocked(stale, "aborting fallback: no longer advertised, switching to discovered address", true)
			c.signal()
		}
		c.mu.Unlock()
	}
}

// fallbackSuperseded: a fallback is attempted, addresses are known and
// none is the fallback (PeerConnection.ts:305-320).
func (c *connection) fallbackSuperseded() bool {
	c.mu.Lock()
	fb := c.fallback
	c.mu.Unlock()
	if fb == nil {
		return false
	}
	addrs := c.p.service.Addresses()
	if len(addrs) == 0 {
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
func (c *connection) addAddressLocked(a discovery.Address) {
	if _, ok := c.attempts[a.AddrPort()]; ok {
		if c.fallback != nil && c.fallback.AddrPort() == a.AddrPort() {
			c.fallback = nil
			c.kickLocked(kickDiscover)
		}
		return
	}
	c.pendingAddLocked(a)
}

// maybeAttemptFallbackLocked queues the descriptor's operational address
// when nothing else is attempted, pending or discovered
// (PeerConnection.ts:360-379).
func (c *connection) maybeAttemptFallbackLocked() {
	if len(c.attempts) > 0 || len(c.pending) > 0 || len(c.p.service.Addresses()) > 0 {
		return
	}
	op := c.p.Descriptor().OperationalAddress
	if op == nil {
		c.fallback = nil
		return
	}
	a := addressOf(op)
	c.fallback = &a
	c.pendingAddLocked(a)
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
func (c *connection) initiateAttemptLocked(a discovery.Address) {
	key := a.AddrPort()
	if _, ok := c.attempts[key]; ok {
		return
	}
	ctx, cancel := context.WithCancel(c.ctx)
	c.lastAttemptAt = c.p.set.clock.Now()
	c.addrsAttempted++
	att := &attempt{addr: a, addrNo: c.addrsAttempted, ctx: ctx, cancel: cancel}
	if c.fallback != nil && c.fallback.AddrPort() != key {
		c.deleteAddressLocked(*c.fallback, "aborting attempt to last known address because device reports address change", false)
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
	key := a.AddrPort()
	op := c.p.Descriptor().OperationalAddress
	isOperational := op != nil && addressOf(op).AddrPort() == key
	remaining := len(c.attempts)
	if _, ok := c.attempts[key]; ok {
		remaining--
	}
	if !force && isOperational && remaining == 0 {
		c.fallback = &a
		return
	}
	if att, ok := c.attempts[key]; ok {
		c.p.log.Debug("matter.controller.peer.attempt_aborted", slog.String("address", a.String()), slog.String("why", why))
		att.cancel()
		delete(c.attempts, key)
	}
	c.pendingDeleteLocked(a)
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

// connect attempts the address until the attempt is aborted
// (PeerConnection.ts:466-493).
func (c *connection) connect(att *attempt) {
	for n := 1; att.ctx.Err() == nil; n++ {
		if err := c.attemptOnce(att, n); err != nil {
			c.handleConnectionError(err, att)
		}
	}
}

// unresponsiveDetector fires once when the retransmission count passes
// MRP's transmission budget and re-arms at 0 — matter.js
// PeerConnection.establishmentUnresponsiveDetector (PeerConnection.ts:810-827).
func unresponsiveDetector(fire func()) func(int) {
	var signaled atomic.Bool
	return func(n int) {
		if n == 0 {
			signaled.Store(false)
		} else if n > exchange.MaxTransmissions && signaled.CompareAndSwap(false, true) {
			fire()
		}
	}
}

// attemptOnce makes one CASE attempt at the address
// (PeerConnection.ts:495-651): a fresh unsecured session with the peer's
// session parameters, CASE with the descriptor's CATs and unlimited first
// retransmissions capped at MaxDelayBetweenInitialContactRetries — which
// is also added to every backoff when this is the fallback and not the
// first address — and the exchange's notifiers of createExchange
// (:829-860). A kick that restarts the attempt or an abort returns nil.
func (c *connection) attemptOnce(att *attempt, attemptNo int) error {
	p := c.p
	if p.set.isDisconnected() {
		return ErrNotConnectable
	}
	fabric, ok := p.set.ctx.Fabrics(p.address)
	if !ok {
		return fmt.Errorf("peer: no fabric with index %d", p.address.FabricIndex)
	}
	c.mu.Lock()
	isFallback := c.fallback != nil && c.addrsAttempted > 1
	markedFallback := c.fallback != nil && c.fallback.AddrPort() == att.addr.AddrPort()
	c.mu.Unlock()

	local, restart := context.WithCancel(att.ctx)
	defer restart()
	unresponsive := unresponsiveDetector(func() {
		if h := p.set.ctx.EstablishmentUnresponsive; h != nil {
			p.goWork(func() { h(p) })
		}
	})
	notifiers := exchange.Options{
		OnSend: func(n int) {
			if n > 0 {
				// Retransmitting: let discovery look for a fresh address.
				p.status.SetReachable(false)
			}
			unresponsive(n)
		},
		OnReceive: func() { p.status.SetLastReceiptAt(p.set.clock.Now()) },
	}
	c.mu.Lock()
	c.kickHandlers[att] = func(origin kickOrigin) { c.onKickLocked(origin, att, restart) }
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		delete(c.kickHandlers, att)
		c.mu.Unlock()
	}()

	p.log.Info("matter.controller.peer.connecting",
		slog.String("address", att.addr.String()), slog.Int("addr", att.addrNo), slog.Int("attempt", attemptNo),
		slog.Bool("fallback", markedFallback))
	params := p.SessionParameters()
	opts := handshake.CASEOptions{
		PeerParameters:               &params,
		CaseAuthenticatedTags:        p.Descriptor().CaseAuthenticatedTags,
		MaxInitialRetransmissions:    -1,
		MaxInitialRetransmissionTime: c.timing.MaxDelayBetweenInitialContactRetries,
		Exchange:                     notifiers,
		OnExchange:                   att.exchange.Store,
		Logger:                       p.log,
	}
	if isFallback {
		opts.InitialRetransmissionTime = c.timing.MaxDelayBetweenInitialContactRetries
	}
	if r := p.set.ctx.Resumption; r != nil {
		opts.Resumption = r
	}
	s, _, err := handshake.CASE(local, p.set.ctx.Exchanges, att.addr.UDPAddr(), fabric, p.address.NodeID, opts)
	if err == nil {
		c.mu.Lock()
		c.session = s
		c.mu.Unlock()
		p.set.Adopt(s)
		c.cancel()
		return nil
	}
	if local.Err() != nil {
		return nil
	}
	return err
}

// onKickLocked is the kick handler of one attempt (PeerConnection.ts:568-594):
// it restarts the attempt's exchange when it has retransmitted enough, a
// restart would save enough, and the last restart of the process is older
// than the origin's cooldown. The thresholds are the set's timing.
func (c *connection) onKickLocked(origin kickOrigin, att *attempt, restart context.CancelFunc) {
	x := att.exchange.Load()
	if x == nil || x.RetransmissionCount() < c.setTiming.KickMinRetransmissions {
		return
	}
	log := c.p.log.With(slog.String("address", att.addr.String()), slog.String("origin", origin.String()))
	if x.RetransmissionRestartSaving() < c.setTiming.KickMinRestartSaving {
		log.Debug("matter.controller.peer.kick_suppressed", slog.String("why", "restart would save too little time"))
		return
	}
	threshold := c.setTiming.KickRestartCooldown.Connect
	if origin == kickDiscover {
		threshold = c.setTiming.KickRestartCooldown.AddressChange
	}
	now := c.p.set.clock.Now()
	if c.lastRestartAt.IsZero() || now.Sub(c.lastRestartAt) >= threshold {
		log.Info("matter.controller.peer.kick_restart")
		c.lastRestartAt = now
		restart()
		return
	}
	log.Debug("matter.controller.peer.kick_suppressed", slog.Duration("since_last_restart", now.Sub(c.lastRestartAt)))
}

// handleConnectionError picks the pause after a failed attempt and sleeps
// it (PeerConnection.ts:653-752, the TCP branch aside): a Busy status
// waits the peer's delay plus up to DelayAfterNetworkError of jitter; a
// NoSharedTrustRoots status whose resumption record could be deleted
// retries at once; any other status waits DelayAfterPeerError; a transient
// error DelayAfterNetworkError; anything else DelayAfterUnhandledError. A
// network error marks the service unreachable. The error handler is
// consulted when a delay was chosen; it may replace it or end the process.
func (c *connection) handleConnectionError(err error, att *attempt) {
	p := c.p
	delay, hasDelay, category := classifyError(err, c.timing, p.set.rand, c.deleteResumptionRecord)
	if errors.Is(err, exchange.ErrNetwork) {
		p.status.SetReachable(false)
	}
	log := p.log.With(slog.String("address", att.addr.String()))
	if hasDelay && c.handleError != nil {
		d, replace, fatal := c.handleError(err)
		if fatal != nil {
			log.Error("matter.controller.peer.fatal", slog.String("err", fatal.Error()))
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
		log.Warn("matter.controller.peer.connect_error", slog.String("category", category), slog.Duration("retry_in", delay), slog.String("err", err.Error()))
	}
	if att.ctx.Err() != nil || delay <= 0 {
		return
	}
	c.sleep(att.ctx, delay, nil)
}

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
		c.p.log.Error("matter.controller.peer.resumption_delete_failed", slog.String("err", err.Error()))
	}
	return ok
}

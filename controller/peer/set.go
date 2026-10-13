// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package peer

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"slices"
	"sync"
	"time"

	"github.com/SukramJ/go-fabric/controller/discovery"
	"github.com/SukramJ/go-fabric/controller/exchange"
	"github.com/SukramJ/go-fabric/controller/handshake"
)

// ErrorHandler is consulted when a connection attempt failed with an error
// after which the process would wait and retry: it may replace the delay
// (replace true) or end the connection process with a fatal error (a
// non-nil fatal). It is matter.js PeerConnection.Context.handleError
// (PeerConnection.ts:768-775), whose "return a Duration / return nothing /
// throw" becomes the three results.
type ErrorHandler func(err error) (delay time.Duration, replace bool, fatal error)

// Context is what a [Set] works with — matter.js PeerSetContext
// (PeerSet.ts:43-51) and Peer.Context (Peer.ts:677-681). Exchanges, Names
// and Fabrics are required.
type Context struct {
	// Exchanges is the controller's exchange manager: its socket is the one
	// a peer connects over (matter.js PeerSet.#openSocket, PeerSet.ts:274-304,
	// has no counterpart here).
	Exchanges *exchange.Manager
	// Names is the DNS-SD cache a peer's operational service lives on.
	Names *discovery.Names
	// Fabrics returns our side of the fabric of a peer address — matter.js
	// SessionManager.fabricFor (session/SessionManager.ts:404-409).
	Fabrics func(handshake.PeerAddress) (handshake.Fabric, bool)
	// Resumption keeps the CASE resumption records; nil keeps none.
	Resumption ResumptionStore
	// Store keeps the peer descriptors; nil keeps none.
	Store Store
	// Timing overrides [DefaultTiming] field-wise; nil is the defaults.
	Timing *Timing
	// HandleError is the set's [ErrorHandler]; nil uses the delays as they
	// are.
	HandleError ErrorHandler
	// Logger receives the diagnostics; nil discards them.
	Logger *slog.Logger
	// Clock paces the connection process and the address monitor; nil is
	// the system clock.
	Clock exchange.Clock
	// Rand returns the jitter draw in [0, 1) of a Busy delay; nil is
	// math/rand/v2.Float64.
	Rand func() float64

	// Disconnected is called when a peer's last session went — matter.js
	// PeerSet.disconnected (PeerSet.ts:95-102).
	Disconnected func(p *Peer)
	// Updated is called after a peer's descriptor was saved — matter.js
	// Peer.updated (Peer.ts:190-195, :575-579).
	Updated func(p *Peer)
	// EstablishmentUnresponsive is called when a CASE establishment
	// attempt retransmitted past the MRP budget — matter.js
	// Peer.establishmentUnresponsive (Peer.ts:197-204).
	EstablishmentUnresponsive func(p *Peer)
}

// Set manages the peers on our fabrics and their operational connections.
// It is matter.js PeerSet (packages/protocol/src/peer/PeerSet.ts:56-339 at
// 0ceb202d), without the transport preference and group lookups. It is
// safe for concurrent use.
type Set struct {
	ctx   Context
	log   *slog.Logger
	clock exchange.Clock
	rand  func() float64

	mu           sync.Mutex
	peers        []*Peer
	timing       Timing
	handleError  ErrorHandler
	disconnected bool
}

// NewSet returns a set over c.
func NewSet(c Context) (*Set, error) {
	if c.Exchanges == nil || c.Names == nil || c.Fabrics == nil {
		return nil, errors.New("peer: a Set needs Exchanges, Names and Fabrics")
	}
	s := &Set{ctx: c, log: c.Logger, clock: c.Clock, rand: c.Rand, timing: DefaultTiming(), handleError: c.HandleError}
	if c.Timing != nil {
		s.timing = Merge(s.timing, *c.Timing)
	}
	if s.log == nil {
		s.log = slog.New(slog.DiscardHandler)
	}
	if s.clock == nil {
		s.clock = exchange.SystemClock{}
	}
	if s.rand == nil {
		s.rand = rand.Float64
	}
	return s, nil
}

// Timing is the set's timing (PeerSet.ts:226-228).
func (s *Set) Timing() Timing {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.timing
}

// SetTiming replaces the set's timing with t over the defaults, for
// connections started from now on (PeerSet.ts:230-232).
func (s *Set) SetTiming(t Timing) {
	s.mu.Lock()
	s.timing = Merge(DefaultTiming(), t)
	s.mu.Unlock()
}

// SetHandleError replaces the set's error handler (PeerSet.ts:234-236).
func (s *Set) SetHandleError(h ErrorHandler) {
	s.mu.Lock()
	s.handleError = h
	s.mu.Unlock()
}

func (s *Set) errorHandler() ErrorHandler {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.handleError
}

func (s *Set) isDisconnected() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.disconnected
}

// For returns the peer at addr, creating it when the set does not know it
// (PeerSet.ts:159-175). It fails when no fabric of ours has the address's
// fabric index (matter.js fabricFor throws).
func (s *Set) For(addr handshake.PeerAddress) (*Peer, error) {
	if p := s.Get(addr); p != nil {
		return p, nil
	}
	return s.AddKnownPeer(Descriptor{Address: addr})
}

// Get returns the peer at addr, nil when unknown (PeerSet.ts:241-246).
func (s *Set) Get(addr handshake.PeerAddress) *Peer {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, p := range s.peers {
		if p.address == addr {
			return p
		}
	}
	return nil
}

// Has reports whether the set knows addr (PeerSet.ts:183-185).
func (s *Set) Has(addr handshake.PeerAddress) bool { return s.Get(addr) != nil }

// Peers returns the peers in the order they joined.
func (s *Set) Peers() []*Peer {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.peers)
}

// Size is the number of peers (PeerSet.ts:187-189).
func (s *Set) Size() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.peers)
}

// AddKnownPeer adds the peer d describes, or updates the known peer: its
// operational address and CATs replace the peer's when d gives them, its
// discovery data fills only what the peer does not know, its reported
// session parameters only when the peer has none. Mirrors matter.js
// PeerSet.addKnownPeer (PeerSet.ts:306-338).
func (s *Set) AddKnownPeer(d Descriptor) (*Peer, error) {
	s.mu.Lock()
	for _, p := range s.peers {
		if p.address == d.Address {
			s.mu.Unlock()
			p.mergeKnown(d)
			return p, nil
		}
	}
	fabric, ok := s.ctx.Fabrics(d.Address)
	if !ok || fabric.Identity == nil {
		s.mu.Unlock()
		return nil, fmt.Errorf("peer: no fabric with index %d", d.Address.FabricIndex)
	}
	p := newPeer(s, d, fabric)
	s.peers = append(s.peers, p)
	s.mu.Unlock()
	p.start()
	return p, nil
}

// Adopt makes a secure session of one of our fabrics its peer's, creating
// the peer — with the session's address as its operational address — when
// the set does not know it. A PASE session, a closed one, or one of a
// fabric we do not have is ignored (nil). Mirrors matter.js
// PeerSet.#adoptSession (PeerSet.ts:113-125) with operationalAddressOf
// (:341-350, the UDP branch). The connection process adopts the session
// its CASE handshake established; stage 2's commissioning adopts its
// first CASE session the same way.
func (s *Set) Adopt(session *exchange.Session) *Peer {
	if session == nil || !session.IsSecure() || session.FabricIndex() == 0 || session.IsClosed() {
		return nil
	}
	addr := handshake.PeerAddress{FabricIndex: session.FabricIndex(), NodeID: session.PeerNodeID()}
	p, err := s.AddKnownPeer(Descriptor{Address: addr, OperationalAddress: session.Addr()})
	if err != nil {
		s.log.Debug("matter.controller.peer.adopt_failed", slog.String("peer", addressString(addr)), slog.String("err", err.Error()))
		return nil
	}
	p.addSession(session)
	return p
}

// Disconnect ends every peer's connection process and keeps the set from
// connecting again: a later attempt fails with [ErrNotConnectable].
// Mirrors matter.js PeerSet.disconnect (PeerSet.ts:248-262).
func (s *Set) Disconnect(ctx context.Context) {
	s.mu.Lock()
	s.disconnected = true
	peers := slices.Clone(s.peers)
	s.mu.Unlock()
	for _, p := range peers {
		p.Disconnect(ctx, nil)
	}
}

// Close closes every peer in order (PeerSet.ts:264-272).
func (s *Set) Close(ctx context.Context) {
	for _, p := range s.Peers() {
		p.Close(ctx)
	}
}

// remove drops p from the set — Peer.Context.closed (PeerSet.ts:91).
func (s *Set) remove(p *Peer) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.peers = slices.DeleteFunc(s.peers, func(x *Peer) bool { return x == p })
}

// addressString formats a peer address as matter.js PeerAddress.toString
// does (PeerAddress.ts:59-63): "@<fabric index>:<node id>", both hex.
func addressString(a handshake.PeerAddress) string {
	return fmt.Sprintf("@%x:%x", a.FabricIndex, a.NodeID)
}

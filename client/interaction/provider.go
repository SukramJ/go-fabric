// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package interaction

import (
	"context"
	"errors"
	"net"
	"time"

	"github.com/SukramJ/go-fabric/client/exchange"
	"github.com/SukramJ/go-fabric/client/peer"
	"github.com/SukramJ/go-fabric/im"
)

// Path floors of the base exchange provider: the CapabilityMinima
// minimums a peer that reports none is assumed to support. Mirrors
// matter.js packages/protocol/src/protocol/ExchangeProvider.ts:85-90 at
// 0ceb202d.
const (
	DefaultReadPathsSupported      = 9
	DefaultSubscribePathsSupported = 3
)

// ErrNoSession is a provider without a session to run the exchange on —
// matter.js's "No existing session available for probe"
// (PeerExchangeProvider.ts:118-121) and a closed dedicated session.
var ErrNoSession = errors.New("interaction: no session")

// ExchangeOptions configures [Provider.InitiateExchange] — the part of
// matter.js NewExchangeOptions (ExchangeProvider.ts:20-63) the controller
// uses.
type ExchangeOptions struct {
	// RequireExistingSession uses the newest session and never connects.
	RequireExistingSession bool
	// AddressOverride sends the exchange to this address instead of the
	// session's.
	AddressOverride *net.UDPAddr
	// ConnectionTimeout bounds the connection a peer provider waits for:
	// 0 is the peer set's default, negative waits without bound (matter.js
	// Forever, ClientInteraction.ts:1095).
	ConnectionTimeout time.Duration
}

// Provider gives an interaction its exchanges — matter.js ExchangeProvider
// (ExchangeProvider.ts:71-117).
type Provider interface {
	// InitiateExchange opens an IM exchange with the peer.
	InitiateExchange(ctx context.Context, opts ExchangeOptions) (*exchange.Exchange, error)
	// PeerAddress is the peer's address, false for an uncommissioned peer
	// (a PASE session).
	PeerAddress() (exchange.PeerAddress, bool)
	// MaximumPeerResponseTime is the longest wait for the peer's answer.
	MaximumPeerResponseTime(ept time.Duration, includeSending bool) time.Duration
	// VerifyReachability verifies the peer answers, driving recovery; the
	// base reports reachable (:114-116).
	VerifyReachability(ctx context.Context) bool
	// ReadPathsSupported and SubscribePathsSupported are the peer's path
	// floors (:85-90).
	ReadPathsSupported() int
	SubscribePathsSupported() int
	// Session is the dedicated session, or the newest one; nil for none.
	Session() *exchange.Session
	// Manager is the exchange manager the sessions live on.
	Manager() *exchange.Manager
}

// SessionProvider is the provider bound to session s of mgr — matter.js
// DedicatedChannelExchangeProvider (ExchangeProvider.ts:122-164).
func SessionProvider(mgr *exchange.Manager, s *exchange.Session) Provider {
	return &sessionProvider{mgr: mgr, s: s}
}

type sessionProvider struct {
	mgr *exchange.Manager
	s   *exchange.Session
}

func (p *sessionProvider) InitiateExchange(_ context.Context, opts ExchangeOptions) (*exchange.Exchange, error) {
	return p.mgr.Initiate(p.s, im.InteractionModelProtocolID, exchange.Options{AddressOverride: opts.AddressOverride})
}

// PeerAddress is NodeSession.peerAddress (ExchangeProvider.ts:159-163): a
// CASE session's (fabric index, peer node id).
func (p *sessionProvider) PeerAddress() (exchange.PeerAddress, bool) {
	if p.s.FabricIndex() == 0 || p.s.PeerNodeID() == 0 {
		return exchange.PeerAddress{}, false
	}
	return exchange.PeerAddress{FabricIndex: p.s.FabricIndex(), NodeID: p.s.PeerNodeID()}, true
}

// MaximumPeerResponseTime is ExchangeManager.calculateMaximumPeerResponseTimeMsFor
// on the session (ExchangeProvider.ts:148-157).
func (p *sessionProvider) MaximumPeerResponseTime(ept time.Duration, includeSending bool) time.Duration {
	if ept == 0 {
		ept = exchange.DefaultExpectedProcessingTime
	}
	if !p.s.UsesMRP() {
		// A reliable channel (BTP) waits the channel's time (MRP.ts:111-113).
		return exchange.ReliableMaxPeerResponseTime(ept)
	}
	local := exchange.IntervalsOf(p.mgr.LocalParameters())
	if !includeSending {
		return exchange.MaxPeerResponseTime(nil, false, local, ept)
	}
	peerIntervals := exchange.IntervalsOf(p.s.Parameters())
	return exchange.MaxPeerResponseTime(&peerIntervals, p.s.IsPeerActive(), local, ept)
}

func (p *sessionProvider) VerifyReachability(context.Context) bool { return true }
func (p *sessionProvider) ReadPathsSupported() int                 { return DefaultReadPathsSupported }

func (p *sessionProvider) SubscribePathsSupported() int { return DefaultSubscribePathsSupported }
func (p *sessionProvider) Session() *exchange.Session   { return p.s }
func (p *sessionProvider) Manager() *exchange.Manager   { return p.mgr }

// PeerProvider is the provider over peer p — matter.js
// PeerExchangeProvider (packages/protocol/src/peer/PeerExchangeProvider.ts:19-163):
// each exchange runs on the peer's newest session, connecting first
// unless RequireExistingSession is set (:73-150), reachability is the
// peer's (:69-71).
func PeerProvider(p *peer.Peer) Provider { return &peerProvider{p: p} }

// ForPeer returns the interaction over peer p.
func ForPeer(p *peer.Peer, cfg Config) *Interaction { return Bind(PeerProvider(p), cfg) }

type peerProvider struct {
	p *peer.Peer
}

func (p *peerProvider) InitiateExchange(ctx context.Context, opts ExchangeOptions) (*exchange.Exchange, error) {
	for {
		if !opts.RequireExistingSession {
			if _, err := p.p.Connect(ctx, peer.ConnectOptions{ConnectionTimeout: opts.ConnectionTimeout}); err != nil {
				return nil, err
			}
		}
		s := p.p.NewestSession()
		if s == nil {
			if opts.RequireExistingSession {
				return nil, ErrNoSession
			}
			// The session closed between connect and here: connect again
			// (:117-125).
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			continue
		}
		x, err := p.p.Exchanges().Initiate(s, im.InteractionModelProtocolID, exchange.Options{AddressOverride: opts.AddressOverride})
		if errors.Is(err, exchange.ErrSessionClosed) && !opts.RequireExistingSession {
			continue
		}
		return x, err
	}
}

func (p *peerProvider) PeerAddress() (exchange.PeerAddress, bool) { return p.p.Address(), true }

// MaximumPeerResponseTime is PeerExchangeProvider.maximumPeerResponseTime
// (:152-162): the peer counted active, its parameters only with
// includeSending.
func (p *peerProvider) MaximumPeerResponseTime(ept time.Duration, includeSending bool) time.Duration {
	if ept == 0 {
		ept = exchange.DefaultExpectedProcessingTime
	}
	local := exchange.IntervalsOf(p.p.Exchanges().LocalParameters())
	if !includeSending {
		return exchange.MaxPeerResponseTime(nil, true, local, ept)
	}
	peerIntervals := exchange.IntervalsOf(p.p.SessionParameters())
	return exchange.MaxPeerResponseTime(&peerIntervals, true, local, ept)
}

// VerifyReachability is Peer.verifyReachability with reason
// "session-suspect" (ClientInteraction.ts:1112).
func (p *peerProvider) VerifyReachability(ctx context.Context) bool {
	ok, _ := p.p.VerifyReachability(ctx, peer.ReasonSessionSuspect)
	return ok
}

func (p *peerProvider) ReadPathsSupported() int      { return DefaultReadPathsSupported }
func (p *peerProvider) SubscribePathsSupported() int { return DefaultSubscribePathsSupported }
func (p *peerProvider) Session() *exchange.Session   { return p.p.NewestSession() }
func (p *peerProvider) Manager() *exchange.Manager   { return p.p.Exchanges() }

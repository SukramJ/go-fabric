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
	// Network names the network profile whose throttle the exchange (and a
	// connection it starts) takes a slot of; "" is the peer's medium
	// profile — matter.js ClientRequest.network
	// (packages/protocol/src/action/client/ClientRequest.ts:14-24 at
	// 862ad75f), "unlimited" disabling the throttle, "icdLit" the priority
	// network of an await-mode LIT ICD (SustainedSubscription.ts:133-149).
	Network string
	// AdditionalMrpDelay, when set, overrides the peer-medium MRP margin of
	// the exchange (ClientRequest.ts:26-32).
	AdditionalMrpDelay *time.Duration
	// RequiredTransport is a hard transport constraint: an exchange on a
	// session over it, established over it when none is — TCP for a large
	// message (ClientInteraction.ts:1235). nil for none. A provider bound to
	// one session ignores it (ClientRequest.ts:81-84).
	RequiredTransport *exchange.Transport
	// PreferredTransport overrides the peer's transport preference.
	PreferredTransport *exchange.Transport
}

// ClientRequest is what every interaction request carries for its
// exchange — matter.js ClientRequest (action/client/ClientRequest.ts:13-87
// at 862ad75f): the network profile and the MRP margin override. It is
// embedded in [ReadRequest], [SubscribeRequest], [WriteOptions] and
// [InvokeOptions].
type ClientRequest struct {
	// Network is [ExchangeOptions].Network.
	Network string
	// AdditionalMrpDelay is [ExchangeOptions].AdditionalMrpDelay.
	AdditionalMrpDelay *time.Duration
	// LargeMessage requires a session that permits payloads larger than
	// the IPv6 MTU: a TCP session, established or failing, never the MRP
	// fallback of a transport preference (ClientRequest.ts:62-86). A peer
	// that negotiates TCP and then denies it fails with
	// peer.ErrTCPUnsupported. nil and false are no requirement; an invoke
	// tells them apart for its inference (ClientInteraction.ts:716-719).
	LargeMessage *bool
}

// large reports a large-message request.
func (r ClientRequest) large() bool { return r.LargeMessage != nil && *r.LargeMessage }

// exchangeOptions are the exchange options of the request: a large message
// requires TCP (ClientInteraction.ts:1235).
func (r ClientRequest) exchangeOptions() ExchangeOptions {
	o := ExchangeOptions{Network: r.Network, AdditionalMrpDelay: r.AdditionalMrpDelay}
	if r.large() {
		tcp := exchange.TransportTCP
		o.RequiredTransport = &tcp
	}
	return o
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

// maxPathsPerInvoke is the dedicated session's (ExchangeProvider.ts:130-132).
func (p *sessionProvider) maxPathsPerInvoke() int { return int(p.s.Parameters().MaxPathsPerInvoke) }

func (p *sessionProvider) ReadPathsSupported() int { return DefaultReadPathsSupported }

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

// The exchange takes a slot of the network profile opts names (the
// peer's medium profile by default), released when it closes, and pads
// its sends with opts.AdditionalMrpDelay, else the medium profile's
// messaging margin (:85-141): the throttle follows the named profile, the
// margin always the medium's.
func (p *peerProvider) InitiateExchange(ctx context.Context, opts ExchangeOptions) (*exchange.Exchange, error) {
	for {
		if !opts.RequireExistingSession {
			if _, err := p.p.Connect(ctx, peer.ConnectOptions{
				ConnectionTimeout: opts.ConnectionTimeout, Network: opts.Network, AdditionalMrpDelay: opts.AdditionalMrpDelay,
				RequiredTransport: opts.RequiredTransport, PreferredTransport: opts.PreferredTransport,
			}); err != nil {
				return nil, err
			}
		}
		networks := p.p.Networks()
		network, err := networks.Select(p.p, opts.Network)
		if err != nil {
			return nil, err
		}
		margin, _ := exchange.MarginFor(&networks.ForPeer(p.p).Margins, im.InteractionModelProtocolID)
		if opts.AdditionalMrpDelay != nil {
			margin = *opts.AdditionalMrpDelay
		}
		slot, err := network.Semaphore.Obtain(ctx)
		if err != nil {
			return nil, err
		}
		// The newest session over the first resolved transport that has one
		// (PeerExchangeProvider.ts:104-115).
		s := p.p.SessionFor(opts.RequiredTransport, opts.PreferredTransport)
		if s == nil {
			slot.Close()
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
		x, err := p.p.Exchanges().Initiate(s, im.InteractionModelProtocolID, exchange.Options{
			AddressOverride: opts.AddressOverride, PeerAdditionalMrpDelay: &margin, OnClosing: slot.Close,
		})
		if err != nil {
			slot.Close()
		}
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
//
// Over a TCP session it is the reliable channel's (MRP.ts:88-91, the
// provider's channelType, PeerExchangeProvider.ts:50-56).
func (p *peerProvider) MaximumPeerResponseTime(ept time.Duration, includeSending bool) time.Duration {
	if ept == 0 {
		ept = exchange.DefaultExpectedProcessingTime
	}
	if s := p.p.NewestSession(); s != nil && !s.UsesMRP() {
		return exchange.ReliableMaxPeerResponseTime(ept)
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

// maxPathsPerInvoke is the peer's session parameters'
// (PeerExchangeProvider.ts:30-32).
func (p *peerProvider) maxPathsPerInvoke() int { return int(p.p.SessionParameters().MaxPathsPerInvoke) }

// connect connects to the peer unless an existing session is required
// — ExchangeProvider.connect, awaited before a command is queued for a
// batch (ClientInteraction.ts:756-760).
func (p *peerProvider) connect(ctx context.Context, opts ExchangeOptions) error {
	if opts.RequireExistingSession {
		return nil
	}
	_, err := p.p.Connect(ctx, peer.ConnectOptions{
		ConnectionTimeout: opts.ConnectionTimeout, Network: opts.Network, AdditionalMrpDelay: opts.AdditionalMrpDelay,
		RequiredTransport: opts.RequiredTransport, PreferredTransport: opts.PreferredTransport,
	})
	return err
}

func (p *peerProvider) ReadPathsSupported() int      { return DefaultReadPathsSupported }
func (p *peerProvider) SubscribePathsSupported() int { return DefaultSubscribePathsSupported }
func (p *peerProvider) Session() *exchange.Session   { return p.p.NewestSession() }
func (p *peerProvider) Manager() *exchange.Manager   { return p.p.Exchanges() }

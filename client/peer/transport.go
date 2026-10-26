// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package peer

import (
	"context"
	"errors"
	"fmt"
	"net"

	"github.com/SukramJ/go-fabric/client/exchange"
	"github.com/SukramJ/go-fabric/secure/sessionparams"
)

// The TCP initiator: matter.js Peer's resolveTransports
// (packages/protocol/src/peer/Peer.ts:352-410 at 862ad75f) and its
// transport preference (:76-83), the tcpUnsupported flag (:292-293, :352-353), PeerSet's
// socket opening by address type (PeerSet.ts:274-304) and the
// TcpUnsupportedError of a TCP session that denies TCP
// (PeerCommunicationError.ts:22-26).

// ErrTCPUnsupported is a peer that negotiated a TCP session but reports no
// TCP server support — matter.js TcpUnsupportedError
// (PeerCommunicationError.ts:22-26), raised from the session-parameter
// validation of a TCP CASE attempt (PeerConnection.ts:596-621).
var ErrTCPUnsupported = errors.New("peer: peer negotiated a TCP session but reports no TCP server support")

// ErrNoTCP is a TCP attempt without a TCP dialer — matter.js's
// NetworkUnreachableError "No TCP interface available"
// (PeerSet.ts:284-288). It is a network error.
var ErrNoTCP = fmt.Errorf("%w: no TCP interface available", exchange.ErrNetwork)

// DialTCP opens a TCP connection to a peer's operational address; the
// channel it returns delivers its messages to the set's exchange manager
// ([exchange.Manager.HandleChannelMessage]).
type DialTCP func(ctx context.Context, addr *net.UDPAddr) (exchange.Channel, error)

// SetTransportPreference sets the peer's preferred transport for outgoing
// connections: [exchange.TransportTCP] is a soft hint — the connection
// falls back to UDP when the peer does not serve TCP — and anything else
// clears it (Peer.ts:76-83; NetworkClient.ts:79).
func (p *Peer) SetTransportPreference(t exchange.Transport) {
	p.mu.Lock()
	p.preferTCP = t == exchange.TransportTCP
	p.mu.Unlock()
}

// TransportPreference is the preferred transport: TCP when set, else UDP.
func (p *Peer) TransportPreference() exchange.Transport {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.preferTCP {
		return exchange.TransportTCP
	}
	return exchange.TransportUDP
}

// MarkTCPUnsupported records that the peer does not serve TCP although a
// TCP session to it was attempted; [Peer.ResolveTransports] honours it and
// it is saved until a later session reports TCP server support
// (Peer.ts:346-353, :178-180).
func (p *Peer) MarkTCPUnsupported() {
	p.mu.Lock()
	if !p.desc.TCPUnsupported {
		p.desc.TCPUnsupported = true
		p.changedLocked()
	}
	p.mu.Unlock()
}

// ResolveTransports is the ordered list of transports to attempt, nil for
// the default UDP (Peer.ts:359-410): a required transport (TCP or UDP)
// alone; without a TCP preference (preferred, else the peer's) nil; with one, the session
// parameters' TCP server bit decides — set: [TCP, UDP], cleared: nil — and
// without it the DNS-SD T record's. A peer marked TCP-unsupported reports
// the bit cleared (:292-293).
//
// matter.js tells an absent bit (tag 8 left out) from a cleared one; the
// module's session parameters are a bitmap and cannot, so a peer of Matter
// 1.5 or later whose bitmap names no TCP role counts as absent and its T
// record decides, while an older one counts as denying (its TCP cleared,
// SessionParameters.ts:49-56).
func (p *Peer) ResolveTransports(required, preferred *exchange.Transport) []exchange.Transport {
	if required != nil && (*required == exchange.TransportTCP || *required == exchange.TransportUDP) {
		return []exchange.Transport{*required}
	}
	pref := p.TransportPreference()
	if preferred != nil {
		pref = *preferred
	}
	if pref != exchange.TransportTCP {
		return nil
	}
	tcp := []exchange.Transport{exchange.TransportTCP, exchange.TransportUDP}
	params := p.SessionParameters()
	p.mu.Lock()
	unsupported := p.desc.TCPUnsupported
	p.mu.Unlock()
	mdnsTCP := p.advertisesTCPServer()
	switch {
	case unsupported:
		return nil
	case params.SupportedTransports&sessionparams.TransportTCPServer != 0:
		return tcp
	case params.SupportedTransports&(sessionparams.TransportTCPClient|sessionparams.TransportTCPServer) != 0:
		return nil // reported, without the server role
	case params.SpecificationVersion != 0 && params.SpecificationVersion < sessionparams.MinTCPSpecificationVersion:
		return nil // pre-1.5: TCP cleared, a denial
	case mdnsTCP:
		return tcp
	}
	return nil
}

// tcpDenied reports a TCP session whose peer parameters deny TCP while
// DNS-SD does not advertise a TCP server — the validation of a TCP CASE
// attempt (PeerConnection.ts:596-621): an absent bit never denies, and a
// cleared one is overridden while the T record still names a server.
func (p *Peer) tcpDenied(reported *sessionparams.Parameters) bool {
	if reported == nil {
		return false
	}
	r := exchange.PeerParameters(reported)
	if r.SupportedTransports&sessionparams.TransportTCPServer != 0 {
		return false
	}
	if r.SupportedTransports == 0 && r.SpecificationVersion >= sessionparams.MinTCPSpecificationVersion {
		return false // absent (see ResolveTransports)
	}
	return !p.advertisesTCPServer()
}

// advertisesTCPServer reads the T key's TCP server bit: the live TXT
// record's while the service has one, else the descriptor's
// (Peer.ts:325-328, advertisedTransports).
func (p *Peer) advertisesTCPServer() bool {
	if p.service.HasTXTRecord() {
		t := p.service.TXTData().T
		return t != nil && t.TCPServer
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	dd := p.desc.DiscoveryData
	return dd != nil && dd.T != nil && dd.T.TCPServer
}

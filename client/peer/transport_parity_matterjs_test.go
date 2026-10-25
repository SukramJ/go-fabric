// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package peer

// The session-parameter TCP gate cases of matter.js
// packages/node/test/node/ClientNodeTcpTest.ts:266-339 at 862ad75f, which
// set a peer's reported parameters by hand: Peer.resolveTransports
// (packages/protocol/src/peer/Peer.ts:359-410) on a harness peer whose
// descriptor and TXT record the test controls.

import (
	"context"
	"net"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/client/exchange"
	"github.com/SukramJ/go-fabric/secure/sessionparams"
	"github.com/SukramJ/go-fabric/transport/message"
	"github.com/SukramJ/go-fabric/transport/mrp"
)

// spec15 is a Matter 1.5 peer (TCP is defined from 1.5 on).
const spec15 = sessionparams.MinTCPSpecificationVersion

func reportOf(p *Peer, transports uint16) {
	p.mu.Lock()
	p.desc.ReportedSessionParameters = &sessionparams.Parameters{SpecificationVersion: spec15, SupportedTransports: transports}
	p.mu.Unlock()
}

func TestPeerTransportGateParity(t *testing.T) {
	t.Parallel()
	tcp := exchange.TransportTCP
	both := []exchange.Transport{exchange.TransportTCP, exchange.TransportUDP}
	gatePeer := func(t *testing.T, txt ...string) (*harness, *Peer) {
		t.Helper()
		h := newHarness(t, nil)
		p, _ := h.set.AddKnownPeer(Descriptor{Address: testAddr})
		if len(txt) > 0 {
			h.mdns.announce(t, srvRR(h.qname, h.hostQ, 5540, 120), txtRR(h.qname, 120, txt...), aaaaRR(h.hostQ, "fd00::1", 120))
			eventually(t, "the TXT record", func() bool { return p.service.HasTXTRecord() })
		}
		return h, p
	}
	t.Run("honors TCP when the peer's session parameters report TCP server support", func(t *testing.T) {
		t.Parallel()
		_, p := gatePeer(t)
		reportOf(p, sessionparams.TransportTCPServer)
		if got := p.ResolveTransports(nil, &tcp); !slices.Equal(got, both) {
			t.Errorf("transports %v", got)
		}
	})
	t.Run("falls back to UDP when the peer's session parameters do not report TCP server support", func(t *testing.T) {
		t.Parallel()
		_, p := gatePeer(t, "T=4")
		reportOf(p, sessionparams.TransportTCPClient) // stated, without the server role
		if got := p.ResolveTransports(nil, &tcp); got != nil {
			t.Errorf("transports %v", got)
		}
	})
	t.Run("attempts TCP when session parameters omit TCP support but mDNS advertises a TCP server", func(t *testing.T) {
		t.Parallel()
		_, p := gatePeer(t, "T=4")
		reportOf(p, 0)
		if got := p.ResolveTransports(nil, &tcp); !slices.Equal(got, both) {
			t.Errorf("transports %v", got)
		}
	})
	t.Run("falls back to UDP as soon as the live TXT record drops the TCP server", func(t *testing.T) {
		t.Parallel()
		h, p := gatePeer(t, "T=4")
		eventually(t, "T in the descriptor", func() bool {
			dd := p.Descriptor().DiscoveryData
			return dd != nil && dd.T != nil && dd.T.TCPServer
		})
		reportOf(p, 0)
		h.mdns.announce(t, txtRR(h.qname, 0, "T=4"), txtRR(h.qname, 120, "SII=5000"))
		eventually(t, "the new TXT record", func() bool {
			dd := p.service.TXTData()
			return dd.SII != nil && *dd.SII == 5*time.Second && dd.T == nil
		})
		if got := p.ResolveTransports(nil, &tcp); got != nil {
			t.Errorf("transports %v", got)
		}
	})
	t.Run("falls back to UDP when session parameters omit TCP support and mDNS does not advertise a TCP server", func(t *testing.T) {
		t.Parallel()
		_, p := gatePeer(t, "SII=5000")
		reportOf(p, 0)
		if got := p.ResolveTransports(nil, &tcp); got != nil {
			t.Errorf("transports %v", got)
		}
	})
	t.Run("a required transport is a hard constraint", func(t *testing.T) {
		t.Parallel()
		_, p := gatePeer(t)
		udp := exchange.TransportUDP
		if got := p.ResolveTransports(&tcp, nil); !slices.Equal(got, []exchange.Transport{tcp}) {
			t.Errorf("required TCP: %v", got)
		}
		if got := p.ResolveTransports(&udp, &tcp); !slices.Equal(got, []exchange.Transport{udp}) {
			t.Errorf("required UDP: %v", got)
		}
		if got := p.ResolveTransports(nil, nil); got != nil {
			t.Errorf("no preference: %v", got)
		}
	})
	t.Run("a pre-1.5 peer denies TCP", func(t *testing.T) {
		t.Parallel()
		_, p := gatePeer(t, "T=4")
		p.mu.Lock()
		p.desc.ReportedSessionParameters = &sessionparams.Parameters{SpecificationVersion: 0x01040000, SupportedTransports: sessionparams.TransportTCPServer}
		p.mu.Unlock()
		if got := p.ResolveTransports(nil, &tcp); got != nil {
			t.Errorf("transports %v", got)
		}
	})
}

// fakeTCP is a connected TCP channel for the selection cases.
type fakeTCP struct {
	remote *net.TCPAddr
	closed chan struct{}
	once   sync.Once
}

func newFakeTCP(port int) *fakeTCP {
	return &fakeTCP{remote: &net.TCPAddr{IP: net.ParseIP("fd00::1"), Port: port}, closed: make(chan struct{})}
}
func (c *fakeTCP) Send([]byte) error       { return nil }
func (c *fakeTCP) Reliable() bool          { return true }
func (c *fakeTCP) MaxMessageSize() int     { return 64000 }
func (c *fakeTCP) Remote() net.Addr        { return c.remote }
func (c *fakeTCP) Close() error            { c.once.Do(func() { close(c.closed) }); return nil }
func (c *fakeTCP) Closed() <-chan struct{} { return c.closed }

// TestPeerTCPSessionSelectionParity is the "TCP session selection" block
// of matter.js TcpSessionBindingTest.ts:469-537 on a peer's sessions:
// Peer.newestSession(ChannelType.TCP) (Peer.ts:581-601).
func TestPeerTCPSessionSelectionParity(t *testing.T) {
	t.Parallel()
	adopt := func(t *testing.T, h *harness, id uint16, addr *net.UDPAddr) *exchange.Session {
		t.Helper()
		s := h.secureSession(t, id, addr)
		h.set.Adopt(s)
		return s
	}
	t.Run("finds a TCP session among mixed sessions", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, nil)
		adopt(t, h, 1, udpAddr(addr1))
		tcpSession := adopt(t, h, 2, h.mgr.AttachChannel(newFakeTCP(5540)))
		if got := h.set.Get(testAddr).NewestSessionOn(exchange.TransportTCP); got != tcpSession {
			t.Errorf("selected %v", got)
		}
	})
	t.Run("returns undefined when no TCP session exists", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, nil)
		adopt(t, h, 1, udpAddr(addr1))
		if got := h.set.Get(testAddr).NewestSessionOn(exchange.TransportTCP); got != nil {
			t.Errorf("selected %v", got)
		}
	})
	t.Run("skips closing TCP sessions", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, nil)
		closing := adopt(t, h, 1, h.mgr.AttachChannel(newFakeTCP(5540)))
		active := adopt(t, h, 2, h.mgr.AttachChannel(newFakeTCP(5541)))
		closing.Close()
		if got := h.set.Get(testAddr).NewestSessionOn(exchange.TransportTCP); got != active {
			t.Errorf("selected %v", got)
		}
	})
	t.Run("returns undefined when all TCP sessions are closing", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, nil)
		s := adopt(t, h, 1, h.mgr.AttachChannel(newFakeTCP(5540)))
		s.Close()
		if got := h.set.Get(testAddr).NewestSessionOn(exchange.TransportTCP); got != nil {
			t.Errorf("selected %v", got)
		}
	})
}

// harnessTCP is a TCP channel of the harness: what the controller sends on
// it lands in the harness's datagram queue addressed to its remote, so
// sigma1 finds it, and replyOn answers through it.
type harnessTCP struct {
	*fakeTCP
	h *harness
}

func (c *harnessTCP) Send(b []byte) error {
	c.h.udp.ch <- datagram{to: &net.UDPAddr{IP: c.remote.IP, Port: c.remote.Port}, bytes: b}
	return nil
}

// replyOn is reply for a Sigma1 that came over ch.
func (h *harness) replyOn(ch exchange.Channel, to unsecured, opcode uint8, payload []byte) {
	h.mu.Lock()
	h.counter++
	c := h.counter
	h.mu.Unlock()
	hdr := message.Header{MessageCounter: c, DestSize: message.DestNodeID, DestNodeID: to.hdr.SourceNodeID}
	proto := message.ProtocolHeader{Opcode: opcode, ExchangeID: to.proto.ExchangeID, ProtocolID: mrp.SecureChannelProtocolID, HasAck: true, AckCounter: to.hdr.MessageCounter}
	b := append(hdr.Marshal(), proto.Marshal()...)
	h.mgr.HandleChannelMessage(ch, append(b, payload...))
}

// tcpOwnershipPeer is a peer that prefers TCP and serves it, connecting
// through a dialer that hands out harness channels (recorded in dialed).
func tcpOwnershipPeer(t *testing.T) (*harness, *Peer, chan *harnessTCP) {
	t.Helper()
	dialed := make(chan *harnessTCP, 4)
	var h *harness
	h = newHarness(t, func(c *Context) {
		c.TransportPreference = exchange.TransportTCP
		c.DialTCP = func(_ context.Context, addr *net.UDPAddr) (exchange.Channel, error) {
			ch := &harnessTCP{fakeTCP: newFakeTCP(addr.Port), h: h}
			ch.remote.IP = addr.IP
			dialed <- ch
			return ch, nil
		}
	})
	p, _ := h.set.AddKnownPeer(Descriptor{
		Address: testAddr, OperationalAddress: udpAddr(addr1),
		ReportedSessionParameters: &sessionparams.Parameters{SpecificationVersion: spec15, SupportedTransports: sessionparams.TransportTCPServer},
	})
	return h, p, dialed
}

// TestPeerConnectionSocketOwnershipParity is the TCP describe of matter.js
// packages/protocol/test/peer/PeerConnectionSocketOwnershipTest.ts:90-127
// at 862ad75f: a TCP attempt's channel is closed when the pairing fails or
// is aborted (PeerConnection.ts:514-516, :640-650); the success case — the
// session adopts the channel — is controller:TestLargeMessageReadOverTCP.
func TestPeerConnectionSocketOwnershipParity(t *testing.T) {
	t.Parallel()
	t.Run("TCP", func(t *testing.T) {
		t.Parallel()
		t.Run("closes the channel when pair() throws", func(t *testing.T) {
			t.Parallel()
			h, p, dialed := tcpOwnershipPeer(t)
			connectAsync(p, ConnectOptions{ConnectionTimeout: -1})
			ch := <-dialed
			s := h.sigma1(t, addr1)
			h.replyOn(ch, s, mrp.SCOpcodeStatusReport, exchange.StatusReport{
				GeneralCode: mrp.SCStatusGeneralFailure, ProtocolID: uint32(mrp.SecureChannelProtocolID), ProtocolCode: mrp.SCStatusProtocolInvalidParameter,
			}.Encode())
			select {
			case <-ch.Closed():
			case <-time.After(10 * time.Second):
				t.Fatal("the channel of a failed pairing stays open")
			}
			p.Close(closingCtx())
		})
		t.Run("closes the channel when pair() throws AbortedError", func(t *testing.T) {
			t.Parallel()
			h, p, dialed := tcpOwnershipPeer(t)
			connectAsync(p, ConnectOptions{ConnectionTimeout: -1})
			ch := <-dialed
			h.sigma1(t, addr1)
			p.Close(closingCtx())
			select {
			case <-ch.Closed():
			case <-time.After(10 * time.Second):
				t.Fatal("the channel of an aborted pairing stays open")
			}
		})
	})
}

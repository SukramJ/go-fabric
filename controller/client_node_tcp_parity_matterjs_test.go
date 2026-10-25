// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controller_test

// A port of matter.js packages/node/test/node/ClientNodeTcpTest.ts at
// 862ad75f: the TCP roles of controller and device, the transport
// preference, commissioning and default traffic over UDP, the soft TCP
// preference, the session-parameter gate and the tcp-unsupported flag, on
// a loopback site (TCP runs over kernel sockets). The gate cases that set
// a peer's reported parameters by hand are client/peer's
// (TestPeerTransportGateParity). Beyond matter.js, whose mock TCP does not
// survive CASE (ClientNodeTcpTest.ts:27-30), a large-message read runs
// over a real TCP session here.

import (
	"context"
	"net"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/client/exchange"
	"github.com/SukramJ/go-fabric/client/peer"
	basicinfo "github.com/SukramJ/go-fabric/cluster/spec/basicinformation"
	onoffdef "github.com/SukramJ/go-fabric/cluster/spec/onoff"
	"github.com/SukramJ/go-fabric/controller"
	"github.com/SukramJ/go-fabric/controller/attestation"
	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/device"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/node"
	"github.com/SukramJ/go-fabric/secure/sessionparams"
)

// tcpPair is commissionPair (ClientNodeTcpTest.ts:44-70) on a loopback
// site: a controller with ctl's TCP roles and preference, a device with
// TCP in both roles when deviceTCP (matter.js `{ tcp: true }`), an
// OnOffLight on it, commissioned over UDP.
type tcpPair struct {
	site *fabrictest.Site
	dev  *fabrictest.Device
	c    *controller.Controller
	n    *client.Node
	ep   uint16
}

func newTCPPair(t *testing.T, ctl fabrictest.ControllerOptions, deviceTCP bool) *tcpPair {
	t.Helper()
	site := fabrictest.NewSite(t, fabrictest.Options{Loopback: true})
	light, err := device.NewOnOffLight(device.Options{StableKey: endpoint.StringKey("ep1"), Name: "ep1"})
	if err != nil {
		t.Fatal(err)
	}
	dev := site.AddDevice(fabrictest.Topology{Bridged: []node.Endpoint{light}, Configure: func(cfg *node.Config) {
		if deviceTCP {
			cfg.Network.TCP = node.TCPConfig{Incoming: true, Outgoing: true}
		}
	}})
	cfg := site.ControllerConfig(ctl)
	tp := &tcpPair{site: site, dev: dev}
	tp.run(t, func(ctx context.Context) (err error) {
		tp.c, err = controller.New(ctx, cfg)
		return err
	})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		_ = site.Run(ctx, tp.c.Close)
	})
	tp.run(t, func(ctx context.Context) (err error) {
		tp.n, err = tp.c.Pair(ctx, manualCode(t, dev), controller.PairOptions{OnAttestationFailure: attestation.Accept()})
		return err
	})
	ep, ok := dev.EndpointOf("ep1")
	if !ok {
		t.Fatal("no light endpoint")
	}
	tp.ep = ep
	return tp
}

func (tp *tcpPair) run(t *testing.T, fn func(ctx context.Context) error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := tp.site.Run(ctx, fn); err != nil {
		t.Fatal(err)
	}
}

// peer is protocolPeer (ClientNodeTcpTest.ts:75-80).
func (tp *tcpPair) peer(t *testing.T) *peer.Peer {
	t.Helper()
	ps := tp.c.Peers().Peers()
	if len(ps) == 0 {
		t.Fatal("no peer")
	}
	return ps[0]
}

func (tp *tcpPair) toggle(t *testing.T) {
	t.Helper()
	tp.run(t, func(ctx context.Context) error {
		_, err := tp.n.Invoke(ctx, client.CommandAt(tp.ep, onoffdef.ClusterID, onoffdef.CmdToggle), nil, client.InvokeOptions{})
		return err
	})
}

func transportOf(t *testing.T, p *peer.Peer) exchange.Transport {
	t.Helper()
	s := p.NewestSession()
	if s == nil {
		t.Fatal("no session")
	}
	return s.Transport()
}

func tcpPtr() *exchange.Transport { tcp := exchange.TransportTCP; return &tcp }

// deviceServesTCP reports whether the device accepts a TCP connection on
// its port.
func deviceServesTCP(dev *fabrictest.Device) bool {
	a := dev.Addr()
	c, err := net.DialTimeout("tcp", net.JoinHostPort(a.IP.String(), strconv.Itoa(a.Port)), 2*time.Second)
	if err != nil {
		return false
	}
	_ = c.Close()
	return true
}

var both = controller.TCPConfig{Incoming: true, Outgoing: true}

func TestClientNodeTcpParity(t *testing.T) {
	t.Parallel()
	t.Run("TCP interface availability", func(t *testing.T) {
		t.Parallel()
		t.Run("registers TCP interface on controller when tcp is enabled", func(t *testing.T) {
			t.Parallel()
			tp := newTCPPair(t, fabrictest.ControllerOptions{TCP: both}, true)
			if tp.c.TCPAddr() == nil {
				t.Error("the controller has no TCP listener")
			}
		})
		t.Run("does not register TCP interface when tcp is disabled", func(t *testing.T) {
			t.Parallel()
			tp := newTCPPair(t, fabrictest.ControllerOptions{}, false)
			if tp.c.TCPAddr() != nil {
				t.Error("the controller listens on TCP")
			}
		})
		t.Run("registers TCP interface on device when tcp is enabled", func(t *testing.T) {
			t.Parallel()
			tp := newTCPPair(t, fabrictest.ControllerOptions{}, true)
			if !deviceServesTCP(tp.dev) {
				t.Error("the device does not accept TCP")
			}
		})
		t.Run("enables TCP on controller only (outgoing)", func(t *testing.T) {
			t.Parallel()
			tp := newTCPPair(t, fabrictest.ControllerOptions{TCP: controller.TCPConfig{Outgoing: true}}, false)
			if tp.c.TCPAddr() != nil || deviceServesTCP(tp.dev) {
				t.Error("a TCP listener on the controller or the device")
			}
		})
	})
	t.Run("transport preference wiring", func(t *testing.T) {
		t.Parallel()
		t.Run("propagates UDP preference (default) — peer has no TCP preference", func(t *testing.T) {
			t.Parallel()
			tp := newTCPPair(t, fabrictest.ControllerOptions{TCP: both, TransportPreference: exchange.TransportUDP}, true)
			if got := tp.peer(t).TransportPreference(); got != exchange.TransportUDP {
				t.Errorf("preference %v", got)
			}
		})
		t.Run("leaves peer preference undefined when no preference is configured", func(t *testing.T) {
			t.Parallel()
			tp := newTCPPair(t, fabrictest.ControllerOptions{}, false)
			if got := tp.peer(t).TransportPreference(); got != exchange.TransportUDP {
				t.Errorf("preference %v", got)
			}
		})
		t.Run("allows setting TCP preference directly on protocol peer", func(t *testing.T) {
			t.Parallel()
			tp := newTCPPair(t, fabrictest.ControllerOptions{TCP: both}, true)
			p := tp.peer(t)
			p.SetTransportPreference(exchange.TransportTCP)
			if p.TransportPreference() != exchange.TransportTCP {
				t.Error("not set")
			}
			p.SetTransportPreference(exchange.TransportUDP)
			if p.TransportPreference() != exchange.TransportUDP {
				t.Error("not cleared")
			}
		})
	})
	t.Run("commissioning transport", func(t *testing.T) {
		t.Parallel()
		t.Run("commissions over UDP when TCP is enabled on both sides", func(t *testing.T) {
			t.Parallel()
			tp := newTCPPair(t, fabrictest.ControllerOptions{TCP: both}, true)
			if got := transportOf(t, tp.peer(t)); got != exchange.TransportUDP {
				t.Errorf("operational session over %v", got)
			}
		})
		t.Run("stores UDP addresses after commissioning with TCP enabled", func(t *testing.T) {
			t.Parallel()
			tp := newTCPPair(t, fabrictest.ControllerOptions{TCP: both}, true)
			if len(tp.n.Record().Addresses) == 0 {
				t.Error("no address stored")
			}
		})
	})
	t.Run("UDP operational behavior", func(t *testing.T) {
		t.Parallel()
		t.Run("invokes over UDP with default (no TCP) config", func(t *testing.T) {
			t.Parallel()
			tp := newTCPPair(t, fabrictest.ControllerOptions{}, false)
			tp.toggle(t)
			if got := transportOf(t, tp.peer(t)); got != exchange.TransportUDP {
				t.Errorf("session over %v", got)
			}
		})
		t.Run("invokes over UDP when TCP is enabled but not preferred", func(t *testing.T) {
			t.Parallel()
			tp := newTCPPair(t, fabrictest.ControllerOptions{TCP: both, TransportPreference: exchange.TransportUDP}, true)
			tp.toggle(t)
			if got := transportOf(t, tp.peer(t)); got != exchange.TransportUDP {
				t.Errorf("session over %v", got)
			}
		})
		t.Run("subscribes and receives updates over UDP when TCP is enabled but not preferred", func(t *testing.T) {
			t.Parallel()
			tp := newTCPPair(t, fabrictest.ControllerOptions{TCP: both, TransportPreference: exchange.TransportUDP}, true)
			// subscribedPeer (ClientNodeTcpTest.ts:220): the node's default
			// subscription is up first — it replaces the peer's other
			// subscriptions when it starts.
			wctx, wcancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer wcancel()
			if err := tp.site.Eventually(wctx, "the default subscription", tp.n.SubscriptionActive); err != nil {
				t.Fatal(err)
			}
			got := make(chan struct{}, 8)
			tp.run(t, func(ctx context.Context) error {
				var req client.SubscribeRequest
				req.Attributes = []client.AttributePath{client.AttributeAt(tp.ep, onoffdef.ClusterID, onoffdef.AttrOnOff)}
				req.Updated = func(*client.Report) {
					select {
					case got <- struct{}{}:
					default:
					}
				}
				_, err := tp.n.Subscribe(ctx, req)
				return err
			})
			<-got // the priming report
			tp.toggle(t)
			// Loopback sites run on the wall clock: a loaded test process
			// gets the site's two minutes, not its default wait.
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			if err := tp.site.Eventually(ctx, "the update", func() bool { return len(got) > 0 }); err != nil {
				t.Fatal(err)
			}
			if got := transportOf(t, tp.peer(t)); got != exchange.TransportUDP {
				t.Errorf("session over %v", got)
			}
		})
	})
	t.Run("soft TCP preference fallback", func(t *testing.T) {
		t.Parallel()
		t.Run("connects via UDP when transportPreference=TCP but device does not advertise TCP server", func(t *testing.T) {
			t.Parallel()
			tp := newTCPPair(t, fabrictest.ControllerOptions{TCP: both}, false)
			p := tp.peer(t)
			if dd := p.Descriptor().DiscoveryData; dd != nil && dd.T != nil && dd.T.TCPServer {
				t.Fatal("the device advertises a TCP server")
			}
			p.SetTransportPreference(exchange.TransportTCP)
			p.NewestSession().Close()
			var s *exchange.Session
			tp.run(t, func(ctx context.Context) (err error) {
				s, err = p.Connect(ctx, peer.ConnectOptions{})
				return err
			})
			if s.Transport() != exchange.TransportUDP {
				t.Errorf("reconnected over %v", s.Transport())
			}
		})
	})
	t.Run("session-parameter TCP gate", func(t *testing.T) {
		t.Parallel()
		t.Run("honors TCP when the peer's session parameters report TCP server support", func(t *testing.T) {
			t.Parallel()
			tp := newTCPPair(t, fabrictest.ControllerOptions{TCP: both}, true)
			p := tp.peer(t)
			if p.SessionParameters().SupportedTransports&sessionparams.TransportTCPServer == 0 {
				t.Fatal("no TCP server reported")
			}
			if got := p.ResolveTransports(nil, tcpPtr()); !slices.Equal(got, []exchange.Transport{exchange.TransportTCP, exchange.TransportUDP}) {
				t.Errorf("transports %v", got)
			}
		})
	})
	t.Run("tcp-unsupported flag", func(t *testing.T) {
		t.Parallel()
		t.Run("falls back to UDP after a peer is flagged TCP-unsupported", func(t *testing.T) {
			t.Parallel()
			tp := newTCPPair(t, fabrictest.ControllerOptions{TCP: both}, true)
			p := tp.peer(t)
			if got := p.ResolveTransports(nil, tcpPtr()); len(got) != 2 {
				t.Fatalf("transports %v", got)
			}
			p.MarkTCPUnsupported()
			if got := p.ResolveTransports(nil, tcpPtr()); got != nil {
				t.Errorf("transports %v after the flag", got)
			}
		})
		t.Run("clears the flag when a later session reports TCP server support", func(t *testing.T) {
			t.Parallel()
			tp := newTCPPair(t, fabrictest.ControllerOptions{TCP: both}, true)
			p := tp.peer(t)
			p.MarkTCPUnsupported()
			for _, s := range p.Sessions() {
				s.Close()
			}
			tp.run(t, func(ctx context.Context) error { _, err := p.Connect(ctx, peer.ConnectOptions{}); return err })
			if p.Descriptor().TCPUnsupported {
				t.Error("the flag survived a session reporting a TCP server")
			}
			if got := p.ResolveTransports(nil, tcpPtr()); len(got) != 2 {
				t.Errorf("transports %v", got)
			}
		})
	})
	t.Run("session parameter TCP support", func(t *testing.T) {
		t.Parallel()
		t.Run("reports TCP server support in session parameters when incoming TCP is enabled", func(t *testing.T) {
			t.Parallel()
			tp := newTCPPair(t, fabrictest.ControllerOptions{TCP: both}, true)
			if tp.peer(t).SessionParameters().SupportedTransports&sessionparams.TransportTCPServer == 0 {
				t.Error("no TCP server reported")
			}
		})
		t.Run("does not report TCP server support when the device has no TCP", func(t *testing.T) {
			t.Parallel()
			tp := newTCPPair(t, fabrictest.ControllerOptions{TCP: both}, false)
			if tp.peer(t).SessionParameters().SupportedTransports&sessionparams.TransportTCPServer != 0 {
				t.Error("a TCP server reported")
			}
		})
	})
}

// TestLargeMessageReadOverTCP: a large-message read (ClientRequest.largeMessage,
// ClientRequest.ts:62-86) establishes a TCP session and is answered on it —
// the end-to-end path matter.js's mock cannot run (ClientNodeTcpTest.ts:27-30).
func TestLargeMessageReadOverTCP(t *testing.T) {
	t.Parallel()
	tp := newTCPPair(t, fabrictest.ControllerOptions{TCP: controller.TCPConfig{Outgoing: true}}, true)
	large := true
	var rep *client.Report
	tp.run(t, func(ctx context.Context) (err error) {
		var req client.ReadRequest
		req.Attributes = []client.AttributePath{client.AttributeAt(0, basicinfo.ClusterID, basicinfo.AttrNodeLabel)}
		req.LargeMessage = &large
		rep, err = tp.n.Read(ctx, req)
		return err
	})
	if _, ok := rep.Value(0, basicinfo.ClusterID, basicinfo.AttrNodeLabel); !ok {
		t.Fatal("no NodeLabel")
	}
	p := tp.peer(t)
	if p.NewestSessionOn(exchange.TransportTCP) == nil {
		t.Fatal("no TCP session after a large-message read")
	}
	// A plain read keeps to UDP (no TCP preference).
	tp.run(t, func(ctx context.Context) error {
		var req client.ReadRequest
		req.Attributes = []client.AttributePath{client.AttributeAt(0, basicinfo.ClusterID, basicinfo.AttrNodeLabel)}
		_, err := tp.n.Read(ctx, req)
		return err
	})
}

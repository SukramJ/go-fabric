// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package interaction

// Ports of matter.js at 862ad75f:
// packages/protocol/test/action/client/LargeMessageTransportTest.ts (the
// largeMessage flag of a read requires TCP; ClientInteraction.ts:1235) and
// the explicit-flag cases of packages/protocol/test/action/InvokeLargeMessageTest.ts
// (an invoke's flag defaults off, reaches its exchange and every part a
// split sends, ClientInteraction.ts:716-737). A recording provider stands
// in for matter.js's: it records each exchange's options and refuses the
// exchange, ending the interaction.

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/client/exchange"
	"github.com/SukramJ/go-fabric/im"
)

var errRecorded = errors.New("recorded")

type recordingProvider struct {
	mu        sync.Mutex
	requested []ExchangeOptions
}

func (p *recordingProvider) InitiateExchange(_ context.Context, o ExchangeOptions) (*exchange.Exchange, error) {
	p.mu.Lock()
	p.requested = append(p.requested, o)
	p.mu.Unlock()
	return nil, errRecorded
}

func (p *recordingProvider) PeerAddress() (exchange.PeerAddress, bool) {
	return exchange.PeerAddress{FabricIndex: 1, NodeID: 1}, true
}
func (p *recordingProvider) MaximumPeerResponseTime(time.Duration, bool) time.Duration { return 0 }
func (p *recordingProvider) VerifyReachability(context.Context) bool                   { return true }
func (p *recordingProvider) ReadPathsSupported() int                                   { return 9 }
func (p *recordingProvider) SubscribePathsSupported() int                              { return 3 }
func (p *recordingProvider) Session() *exchange.Session                                { return nil }

func (p *recordingProvider) Manager() *exchange.Manager { return nil }

func (p *recordingProvider) required() []*exchange.Transport {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]*exchange.Transport, len(p.requested))
	for i, o := range p.requested {
		out[i] = o.RequiredTransport
	}
	return out
}

func isTCP(t *exchange.Transport) bool { return t != nil && *t == exchange.TransportTCP }

func TestLargeMessageTransportParity(t *testing.T) {
	t.Parallel()
	yes, no := true, false
	read := func(t *testing.T, large *bool) []*exchange.Transport {
		t.Helper()
		p := &recordingProvider{}
		c := Bind(p, Config{})
		req := ReadRequest{Attributes: []im.ConcreteAttributePath{{Endpoint: 0, Cluster: 0x28, Attribute: 1, HasEndpoint: true, HasCluster: true, HasAttribute: true}}}
		req.LargeMessage = large
		if _, err := c.ReadWith(context.Background(), req); !errors.Is(err, errRecorded) {
			t.Fatalf("read: %v", err)
		}
		return p.required()
	}
	t.Run("requires TCP for a read that asks for a large payload", func(t *testing.T) {
		t.Parallel()
		if r := read(t, &yes); len(r) != 1 || !isTCP(r[0]) {
			t.Errorf("required %v", r)
		}
	})
	t.Run("requires no particular transport for a read that does not", func(t *testing.T) {
		t.Parallel()
		if r := read(t, nil); len(r) != 1 || r[0] != nil {
			t.Errorf("required %v", r)
		}
	})
	t.Run("requires no particular transport when the flag is explicitly false", func(t *testing.T) {
		t.Parallel()
		if r := read(t, &no); len(r) != 1 || r[0] != nil {
			t.Errorf("required %v", r)
		}
	})
}

func TestInvokeLargeMessageParity(t *testing.T) {
	t.Parallel()
	yes := true
	toggle := im.ConcreteCommandPath{Endpoint: 1, Cluster: 6, Command: 2, HasEndpoint: true, HasCluster: true, HasCommand: true}
	t.Run("ClientInvoke largeMessage property", func(t *testing.T) {
		t.Parallel()
		t.Run("Invoke factory does not set largeMessage by default", func(t *testing.T) {
			if (InvokeOptions{}).LargeMessage != nil {
				t.Error("set by default")
			}
		})
		t.Run("largeMessage can be set on an Invoke result", func(t *testing.T) {
			var o InvokeOptions
			o.LargeMessage = &yes
			if !o.large() {
				t.Error("not set")
			}
		})
	})
	t.Run("ClientInteraction.invoke routing decision", func(t *testing.T) {
		t.Parallel()
		t.Run("routes a single L command to #invokeSingle", func(t *testing.T) {
			p := &recordingProvider{}
			var o InvokeOptions
			o.LargeMessage = &yes
			if _, err := Bind(p, Config{}).Invoke(context.Background(), toggle, nil, o); !errors.Is(err, errRecorded) {
				t.Fatal(err)
			}
			if r := p.required(); len(r) != 1 || !isTCP(r[0]) {
				t.Errorf("required %v", r)
			}
		})
		t.Run("splits a multi-command L invoke when count exceeds MaxPathsPerInvoke", func(t *testing.T) {
			p := &recordingProvider{}
			var o InvokeOptions
			o.LargeMessage = &yes
			cmds := []CommandRequest{{Path: toggle}, {Path: toggle}, {Path: toggle}}
			if _, err := Bind(p, Config{}).InvokeBatch(context.Background(), cmds, 2, o); !errors.Is(err, errRecorded) {
				t.Fatal(err)
			}
			r := p.required()
			if len(r) != 2 || !isTCP(r[0]) || !isTCP(r[1]) {
				t.Errorf("required %v, want TCP on both parts", r)
			}
		})
	})
}

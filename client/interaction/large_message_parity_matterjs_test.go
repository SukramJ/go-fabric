// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package interaction

// Ports of matter.js at 862ad75f:
// packages/protocol/test/action/client/LargeMessageTransportTest.ts (the
// largeMessage flag of a read requires TCP; ClientInteraction.ts:1235) and
// the explicit-flag cases of packages/protocol/test/action/InvokeLargeMessageTest.ts
// (an invoke's flag defaults off, reaches its exchange and every part a
// split sends, ClientInteraction.ts:716-737) and its inference cases (an
// undecided invoke of a command with the "L" quality carries the flag,
// :111-127, :716-719). A recording provider stands
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

// The inference cases of InvokeLargeMessageTest.ts ("inferLargeMessage",
// "ClientInteraction.invoke inference policy") at 862ad75f: an invoke the
// caller left undecided carries LargeMessage when any of its commands has
// the "L" quality in the snapshot (ClientInteraction.ts:111-127,
// :716-719), and the inferred flag requires TCP on the exchange and on
// every part of a split.
func TestInferLargeMessageParity(t *testing.T) {
	t.Parallel()
	no := false
	toggle := im.ConcreteCommandPath{Endpoint: 1, Cluster: 6, Command: 2, HasEndpoint: true, HasCluster: true, HasCommand: true}
	// WebRtcTransportProvider.ProvideOffer, the "L" command the matter.js
	// cases use (web-rtc-transport-provider.element.ts:76).
	provideOffer := im.ConcreteCommandPath{Endpoint: 2, Cluster: 0x0553, Command: 0x02, HasEndpoint: true, HasCluster: true, HasCommand: true}
	// CameraAvStreamManagement.CaptureSnapshot
	// (camera-av-stream-management.element.ts:299).
	captureSnapshot := im.ConcreteCommandPath{Endpoint: 1, Cluster: 0x0551, Command: 0x0C, HasEndpoint: true, HasCluster: true, HasCommand: true}
	inferred := func(paths ...im.ConcreteCommandPath) bool {
		return (InvokeOptions{}).inferLargeMessage(paths...).large()
	}
	t.Run("inferLargeMessage", func(t *testing.T) {
		t.Parallel()
		t.Run("detects L quality on a concrete command request", func(t *testing.T) {
			if !inferred(provideOffer) {
				t.Error("not inferred")
			}
		})
		t.Run("returns false for a command without L quality", func(t *testing.T) {
			if inferred(toggle) {
				t.Error("inferred")
			}
		})
		t.Run("flags the whole invoke when any one command carries L quality", func(t *testing.T) {
			if !inferred(toggle, provideOffer) {
				t.Error("not inferred")
			}
		})
		t.Run("swallows MalformedRequestError when cluster/command unresolved", func(t *testing.T) {
			// A path the snapshot does not model infers nothing and fails
			// nothing; the exchange reports what the peer makes of it.
			unknown := im.ConcreteCommandPath{Endpoint: 1, Cluster: 0x1234, Command: 0xFF, HasEndpoint: true, HasCluster: true, HasCommand: true}
			if inferred(unknown) {
				t.Error("inferred")
			}
		})
	})
	t.Run("ClientInteraction.invoke inference policy", func(t *testing.T) {
		t.Parallel()
		t.Run("preserves an explicit largeMessage:false on a command with L quality", func(t *testing.T) {
			var o InvokeOptions
			o.LargeMessage = &no
			got := o.inferLargeMessage(provideOffer)
			if got.LargeMessage != &no || *got.LargeMessage {
				t.Errorf("LargeMessage %v, want the caller's false", got.LargeMessage)
			}
		})
	})
	t.Run("an inferred large message requires TCP", func(t *testing.T) {
		t.Parallel()
		t.Run("for a single L command the caller left undecided", func(t *testing.T) {
			p := &recordingProvider{}
			if _, err := Bind(p, Config{}).Invoke(context.Background(), captureSnapshot, nil, InvokeOptions{}); !errors.Is(err, errRecorded) {
				t.Fatal(err)
			}
			if r := p.required(); len(r) != 1 || !isTCP(r[0]) {
				t.Errorf("required %v, want TCP as for LargeMessage true", r)
			}
		})
		t.Run("for no command of an invoke the caller set to false", func(t *testing.T) {
			p := &recordingProvider{}
			var o InvokeOptions
			o.LargeMessage = &no
			if _, err := Bind(p, Config{}).Invoke(context.Background(), captureSnapshot, nil, o); !errors.Is(err, errRecorded) {
				t.Fatal(err)
			}
			if r := p.required(); len(r) != 1 || r[0] != nil {
				t.Errorf("required %v, want none", r)
			}
		})
		t.Run("for every part of a split invoke when one command carries L", func(t *testing.T) {
			p := &recordingProvider{}
			cmds := []CommandRequest{{Path: toggle}, {Path: captureSnapshot}}
			if _, err := Bind(p, Config{}).InvokeBatch(context.Background(), cmds, 1, InvokeOptions{}); !errors.Is(err, errRecorded) {
				t.Fatal(err)
			}
			r := p.required()
			if len(r) != 2 || !isTCP(r[0]) || !isTCP(r[1]) {
				t.Errorf("required %v, want TCP on both parts", r)
			}
		})
		t.Run("for no command without L", func(t *testing.T) {
			p := &recordingProvider{}
			if _, err := Bind(p, Config{}).Invoke(context.Background(), toggle, nil, InvokeOptions{}); !errors.Is(err, errRecorded) {
				t.Fatal(err)
			}
			if r := p.required(); len(r) != 1 || r[0] != nil {
				t.Errorf("required %v, want none", r)
			}
		})
	})
}

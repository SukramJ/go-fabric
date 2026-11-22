// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package commissioner_test

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"

	"thingfabric.dev/go-fabric/client/exchange"
	"thingfabric.dev/go-fabric/client/interaction"
	"thingfabric.dev/go-fabric/client/peer"
	"thingfabric.dev/go-fabric/controller/client"
	"thingfabric.dev/go-fabric/controller/commissioner"
	"thingfabric.dev/go-fabric/fabrictest"
	"thingfabric.dev/go-fabric/im"
)

// TestCertificateUpdateForceCloseCauseIsErrFabricChanged holds
// F-b25ed960-C5 (a Go case, not a port): a host's read pending on the
// peer while the certificate update force-closes the peer's sessions
// fails with an error a host can match as commissioner.ErrFabricChanged —
// matter.js's FabricChangedError, exported from @matter/protocol — and
// not as transient, as matter.js's class extends PeerCommunicationError
// (packages/protocol/src/peer/PeerCommunicationError.ts:105-112 at
// b25ed960). The read is held pending by dropping the device's datagrams
// from its request on until the disconnect has run.
func TestCertificateUpdateForceCloseCauseIsErrFabricChanged(t *testing.T) {
	p := newOCUPair(t, ocuOptions{})
	dev := p.pair.Device.Addr()
	same := func(a, b *net.UDPAddr) bool { return a != nil && b != nil && a.IP.Equal(b.IP) && a.Port == b.Port }
	readErr := make(chan error, 1)
	var once sync.Once
	p.patch(func(s commissioner.NOCUpdateSeam) commissioner.NOCUpdateSeam {
		next := s.Disconnect
		s.Disconnect = func(ctx context.Context, pr *peer.Peer, cause error) {
			first := false
			once.Do(func() { first = true })
			if !first {
				next(ctx, pr, cause)
				return
			}
			sent := make(chan struct{})
			var sentOnce sync.Once
			p.site.Net().Filter(func(d fabrictest.Datagram) bool {
				if same(d.To, dev) {
					sentOnce.Do(func() { close(sent) })
				}
				return !same(d.From, dev)
			})
			go func() {
				_, err := p.node.Read(context.WithoutCancel(ctx), client.ReadRequest{
					ReadRequest:          interaction.ReadRequest{Attributes: []im.ConcreteAttributePath{{Endpoint: 0, HasEndpoint: true}}},
					IncludeKnownVersions: true,
				})
				readErr <- err
			}()
			<-sent
			next(ctx, pr, cause)
			p.site.Net().Filter(nil)
		}
		return s
	})
	p.update()
	var err error
	select {
	case err = <-readErr:
	case <-ocuCtx(t).Done():
		t.Fatal("the pending read did not end")
	}
	if !errors.Is(err, commissioner.ErrFabricChanged) || errors.Is(err, exchange.ErrTransient) {
		t.Fatalf("pending read = %v (ErrFabricChanged %v, transient %v); want ErrFabricChanged, not transient",
			err, errors.Is(err, commissioner.ErrFabricChanged), errors.Is(err, exchange.ErrTransient))
	}
}

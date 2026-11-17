// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/secure/operational"
)

// TestInitiatedSessionKeepsItsTransport: a session this node established as
// the CASE initiator is bound to its UDP route under the rule every bound
// session follows — it never moves between transports — so a message of
// the session that arrives over a connection before the peer's first one
// does not rebind it there (review II, R2-P6-conc-0314).
func TestInitiatedSessionKeepsItsTransport(t *testing.T) {
	fab := newResumeFabric(t)
	peer := newFakeController(t, fab)
	dev := startResumeDevice(t, newMemSubscriptionStore(), func(br *Bridge, sessions *operational.Manager) {
		br.AttachCaseInitiatorProvider(resumeInitiatorProvider(fab, sessions))
		br.AttachOperationalResolver(staticResolver{addr: peer.addr})
	})
	peer.setBridge(dev.addr(t))
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	s, err := dev.br.ConnectPeer(ctx, resumeFabricIndex, resumeCtrlNode)
	if err != nil {
		t.Fatalf("CASE to the peer: %v", err)
	}
	sid := s.link.sessionID
	conn := dev.br.routeFor(&fakeConn{})
	dev.br.bindSessionRoute(sid, conn)
	raw, _ := dev.br.sessionPeerAddrs.Load(sid)
	if _, udp := raw.(*net.UDPAddr); !udp {
		t.Fatalf("the initiated session's route is %v (%T), want its UDP address", raw, raw)
	}
}

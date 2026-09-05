// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package bridge

// This file installs the module-internal seam that
// [github.com/SukramJ/go-fabric/bridge/bridgetest] reads. It declares no
// exported identifier on purpose: the affordances an external test needs —
// establishing a subscription's reply route without wire traffic, anchoring
// the peer's receive window on the handshake it never sent, driving one pump
// tick — are test support, and test support that lives on *Bridge is
// production API that every consumer sees and some consumer eventually
// depends on. See the bridgeseam package doc for why the indirection is what
// it is.

import (
	"net"
	"time"

	"github.com/SukramJ/go-fabric/internal/bridgeseam"
	"github.com/SukramJ/go-fabric/internal/channelseam"
	"github.com/SukramJ/go-fabric/transport/message"
)

func init() {
	bridgeseam.CaptureSubTarget = func(
		b any,
		subID uint32,
		src *net.UDPAddr,
		hdr *message.Header,
		proto message.ProtocolHeader,
		fabricFiltered bool,
	) bool {
		br, ok := b.(*Bridge)
		if !ok || br == nil {
			return false
		}
		br.captureSubTarget(subID, src, hdr, proto, fabricFiltered)
		// captureSubTarget drops silently on arguments it cannot route,
		// so the presence of the entry — not the fact that the call
		// returned — is what the caller is told about.
		_, stored := br.routing.subTargets.Load(subID)
		return stored
	}

	bridgeseam.AnchorPeerCounter = func(b any, sessionID uint16, peerNodeID uint64, counter uint32) bool {
		br, ok := b.(*Bridge)
		if !ok || br == nil {
			return false
		}
		if sessionID == 0 {
			// Unsecured traffic is deduplicated per source node id, and
			// without one the receive path has nothing stable to key on
			// and treats every frame as fresh — so there is no window to
			// anchor either.
			if peerNodeID == 0 {
				return false
			}
			w := br.unsecuredWindow(peerNodeID)
			return w != nil && w.Accept(counter)
		}
		br.mu.RLock()
		lookup := br.sessions
		br.mu.RUnlock()
		if lookup == nil {
			return false
		}
		sess, found := lookup.Lookup(sessionID)
		if !found || sess == nil {
			return false
		}
		// The window lives inside the session and is written by decrypt
		// alone; channelseam is the module-internal way in. The hook is
		// installed by secure/channel's init, which is linked in because
		// this package depends on the session type — the nil guard is for
		// the impossible case, not an expected one.
		anchor := channelseam.AnchorPeerCounter
		return anchor != nil && anchor(sess, counter)
	}

	bridgeseam.AckPumpTick = func(b any, now time.Time) (int, bool) {
		br, ok := b.(*Bridge)
		if !ok || br == nil {
			return 0, false
		}
		br.mu.RLock()
		tracker := br.ackTracker
		outbound := br.outboundReliable
		br.mu.RUnlock()
		return br.ackPumpTick(tracker, outbound, now), true
	}
}

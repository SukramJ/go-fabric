// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package bridge

// This file installs the module-internal seam that
// [github.com/SukramJ/go-fabric/bridge/bridgetest] reads. It declares no
// exported identifier on purpose: the affordances an external test needs —
// establishing a subscription's reply route without wire traffic, driving
// one pump tick — are test support, and test support that lives on
// *Bridge is production API that every consumer sees and some consumer
// eventually depends on. See the bridgeseam package doc for why the
// indirection is what it is.

import (
	"net"
	"time"

	"github.com/SukramJ/go-fabric/internal/bridgeseam"
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

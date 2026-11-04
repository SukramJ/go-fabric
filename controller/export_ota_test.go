// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controller

import (
	"github.com/SukramJ/go-fabric/client/exchange"
	"github.com/SukramJ/go-fabric/client/peer"
)

// ArmerObserver is the reboot armer's test seam (armerObserver, ota.go).
type ArmerObserver = armerObserver

// ObserveArmer installs o on c's reboot armer.
func ObserveArmer(c *Controller, o *ArmerObserver) { c.armerObs.Store(o) }

// ArmerSessionOf is the exchange session an observed armer session wraps.
func ArmerSessionOf(s peer.ArmerSession) *exchange.Session {
	if a, ok := s.(armerSession); ok {
		return a.s
	}
	return nil
}

// PeerSessions are c's live sessions with p (SessionManager.sessions of
// the peer).
func PeerSessions(c *Controller, p exchange.PeerAddress) []*exchange.Session {
	if pp := c.peers.Get(p); pp != nil {
		return pp.Sessions()
	}
	return nil
}

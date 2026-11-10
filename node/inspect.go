// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

import (
	"time"

	"github.com/SukramJ/go-fabric/clock"
	"github.com/SukramJ/go-fabric/diag"
	"github.com/SukramJ/go-fabric/secure/operational"
)

// SetupPayload is the node's configured commissioning secret with the
// identity it is encoded under — the fields of the setup payload and the
// two codes encoded from them.
type SetupPayload struct {
	// Discriminator is the configured 12-bit discriminator
	// ([Config].Commissioning).
	Discriminator uint16
	// Passcode is the configured setup passcode. It is in the payload by
	// design: whoever reads it can commission the node while its own
	// window is open, so a host serves it to an administrator only.
	Passcode uint32
	// VendorID and ProductID are the node's ([Config].ProductDescription).
	VendorID, ProductID uint16
	// QR is the QR code payload ("MT:…"), Manual the manual pairing code;
	// both encoded from Discriminator and Passcode.
	QR, Manual string
}

// SetupPayload returns the configured setup payload: passcode,
// discriminator, vendor and product id, and the QR and manual codes
// encoded from them. It carries the passcode (see
// [SetupPayload].Passcode). A window opened with other credentials
// ([Node.OpenBasicWindow]) has its codes from [Node.PairingCodesFor].
//
// Mirrors matter.js CommissioningServer's state — passcode, discriminator
// and the pairingCodes property derived from them with BasicInformation's
// vendorId and productId
// (packages/node/src/behavior/system/commissioning/CommissioningServer.ts:
// 405-414, :417-450 at 862ad75f).
func (n *Node) SetupPayload() (SetupPayload, error) {
	cm, pd := n.cfg.Commissioning, n.cfg.ProductDescription
	qr, manual, err := pairingCodes(pd, cm.Discriminator, cm.Passcode)
	if err != nil {
		return SetupPayload{}, err
	}
	return SetupPayload{
		Discriminator: cm.Discriminator,
		Passcode:      cm.Passcode,
		VendorID:      pd.VendorID,
		ProductID:     pd.ProductID,
		QR:            qr,
		Manual:        manual,
	}, nil
}

// SessionDiagnostics is a read-only snapshot of the node's secure
// sessions and of its session table — what [Node.SessionDiagnostics]
// returns ([diag.SessionDiagnostics], shared with the controller; Peers
// is nil on the node).
type SessionDiagnostics = diag.SessionDiagnostics

// SessionInfo is one open PASE or CASE session ([diag.SessionInfo]).
// The node leaves CreatedAt, Resumed, Transport and PeerLost zero.
type SessionInfo = diag.SessionInfo

// Occupancy is how much of the session-id space the node holds
// ([diag.Occupancy]).
type Occupancy = diag.Occupancy

// SessionDiagnostics returns the node's sessions, each with its
// subscription count and idle ages on the node's clock, and the session
// table's occupancy — the read-only view of what [Node.Sessions] and
// [Node.Subscriptions] hold, through which a host neither closes a
// session nor a subscription. It reads operational.Manager.Sessions and
// Occupancy and subscription.Manager.Snapshot, whose ids it groups by
// session.
//
// matter.js exposes the same per-session facts on its sessions
// (packages/protocol/src/session/NodeSession.ts:183 `subscriptions`,
// :238 `fabric`, :250 `id`, :270 `peerNodeId`; the activity timestamps
// `timestamp` and `activeTimestamp` of
// packages/protocol/src/session/Session.ts:56, :65, :142-146 at
// 862ad75f); the occupancy split is this module's
// (operational.SessionTableOccupancy).
func (n *Node) SessionDiagnostics() SessionDiagnostics {
	now := n.Clock().Now()
	out := SessionDiagnostics{AsOf: now}
	if n.sec == nil || n.sec.sessions == nil {
		return out
	}
	subs := n.subscriptionIDs()
	for _, s := range n.sec.sessions.Sessions() {
		out.Sessions = append(out.Sessions, sessionInfoOf(s, subs[s.SessionID], now))
	}
	occ := n.sec.sessions.Occupancy()
	out.Occupancy = Occupancy{Live: occ.Live, Reserved: occ.Reserved, Capacity: occ.Capacity, Free: occ.Free()}
	return out
}

// subscriptionIDs are the ids of the node's subscriptions by session,
// each list ascending (Snapshot lists by id).
func (n *Node) subscriptionIDs() map[uint16][]uint32 {
	if n.sec == nil || n.sec.subs == nil {
		return nil
	}
	out := map[uint16][]uint32{}
	for _, sub := range n.sec.subs.Snapshot() {
		out[sub.SessionID] = append(out[sub.SessionID], sub.ID)
	}
	return out
}

// sessionInfoOf is one session of the table as the facade reports it,
// with the subscriptions subs riding on it, its ages at now.
func sessionInfoOf(s operational.SessionInfo, subs []uint32, now time.Time) SessionInfo {
	return SessionInfo{
		ID:               s.SessionID,
		FabricIndex:      s.FabricIndex,
		PeerNodeID:       s.PeerNodeID,
		LocalNodeID:      s.LocalNodeID,
		PASE:             s.IsPASE,
		Subscriptions:    len(subs),
		SubscriptionIDs:  subs,
		LastActivity:     s.LastActivity,
		LastPeerActivity: s.LastPeerActivity,
		Idle:             ageAt(now, s.LastActivity),
		PeerIdle:         ageAt(now, s.LastPeerActivity),

		PeerIdleInterval:    time.Duration(s.PeerIdleIntervalMs) * time.Millisecond,
		PeerActiveInterval:  time.Duration(s.PeerActiveIntervalMs) * time.Millisecond,
		PeerActiveThreshold: time.Duration(s.PeerActiveThresholdMs) * time.Millisecond,
	}
}

// ageAt is now minus t, 0 for a zero t or a t after now.
func ageAt(now, t time.Time) time.Duration {
	if t.IsZero() || t.After(now) {
		return 0
	}
	return now.Sub(t)
}

// Clock returns the node's time source: [Config].Clock, or [clock.System]
// when it was nil. Every interval the node keeps runs on it — session
// activity, the commissioning window's deadline, subscriptions — so a
// host computes ages against it rather than wall time.
func (n *Node) Clock() clock.Clock { return clock.Or(n.cfg.Clock) }

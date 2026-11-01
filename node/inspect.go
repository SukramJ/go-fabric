// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

import (
	"time"

	"github.com/SukramJ/go-fabric/clock"
)

// SetupPayload is the node's configured commissioning secret with the
// identity it is encoded under — the fields of the setup payload and the
// two codes [Node.PairingCodes] returns for it.
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
	// both what [Node.PairingCodes] returns.
	QR, Manual string
}

// SetupPayload returns the configured setup payload: passcode,
// discriminator, vendor and product id, and the QR and manual codes
// [Node.PairingCodes] encodes from them. It carries the passcode (see
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
// returns.
type SessionDiagnostics struct {
	// AsOf is when the snapshot was taken, on the node's clock
	// ([Node.Clock]); every age in Sessions is measured against it.
	AsOf time.Time
	// Sessions are the open sessions, by session id.
	Sessions []SessionInfo
	// Occupancy is the session table's id usage.
	Occupancy Occupancy
}

// SessionInfo is one open PASE or CASE session.
type SessionInfo struct {
	// ID is the local session id.
	ID uint16
	// FabricIndex is the session's fabric; 0 for a PASE session before
	// AddNOC.
	FabricIndex uint8
	// PeerNodeID and LocalNodeID are the session's two node ids.
	PeerNodeID, LocalNodeID uint64
	// PASE is true for a commissioning (PASE) session, false for CASE.
	PASE bool
	// Subscriptions counts the subscriptions riding on the session.
	Subscriptions int
	// LastActivity is when the session last carried traffic in either
	// direction, zero when none yet; LastPeerActivity when the peer last
	// sent, counted from the session's creation (the peer is active
	// then: matter.js NodeSession.ts:120, chip SecureSession.h:349).
	LastActivity, LastPeerActivity time.Time
	// Idle and PeerIdle are AsOf minus LastActivity and LastPeerActivity;
	// 0 when the timestamp is zero.
	Idle, PeerIdle time.Duration
	// PeerIdleInterval, PeerActiveInterval and PeerActiveThreshold are the
	// session intervals the peer reported in its handshake (PASE
	// PBKDFParamRequest or CASE Sigma1 SessionParameters, also on a resumed
	// session); 0 when it reported none. The device's record of what the
	// controller sent (matter.js NodeSession.reportedParameters).
	PeerIdleInterval, PeerActiveInterval, PeerActiveThreshold time.Duration
}

// Occupancy is how much of the session-id space the node holds.
type Occupancy struct {
	// Live counts sessions with key material installed.
	Live int
	// Reserved counts ids a handshake staked that has not completed.
	Reserved int
	// Capacity is the size of the id space.
	Capacity int
	// Free is Capacity minus Live and Reserved, never negative.
	Free int
}

// SessionDiagnostics returns the node's sessions, each with its
// subscription count and idle ages on the node's clock, and the session
// table's occupancy — the read-only view of what [Node.Sessions] and
// [Node.Subscriptions] hold, through which a host neither closes a
// session nor a subscription. It reads operational.Manager.Sessions and
// Occupancy and subscription.Manager.CountBySession.
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
	var subs map[uint16]int
	if n.sec.subs != nil {
		subs = n.sec.subs.CountBySession()
	}
	for _, s := range n.sec.sessions.Sessions() {
		out.Sessions = append(out.Sessions, SessionInfo{
			ID:               s.SessionID,
			FabricIndex:      s.FabricIndex,
			PeerNodeID:       s.PeerNodeID,
			LocalNodeID:      s.LocalNodeID,
			PASE:             s.IsPASE,
			Subscriptions:    subs[s.SessionID],
			LastActivity:     s.LastActivity,
			LastPeerActivity: s.LastPeerActivity,
			Idle:             ageAt(now, s.LastActivity),
			PeerIdle:         ageAt(now, s.LastPeerActivity),

			PeerIdleInterval:    time.Duration(s.PeerIdleIntervalMs) * time.Millisecond,
			PeerActiveInterval:  time.Duration(s.PeerActiveIntervalMs) * time.Millisecond,
			PeerActiveThreshold: time.Duration(s.PeerActiveThresholdMs) * time.Millisecond,
		})
	}
	occ := n.sec.sessions.Occupancy()
	out.Occupancy = Occupancy{Live: occ.Live, Reserved: occ.Reserved, Capacity: occ.Capacity, Free: occ.Free()}
	return out
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

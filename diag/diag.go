// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package diag holds the diagnostic shapes both roles serve: the
// session snapshot ([SessionDiagnostics]), the trace's event ([Event])
// and the trace's configuration ([Diagnostics]). A node
// (node.Node.SessionDiagnostics, DiagnosticEvents) and a controller
// (controller.Controller.SessionDiagnostics, DiagnosticEvents) return
// the same types, so a host renders one page model for either role; node
// keeps its former names as aliases of these. The mDNS findings both
// roles serve are mdns.Finding.
package diag

import (
	"time"

	"github.com/SukramJ/go-fabric/diagevent"
)

// Event is one entry of a diagnostic trace ([diagevent.Event]): the
// moments that explain a failed pairing or a lost session, with an
// operator sentence and the identifiers in Detail.
type Event = diagevent.Event

// Diagnostics configures a role's diagnostic trace: a bounded in-memory
// ring of the moments that explain a failed pairing or a lost session
// ([Event]). The ring is memory only; the logs are the durable record.
type Diagnostics struct {
	// Capacity is the number of events kept, the oldest dropped first; 0
	// keeps [DefaultDiagnosticsCapacity].
	Capacity int
}

// DefaultDiagnosticsCapacity is the trace's capacity when
// [Diagnostics].Capacity is 0.
const DefaultDiagnosticsCapacity = 256

// Effective is the ring's capacity: Capacity, or
// [DefaultDiagnosticsCapacity] below one.
func (d Diagnostics) Effective() int {
	if d.Capacity < 1 {
		return DefaultDiagnosticsCapacity
	}
	return d.Capacity
}

// SessionDiagnostics is a read-only snapshot of a role's secure sessions
// — what node.Node.SessionDiagnostics and
// controller.Controller.SessionDiagnostics return.
type SessionDiagnostics struct {
	// AsOf is when the snapshot was taken, on the role's clock; every age
	// in Sessions and Peers is measured against it.
	AsOf time.Time
	// Sessions are the open sessions, by session id on the node, by peer
	// then session on the controller.
	Sessions []SessionInfo
	// Occupancy is the session table's id usage. The zero value with
	// Capacity 0 means the role does not track it: the controller's
	// exchange manager keeps no id-occupancy figure.
	Occupancy Occupancy
	// Peers are the controller's peers, one per peer in the order they
	// joined, each referencing its sessions in Sessions by id; nil on the
	// node.
	Peers []PeerInfo
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
	// The controller's subscriptions are kept by peer, not by session, so
	// it reports 0 here.
	Subscriptions int
	// SubscriptionIDs are their ids, ascending; nil for none. The node's
	// subscriptions themselves are listed by Node.Subscriptions().Snapshot.
	SubscriptionIDs []uint32
	// LastActivity is when the session last carried traffic in either
	// direction, zero when none yet — and always zero on the controller,
	// whose exchange sessions export no any-direction timestamp;
	// LastPeerActivity when the peer last sent, counted from the
	// session's creation (the peer is active then: matter.js
	// NodeSession.ts:120, chip SecureSession.h:349).
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
	// CreatedAt is when the session was established; zero on the node.
	CreatedAt time.Time
	// Resumed is true for a session that came from a CASE resumption
	// (Sigma2_Resume); false on the node.
	Resumed bool
	// Transport is the session's carrier: "udp", "tcp" or "ble"; "" when
	// unknown, as on the node.
	Transport string
	// PeerLost is true for a session given up because the peer is gone or
	// closed it, still listed while it closes; false on the node.
	PeerLost bool
}

// Occupancy is how much of the session-id space a role holds.
type Occupancy struct {
	// Live counts sessions with key material installed.
	Live int
	// Reserved counts ids a handshake staked that has not completed.
	Reserved int
	// Capacity is the size of the id space; 0 when the role does not
	// track it (the controller).
	Capacity int
	// Free is Capacity minus Live and Reserved, never negative.
	Free int
}

// PeerInfo is one of the controller's peers: a node on our fabric it
// keeps a connection to, with what its connection process, its DNS-SD
// service and its client node know.
type PeerInfo struct {
	// FabricIndex and NodeID are the peer's address.
	FabricIndex uint8
	NodeID      uint64
	// Registered is true when a commissioned client node of the
	// controller's registry has this address; a peer the host added
	// itself (peer.Set.AddKnownPeer) has none.
	Registered bool
	// State is the client node's connection state as its String names it
	// ("Connected", "Disconnected", "Reconnecting",
	// "WaitingForDeviceDiscovery"); "" without a registered node.
	State string
	// LoadError is why the client node's stored state did not load, ""
	// when it did or without a registered node.
	LoadError string
	// TimeOffline is how long the peer's connection process has been
	// trying; 0 when none runs.
	TimeOffline time.Duration
	// OperationalAddress is the address the peer's sessions use ("" when
	// none is known); OperationalHost the discovered SRV target that owns
	// it ("" when none does).
	OperationalAddress string
	OperationalHost    string
	// TCPUnsupported is true when the peer refused TCP.
	TCPUnsupported bool
	// Reachable is whether the peer's operational service is believed
	// reachable.
	Reachable bool
	// DiscoveredNames are the operational instance names of the peer the
	// DNS-SD cache holds records for; empty when none.
	DiscoveredNames []string
	// LastCheckInAt is the last ICD Check-In the peer sent and
	// NextExpectedCheckIn when the next is due; zero when the peer is no
	// registered LIT ICD or none arrived.
	LastCheckInAt, NextExpectedCheckIn time.Time
	// SubscriptionActive is true while the client node's default
	// subscription is established.
	SubscriptionActive bool
	// SessionIDs are the ids of the peer's sessions, each listed in
	// [SessionDiagnostics].Sessions.
	SessionIDs []uint16
}

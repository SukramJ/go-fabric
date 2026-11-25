// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package peer

import (
	"net"
	"slices"

	"thingfabric.dev/go-fabric/client/discovery"
	"thingfabric.dev/go-fabric/client/handshake"
	"thingfabric.dev/go-fabric/secure/sessionparams"
)

// Descriptor is what is known about a peer — a node on one of our
// fabrics — and what a [Store] keeps of it. It is matter.js PeerDescriptor
// (packages/protocol/src/peer/PeerDescriptor.ts:39-75 at b25ed960).
type Descriptor struct {
	// Address is the peer's logical address: our fabric index and its node
	// id (:40-43).
	Address handshake.PeerAddress
	// DataModelRevision is the data model revision the peer supports, 0
	// when unknown. Nothing sets or reads it.
	//
	// Deprecated: PeerDescriptor has no such member at db021da0; the peer's
	// data model revision is the one its session parameters report — use
	// ReportedSessionParameters.DataModelRevision. Removable in
	// v0.25.0 (review II, R2-P6-par-3351).
	DataModelRevision uint16
	// OperationalAddress is where the peer was last reached, nil when
	// unknown (:45-48). It is kept as a UDP address: matter.js
	// OperationalAddress.from (:22-31) tags an untyped address as UDP; a
	// TCP session's address is recorded with the discovered port, the
	// transport not kept.
	OperationalAddress *net.UDPAddr
	// OperationalHost is the discovered SRV target that owned the
	// session's address when the peer was last reached
	// ([discovery.Address.Host]), empty when unknown. matter.js persists
	// the operational address and not the host it belonged to
	// (PeerDescriptor.ts:48 at b25ed960); the host is this module's, ahead
	// of matter.js: while discovery still advertises it, its addresses are
	// tried first on reconnect (issue 478, a border router answering a
	// node's instance with an SRV to its own host).
	OperationalHost string
	// DiscoveryData is what the peer's operational TXT record announced,
	// nil when nothing is known (:50-53).
	DiscoveryData *discovery.TXTData
	// ReportedSessionParameters is what the peer reported in its most
	// recent session that reported any; a report replaces the previous one
	// as a whole (:55-60).
	ReportedSessionParameters *sessionparams.Parameters
	// TCPUnsupported records a peer that negotiated a TCP session but
	// reported no TCP server: TCP is not attempted again until a later
	// session reports TCP server support (PeerDescriptor.ts:62-66,
	// Peer.ts:353-355 at db021da0).
	TCPUnsupported bool
}

// clone deep-copies d.
func (d Descriptor) clone() Descriptor {
	if d.OperationalAddress != nil {
		a := *d.OperationalAddress
		a.IP = slices.Clone(a.IP)
		d.OperationalAddress = &a
	}
	if d.DiscoveryData != nil {
		d.DiscoveryData = cloneTXTData(d.DiscoveryData)
	}
	if d.ReportedSessionParameters != nil {
		p := *d.ReportedSessionParameters
		d.ReportedSessionParameters = &p
	}
	return d
}

// cloneTXTData deep-copies t: every field is a pointer, and a copy that
// shares one lets a write through it reach the original (review II,
// R2-C2-api-2157).
func cloneTXTData(t *discovery.TXTData) *discovery.TXTData {
	return &discovery.TXTData{
		VP:  clonePtr(t.VP),
		DT:  clonePtr(t.DT),
		DN:  clonePtr(t.DN),
		RI:  clonePtr(t.RI),
		PH:  clonePtr(t.PH),
		PI:  clonePtr(t.PI),
		SII: clonePtr(t.SII),
		SAI: clonePtr(t.SAI),
		SAT: clonePtr(t.SAT),
		T:   clonePtr(t.T),
		ICD: clonePtr(t.ICD),
	}
}

// clonePtr returns a pointer to a copy of *p, nil for nil.
func clonePtr[T any](p *T) *T {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}

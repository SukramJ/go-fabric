// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package peer

import (
	"net"
	"slices"

	"github.com/SukramJ/go-fabric/client/discovery"
	"github.com/SukramJ/go-fabric/client/handshake"
	"github.com/SukramJ/go-fabric/secure/sessionparams"
)

// Descriptor is what is known about a peer — a node on one of our
// fabrics — and what a [Store] keeps of it. It is matter.js PeerDescriptor
// (packages/protocol/src/peer/PeerDescriptor.ts:34-81 at 0ceb202d) without
// the TCP flag (this module has no TCP client).
type Descriptor struct {
	// Address is the peer's logical address: our fabric index and its node
	// id (:40-43).
	Address handshake.PeerAddress
	// DataModelRevision is the data model revision the peer supports, 0
	// when unknown (:45-48).
	DataModelRevision uint16
	// OperationalAddress is where the peer was last reached, nil when
	// unknown (:50-53). It is a UDP address: matter.js
	// OperationalAddress.from (:22-31) tags an untyped address as UDP, and
	// this module speaks UDP only.
	OperationalAddress *net.UDPAddr
	// DiscoveryData is what the peer's operational TXT record announced,
	// nil when nothing is known (:55-58).
	DiscoveryData *discovery.TXTData
	// ReportedSessionParameters is what the peer reported in its most
	// recent session that reported any; a report replaces the previous one
	// as a whole (:60-65).
	ReportedSessionParameters *sessionparams.Parameters
	// CaseAuthenticatedTags are our CATs for CASE sessions to the peer;
	// nil is unknown, as distinct from an empty set (:73-80).
	CaseAuthenticatedTags []uint32
}

// clone deep-copies d.
func (d Descriptor) clone() Descriptor {
	if d.OperationalAddress != nil {
		a := *d.OperationalAddress
		a.IP = slices.Clone(a.IP)
		d.OperationalAddress = &a
	}
	if d.DiscoveryData != nil {
		dd := *d.DiscoveryData
		d.DiscoveryData = &dd
	}
	if d.ReportedSessionParameters != nil {
		p := *d.ReportedSessionParameters
		d.ReportedSessionParameters = &p
	}
	if d.CaseAuthenticatedTags != nil {
		d.CaseAuthenticatedTags = slices.Clone(d.CaseAuthenticatedTags)
	}
	return d
}

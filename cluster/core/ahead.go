// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core

import (
	timesyncdef "github.com/SukramJ/go-fabric/cluster/spec/timesynchronization"
	"github.com/SukramJ/go-fabric/surface"
)

// TimeSynchronizationAhead declares the TimeSynchronization server's
// elements that implement a row of the "Ahead of matter.js" register
// (notes/parity/by_design.md): the features TZ, NTPC, NTPS and TSC
// ([NewTimeSynchronizationWith]), row BD-chip-TimeSynchronization-Features.
// The register's matter.js marker: "`TimeSynchronizationServer.ts:14` is
// the empty subclass (absence: the class line, 14 lines in all)". A node
// that selects one of them enables the row in surface.Policies.Extensions,
// or the composition is refused (surface.ErrExtensionNotEnabled, ADR 0030
// decision 3).
var TimeSynchronizationAhead = surface.AheadSet{
	"BD-chip-TimeSynchronization-Features": {
		Features: uint32(timesyncdef.FeatureTimeZone | timesyncdef.FeatureNtpClient |
			timesyncdef.FeatureNtpServer | timesyncdef.FeatureTimeSyncClient),
	},
}

// NetworkCommissioningAhead declares the NetworkCommissioning server's
// elements that implement a row of the "Ahead of matter.js" register
// (notes/parity/by_design.md): the Thread feature
// ([NetworkCommFeatureThread], [ThreadNetworkCommissioning]), row
// BD-Thread-NetComm-ThreadVariant. The register's matter.js marker:
// "`NetworkCommissioningServer.ts:18` is the empty subclass: no
// network-type branch, no Thread logic (absence: the class line, 18 lines
// in all)".
var NetworkCommissioningAhead = surface.AheadSet{
	"BD-Thread-NetComm-ThreadVariant": {Features: NetworkCommFeatureThread},
}

// GeneralCommissioningAhead declares the GeneralCommissioning server's
// elements that implement a row of the "Ahead of matter.js" register
// (notes/parity/by_design.md): the TermsAndConditions feature
// (general_commissioning_tc.go), row
// BD-chip-GeneralCommissioning-TermsAndConditions. The register's matter.js
// marker: "`GeneralCommissioningServer.ts:31` is the class line (absence:
// no terms-and-conditions text in the 285-line file)".
var GeneralCommissioningAhead = surface.AheadSet{
	"BD-chip-GeneralCommissioning-TermsAndConditions": {Features: gencommFeatureTC},
}

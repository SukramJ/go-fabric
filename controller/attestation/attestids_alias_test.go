// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package attestation

import "thingfabric.dev/go-fabric/internal/attestids"

// The commonName fallback's helpers the parity and unit tests of this
// package name: since R2-P5-port-12 the one implementation of the rule is
// internal/attestids, which secure/attestation reads too.
var (
	fallbackID = attestids.FallbackID
	parseHexID = attestids.ParseHexID
)

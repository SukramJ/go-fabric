// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/im"
)

// diagnosticsClusters are the clusters matter.js gives the Diagnostics (K)
// quality, the set WildcardSkipDiagnosticsClusters skips
// (packages/node/src/node/integration/ProtocolService.ts:428). The schema
// snapshot carries no cluster-level quality, so the set is stated here, one
// line per matter.js element at the pin:
var diagnosticsClusters = map[uint32]struct{}{
	0x0032: {}, // DiagnosticLogs, packages/model/src/standard/elements/diagnostic-logs.element.ts:19
	0x0033: {}, // GeneralDiagnostics, general-diagnostics.element.ts:20
	0x0034: {}, // SoftwareDiagnostics, software-diagnostics.element.ts:20
	0x0035: {}, // ThreadNetworkDiagnostics, thread-network-diagnostics.element.ts:20
	0x0036: {}, // WiFiNetworkDiagnostics, wi-fi-network-diagnostics.element.ts:20
	0x0037: {}, // EthernetNetworkDiagnostics, ethernet-network-diagnostics.element.ts:20
}

// specElementQualities answers [im.ElementQualities] from the generated
// cluster definitions: an attribute is Fixed when its definition says so
// (spec.Quality.Fixed, matter.js effectiveQuality.fixed); a cluster no
// definition is registered for has no Fixed attribute beyond the global
// ones im knows.
type specElementQualities struct{}

var _ im.ElementQualities = specElementQualities{}

func (specElementQualities) DiagnosticsCluster(cluster uint32) bool {
	_, ok := diagnosticsClusters[cluster]
	return ok
}

func (specElementQualities) AttributeFixed(cluster, attribute uint32) bool {
	def := spec.Lookup(cluster)
	if def == nil {
		return false
	}
	a := def.Attribute(attribute)
	return a != nil && a.Quality.Fixed
}

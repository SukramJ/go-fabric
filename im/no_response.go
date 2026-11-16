// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package im

// NoResponse is the response a command handler returns for a command it
// answers with nothing: [HandleInvokeRequest] leaves the command out of
// the InvokeResponse, and an InvokeRequest none of whose commands is
// answered is answered with no message at all (the bridge sends none).
//
// A NetworkCommissioning server returns it for a ConnectNetwork whose
// fail-safe expired while the driver ran: connectedhomeip releases the
// pending command handle on the expiry
// (src/app/clusters/network-commissioning/NetworkCommissioningCluster.cpp:1001-1002
// at 6170af84), and the result that arrives afterwards returns before it
// adds a ConnectNetworkResponse (:845-857). Review II, R2-P6-fsm-1001,
// F-db021da0-10.
type NoResponse struct{}

// isNoResponse reports whether a handler's response is [NoResponse].
func isNoResponse(v any) bool {
	switch v.(type) {
	case NoResponse, *NoResponse:
		return true
	}
	return false
}

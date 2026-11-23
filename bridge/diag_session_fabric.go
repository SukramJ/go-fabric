// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"strconv"

	endpointpkg "thingfabric.dev/go-fabric/endpoint"
)

// diagDetailFabricIndex is the detail key of a session event that names
// the session's fabric (decimal FabricIndex). The node's DiagnosticLogs
// provider reads it to serve a controller its own fabric's peers and
// session ids and nobody else's (node ringLogs; review II,
// R2-C2-sec-2451, owner decision 524).
const diagDetailFabricIndex = "fabric_index"

// sessionEventDetail adds sessionID's fabric to detail under
// [diagDetailFabricIndex] when the bridge can name it; a session it
// cannot attribute to a fabric (a PASE session, an unresolvable one)
// gets no key.
func (b *Bridge) sessionEventDetail(sessionID uint16, detail map[string]string) map[string]string {
	idx := b.resolveSessionFabric(sessionID)
	if idx != 0 && idx != endpointpkg.FabricIndexUnresolvable {
		detail[diagDetailFabricIndex] = strconv.Itoa(int(idx))
	}
	return detail
}

// establishedEventDetail is [Bridge.sessionEventDetail] for the session
// a Sigma3 step established; without one, detail as it is.
func (b *Bridge) establishedEventDetail(step *sigma3Step, detail map[string]string) map[string]string {
	if sid, ok := step.establishedSession(); ok && sid != 0 {
		return b.sessionEventDetail(sid, detail)
	}
	return detail
}

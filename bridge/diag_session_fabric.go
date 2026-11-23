// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"strconv"

	"thingfabric.dev/go-fabric/diagevent"
	endpointpkg "thingfabric.dev/go-fabric/endpoint"
)

// sessionEventDetail adds sessionID's fabric to detail under
// [diagevent.DetailFabricIndex] when the bridge can name it; a session it
// cannot attribute to a fabric (a PASE session, an unresolvable one)
// gets no key.
func (b *Bridge) sessionEventDetail(sessionID uint16, detail map[string]string) map[string]string {
	idx := b.resolveSessionFabric(sessionID)
	if idx != 0 && idx != endpointpkg.FabricIndexUnresolvable {
		detail[diagevent.DetailFabricIndex] = strconv.Itoa(int(idx))
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

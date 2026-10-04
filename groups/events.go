// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package groups

import (
	"net"
	"slices"
)

// GroupcastTestResultEnum values (groupcast.element.ts), the outcome a
// received group message is reported with.
const (
	TestResultSuccess        uint8 = 0
	TestResultGeneralError   uint8 = 1
	TestResultMessageReplay  uint8 = 2
	TestResultFailedAuth     uint8 = 3
	TestResultNoAvailableKey uint8 = 4
	TestResultSendFailure    uint8 = 5
)

// GroupMessageEvent is the outcome of one received group message. Mirrors
// matter.js GroupMessageEventInfo (packages/protocol/src/session/
// SessionManager.ts), which SessionManager.onGroupMessage carries to the
// Groupcast server's GroupcastTesting.
type GroupMessageEvent struct {
	// Result is a GroupcastTestResultEnum value.
	Result uint8
	// FabricIndex names the fabric whose key set authenticated the
	// message, 0 when none did.
	FabricIndex uint8
	// GroupID is the authenticated destination group (HasGroupID).
	GroupID    uint16
	HasGroupID bool
	// HeaderGroupID is the group id of a message that failed to decode,
	// taken from the unobfuscated wire header — fit to derive the arrival
	// address, never to report as the authenticated group
	// (HasHeaderGroupID).
	HeaderGroupID    uint16
	HasHeaderGroupID bool
	// SourceIP is the sender's address, when known.
	SourceIP net.IP
	// HasPath reports that EndpointID, ClusterID and ElementID name the
	// command the message dispatched; HasEndpoint whether EndpointID is
	// set (a wildcard command path carries none).
	HasPath     bool
	HasEndpoint bool
	EndpointID  uint16
	ClusterID   uint32
	ElementID   uint32
	// AccessAllowed is the access-control outcome of a Success, nil when
	// no access evaluation took place.
	AccessAllowed *bool
}

// OnGroupMessage registers fn for the outcome of every received group
// message and returns the function that removes it again. fn runs
// synchronously on the receive path, outside the manager's locks, and must
// not block. Mirrors matter.js SessionManager.onGroupMessage, which the
// Groupcast server listens on only while a fabric is under test.
func (m *Manager) OnGroupMessage(fn func(GroupMessageEvent)) (unsubscribe func()) {
	if fn == nil {
		return func() {}
	}
	m.listenersMu.Lock()
	if m.messageListeners == nil {
		m.messageListeners = make(map[uint64]func(GroupMessageEvent))
	}
	m.nextListener++
	id := m.nextListener
	m.messageListeners[id] = fn
	m.listenersMu.Unlock()
	return func() {
		m.listenersMu.Lock()
		delete(m.messageListeners, id)
		m.listenersMu.Unlock()
	}
}

// ReportGroupMessage hands a group message's outcome to every listener
// registered with OnGroupMessage; without one it costs nothing. The receive
// path calls it. Mirrors matter.js SessionManager.emitGroupMessage.
func (m *Manager) ReportGroupMessage(ev GroupMessageEvent) {
	m.listenersMu.Lock()
	if len(m.messageListeners) == 0 {
		m.listenersMu.Unlock()
		return
	}
	ids := make([]uint64, 0, len(m.messageListeners))
	for id := range m.messageListeners {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	fns := make([]func(GroupMessageEvent), 0, len(ids))
	for _, id := range ids {
		fns = append(fns, m.messageListeners[id])
	}
	m.listenersMu.Unlock()
	ev.SourceIP = slices.Clone(ev.SourceIP)
	for _, fn := range fns {
		fn(ev)
	}
}

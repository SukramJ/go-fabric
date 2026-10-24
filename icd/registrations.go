// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package icd

import (
	"slices"
	"sync"
)

// ClientType is a registration's client type (ClientTypeEnum,
// icd-management.element.ts:133-135).
type ClientType uint8

// The two client types.
const (
	// ClientPermanent is a client the device sends Check-Ins to.
	ClientPermanent ClientType = 0
	// ClientEphemeral is a client that gets no Check-In.
	ClientEphemeral ClientType = 1
)

// Registration is one registered Check-In client of the device on a
// fabric: the RegisteredClients entry and its ICDToken key. Mirrors
// matter.js FabricIcd.ts:281-286 (FabricIcd.Registration).
type Registration struct {
	CheckInNodeID    uint64
	MonitoredSubject uint64
	Key              []byte
	ClientType       ClientType
}

// Registrations is the device role of a fabric's ICD state: its
// registered clients, keyed by CheckInNodeID, in the order they first
// registered. It is runtime state only; the ICD Management server persists
// the registrations and rebuilds the table on every start (FabricIcd.ts:16-23).
// Safe for concurrent use.
//
// Mirrors matter.js FabricIcd.ts:45-60 (registrations, setRegistration,
// deleteRegistration, clearRegistrations).
type Registrations struct {
	// mu guards regs.
	mu   sync.Mutex
	regs []Registration
}

// All returns a copy of the registrations (FabricIcd.ts:45-47).
func (r *Registrations) All() []Registration {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.regs)
}

// Set adds a registration or replaces the one of the same CheckInNodeID in
// place (FabricIcd.ts:50-52).
func (r *Registrations) Set(reg Registration) {
	reg.Key = slices.Clone(reg.Key)
	r.mu.Lock()
	defer r.mu.Unlock()
	if i := r.indexLocked(reg.CheckInNodeID); i >= 0 {
		r.regs[i] = reg
		return
	}
	r.regs = append(r.regs, reg)
}

// Delete removes the registration of checkInNodeID (FabricIcd.ts:54-56).
func (r *Registrations) Delete(checkInNodeID uint64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if i := r.indexLocked(checkInNodeID); i >= 0 {
		r.regs = slices.Delete(r.regs, i, i+1)
	}
}

// Clear removes every registration (FabricIcd.ts:58-60).
func (r *Registrations) Clear() {
	r.mu.Lock()
	r.regs = nil
	r.mu.Unlock()
}

func (r *Registrations) indexLocked(checkInNodeID uint64) int {
	return slices.IndexFunc(r.regs, func(reg Registration) bool { return reg.CheckInNodeID == checkInNodeID })
}

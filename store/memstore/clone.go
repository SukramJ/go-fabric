// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package memstore

import (
	"bytes"
	"cmp"
	"maps"
	"slices"

	"github.com/SukramJ/go-fabric/store"
)

// The copies below keep the store and its callers apart: what goes in is
// copied on the way in, what comes out on the way out. Nil and empty stay
// what SQLite's encoding makes of them (a nil ICAC stays nil, an empty
// list of door-lock records comes back nil).

func cloneFabric(f store.FabricRecord) store.FabricRecord {
	f.RootPublicKey = bytes.Clone(f.RootPublicKey)
	f.RootCert = bytes.Clone(f.RootCert)
	return f
}

func cloneIdentity(r store.IdentityRecord) store.IdentityRecord {
	r.NOC = bytes.Clone(r.NOC)
	r.ICAC = bytes.Clone(r.ICAC)
	r.PrivateKey = bytes.Clone(r.PrivateKey)
	r.IPK = bytes.Clone(r.IPK)
	return r
}

func cloneACLEntry(e store.ACLEntry) store.ACLEntry {
	e.Subjects = slices.Clone(e.Subjects)
	if e.Targets != nil {
		targets := make([]store.ACLTarget, len(e.Targets))
		for i, t := range e.Targets {
			targets[i] = store.ACLTarget{Cluster: clonePtr(t.Cluster), Endpoint: clonePtr(t.Endpoint), DeviceType: clonePtr(t.DeviceType)}
		}
		e.Targets = targets
	}
	return e
}

func cloneKeySet(k store.GroupKeySet) store.GroupKeySet {
	k.EpochKey0 = bytes.Clone(k.EpochKey0)
	k.EpochKey1 = bytes.Clone(k.EpochKey1)
	k.EpochKey2 = bytes.Clone(k.EpochKey2)
	return k
}

func cloneGroupEntry(e store.GroupTableEntry) store.GroupTableEntry {
	e.Endpoints = slices.Clone(e.Endpoints)
	return e
}

func cloneResumption(r store.ResumptionRecord) store.ResumptionRecord {
	r.ResumptionID = bytes.Clone(r.ResumptionID)
	r.SharedSecret = bytes.Clone(r.SharedSecret)
	r.CASEAuthTags = slices.Clone(r.CASEAuthTags)
	return r
}

func cloneLockAuth(a store.DoorLockAuth) store.DoorLockAuth {
	var out store.DoorLockAuth
	for _, u := range a.Users {
		u.UserUniqueID = clonePtr(u.UserUniqueID)
		u.ExpiringUserExpiresAt = clonePtr(u.ExpiringUserExpiresAt)
		u.Credentials = slices.Clone(u.Credentials)
		out.Users = append(out.Users, u)
	}
	for _, c := range a.Credentials {
		c.Data = bytes.Clone(c.Data)
		out.Credentials = append(out.Credentials, c)
	}
	out.WeekDaySchedules = append(out.WeekDaySchedules, a.WeekDaySchedules...)
	out.YearDaySchedules = append(out.YearDaySchedules, a.YearDaySchedules...)
	out.HolidaySchedules = append(out.HolidaySchedules, a.HolidaySchedules...)
	return out
}

func clonePtr[T any](p *T) *T {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}

func identity[T any](v T) T { return v }

// inner returns the fabric's map in m, creating it.
func inner[V any](m map[uint8]map[uint16]V, fabricIndex uint8) map[uint16]V {
	in, ok := m[fabricIndex]
	if !ok {
		in = map[uint16]V{}
		m[fabricIndex] = in
	}
	return in
}

func sortedKeys[K cmp.Ordered, V any](m map[K]V) []K {
	return slices.Sorted(maps.Keys(m))
}

// sortedValues returns copies of m's values by key ascending; nil when m is
// empty, as a query without rows returns.
func sortedValues[V any](m map[uint16]V, clone func(V) V) []V {
	var out []V
	for _, k := range sortedKeys(m) {
		out = append(out, clone(m[k]))
	}
	return out
}

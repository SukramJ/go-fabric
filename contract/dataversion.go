// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package contract

import (
	"crypto/rand"
	"encoding/binary"
	"sync/atomic"
)

// DataVersionTracker is a per-cluster monotonic counter satisfying the
// [ClusterDataVersion] contract. Every attribute write that changed a
// value SHOULD call [DataVersionTracker.Bump] so subscribers see a fresh
// version number in their cached state; a write that changed nothing
// leaves it, and a server that says so implements
// [ChangeReportingWriter].
//
// A cluster's data version is a Matter concept, so the counter lives
// with the other Matter port contracts rather than in the host type
// package. Both sides that embed it reach it from here: the cluster
// servers under cluster and the model packages
// on the host side, neither of which then depends on the
// other.
//
// Three facts govern its behaviour: the counter is monotonic, it is
// per-cluster, and it starts at a random non-zero value rather than at
// 1 or 0. The controller behaviour that forced the random start is
// documented with the cluster-side alias in
// cluster/dataversion.go.
//
// Usage:
//
//	type MyCluster struct {
//	    contract.DataVersionTracker
//	    // ...
//	}
//
//	func (c *MyCluster) MatterDataVersion() uint32 { return c.DataVersionTracker.Current() }
//
//	func (c *MyCluster) MatterWrite(...) error {
//	    // do mutation; when it changed a value:
//	    c.DataVersionTracker.Bump()
//	    return nil
//	}
type DataVersionTracker struct {
	// v packs the version (the low 32 bits) and whether one is installed
	// ([installedVersion]): 0 is an ordinary version, not the "none
	// installed yet" state (owner decision 146, review II,
	// F-db021da0-17). matter.js's datasource version starts at a random
	// uint32 and wraps to 0, and is compared by `!== undefined`, never by
	// truthiness (packages/node/src/behavior/state/managed/Datasource.ts:597,
	// :690-699 at db021da0; AttributeWriteResponse.ts:254-257,
	// AttributeReadResponse.ts:276-281).
	v atomic.Uint64
}

// installedVersion marks a tracker's packed state as holding a version.
const installedVersion = uint64(1) << 32

// initialDataVersion returns a uniformly-random uint32, the first
// DataVersion of every tracker — 0 included, as matter.js's
// entropy.randomUint32 (Datasource.ts:763 at b25ed960). Unexported (review 2026-11
// W5-api-12): a host that replaced it changed every server's first
// DataVersion process-wide.
func initialDataVersion() uint32 {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand failure on this platform is fatal — the daemon
		// cannot ship Subscribe-Initial with a deterministic-looking
		// DataVersion fallback (Apple-cache regression). Panic
		// surfaces the configuration problem at start-up rather than
		// silently degrading.
		panic("contract: crypto/rand.Read failed: " + err.Error())
	}
	return binary.BigEndian.Uint32(b[:])
}

// Current returns the current DataVersion. On first access on a fresh
// tracker, a uniformly-random uint32 is installed via CAS so every
// subsequent reader sees the same value until [Bump] advances it.
func (d *DataVersionTracker) Current() uint32 {
	if v := d.v.Load(); v&installedVersion != 0 {
		return uint32(v) //nolint:gosec // G115: the low 32 bits are the version
	}
	// Race-tolerant init: only the first CAS wins, every other caller
	// reloads and observes the installed value.
	d.v.CompareAndSwap(0, installedVersion|uint64(initialDataVersion()))
	return uint32(d.v.Load()) //nolint:gosec // G115: the low 32 bits are the version
}

// Bump increments the DataVersion and returns the new value. The first
// call on a fresh tracker installs the random initial value first (via
// [Current]) and then advances by 1.
//
// Wrap-around past 0xFFFFFFFF to 0 is intentional and fine — the IM
// filter comparison uses == so any increment signals "cluster changed",
// and 0 is a version like any other (matter.js advanceVersion wraps to
// 0, Datasource.ts:690-699 at db021da0; owner decision 146).
func (d *DataVersionTracker) Bump() uint32 {
	// Ensure the random initial value is installed before incrementing.
	_ = d.Current()
	for {
		old := d.v.Load()
		next := installedVersion | uint64(uint32(old)+1) //nolint:gosec // G115: the low 32 bits are the version
		if d.v.CompareAndSwap(old, next) {
			return uint32(next) //nolint:gosec // G115: the low 32 bits are the version
		}
	}
}

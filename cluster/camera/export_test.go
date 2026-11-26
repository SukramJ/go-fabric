// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package camera

import "thingfabric.dev/go-fabric/clock"

// SetAvStreamCommitRefusal makes every later change of a's tables call
// refuse first and fail with its error, as a table the schema refuses
// does; nil restores the production path.
func SetAvStreamCommitRefusal(a *AvStream, refuse func() error) {
	a.refuseForTests = refuse
}

// SetZoneCommitRefusal is [SetAvStreamCommitRefusal] for z's tables.
func SetZoneCommitRefusal(z *Zones, refuse func() error) {
	z.refuseForTests = refuse
}

// SetProviderClock makes p read c for the activity of its sessions and the
// durations of its signals; set before the first command.
func SetProviderClock(p *Provider, c clock.Clock) {
	p.clock = c
}

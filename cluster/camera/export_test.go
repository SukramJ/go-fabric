// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package camera

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

// SetEvictHook makes p call hook between the admission of a new session
// and the removal of the victim it names; nil restores the production
// path.
func SetEvictHook(p *Provider, hook func(victim ProviderSession)) {
	p.beforeEvictForTests = hook
}

// LockDeliver takes r's delivery lock exclusively, as an End's removal
// does, until the returned unlock.
func LockDeliver(r *Requestor) (unlock func()) {
	r.deliver.Lock()
	return r.deliver.Unlock
}

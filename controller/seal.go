// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controller

import "thingfabric.dev/go-fabric/store"

// The store errors of a sealed controller database and of its scrub, for
// errors.Is. They are the store's values, not copies.
var (
	// ErrUnseal is wrapped by [New] when [Config].Sealer does not open the
	// sealed database — another key than the one that sealed it, or a
	// tampered value — and by [Reseal] when its old sealer does not
	// (store.ErrUnseal).
	ErrUnseal = store.ErrUnseal
	// ErrNoSealer is wrapped by [New] on a sealed database opened without
	// a sealer (store.ErrNoSealer): the database was sealed, and
	// [Config].Sealer must be set.
	ErrNoSealer = store.ErrNoSealer
	// ErrScrubIncomplete is wrapped by every error [Reseal] returns after
	// its commit — the values are sealed under the new sealer, but the
	// scrub of the file did not finish or its mark could not be dropped,
	// and the next [New] under the new sealer scrubs again — and by a
	// [Controller.Erase] whose reset completed but whose scrub did not
	// (store.ErrScrubIncomplete).
	ErrScrubIncomplete = store.ErrScrubIncomplete
)

// NewAESGCMSealer returns the module's stdlib sealer for [Config].Sealer
// and [Reseal] — AES-256-GCM under key, which must be 32 bytes. It is
// store.NewAESGCMSealer, for a host that imports the facade only
// (decision 551). The key is the host's: generated once (crypto/rand) and
// kept outside the database (docs/security.md, "Sealing at rest").
func NewAESGCMSealer(key []byte) (*store.AESGCMSealer, error) {
	return store.NewAESGCMSealer(key)
}

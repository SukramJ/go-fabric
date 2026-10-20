// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package groups

import (
	"context"
	"fmt"
)

// Key set 0 is the fabric's Identity Protection Key set. matter.js keeps it
// in the fabric's KeySets — seeded from the IPK of AddNOC
// (FabricGroups.ts:35-50), replaced by a KeySetWrite of id 0 — but never
// in the session index (KeySets.ts #updateSessions skips id 0), so it
// authenticates no group message. Its keys are the CASE destination
// identifier's: the responder matches a Sigma1 against every key of key
// set 0 (Fabric.ts:399-403 destinationIdsFor), the initiator uses the
// second-newest of three, else the only one (KeySets.ts:113-121
// currentKeyForId, §4.14.2.6.1). The Go manager holds it apart from the
// operational key sets, as fabricGroups.ipk.

// ipkKeys is matter.js KeySets.allKeysForId(0): the operational keys of
// key set 0 in slot order, nil when the fabric has no key set 0 row (a
// store that predates AddNOC writing it). Called with m.mu held.
func (f *fabricGroups) ipkKeys() [][]byte {
	if f.ipk == nil {
		return nil
	}
	keys := make([][]byte, 0, len(f.ipk.epochs))
	for _, e := range f.ipk.epochs {
		keys = append(keys, e.key)
	}
	return keys
}

// currentIPK is matter.js KeySets.currentKeyForId(0): the key with the
// second-newest EpochStartTime when the set holds three, else the first
// — the originator's key for the destination identifier (§4.14.2.6.1;
// KeySets.ts:113-121, `operationalKeys.length > 2 ? [1] : [0]`), without
// the start-time filter of the other key sets. Called with m.mu held.
func (f *fabricGroups) currentIPK() ([]byte, error) {
	keys := f.ipkKeys()
	switch {
	case len(keys) == 0:
		return nil, fmt.Errorf("%w: no operational keys found for groupKeySet 0", ErrNoKey)
	case len(keys) > 2:
		return keys[1], nil
	default:
		return keys[0], nil
	}
}

// IPKKeys returns the operational keys of the fabric's key set 0, the
// Identity Protection Key set, in slot order — matter.js
// Fabric.destinationIdsFor's candidates (Fabric.ts:399-403,
// `keySets.allKeysForId(0)`), each a 16-byte key derived from its epoch
// key with the compressed fabric id. Empty when the store holds no key
// set 0 for the fabric. [ErrNoFabric] for an unknown fabric.
func (m *Manager) IPKKeys(ctx context.Context, fabricIndex uint8) ([][]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	f, err := m.fabricLocked(ctx, fabricIndex)
	if err != nil {
		return nil, err
	}
	keys := f.ipkKeys()
	out := make([][]byte, len(keys))
	for i, k := range keys {
		out[i] = append([]byte(nil), k...)
	}
	return out, nil
}

// CurrentIPK returns the operational key of key set 0 an initiator uses
// for the CASE destination identifier: the second-newest of three, else
// the only one (matter.js Fabric.currentDestinationIdFor, Fabric.ts:391-393,
// KeySets.ts:113-121). [ErrNoKey] when the fabric has no key set 0,
// [ErrNoFabric] for an unknown fabric.
func (m *Manager) CurrentIPK(ctx context.Context, fabricIndex uint8) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	f, err := m.fabricLocked(ctx, fabricIndex)
	if err != nil {
		return nil, err
	}
	k, err := f.currentIPK()
	if err != nil {
		return nil, err
	}
	return append([]byte(nil), k...), nil
}

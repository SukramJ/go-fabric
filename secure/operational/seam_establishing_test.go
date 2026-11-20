// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package operational

import (
	"errors"
	"testing"

	"github.com/SukramJ/go-fabric/internal/sessionseam"
)

// TestSessionSeamOpenEstablishingAndComplete: the node facade's install of a
// responder session through the seam (P2-J, decision 312) puts it under its
// stake's id establishing, and the completion hook establishes it once; a
// manager, stake or entry of another type is refused.
func TestSessionSeamOpenEstablishingAndComplete(t *testing.T) {
	t.Parallel()
	m := NewManager(newMinimalFakeStore())
	s, err := m.AllocateStake()
	if err != nil {
		t.Fatalf("AllocateStake: %v", err)
	}
	got, err := sessionseam.OpenEstablishing(m, s, 1, 0x10, 0x20, 0x2000, nil, closeParityKeys(1))
	if err != nil {
		t.Fatalf("OpenEstablishing: %v", err)
	}
	e, ok := got.(*Entry)
	if !ok || e.SessionID != s.ID {
		t.Fatalf("OpenEstablishing = %+v, want an entry under the stake's id %#x", got, s.ID)
	}
	m.mu.Lock()
	establishing := e.establishing
	m.mu.Unlock()
	if !establishing {
		t.Fatal("the seam's install is not establishing")
	}
	if !sessionseam.CompleteEstablishment(m, e) {
		t.Fatal("CompleteEstablishment of the establishing entry completed nothing")
	}
	m.mu.Lock()
	establishing = e.establishing
	m.mu.Unlock()
	if establishing {
		t.Fatal("the entry is still establishing after its completion")
	}

	if _, err := sessionseam.OpenEstablishing(struct{}{}, s, 1, 0x10, 0x20, 0x2000, nil, closeParityKeys(2)); !errors.Is(err, errSeamManager) {
		t.Fatalf("OpenEstablishing with a non-manager: %v, want errSeamManager", err)
	}
	if _, err := sessionseam.OpenEstablishing(m, struct{}{}, 1, 0x10, 0x20, 0x2000, nil, closeParityKeys(3)); !errors.Is(err, errSeamStake) {
		t.Fatalf("OpenEstablishing with a non-stake: %v, want errSeamStake", err)
	}
	if sessionseam.CompleteEstablishment(struct{}{}, e) || sessionseam.CompleteEstablishment(m, struct{}{}) {
		t.Fatal("CompleteEstablishment with a wrong type completed something")
	}
}

// TestEntryPeerReadings: the readings of what the peer stated and when it
// was last heard; an entry that never carried a peer reports no activity.
func TestEntryPeerReadings(t *testing.T) {
	t.Parallel()
	e := &Entry{}
	if idle, active, threshold := e.PeerMRPIntervals(); idle != 0 || active != 0 || threshold != 0 {
		t.Fatalf("PeerMRPIntervals of a bare entry = %d, %d, %d, want zeros", idle, active, threshold)
	}
	if _, ok := e.LastPeerActivityMonotonic(); ok {
		t.Fatal("LastPeerActivityMonotonic of an entry without a peer reported activity")
	}
}

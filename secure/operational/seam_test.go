// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package operational

import (
	"testing"

	"github.com/SukramJ/go-fabric/internal/sessionseam"
)

// TestSessionSeamOpenAtInstallsUnderTheChosenID: the fixture install puts
// the session under the id it names, responder and initiator alike, and
// refuses a manager of another type (decision 160).
func TestSessionSeamOpenAtInstallsUnderTheChosenID(t *testing.T) {
	t.Parallel()
	m := NewManager(newMinimalFakeStore())
	for i, initiator := range []bool{false, true} {
		id := uint16(0x0A01 + i)
		got, err := sessionseam.OpenAt(m, id, initiator, 1, 0x10, 0x20, 0x2000, nil, closeParityKeys(byte(i)))
		if err != nil {
			t.Fatalf("OpenAt(%#x, initiator %t): %v", id, initiator, err)
		}
		e, ok := got.(*Entry)
		if !ok || e.SessionID != id || e.isInitiator != initiator {
			t.Fatalf("OpenAt(%#x, initiator %t) = %+v, want an entry under the id with that role", id, initiator, got)
		}
		if cur, err := m.Get(id); err != nil || cur != e {
			t.Fatalf("Get(%#x) = (%p, %v), want the installed entry", id, cur, err)
		}
	}
	if _, err := sessionseam.OpenAt(struct{}{}, 1, false, 1, 0x10, 0x20, 0x2000, nil, closeParityKeys(9)); err == nil {
		t.Fatal("OpenAt with a non-manager succeeded")
	}
}

// TestSessionSeamCloseEntryClosesOnlyThatEntry: the close of an entry takes
// it while it holds its id, and leaves alone a session or a stake that
// holds that id by then (decision 159, Fable Q1 record).
func TestSessionSeamCloseEntryClosesOnlyThatEntry(t *testing.T) {
	t.Parallel()
	m := NewManager(newMinimalFakeStore())
	var closed closedIDs
	m.SetOnSessionClose(closed.add)
	s, err := m.AllocateStake()
	if err != nil {
		t.Fatalf("AllocateStake: %v", err)
	}
	e, err := m.OpenFromSigmaWithStake(s, 1, 0x10, 0x20, 0x2000, nil, closeParityKeys(1))
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	if !sessionseam.CloseEntry(m, e) {
		t.Fatal("CloseEntry of the installed entry closed nothing")
	}
	if ids := closed.snapshot(); len(ids) != 1 || ids[0] != e.SessionID {
		t.Fatalf("closed %v, want [%d]", ids, e.SessionID)
	}
	if sessionseam.CloseEntry(m, e) {
		t.Fatal("a second CloseEntry of the same entry closed something")
	}

	// The id held by another session now: the stale entry closes nothing.
	got, err := sessionseam.OpenAt(m, e.SessionID, false, 1, 0x10, 0x21, 0x2001, nil, closeParityKeys(2))
	if err != nil {
		t.Fatalf("OpenAt: %v", err)
	}
	if sessionseam.CloseEntry(m, e) {
		t.Fatal("CloseEntry of a closed entry closed the session that holds its id now")
	}
	if cur, err := m.Get(e.SessionID); err != nil || cur != got {
		t.Fatalf("Get(%d) = (%p, %v), want the other session", e.SessionID, cur, err)
	}
	if sessionseam.CloseEntry(struct{}{}, got) || sessionseam.CloseEntry(m, struct{}{}) || sessionseam.CloseEntry(m, (*Entry)(nil)) {
		t.Fatal("CloseEntry with a wrong type closed something")
	}
}

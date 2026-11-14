// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package operational

import (
	"bytes"
	"testing"
)

// TestPlaceholderQueueStaysWithinAFactorOfTheTable: stakes that are released
// leave their reference in the queue of placeholders, and the queue is
// compacted once the references outnumber the table twice over, so a host
// whose handshakes keep failing does not grow it without bound
// ([Manager.compactPlaceholdersLocked]; review II, R2-P3-lan-dev-02).
func TestPlaceholderQueueStaysWithinAFactorOfTheTable(t *testing.T) {
	t.Parallel()
	m := NewManager(newMinimalFakeStore())
	kept, err := m.AllocateStake()
	if err != nil {
		t.Fatalf("AllocateStake: %v", err)
	}
	for range 10 * placeholderQueueSlack {
		s, err := m.AllocateStake()
		if err != nil {
			t.Fatalf("AllocateStake: %v", err)
		}
		m.ReleaseStake(s)
		m.mu.RLock()
		queued, table := len(m.placeholders), len(m.sessions)
		m.mu.RUnlock()
		if queued > 2*(table+1)+placeholderQueueSlack {
			t.Fatalf("the queue holds %d references for a table of %d, want at most twice the table plus %d", queued, table, placeholderQueueSlack)
		}
	}
	// The stake that still stands survives every compaction.
	m.mu.RLock()
	stands := m.stakes(placeholderRef{id: kept.ID, e: kept.e})
	queued := 0
	for _, ref := range m.placeholders {
		if ref.id == kept.ID && ref.e == kept.e {
			queued++
		}
	}
	m.mu.RUnlock()
	if !stands || queued != 1 {
		t.Fatalf("the standing stake: stakes = %v, %d references in the queue, want true and 1", stands, queued)
	}
}

// TestPASESessionIDsNamesTheEstablishedPASESessionsOnly: the ids the bridge's
// PASE gate asks for are those of established PASE sessions — not a stake
// no session has taken yet, not a CASE session ([Manager.PASESessionIDs]).
func TestPASESessionIDsNamesTheEstablishedPASESessionsOnly(t *testing.T) {
	t.Parallel()
	m := NewManager(newMinimalFakeStore())
	if ids := m.PASESessionIDs(); len(ids) != 0 {
		t.Fatalf("an empty table names PASE sessions %v", ids)
	}
	if _, err := m.AllocateStake(); err != nil {
		t.Fatalf("AllocateStake: %v", err)
	}
	if _, err := m.OpenFromSigma(1, 0xB0B, 0xA11CE, lifecycleKeys(0x10)); err != nil {
		t.Fatalf("OpenFromSigma: %v", err)
	}
	pase, err := m.OpenFromPase(1, 2, 0, bytes.Repeat([]byte{0x5A}, 32))
	if err != nil {
		t.Fatalf("OpenFromPase: %v", err)
	}
	ids := m.PASESessionIDs()
	if len(ids) != 1 || ids[0] != pase.SessionID {
		t.Fatalf("PASESessionIDs = %v, want the PASE session's id %d alone", ids, pase.SessionID)
	}
}

// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package operational

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/secure/sigma"
)

// stakeInstall is one of the installs that take a [Stake]; the table below
// runs every case against each, so an install added beside them that
// trusts the bare id has a row to join (N3-findev-1).
type stakeInstall struct {
	name    string
	install func(m *Manager, s Stake, peerNodeID uint64, keys sigma.SessionKeys) (*Entry, error)
}

var stakeInstalls = []stakeInstall{
	{"OpenFromSigmaWithStake", func(m *Manager, s Stake, peer uint64, keys sigma.SessionKeys) (*Entry, error) {
		return m.OpenFromSigmaWithStake(s, 1, 0x10, peer, 0x2000, nil, keys)
	}},
	{"OpenFromSigmaAsInitiatorWithStake", func(m *Manager, s Stake, peer uint64, keys sigma.SessionKeys) (*Entry, error) {
		return m.OpenFromSigmaAsInitiatorWithStake(s, 1, 0x10, peer, 0x2000, nil, keys)
	}},
}

// closedIDs records the ids [Manager.SetOnSessionClose] reports.
type closedIDs struct {
	mu  sync.Mutex
	ids []uint16
}

func (c *closedIDs) add(id uint16) {
	c.mu.Lock()
	c.ids = append(c.ids, id)
	c.mu.Unlock()
}

func (c *closedIDs) snapshot() []uint16 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]uint16(nil), c.ids...)
}

// fillAndTakeOver stakes every id of the space, then allocates once more:
// the allocation at the full table takes the oldest stake's id. It returns
// the stake that lost its id and the one that holds it now.
func fillAndTakeOver(t *testing.T, m *Manager) (lost, taker Stake) {
	t.Helper()
	lost, err := m.AllocateStake()
	if err != nil {
		t.Fatalf("AllocateStake: %v", err)
	}
	for range SessionIDSpace - 1 {
		if _, err := m.AllocateStake(); err != nil {
			t.Fatalf("AllocateStake: %v", err)
		}
	}
	taker, err = m.AllocateStake()
	if err != nil || taker.ID != lost.ID {
		t.Fatalf("AllocateStake at a full table = (%d, %v), want the oldest stake's id %d", taker.ID, err, lost.ID)
	}
	return lost, taker
}

// TestOpenWithAStakeThatLostItsIDInstallsNothing (review II, N3-findev-1):
// an install that takes a stake registers its session only while the stake
// still holds its id. A stake an allocation at a full table took over, one
// released, one the reaper reclaimed, one already installed and the zero
// Stake are refused with ErrStakeLost, and the id's holder — the other
// handshake's stake or the session it established — stays as it is.
func TestOpenWithAStakeThatLostItsIDInstallsNothing(t *testing.T) {
	t.Parallel()
	for _, in := range stakeInstalls {
		t.Run(in.name, func(t *testing.T) {
			t.Parallel()

			t.Run("taken over, the other handshake still staking", func(t *testing.T) {
				t.Parallel()
				m := NewManager(newMinimalFakeStore())
				lost, taker := fillAndTakeOver(t, m)
				if e, err := in.install(m, lost, 0x20, closeParityKeys(1)); !errors.Is(err, ErrStakeLost) || e != nil {
					t.Fatalf("install under the taken-over stake = (installed %t, %v), want (nil, ErrStakeLost)", e != nil, err)
				}
				m.mu.RLock()
				held := m.stakes(placeholderRef{id: taker.ID, e: taker.e})
				m.mu.RUnlock()
				if !held {
					t.Fatal("the install under the lost stake overwrote the stake that holds the id now")
				}
				if _, err := in.install(m, taker, 0x21, closeParityKeys(2)); err != nil {
					t.Fatalf("install under the stake that holds the id: %v", err)
				}
			})

			t.Run("taken over, the other handshake established", func(t *testing.T) {
				t.Parallel()
				m := NewManager(newMinimalFakeStore())
				var closed closedIDs
				m.SetOnSessionClose(closed.add)
				lost, taker := fillAndTakeOver(t, m)
				established, err := in.install(m, taker, 0x21, closeParityKeys(2))
				if err != nil {
					t.Fatalf("install under the stake that holds the id: %v", err)
				}
				if e, err := in.install(m, lost, 0x20, closeParityKeys(1)); !errors.Is(err, ErrStakeLost) || e != nil {
					t.Fatalf("install under the taken-over stake = (installed %t, %v), want (nil, ErrStakeLost)", e != nil, err)
				}
				if got, err := m.Get(taker.ID); err != nil || got != established {
					t.Fatalf("Get(%d) after the refused install = (%p, %v), want the established session %p", taker.ID, got, err, established)
				}
				if ids := closed.snapshot(); len(ids) != 0 {
					t.Fatalf("the refused install closed sessions %v; the established one must stay", ids)
				}
			})

			t.Run("released", func(t *testing.T) {
				t.Parallel()
				m := NewManager(newMinimalFakeStore())
				s, err := m.AllocateStake()
				if err != nil {
					t.Fatalf("AllocateStake: %v", err)
				}
				m.ReleaseStake(s)
				if e, err := in.install(m, s, 0x20, closeParityKeys(1)); !errors.Is(err, ErrStakeLost) || e != nil {
					t.Fatalf("install under a released stake = (installed %t, %v), want (nil, ErrStakeLost)", e != nil, err)
				}
				if occ := m.Occupancy(); occ.Live != 0 || occ.Reserved != 0 {
					t.Fatalf("occupancy after the refused install = %+v, want an empty table", occ)
				}
			})

			t.Run("reclaimed by the reaper", func(t *testing.T) {
				t.Parallel()
				m := NewManager(newMinimalFakeStore())
				s, err := m.AllocateStake()
				if err != nil {
					t.Fatalf("AllocateStake: %v", err)
				}
				m.mu.Lock()
				s.e.allocatedAt = s.e.allocatedAt.Add(-2 * PlaceholderIDTTL)
				m.mu.Unlock()
				m.reapIdle(time.Minute)
				if e, err := in.install(m, s, 0x20, closeParityKeys(1)); !errors.Is(err, ErrStakeLost) || e != nil {
					t.Fatalf("install under a reclaimed stake = (installed %t, %v), want (nil, ErrStakeLost)", e != nil, err)
				}
			})

			t.Run("already installed", func(t *testing.T) {
				t.Parallel()
				m := NewManager(newMinimalFakeStore())
				var closed closedIDs
				m.SetOnSessionClose(closed.add)
				s, err := m.AllocateStake()
				if err != nil {
					t.Fatalf("AllocateStake: %v", err)
				}
				first, err := in.install(m, s, 0x20, closeParityKeys(1))
				if err != nil {
					t.Fatalf("first install: %v", err)
				}
				if e, err := in.install(m, s, 0x21, closeParityKeys(2)); !errors.Is(err, ErrStakeLost) || e != nil {
					t.Fatalf("second install under the same stake = (installed %t, %v), want (nil, ErrStakeLost)", e != nil, err)
				}
				if got, err := m.Get(s.ID); err != nil || got != first {
					t.Fatalf("Get(%d) = (%p, %v), want the first session %p", s.ID, got, err, first)
				}
				if ids := closed.snapshot(); len(ids) != 0 {
					t.Fatalf("the refused install closed sessions %v", ids)
				}
			})

			t.Run("zero stake", func(t *testing.T) {
				t.Parallel()
				m := NewManager(newMinimalFakeStore())
				if e, err := in.install(m, Stake{}, 0x20, closeParityKeys(1)); !errors.Is(err, ErrStakeLost) || e != nil {
					t.Fatalf("install under the zero Stake = (installed %t, %v), want (nil, ErrStakeLost)", e != nil, err)
				}
				if occ := m.Occupancy(); occ.Live != 0 {
					t.Fatalf("occupancy after the refused install = %+v, want no session", occ)
				}
			})
		})
	}
}

// TestOpenWithAStakeInstallsInPlaceOfItsPlaceholder: the stake that holds
// its id installs the session there; the placeholder is gone and no other
// session is touched.
func TestOpenWithAStakeInstallsInPlaceOfItsPlaceholder(t *testing.T) {
	t.Parallel()
	for _, in := range stakeInstalls {
		t.Run(in.name, func(t *testing.T) {
			t.Parallel()
			m := NewManager(newMinimalFakeStore())
			s, err := m.AllocateStake()
			if err != nil {
				t.Fatalf("AllocateStake: %v", err)
			}
			e, err := in.install(m, s, 0x20, closeParityKeys(1))
			if err != nil {
				t.Fatalf("install: %v", err)
			}
			if e.SessionID != s.ID {
				t.Fatalf("installed under %d, want the stake's id %d", e.SessionID, s.ID)
			}
			if got, err := m.Get(s.ID); err != nil || got != e {
				t.Fatalf("Get(%d) = (%p, %v), want %p", s.ID, got, err, e)
			}
			if occ := m.Occupancy(); occ.Live != 1 || occ.Reserved != 0 {
				t.Fatalf("occupancy = %+v, want one live session and no stake", occ)
			}
		})
	}
}

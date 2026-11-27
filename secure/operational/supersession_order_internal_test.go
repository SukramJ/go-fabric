// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package operational

// The supersession hooks of one establishment run before those of the next
// (F-b25ed960-40). matter.js emits them synchronously inside the
// establishment (packages/protocol/src/session/SessionManager.ts:750-769
// at db91886c, #sessionEstablished), on one thread, so two establishments
// never interleave their emits; here two goroutines can, between the
// computation under the table's lock and the hook. No matter.js `it(`
// covers it (single-threaded); the ported cases are
// TestSessionManagerSupersession.

import (
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"
)

// supersessionRace establishes N1 on one goroutine and, while N1's
// establishment sits between its computation and its hooks, N2 on
// another; it returns N1 and N2. viaCompletion installs both establishing
// and completes them ([Manager.completeEstablishment]) instead of
// installing them established. raceWindow bounds how long N1 waits for N2
// to finish: N2 blocked behind N1 never finishes inside it.
func supersessionRace(t *testing.T, m *Manager, n1ID, n2ID uint16, viaCompletion bool) (n1, n2 *Entry) {
	t.Helper()
	const raceWindow = 200 * time.Millisecond
	establish := func(id uint16) *Entry {
		e, err := m.openFromSigma(id, nil, sessionManagerFabric, sessionManagerLocal, establishingPeer, id, nil, closeParityKeys(byte(id)), false, viaCompletion)
		if err != nil {
			t.Errorf("open %#x: %v", id, err)
			return nil
		}
		if viaCompletion && !m.completeEstablishment(e) {
			t.Errorf("complete %#x: not established", id)
		}
		return e
	}
	n2Done := make(chan *Entry, 1)
	var once sync.Once
	m.testBeforeSupersede = func(newer *Entry) {
		if newer.SessionID != n1ID {
			return
		}
		once.Do(func() {
			go func() { n2Done <- establish(n2ID) }()
			timer := time.NewTimer(raceWindow)
			defer timer.Stop()
			select {
			case e := <-n2Done:
				n2Done <- e
			case <-timer.C:
			}
		})
	}
	n1 = establish(n1ID)
	n2 = <-n2Done
	m.testBeforeSupersede = nil
	return n1, n2
}

func TestSupersessionHooksInEstablishmentOrder(t *testing.T) {
	t.Parallel()
	for _, viaCompletion := range []bool{false, true} {
		name := map[bool]string{false: "installed established", true: "completed"}[viaCompletion]
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			t.Run("every hook of the first establishment precedes the second's", func(t *testing.T) {
				t.Parallel()
				m, _ := newEstablishingManager(t)
				var mu sync.Mutex
				var pairs []string
				m.SetOnSessionSuperseded(func(olderID, newerID uint16) {
					mu.Lock()
					pairs = append(pairs, fmt.Sprintf("%#x->%#x", olderID, newerID))
					mu.Unlock()
				})
				o, err := m.openFromSigma(1, nil, sessionManagerFabric, sessionManagerLocal, establishingPeer, 1, nil, closeParityKeys(1), false, false)
				if err != nil {
					t.Fatal(err)
				}
				n1, n2 := supersessionRace(t, m, 2, 3, viaCompletion)
				if n1 == nil || n2 == nil {
					return
				}
				mu.Lock()
				defer mu.Unlock()
				want := []string{
					fmt.Sprintf("%#x->%#x", o.SessionID, n1.SessionID),
					fmt.Sprintf("%#x->%#x", o.SessionID, n2.SessionID),
					fmt.Sprintf("%#x->%#x", n1.SessionID, n2.SessionID),
				}
				if !slices.Equal(pairs, want) {
					t.Fatalf("supersession hooks %v; want %v", pairs, want)
				}
			})
			t.Run("a subscription follows to the newest session when the first establishment evicts the oldest", func(t *testing.T) {
				t.Parallel()
				m, _ := newEstablishingManager(t)
				// The cap full: O and four more, O the least recently
				// active (no traffic, lowest id), so N1's establishment
				// evicts it from the table before its hooks run.
				var o *Entry
				for id := uint16(1); id <= MaxSessionsPerPeer; id++ {
					e, err := m.openFromSigma(id, nil, sessionManagerFabric, sessionManagerLocal, establishingPeer, id, nil, closeParityKeys(byte(id)), false, false)
					if err != nil {
						t.Fatal(err)
					}
					if o == nil {
						o = e
					}
				}
				// The marker is a subscription on O, moved as the bridge's
				// hook moves one.
				var mu sync.Mutex
				marker := o.SessionID
				m.SetOnSessionSuperseded(func(olderID, newerID uint16) {
					mu.Lock()
					if marker == olderID {
						marker = newerID
					}
					mu.Unlock()
				})
				_, n2 := supersessionRace(t, m, 0x10, 0x11, viaCompletion)
				if n2 == nil {
					return
				}
				mu.Lock()
				defer mu.Unlock()
				if marker != n2.SessionID {
					t.Fatalf("the subscription ends on session %#x; want the newest, %#x", marker, n2.SessionID)
				}
			})
		})
	}
}

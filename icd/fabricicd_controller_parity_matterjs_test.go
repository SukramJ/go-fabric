// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// The controller role of the port of matter.js
// packages/protocol/test/icd/FabricIcdTest.ts at 862ad75f (:56-463):
// icd.PeerTable's trial decryption and replay drop, the pending key and its
// promotion, ConfirmKey, RefreshNeeded and the peers' wakefulness. matter.js's
// thrown handler error is a handler panic; its "crypto broken" error is a
// peer key the codec cannot use (not 16 bytes), which ProcessCheckIn returns.
package icd_test

import (
	"bytes"
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock/clocktest"
	"github.com/SukramJ/go-fabric/icd"
	"github.com/SukramJ/go-fabric/icd/checkin"
	"github.com/SukramJ/go-fabric/secure/aesccm"
)

func encodeICD(t *testing.T, key []byte, counter uint32, threshold uint16) []byte {
	t.Helper()
	p, err := checkin.EncodeICD(key, counter, threshold)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func processed(t *testing.T, table *icd.PeerTable, payload []byte) bool {
	t.Helper()
	ok, err := table.ProcessCheckIn(payload)
	if err != nil {
		t.Fatal(err)
	}
	return ok
}

type receiver struct {
	mu  sync.Mutex
	got []icd.ReceivedCheckIn
}

func (r *receiver) handle(c icd.ReceivedCheckIn) {
	r.mu.Lock()
	r.got = append(r.got, c)
	r.mu.Unlock()
}

func (r *receiver) counters() []uint32 {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []uint32
	for _, c := range r.got {
		out = append(out, c.Counter)
	}
	return out
}

func newTable(fake *clocktest.Fake) *icd.PeerTable {
	if fake == nil {
		fake = clocktest.New(modeEpoch)
	}
	return icd.NewPeerTable(fake, nil)
}

func peerOf(t *testing.T, table *icd.PeerTable, id uint64) icd.Peer {
	t.Helper()
	p, ok := table.PeerFor(id)
	if !ok {
		t.Fatalf("peer %d not registered", id)
	}
	return p
}

func TestFabricIcdControllerRole(t *testing.T) {
	t.Parallel()
	noop := func(icd.ReceivedCheckIn) {}

	t.Run("controller role — processCheckIn", func(t *testing.T) {
		t.Parallel()
		t.Run("wakefulness", func(t *testing.T) {
			t.Parallel()
			t.Run("starts Active Mode of the peer's wakefulness for its ActiveModeDuration", func(t *testing.T) {
				t.Parallel()
				fake := clocktest.New(modeEpoch)
				table := newTable(fake)
				table.AddPeer(icd.Peer{PeerNodeID: 11, Key: fabricKeyA, CounterStart: 10}, noop)
				w := table.WakefulnessFor(11)
				w.SetRequiresAwait(true)
				w.SetTimings(icd.Timings{ActiveModeThreshold: 5 * time.Second, ActiveModeDuration: 20 * time.Second, IdleModeDuration: 30 * time.Second})
				processed(t, table, encodeICD(t, fabricKeyA, 12, 5000))
				fake.Advance(10 * time.Second)
				if !w.Awake() {
					t.Fatal("asleep")
				}
				w.Close()
			})
		})
		t.Run("trial-decrypts and routes to the matching peer", func(t *testing.T) {
			t.Parallel()
			table := newTable(nil)
			r := &receiver{}
			table.AddPeer(icd.Peer{PeerNodeID: 11, Key: fabricKeyA, CounterStart: 10}, r.handle)
			table.AddPeer(icd.Peer{PeerNodeID: 22, Key: fabricKeyB}, func(icd.ReceivedCheckIn) { panic("wrong peer") })
			if !processed(t, table, encodeICD(t, fabricKeyA, 12, 5000)) {
				t.Fatal("no match")
			}
			if !slices.Equal(r.got, []icd.ReceivedCheckIn{{PeerNodeID: 11, Counter: 12, ActiveModeThreshold: 5000}}) {
				t.Fatal(r.got)
			}
		})
		t.Run("ignores undecryptable payloads", func(t *testing.T) {
			t.Parallel()
			table := newTable(nil)
			table.AddPeer(icd.Peer{PeerNodeID: 11, Key: fabricKeyA, CounterStart: 10}, func(icd.ReceivedCheckIn) { panic("called") })
			if processed(t, table, encodeICD(t, fabricKeyB, 12, 5000)) {
				t.Fatal("matched")
			}
		})
		t.Run("drops replayed counters without routing", func(t *testing.T) {
			t.Parallel()
			table := newTable(nil)
			r := &receiver{}
			table.AddPeer(icd.Peer{PeerNodeID: 11, Key: fabricKeyA, CounterStart: 10}, r.handle)
			payload := encodeICD(t, fabricKeyA, 12, 5000)
			if !processed(t, table, payload) || !processed(t, table, payload) || len(r.got) != 1 {
				t.Fatal(r.got)
			}
		})
		t.Run("updates lastOffset after successful processing", func(t *testing.T) {
			t.Parallel()
			table := newTable(nil)
			table.AddPeer(icd.Peer{PeerNodeID: 11, Key: fabricKeyA, CounterStart: 10}, noop)
			processed(t, table, encodeICD(t, fabricKeyA, 12, 5000))
			if p := peerOf(t, table, 11); p.LastOffset != 2 {
				t.Fatal(p.LastOffset)
			}
		})
		t.Run("handler errors do not escape processCheckIn", func(t *testing.T) {
			t.Parallel()
			table := newTable(nil)
			table.AddPeer(icd.Peer{PeerNodeID: 11, Key: fabricKeyA, CounterStart: 10}, func(icd.ReceivedCheckIn) { panic("handler failure") })
			if !processed(t, table, encodeICD(t, fabricKeyA, 12, 5000)) || peerOf(t, table, 11).LastOffset != 2 {
				t.Fatal("not consumed")
			}
		})
		t.Run("removes peers", func(t *testing.T) {
			t.Parallel()
			table := newTable(nil)
			table.AddPeer(icd.Peer{PeerNodeID: 11, Key: fabricKeyA, CounterStart: 10}, noop)
			table.DeletePeer(11)
			if processed(t, table, encodeICD(t, fabricKeyA, 12, 5000)) {
				t.Fatal("matched")
			}
		})
	})

	t.Run("controller role — pending key", func(t *testing.T) {
		t.Parallel()
		// refreshingPeer is FabricIcdTest.ts:165-173: the last Check-In
		// under KEY_A carried counter 15, KEY_B pending.
		refreshingPeer := func(t *testing.T, r *receiver, counterStart, lastOffset uint32) *icd.PeerTable {
			t.Helper()
			table := newTable(nil)
			h := noop
			if r != nil {
				h = r.handle
			}
			table.AddPeer(icd.Peer{PeerNodeID: 11, Key: fabricKeyA, CounterStart: counterStart, LastOffset: lastOffset}, h)
			if err := table.SetPendingKey(11, fabricKeyB); err != nil {
				t.Fatal(err)
			}
			return table
		}
		t.Run("keeps decrypting with the current key", func(t *testing.T) {
			t.Parallel()
			r := &receiver{}
			table := refreshingPeer(t, r, 10, 5)
			if !processed(t, table, encodeICD(t, fabricKeyA, 16, 5000)) {
				t.Fatal("no match")
			}
			p := peerOf(t, table, 11)
			if !slices.Equal(r.counters(), []uint32{16}) || p.LastOffset != 6 || !bytes.Equal(p.Key, fabricKeyA) || !bytes.Equal(p.PendingKey, fabricKeyB) {
				t.Fatalf("%v %+v", r.counters(), p)
			}
		})
		t.Run("promotes the pending key on its first Check-In, counting from the last counter under the old key", func(t *testing.T) {
			t.Parallel()
			r := &receiver{}
			table := refreshingPeer(t, r, 10, 5)
			if !processed(t, table, encodeICD(t, fabricKeyB, 20, 5000)) {
				t.Fatal("no match")
			}
			p := peerOf(t, table, 11)
			if !slices.Equal(r.counters(), []uint32{20}) || !bytes.Equal(p.Key, fabricKeyB) || p.CounterStart != 15 || p.LastOffset != 5 || p.PendingKey != nil {
				t.Fatalf("%v %+v", r.counters(), p)
			}
		})
		t.Run("computes the promotion baseline modulo 2³²", func(t *testing.T) {
			t.Parallel()
			r := &receiver{}
			table := refreshingPeer(t, r, 0xfffffff0, 0x20)
			processed(t, table, encodeICD(t, fabricKeyB, 0x13, 5000))
			p := peerOf(t, table, 11)
			if !slices.Equal(r.counters(), []uint32{0x13}) || p.LastOffset != 3 || p.CounterStart != 0x10 {
				t.Fatalf("%v %+v", r.counters(), p)
			}
		})
		t.Run("rejects a replay and an earlier counter under the promoted key", func(t *testing.T) {
			t.Parallel()
			r := &receiver{}
			table := refreshingPeer(t, r, 10, 5)
			promoting := encodeICD(t, fabricKeyB, 20, 5000)
			processed(t, table, promoting)
			if !processed(t, table, promoting) || !processed(t, table, encodeICD(t, fabricKeyB, 18, 5000)) {
				t.Fatal("no match")
			}
			if len(r.counters()) != 1 || peerOf(t, table, 11).LastOffset != 5 {
				t.Fatalf("%v %+v", r.counters(), peerOf(t, table, 11))
			}
		})
		for _, counter := range []uint32{15, 12} {
			t.Run(fmt.Sprintf("does not promote the pending key with counter %d, not after the old key's last counter 15", counter), func(t *testing.T) {
				t.Parallel()
				r := &receiver{}
				table := refreshingPeer(t, r, 10, 5)
				if !processed(t, table, encodeICD(t, fabricKeyB, counter, 5000)) {
					t.Fatal("no match")
				}
				p := peerOf(t, table, 11)
				if len(r.counters()) != 0 || !bytes.Equal(p.Key, fabricKeyA) || !bytes.Equal(p.PendingKey, fabricKeyB) || p.LastOffset != 5 {
					t.Fatalf("%v %+v", r.counters(), p)
				}
				processed(t, table, encodeICD(t, fabricKeyB, 20, 5000))
				if !slices.Equal(r.counters(), []uint32{20}) || peerOf(t, table, 11).LastOffset != 5 {
					t.Fatalf("%v %+v", r.counters(), peerOf(t, table, 11))
				}
			})
		}
		t.Run("stops decrypting with the old key once the pending key is promoted", func(t *testing.T) {
			t.Parallel()
			table := refreshingPeer(t, nil, 10, 5)
			processed(t, table, encodeICD(t, fabricKeyB, 20, 5000))
			if processed(t, table, encodeICD(t, fabricKeyA, 30, 5000)) {
				t.Fatal("matched the old key")
			}
		})
		t.Run("confirmKey", func(t *testing.T) {
			t.Parallel()
			confirm := func(t *testing.T, table *icd.PeerTable, key []byte, start uint32) bool {
				t.Helper()
				ok, err := table.ConfirmKey(11, key, start)
				if err != nil {
					t.Fatal(err)
				}
				return ok
			}
			t.Run("replaces the key with the pending one at the reported start", func(t *testing.T) {
				t.Parallel()
				table := refreshingPeer(t, nil, 10, 5)
				if !confirm(t, table, fabricKeyB, 17) {
					t.Fatal("refused")
				}
				p := peerOf(t, table, 11)
				if !bytes.Equal(p.Key, fabricKeyB) || p.PendingKey != nil || p.CounterStart != 17 || p.LastOffset != 0 {
					t.Fatalf("%+v", p)
				}
			})
			t.Run("measures the last counter of a key a Check-In promoted from the reported start", func(t *testing.T) {
				t.Parallel()
				table := refreshingPeer(t, nil, 10, 5)
				processed(t, table, encodeICD(t, fabricKeyB, 20, 5000))
				if !confirm(t, table, fabricKeyB, 17) {
					t.Fatal("refused")
				}
				if p := peerOf(t, table, 11); !bytes.Equal(p.Key, fabricKeyB) || p.CounterStart != 17 || p.LastOffset != 3 {
					t.Fatalf("%+v", p)
				}
				if !processed(t, table, encodeICD(t, fabricKeyB, 20, 5000)) || peerOf(t, table, 11).LastOffset != 3 {
					t.Fatalf("%+v", peerOf(t, table, 11))
				}
			})
			t.Run("keeps the Check-In baseline when the reported start lies after the last counter", func(t *testing.T) {
				t.Parallel()
				table := refreshingPeer(t, nil, 10, 5)
				processed(t, table, encodeICD(t, fabricKeyB, 20, 5000))
				if !confirm(t, table, fabricKeyB, 25) {
					t.Fatal("refused")
				}
				if p := peerOf(t, table, 11); !bytes.Equal(p.Key, fabricKeyB) || p.CounterStart != 15 || p.LastOffset != 5 {
					t.Fatalf("%+v", p)
				}
			})
			t.Run("measures across the 2³² wrap from the reported start", func(t *testing.T) {
				t.Parallel()
				table := refreshingPeer(t, nil, 0xfffffff0, 0x10)
				processed(t, table, encodeICD(t, fabricKeyB, 2, 5000))
				if !confirm(t, table, fabricKeyB, 0xfffffffe) {
					t.Fatal("refused")
				}
				if p := peerOf(t, table, 11); p.CounterStart != 0xfffffffe || p.LastOffset != 4 {
					t.Fatalf("%+v", p)
				}
			})
			t.Run("ignores a key that is neither current nor pending", func(t *testing.T) {
				t.Parallel()
				table := refreshingPeer(t, nil, 10, 5)
				other, _ := hexKey("00112233445566778899aabbccddeeff")
				if confirm(t, table, other, 12) {
					t.Fatal("confirmed")
				}
				p := peerOf(t, table, 11)
				if !bytes.Equal(p.Key, fabricKeyA) || !bytes.Equal(p.PendingKey, fabricKeyB) || p.CounterStart != 10 || p.LastOffset != 5 {
					t.Fatalf("%+v", p)
				}
			})
		})
		t.Run("answers refreshNeeded from the current key's last offset", func(t *testing.T) {
			t.Parallel()
			table := refreshingPeer(t, nil, 10, 0x7fffffff)
			if table.RefreshNeeded(11) {
				t.Fatal("refresh at 2^31 - 1")
			}
			processed(t, table, encodeICD(t, fabricKeyA, 10+0x80000000, 5000))
			if !table.RefreshNeeded(11) {
				t.Fatal("no refresh at 2^31")
			}
			if _, err := table.ConfirmKey(11, fabricKeyB, 0x80000010); err != nil || table.RefreshNeeded(11) {
				t.Fatalf("after confirm %v %v", err, table.RefreshNeeded(11))
			}
			if newTable(nil).RefreshNeeded(11) {
				t.Fatal("unknown peer needs a refresh")
			}
		})
		t.Run("rethrows an error that is not a failed decryption", func(t *testing.T) {
			t.Parallel()
			table := newTable(nil)
			table.AddPeer(icd.Peer{PeerNodeID: 11, Key: fabricKeyA[:15], CounterStart: 10}, noop)
			if _, err := table.ProcessCheckIn(encodeICD(t, fabricKeyA, 12, 5000)); !errors.Is(err, aesccm.ErrKeySize) {
				t.Fatalf("ProcessCheckIn = %v", err)
			}
		})
		t.Run("refuses a pending key for an unregistered peer", func(t *testing.T) {
			t.Parallel()
			if err := newTable(nil).SetPendingKey(11, fabricKeyB); !errors.Is(err, icd.ErrUnknownPeer) {
				t.Fatal(err)
			}
			if _, err := newTable(nil).ConfirmKey(11, fabricKeyB, 1); !errors.Is(err, icd.ErrUnknownPeer) {
				t.Fatal(err)
			}
		})
	})

	t.Run("controller role — wakefulness", func(t *testing.T) {
		t.Parallel()
		fedRecorder := func(table *icd.PeerTable) func() []uint64 {
			var mu sync.Mutex
			var fed []uint64
			table.OnPeerFed(func(id uint64) { mu.Lock(); fed = append(fed, id); mu.Unlock() })
			return func() []uint64 { mu.Lock(); defer mu.Unlock(); return slices.Clone(fed) }
		}
		t.Run("wakefulnessFor returns an IcdPeerWakefulness after addPeer", func(t *testing.T) {
			t.Parallel()
			table := newTable(nil)
			table.AddPeer(icd.Peer{PeerNodeID: 11, Key: fabricKeyA, CounterStart: 10}, noop)
			if table.WakefulnessFor(11) == nil {
				t.Fatal("none")
			}
		})
		t.Run("processCheckIn wakes the matching peer for the threshold the Check-In carries", func(t *testing.T) {
			t.Parallel()
			fake := clocktest.New(modeEpoch)
			table := newTable(fake)
			table.AddPeer(icd.Peer{PeerNodeID: 11, Key: fabricKeyA, CounterStart: 10}, noop)
			w := table.WakefulnessFor(11)
			w.SetRequiresAwait(true)
			if w.Awake() {
				t.Fatal("awake")
			}
			processed(t, table, encodeICD(t, fabricKeyA, 12, 20000))
			fake.Advance(15 * time.Second)
			if !w.Awake() {
				t.Fatal("asleep")
			}
			w.Close()
		})
		t.Run("keeps a peer's wakefulness when the peer is registered again", func(t *testing.T) {
			t.Parallel()
			table := newTable(nil)
			fed := fedRecorder(table)
			table.AddPeer(icd.Peer{PeerNodeID: 11, Key: fabricKeyA, CounterStart: 10}, noop)
			w := table.WakefulnessFor(11)
			table.AddPeer(icd.Peer{PeerNodeID: 11, Key: fabricKeyB, CounterStart: 20}, noop)
			if table.WakefulnessFor(11) != w || !bytes.Equal(peerOf(t, table, 11).Key, fabricKeyB) || !slices.Equal(fed(), []uint64{11}) {
				t.Fatal(fed())
			}
		})
		t.Run("suspends the wakefulness of a peer whose registration is deleted", func(t *testing.T) {
			t.Parallel()
			table := newTable(nil)
			table.AddPeer(icd.Peer{PeerNodeID: 11, Key: fabricKeyA, CounterStart: 10}, noop)
			w := table.WakefulnessFor(11)
			w.SetRequiresAwait(true)
			table.DeletePeer(11)
			if _, ok := table.PeerFor(11); ok || table.WakefulnessFor(11) != nil || !w.Awake() || !w.RequiresAwait() {
				t.Fatal("not suspended")
			}
		})
		t.Run("gives a removed peer a fresh wakefulness when its node ID registers again", func(t *testing.T) {
			t.Parallel()
			table := newTable(nil)
			table.AddPeer(icd.Peer{PeerNodeID: 11, Key: fabricKeyA, CounterStart: 10}, noop)
			removed := table.WakefulnessFor(11)
			removed.SetRequiresAwait(true)
			removed.NoteActive()
			table.RemovePeer(11)
			table.AddPeer(icd.Peer{PeerNodeID: 11, Key: fabricKeyB}, noop)
			fresh := table.WakefulnessFor(11)
			if fresh == removed {
				t.Fatal("same wakefulness")
			}
			fresh.SetRequiresAwait(true)
			wantNoDue(t, fresh)
			wantNoDue(t, removed)
		})
		t.Run("resumes the same wakefulness when a deleted peer registers again, and announces it", func(t *testing.T) {
			t.Parallel()
			table := newTable(nil)
			fed := fedRecorder(table)
			table.AddPeer(icd.Peer{PeerNodeID: 11, Key: fabricKeyA, CounterStart: 10}, noop)
			w := table.WakefulnessFor(11)
			w.SetRequiresAwait(true)
			table.DeletePeer(11)
			table.AddPeer(icd.Peer{PeerNodeID: 11, Key: fabricKeyA, CounterStart: 10}, noop)
			if table.WakefulnessFor(11) != w || w.Awake() || !slices.Equal(fed(), []uint64{11, 11}) {
				t.Fatalf("awake %v fed %v", w.Awake(), fed())
			}
		})
		t.Run("wakefulnessFor returns undefined for unknown peer", func(t *testing.T) {
			t.Parallel()
			if newTable(nil).WakefulnessFor(99) != nil {
				t.Fatal("wakefulness")
			}
		})
		t.Run("close cancels peer wakefulness timers and clears peers", func(t *testing.T) {
			t.Parallel()
			fake := clocktest.New(modeEpoch)
			table := newTable(fake)
			table.AddPeer(icd.Peer{PeerNodeID: 11, Key: fabricKeyA, CounterStart: 10}, noop)
			w := table.WakefulnessFor(11)
			w.SetRequiresAwait(true)
			w.NoteActive()
			if !w.Available() {
				t.Fatal("unavailable")
			}
			table.Close()
			if table.HasPeers() || table.WakefulnessFor(11) != nil {
				t.Fatal("not cleared")
			}
			fake.Advance(600 * time.Second)
			if !w.Available() {
				t.Fatal("a leaked timer fired")
			}
		})
		t.Run("peerFed emits the peer node ID on addPeer", func(t *testing.T) {
			t.Parallel()
			table := newTable(nil)
			fed := fedRecorder(table)
			table.AddPeer(icd.Peer{PeerNodeID: 11, Key: fabricKeyA, CounterStart: 10}, noop)
			if !slices.Equal(fed(), []uint64{11}) {
				t.Fatal(fed())
			}
		})
	})
}

func hexKey(s string) ([]byte, error) {
	b := make([]byte, len(s)/2)
	_, err := fmt.Sscanf(s, "%x", &b)
	return b, err
}

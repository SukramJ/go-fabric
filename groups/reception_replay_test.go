// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package groups

// The reception state of group messages is one replay window per
// operational key and source node id, without a bound, as matter.js keeps
// it (packages/protocol/src/groups/MessagingState.ts:19, :80-90 at
// db021da0: a Map per operational key, one entry set per source node id,
// removed by forgetReceptionState :97-99 alone). These cases pin what that
// buys, which a bounded table with eviction cannot give (review II,
// R2-P0-rv-B01, owner decision 22): a recorded datagram stays a duplicate
// for as long as its key is held, however many other senders were seen
// since, and only a message that authenticates under a mapped key adds an
// entry. matter.js has no test of either to port.

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"testing"

	"github.com/SukramJ/go-fabric/store"
)

// receptionEntries is how many source node ids the Manager holds a
// reception window for under key.
func receptionEntries(m *Manager, key []byte) int {
	m.receptionMu.Lock()
	defer m.receptionMu.Unlock()
	return len(m.reception[hex.EncodeToString(key)])
}

// TestRecordedGroupMessageStaysADuplicate is the scenario of R2-P0-rv-B01:
// a party without the group key holds one recorded datagram of each of n
// senders and presents them again, round-robin and oldest first. With the
// table bounded at 15 senders and the least recently accepted one evicted,
// every such replay of 16 or more senders came back as a new message.
func TestRecordedGroupMessageStaysADuplicate(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	op, sid := opKeyOf(t, 0xA1)

	for _, senders := range []int{16, 100, 1000} {
		t.Run(fmt.Sprintf("%d senders", senders), func(t *testing.T) {
			t.Parallel()
			m, _ := newTestManager(t)
			recorded := make([][]byte, senders)
			for s := range recorded {
				recorded[s] = sealGroup(t, op, sid, 0x0101, uint64(s+1), 7, true, []byte("toggle"))
				msg, err := m.Decode(ctx, recorded[s])
				if err != nil {
					t.Fatalf("the message of sender %d: %v", s+1, err)
				}
				if msg.Duplicate {
					t.Fatalf("the first message of sender %d was taken for a replay", s+1)
				}
			}
			if got := receptionEntries(m, op); got != senders {
				t.Errorf("%d reception windows after %d senders, want one each", got, senders)
			}

			accepted := 0
			for range 3 {
				for s := range recorded {
					msg, err := m.Decode(ctx, recorded[s])
					if err != nil {
						t.Fatalf("the replay of sender %d: %v", s+1, err)
					}
					if !msg.Duplicate {
						accepted++
					}
				}
			}
			if accepted != 0 {
				t.Fatalf("%d of %d byte-identical replays were accepted as new messages", accepted, 3*senders)
			}

			// A real sender is not lost among them: its next counter is new,
			// and the one before stays refused.
			for _, source := range []uint64{1, uint64(senders)} {
				next, err := m.Decode(ctx, sealGroup(t, op, sid, 0x0101, source, 8, true, []byte("toggle")))
				if err != nil || next.Duplicate {
					t.Fatalf("the next message of sender %d: err %v, duplicate %v", source, err, next != nil && next.Duplicate)
				}
				old, err := m.Decode(ctx, recorded[source-1])
				if err != nil || !old.Duplicate {
					t.Fatalf("the recorded message of sender %d after its next one: err %v, want a duplicate", source, err)
				}
			}
		})
	}
}

// TestReceptionStateOfOneKeyIsNotAnothers: the windows are per operational
// key, so the senders seen under one key take nothing from the replay
// protection under another.
func TestReceptionStateOfOneKeyIsNotAnothers(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	op, sid := opKeyOf(t, 0xA1)
	m, _ := newTestManager(t)
	other := []byte{9, 9, 9}
	if !m.receptionWindow(other, 1).Accept(1000) {
		t.Fatal("precondition: the first counter under the other key was refused")
	}
	for source := uint64(1); source <= 100; source++ {
		if _, err := m.Decode(ctx, sealGroup(t, op, sid, 0x0101, source, 1, true, []byte("x"))); err != nil {
			t.Fatal(err)
		}
	}
	if m.receptionWindow(other, 1).Accept(1000) {
		t.Fatal("a replay under one key was accepted after 100 senders under another")
	}
	if got := receptionEntries(m, other); got != 1 {
		t.Fatalf("%d reception windows under the other key, want 1", got)
	}
}

// TestOnlyAnAuthenticatedGroupMessageCreatesReceptionState: the table grows
// with the source node ids a holder of the group key states, and with
// nothing else. A datagram that does not authenticate under a mapped key
// — a forged source node id, a changed counter, a wrong key under the
// right session id — is refused before the reception state is touched, so
// a party without the key cannot add an entry, whatever it sends.
func TestOnlyAnAuthenticatedGroupMessageCreatesReceptionState(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	op, sid := opKeyOf(t, 0xA1)
	total := func(m *Manager) int {
		m.receptionMu.Lock()
		defer m.receptionMu.Unlock()
		n := 0
		for _, perKey := range m.reception {
			n += len(perKey)
		}
		return n
	}

	t.Run("a recorded message under 1000 forged source node ids", func(t *testing.T) {
		t.Parallel()
		m, _ := newTestManager(t)
		// Without privacy the header is in the clear: flags, session id,
		// security flags, counter (4), source node id (8) at offset 8.
		genuine := sealGroup(t, op, sid, 0x0101, 1, 7, false, []byte("toggle"))
		for source := uint64(2); source <= 1001; source++ {
			forged := append([]byte(nil), genuine...)
			binary.LittleEndian.PutUint64(forged[8:16], source)
			if _, err := m.Decode(ctx, forged); !errors.Is(err, ErrDecrypt) {
				t.Fatalf("a forged source node id %d: err = %v, want ErrDecrypt", source, err)
			}
		}
		if got := total(m); got != 0 {
			t.Fatalf("%d reception windows after 1000 datagrams that do not authenticate, want none", got)
		}
		// The genuine one creates the first and only entry.
		if msg, err := m.Decode(ctx, genuine); err != nil || msg.Duplicate {
			t.Fatalf("the genuine message: err %v, duplicate %v", err, msg != nil && msg.Duplicate)
		}
		if got := total(m); got != 1 {
			t.Fatalf("%d reception windows after one authenticated message, want 1", got)
		}
	})

	t.Run("a changed counter, a changed payload, a changed tag", func(t *testing.T) {
		t.Parallel()
		m, _ := newTestManager(t)
		genuine := sealGroup(t, op, sid, 0x0101, 1, 7, false, []byte("toggle"))
		for name, at := range map[string]int{"counter": 4, "payload": len(genuine) - 17, "tag": len(genuine) - 1} {
			forged := append([]byte(nil), genuine...)
			forged[at] ^= 0x01
			if _, err := m.Decode(ctx, forged); !errors.Is(err, ErrDecrypt) {
				t.Fatalf("a changed %s: err = %v, want ErrDecrypt", name, err)
			}
		}
		if got := total(m); got != 0 {
			t.Fatalf("%d reception windows after datagrams that do not authenticate, want none", got)
		}
	})

	t.Run("a key the node does not hold, under a session id it does", func(t *testing.T) {
		t.Parallel()
		m, _ := newTestManager(t)
		foreign, _ := opKeyOf(t, 0xEE)
		for _, privacy := range []bool{false, true} {
			for source := uint64(1); source <= 100; source++ {
				if _, err := m.Decode(ctx, sealGroup(t, foreign, sid, 0x0101, source, 7, privacy, []byte("toggle"))); !errors.Is(err, ErrDecrypt) {
					t.Fatalf("privacy %v, source %d: err = %v, want ErrDecrypt", privacy, source, err)
				}
			}
		}
		if got := total(m); got != 0 {
			t.Fatalf("%d reception windows after datagrams under a foreign key, want none", got)
		}
	})

	t.Run("a key set no group is mapped to", func(t *testing.T) {
		t.Parallel()
		m, st := newTestManager(t)
		st.putKeySet(1, store.GroupKeySet{GroupKeySetID: 0x20, EpochKey0: epochKeyOf(0xB2), EpochStart0: 1})
		if err := m.Reload(ctx, 1); err != nil {
			t.Fatal(err)
		}
		unmapped, unmappedSID := opKeyOf(t, 0xB2)
		var noKey *NoKeyError
		_, err := m.Decode(ctx, sealGroup(t, unmapped, unmappedSID, 0x0101, 1, 7, true, []byte("toggle")))
		if !errors.As(err, &noKey) || !noKey.Authenticated {
			t.Fatalf("err = %v, want a NoKeyError that authenticated", err)
		}
		if got := total(m); got != 0 {
			t.Fatalf("%d reception windows after a message under an unmapped key set, want none", got)
		}
	})
}

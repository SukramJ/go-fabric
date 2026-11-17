// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package client

import (
	"errors"
	"log/slog"
	"runtime"
	"testing"

	"github.com/SukramJ/go-fabric/client/exchange"
	"github.com/SukramJ/go-fabric/client/interaction"
	"github.com/SukramJ/go-fabric/icd"
)

func icdTestClient(t *testing.T, st Store, table *icd.PeerTable) *ICDClient {
	t.Helper()
	n, err := New(Config{ID: "n", Store: st, Interaction: interaction.Bind(nil, interaction.Config{})})
	if err != nil {
		t.Fatal(err)
	}
	return newICDClient(n, icdEnv{
		table:     func(uint8) *icd.PeerTable { return table },
		ownNodeID: func(uint8) (uint64, bool) { return 1, true },
	})
}

// TestICDClientCloseRefusesNewWork (review II, R2-P6-conc-2308): once close
// returned, no operation starts on the client — its doc says it waits for
// the operations, so none may begin after the wait. Before, startLocked
// started the operation's goroutine after close, an Add after Wait.
func TestICDClientCloseRefusesNewWork(t *testing.T) {
	t.Parallel()
	c := icdTestClient(t, &MemoryStore{}, nil)
	c.close()
	ran := make(chan struct{}, 1)
	c.mu.Lock()
	op, err := c.startLocked(icdRefreshing, nil, func(*icdOperation) error { ran <- struct{}{}; return nil })
	c.mu.Unlock()
	if err == nil || op != nil {
		t.Fatalf("startLocked after close = %v, %v; want a refusal", op, err)
	}
	if !errors.Is(err, ErrICDAbandoned) {
		t.Fatalf("startLocked after close = %v, want ErrICDAbandoned", err)
	}
	select {
	case <-ran:
		t.Fatal("an operation ran after close")
	default:
	}
}

// icdSaveBlocker blocks the first ICD Save until release is closed.
type icdSaveBlocker struct {
	*MemoryStore
	saving  chan struct{}
	release chan struct{}
}

func (s *icdSaveBlocker) Save(id string, ep uint16, cluster uint32, values map[uint32][]byte, version *uint32) error {
	if cluster == icdCluster && s.saving != nil {
		close(s.saving)
		s.saving = nil
		<-s.release
	}
	return s.MemoryStore.Save(id, ep, cluster, values, version)
}

// TestICDDecommissionDuringACommitRemovesThePeer (review II,
// R2-P6-conc-2353): a decommission that runs while a registration's commit
// stores it — past its ownership check, before it feeds the peer table —
// still removes the peer the commit fed. Before, onDecommissioned read the
// fed peer before it took the state lock, found none, and the table kept
// the decommissioned peer with its key and handler.
func TestICDDecommissionDuringACommitRemovesThePeer(t *testing.T) {
	t.Parallel()
	table := icd.NewPeerTable(exchange.SystemClock{}, slog.New(slog.DiscardHandler))
	st := &icdSaveBlocker{MemoryStore: &MemoryStore{}, saving: make(chan struct{}), release: make(chan struct{})}
	saving := st.saving
	c := icdTestClient(t, st, table)
	addr := PeerAddress{FabricIndex: 1, NodeID: 0x42}
	op := &icdOperation{kind: icdRegistering, done: make(chan struct{})}
	op.ctx, op.cancel = t.Context(), func() {}
	c.mu.Lock()
	c.op = op
	c.mu.Unlock()
	committed := make(chan bool, 1)
	go func() {
		committed <- c.commitIfOwned(op, func() {
			reg := &ICDRegistration{Key: make([]byte, 16), CounterStart: 1}
			_ = c.persist(reg)
			c.feed(table, addr, reg, true)
		})
	}()
	<-saving
	decommissioned := make(chan struct{})
	go func() { c.onDecommissioned(); close(decommissioned) }()
	// Before the fix the decommission ran ahead of the commit and ended
	// here; the fixed order waits for the commit, so it is given the
	// scheduler for a while and the commit is released either way.
	for i := 0; i < 10_000; i++ {
		select {
		case <-decommissioned:
			i = 10_000
		default:
			runtime.Gosched()
		}
	}
	close(st.release)
	if !<-committed {
		t.Fatal("the commit did not run")
	}
	<-decommissioned
	if _, ok := table.PeerFor(addr.NodeID); ok {
		t.Fatal("the fabric's ICD table keeps the decommissioned peer")
	}
	c.mu.Lock()
	fed := c.fed
	c.mu.Unlock()
	if fed != nil {
		t.Fatalf("fed = %v after decommission, want nil", fed)
	}
}

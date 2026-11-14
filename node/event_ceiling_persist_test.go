// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	epmem "github.com/SukramJ/go-fabric/endpoint/memstore"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/node"
	"github.com/SukramJ/go-fabric/store"
	"github.com/SukramJ/go-fabric/store/memstore"
)

// failingCounter is a credential store whose metadata counter refuses
// writes while failWrite is set and reads while failRead is set.
type failingCounter struct {
	store.Port
	failWrite, failRead atomic.Bool
}

var errCounterRefused = errors.New("counter refused")

func (s *failingCounter) SetMetadataCounter(ctx context.Context, key string, value uint64) error {
	if s.failWrite.Load() {
		return errCounterRefused
	}
	return s.Port.SetMetadataCounter(ctx, key, value)
}

func (s *failingCounter) GetMetadataCounter(ctx context.Context, key string) (value uint64, found bool, err error) {
	if s.failRead.Load() {
		return 0, false, errCounterRefused
	}
	return s.Port.GetMetadataCounter(ctx, key)
}

func appendEvent(n *node.Node) uint64 {
	return n.Bridge().EventLog().Append(im.EventRecord{Priority: im.EventPriorityInfo, Cluster: 0x0028})
}

// TestEventNumberCeilingRefusedWriteIsRetried: a ceiling write the store
// refuses does not move the ceiling in memory; the next event retries it,
// so once the store takes it again a restart continues past every number
// handed out (R2-P2-store-02).
func TestEventNumberCeilingRefusedWriteIsRetried(t *testing.T) {
	t.Parallel()
	st := &failingCounter{Port: memstore.New()}
	cfg := minimalConfig(nil)
	cfg.Store = node.Store{Credentials: st, Endpoints: epmem.New()}
	n := newNode(t, cfg)
	first := appendEvent(n) // stores the first ceiling, one epoch ahead
	stored, _, err := st.Port.GetMetadataCounter(t.Context(), store.MetadataKeyEventNumber)
	if err != nil || stored <= first {
		t.Fatalf("stored ceiling %d (%v) after event %d", stored, err, first)
	}
	// The next number needs a new ceiling, and the store refuses it.
	n.Bridge().EventLog().SeedNumber(stored - 1)
	st.failWrite.Store(true)
	for range 3 {
		appendEvent(n)
	}
	st.failWrite.Store(false)
	last := appendEvent(n)
	if err := n.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}
	again := newNode(t, cfg)
	if got := appendEvent(again); got <= last {
		t.Errorf("event number %d after the restart; %d was handed out before it", got, last)
	}
}

// TestNewFailsWhenTheEventNumberCeilingCannotBeRead: a read error of the
// stored ceiling fails New instead of restarting the numbering at 1 and
// writing a lower ceiling over the stored one (R2-P2-ctr-07).
func TestNewFailsWhenTheEventNumberCeilingCannotBeRead(t *testing.T) {
	t.Parallel()
	st := &failingCounter{Port: memstore.New()}
	st.failRead.Store(true)
	cfg := minimalConfig(nil)
	cfg.Store = node.Store{Credentials: st, Endpoints: epmem.New()}
	n, err := node.New(t.Context(), cfg)
	if err == nil {
		_ = n.Stop(context.Background())
	}
	if !errors.Is(err, errCounterRefused) {
		t.Fatalf("New with the event-number ceiling unreadable: %v, want the store's error", err)
	}
}

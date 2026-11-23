// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package groups

import (
	"context"
	"testing"
	"time"
)

// blockingCounterStore is a memStore whose counter read waits for gate,
// holding none of the store's locks.
type blockingCounterStore struct {
	*memStore
	entered chan struct{}
	gate    chan struct{}
}

func (s *blockingCounterStore) GetMetadataCounter(ctx context.Context, key string) (uint64, bool, error) {
	select {
	case s.entered <- struct{}{}:
	default:
	}
	<-s.gate
	return s.memStore.GetMetadataCounter(ctx, key)
}

// TestSenderCounterCreationBlocksNoDecode: a Sender whose group data
// counter creation waits on the store holds no lock the receive path
// needs — a second Sender meanwhile, then a group table change, leave a
// Decode free to finish while the store still waits (review II,
// R2-C2-conc-0304; Manager's rule that mu is never held across a store
// call).
func TestSenderCounterCreationBlocksNoDecode(t *testing.T) {
	t.Parallel()
	base, _, _ := senderSetup(t)
	st := &blockingCounterStore{memStore: base.st.(*memStore), entered: make(chan struct{}, 1), gate: make(chan struct{})}
	m, err := NewManager(st, nil)
	if err != nil {
		t.Fatal(err)
	}
	m.clock = base.clock
	m.counterRandom = base.counterRandom
	m.SetTransport(&fakeTransport{})
	ctx := context.Background()
	defer close(st.gate)

	go func() { _, _ = m.Sender(ctx, 1, 257) }()
	select {
	case <-st.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the first Sender did not reach the counter store")
	}
	go func() { _, _ = m.Sender(ctx, 1, 257) }()
	pause := func() {
		select {
		case <-time.After(50 * time.Millisecond):
		case <-t.Context().Done():
		}
	}
	pause() // the second Sender reaches its wait
	policy := uint8(1)
	go func() { _ = m.SetGroupProperties(ctx, 1, 257, &policy, nil) }()
	pause() // the group change queues on the manager's lock

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = m.Decode(ctx, sealGroup(t, benchEpochKey(0xEE, 0xEEEE, 0, false), 0x1234, 257, 0x77, 1, false, imPayload([]byte("x"))))
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Decode waited for the Sender's counter store")
	}
}

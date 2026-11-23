// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package groups

import (
	"context"
	"testing"
	"testing/synctest"
)

// blockingCounterStore is a memStore whose counter read waits for gate,
// holding none of the store's locks.
type blockingCounterStore struct {
	*memStore
	entered chan struct{}
	gate    chan struct{}
}

func (s *blockingCounterStore) GetMetadataCounter(ctx context.Context, key string) (value uint64, ok bool, err error) {
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
	synctest.Test(t, func(t *testing.T) {
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
		<-st.entered
		go func() { _, _ = m.Sender(ctx, 1, 257) }()
		synctest.Wait() // the second Sender waits for the first's counter
		policy := uint8(1)
		go func() { _ = m.SetGroupProperties(ctx, 1, 257, &policy, nil) }()
		synctest.Wait() // the group change is through, or waits

		done := make(chan struct{})
		go func() {
			defer close(done)
			_, _ = m.Decode(ctx, sealGroup(t, benchEpochKey(0xEE, 0xEEEE, 0, false), 0x1234, 257, 0x77, 1, false, imPayload([]byte("x"))))
		}()
		// Before the fix the group change, and Decode behind it, waited on
		// a mutex the Sender held across the counter store: the test then
		// hangs to the package timeout (a goroutine blocked on a mutex is
		// not durably blocked, so the bubble cannot report it).
		<-done
	})
}

// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"thingfabric.dev/go-fabric/cluster/core"
)

// TestIdentifyCommandAdvancesTheVersionOnChangeOnly (review II,
// R2-P6-err-1213): the Identify command advances DataVersion when
// IdentifyTime moves, as the write does, and not for the held value.
func TestIdentifyCommandAdvancesTheVersionOnChangeOnly(t *testing.T) {
	t.Parallel()
	i := core.NewIdentify()
	t.Cleanup(i.Close)
	// Without a running countdown the held value stays: 0 → 0.
	before := i.MatterDataVersion()
	if _, err := i.MatterInvoke(context.Background(), 0x00, uint16(0)); err != nil {
		t.Fatal(err)
	}
	if after := i.MatterDataVersion(); after != before {
		t.Errorf("DataVersion %d → %d for the held IdentifyTime 0", before, after)
	}
}

// blockingIdentify holds MatterIdentify until release is closed.
type blockingIdentify struct {
	entered chan struct{}
	release chan struct{}
}

func (b *blockingIdentify) MatterIdentify(context.Context, uint16) error {
	b.entered <- struct{}{}
	<-b.release
	return nil
}

func (*blockingIdentify) MatterTriggerEffect(context.Context, uint8, uint8) error { return nil }

// TestIdentifyWaitForTheQueueHonoursTheContext (review II, R2-P6-err-1213):
// a request waiting behind a host handler that does not return gives up
// with its context.
func TestIdentifyWaitForTheQueueHonoursTheContext(t *testing.T) {
	t.Parallel()
	h := &blockingIdentify{entered: make(chan struct{}, 1), release: make(chan struct{})}
	i := core.NewIdentify()
	i.SetHandler(h)
	go func() { _, _ = i.MatterInvoke(context.Background(), 0x00, uint16(5)) }()
	<-h.entered
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := i.MatterInvoke(ctx, 0x00, uint16(7)); done <- err }()
	limit, stop := context.WithTimeout(context.Background(), 5*time.Second)
	defer stop()
	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("err %v, want the context's", err)
		}
	case <-limit.Done():
		t.Error("the waiting request ignored its context")
	}
	close(h.release)
	i.Close()
}

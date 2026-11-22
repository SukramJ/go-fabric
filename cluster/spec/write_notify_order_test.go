// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package spec_test

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"

	"thingfabric.dev/go-fabric/cluster/spec"
	hepa "thingfabric.dev/go-fabric/cluster/spec/hepafiltermonitoring"
	"thingfabric.dev/go-fabric/contract"
)

// windowSetSink runs a host Set of Condition on another goroutine while
// the write is in its Sink, waits until that Set stored its change, then
// refuses the write.
type windowSetSink struct {
	srv     *spec.Server
	stored  chan struct{}
	setDone chan error
}

func (k *windowSetSink) MatterWriteAttribute(context.Context, uint32, any) error {
	go func() { k.setDone <- k.srv.Set(hepa.AttrCondition, uint8(41)) }()
	<-k.stored
	return errors.New("refused")
}

// TestSetInAWriteWindowIsToldAfterTheAdvance (review II, phase 5b): a host
// Set that stored its change while a controller's write held the commit
// window, and reaches its notification only after the write left the
// window, is told after the version advance the write makes for it — a
// listener reading the data version on the notification sees the new
// one. Before, the Set decided whether to queue its notification after
// its store, found the window closed, and told the change before the
// write's advance.
func TestSetInAWriteWindowIsToldAfterTheAdvance(t *testing.T) {
	t.Parallel()
	sk := &windowSetSink{stored: make(chan struct{}), setDone: make(chan error, 1)}
	srv := hepaServer(t, spec.ServerConfig{Sink: sk})
	sk.srv = srv
	release := make(chan struct{})
	var once sync.Once
	var setErr error
	spec.SetWindowHooksForTests(srv,
		func() {
			once.Do(func() { close(sk.stored) })
			<-release
		},
		func() {
			close(release)
			setErr = <-sk.setDone
		})
	v0 := srv.MatterDataVersion()
	var (
		mu   sync.Mutex
		seen []uint32
	)
	srv.OnAttributeChange(func(ch contract.Change) {
		if slices.Contains(ch.Attributes, hepa.AttrCondition) {
			mu.Lock()
			seen = append(seen, srv.MatterDataVersion())
			mu.Unlock()
		}
	})
	if _, err := srv.MatterWriteCommit(context.Background(), hepa.AttrLastChangedTime, uint64(1000)); err == nil {
		t.Fatal("the Sink's refusal was not the write's error")
	}
	if setErr != nil {
		t.Fatalf("Set: %v", setErr)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(seen) != 1 || seen[0] != v0+1 {
		t.Errorf("Condition's change was told at data versions %v, want once at %d (after the write's advance)", seen, v0+1)
	}
	if d := srv.MatterDataVersion() - v0; d != 1 {
		t.Errorf("the version advanced %d times, want once", d)
	}
}

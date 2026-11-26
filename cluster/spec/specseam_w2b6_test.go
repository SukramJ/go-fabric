// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package spec_test

import (
	"context"
	"slices"
	"sync/atomic"
	"testing"

	"thingfabric.dev/go-fabric/cluster/spec"
	hepa "thingfabric.dev/go-fabric/cluster/spec/hepafiltermonitoring"
	"thingfabric.dev/go-fabric/contract"
	"thingfabric.dev/go-fabric/internal/specseam"
)

// TestSetAttributesStoredInsideAnOpenWindowW2B6 (N1, decision 689): a Set
// from another goroutine while a controller write's window is open joins
// that write's commit, and the write tells its notifications once it left
// the window. The seam's stored step runs under the server's lock, in the
// hold that stores the values and queues the notification, so the write
// cannot tell a listener of the Set before stored ran. Before, stored ran
// after the unlock: the write could leave its window and tell the change
// in between — told before the copy followed, the M1 order (decision 682).
func TestSetAttributesStoredInsideAnOpenWindowW2B6(t *testing.T) {
	t.Parallel()
	sink := &funcSink{}
	srv := hepaServer(t, spec.ServerConfig{Sink: sink})
	windowOpen, release := make(chan struct{}), make(chan struct{})
	sink.fn = func(context.Context, uint32, any) error {
		close(windowOpen)
		<-release
		return nil
	}
	var storedDone atomic.Bool
	told := make(chan bool, 4) // storedDone when the Set's change is told
	srv.OnAttributeChange(func(c contract.Change) {
		if slices.Contains(c.Attributes, hepa.AttrCondition) {
			told <- storedDone.Load()
		}
	})
	writeDone := make(chan error, 1)
	go func() {
		_, err := srv.MatterWriteCommit(context.Background(), hepa.AttrLastChangedTime, uint32(1000))
		writeDone <- err
	}()
	<-windowOpen
	stored := func() {
		// The writer waits in its Sink and holds no lock: the server's lock
		// is held here only when stored runs under it.
		locked := spec.ServerLockedW2B6(srv)
		close(release)
		if !locked {
			t.Error("stored ran outside the server's lock")
			// The write may now leave its window and tell its commit's
			// notifications, this Set's among them: wait for it.
			v := <-told
			told <- v
		}
		storedDone.Store(true)
	}
	if err := specseam.SetAttributesStored(srv, contract.OriginHostSet, map[uint32]any{hepa.AttrCondition: uint8(41)}, stored); err != nil {
		t.Fatal(err)
	}
	if err := <-writeDone; err != nil {
		t.Fatal(err)
	}
	select {
	case done := <-told:
		if !done {
			t.Error("the listener was told of the Set before its stored step ran")
		}
	default:
		t.Fatal("the write's commit never told the Set's change")
	}
}

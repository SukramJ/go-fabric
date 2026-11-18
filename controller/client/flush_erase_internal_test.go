// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package client

import (
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/cluster/spec"
)

// TestFlushDoesNotWriteAnErasedClusterBack (review II, R2-P6-store-2307):
// a cluster the structure erases while a flush writes its snapshot — the
// write runs without the node's lock — is not left in the store: the
// flush erases it again once its writes are done. Before, the flush's
// write put the erased cluster's values and version back, and the next
// load found a cluster the peer no longer has.
func TestFlushDoesNotWriteAnErasedClusterBack(t *testing.T) {
	t.Parallel()
	st := &blockingStore{entered: make(chan struct{}), release: make(chan struct{})}
	p, image := newTestPersister(st, 5*time.Second, newManualClock())
	image.Lock()
	c := newCache(p, 1, 6, nil, nil)
	c.externalSet(map[uint32]*spec.Node{0: ptr(boolNode(true))}, nil)
	image.Unlock()
	flushed := make(chan struct{})
	go func() {
		defer close(flushed)
		p.flush()
	}()
	<-st.entered // the flush took its snapshot and writes it
	image.Lock()
	c.erase() // the endpoint or cluster removed by a report, under the node's lock
	image.Unlock()
	close(st.release)
	<-flushed
	if _, _, ok := stored(t, st, 1, 6); ok {
		t.Fatal("the erased cluster is in the store after the flush")
	}
}

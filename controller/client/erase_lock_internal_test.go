// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package client

import (
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/controller/interaction"
)

// readingStore reads the node from inside EraseNode, as a host store that
// logs what it erases would.
type readingStore struct {
	*MemoryStore
	n *Node
}

func (s *readingStore) EraseNode(nodeID string) error {
	if s.n != nil {
		_ = s.n.Record()
	}
	return s.MemoryStore.EraseNode(nodeID)
}

// TestEraseCallsTheStoreWithoutTheLock pins G1-16 (ADR 0026 P6): Erase
// erases the host's store with the node's lock released.
func TestEraseCallsTheStoreWithoutTheLock(t *testing.T) {
	t.Parallel()
	st := &readingStore{MemoryStore: &MemoryStore{}}
	n, err := New(Config{ID: "peer1", Store: st, Interaction: interaction.Bind(nil, interaction.Config{})})
	if err != nil {
		t.Fatal(err)
	}
	st.n = n
	done := make(chan error, 1)
	go func() { done <- n.Erase(t.Context()) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second): // a held lock never releases: this bound only names the failure
		t.Fatal("Erase holds the node's lock while the store erases")
	}
}

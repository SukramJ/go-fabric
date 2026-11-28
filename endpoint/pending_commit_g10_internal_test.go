// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package endpoint

import (
	"context"
	"testing"
)

func g10HasState(a *Assembler, key string) bool {
	a.states.mu.Lock()
	defer a.states.mu.Unlock()
	_, ok := a.states.states[StringKey(key)]
	return ok
}

// TestPendingCommit (P10-fsm-6001): AssemblePending changes the registry
// only on Commit; Commit is idempotent; a pending commit is void once the
// next assembly began; Assemble commits before it returns.
func TestPendingCommit(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	a := c3Assembler(t)
	snap := func(key string) []Snapshot {
		return []Snapshot{{Scope: "s", Endpoints: []Spec{flowSpec(StringKey(key), "Flow")}}}
	}
	topo, err := a.AssemblePending(ctx, snap("a"))
	if err != nil {
		t.Fatal(err)
	}
	if g10HasState(a, "a") {
		t.Fatal("AssemblePending wrote the key's state before Commit")
	}
	topo.Commit()
	topo.Commit()
	if !g10HasState(a, "a") {
		t.Fatal("Commit did not write the key's state")
	}

	stale, err := a.AssemblePending(ctx, snap("b"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Assemble(ctx, snap("a")); err != nil {
		t.Fatal(err)
	}
	stale.Commit()
	if !g10HasState(a, "a") || g10HasState(a, "b") {
		t.Error("a pending commit ran after the next assembly began")
	}
	(&Topology{}).Commit()
	(*Topology)(nil).Commit()
}

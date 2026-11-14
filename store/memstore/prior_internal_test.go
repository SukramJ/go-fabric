// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package memstore

import (
	"bytes"
	"context"
	"testing"

	"github.com/SukramJ/go-fabric/store"
)

// TestCommitAndRollbackErasePriors is R2-P2-dr-noc-06 for the in-memory
// store: after CommitFabric and after RollbackFabric of an update, no
// prior value is held for the fabric — not the replaced NOC, ICAC or
// operational private key. A leftover is unobservable through the port,
// so the store's own fields are read.
func TestCommitAndRollbackErasePriors(t *testing.T) {
	t.Parallel()
	for _, end := range []string{"commit", "rollback"} {
		t.Run(end, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			s := New()
			idx, err := s.AddFabric(ctx, store.FabricRecord{FabricID: 1, NodeID: 2, RootPublicKey: []byte{4}, VIDVerificationStatement: []byte{4}, VVSC: []byte{5}})
			if err != nil {
				t.Fatal(err)
			}
			if err := s.UpsertIdentity(ctx, store.IdentityRecord{FabricIndex: idx, NOC: []byte{1}, ICAC: []byte{2}, PrivateKey: bytes.Repeat([]byte{3}, 32), IPK: bytes.Repeat([]byte{6}, 16)}); err != nil {
				t.Fatal(err)
			}
			if err := s.UpdateFabricCredentials(ctx, idx, store.FabricCredentials{NOC: []byte{9}, ICAC: []byte{9}, PrivateKey: bytes.Repeat([]byte{9}, 32), NodeID: 9}); err != nil {
				t.Fatal(err)
			}
			s.mu.Lock()
			p, held := s.priors[idx]
			s.mu.Unlock()
			if !held || p.noc == nil || p.icac == nil || p.privateKey == nil {
				t.Fatalf("priors after the update = %+v, %v: the read does not see them", p, held)
			}
			if end == "commit" {
				err = s.CommitFabric(ctx, idx)
			} else {
				_, err = s.RollbackFabric(ctx, idx)
			}
			if err != nil {
				t.Fatal(err)
			}
			s.mu.Lock()
			p, held = s.priors[idx]
			s.mu.Unlock()
			if held {
				t.Fatalf("priors left after the %s: %+v, want none", end, p)
			}
		})
	}
}

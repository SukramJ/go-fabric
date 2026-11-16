// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package store

import (
	"bytes"
	"errors"
	"path/filepath"
	"testing"
)

// TestSealUnmarkedReadsTheCanaryInItsTransaction (Fable read R2): two first
// NewSealed under different keys on one file. The second one's check before
// its transaction saw no canary; its transaction reads the canary again,
// finds the first one's, writes nothing — the canary stays under the first
// key — and the open then fails with ErrUnseal under the second key.
func TestSealUnmarkedReadsTheCanaryInItsTransaction(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	db, err := OpenSQLite(filepath.Join(t.TempDir(), "matter.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := Apply(ctx, db); err != nil {
		t.Fatal(err)
	}
	sealer := func(seed byte) Sealer {
		s, err := NewAESGCMSealer(bytes.Repeat([]byte{seed}, 32))
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	a, b := &Store{db: db, sealer: sealer(1)}, &Store{db: db, sealer: sealer(2)}
	if marked, err := a.sealUnmarked(ctx); err != nil || marked {
		t.Fatalf("the first sealing = %v, %v", marked, err)
	}
	stored, sealed, _, err := readCanary(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	// The second NewSealed, past its check before the transaction.
	marked, err := b.sealUnmarked(ctx)
	if err != nil || !marked {
		t.Fatalf("the second sealing = %v, %v; want it to find the canary", marked, err)
	}
	if again, _, _, _ := readCanary(ctx, db); again != stored {
		t.Error("the second sealing replaced the canary")
	}
	if err := a.openCanary(stored, sealed); err != nil {
		t.Errorf("the canary under the first key: %v", err)
	}
	if err := b.openMarked(ctx); !errors.Is(err, ErrUnseal) {
		t.Errorf("the second key's open of the marked database = %v, want ErrUnseal", err)
	}
}

// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package store_test

import (
	"bytes"
	"testing"

	store "github.com/SukramJ/go-fabric/store"
)

func TestServerSubscriptions_Lifecycle(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	s := store.New(openTestDB(t))

	if rows, err := s.LoadServerSubscriptions(ctx); err != nil || len(rows) != 0 {
		t.Fatalf("empty table: rows=%d err=%v", len(rows), err)
	}
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(s.SaveServerSubscription(ctx, 0xFFFFFFF0, 1, 0xC0FFEE, []byte("a")))
	must(s.SaveServerSubscription(ctx, 2, 1, 0xC0FFEE, []byte("b")))
	must(s.SaveServerSubscription(ctx, 3, 2, 0xBEEF, []byte("c")))
	// Upsert by subscription id.
	must(s.SaveServerSubscription(ctx, 2, 1, 0xC0FFEE, []byte("b2")))

	rows, err := s.LoadServerSubscriptions(ctx)
	must(err)
	if len(rows) != 3 {
		t.Fatalf("rows = %d, want 3", len(rows))
	}
	var sawB2 bool
	for _, r := range rows {
		if bytes.Equal(r, []byte("b2")) {
			sawB2 = true
		}
		if bytes.Equal(r, []byte("b")) {
			t.Fatal("upsert kept the stale payload")
		}
	}
	if !sawB2 {
		t.Fatal("upserted payload missing")
	}

	must(s.DeleteServerSubscription(ctx, 0xFFFFFFF0))
	must(s.DeleteServerSubscription(ctx, 0xFFFFFFF0)) // idempotent
	must(s.DeleteServerSubscriptionsByFabric(ctx, 2))
	rows, err = s.LoadServerSubscriptions(ctx)
	must(err)
	if len(rows) != 1 || !bytes.Equal(rows[0], []byte("b2")) {
		t.Fatalf("after deletes rows = %q", rows)
	}
	must(s.ClearServerSubscriptions(ctx))
	if rows, _ := s.LoadServerSubscriptions(ctx); len(rows) != 0 {
		t.Fatalf("after clear rows = %d", len(rows))
	}
}

// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"context"
	"path/filepath"
	"sync"
	"testing"

	"github.com/SukramJ/go-fabric/bridge/bridgetest"
	"github.com/SukramJ/go-fabric/node"
)

// TestFailedAddNOCIsNotReportedAsRemoved (review II, rework2): an AddNOC
// that fails after its fabric row was written, and whose own rollback
// fails too, never reached the host as an added fabric; when the
// fail-safe's end rolls the row back, the host is not told of a removal
// either.
func TestFailedAddNOCIsNotReportedAsRemoved(t *testing.T) {
	t.Parallel()
	db := openDB(t, filepath.Join(t.TempDir(), "node.db"))
	n := newNode(t, minimalConfig(db))
	var (
		mu             sync.Mutex
		added, removed []node.FabricInfo
	)
	n.Observe(node.Observers{
		OnFabricAdded: func(info node.FabricInfo) {
			mu.Lock()
			added = append(added, info)
			mu.Unlock()
		},
		OnFabricRemoved: func(info node.FabricInfo) {
			mu.Lock()
			removed = append(removed, info)
			mu.Unlock()
		},
	})
	if err := n.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	// The ACL write fails AddNOC after its fabric row; the removal its
	// own rollback runs is refused as well, so the row stays pending.
	for _, stmt := range []string{
		`CREATE TRIGGER refuse_acl BEFORE INSERT ON matter_acl_entries BEGIN SELECT RAISE(ABORT, 'test: acl refused'); END`,
		`CREATE TRIGGER refuse_removal BEFORE DELETE ON matter_fabrics BEGIN SELECT RAISE(ABORT, 'test: removal refused'); END`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	c := bridgetest.Dial(t, n.Bridge().LocalAddr(), commissionerOptions())
	if _, err := c.Commission(context.Background()); err == nil {
		t.Fatal("Commission succeeded although the store refused the ACL")
	}
	for _, trigger := range []string{"refuse_acl", "refuse_removal"} {
		if _, err := db.Exec(`DROP TRIGGER ` + trigger); err != nil {
			t.Fatal(err)
		}
	}
	// The fail-safe's end (the commissioner's failure handling may have
	// ended it already, while the removal was still refused).
	n.Root().OperationalCredentials.OnFailSafeExpiry(context.Background(), 0)
	if fabrics, err := n.FabricInfos(context.Background()); err != nil || len(fabrics) != 0 {
		t.Fatalf("fabrics after the fail-safe's end: %+v, %v; want none", fabrics, err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(added) != 0 || len(removed) != 0 {
		t.Fatalf("the host saw added %+v, removed %+v; want neither", added, removed)
	}
}

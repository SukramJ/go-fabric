// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package sqlstore_test

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/controller/client/sqlstore"
)

// connectionPragmas ride on the DSN so every pooled connection has them
// (store/testhelper_test.go).
const connectionPragmas = "_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)"

func openEmptyDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "client.db")+"?"+connectionPragmas)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db := openEmptyDB(t)
	if err := sqlstore.Apply(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	return db
}

func ptr[T any](v T) *T { return &v }

// TestStoreRoundTrip: values and versions survive, a nil value deletes its
// key, a nil version clears the stored one, Load sorts and copies — the
// contract client.MemoryStore keeps.
func TestStoreRoundTrip(t *testing.T) {
	t.Parallel()
	st := sqlstore.New(openTestDB(t))
	mem := &client.MemoryStore{}
	ops := []func(s client.Store) error{
		func(s client.Store) error {
			return s.Save("n", 2, 6, map[uint32][]byte{0: {1}, 1: {2}}, ptr(uint32(7)))
		},
		func(s client.Store) error { return s.Save("n", 0, 0x1D, map[uint32][]byte{3: {3}}, nil) },
		func(s client.Store) error { return s.Save("n", 0, 0xFFFF_FFFF, map[uint32][]byte{0: {9}}, nil) },
		func(s client.Store) error {
			return s.Save("n", 2, 6, map[uint32][]byte{0: nil, 5: {5}}, ptr(uint32(0xFFFF_FFFF)))
		},
		func(s client.Store) error { return s.Save("n", 1, 8, map[uint32][]byte{0: nil}, ptr(uint32(1))) },
		func(s client.Store) error { return s.Save("n", 1, 8, nil, nil) },
		func(s client.Store) error { return s.Save("m", 3, 3, map[uint32][]byte{0: {1}}, nil) },
	}
	for i, op := range ops {
		if err := op(st); err != nil {
			t.Fatalf("op %d: %v", i, err)
		}
		_ = op(mem)
	}
	for _, id := range []string{"n", "m", "unknown"} {
		got, err := st.Load(id)
		if err != nil {
			t.Fatal(err)
		}
		want, _ := mem.Load(id)
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("%s: sqlstore %+v\nmemory %+v", id, got, want)
		}
	}
	got, _ := st.Load("n")
	got[0].Clusters[0].Values[3][0] = 0xEE
	if again, _ := st.Load("n"); again[0].Clusters[0].Values[3][0] == 0xEE {
		t.Fatal("Load does not copy")
	}
	if ids, err := st.NodeIDs(); err != nil || !slices.Equal(ids, []string{"m", "n"}) {
		t.Fatalf("NodeIDs %v %v", ids, err)
	}
}

// TestStoreErase: the three erase levels, and NodeIDs after them.
func TestStoreErase(t *testing.T) {
	t.Parallel()
	st := sqlstore.New(openTestDB(t))
	for _, ep := range []uint16{0, 1} {
		for _, c := range []uint32{6, 8} {
			if err := st.Save("n", ep, c, map[uint32][]byte{0: {1}}, ptr(uint32(1))); err != nil {
				t.Fatal(err)
			}
		}
	}
	_ = st.Save("o", 0, 6, map[uint32][]byte{0: {1}}, nil)
	if err := st.EraseCluster("n", 1, 6); err != nil {
		t.Fatal(err)
	}
	if err := st.EraseEndpoint("n", 0); err != nil {
		t.Fatal(err)
	}
	eps, _ := st.Load("n")
	if len(eps) != 1 || eps[0].Number != 1 || len(eps[0].Clusters) != 1 || eps[0].Clusters[0].ID != 8 {
		t.Fatalf("after the erases %+v", eps)
	}
	if err := st.EraseNode("n"); err != nil {
		t.Fatal(err)
	}
	if eps, _ := st.Load("n"); len(eps) != 0 {
		t.Fatal("EraseNode left endpoints")
	}
	if ids, _ := st.NodeIDs(); !slices.Equal(ids, []string{"o"}) {
		t.Fatalf("NodeIDs %v", ids)
	}
	_ = st.EraseNode("o")
	if ids, _ := st.NodeIDs(); len(ids) != 0 {
		t.Fatalf("NodeIDs of an empty store %v", ids)
	}
}

// TestStoreConcurrent saves, loads and erases from many goroutines (run it
// under -race).
func TestStoreConcurrent(t *testing.T) {
	t.Parallel()
	st := sqlstore.New(openTestDB(t))
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id := fmt.Sprint("n", i%2)
			for j := range 20 {
				if err := st.Save(id, uint16(j%3), 6, map[uint32][]byte{uint32(j): {byte(j)}}, nil); err != nil { //nolint:gosec // a test index
					t.Error(err)
					return
				}
				if _, err := st.Load(id); err != nil {
					t.Error(err)
					return
				}
				if j%10 == 0 {
					_ = st.EraseEndpoint(id, 1)
				}
			}
		}()
	}
	wg.Wait()
	if ids, _ := st.NodeIDs(); !slices.Equal(ids, []string{"n0", "n1"}) {
		t.Fatalf("NodeIDs %v", ids)
	}
}

// TestStoreErrors: a database without the schema fails every call.
func TestStoreErrors(t *testing.T) {
	t.Parallel()
	st := sqlstore.New(openEmptyDB(t))
	if _, err := st.Load("n"); err == nil {
		t.Error("Load")
	}
	if err := st.Save("n", 0, 6, map[uint32][]byte{0: {1}}, nil); err == nil {
		t.Error("Save")
	}
	if err := st.Save("n", 0, 6, nil, nil); err == nil {
		t.Error("Save of a version")
	}
	if err := st.EraseNode("n"); err == nil {
		t.Error("EraseNode")
	}
	if _, err := st.NodeIDs(); err == nil {
		t.Error("NodeIDs")
	}
	closed := openTestDB(t)
	_ = closed.Close()
	cs := sqlstore.New(closed)
	if err := cs.Save("n", 0, 6, nil, nil); err == nil {
		t.Error("Save on a closed database")
	}
	if err := cs.EraseCluster("n", 0, 6); err == nil {
		t.Error("EraseCluster on a closed database")
	}
	if _, err := sqlstore.SchemaFingerprint(context.Background(), closed); err == nil {
		t.Error("SchemaFingerprint on a closed database")
	}
	if err := sqlstore.Apply(context.Background(), closed); err == nil {
		t.Error("Apply on a closed database")
	}
}

// TestMigrationsMatchApply mirrors store's guard: the list applied to an
// empty database and Apply on another leave the same schema; Apply after
// the list changes nothing; each Down reverts its Up.
func TestMigrationsMatchApply(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	byList := openEmptyDB(t)
	for _, m := range sqlstore.Migrations() {
		if _, err := byList.ExecContext(ctx, m.Up); err != nil {
			t.Fatalf("%s up: %v", m.Version, err)
		}
	}
	byApply := openTestDB(t)
	a, err := sqlstore.SchemaFingerprint(ctx, byList)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := sqlstore.SchemaFingerprint(ctx, byApply)
	if a != b {
		t.Fatalf("SchemaFingerprint: migrations %s, Apply %s", a, b)
	}
	if err := sqlstore.Apply(ctx, byList); err != nil {
		t.Fatal(err)
	}
	if after, _ := sqlstore.SchemaFingerprint(ctx, byList); after != a {
		t.Fatal("Apply after the list moved the fingerprint")
	}
	empty, _ := sqlstore.SchemaFingerprint(ctx, openEmptyDB(t))
	if empty == a {
		t.Fatal("the fingerprint ignores the tables")
	}
	ms := sqlstore.Migrations()
	for i := len(ms) - 1; i >= 0; i-- {
		if _, err := byList.ExecContext(ctx, ms[i].Down); err != nil {
			t.Fatalf("%s down: %v", ms[i].Version, err)
		}
	}
	if down, _ := sqlstore.SchemaFingerprint(ctx, byList); down != empty {
		t.Fatal("the Downs left tables")
	}
	// Another table and rows do not move it.
	if _, err := byApply.ExecContext(ctx, `CREATE TABLE host_table(x)`); err != nil {
		t.Fatal(err)
	}
	_ = sqlstore.New(byApply).Save("n", 0, 6, map[uint32][]byte{0: {1}}, nil)
	if c, _ := sqlstore.SchemaFingerprint(ctx, byApply); c != a {
		t.Fatal("a host table or a row moved the fingerprint")
	}
}

// TestMigrationsPin is the pin a host keeps: one step, v1; the schema
// names only matter_client_* tables.
func TestMigrationsPin(t *testing.T) {
	t.Parallel()
	ms := sqlstore.Migrations()
	if len(ms) != 1 || ms[0].Version != "v1" {
		t.Fatalf("migrations %+v", ms)
	}
	for _, line := range strings.Split(sqlstore.Schema(), "\n") {
		if strings.HasPrefix(line, "CREATE TABLE") && !strings.Contains(line, " matter_client_") {
			t.Fatalf("a table outside matter_client_*: %s", line)
		}
	}
}

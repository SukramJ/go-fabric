// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package sqlitestore_test

import (
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/endpoint/sqlitestore"
)

// insertRow writes a row past the store — what an older version, a
// restore or a host's own tooling leaves behind — without touching the
// counter.
func insertRow(t *testing.T, db *sql.DB, key string, id uint16) {
	t.Helper()
	if _, err := db.ExecContext(t.Context(),
		`INSERT INTO matter_endpoints (source_key, scope, endpoint_id, device_type) VALUES (?, 'scope', ?, 256)`,
		key, int64(id)); err != nil {
		t.Fatalf("insert %s=%d: %v", key, id, err)
	}
}

func place(t *testing.T, s *sqlitestore.Store, key string, id uint16) {
	t.Helper()
	if _, err := s.UpsertEndpointAssigning(t.Context(), endpoint.Record{
		Key: endpoint.StringKey(key), Scope: "scope", EndpointID: id, DeviceType: 0x0100,
	}); err != nil {
		t.Fatalf("place %s=%d: %v", key, id, err)
	}
}

func remove(t *testing.T, s *sqlitestore.Store, key string) {
	t.Helper()
	if err := s.RemoveEndpoint(t.Context(), endpoint.StringKey(key)); err != nil {
		t.Fatalf("remove %s: %v", key, err)
	}
}

// TestRowAboveTheCounterIsNotReissued: a row the counter never saw is
// lifted past, as matter.js reserves every persisted number at load
// (ServerEndpointStores.ts:52-60) and lifts its counter past each it
// claims (:156-158).
func TestRowAboveTheCounterIsNotReissued(t *testing.T) {
	t.Parallel()
	db := openAt(t, filepath.Join(t.TempDir(), "endpoints.db"))
	s := sqlitestore.New(db)
	insertRow(t, db, "src:old", 7)
	if got := assign(t, s, "src:first", "scope", 0x0100); got != 8 {
		t.Errorf("allocated %d, want 8 (past the persisted 7)", got)
	}
	if got := assign(t, s, "src:second", "scope", 0x0100); got != 9 {
		t.Errorf("allocated %d, want 9", got)
	}
}

// TestAllocationWrapsToTheFirstFreeNumber: past 0xFFFE the walk wraps to
// the first bridged number and skips the held ones (matter.js
// ServerEndpointStores.ts #nextFreeNumber :169-181).
func TestAllocationWrapsToTheFirstFreeNumber(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	for _, k := range []string{"a", "b", "c"} {
		assign(t, s, "src:"+k, "scope", 0x0100) // 2, 3, 4
	}
	remove(t, s, "src:b")
	place(t, s, "src:high", 65533) // the counter is now 65534
	if got := assign(t, s, "src:top", "scope", 0x0100); got != 65534 {
		t.Fatalf("allocated %d, want the last number 65534", got)
	}
	if got := assign(t, s, "src:wrapped", "scope", 0x0100); got != 3 {
		t.Errorf("allocated %d past 0xFFFE, want the first free number 3 (2 and 4 held)", got)
	}
	if got := assign(t, s, "src:next", "scope", 0x0100); got != 5 {
		t.Errorf("allocated %d, want 5", got)
	}
}

// TestFullTableIsExhausted: ErrEndpointsExhausted only once every number
// from 2 to 65534 is held (matter.js ServerEndpointStores.ts:183, after a
// full pass); freeing one makes it the next.
func TestFullTableIsExhausted(t *testing.T) {
	t.Parallel()
	if raceEnabled {
		// One goroutine, nothing for the detector to find; 65533 rows
		// through the transpiled SQLite under -race take over a minute.
		t.Skip("skipped under -race; runs in make test")
	}
	db := openAt(t, filepath.Join(t.TempDir(), "endpoints.db"))
	s := sqlitestore.New(db)
	if _, err := db.ExecContext(t.Context(),
		`WITH RECURSIVE c(n) AS (SELECT 2 UNION ALL SELECT n + 1 FROM c WHERE n < 65534)
		 INSERT INTO matter_endpoints (source_key, scope, endpoint_id, device_type)
		 SELECT 'src:' || n, 'scope', n, 256 FROM c`); err != nil {
		t.Fatalf("fill: %v", err)
	}
	_, err := s.UpsertEndpointAssigning(t.Context(), endpoint.Record{
		Key: endpoint.StringKey("src:one-too-many"), Scope: "scope", DeviceType: 0x0100,
	})
	if !errors.Is(err, sqlitestore.ErrEndpointsExhausted) {
		t.Fatalf("full table: %v, want ErrEndpointsExhausted", err)
	}
	remove(t, s, "src:100")
	if got := assign(t, s, "src:one-too-many", "scope", 0x0100); got != 100 {
		t.Errorf("allocated %d, want the freed 100", got)
	}
}

// TestBusyWriterIsWaitedOut is brief AV's D2 check on the SQLITE_BUSY
// retry the issue (#121) proposed: a second connection holding a write
// transaction makes the upsert fail with SQLITE_BUSY without a busy
// timeout, and with one — in WAL and in rollback-journal mode — the
// upsert waits the holder out and succeeds, because BEGIN IMMEDIATE takes
// the write lock through the busy handler. busy_timeout covers the case,
// so the store carries no retry of its own.
func TestBusyWriterIsWaitedOut(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name    string
		pragmas string
		wantErr bool
	}{
		{"WAL, busy_timeout", "_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)", false},
		{"rollback journal, busy_timeout", "_pragma=busy_timeout(5000)&_pragma=journal_mode(DELETE)", false},
		{"WAL, no busy_timeout", "_pragma=busy_timeout(0)&_pragma=journal_mode(WAL)", true},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "endpoints.db")
			open := func() *sql.DB {
				db, err := sql.Open("sqlite", "file:"+path+"?"+c.pragmas)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = db.Close() })
				return db
			}
			holderDB, storeDB := open(), open()
			if err := sqlitestore.Apply(t.Context(), holderDB); err != nil {
				t.Fatal(err)
			}
			holder, err := holderDB.Conn(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = holder.Close() }()
			if _, err := holder.ExecContext(t.Context(), `BEGIN IMMEDIATE`); err != nil {
				t.Fatal(err)
			}
			if _, err := holder.ExecContext(t.Context(),
				`INSERT INTO matter_endpoints (source_key, scope, endpoint_id, device_type) VALUES ('src:holder', 'scope', 50, 256)`); err != nil {
				t.Fatal(err)
			}
			const hold = 300 * time.Millisecond
			released := make(chan error, 1)
			go func() {
				time.Sleep(hold)
				_, err := holder.ExecContext(t.Context(), `COMMIT`)
				released <- err
			}()

			start := time.Now()
			id, err := sqlitestore.New(storeDB).UpsertEndpointAssigning(t.Context(), endpoint.Record{
				Key: endpoint.StringKey("src:waiting"), Scope: "scope", DeviceType: 0x0100,
			})
			waited := time.Since(start)
			if rerr := <-released; rerr != nil {
				t.Fatalf("holder commit: %v", rerr)
			}
			if c.wantErr {
				if err == nil || !strings.Contains(err.Error(), "SQLITE_BUSY") {
					t.Fatalf("without a busy timeout: %v, want SQLITE_BUSY", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("upsert behind a writer: %v", err)
			}
			if waited < hold/2 {
				t.Errorf("returned after %v, before the holder let go", waited)
			}
			if id != 51 {
				t.Errorf("allocated %d, want 51 (past the holder's committed 50)", id)
			}
		})
	}
}

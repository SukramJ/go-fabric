// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package sqlitestore_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/endpoint/endpointtest"
	"github.com/SukramJ/go-fabric/endpoint/sqlitestore"
)

// The store must satisfy the port it exists to implement; a signature drift
// on either side is a compile error rather than a test failure.
var _ endpoint.Store = (*sqlitestore.Store)(nil)

// connectionPragmas ride on the DSN because SQLite resets connection-scoped
// pragmas per connection, and a pool opens more than one.
const connectionPragmas = "_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)"

// openAt opens (creating on first use) a database at path with the schema
// applied, and closes it when the test ends. Two calls with the same path
// are what a restart looks like from this package's side.
func openAt(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+path+"?"+connectionPragmas)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := sqlitestore.Apply(t.Context(), db); err != nil {
		t.Fatalf("apply schema: %v", err)
	}
	return db
}

// newStore returns a store over a fresh database in t's temp directory.
func newStore(t *testing.T) *sqlitestore.Store {
	t.Helper()
	return sqlitestore.New(openAt(t, filepath.Join(t.TempDir(), "endpoints.db")))
}

// assign is the "new source arrives" call the assembler makes.
func assign(t *testing.T, s *sqlitestore.Store, key, scope string, deviceType uint16) uint16 {
	t.Helper()
	id, err := s.UpsertEndpointAssigning(t.Context(), endpoint.Record{
		Key: endpoint.StringKey(key), Scope: scope, DeviceType: deviceType,
	})
	if err != nil {
		t.Fatalf("assign %s: %v", key, err)
	}
	return id
}

// TestApplyIsIdempotent covers the boot path: a host applies the schema on
// every start, so the second application must be a no-op rather than a
// "table already exists" failure that only shows up on the second run.
func TestApplyIsIdempotent(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "endpoints.db")
	db := openAt(t, path)
	s := sqlitestore.New(db)
	first := assign(t, s, "src:1", "scope", 0x0100)

	if err := sqlitestore.Apply(t.Context(), db); err != nil {
		t.Fatalf("second Apply: %v", err)
	}
	rec, err := s.GetEndpoint(t.Context(), endpoint.StringKey("src:1"))
	if err != nil {
		t.Fatalf("GetEndpoint after re-Apply: %v", err)
	}
	if rec.EndpointID != first {
		t.Errorf("re-applying the schema moved src:1 from endpoint %d to %d", first, rec.EndpointID)
	}
}

// TestSchemaIsTheEmbeddedText guards the reason the DDL moved into the
// module: Schema must hand back real DDL, so a host feeding it to its own
// migration tool gets the tables rather than an empty string.
func TestSchemaIsTheEmbeddedText(t *testing.T) {
	t.Parallel()
	if !strings.Contains(sqlitestore.Schema(), "CREATE TABLE IF NOT EXISTS matter_endpoints") {
		t.Errorf("Schema() does not create matter_endpoints:\n%s", sqlitestore.Schema())
	}
}

// TestGetEndpointMissReturnsErrNotFound pins the sentinel the assembler
// branches on. Any other error makes the whole assembly fail instead of
// allocating a number for a new source.
func TestGetEndpointMissReturnsErrNotFound(t *testing.T) {
	t.Parallel()
	_, err := newStore(t).GetEndpoint(t.Context(), endpoint.StringKey("absent"))
	if !errors.Is(err, endpoint.ErrNotFound) {
		t.Errorf("GetEndpoint(absent) = %v, want endpoint.ErrNotFound", err)
	}
}

// TestEndpointIDsSurviveAReopen is the whole point of the package: the
// numbers a controller has cached must still be there after a restart.
func TestEndpointIDsSurviveAReopen(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "endpoints.db")

	first := sqlitestore.New(openAt(t, path))
	lightID := assign(t, first, "demo:light:1", "demo", 0x0100)
	thermoID := assign(t, first, "demo:thermometer:1", "demo", 0x0302)

	// A second handle on the same file stands in for the restart: nothing
	// of the first store's in-process state carries over.
	second := sqlitestore.New(openAt(t, path))
	for _, want := range []struct {
		key string
		id  uint16
	}{{"demo:light:1", lightID}, {"demo:thermometer:1", thermoID}} {
		rec, err := second.GetEndpoint(t.Context(), endpoint.StringKey(want.key))
		if err != nil {
			t.Fatalf("GetEndpoint(%s) after reopen: %v", want.key, err)
		}
		if rec.EndpointID != want.id {
			t.Errorf("%s came back as endpoint %d, was %d before the restart", want.key, rec.EndpointID, want.id)
		}
	}
	// And a source that arrives after the restart must not land on a
	// number the counter already handed out.
	if got := assign(t, second, "demo:light:2", "demo", 0x0100); got == lightID || got == thermoID {
		t.Errorf("post-restart allocation returned %d, which is already in use", got)
	}
}

// TestRemovedNumberIsNeverReissued is the invariant the port documents. A
// reissued number makes a new accessory arrive under a removed one's
// identity on every controller that cached the list.
func TestRemovedNumberIsNeverReissued(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	gone := assign(t, s, "src:gone", "scope", 0x0100)

	if err := s.RemoveEndpoint(t.Context(), endpoint.StringKey("src:gone")); err != nil {
		t.Fatalf("RemoveEndpoint: %v", err)
	}
	if got := assign(t, s, "src:new", "scope", 0x0100); got == gone {
		t.Errorf("the number %d freed by src:gone was reissued to src:new", got)
	}
}

// TestRemoveEndpointIsIdempotent: the assembler's garbage collection does
// not coordinate with concurrent removals, so a missing row is a success.
func TestRemoveEndpointIsIdempotent(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	if err := s.RemoveEndpoint(t.Context(), endpoint.StringKey("never-existed")); err != nil {
		t.Errorf("removing an absent row returned %v, want nil", err)
	}
}

// TestListEndpointsFiltersByScopeAndOrders covers the read the garbage
// collector makes: one scope at a time, so a partition that has not loaded
// cannot delete another's rows.
func TestListEndpointsFiltersByScopeAndOrders(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	assign(t, s, "a:1", "alpha", 0x0100)
	assign(t, s, "b:1", "beta", 0x0100)
	assign(t, s, "a:2", "alpha", 0x0302)

	alpha, err := s.ListEndpoints(t.Context(), "alpha")
	if err != nil {
		t.Fatalf("ListEndpoints(alpha): %v", err)
	}
	if len(alpha) != 2 {
		t.Fatalf("ListEndpoints(alpha) returned %d rows, want 2: %+v", len(alpha), alpha)
	}
	if alpha[0].EndpointID > alpha[1].EndpointID {
		t.Errorf("rows are not ascending by endpoint id: %d then %d", alpha[0].EndpointID, alpha[1].EndpointID)
	}
	for _, rec := range alpha {
		if rec.Scope != "alpha" {
			t.Errorf("scope filter leaked a %q row into the alpha listing: %+v", rec.Scope, rec)
		}
	}

	all, err := s.ListEndpoints(t.Context(), "")
	if err != nil {
		t.Fatalf(`ListEndpoints(""): %v`, err)
	}
	if len(all) != 3 {
		t.Errorf(`ListEndpoints("") returned %d rows, want every scope's 3`, len(all))
	}
}

// TestConcurrentAssignmentsAreDistinct exercises the transaction around the
// counter. Two assemblies racing must not be handed one number; without the
// read-bump-insert being atomic they are.
func TestConcurrentAssignmentsAreDistinct(t *testing.T) {
	t.Parallel()
	s := newStore(t)

	const n = 16
	var (
		wg  sync.WaitGroup
		mu  sync.Mutex
		ids = make(map[uint16]string, n)
	)
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			key := fmt.Sprintf("src:%d", i)
			id, err := s.UpsertEndpointAssigning(context.Background(), endpoint.Record{
				Key: endpoint.StringKey(key), Scope: "scope", DeviceType: 0x0100,
			})
			if err != nil {
				t.Errorf("assign %s: %v", key, err)
				return
			}
			mu.Lock()
			defer mu.Unlock()
			if other, clash := ids[id]; clash {
				t.Errorf("endpoint %d handed to both %s and %s", id, other, key)
			}
			ids[id] = key
		}()
	}
	wg.Wait()
	if len(ids) != n {
		t.Errorf("%d concurrent assignments produced %d distinct numbers", n, len(ids))
	}
}

// TestStoredNumberWinsOverACallerSuppliedOne: the assembler re-states
// records it read back, and a stored number is the one a controller
// cached. Honouring the caller's value instead would silently renumber an
// endpoint.
func TestStoredNumberWinsOverACallerSuppliedOne(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	stored := assign(t, s, "src:1", "scope", 0x0100)

	got, err := s.UpsertEndpointAssigning(t.Context(), endpoint.Record{
		Key: endpoint.StringKey("src:1"), Scope: "scope", EndpointID: stored + 40, DeviceType: 0x0100,
	})
	if err != nil {
		t.Fatalf("upsert with a conflicting id: %v", err)
	}
	if got != stored {
		t.Errorf("upsert returned %d for a source already stored as %d", got, stored)
	}
}

// TestExplicitNumberRaisesTheAllocationCounter: a host restoring a number
// it allocated elsewhere must not leave the counter below it, or the next
// allocation collides with the row and the UNIQUE index fails the assembly.
func TestExplicitNumberRaisesTheAllocationCounter(t *testing.T) {
	t.Parallel()
	s := newStore(t)

	const placed uint16 = 40
	if _, err := s.UpsertEndpointAssigning(t.Context(), endpoint.Record{
		Key: endpoint.StringKey("src:placed"), Scope: "scope", EndpointID: placed, DeviceType: 0x0100,
	}); err != nil {
		t.Fatalf("place endpoint %d: %v", placed, err)
	}
	next := assign(t, s, "src:fresh", "scope", 0x0100)
	if next <= placed {
		t.Errorf("fresh allocation returned %d, at or below the hand-placed %d", next, placed)
	}
}

// hostKey is a composite [endpoint.SourceKey] of the shape the port
// describes: a host whose source identity has parts keeps its own type so
// it can recover them by type assertion rather than by parsing.
type hostKey struct {
	central string
	address string
}

func (k hostKey) String() string { return k.central + ":" + k.address }

// parseHostKey is the decoder such a host supplies. Only its author can
// write it: the module has no way to know where the separator is.
func parseHostKey(rendered string) endpoint.SourceKey {
	central, address, _ := strings.Cut(rendered, ":")
	return hostKey{central: central, address: address}
}

// assembleTwice runs two model-complete assemblies over one spec — a boot
// and the next reassembly — and returns the endpoint id each produced.
func assembleTwice(t *testing.T, s endpoint.Store, key endpoint.SourceKey) (boot, reassembly uint16) {
	t.Helper()
	asm, err := endpoint.New(s, endpointtest.AssemblerConfig(), nil)
	if err != nil {
		t.Fatalf("endpoint.New: %v", err)
	}
	snap := []endpoint.Snapshot{{
		Scope:         "site",
		ModelComplete: true,
		Endpoints: []endpoint.Spec{{
			StableKey: key, DeviceType: 0x0100, FriendlyName: "lamp",
		}},
	}}
	var ids [2]uint16
	for i := range ids {
		topo, err := asm.Assemble(t.Context(), snap)
		if err != nil {
			t.Fatalf("Assemble #%d: %v", i+1, err)
		}
		// Endpoints[0] and [1] are the root and the aggregator.
		if len(topo.Endpoints) != 3 {
			t.Fatalf("Assemble #%d produced %d endpoints, want root + aggregator + 1", i+1, len(topo.Endpoints))
		}
		ids[i] = topo.Endpoints[2].ID
	}
	return ids[0], ids[1]
}

// TestKeyDecoderKeepsGarbageCollectionFromEatingLiveRows drives the real
// assembler, because the defect it guards lives in the seam between the two:
// garbage collection compares the keys a store LISTS against the keys the
// snapshot carried, as interface values (endpoint/assembler.go, `seen[rec.Key]`).
//
// With the host's decoder the listed key equals the live one and the row
// survives, so the endpoint number is stable across reassemblies.
func TestKeyDecoderKeepsGarbageCollectionFromEatingLiveRows(t *testing.T) {
	t.Parallel()
	db := openAt(t, filepath.Join(t.TempDir(), "endpoints.db"))
	s := sqlitestore.New(db, sqlitestore.WithKeyDecoder(parseHostKey))

	first, second := assembleTwice(t, s, hostKey{central: "ccu1", address: "ABC:1"})
	if first != second {
		t.Errorf("endpoint id changed between two assemblies: %d then %d", first, second)
	}
	rows, err := s.ListEndpoints(t.Context(), "site")
	if err != nil {
		t.Fatalf("ListEndpoints: %v", err)
	}
	if len(rows) != 1 {
		t.Errorf("after two assemblies the store holds %d rows, want 1", len(rows))
	}
}

// TestWithoutAKeyDecoderACompositeKeyIsCollected is the negative control
// for the test above, and the reason [sqlitestore.WithKeyDecoder] exists at
// all: it records what the default StringKey decoding does to a host that
// does not use StringKey, so the option cannot be dismissed as ceremony.
//
// If this ever stops holding — because the assembler grew to compare
// renderings rather than interface values — the option has become optional
// in fact, and its documentation must be corrected rather than this test
// deleted.
func TestWithoutAKeyDecoderACompositeKeyIsCollected(t *testing.T) {
	t.Parallel()
	db := openAt(t, filepath.Join(t.TempDir(), "endpoints.db"))
	s := sqlitestore.New(db) // no decoder: rows come back as endpoint.StringKey

	first, second := assembleTwice(t, s, hostKey{central: "ccu1", address: "ABC:1"})
	if first == second {
		t.Fatalf("both assemblies returned endpoint %d — the mismatch this option guards against "+
			"no longer happens, so WithKeyDecoder's documentation is now wrong", first)
	}
	t.Logf("without a decoder the endpoint number moved from %d to %d on the second assembly", first, second)
}

// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

//nolint:thelper // each case is a test body that Run runs as a subtest, not a helper: its failures belong to its own lines
package storetest

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/SukramJ/go-fabric/store"
)

// The cases of a fabric row's commit state (store.FabricPending): an
// AddNOC's row and an UpdateNOC's credentials stay pending until
// CommitFabric, and RollbackFabric undoes them — a pending add with every
// row it owns, a pending update value for value.

// identityWithICAC is the identity installedFabric gives its fabric.
func identityWithICAC(f uint8) store.IdentityRecord {
	return store.IdentityRecord{
		FabricIndex: f,
		NOC:         []byte{0x15, 1, f},
		ICAC:        []byte{0x15, 2, f},
		PrivateKey:  bytes.Repeat([]byte{f}, 32),
		IPK:         bytes.Repeat([]byte{0x1F}, 16),
	}
}

// installedFabric adds fabric(n) in the given state with VID data and an
// identity with an ICAC, and returns its index.
func installedFabric(ctx context.Context, t *testing.T, p store.Port, n byte, state store.FabricPending) uint8 {
	t.Helper()
	rec := fabric(n)
	rec.Pending = state
	rec.VIDVerificationStatement = bytes.Repeat([]byte{n}, 85)
	rec.VVSC = []byte{0x15, n, 0x18}
	idx, err := p.AddFabric(ctx, rec)
	must(t, err)
	must(t, p.UpsertIdentity(ctx, identityWithICAC(idx)))
	return idx
}

// newCredentials differs from what installedFabric wrote in every field
// an UpdateNOC changes; it has no ICAC, so a rollback must bring back a
// value where the update left none.
func newCredentials(n byte) store.FabricCredentials {
	return store.FabricCredentials{
		NOC:        []byte{0x15, 0xA0, n},
		PrivateKey: bytes.Repeat([]byte{0xA0 + n}, 32),
		NodeID:     0xBEEF00 + uint64(n),
	}
}

// fabricState is the fabric's row and identity, as a reader sees them.
type fabricState struct {
	fabric   store.FabricRecord
	identity store.IdentityRecord
}

func stateOf(ctx context.Context, t *testing.T, p store.Port, f uint8) fabricState {
	t.Helper()
	rec, err := p.GetFabric(ctx, f)
	must(t, err)
	id, err := p.GetIdentity(ctx, f)
	must(t, err)
	return fabricState{rec, id}
}

func wantState(t *testing.T, what string, p, want store.FabricPending, err error) {
	t.Helper()
	if err != nil || p != want {
		t.Fatalf("%s = %v, %v; want %v, nil", what, p, err, want)
	}
}

// applied is what a reader sees after UpdateFabricCredentials(c) on before:
// the new credentials and node id; the IPK, label, root, vendor id and VID
// verification data kept.
func applied(before fabricState, c store.FabricCredentials, state store.FabricPending) fabricState {
	after := before
	after.fabric.Pending = state
	after.fabric.NodeID = c.NodeID
	after.identity.NOC, after.identity.ICAC, after.identity.PrivateKey = c.NOC, c.ICAC, c.PrivateKey
	return after
}

// fabricsCommit: CommitFabric commits a pending add; on a committed row
// it, and RollbackFabric, change nothing.
func fabricsCommit(ctx context.Context, t *testing.T, p store.Port) {
	idx := installedFabric(ctx, t, p, 1, store.FabricPendingAdd)
	before := stateOf(ctx, t, p, idx)
	must(t, p.CommitFabric(ctx, idx))
	want := before
	want.fabric.Pending = store.FabricCommitted
	wantEqual(t, "the committed add", stateOf(ctx, t, p, idx), want)
	must(t, p.CommitFabric(ctx, idx))
	state, err := p.RollbackFabric(ctx, idx)
	wantState(t, "RollbackFabric of a committed row", state, store.FabricCommitted, err)
	wantEqual(t, "a committed row after CommitFabric and RollbackFabric", stateOf(ctx, t, p, idx), want)
}

// fabricsRollbackPendingAdd: the rollback of a pending add deletes the row
// and every row it owns in each cascading table; another fabric keeps its.
func fabricsRollbackPendingAdd(ctx context.Context, t *testing.T, p store.Port) {
	committed := addFabric(ctx, t, p, 1)
	rec := fabric(2)
	rec.Pending = store.FabricPendingAdd
	pending, err := p.AddFabric(ctx, rec)
	must(t, err)
	for _, f := range []uint8{committed, pending} {
		seedFabricRows(ctx, t, p, f)
	}
	state, err := p.RollbackFabric(ctx, pending)
	wantState(t, "RollbackFabric of a pending add", state, store.FabricPendingAdd, err)
	if _, err := p.GetFabric(ctx, pending); !errors.Is(err, store.ErrFabricNotFound) {
		t.Fatalf("GetFabric of the rolled-back add = %v, want ErrFabricNotFound", err)
	}
	wantEqual(t, "the rolled-back add's rows", fabricRows(ctx, p, pending), noFabricRows)
	wantEqual(t, "the committed fabric's rows", fabricRows(ctx, p, committed), allFabricRows)
	if list, _ := p.ListFabrics(ctx); len(list) != 1 || list[0].FabricIndex != committed {
		t.Fatalf("ListFabrics after the rollback = %+v", list)
	}
}

// fabricsUpdateRollback: an update, and the vendor id, node id and VID
// writes inside the same fail-safe, are rolled back byte for byte; the row
// is committed again and a second rollback changes nothing. Both
// directions of "none": a fabric with an ICAC and VID data updated to
// none, and back.
func fabricsUpdateRollback(ctx context.Context, t *testing.T, p store.Port) {
	idx := installedFabric(ctx, t, p, 1, store.FabricCommitted)
	before := stateOf(ctx, t, p, idx)
	c := newCredentials(1)
	must(t, p.UpdateFabricCredentials(ctx, idx, c))
	wantEqual(t, "the fabric after the update", stateOf(ctx, t, p, idx), applied(before, c, store.FabricPendingUpdate))

	must(t, p.UpdateFabricVendorID(ctx, idx, 0x4321))
	must(t, p.UpdateFabricNodeID(ctx, idx, 0x99))
	must(t, p.UpdateFabricVIDVerification(ctx, idx, []byte{9}, []byte{9}))
	if got, _ := p.GetFabric(ctx, idx); got.Pending != store.FabricPendingUpdate || got.VendorID != 0x4321 {
		t.Fatalf("the live writes during the update: %+v", got)
	}

	state, err := p.RollbackFabric(ctx, idx)
	wantState(t, "RollbackFabric of a pending update", state, store.FabricPendingUpdate, err)
	wantEqual(t, "the fabric after the rollback", stateOf(ctx, t, p, idx), before)
	state, err = p.RollbackFabric(ctx, idx)
	wantState(t, "a second RollbackFabric", state, store.FabricCommitted, err)
	wantEqual(t, "the fabric after a second rollback", stateOf(ctx, t, p, idx), before)

	// The other direction: from none (no ICAC, no VID data) to some.
	rec := fabric(2)
	plain, err := p.AddFabric(ctx, rec)
	must(t, err)
	must(t, p.UpsertIdentity(ctx, store.IdentityRecord{FabricIndex: plain, NOC: []byte{1}, PrivateKey: []byte{2}, IPK: []byte{3}}))
	before = stateOf(ctx, t, p, plain)
	c = store.FabricCredentials{NOC: []byte{4}, ICAC: []byte{5}, PrivateKey: []byte{6}, NodeID: 7}
	must(t, p.UpdateFabricCredentials(ctx, plain, c))
	wantEqual(t, "the plain fabric after the update", stateOf(ctx, t, p, plain), applied(before, c, store.FabricPendingUpdate))
	must(t, p.UpdateFabricVendorID(ctx, plain, 8))
	must(t, p.UpdateFabricVIDVerification(ctx, plain, []byte{9}, []byte{10}))
	state, err = p.RollbackFabric(ctx, plain)
	wantState(t, "RollbackFabric of the plain fabric's update", state, store.FabricPendingUpdate, err)
	wantEqual(t, "the plain fabric after the rollback", stateOf(ctx, t, p, plain), before)
}

// fabricsUpdateCommit: a committed update keeps the new values and erases
// the prior ones, so a later rollback changes nothing.
func fabricsUpdateCommit(ctx context.Context, t *testing.T, p store.Port) {
	idx := installedFabric(ctx, t, p, 1, store.FabricCommitted)
	before := stateOf(ctx, t, p, idx)
	c := newCredentials(1)
	must(t, p.UpdateFabricCredentials(ctx, idx, c))
	must(t, p.CommitFabric(ctx, idx))
	want := applied(before, c, store.FabricCommitted)
	wantEqual(t, "the fabric after the commit", stateOf(ctx, t, p, idx), want)
	state, err := p.RollbackFabric(ctx, idx)
	wantState(t, "RollbackFabric after the commit", state, store.FabricCommitted, err)
	wantEqual(t, "the fabric after a rollback of the committed update", stateOf(ctx, t, p, idx), want)

	// A new update after the commit keeps the committed values as its prior.
	must(t, p.UpdateFabricCredentials(ctx, idx, newCredentials(2)))
	state, err = p.RollbackFabric(ctx, idx)
	wantState(t, "RollbackFabric of the next update", state, store.FabricPendingUpdate, err)
	wantEqual(t, "the fabric after the next update's rollback", stateOf(ctx, t, p, idx), want)
}

// fabricsUpdateWritesWhatItChanges: an update writes the identity and the
// node id only. The vendor id and VID verification data stay as the row
// holds them when the update lands, while their prior values are kept, so
// a rollback still drops what the fail-safe wrote.
func fabricsUpdateWritesWhatItChanges(ctx context.Context, t *testing.T, p store.Port) {
	idx := installedFabric(ctx, t, p, 1, store.FabricCommitted)
	before := stateOf(ctx, t, p, idx)
	c := newCredentials(1)
	must(t, p.UpdateFabricCredentials(ctx, idx, c))
	want := applied(before, c, store.FabricPendingUpdate)
	wantEqual(t, "the fabric after the update", stateOf(ctx, t, p, idx), want)
	must(t, p.CommitFabric(ctx, idx))
	want.fabric.Pending = store.FabricCommitted
	wantEqual(t, "the fabric after its commit", stateOf(ctx, t, p, idx), want)

	// A vendor id and VID data written inside the update's fail-safe stay
	// with its commit and leave with its rollback.
	must(t, p.UpdateFabricCredentials(ctx, idx, newCredentials(2)))
	must(t, p.UpdateFabricVendorID(ctx, idx, 0x1357))
	must(t, p.UpdateFabricVIDVerification(ctx, idx, []byte{0xAB}, nil))
	state, err := p.RollbackFabric(ctx, idx)
	wantState(t, "RollbackFabric of the second update", state, store.FabricPendingUpdate, err)
	wantEqual(t, "the fabric after the second update's rollback", stateOf(ctx, t, p, idx), want)
}

// fabricsUpdateTwice: a second update before the commit keeps the first
// update's prior values; the rollback returns to the state before both.
func fabricsUpdateTwice(ctx context.Context, t *testing.T, p store.Port) {
	idx := installedFabric(ctx, t, p, 1, store.FabricCommitted)
	before := stateOf(ctx, t, p, idx)
	must(t, p.UpdateFabricCredentials(ctx, idx, newCredentials(1)))
	second := newCredentials(2)
	second.ICAC = []byte{0x15, 0xC0}
	must(t, p.UpdateFabricCredentials(ctx, idx, second))
	wantEqual(t, "the fabric after the second update", stateOf(ctx, t, p, idx), applied(before, second, store.FabricPendingUpdate))
	state, err := p.RollbackFabric(ctx, idx)
	wantState(t, "RollbackFabric after two updates", state, store.FabricPendingUpdate, err)
	wantEqual(t, "the fabric after the rollback of two updates", stateOf(ctx, t, p, idx), before)
}

// fabricsUpdatePendingAdd: an update of a pending add writes and stays a
// pending add; its rollback deletes the row.
func fabricsUpdatePendingAdd(ctx context.Context, t *testing.T, p store.Port) {
	idx := installedFabric(ctx, t, p, 1, store.FabricPendingAdd)
	before := stateOf(ctx, t, p, idx)
	c := newCredentials(1)
	must(t, p.UpdateFabricCredentials(ctx, idx, c))
	wantEqual(t, "the pending add after the update", stateOf(ctx, t, p, idx), applied(before, c, store.FabricPendingAdd))
	state, err := p.RollbackFabric(ctx, idx)
	wantState(t, "RollbackFabric of the updated pending add", state, store.FabricPendingAdd, err)
	if _, err := p.GetFabric(ctx, idx); !errors.Is(err, store.ErrFabricNotFound) {
		t.Fatalf("GetFabric after the rollback = %v, want ErrFabricNotFound", err)
	}
	if _, err := p.GetIdentity(ctx, idx); !errors.Is(err, store.ErrIdentityNotFound) {
		t.Fatalf("GetIdentity after the rollback = %v, want ErrIdentityNotFound", err)
	}
}

// fabricsUpdateRefused: an update without an identity, or with an empty
// NOC or private key, is refused and changes nothing — the row stays
// committed, so no rollback can follow it.
func fabricsUpdateRefused(ctx context.Context, t *testing.T, p store.Port) {
	bare := addFabric(ctx, t, p, 1)
	if err := p.UpdateFabricCredentials(ctx, bare, newCredentials(1)); !errors.Is(err, store.ErrIdentityNotFound) {
		t.Fatalf("UpdateFabricCredentials without an identity = %v, want ErrIdentityNotFound", err)
	}
	want := fabric(1)
	want.FabricIndex = bare
	if got, _ := p.GetFabric(ctx, bare); !equalFabric(got, want) {
		t.Fatalf("a refused update changed the fabric: %+v", got)
	}

	idx := installedFabric(ctx, t, p, 2, store.FabricCommitted)
	before := stateOf(ctx, t, p, idx)
	noNOC, noKey := newCredentials(2), newCredentials(2)
	noNOC.NOC, noKey.PrivateKey = nil, []byte{}
	for name, c := range map[string]*store.FabricCredentials{"no NOC": &noNOC, "no private key": &noKey} {
		if err := p.UpdateFabricCredentials(ctx, idx, *c); err == nil {
			t.Errorf("UpdateFabricCredentials with %s succeeded", name)
		}
	}
	wantEqual(t, "the fabric after the refused updates", stateOf(ctx, t, p, idx), before)
}

func equalFabric(a, b store.FabricRecord) bool {
	return a.FabricIndex == b.FabricIndex && a.NodeID == b.NodeID && a.VendorID == b.VendorID && a.Pending == b.Pending &&
		bytes.Equal(a.VIDVerificationStatement, b.VIDVerificationStatement) && bytes.Equal(a.VVSC, b.VVSC)
}

// fabricsPendingNotFound: each commit-state method on an unknown index.
func fabricsPendingNotFound(ctx context.Context, t *testing.T, p store.Port) {
	addFabric(ctx, t, p, 1)
	const missing = 7
	if err := p.UpdateFabricCredentials(ctx, missing, newCredentials(1)); !errors.Is(err, store.ErrFabricNotFound) {
		t.Errorf("UpdateFabricCredentials on a missing fabric = %v, want ErrFabricNotFound", err)
	}
	if err := p.CommitFabric(ctx, missing); !errors.Is(err, store.ErrFabricNotFound) {
		t.Errorf("CommitFabric on a missing fabric = %v, want ErrFabricNotFound", err)
	}
	if _, err := p.RollbackFabric(ctx, missing); !errors.Is(err, store.ErrFabricNotFound) {
		t.Errorf("RollbackFabric on a missing fabric = %v, want ErrFabricNotFound", err)
	}
}

// copiesCredentials: the slices of FabricCredentials are copied on the way
// in, and what an update's rollback brings back is not the caller's either.
func copiesCredentials(ctx context.Context, t *testing.T, p store.Port) {
	idx := installedFabric(ctx, t, p, 1, store.FabricCommitted)
	before := stateOf(ctx, t, p, idx)
	c := store.FabricCredentials{NOC: []byte{1}, ICAC: []byte{2}, PrivateKey: []byte{3}}
	must(t, p.UpdateFabricCredentials(ctx, idx, c))
	c.NOC[0], c.ICAC[0], c.PrivateKey[0] = 0xEE, 0xEE, 0xEE
	got := stateOf(ctx, t, p, idx)
	if !bytes.Equal(got.identity.NOC, []byte{1}) || !bytes.Equal(got.identity.ICAC, []byte{2}) || !bytes.Equal(got.identity.PrivateKey, []byte{3}) {
		t.Fatal("mutating the written FabricCredentials changed the store")
	}
	_, err := p.RollbackFabric(ctx, idx)
	must(t, err)
	restored := stateOf(ctx, t, p, idx)
	restored.identity.NOC[0], restored.identity.PrivateKey[0], restored.fabric.VVSC[0] = 0xDD, 0xDD, 0xDD
	wantEqual(t, "the fabric after mutating what its rollback returned", stateOf(ctx, t, p, idx), before)
}

// fabricsConcurrentUpdates: UpdateFabricCredentials calls racing on one
// committed fabric all succeed, and exactly one of them keeps the prior
// values — the fabric as it was before all of them — so the rollback
// restores that state, not one racer's new values.
func fabricsConcurrentUpdates(ctx context.Context, t *testing.T, p store.Port) {
	idx := installedFabric(ctx, t, p, 1, store.FabricCommitted)
	before := stateOf(ctx, t, p, idx)
	racers := []byte{2, 3, 4, 5, 6, 7, 8, 9}
	errs := make(chan error, len(racers))
	var wg sync.WaitGroup
	for _, n := range racers {
		wg.Go(func() { errs <- p.UpdateFabricCredentials(ctx, idx, newCredentials(n)) })
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		must(t, err)
	}
	state, err := p.RollbackFabric(ctx, idx)
	wantState(t, "RollbackFabric after the racing updates", state, store.FabricPendingUpdate, err)
	wantEqual(t, "the fabric after the racing updates' rollback", stateOf(ctx, t, p, idx), before)
}

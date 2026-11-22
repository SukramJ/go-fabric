// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

//nolint:thelper // each case is a test body that Run runs as a subtest, not a helper: its failures belong to its own lines
package storetest

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"thingfabric.dev/go-fabric/secure/sessionparams"
	"thingfabric.dev/go-fabric/store"
)

// The cases of the port's review II growth (decisions 107 and 109,
// Q-port-6): the settings deletes, the atomic key-map replace, the one
// vendor-verification write, the owed fabric events and the resumption
// record's session parameters.

// settingsDeleteCase: DeleteSettings removes the keys it names and no
// other, a key the store does not hold is no error, no key is a no-op,
// and the counters stay.
func settingsDeleteCase(ctx context.Context, t *testing.T, p store.Port) {
	must(t, p.DeleteSettings(ctx))
	must(t, p.SetSettings(ctx, map[string]string{"del.a": "1", "del.b": "", "del.c": "3"}))
	must(t, p.SetMetadataCounter(ctx, "del.a", 7))
	must(t, p.DeleteSettings(ctx, "del.a", "del.b", "del.missing"))
	for k, want := range map[string]bool{"del.a": false, "del.b": false, "del.c": true} {
		if _, ok, err := p.GetSetting(ctx, k); err != nil || ok != want {
			t.Errorf("GetSetting(%q) after DeleteSettings: ok=%v err=%v; want ok=%v", k, ok, err, want)
		}
	}
	if v, ok, err := p.GetMetadataCounter(ctx, "del.a"); err != nil || !ok || v != 7 {
		t.Errorf("DeleteSettings touched the counter of the same key: %d, %v, %v", v, ok, err)
	}
}

// settingsDeletePrefixCase: DeleteSettingsWithPrefix removes every key
// under a root and nothing beside it — not the root's sibling that
// extends its name ("scenes.ab." does not take "scenes.abc.x"), not the
// key that is the root without its dot, not a key that sorts right after
// the root — and refuses a prefix that is no root, the empty one
// included, with nothing removed.
func settingsDeletePrefixCase(ctx context.Context, t *testing.T, p store.Port) {
	keys := map[string]bool{
		"scenes.ab.":     false,
		"scenes.ab.x":    false,
		"scenes.ab.x.y":  false,
		"scenes.ab":      true,
		"scenes.abc.x":   true,
		"scenes.ab/":     true,
		"scenes.ab0":     true,
		"scenes.a":       true,
		"other.scenes.a": true,
	}
	values := map[string]string{}
	for k := range keys {
		values[k] = "v"
	}
	must(t, p.SetSettings(ctx, values))
	for _, bad := range []string{"", "scenes.ab", "scenes"} {
		if err := p.DeleteSettingsWithPrefix(ctx, bad); err == nil {
			t.Errorf("DeleteSettingsWithPrefix(%q) succeeded; want a refusal", bad)
		}
	}
	for k := range keys {
		if _, ok, err := p.GetSetting(ctx, k); err != nil || !ok {
			t.Fatalf("a refused prefix delete removed %q (err %v)", k, err)
		}
	}
	must(t, p.DeleteSettingsWithPrefix(ctx, "scenes.ab."))
	must(t, p.DeleteSettingsWithPrefix(ctx, "nothing.here."))
	for k, want := range keys {
		if _, ok, err := p.GetSetting(ctx, k); err != nil || ok != want {
			t.Errorf("GetSetting(%q) after DeleteSettingsWithPrefix(scenes.ab.): ok=%v err=%v; want ok=%v", k, ok, err, want)
		}
	}
}

// groupKeysReplaceMappingsCase: ReplaceGroupKeyMappings swaps the whole
// map — groups it no longer names go, the others take their new set — and
// reads back by GroupID ascending whatever the input order; the mappings'
// own FabricIndex does not matter; an empty map clears it; a duplicate
// GroupID and an unknown fabric are refused and change nothing; another
// fabric's map stays.
func groupKeysReplaceMappingsCase(ctx context.Context, t *testing.T, p store.Port) {
	f := addFabric(ctx, t, p, 1)
	other := addFabric(ctx, t, p, 2)
	must(t, p.SetGroupKeyMapping(ctx, store.GroupKeyMapping{FabricIndex: f, GroupID: 9, GroupKeySetID: 1}))
	must(t, p.SetGroupKeyMapping(ctx, store.GroupKeyMapping{FabricIndex: f, GroupID: 3, GroupKeySetID: 1}))
	must(t, p.SetGroupKeyMapping(ctx, store.GroupKeyMapping{FabricIndex: other, GroupID: 3, GroupKeySetID: 4}))

	must(t, p.ReplaceGroupKeyMappings(ctx, f, []store.GroupKeyMapping{
		{FabricIndex: 99, GroupID: 7, GroupKeySetID: 2},
		{GroupID: 3, GroupKeySetID: 5},
		{GroupID: 1, GroupKeySetID: 2},
	}))
	want := []store.GroupKeyMapping{
		{FabricIndex: f, GroupID: 7, GroupKeySetID: 2},
		{FabricIndex: f, GroupID: 3, GroupKeySetID: 5},
		{FabricIndex: f, GroupID: 1, GroupKeySetID: 2},
	}
	got, err := p.ListGroupKeyMappings(ctx, f)
	must(t, err)
	wantEqual(t, "the replaced key map", got, want)

	if err := p.ReplaceGroupKeyMappings(ctx, f, []store.GroupKeyMapping{{GroupID: 4, GroupKeySetID: 1}, {GroupID: 4, GroupKeySetID: 2}}); err == nil {
		t.Fatal("ReplaceGroupKeyMappings with GroupID 4 twice succeeded")
	}
	got, err = p.ListGroupKeyMappings(ctx, f)
	must(t, err)
	wantEqual(t, "the key map after a refused replace", got, want)
	if err := p.ReplaceGroupKeyMappings(ctx, 200, []store.GroupKeyMapping{{GroupID: 1, GroupKeySetID: 1}}); err == nil {
		t.Fatal("ReplaceGroupKeyMappings for an unknown fabric succeeded")
	}

	must(t, p.ReplaceGroupKeyMappings(ctx, f, nil))
	if got, err := p.ListGroupKeyMappings(ctx, f); err != nil || len(got) != 0 {
		t.Fatalf("the key map after an empty replace = %+v, %v; want none", got, err)
	}
	got, err = p.ListGroupKeyMappings(ctx, other)
	must(t, err)
	wantEqual(t, "the other fabric's key map", got, []store.GroupKeyMapping{{FabricIndex: other, GroupID: 3, GroupKeySetID: 4}})
}

// fabricsVendorVerificationCase: UpdateFabricVendorVerification writes
// the vendor id and the VID data together, each part only when the update
// names it; an unknown fabric changes nothing; on a pending update both
// are live values its rollback restores together.
func fabricsVendorVerificationCase(ctx context.Context, t *testing.T, p store.Port) {
	idx := installedFabric(ctx, t, p, 1, store.FabricCommitted)
	before, err := p.GetFabric(ctx, idx)
	must(t, err)
	vendor := uint16(0x1234)
	statement, vvsc := bytes.Repeat([]byte{9}, 85), []byte{4, 5}

	if err := p.UpdateFabricVendorVerification(ctx, 200, store.FabricVendorUpdate{VendorID: &vendor}); !errors.Is(err, store.ErrFabricNotFound) {
		t.Fatalf("UpdateFabricVendorVerification of an unknown fabric = %v, want ErrFabricNotFound", err)
	}
	must(t, p.UpdateFabricVendorVerification(ctx, idx, store.FabricVendorUpdate{}))
	got, err := p.GetFabric(ctx, idx)
	must(t, err)
	wantEqual(t, "the row after an update naming nothing", got, before)

	must(t, p.UpdateFabricVendorVerification(ctx, idx, store.FabricVendorUpdate{VendorID: &vendor}))
	got, err = p.GetFabric(ctx, idx)
	must(t, err)
	if got.VendorID != vendor || !bytes.Equal(got.VIDVerificationStatement, before.VIDVerificationStatement) || !bytes.Equal(got.VVSC, before.VVSC) {
		t.Fatalf("a vendor-only update gave %+v; want the vendor id moved and the VID data kept", got)
	}
	must(t, p.UpdateFabricVendorVerification(ctx, idx, store.FabricVendorUpdate{SetVIDData: true, Statement: []byte{}}))
	got, err = p.GetFabric(ctx, idx)
	must(t, err)
	if got.VendorID != vendor || got.VIDVerificationStatement != nil || got.VVSC != nil {
		t.Fatalf("a data-only clearing update gave %+v; want the vendor id kept and the VID data none", got)
	}

	// On a pending update: both written in one call, both dropped by the
	// rollback.
	pendingBefore, err := p.GetFabric(ctx, idx)
	must(t, err)
	must(t, p.UpdateFabricCredentials(ctx, idx, newCredentials(1)))
	other := uint16(0x4321)
	must(t, p.UpdateFabricVendorVerification(ctx, idx, store.FabricVendorUpdate{VendorID: &other, SetVIDData: true, Statement: statement, VVSC: vvsc}))
	got, err = p.GetFabric(ctx, idx)
	must(t, err)
	if got.VendorID != other || !bytes.Equal(got.VIDVerificationStatement, statement) || !bytes.Equal(got.VVSC, vvsc) || got.Pending != store.FabricPendingUpdate {
		t.Fatalf("the pending row after the vendor-verification write = %+v", got)
	}
	was, err := p.RollbackFabric(ctx, idx)
	wantState(t, "RollbackFabric", was, store.FabricPendingUpdate, err)
	got, err = p.GetFabric(ctx, idx)
	must(t, err)
	if got.VendorID != pendingBefore.VendorID || got.VIDVerificationStatement != nil || got.VVSC != nil {
		t.Fatalf("the rolled-back row = %+v; want vendor id %#x and no VID data back", got, pendingBefore.VendorID)
	}
}

// fabricsRollbackOwesEventCase: a rollback that undoes a row records the
// hook it owes, in the same write — removed for a pending add, updated for
// a pending update, with the row as it stood before — and a pending
// update's rollback removes the fabric's resumption records with it. A
// rollback that undoes nothing owes nothing. The events read back in the
// order they were recorded, stay until acknowledged, and an
// acknowledgement removes exactly its event; a second one is no error.
// The events are no fabric's rows: removing the fabric keeps them.
func fabricsRollbackOwesEventCase(ctx context.Context, t *testing.T, p store.Port) {
	if owed, err := p.OwedFabricEvents(ctx); err != nil || len(owed) != 0 {
		t.Fatalf("OwedFabricEvents on an empty store = %+v, %v", owed, err)
	}
	committed := installedFabric(ctx, t, p, 1, store.FabricCommitted)
	updated := installedFabric(ctx, t, p, 2, store.FabricCommitted)
	added := installedFabric(ctx, t, p, 3, store.FabricPendingAdd)
	must(t, p.UpsertResumption(ctx, resumptionRec(updated, 5, 1)))
	must(t, p.UpsertResumption(ctx, resumptionRec(committed, 5, 3)))
	must(t, p.UpdateFabricCredentials(ctx, updated, newCredentials(2)))
	updatedRow, err := p.GetFabric(ctx, updated)
	must(t, err)
	addedRow, err := p.GetFabric(ctx, added)
	must(t, err)

	was, err := p.RollbackFabric(ctx, committed)
	wantState(t, "RollbackFabric of a committed row", was, store.FabricCommitted, err)
	was, err = p.RollbackFabric(ctx, added)
	wantState(t, "RollbackFabric of the pending add", was, store.FabricPendingAdd, err)
	was, err = p.RollbackFabric(ctx, updated)
	wantState(t, "RollbackFabric of the pending update", was, store.FabricPendingUpdate, err)

	if _, err := p.GetResumptionByPeer(ctx, updated, 5); !errors.Is(err, store.ErrResumptionNotFound) {
		t.Fatalf("the rolled-back update's resumption record = %v, want ErrResumptionNotFound", err)
	}
	if _, err := p.GetResumptionByPeer(ctx, committed, 5); err != nil {
		t.Fatalf("the committed fabric's resumption record = %v, want it kept", err)
	}

	owed, err := p.OwedFabricEvents(ctx)
	must(t, err)
	if len(owed) != 2 {
		t.Fatalf("OwedFabricEvents = %+v; want the add's and the update's", owed)
	}
	if owed[0].ID == 0 || owed[1].ID <= owed[0].ID {
		t.Fatalf("event IDs %d, %d; want non-zero and increasing", owed[0].ID, owed[1].ID)
	}
	wantEqual(t, "the add's event kind", owed[0].Kind, store.FabricPendingAdd)
	wantEqual(t, "the add's event fabric", owed[0].Fabric, owedPart(addedRow))
	wantEqual(t, "the update's event kind", owed[1].Kind, store.FabricPendingUpdate)
	wantEqual(t, "the update's event fabric", owed[1].Fabric, owedPart(updatedRow))

	owed[0].Fabric.RootPublicKey[1] ^= 0xFF
	again, err := p.OwedFabricEvents(ctx)
	must(t, err)
	wantEqual(t, "the events after mutating a read one", again[0].Fabric, owedPart(addedRow))

	must(t, p.RemoveFabric(ctx, updated))
	must(t, p.AckOwedFabricEvent(ctx, again[0].ID))
	must(t, p.AckOwedFabricEvent(ctx, again[0].ID))
	left, err := p.OwedFabricEvents(ctx)
	must(t, err)
	if len(left) != 1 || left[0].ID != again[1].ID {
		t.Fatalf("OwedFabricEvents after one acknowledgement = %+v; want only event %d", left, again[1].ID)
	}
	must(t, p.AckOwedFabricEvent(ctx, left[0].ID))
	if left, err := p.OwedFabricEvents(ctx); err != nil || len(left) != 0 {
		t.Fatalf("OwedFabricEvents after both acknowledgements = %+v, %v", left, err)
	}
	// A later event never takes an acknowledged one's ID.
	again2 := installedFabric(ctx, t, p, 4, store.FabricPendingAdd)
	_, err = p.RollbackFabric(ctx, again2)
	must(t, err)
	last, err := p.OwedFabricEvents(ctx)
	must(t, err)
	if len(last) != 1 || last[0].ID <= again[1].ID {
		t.Fatalf("a later event = %+v; want an ID above %d", last, again[1].ID)
	}
}

// owedPart is the part of a fabric row an owed event keeps.
func owedPart(f store.FabricRecord) store.FabricRecord {
	return store.FabricRecord{
		FabricIndex: f.FabricIndex, FabricID: f.FabricID, NodeID: f.NodeID,
		RootPublicKey: f.RootPublicKey, CompressedID: f.CompressedID,
		VendorID: f.VendorID, Label: f.Label,
	}
}

// resumptionSessionParametersCase: every session-parameter field round
// trips, through both lookups, and a later upsert without them stores
// none (zero is absent).
func resumptionSessionParametersCase(ctx context.Context, t *testing.T, p store.Port) {
	f := addFabric(ctx, t, p, 1)
	rec := resumptionRec(f, 5, 1)
	rec.CASEAuthTags = []uint32{0x00010001}
	rec.SessionParameters = sessionparams.Parameters{
		SessionIdleInterval:      0xFFFFFFFF,
		SessionActiveInterval:    300,
		SessionActiveThreshold:   0xFFFF,
		DataModelRevision:        19,
		InteractionModelRevision: 13,
		SpecificationVersion:     0x01060100,
		MaxPathsPerInvoke:        10,
		SupportedTransports:      0x0003,
		MaxTCPMessageSize:        64000,
	}
	must(t, p.UpsertResumption(ctx, rec))
	got, err := p.GetResumptionByPeer(ctx, f, 5)
	must(t, err)
	wantEqual(t, "GetResumptionByPeer", got, rec)
	got, err = p.GetResumptionByID(ctx, rec.ResumptionID)
	must(t, err)
	wantEqual(t, "GetResumptionByID", got, rec)

	plain := resumptionRec(f, 5, 3)
	must(t, p.UpsertResumption(ctx, plain))
	got, err = p.GetResumptionByPeer(ctx, f, 5)
	must(t, err)
	wantEqual(t, "the session parameters after an upsert without them", got.SessionParameters, sessionparams.Parameters{})
}

// eraseOwedEventIDsCase: an owed fabric event's ID is never assigned again,
// an Erase between two events included — the event recorded after the
// Erase has an ID above the one recorded before it.
func eraseOwedEventIDsCase(ctx context.Context, t *testing.T, p store.Port) {
	idx := installedFabric(ctx, t, p, 1, store.FabricPendingAdd)
	was, err := p.RollbackFabric(ctx, idx)
	wantState(t, "RollbackFabric of the pending add", was, store.FabricPendingAdd, err)
	before, err := p.OwedFabricEvents(ctx)
	must(t, err)
	if len(before) != 1 {
		t.Fatalf("OwedFabricEvents before the Erase = %+v; want the rollback's", before)
	}
	must(t, p.Erase(ctx))
	idx = installedFabric(ctx, t, p, 1, store.FabricPendingAdd)
	was, err = p.RollbackFabric(ctx, idx)
	wantState(t, "RollbackFabric of the pending add after the Erase", was, store.FabricPendingAdd, err)
	after, err := p.OwedFabricEvents(ctx)
	must(t, err)
	if len(after) != 1 {
		t.Fatalf("OwedFabricEvents after the Erase = %+v; want the second rollback's alone", after)
	}
	if after[0].ID <= before[0].ID {
		t.Errorf("the event after the Erase has ID %d, the one before it %d; want an ID never assigned before", after[0].ID, before[0].ID)
	}
}

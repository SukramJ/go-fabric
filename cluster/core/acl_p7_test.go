// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core_test

import (
	"context"
	"encoding/hex"
	"errors"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/im"
	mstore "github.com/SukramJ/go-fabric/store"
)

// A change of fabric 2 signalled under fabric 1's request context while a
// batch is open (GroupKeyManagement reloads every stale fabric with the
// context of the command that runs it): fabric 2's AuxiliaryAccessUpdated
// names no node, never fabric 1's administrator (review II, R2-P7-acl-01).
func TestAuxiliaryEventOfAnotherFabricsChangeNamesNoForeignNode(t *testing.T) {
	t.Parallel()
	ac, err := core.NewAccessControl(newFakeStore())
	if err != nil {
		t.Fatal(err)
	}
	ev := &deferAuxEvents{got: map[uint8]*uint64{}}
	ac.SetMatterEventEmitter(ev)
	src := &deferAuxSource{}
	ac.RegisterAuxiliaryProvider(context.Background(), src)
	const adminA = 0xA1
	ctxA := im.WithSubject(im.WithFabricFilter(context.Background(), false, 1), adminA, nil)
	ac.BeginAuxiliaryBatch()
	src.add(ctxA, 2, true)
	ac.EndAuxiliaryBatch(ctxA)
	ev.mu.Lock()
	defer ev.mu.Unlock()
	a, fired := ev.got[2]
	if !fired {
		t.Fatal("fabric 2's change fired no AuxiliaryAccessUpdated")
	}
	if a != nil {
		t.Errorf("fabric 2's event names node %#x of fabric 1's request", *a)
	}
}

// p7ExtStore is a settings ACL store whose settings read and fabric listing
// a test can fail.
type p7ExtStore struct {
	settingsACLStore
	getErr  error
	setErr  error
	listErr error
	fabrics []mstore.FabricRecord
}

func (s *p7ExtStore) SetSetting(ctx context.Context, key, value string) error {
	if s.setErr != nil {
		return s.setErr
	}
	return s.settingsACLStore.SetSetting(ctx, key, value)
}

func (s *p7ExtStore) GetSetting(ctx context.Context, key string) (value string, ok bool, err error) {
	if s.getErr != nil {
		return "", false, s.getErr
	}
	return s.settingsACLStore.GetSetting(ctx, key)
}

func (s *p7ExtStore) ListFabrics(context.Context) ([]mstore.FabricRecord, error) {
	return s.fabrics, s.listErr
}

// The stored Extension of a fabric whose setting could not be read at
// construction is cleared by the fabric's drop, so a later construction
// does not load it again (review II, R2-P7-acl-03).
func TestDropFabricClearsAnExtensionThatFailedToLoad(t *testing.T) {
	t.Parallel()
	key := "access_control.extension.2"
	st := &p7ExtStore{settingsACLStore: settingsACLStore{settings: map[string]string{key: hex.EncodeToString(encodeExtensionTLV(t))}}}
	st.getErr = errors.New("busy")
	ac, err := core.NewAccessControl(st)
	if err != nil {
		t.Fatal(err)
	}
	st.getErr = nil
	before := ac.MatterDataVersion()
	if err := ac.MatterDropFabric(context.Background(), 2); err != nil {
		t.Fatal(err)
	}
	if v := st.settings[key]; v != "" {
		t.Errorf("after MatterDropFabric(2) the setting still holds %q", v)
	}
	if after := ac.MatterDataVersion(); after != before {
		t.Errorf("DataVersion moved (%d -> %d) dropping a fabric with no Extension in memory", before, after)
	}
	again, err := core.NewAccessControl(st)
	if err != nil {
		t.Fatal(err)
	}
	if held := again.MatterHeldFabrics(); len(held) != 0 {
		t.Errorf("held after restart: %v, want none", held)
	}
}

// A drop whose clear of an unread Extension fails keeps the record that
// the setting is unread, so the next drop clears it (review II,
// R2-P7-acl-03, the Fable diff read of phase 7).
func TestDropFabricRetriesAFailedClearOfAnUnreadExtension(t *testing.T) {
	t.Parallel()
	key := "access_control.extension.2"
	st := &p7ExtStore{settingsACLStore: settingsACLStore{settings: map[string]string{key: hex.EncodeToString(encodeExtensionTLV(t))}}}
	st.getErr = errors.New("busy")
	ac, err := core.NewAccessControl(st)
	if err != nil {
		t.Fatal(err)
	}
	st.getErr = nil
	st.setErr = errors.New("disk")
	if err := ac.MatterDropFabric(context.Background(), 2); err == nil {
		t.Fatal("MatterDropFabric(2) reported no error for a failed clear")
	}
	st.setErr = nil
	if err := ac.MatterDropFabric(context.Background(), 2); err != nil {
		t.Fatal(err)
	}
	if v := st.settings[key]; v != "" {
		t.Errorf("after the second MatterDropFabric(2) the setting still holds %q", v)
	}
}

// An Extension write while the fabric listing fails is refused with
// FAILURE and stores nothing: a fabric whose existence cannot be confirmed
// counts as gone (review II, R2-P7-acl-02).
func TestExtensionWriteRefusedWhenFabricListingFails(t *testing.T) {
	t.Parallel()
	st := &p7ExtStore{
		settingsACLStore: settingsACLStore{settings: map[string]string{}},
		fabrics:          []mstore.FabricRecord{{FabricIndex: 7}},
		listErr:          errors.New("busy"),
	}
	ac, err := core.NewAccessControl(st)
	if err != nil {
		t.Fatal(err)
	}
	ctx := im.WithFabricFilter(context.Background(), true, 7)
	err = ac.MatterWrite(ctx, 0x0001, []core.AccessControlExtensionEntry{{Data: encodeExtensionTLV(t)}})
	if err == nil {
		t.Fatal("Extension write stored while the fabric listing failed")
	}
	if got := spec.StatusOf(err); got != im.StatusFailure {
		t.Errorf("status %v (%v), want FAILURE", got, err)
	}
	if v := st.settings["access_control.extension.7"]; v != "" {
		t.Errorf("setting holds %q after a refused write", v)
	}
	// Control: the listing works again and holds fabric 7 — the write lands.
	st.listErr = nil
	if err := ac.MatterWrite(ctx, 0x0001, []core.AccessControlExtensionEntry{{Data: encodeExtensionTLV(t)}}); err != nil {
		t.Fatalf("write with a working listing: %v", err)
	}
}

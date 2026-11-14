// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core_test

import (
	"bytes"
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/im"
	mstore "github.com/SukramJ/go-fabric/store"
	"github.com/SukramJ/go-fabric/store/memstore"
)

// markerCallSettings is GeneralCommissioning's Settings over the node's
// store, counting its reads and writes of the two pending-NOC marker keys.
// In fault mode the failAt-th of those calls fails and every other one
// lands; in crash mode the first crashAfter land and every later one
// fails, as a node that loses power right after the crashAfter-th.
type markerCallSettings struct {
	mstore.Settings
	mu         sync.Mutex
	armed      bool
	crash      bool
	failAt     int
	crashAfter int
	calls      int
}

func (s *markerCallSettings) arm(crash bool, n int) {
	s.mu.Lock()
	s.armed, s.crash, s.calls = true, crash, 0
	s.failAt, s.crashAfter = n, n
	s.mu.Unlock()
}

func (s *markerCallSettings) call(key string) error {
	if key != pendingNOCKey && key != pendingNOCUpdateKey {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.armed {
		return nil
	}
	s.calls++
	switch {
	case s.crash && s.calls > s.crashAfter:
		return errCrashed
	case !s.crash && s.calls == s.failAt:
		return errFault
	}
	return nil
}

func (s *markerCallSettings) GetSetting(ctx context.Context, key string) (value string, ok bool, err error) {
	if err := s.call(key); err != nil {
		return "", false, err
	}
	return s.Settings.GetSetting(ctx, key)
}

func (s *markerCallSettings) SetSetting(ctx context.Context, key, value string) error {
	if err := s.call(key); err != nil {
		return err
	}
	return s.Settings.SetSetting(ctx, key, value)
}

// bothMarkers is the state bothMarkersStanding leaves: the rig, the
// Settings GeneralCommissioning was given, the committed fabric with its
// identity before the UpdateNOC, and the fabric the AddNOC added.
type bothMarkers struct {
	r          failSafeRig
	gcSettings *markerCallSettings
	committed  uint8
	added      uint8
	before     mstore.IdentityRecord
}

// bothMarkersStanding commissions fabric 1, then runs the dr-sec-04 case
// in one fail-safe armed over its CASE session: an UpdateNOC of fabric 1
// that fails after its identity write, then an AddNOC of fabric 2. Both
// marker keys stand; the fail-safe belongs to fabric 2.
func bothMarkersStanding(ctx context.Context, t *testing.T, inner *memstore.Store) bothMarkers {
	t.Helper()
	st := &nodeIDFailingStore{restartStore: inner}
	gcSettings := &markerCallSettings{Settings: inner}
	r := newFailSafeRig(t, st, gcSettings, nil, nil)
	resp, err := r.gc.MatterInvoke(ctx, 0x00, core.ArmFailSafeRequest{ExpiryLengthSeconds: 60})
	if err != nil || resp.(core.ArmFailSafeResponse).ErrorCode != core.CommissioningErrorOK {
		t.Fatalf("ArmFailSafe over PASE: %+v, %v", resp, err)
	}
	_, rootPriv, committed := commissionTestFabric(ctx, t, r.oc)
	if !r.commissioningComplete(ctx, t, committed) {
		t.Fatal("the first CommissioningComplete failed")
	}
	before, err := inner.GetIdentity(ctx, committed)
	if err != nil {
		t.Fatal(err)
	}

	fabCtx := im.WithFabricFilter(ctx, true, committed)
	resp, err = r.gc.MatterInvoke(fabCtx, 0x00, core.ArmFailSafeRequest{ExpiryLengthSeconds: 60})
	if err != nil || resp.(core.ArmFailSafeResponse).ErrorCode != core.CommissioningErrorOK {
		t.Fatalf("ArmFailSafe over CASE: %+v, %v", resp, err)
	}
	pendingPub := issueCSRPendingPubKey(fabCtx, t, r.oc, true)
	nocRaw := buildCoreSignedCertForPubKey(t, pendingPub, false, rootPriv, testDefaultFabricID, testDefaultNodeID+7)
	st.fail.Store(true)
	resp, err = r.oc.MatterInvoke(fabCtx, 0x07, core.UpdateNOCRequest{NOCValue: nocRaw})
	st.fail.Store(false)
	if nr, _ := resp.(core.NOCResponse); err != nil || nr.StatusCode != core.NOCStatusInvalidNOC {
		t.Fatalf("UpdateNOC with a failing node id write: %+v, %v; want InvalidNOC", resp, err)
	}
	resp, err = r.oc.MatterInvoke(fabCtx, 0x06, prepareAddNOC(fabCtx, t, r.oc))
	nr, _ := resp.(core.NOCResponse)
	if err != nil || nr.StatusCode != core.NOCStatusOK {
		t.Fatalf("AddNOC after the failed UpdateNOC: %+v, %v; want OK", resp, err)
	}
	for _, key := range []string{pendingNOCKey, pendingNOCUpdateKey} {
		if v, _, _ := inner.GetSetting(ctx, key); v == "" {
			t.Fatalf("marker %s does not stand before CommissioningComplete", key)
		}
	}
	return bothMarkers{r: r, gcSettings: gcSettings, committed: committed, added: nr.FabricIndex, before: before}
}

// afterRestart is what a restart finds of the two commands: whether the
// added fabric is still there and whether the updated fabric still has
// its pre-update identity (the update reverted).
func afterRestart(ctx context.Context, t *testing.T, inner *memstore.Store, m bothMarkers) (addKept, updateReverted bool) {
	t.Helper()
	restartOpcreds(t, inner)
	_, err := inner.GetFabric(ctx, m.added)
	addKept = err == nil
	id, err := inner.GetIdentity(ctx, m.committed)
	if err != nil {
		t.Fatalf("the committed fabric %d lost its identity: %v", m.committed, err)
	}
	return addKept, bytes.Equal(id.NOC, m.before.NOC) && bytes.Equal(id.PrivateKey, m.before.PrivateKey)
}

// TestGencomm_CommissioningCompleteCommitsBothMarkersOrNeither pins review
// II R2-P1-rw-06: CommissioningComplete clears the AddNOC's and the
// UpdateNOC's markers with two store calls. A fault at any of its marker
// reads and writes answers FAILURE, and a restart before the fail-safe
// expires then rolls back both commands — the add key a failed update-key
// clear had already cleared is written back. Without a fault it answers
// success and a restart keeps both.
func TestGencomm_CommissioningCompleteCommitsBothMarkersOrNeither(t *testing.T) {
	t.Parallel()
	// 0: no fault. 1, 2: the reads of the add and the update key; 3, 4:
	// their clears.
	for failAt := range 5 {
		t.Run(fmt.Sprintf("fault at marker call %d", failAt), func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			inner := memstore.New()
			m := bothMarkersStanding(ctx, t, inner)
			m.gcSettings.arm(false, failAt)
			ok := m.r.commissioningComplete(ctx, t, m.added)
			if ok != (failAt == 0) {
				t.Fatalf("CommissioningComplete answered success=%v with a fault at marker call %d", ok, failAt)
			}
			addKept, updateReverted := afterRestart(ctx, t, inner, m)
			if ok && (!addKept || updateReverted) {
				t.Fatalf("after a successful CommissioningComplete and a restart: added fabric kept %v, update reverted %v; want kept, not reverted", addKept, updateReverted)
			}
			if !ok && (addKept || !updateReverted) {
				t.Fatalf("after CommissioningComplete answered FAILURE (fault at marker call %d) and a restart: added fabric kept %v, update reverted %v; want both rolled back", failAt, addKept, updateReverted)
			}
		})
	}
}

// TestGencomm_CommissioningCompleteCrashPoints walks the node going down
// after each of CommissioningComplete's marker calls, before it answers:
// up to the add key's clear both commands roll back at the restart; after
// the update key's clear both are committed. Between the two clears — the
// one point the store's two separate writes leave — the added fabric is
// committed and the update rolled back; the commissioner got no answer
// there (review II, R2-P1-rw-06).
func TestGencomm_CommissioningCompleteCrashPoints(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		landed                  int
		addKept, updateReverted bool
	}{
		{landed: 0, addKept: false, updateReverted: true},
		{landed: 1, addKept: false, updateReverted: true},
		{landed: 2, addKept: false, updateReverted: true},
		{landed: 3, addKept: true, updateReverted: true},
		{landed: 4, addKept: true, updateReverted: false},
	} {
		t.Run(fmt.Sprintf("down after %d marker calls", tc.landed), func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			inner := memstore.New()
			m := bothMarkersStanding(ctx, t, inner)
			m.gcSettings.arm(true, tc.landed)
			_ = m.r.commissioningComplete(ctx, t, m.added) // the node is down: no answer reaches the commissioner
			addKept, updateReverted := afterRestart(ctx, t, inner, m)
			if addKept != tc.addKept || updateReverted != tc.updateReverted {
				t.Fatalf("restart after %d marker calls: added fabric kept %v, update reverted %v; want %v, %v", tc.landed, addKept, updateReverted, tc.addKept, tc.updateReverted)
			}
		})
	}
}

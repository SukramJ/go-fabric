// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core_test

import (
	"context"
	"errors"
	"testing"

	"thingfabric.dev/go-fabric/cluster/core"
	"thingfabric.dev/go-fabric/im"
	mstore "thingfabric.dev/go-fabric/store"
	"thingfabric.dev/go-fabric/store/memstore"
)

// errStoreDown stands in for a persistence layer that cannot answer.
var errStoreDown = errors.New("group store is unavailable")

// brokenGroupStore fails the one call each test names and answers every
// other call the way an empty store would. It exists because the
// GroupKeyManagement cluster is a facade over the store: the interesting
// paths are the ones where the store says no, and a working fake never
// reaches them.
type brokenGroupStore struct {
	failUpsert       bool
	failGet          bool
	failList         bool
	failRemoveSet    bool
	failSetMapping   bool
	failRemoveMap    bool
	failReplaceMap   bool
	failListMappings bool

	// mappings is the state a non-failing ListGroupKeyMappings returns.
	mappings []mstore.GroupKeyMapping
	// sets is the state a non-failing ListGroupKeySets returns.
	sets []mstore.GroupKeySet
}

func (s *brokenGroupStore) UpsertGroupKeySet(context.Context, mstore.GroupKeySet) error {
	if s.failUpsert {
		return errStoreDown
	}
	return nil
}

func (s *brokenGroupStore) GetGroupKeySet(_ context.Context, _ uint8, id uint16) (mstore.GroupKeySet, error) {
	if s.failGet {
		return mstore.GroupKeySet{}, errStoreDown
	}
	for _, ks := range s.sets {
		if ks.GroupKeySetID == id {
			return ks, nil
		}
	}
	return mstore.GroupKeySet{}, mstore.ErrGroupKeySetNotFound
}

func (s *brokenGroupStore) ListGroupKeySets(context.Context, uint8) ([]mstore.GroupKeySet, error) {
	if s.failList {
		return nil, errStoreDown
	}
	return s.sets, nil
}

func (s *brokenGroupStore) RemoveGroupKeySet(context.Context, uint8, uint16) error {
	if s.failRemoveSet {
		return errStoreDown
	}
	return nil
}

func (s *brokenGroupStore) SetGroupKeyMapping(context.Context, mstore.GroupKeyMapping) error {
	if s.failSetMapping {
		return errStoreDown
	}
	return nil
}

func (s *brokenGroupStore) RemoveGroupKeyMapping(context.Context, uint8, uint16) error {
	if s.failRemoveMap {
		return errStoreDown
	}
	return nil
}

func (s *brokenGroupStore) ReplaceGroupKeyMappings(context.Context, uint8, []mstore.GroupKeyMapping) error {
	if s.failReplaceMap {
		return errStoreDown
	}
	return nil
}

func (s *brokenGroupStore) ListGroupKeyMappings(context.Context, uint8) ([]mstore.GroupKeyMapping, error) {
	if s.failListMappings {
		return nil, errStoreDown
	}
	return s.mappings, nil
}

func newBrokenGKM(t *testing.T, s *brokenGroupStore) *core.GroupKeyManagement {
	t.Helper()
	gkm, err := core.NewGroupKeyManagement(s, core.GroupKeyMgmtConfig{})
	if err != nil {
		t.Fatalf("NewGroupKeyManagement: %v", err)
	}
	gkm.SetCurrentFabric(1)
	return gkm
}

// validKeySet is a KeySetWrite payload that passes every validation arm,
// so a test can vary exactly one field and know the refusal came from
// that field. EpochKey0 is 16 bytes (chip
// src/app/clusters/group-key-mgmt-server: epochKey.size() == 16) and
// GroupKeySecurityPolicy is TrustFirst, the only policy matter.js
// GroupKeyManagementServer.ts accepts.
func validKeySet(id uint16) core.KeySetWriteRequest {
	return core.KeySetWriteRequest{GroupKeySet: core.GroupKeySetStruct{
		GroupKeySetID:          id,
		GroupKeySecurityPolicy: uint8(mstore.SecurityPolicyTrustFirst),
		EpochKey0:              make([]byte, 16),
		EpochStartTime0:        1000,
	}}
}

// TestGKMPrivilegesMatchTheElementAccessTags pins the privilege each
// command and each writable attribute demands. Every GroupKeyManagement
// command carries access "F A" — Administer — in matter.js
// packages/model/src/standard/elements/group-key-management.element.ts:58,64,75,81,
// and GroupKeyMap carries "RW F VM" — Manage — at :29. Getting one of
// these too low would let an Operate-privileged fabric rewrite another
// controller's group keys.
func TestGKMPrivilegesMatchTheElementAccessTags(t *testing.T) {
	t.Parallel()
	gkm := newGKM(t)

	const administer, manage, operate uint8 = 5, 4, 3
	for _, cmdID := range []uint32{0x00, 0x01, 0x03, 0x04} {
		if got := gkm.MinInvokePrivilege(cmdID); got != administer {
			t.Errorf("MinInvokePrivilege(0x%02X) = %d, want %d (Administer, access \"F A\")", cmdID, got, administer)
		}
	}
	// A command the cluster does not carry falls back to the standard
	// default rather than accidentally advertising Administer.
	if got := gkm.MinInvokePrivilege(0x7F); got != operate {
		t.Errorf("MinInvokePrivilege(unknown) = %d, want %d (Operate default)", got, operate)
	}

	if got := gkm.MinWritePrivilege(0x0000); got != manage {
		t.Errorf("MinWritePrivilege(GroupKeyMap) = %d, want %d (Manage, access \"RW F VM\")", got, manage)
	}
	if got := gkm.MinWritePrivilege(0x0002); got != operate {
		t.Errorf("MinWritePrivilege(MaxGroupsPerFabric) = %d, want %d (Operate default)", got, operate)
	}
}

// TestGKMUnsupportedCommandIsRefused covers the fall-through arm of the
// command dispatch. The cluster accepts exactly KeySetWrite 0x0,
// KeySetRead 0x1, KeySetRemove 0x3 and KeySetReadAllIndices 0x4
// (group-key-management.element.ts:58,64,75,81); anything else must
// answer UnsupportedCommand rather than succeed silently.
func TestGKMUnsupportedCommandIsRefused(t *testing.T) {
	t.Parallel()
	gkm := newGKM(t)

	// 0x02 is KeySetReadResponse — a response, never an accepted command.
	_, err := gkm.MatterInvoke(fabricCtx(1), 0x02, nil)
	var status im.StatusCodeError
	if !errors.As(err, &status) {
		t.Fatalf("err = %v (%T), want an im.StatusCodeError", err, err)
	}
	if got := status.MatterStatusCode(); got != im.StatusUnsupportedCommand {
		t.Errorf("status = %v, want UnsupportedCommand", got)
	}
}

// TestGKMCommandsRejectTheWrongRequestType covers the payload type guard
// on each handler. The cluster is invoked with typed requests; a
// mismatched type is an invalid argument, not a request to act on
// whatever zero value the assertion would have produced — a
// zero-valued KeySetRemoveRequest would target key set 0, the IPK.
func TestGKMCommandsRejectTheWrongRequestType(t *testing.T) {
	t.Parallel()
	cases := map[string]uint32{
		"KeySetWrite":  0x00,
		"KeySetRead":   0x01,
		"KeySetRemove": 0x03,
	}
	for name, cmdID := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			gkm := newGKM(t)
			_, err := gkm.MatterInvoke(fabricCtx(1), cmdID, "not a request")
			var sce im.StatusCodeError
			if !errors.As(err, &sce) || sce.MatterStatusCode() != im.StatusInvalidCommand {
				t.Fatalf("%s with a payload of the wrong type: %v, want INVALID_COMMAND (W6-test-17, R2-P6-err-3112)", name, err)
			}
		})
	}
}

// TestGKMSurfacesStoreFailures pins that a persistence failure reaches
// the controller as an error. The cluster is a facade over the store, so
// a swallowed store error would answer Success on a key set that was
// never written — and a commissioner would then use a group key the
// bridge does not hold.
func TestGKMSurfacesStoreFailures(t *testing.T) {
	t.Parallel()
	ctx := fabricCtx(1)

	t.Run("KeySetWrite upsert fails", func(t *testing.T) {
		t.Parallel()
		gkm := newBrokenGKM(t, &brokenGroupStore{failUpsert: true})
		if _, err := gkm.MatterInvoke(ctx, 0x00, validKeySet(7)); !errors.Is(err, errStoreDown) {
			t.Fatalf("err = %v, want it to wrap %v", err, errStoreDown)
		}
	})
	t.Run("KeySetWrite budget list fails", func(t *testing.T) {
		t.Parallel()
		gkm := newBrokenGKM(t, &brokenGroupStore{failList: true})
		if _, err := gkm.MatterInvoke(ctx, 0x00, validKeySet(7)); !errors.Is(err, errStoreDown) {
			t.Fatalf("err = %v, want it to wrap %v", err, errStoreDown)
		}
	})
	t.Run("KeySetRead get fails", func(t *testing.T) {
		t.Parallel()
		gkm := newBrokenGKM(t, &brokenGroupStore{failGet: true})
		_, err := gkm.MatterInvoke(ctx, 0x01, core.KeySetReadRequest{GroupKeySetID: 7})
		if !errors.Is(err, errStoreDown) {
			t.Fatalf("err = %v, want it to wrap %v", err, errStoreDown)
		}
		// A store outage is not a NotFound: telling a controller the key
		// set does not exist would have it write a fresh one over a key
		// set that may still be there.
		var status im.StatusCodeError
		if errors.As(err, &status) && status.MatterStatusCode() == im.StatusNotFound {
			t.Error("a store outage reported as NotFound")
		}
	})
	t.Run("KeySetRemove existence check fails", func(t *testing.T) {
		t.Parallel()
		gkm := newBrokenGKM(t, &brokenGroupStore{failGet: true})
		if _, err := gkm.MatterInvoke(ctx, 0x03, core.KeySetRemoveRequest{GroupKeySetID: 7}); !errors.Is(err, errStoreDown) {
			t.Fatalf("err = %v, want it to wrap %v", err, errStoreDown)
		}
	})
	t.Run("KeySetRemove delete fails", func(t *testing.T) {
		t.Parallel()
		gkm := newBrokenGKM(t, &brokenGroupStore{
			failRemoveSet: true,
			sets:          []mstore.GroupKeySet{{FabricIndex: 1, GroupKeySetID: 7}},
		})
		if _, err := gkm.MatterInvoke(ctx, 0x03, core.KeySetRemoveRequest{GroupKeySetID: 7}); !errors.Is(err, errStoreDown) {
			t.Fatalf("err = %v, want it to wrap %v", err, errStoreDown)
		}
	})
	t.Run("KeySetReadAllIndices list fails", func(t *testing.T) {
		t.Parallel()
		gkm := newBrokenGKM(t, &brokenGroupStore{failList: true})
		if _, err := gkm.MatterInvoke(ctx, 0x04, nil); !errors.Is(err, errStoreDown) {
			t.Fatalf("err = %v, want it to wrap %v", err, errStoreDown)
		}
	})
	t.Run("GroupKeyMap read fails", func(t *testing.T) {
		t.Parallel()
		gkm := newBrokenGKM(t, &brokenGroupStore{failListMappings: true})
		if _, ok := gkm.MatterReadFiltered(im.WithFabricFilter(ctx, true, 1), 0x0000); ok {
			t.Fatal("GroupKeyMap answered ok=true while the store was down")
		}
	})
	t.Run("GroupKeyMap write list fails", func(t *testing.T) {
		t.Parallel()
		gkm := newBrokenGKM(t, &brokenGroupStore{failListMappings: true})
		err := gkm.MatterWrite(ctx, 0x0000, []core.GroupKeyMapStruct{{GroupID: 1, GroupKeySetID: 2}})
		if !errors.Is(err, errStoreDown) {
			t.Fatalf("err = %v, want it to wrap %v", err, errStoreDown)
		}
	})
	t.Run("GroupKeyMap write replace fails", func(t *testing.T) {
		t.Parallel()
		gkm := newBrokenGKM(t, &brokenGroupStore{
			failReplaceMap: true,
			mappings:       []mstore.GroupKeyMapping{{FabricIndex: 1, GroupID: 9, GroupKeySetID: 2}},
		})
		err := gkm.MatterWrite(ctx, 0x0000, []core.GroupKeyMapStruct{{GroupID: 1, GroupKeySetID: 2}})
		if !errors.Is(err, errStoreDown) {
			t.Fatalf("err = %v, want it to wrap %v", err, errStoreDown)
		}
	})
}

// midReplaceStore is a memstore whose key-map writes fail after the
// first one: the piecewise write a GroupKeyMap write used to make
// (SetGroupKeyMapping per entry, RemoveGroupKeyMapping per dropped one)
// lands its sets and fails its removal, and the one atomic replace fails
// whole.
type midReplaceStore struct {
	*memstore.Store
}

func (midReplaceStore) RemoveGroupKeyMapping(context.Context, uint8, uint16) error {
	return errStoreDown
}

func (midReplaceStore) ReplaceGroupKeyMappings(context.Context, uint8, []mstore.GroupKeyMapping) error {
	return errStoreDown
}

// TestGKMGroupKeyMapWriteFailingMidReplaceKeepsTheOldMap: a store that
// refuses the GroupKeyMap write part-way leaves the map the fabric held,
// served under the same DataVersion, and the write fails (FAILURE) —
// the map is one store write (store.GroupKeys ReplaceGroupKeyMappings;
// review II, R2-P5-port-04).
func TestGKMGroupKeyMapWriteFailingMidReplaceKeepsTheOldMap(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mem := memstore.New()
	idx, err := mem.AddFabric(ctx, mstore.FabricRecord{FabricID: 1, NodeID: 2, RootPublicKey: []byte{4, 1}, VendorID: 0xFFF1})
	if err != nil {
		t.Fatal(err)
	}
	if err := mem.SetGroupKeyMapping(ctx, mstore.GroupKeyMapping{FabricIndex: idx, GroupID: 9, GroupKeySetID: 2}); err != nil {
		t.Fatal(err)
	}
	gkm, err := core.NewGroupKeyManagement(midReplaceStore{mem}, core.GroupKeyMgmtConfig{})
	if err != nil {
		t.Fatal(err)
	}
	gkm.SetCurrentFabric(idx)
	before := gkm.MatterDataVersion()
	err = gkm.MatterWrite(ctx, 0x0000, []core.GroupKeyMapStruct{{GroupID: 1, GroupKeySetID: 2}})
	if !errors.Is(err, errStoreDown) {
		t.Fatalf("err = %v, want it to wrap %v", err, errStoreDown)
	}
	got, err := mem.ListGroupKeyMappings(ctx, idx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].GroupID != 9 || got[0].GroupKeySetID != 2 {
		t.Errorf("stored GroupKeyMap after the refused write = %+v, want only group 9 -> set 2", got)
	}
	if after := gkm.MatterDataVersion(); after != before {
		t.Errorf("DataVersion %d -> %d after a refused write", before, after)
	}
}

// TestGKMGroupKeyMapWriteRejectsTheWrongValueType covers the type guard
// on the one writable attribute. GroupKeyMap is a list of
// GroupKeyMapStruct (group-key-management.element.ts:29-33); anything
// else must be refused rather than clearing the fabric's bindings, which
// is what an empty-list coercion would do.
func TestGKMGroupKeyMapWriteRejectsTheWrongValueType(t *testing.T) {
	t.Parallel()
	gkm := newGKM(t)
	gkm.SetCurrentFabric(1)
	err := gkm.MatterWrite(context.Background(), 0x0000, "not a list")
	var sce im.StatusCodeError
	if !errors.As(err, &sce) || sce.MatterStatusCode() != im.StatusInvalidDataType {
		t.Fatalf("GroupKeyMap write of a string: %v, want INVALID_DATA_TYPE (owner decision 234; W6-test-17, R2-P6-err-3112)", err)
	}
}

// TestGKMKeySetWriteEpochKeyLengthConstraint pins the 16-byte epoch-key
// constraint on all three slots: CONSTRAINT_ERROR. In matter.js the length
// is the request schema's, not the handler's — the model bounds each epoch
// key to 16 bytes (packages/model/src/standard/elements/group-key-management.element.ts:117,
// :119, :121 at db021da0), TlvOfModel builds the bound into the request
// TLV (packages/types/src/tlv/TlvOfModel.ts:213-214, :320-326), and
// CommandInvokeResponse validates it before the handler
// (packages/protocol/src/action/server/CommandInvokeResponse.ts:486), a
// ValidationOutOfBoundsError answered CONSTRAINT_ERROR (TlvString.ts:82-90,
// ValidationError.ts:16-20); GroupKeyManagementServer.ts:291-357 has no
// length check. chip answers the same
// (src/app/clusters/group-key-mgmt-server/GroupKeyManagementCluster.cpp:300-305,
// :332-337, :371-376 at 6170af84). Storing a short key would leave the
// bridge deriving group session keys from material the controller never
// sent. Each subtest sets the accessing fabric, so the length check, not a
// missing fabric (UNSUPPORTED_ACCESS), is what refuses.
func TestGKMKeySetWriteEpochKeyLengthConstraint(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("EpochKey0 too short", func(t *testing.T) {
		t.Parallel()
		gkm := newGKM(t)
		req := validKeySet(7)
		req.GroupKeySet.EpochKey0 = make([]byte, 8)
		if _, err := gkm.MatterInvoke(im.WithFabricFilter(ctx, false, 1), 0x00, req); statusOf(err) != im.StatusConstraintError {
			t.Fatalf("an 8-byte EpochKey0: %v (status %v), want CONSTRAINT_ERROR", err, statusOf(err))
		}
	})
	t.Run("EpochKey1 too long", func(t *testing.T) {
		t.Parallel()
		gkm := newGKM(t)
		req := validKeySet(7)
		req.GroupKeySet.EpochKey1 = make([]byte, 17)
		req.GroupKeySet.EpochStartTime1 = 2000
		if _, err := gkm.MatterInvoke(im.WithFabricFilter(ctx, false, 1), 0x00, req); statusOf(err) != im.StatusConstraintError {
			t.Fatalf("a 17-byte EpochKey1: %v (status %v), want CONSTRAINT_ERROR", err, statusOf(err))
		}
	})
	t.Run("EpochKey2 too short", func(t *testing.T) {
		t.Parallel()
		gkm := newGKM(t)
		req := validKeySet(7)
		req.GroupKeySet.EpochKey1 = make([]byte, 16)
		req.GroupKeySet.EpochStartTime1 = 2000
		req.GroupKeySet.EpochKey2 = make([]byte, 4)
		req.GroupKeySet.EpochStartTime2 = 3000
		if _, err := gkm.MatterInvoke(im.WithFabricFilter(ctx, false, 1), 0x00, req); statusOf(err) != im.StatusConstraintError {
			t.Fatalf("a 4-byte EpochKey2: %v (status %v), want CONSTRAINT_ERROR", err, statusOf(err))
		}
	})
}

// TestGKMKeySetWriteDisablesASlotOnTheSentinelStartTime pins the
// MAX_64BIT_TIME handling. matter.js GroupKeyManagementServer.ts:24,298-309
// treats an EpochStartTime of 0xFFFFFFFFFFFFFFFF as "this slot is
// disabled" and nulls the paired key. Slot 1 carrying the sentinel must
// therefore leave a key set that is valid on slot 0 alone — not a
// refusal, and not a stored key with a nonsensical start time.
func TestGKMKeySetWriteDisablesASlotOnTheSentinelStartTime(t *testing.T) {
	t.Parallel()
	gkm := newGKM(t)

	req := validKeySet(7)
	req.GroupKeySet.EpochKey1 = make([]byte, 16)
	req.GroupKeySet.EpochStartTime1 = 0xFFFFFFFFFFFFFFFF
	req.GroupKeySet.EpochKey2 = make([]byte, 16)
	req.GroupKeySet.EpochStartTime2 = 0xFFFFFFFFFFFFFFFF

	if _, err := gkm.MatterInvoke(fabricCtx(1), 0x00, req); err != nil {
		t.Fatalf("KeySetWrite with two disabled slots: %v", err)
	}
	raw, err := gkm.MatterInvoke(fabricCtx(1), 0x01, core.KeySetReadRequest{GroupKeySetID: 7})
	if err != nil {
		t.Fatalf("KeySetRead: %v", err)
	}
	resp, ok := raw.(core.KeySetReadResponse)
	if !ok {
		t.Fatalf("KeySetRead returned %T, want KeySetReadResponse", raw)
	}
	if resp.GroupKeySet.EpochStartTime1 != 0 || resp.GroupKeySet.EpochStartTime2 != 0 {
		t.Errorf("EpochStartTime1/2 = %d/%d, want the disabled slots stored as 0",
			resp.GroupKeySet.EpochStartTime1, resp.GroupKeySet.EpochStartTime2)
	}
	if resp.GroupKeySet.EpochStartTime0 != 1000 {
		t.Errorf("EpochStartTime0 = %d, want the 1000 that was written", resp.GroupKeySet.EpochStartTime0)
	}
}

// TestGKMKeySetWriteEpochSlotOrdering pins the ordering rules matter.js
// GroupKeyManagementServer.ts:291-357 at db021da0 enforces between the three epoch
// slots, each answered INVALID_COMMAND by the handler: EpochStartTime1 not
// after EpochStartTime0 :329-334, EpochStartTime1 without EpochKey1
// :335-337, EpochKey2 without EpochKey1 :339-341, EpochStartTime2 not after
// EpochStartTime1 :342-350, EpochStartTime2 without EpochKey2 :351-353.
// Each case names one broken relationship; a bridge that accepted them
// would rotate onto a key at a time no controller agreed on.
func TestGKMKeySetWriteEpochSlotOrdering(t *testing.T) {
	t.Parallel()
	cases := map[string]func(*core.GroupKeySetStruct){
		"EpochStartTime1 not after EpochStartTime0": func(g *core.GroupKeySetStruct) {
			g.EpochKey1 = make([]byte, 16)
			g.EpochStartTime1 = 1000 // equal to EpochStartTime0
		},
		"EpochStartTime1 set without EpochKey1": func(g *core.GroupKeySetStruct) {
			g.EpochStartTime1 = 2000
		},
		"EpochKey2 without EpochKey1": func(g *core.GroupKeySetStruct) {
			g.EpochKey2 = make([]byte, 16)
			g.EpochStartTime2 = 3000
		},
		"EpochStartTime2 not after EpochStartTime1": func(g *core.GroupKeySetStruct) {
			g.EpochKey1 = make([]byte, 16)
			g.EpochStartTime1 = 2000
			g.EpochKey2 = make([]byte, 16)
			g.EpochStartTime2 = 2000
		},
		"EpochStartTime2 set without EpochKey2": func(g *core.GroupKeySetStruct) {
			g.EpochKey1 = make([]byte, 16)
			g.EpochStartTime1 = 2000
			g.EpochStartTime2 = 3000
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			gkm := newGKM(t)
			req := validKeySet(7)
			mutate(&req.GroupKeySet)
			if _, err := gkm.MatterInvoke(fabricCtx(1), 0x00, req); statusOf(err) != im.StatusInvalidCommand {
				t.Fatalf("KeySetWrite with %s: %v (status %v), want INVALID_COMMAND", name, err, statusOf(err))
			}
		})
	}
}

// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package core_test

import (
	"context"
	"errors"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/im"
	mstore "github.com/SukramJ/go-fabric/store"
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
	gkm.SetCurrentFabric(1)

	// 0x02 is KeySetReadResponse — a response, never an accepted command.
	_, err := gkm.MatterInvoke(context.Background(), 0x02, nil)
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
			gkm.SetCurrentFabric(1)
			if _, err := gkm.MatterInvoke(context.Background(), cmdID, "not a request"); err == nil {
				t.Fatalf("%s accepted a payload of the wrong type", name)
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
	ctx := context.Background()

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
		if _, ok := gkm.MatterRead(0x0000); ok {
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
	t.Run("GroupKeyMap write set fails", func(t *testing.T) {
		t.Parallel()
		gkm := newBrokenGKM(t, &brokenGroupStore{failSetMapping: true})
		err := gkm.MatterWrite(ctx, 0x0000, []core.GroupKeyMapStruct{{GroupID: 1, GroupKeySetID: 2}})
		if !errors.Is(err, errStoreDown) {
			t.Fatalf("err = %v, want it to wrap %v", err, errStoreDown)
		}
	})
	t.Run("GroupKeyMap write remove fails", func(t *testing.T) {
		t.Parallel()
		// One binding exists and the write omits it, so the replace
		// semantics have to delete it — and that delete fails.
		gkm := newBrokenGKM(t, &brokenGroupStore{
			failRemoveMap: true,
			mappings:      []mstore.GroupKeyMapping{{FabricIndex: 1, GroupID: 9, GroupKeySetID: 2}},
		})
		err := gkm.MatterWrite(ctx, 0x0000, []core.GroupKeyMapStruct{{GroupID: 1, GroupKeySetID: 2}})
		if !errors.Is(err, errStoreDown) {
			t.Fatalf("err = %v, want it to wrap %v", err, errStoreDown)
		}
	})
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
	if err := gkm.MatterWrite(context.Background(), 0x0000, "not a list"); err == nil {
		t.Fatal("GroupKeyMap accepted a string")
	}
}

// TestGKMKeySetWriteEpochKeyLengthConstraint pins the 16-byte epoch-key
// constraint on all three slots. A short key is a ConstraintError in chip
// (src/app/clusters/group-key-mgmt-server: epochKey.size() == 16 →
// CHIP_ERROR_INVALID_ARGUMENT); storing one would leave the bridge
// deriving group session keys from material the controller never sent.
func TestGKMKeySetWriteEpochKeyLengthConstraint(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("EpochKey0 too short", func(t *testing.T) {
		t.Parallel()
		req := validKeySet(7)
		req.GroupKeySet.EpochKey0 = make([]byte, 8)
		if _, err := newGKM(t).MatterInvoke(ctx, 0x00, req); err == nil {
			t.Fatal("an 8-byte EpochKey0 was accepted")
		}
	})
	t.Run("EpochKey1 too long", func(t *testing.T) {
		t.Parallel()
		gkm := newGKM(t)
		gkm.SetCurrentFabric(1)
		req := validKeySet(7)
		req.GroupKeySet.EpochKey1 = make([]byte, 17)
		req.GroupKeySet.EpochStartTime1 = 2000
		if _, err := gkm.MatterInvoke(ctx, 0x00, req); err == nil {
			t.Fatal("a 17-byte EpochKey1 was accepted")
		}
	})
	t.Run("EpochKey2 too short", func(t *testing.T) {
		t.Parallel()
		gkm := newGKM(t)
		gkm.SetCurrentFabric(1)
		req := validKeySet(7)
		req.GroupKeySet.EpochKey1 = make([]byte, 16)
		req.GroupKeySet.EpochStartTime1 = 2000
		req.GroupKeySet.EpochKey2 = make([]byte, 4)
		req.GroupKeySet.EpochStartTime2 = 3000
		if _, err := gkm.MatterInvoke(ctx, 0x00, req); err == nil {
			t.Fatal("a 4-byte EpochKey2 was accepted")
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
	gkm.SetCurrentFabric(1)

	req := validKeySet(7)
	req.GroupKeySet.EpochKey1 = make([]byte, 16)
	req.GroupKeySet.EpochStartTime1 = 0xFFFFFFFFFFFFFFFF
	req.GroupKeySet.EpochKey2 = make([]byte, 16)
	req.GroupKeySet.EpochStartTime2 = 0xFFFFFFFFFFFFFFFF

	if _, err := gkm.MatterInvoke(context.Background(), 0x00, req); err != nil {
		t.Fatalf("KeySetWrite with two disabled slots: %v", err)
	}
	raw, err := gkm.MatterInvoke(context.Background(), 0x01, core.KeySetReadRequest{GroupKeySetID: 7})
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
// GroupKeyManagementServer.ts:280-352 enforces between the three epoch
// slots. Each case names one broken relationship; a bridge that accepted
// them would rotate onto a key at a time no controller agreed on.
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
			gkm.SetCurrentFabric(1)
			req := validKeySet(7)
			mutate(&req.GroupKeySet)
			if _, err := gkm.MatterInvoke(context.Background(), 0x00, req); err == nil {
				t.Fatalf("KeySetWrite accepted %s", name)
			}
		})
	}
}

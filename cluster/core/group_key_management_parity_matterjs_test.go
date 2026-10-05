// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package core_test

// Behavioural parity for GroupKeyManagement against matter.js HEAD
// packages/node/src/behaviors/group-key-management/GroupKeyManagementServer.ts.
// Each case names the matter.js function whose outcome it pins.

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/im"
)

func gkmStatus(t *testing.T, err error) im.StatusCode {
	t.Helper()
	if err == nil {
		return im.StatusSuccess
	}
	var sce im.StatusCodeError
	if !errors.As(err, &sce) {
		t.Fatalf("error %v carries no IM status", err)
	}
	return sce.MatterStatusCode()
}

func TestGroupKeyManagementParityMatterJS(t *testing.T) {
	t.Parallel()
	ctx := im.WithFabricFilter(context.Background(), true, 1)
	write := func(gkm *core.GroupKeyManagement, id uint16, policy uint8) error {
		_, err := gkm.MatterInvoke(ctx, 0x00, core.KeySetWriteRequest{GroupKeySet: core.GroupKeySetStruct{
			GroupKeySetID: id, EpochKey0: make([]byte, 16), EpochStartTime0: 1, GroupKeyMulticastPolicy: policy,
		}})
		return err
	}

	t.Run("keySetWrite ignores GroupKeyMulticastPolicy (452d6f5c)", func(t *testing.T) {
		t.Parallel()
		gkm, _ := core.NewGroupKeyManagement(newFakeStore(), core.GroupKeyMgmtConfig{})
		if err := write(gkm, 7, 1 /* AllNodes */); err != nil {
			t.Fatalf("KeySetWrite with AllNodes policy: %v", err)
		}
	})

	t.Run("keySetRead reports PerGroupID", func(t *testing.T) {
		t.Parallel()
		gkm, _ := core.NewGroupKeyManagement(newFakeStore(), core.GroupKeyMgmtConfig{})
		if err := write(gkm, 7, 1); err != nil {
			t.Fatal(err)
		}
		resp, err := gkm.MatterInvoke(ctx, 0x01, core.KeySetReadRequest{GroupKeySetID: 7})
		if err != nil {
			t.Fatal(err)
		}
		got := resp.(core.KeySetReadResponse).GroupKeySet
		if got.GroupKeyMulticastPolicy != core.GroupKeyMulticastPolicyPerGroupID || got.EpochKey0 != nil {
			t.Fatalf("KeySetRead = %+v, want PerGroupID and no key", got)
		}
	})

	t.Run("keySetReadAllIndices lists key set 0 first", func(t *testing.T) {
		t.Parallel()
		gkm, _ := core.NewGroupKeyManagement(newFakeStore(), core.GroupKeyMgmtConfig{})
		for _, id := range []uint16{9, 3} {
			if err := write(gkm, id, 0); err != nil {
				t.Fatal(err)
			}
		}
		resp, err := gkm.MatterInvoke(ctx, 0x04, nil)
		if err != nil {
			t.Fatal(err)
		}
		// matter.js lists the written key sets in write order, the store
		// in id order; the list is a set, only key set 0 leading is pinned.
		got := resp.(core.KeySetReadAllIndicesResponse).GroupKeySetIDs
		if len(got) != 3 || got[0] != 0 || !slices.Contains(got, 3) || !slices.Contains(got, 9) {
			t.Fatalf("GroupKeySetIDs = %v, want 0 followed by 3 and 9", got)
		}
	})

	t.Run("keySetWrite counts the implicit IPK against maxGroupKeysPerFabric", func(t *testing.T) {
		t.Parallel()
		gkm, _ := core.NewGroupKeyManagement(newFakeStore(), core.GroupKeyMgmtConfig{MaxGroupKeysPerFabric: 2})
		if err := write(gkm, 1, 0); err != nil {
			t.Fatalf("first key set: %v", err)
		}
		if got := gkmStatus(t, write(gkm, 2, 0)); got != im.StatusResourceExhausted {
			t.Fatalf("second key set at cap 2 = %v, want ResourceExhausted", got)
		}
	})

	t.Run("#validateGroupKeyMap", func(t *testing.T) {
		t.Parallel()
		cases := []struct {
			name string
			max  uint16
			list []core.GroupKeyMapStruct
			want im.StatusCode
		}{
			{"group id 0 is not operational", 0, []core.GroupKeyMapStruct{{GroupID: 0, GroupKeySetID: 1}}, im.StatusInvalidAction},
			{"universal group id is not operational", 0, []core.GroupKeyMapStruct{{GroupID: 0xFF00, GroupKeySetID: 1}}, im.StatusInvalidAction},
			{"duplicate group id", 0, []core.GroupKeyMapStruct{{GroupID: 5, GroupKeySetID: 1}, {GroupID: 5, GroupKeySetID: 2}}, im.StatusConstraintError},
			{"more groups than maxGroupsPerFabric", 1, []core.GroupKeyMapStruct{{GroupID: 5, GroupKeySetID: 1}, {GroupID: 6, GroupKeySetID: 1}}, im.StatusResourceExhausted},
			{"application group ids within the cap", 2, []core.GroupKeyMapStruct{{GroupID: 1, GroupKeySetID: 1}, {GroupID: 0xFEFF, GroupKeySetID: 1}}, im.StatusSuccess},
		}
		for _, tc := range cases {
			fs := newFakeStore()
			gkm, _ := core.NewGroupKeyManagement(fs, core.GroupKeyMgmtConfig{MaxGroupsPerFabric: tc.max})
			if err := write(gkm, 1, 0); err != nil {
				t.Fatal(err)
			}
			if err := write(gkm, 2, 0); err != nil {
				t.Fatal(err)
			}
			if got := gkmStatus(t, gkm.MatterWrite(ctx, 0x0000, tc.list)); got != tc.want {
				t.Errorf("%s: status %v, want %v", tc.name, got, tc.want)
			}
		}
	})
}

// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package client

// A port of matter.js packages/node/test/behavior/system/icd/litSupportedTest.ts
// at 862ad75f: a peer's LIT support is its LongIdleTimeSupport and a
// specification version of 1.4.0 or later. matter.js commissions devices
// reporting the versions; a node of this module always reports 1.6.1, so the
// port applies a report of each shape to a node's image, the way
// TestProperties does.

import (
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/cluster/spec/basicinformation"
	"github.com/SukramJ/go-fabric/cluster/spec/descriptor"
	icddef "github.com/SukramJ/go-fabric/cluster/spec/icdmanagement"
)

// litImage is a node whose root serves BasicInformation at version and,
// with features, IcdManagement.
func litImage(t *testing.T, version uint64, icd bool, features icddef.Feature) *Node {
	t.Helper()
	n, _, _ := testNode(t, &MemoryStore{})
	b := &report{}
	servers := []uint32{descriptor.ClusterID, basicinformation.ClusterID}
	if icd {
		servers = append(servers, icddef.ClusterID)
	}
	b.endpoint(0, []uint64{dtRootNode}, servers)
	b.cluster(0, basicinformation.ClusterID, 0, map[uint32]spec.Node{basicinformation.AttrSpecificationVersion: uintNode(version)})
	if icd {
		b.cluster(0, icddef.ClusterID, uint64(features), map[uint32]spec.Node{icddef.AttrIdleModeDuration: uintNode(3600)})
	}
	n.apply(wildcard, &b.r, true)
	return n
}

func TestLITSupportedParity(t *testing.T) {
	t.Parallel()
	lit := icddef.FeatureCheckInProtocolSupport | icddef.FeatureLongIdleTimeSupport | icddef.FeatureUserActiveModeTrigger
	t.Run("litSupported", func(t *testing.T) {
		t.Parallel()
		t.Run("exports MIN_LIT_SPECIFICATION_VERSION as 0x01040000", func(t *testing.T) {
			t.Parallel()
			if MinLITSpecificationVersion != 0x01040000 {
				t.Fatal(MinLITSpecificationVersion)
			}
		})
		t.Run("returns true for a LIT peer with specificationVersion >= 1.4.0 (default)", func(t *testing.T) {
			t.Parallel()
			if !litImage(t, 0x0106_0100, true, lit).LITSupported() {
				t.Fatal("not LIT")
			}
		})
		t.Run("returns true for a LIT peer whose specificationVersion is exactly 1.4.0 (0x01040000)", func(t *testing.T) {
			t.Parallel()
			if !litImage(t, 0x0104_0000, true, lit).LITSupported() {
				t.Fatal("not LIT")
			}
		})
		t.Run("returns false for a LIT peer whose specificationVersion is 1.3.0 (0x01030000)", func(t *testing.T) {
			t.Parallel()
			if litImage(t, 0x0103_0000, true, lit).LITSupported() {
				t.Fatal("LIT")
			}
		})
		t.Run("returns false for a non-ICD peer (no LongIdleTimeSupport feature)", func(t *testing.T) {
			t.Parallel()
			if litImage(t, 0x0106_0100, true, icddef.FeatureCheckInProtocolSupport).LITSupported() {
				t.Fatal("LIT")
			}
		})
		t.Run("returns false for a peer with no ICD cluster at all", func(t *testing.T) {
			t.Parallel()
			if litImage(t, 0x0106_0100, false, 0).LITSupported() {
				t.Fatal("LIT")
			}
		})
	})
}

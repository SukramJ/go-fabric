// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/client/interaction"
	mattercore "github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/im/subscription"
	"github.com/SukramJ/go-fabric/node"
)

const clusterGroupKeyManagement uint32 = 0x003F

// TestLimitsAttributes: every capacity field of node.Limits is the root
// attribute of its name — the configured value when set, the default (the
// constant the node had before the field) when zero, one table over the
// fields.
func TestLimitsAttributes(t *testing.T) {
	t.Parallel()
	set := node.Limits{
		AccessControlEntriesPerFabric: 8,
		SubjectsPerAccessControlEntry: 6,
		TargetsPerAccessControlEntry:  3,
		MaxGroupsPerFabric:            12,
		MaxGroupKeysPerFabric:         7,
	}
	fields := []struct {
		name      string
		cluster   uint32
		attribute uint32
		set, def  uint16
	}{
		{"SubjectsPerAccessControlEntry", clusterAccessControl, 0x0002, set.SubjectsPerAccessControlEntry, mattercore.DefaultSubjectsPerAccessControlEntry},
		{"TargetsPerAccessControlEntry", clusterAccessControl, 0x0003, set.TargetsPerAccessControlEntry, mattercore.DefaultTargetsPerAccessControlEntry},
		{"AccessControlEntriesPerFabric", clusterAccessControl, 0x0004, set.AccessControlEntriesPerFabric, mattercore.DefaultAccessControlEntriesPerFabric},
		{"MaxGroupsPerFabric", clusterGroupKeyManagement, 0x0002, set.MaxGroupsPerFabric, mattercore.DefaultMaxGroupsPerFabric},
		{"MaxGroupKeysPerFabric", clusterGroupKeyManagement, 0x0003, set.MaxGroupKeysPerFabric, mattercore.DefaultMaxGroupKeysPerFabric},
	}
	for name, limits := range map[string]node.Limits{"zero": {}, "configured": set} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			cfg := minimalConfig(openDB(t, filepath.Join(t.TempDir(), "node.db")))
			cfg.Limits = limits
			s := paseSession(t, newNode(t, cfg))
			for _, f := range fields {
				want := f.def
				if name == "configured" {
					want = f.set
				}
				report, err := s.Read(context.Background(), attr(0, f.cluster, f.attribute))
				if err != nil {
					t.Fatalf("read %s: %v", f.name, err)
				}
				if v, ok := report.Value(0, f.cluster, f.attribute); !ok || v.Uint != uint64(want) {
					t.Errorf("%s = %+v (%v), want %d", f.name, v, ok, want)
				}
			}
		})
	}
}

// TestLimitsSubscriptionMaxInterval: a subscriber asking for a longer
// MaxInterval is granted Limits.SubscriptionMaxInterval, one hour when it is
// zero (subscription.DefaultMaxIntervalCeilingSeconds) — matter.js
// subscriptionOptions.maxInterval and MAX_INTERVAL_PUBLISHER_LIMIT
// (ServerSubscription.ts:71, :314-344 at 862ad75f).
//
// The subtests run one after the other: a fake site clock moves only while
// the process is quiet (holdSite).
func TestLimitsSubscriptionMaxInterval(t *testing.T) { //nolint:paralleltest // one fabrictest site at a time
	for name, tc := range map[string]struct {
		set, want time.Duration
	}{
		"zero":       {0, subscription.DefaultMaxIntervalCeilingSeconds * time.Second},
		"configured": {30 * time.Second, 30 * time.Second},
	} {
		t.Run(name, func(t *testing.T) {
			site := quietSite(t)
			p := site.CommissionedPair(fabrictest.PairOptions{Device: fabrictest.Topology{
				Configure: func(cfg *node.Config) { cfg.Limits.SubscriptionMaxInterval = tc.set },
			}})
			ctx := parityCtx(t)
			sub, err := fabrictest.Subscribe(ctx, p, interaction.SubscribeRequest{
				Attributes:         []im.ConcreteAttributePath{attr(0, clusterBasicInformation, attrNodeLabel)},
				MinIntervalFloor:   interaction.Interval(0),
				MaxIntervalCeiling: interaction.Interval(2 * time.Hour),
			})
			if err != nil {
				t.Fatal(err)
			}
			defer sub.Close()
			if got := sub.MaxInterval(); got != tc.want {
				t.Errorf("granted MaxInterval %v, want %v", got, tc.want)
			}
		})
	}
}

// TestLimitsValidate: a field outside its bound fails node.New with
// ErrInvalidConfig naming the field.
func TestLimitsValidate(t *testing.T) {
	t.Parallel()
	for name, limits := range map[string]node.Limits{
		"SubscriptionMaxInterval":       {SubscriptionMaxInterval: 2 * time.Hour},
		"SubscriptionMaxInterval/":      {SubscriptionMaxInterval: 1500 * time.Millisecond},
		"AccessControlEntriesPerFabric": {AccessControlEntriesPerFabric: 3},
		"SubjectsPerAccessControlEntry": {SubjectsPerAccessControlEntry: 65535},
		"TargetsPerAccessControlEntry":  {TargetsPerAccessControlEntry: 2},
		"BDXAdditionalMrpDelay":         {BDXAdditionalMrpDelay: -time.Millisecond},
	} {
		cfg := minimalConfig(openDB(t, filepath.Join(t.TempDir(), "node.db")))
		cfg.Limits = limits
		_, err := node.New(cfg)
		if !errors.Is(err, node.ErrInvalidConfig) || !strings.Contains(err.Error(), "Limits."+strings.TrimSuffix(name, "/")) {
			t.Errorf("New with %+v = %v, want ErrInvalidConfig naming Limits.%s", limits, err, strings.TrimSuffix(name, "/"))
		}
	}
}

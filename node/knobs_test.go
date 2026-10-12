// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package node_test

// The root's configuration knobs (issue #58): SupportedFabrics and
// TimeSynchronization, read over a PASE session.

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"testing"

	"github.com/SukramJ/go-fabric/bridge/bridgetest"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/node"
)

const (
	clusterOperationalCredentials uint32 = 0x003E
	attrSupportedFabrics          uint32 = 0x0002
	attrServerList                uint32 = 0x0001
	clusterGeneralDiagnostics     uint32 = 0x0033
	cmdTimeSnapshot               uint32 = 0x01
	cmdTimeSnapshotResponse       uint32 = 0x02
	clusterTimeSynchronization    uint32 = 0x0038
)

// paseSession starts n and opens a PASE session to it.
func paseSession(t *testing.T, n *node.Node) *bridgetest.Session {
	t.Helper()
	if err := n.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	c := bridgetest.Dial(t, n.Bridge().LocalAddr(), commissionerOptions())
	s, err := c.PASE(context.Background())
	if err != nil {
		t.Fatalf("PASE: %v", err)
	}
	return s
}

// TestSupportedFabrics: OperationalCredentials SupportedFabrics is the
// configured value, 5 when unset; 255 is refused by Validate.
func TestSupportedFabrics(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		set, want uint8
	}{
		"default":    {0, 5},
		"configured": {16, 16},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			cfg := minimalConfig(openDB(t, filepath.Join(t.TempDir(), "node.db")))
			cfg.SupportedFabrics = tc.set
			s := paseSession(t, newNode(t, cfg))
			report, err := s.Read(context.Background(), attr(0, clusterOperationalCredentials, attrSupportedFabrics))
			if err != nil {
				t.Fatalf("read SupportedFabrics: %v", err)
			}
			if v, ok := report.Value(0, clusterOperationalCredentials, attrSupportedFabrics); !ok || v.Uint != uint64(tc.want) {
				t.Errorf("SupportedFabrics = %+v (%v), want %d", v, ok, tc.want)
			}
		})
	}
	cfg := minimalConfig(openDB(t, filepath.Join(t.TempDir(), "node.db")))
	cfg.SupportedFabrics = 255
	if err := cfg.Validate(); !errors.Is(err, node.ErrInvalidConfig) {
		t.Errorf("Validate with SupportedFabrics 255 = %v, want ErrInvalidConfig", err)
	}
}

// TestTimeSynchronizationOptional: with DisableTimeSynchronization the
// root's ServerList lacks 0x0038 and GeneralDiagnostics TimeSnapshot
// still answers, with SystemTimeMs and a null PosixTimeMs; by default
// 0x0038 is listed and PosixTimeMs is set.
func TestTimeSynchronizationOptional(t *testing.T) {
	t.Parallel()
	for name, disable := range map[string]bool{"mounted": false, "disabled": true} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			cfg := minimalConfig(openDB(t, filepath.Join(t.TempDir(), "node.db")))
			cfg.DisableTimeSynchronization = disable
			n := newNode(t, cfg)
			if (n.Root().TimeSynchronization == nil) != disable {
				t.Errorf("Root().TimeSynchronization = %v with DisableTimeSynchronization %v", n.Root().TimeSynchronization, disable)
			}
			s := paseSession(t, n)
			ctx := context.Background()
			report, err := s.Read(ctx, attr(0, clusterDescriptor, attrServerList))
			if err != nil {
				t.Fatalf("read ServerList: %v", err)
			}
			v, ok := report.Value(0, clusterDescriptor, attrServerList)
			if !ok {
				t.Fatalf("no ServerList: %+v", report.Statuses)
			}
			if listed := slices.Contains(uintList(v), uint64(clusterTimeSynchronization)); listed == disable {
				t.Errorf("ServerList %v lists TimeSynchronization: %v, want %v", uintList(v), listed, !disable)
			}
			res, err := s.Invoke(ctx, im.ConcreteCommandPath{Endpoint: 0, Cluster: clusterGeneralDiagnostics, Command: cmdTimeSnapshot}, nil)
			if err != nil {
				t.Fatalf("TimeSnapshot: %v", err)
			}
			if res.Command != cmdTimeSnapshotResponse {
				t.Fatalf("TimeSnapshot answered %+v, want TimeSnapshotResponse", res)
			}
			if _, ok := res.Fields.Child(0); !ok {
				t.Error("TimeSnapshotResponse without SystemTimeMs")
			}
			posix, ok := res.Fields.Child(1)
			if !ok {
				t.Fatal("TimeSnapshotResponse without PosixTimeMs")
			}
			if posix.IsNull != disable {
				t.Errorf("PosixTimeMs null = %v, want %v (TimeSynchronization disabled: %v)", posix.IsNull, disable, disable)
			}
		})
	}
}

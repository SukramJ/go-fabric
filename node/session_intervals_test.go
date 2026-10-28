// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/node"
)

// Config.Sessions reaches what the node reports (Node.SessionParameters),
// and an active threshold the session parameters cannot carry fails New
// (matter.js SessionManager.ts:58-67).
func TestNodeReportsConfiguredSessionIntervals(t *testing.T) {
	t.Parallel()
	cfg := minimalConfig(openDB(t, filepath.Join(t.TempDir(), "n.db")))
	cfg.MaxPathsPerInvoke = 1
	cfg.Sessions = &node.SessionIntervals{Idle: 2 * time.Second, ActiveThreshold: 10 * time.Second}
	p := newNode(t, cfg).SessionParameters()
	if p.SessionIdleInterval != 2000 || p.SessionActiveInterval != 300 || p.SessionActiveThreshold != 10000 || p.MaxPathsPerInvoke != 1 {
		t.Fatalf("reported %+v", p)
	}

	bad := minimalConfig(openDB(t, filepath.Join(t.TempDir(), "b.db")))
	bad.Sessions = &node.SessionIntervals{ActiveThreshold: 70 * time.Second}
	if _, err := node.New(bad); !errors.Is(err, node.ErrInvalidConfig) {
		t.Fatalf("New with a 70 s threshold: %v, want ErrInvalidConfig", err)
	}
}

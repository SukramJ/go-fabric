// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"thingfabric.dev/go-fabric/cluster/wire"
	"thingfabric.dev/go-fabric/mdns"
	"thingfabric.dev/go-fabric/node"
	"thingfabric.dev/go-fabric/secure/spake2"
	"thingfabric.dev/go-fabric/store"
	"thingfabric.dev/go-fabric/surface"
)

// The facade names the sealer type a host passes as [node.Store].Sealer
// and to [node.Reseal] (review II, R4-api-4706, decision 657): the alias
// is store.Sealer itself, so either name takes the other's values.
var (
	_ node.Sealer  = (*store.AESGCMSealer)(nil)
	_ store.Sealer = node.Sealer(nil)
	_              = node.Store{Sealer: node.Sealer(nil)}
)

// TestOpenOwnWindowRefusesNegativeTimeout (review II, R4-api-4752): a
// negative timeout is refused as Config.Validate and OpenBasicWindow
// refuse one, and no window opens — the bridge would read it as the
// default timeout.
func TestOpenOwnWindowRefusesNegativeTimeout(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	cfg := minimalConfig(openDB(t, filepath.Join(t.TempDir(), "node.db")))
	cfg.Commissioning.WindowOnDemand = true
	n := newNode(t, cfg)
	if err := n.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if n.Window().Open {
		t.Fatal("a WindowOnDemand node opened its window at Start")
	}
	err := n.OpenOwnWindow(ctx, -time.Second)
	if err == nil || !strings.Contains(err.Error(), "negative") {
		t.Fatalf("OpenOwnWindow(-1s) = %v; want the negative timeout refused", err)
	}
	if n.Window().Open {
		t.Fatal("OpenOwnWindow(-1s) opened a window")
	}
	if err := n.OpenOwnWindow(ctx, 0); err != nil {
		t.Fatalf("OpenOwnWindow(0) after the refusal: %v", err)
	}
}

// TestJointWindowIsCommissioningMode3 (review II, R3-doc-3782): an
// administrator's joint commissioning window is advertised CM=3, and
// [node.WindowSnapshot].CommissioningMode reports it.
func TestJointWindowIsCommissioningMode3(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	n := startedNode(t, mdns.NewNoop())
	salt := bytes.Repeat([]byte{0x07}, 16)
	vc, err := spake2.NewVerifierContext(20202021, salt, 1000)
	if err != nil {
		t.Fatal(err)
	}
	l, err := vc.L.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	// The verifier on the wire is w0 (32 bytes) then L (65 bytes,
	// uncompressed), as spake2.NewVerifierFromValue reads it.
	verifier := append(vc.W0.FillBytes(make([]byte, spake2.VerifierW0Size)), l...)
	p := wire.OpenWindowParams{
		CommissioningTimeoutSeconds: 180, Discriminator: 100, IsUncommissioned: true, JointFabric: true,
		PAKEPasscodeVerifier: verifier, Iterations: 1000, Salt: salt,
	}
	if err := n.Bridge().CommissioningWindow().OpenWindow(ctx, p); err != nil {
		t.Fatalf("joint OpenWindow: %v", err)
	}
	if w := n.Window(); !w.Open || w.CommissioningMode != 3 || w.Discriminator != 100 {
		t.Fatalf("joint window snapshot open=%v CM=%d D=%d; want open, CM=3, D=100", w.Open, w.CommissioningMode, w.Discriminator)
	}
}

// TestTLSRefusedBelowItsLevel (review II, R4-doc-4754, decision 656):
// Config.TLS under a surface level without the TLS clusters is a
// configuration error at Validate and New — the level keeps both servers
// on the root ("kept: mandatory at pin") though it does not know them,
// and the node would hand them to its push transports. At a level that
// has them the configuration is valid.
func TestTLSRefusedBelowItsLevel(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		level surface.Level
		ok    bool
	}{
		{"1.4", false},
		{"1.4.2", false},
		{"1.5.1", true},
		{"", true},
	} {
		cfg := minimalConfig(openDB(t, filepath.Join(t.TempDir(), "node.db")))
		cfg.TLS = &node.TLSConfig{CertificatePort: tlsKeys{}}
		cfg.Surface = surface.Policies{Level: tc.level}
		err := cfg.Validate()
		if tc.ok {
			if err != nil {
				t.Errorf("level %q: Validate = %v; want nil", tc.level, err)
			}
			continue
		}
		if err == nil || !strings.Contains(err.Error(), "TLS") {
			t.Errorf("level %q: Validate = %v; want Config.TLS refused", tc.level, err)
		}
		if n, err := node.New(context.Background(), cfg); err == nil {
			_ = n.Stop(context.Background())
			t.Errorf("level %q: New built a node with Config.TLS", tc.level)
		} else if !strings.Contains(err.Error(), "TLS") {
			t.Errorf("level %q: New = %v; want Config.TLS refused", tc.level, err)
		}
	}
}

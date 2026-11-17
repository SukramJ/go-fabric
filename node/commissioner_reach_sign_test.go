// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"bytes"
	"context"
	"slices"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/node"
	"github.com/SukramJ/go-fabric/store"
)

// TestCommissionerReachSignsOverOneSession (review II, R2-P6-conc-2451):
// SignVIDVerification sends its request over one session of the peer and
// returns that session's attestation challenge — the one the peer signed.
func TestCommissionerReachSignsOverOneSession(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	site := fabrictest.NewSite(t, fabrictest.Options{Clock: clock.System{}})
	commissioner := site.AddDevice(fabrictest.Topology{})
	reader := site.AddDevice(fabrictest.Topology{Configure: func(cfg *node.Config) { cfg.Client = &node.ClientConfig{} }})
	c := site.AddController(fabrictest.ControllerOptions{})
	pairC, err := c.Commission(ctx, commissioner, fabrictest.CommissionOptions{})
	if err != nil {
		t.Fatalf("commission the commissioner: %v", err)
	}
	pairR, err := c.Commission(ctx, reader, fabrictest.CommissionOptions{})
	if err != nil {
		t.Fatalf("commission the reader: %v", err)
	}
	if err := commissioner.Node().Store().ReplaceACL(ctx, 1, []store.ACLEntry{{
		FabricIndex: 1, Privilege: store.PrivilegeAdminister, AuthMode: store.AuthModeCASE,
		Subjects: []uint64{c.Fabric().RootNodeID, pairR.Address.NodeID},
	}}); err != nil {
		t.Fatal(err)
	}
	challenge, sessions, err := node.CommissionerReachSign(ctx, reader.Node(), pairR.Address.FabricIndex, pairC.Address.NodeID, 1, bytes.Repeat([]byte{7}, 32))
	if err != nil {
		t.Fatalf("SignVIDVerification: %v", err)
	}
	if len(challenge) == 0 || !slices.ContainsFunc(sessions, func(s []byte) bool { return bytes.Equal(s, challenge) }) {
		t.Errorf("challenge %x is not the attestation challenge of a session to the peer (%x)", challenge, sessions)
	}
}

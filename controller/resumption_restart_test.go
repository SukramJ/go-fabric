// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controller_test

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"thingfabric.dev/go-fabric/client/peer"
	"thingfabric.dev/go-fabric/controller"
	"thingfabric.dev/go-fabric/controller/attestation"
	"thingfabric.dev/go-fabric/device"
	"thingfabric.dev/go-fabric/endpoint"
	"thingfabric.dev/go-fabric/fabrictest"
	"thingfabric.dev/go-fabric/node"
	"thingfabric.dev/go-fabric/store"
)

// TestControllerResumesAcrossARestart: the controller keeps its CASE
// resumption records in its database, one per peer, so a controller
// restarted on the same database resumes with a node instead of running a
// full CASE (review II, R2-P5-port-06, F-db021da0-19; matter.js keeps one
// persisted record per peer, SessionManager.ts:261, :896-908 at db021da0).
func TestControllerResumesAcrossARestart(t *testing.T) {
	t.Parallel()
	site := fabrictest.NewSite(t, fabrictest.Options{Loopback: true, Logger: testLogger(t)})
	light, err := device.NewOnOffLight(device.Options{StableKey: endpoint.StringKey("ep1"), Name: "ep1"})
	if err != nil {
		t.Fatal(err)
	}
	dev := site.AddDevice(fabrictest.Topology{Bridged: []node.Endpoint{light}})
	cfg := site.ControllerConfig(fabrictest.ControllerOptions{})
	run := func(fn func(ctx context.Context) error) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		if err := site.Run(ctx, fn); err != nil {
			t.Fatal(err)
		}
	}
	var c *controller.Controller
	run(func(ctx context.Context) (err error) { c, err = controller.New(ctx, cfg); return err })
	var addr peer.Descriptor
	run(func(ctx context.Context) error {
		n, err := c.Pair(ctx, manualCode(t, dev), controller.PairOptions{OnAttestationFailure: attestation.Accept()})
		if err != nil {
			return err
		}
		a, _ := n.PeerAddress()
		addr = peer.Descriptor{Address: a}
		return nil
	})
	run(c.Close)

	// The restart: a new socket, the same database.
	again := site.ControllerConfig(fabrictest.ControllerOptions{})
	again.DB = cfg.DB
	run(func(ctx context.Context) (err error) { c, err = controller.New(ctx, again); return err })
	defer run(c.Close)
	run(func(ctx context.Context) error {
		pr, err := c.Peers().For(addr.Address)
		if err != nil {
			return err
		}
		s, err := pr.Connect(ctx, peer.ConnectOptions{})
		if err != nil {
			return err
		}
		if !s.Resumed() {
			t.Error("the restarted controller ran a full CASE, want a resumption")
		}
		return nil
	})
}

// TestControllerTakeOverDropsFormerResumptions: when the controller takes
// over a fabric row left by the former own node (another node id on the
// same fabric), the row's resumption records go with it — they were made
// under that node's id, and resuming with one would hand the peer a session
// it takes for that node (review II, Fable P-4). A row of the controller's
// own id keeps its records (TestControllerResumesAcrossARestart).
func TestControllerTakeOverDropsFormerResumptions(t *testing.T) {
	t.Parallel()
	var cfg controller.Config
	site, _, c, n := sitePair(t, fabrictest.Topology{}, func(c *controller.Config) { cfg = *c })
	addr, ok := n.PeerAddress()
	if !ok {
		t.Fatal("the paired node has no address")
	}
	idx := c.Fabric().FabricIndex
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	if err := site.Run(ctx, c.Close); err != nil {
		t.Fatal(err)
	}
	st := store.New(cfg.DB)
	former, err := st.GetResumptionByPeer(ctx, idx, addr.NodeID)
	if err != nil {
		t.Fatalf("no resumption record after the pairing: %v", err)
	}
	// The row as the former own node left it: its node id, not RootNodeID.
	if err := st.UpdateFabricNodeID(ctx, idx, 0x0BADC0DE); err != nil {
		t.Fatal(err)
	}
	again := site.ControllerConfig(fabrictest.ControllerOptions{})
	again.DB = cfg.DB
	var c2 *controller.Controller
	if err := site.Run(ctx, func(ctx context.Context) (err error) { c2, err = controller.New(ctx, again); return err }); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = site.Run(context.WithoutCancel(ctx), c2.Close) })
	// The new controller may establish a session of its own with the node
	// at once and store a fresh record; what must be gone is the former
	// node's — its resumption id never comes back.
	got, err := st.GetResumptionByPeer(ctx, idx, addr.NodeID)
	switch {
	case errors.Is(err, store.ErrResumptionNotFound):
	case err != nil:
		t.Fatal(err)
	case bytes.Equal(got.ResumptionID, former.ResumptionID):
		t.Fatalf("the former own node's resumption record survived the take-over (id %x)", got.ResumptionID)
	}
}

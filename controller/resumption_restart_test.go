// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controller_test

import (
	"context"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/client/peer"
	"github.com/SukramJ/go-fabric/controller"
	"github.com/SukramJ/go-fabric/controller/attestation"
	"github.com/SukramJ/go-fabric/device"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/node"
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

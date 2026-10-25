// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package client_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/controller/commissioner"
	"github.com/SukramJ/go-fabric/fabrictest"
)

// TestSetFabricLabel: Registry.SetFabricLabel renames our fabric on the
// connected node at once, keeps the label across a controller restart, and
// refuses a label of 33 characters.
func TestSetFabricLabel(t *testing.T) {
	t.Parallel()
	site := fabrictest.NewSite(t, fabrictest.Options{})
	pair := site.UncommissionedPair(fabrictest.PairOptions{})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	n, err := pair.Controller.CommissionNode(ctx, pair.Device, client.CommissionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := site.Eventually(ctx, "the node is connected", func() bool { return n.ConnectionState() == client.Connected }); err != nil {
		t.Fatal(err)
	}
	reg := pair.Controller.Registry()
	if err := reg.SetFabricLabel(ctx, strings.Repeat("x", 33)); !errors.Is(err, commissioner.ErrLabel) {
		t.Fatalf("a 33-character label: %v, want ErrLabel", err)
	}
	if err := site.Run(ctx, func(ctx context.Context) error { return reg.SetFabricLabel(ctx, "living room") }); err != nil {
		t.Fatal(err)
	}
	fabrics, err := pair.Device.Node().FabricInfos(ctx)
	if err != nil || len(fabrics) != 1 || fabrics[0].Label != "living room" {
		t.Fatalf("the device's fabrics %+v (%v), want the label living room", fabrics, err)
	}
	if got := pair.Controller.Fabric().CurrentLabel(); got != "living room" {
		t.Fatalf("CurrentLabel %q", got)
	}
	if err := pair.Controller.Restart(ctx); err != nil {
		t.Fatal(err)
	}
	if got := pair.Controller.Fabric().CurrentLabel(); got != "living room" {
		t.Fatalf("CurrentLabel after a restart %q, want the stored living room", got)
	}
}

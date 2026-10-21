// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package fabrictest_test

import (
	"context"
	"testing"

	"github.com/SukramJ/go-fabric/client/interaction"
	basicinfo "github.com/SukramJ/go-fabric/cluster/spec/basicinformation"
	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/im"
)

// TestControllerRestart: a controller restarted on its stores keeps its
// fabric and its client nodes — the registry restores the stored node,
// which reads the device over a new CASE session — and the registry edit
// runs on every start.
func TestControllerRestart(t *testing.T) {
	t.Parallel()
	site := fabrictest.NewSite(t, fabrictest.Options{})
	ctx := testContext(t)
	edits := 0
	p := site.CommissionedPair(fabrictest.PairOptions{Controller: fabrictest.ControllerOptions{
		Registry: func(*client.RegistryConfig) { edits++ },
	}})
	n, err := p.Node()
	if err != nil {
		t.Fatal(err)
	}
	id, fabricID := n.ID(), p.Controller.Fabric().Identity.CompressedFabricID
	if err := p.Controller.Restart(ctx); err != nil {
		t.Fatal(err)
	}
	if edits != 2 {
		t.Fatalf("the registry edit ran %d times, want 2", edits)
	}
	if got := p.Controller.Fabric().Identity.CompressedFabricID; got != fabricID {
		t.Fatalf("the fabric changed across the restart: %x, want %x", got, fabricID)
	}
	restored := p.Controller.Registry().Get(id)
	if restored == nil || !restored.IsCommissioned() {
		t.Fatalf("node %q not restored as commissioned: %v", id, restored)
	}
	attr := rootAttr(basicinfo.ClusterID, basicinfo.AttrNodeLabel)
	err = site.Run(ctx, func(ctx context.Context) error {
		_, err := restored.Read(ctx, client.ReadRequest{ReadRequest: interaction.ReadRequest{Attributes: []im.ConcreteAttributePath{attr}}})
		return err
	})
	if err != nil {
		t.Fatalf("read after the restart: %v", err)
	}
}

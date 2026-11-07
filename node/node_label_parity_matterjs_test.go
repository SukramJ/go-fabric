// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/SukramJ/go-fabric/client/interaction"
	bidef "github.com/SukramJ/go-fabric/cluster/spec/basicinformation"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/node"
)

// nodeLabelChanges collects the root NodeLabel changes the node's
// observers hear, by origin.
type nodeLabelChanges struct {
	mu   sync.Mutex
	list []contract.Origin
}

func (c *nodeLabelChanges) observe(ac node.AttributeChange) {
	if ac.Endpoint != 0 || ac.Cluster != bidef.ClusterID || !slices.Contains(ac.Change.Attributes, bidef.AttrNodeLabel) {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.list = append(c.list, ac.Change.Origin)
}

func (c *nodeLabelChanges) count(origin contract.Origin) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for _, o := range c.list {
		if o == origin {
			n++
		}
	}
	return n
}

func (c *nodeLabelChanges) snapshot() []contract.Origin {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.list)
}

// TestSetNodeLabelReportedPersistedParity (issue 414, cases a and b): the
// host's Node.SetNodeLabel is reported to a subscribed controller and to
// Observers.OnAttributeChanged with OriginHostSet, and is persisted — the
// node restarted on its store serves it; a controller's later write wins
// and reaches the observers with OriginControllerWrite. matter.js has no
// test of its own for a host's set: its host assigns
// state.basicInformation.nodeLabel, and the datasource commits, persists
// and reports it as it does a controller's write
// (mattercore.BasicInformation.SetNodeLabel cites the lines).
func TestSetNodeLabelReportedPersistedParity(t *testing.T) {
	t.Parallel()
	site := quietSite(t)
	changes := &nodeLabelChanges{}
	p := site.CommissionedPair(fabrictest.PairOptions{Device: fabrictest.Topology{
		Configure: func(cfg *node.Config) { cfg.Observers.OnAttributeChanged = changes.observe },
	}})
	ctx := parityCtx(t)
	label := lifecycleAttr(0, bidef.ClusterID, bidef.AttrNodeLabel)

	var mu sync.Mutex
	var reported []string
	sub, err := fabrictest.Subscribe(ctx, p, interaction.SubscribeRequest{
		Attributes: []im.ConcreteAttributePath{label},
		Updated: func(r *interaction.Report) error {
			if v, ok := r.Value(0, bidef.ClusterID, bidef.AttrNodeLabel); ok {
				mu.Lock()
				reported = append(reported, v.El.String)
				mu.Unlock()
			}
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(sub.Close)

	if err := p.Device.Act(func(n *node.Node) error { return n.SetNodeLabel(ctx, "Wohnzimmer") }); err != nil {
		t.Fatalf("SetNodeLabel: %v", err)
	}
	if err := site.Eventually(ctx, "the host's label reported", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return slices.Contains(reported, "Wohnzimmer")
	}); err != nil {
		t.Fatal(err)
	}
	if got := changes.count(contract.OriginHostSet); got != 1 {
		t.Errorf("observers heard %d host changes of NodeLabel, want 1 (%v)", got, changes.snapshot())
	}

	// Persisted: the node restarted on its store serves the host's label.
	if err := p.Device.Restart(ctx); err != nil {
		t.Fatal(err)
	}
	if got, err := fabrictest.Read(ctx, p, label); err != nil || got.Value.El.String != "Wohnzimmer" {
		t.Fatalf("NodeLabel after the restart = %+v (%v), want Wohnzimmer", got.Value.El, err)
	}

	// A controller's later write wins and is reported to the host.
	if err := fabrictest.Write(ctx, p, label, strVal("Küche")); err != nil {
		t.Fatal(err)
	}
	if got, err := fabrictest.Read(ctx, p, label); err != nil || got.Value.El.String != "Küche" {
		t.Fatalf("NodeLabel after the controller's write = %+v (%v), want Küche", got.Value.El, err)
	}
	if err := site.Eventually(ctx, "the controller's write observed", func() bool {
		return changes.count(contract.OriginControllerWrite) == 1
	}); err != nil {
		t.Fatal(err)
	}
	if got := changes.count(contract.OriginHostSet); got != 1 {
		t.Errorf("observers heard %d host changes of NodeLabel, want 1 — the restore at New reports nothing (%v)", got, changes.snapshot())
	}
}

// TestSetNodeLabelBeforeStartAndRefusedParity (issue 414, cases c and d): a
// label set between New and Start is the one the node serves at start; a
// label over 32 bytes is refused with the status a controller's write of
// it gets, CONSTRAINT_ERROR ("max 32", basic-information.element.ts:35-38
// at db021da0), and the served label stays.
func TestSetNodeLabelBeforeStartAndRefusedParity(t *testing.T) {
	t.Parallel()
	site := quietSite(t)
	var prepErr error
	p := site.CommissionedPair(fabrictest.PairOptions{Device: fabrictest.Topology{
		Prepare: func(n *node.Node) { prepErr = n.SetNodeLabel(context.Background(), "Früh") },
	}})
	if prepErr != nil {
		t.Fatalf("SetNodeLabel before Start: %v", prepErr)
	}
	ctx := parityCtx(t)
	label := lifecycleAttr(0, bidef.ClusterID, bidef.AttrNodeLabel)
	if got, err := fabrictest.Read(ctx, p, label); err != nil || got.Value.El.String != "Früh" {
		t.Fatalf("NodeLabel at start = %+v (%v), want Früh", got.Value.El, err)
	}

	long := strings.Repeat("x", 33)
	hostErr := p.Device.Act(func(n *node.Node) error { return n.SetNodeLabel(ctx, long) })
	sce, ok := errors.AsType[im.StatusCodeError](hostErr)
	if !ok || sce.MatterStatusCode() != im.StatusConstraintError {
		t.Errorf("host: %v, want CONSTRAINT_ERROR", hostErr)
	}
	wireErr := fabrictest.Write(ctx, p, label, strVal(long))
	se, ok := errors.AsType[*interaction.StatusError](wireErr)
	if !ok || se.Status != im.StatusConstraintError {
		t.Errorf("controller: %v, want CONSTRAINT_ERROR", wireErr)
	}
	if got, err := fabrictest.Read(ctx, p, label); err != nil || got.Value.El.String != "Früh" {
		t.Fatalf("NodeLabel after the refusals = %+v (%v), want Früh", got.Value.El, err)
	}
}

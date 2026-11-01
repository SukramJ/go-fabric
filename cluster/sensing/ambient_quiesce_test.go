// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package sensing_test

import (
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/cluster/sensing"
	def "github.com/SukramJ/go-fabric/cluster/spec/ambientcontextsensing"
	"github.com/SukramJ/go-fabric/contract"
)

// TestAmbientContextMatterQuiesce: the bridge's MatterQuiesce cancels the
// hold-time timer, so the detection does not end — and emits no
// AmbientContextDetectEnded — after the server left the topology, as
// connectedhomeip's Shutdown cancels it
// (AmbientContextSensingCluster.cpp:90-98 at 6170af84; W3-club2-02).
func TestAmbientContextMatterQuiesce(t *testing.T) {
	t.Parallel()
	clk := newFake()
	c := newACS(t, sensing.AmbientContextConfig{
		Features: sensing.FeatureHumanActivity, HoldTimeLimits: limits(10, 300, 20), AmbientContextTypeSupported: []sensing.SemanticTag{haFall}, Clock: clk,
	})
	var q contract.ClusterQuiescer = c
	if err := c.AddDetection(detect(haFall)); err != nil {
		t.Fatal(err)
	}
	if n := clk.Pending(); n != 1 {
		t.Fatalf("%d timers armed, want the hold-time timer", n)
	}
	q.MatterQuiesce()
	if n := clk.Pending(); n != 0 {
		t.Fatalf("%d timers armed after MatterQuiesce, want 0", n)
	}
	clk.Advance(time.Hour)
	if !read[bool](t, c, def.AttrHumanActivityDetected) {
		t.Error("the detection ended after MatterQuiesce")
	}
}
